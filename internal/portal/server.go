package portal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Options struct {
	Identity      IdentityProvider
	Gateway       PaymentGateway
	Origin        string
	SecureCookies bool
}
type Server struct {
	pool    *pgxpool.Pool
	options Options
	limits  *limiter
}

func New(pool *pgxpool.Pool, options Options) *Server {
	if options.Identity == nil {
		options.Identity = NewSupabase("", "")
	}
	if options.Gateway == nil {
		options.Gateway = NewRazorpay("", "", "")
	}
	if options.Origin == "" {
		options.Origin = "http://localhost:3000"
	}
	return &Server{pool: pool, options: options, limits: &limiter{entries: make(map[string]limitEntry)}}
}
func (s *Server) Origin() string { return s.options.Origin }

// Search strings may contain names or phone numbers. Log paths without query data.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		defer func() {
			slog.Info("http request", "method", r.Method, "path", r.URL.Path, "status", wrapped.Status(), "duration", time.Since(start))
		}()
		next.ServeHTTP(wrapped, r)
	})
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Warn("response encoding", "error", err)
	}
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func serverError(w http.ResponseWriter, err error) {
	slog.Error("portal request", "error", err)
	fail(w, 500, "The request could not be completed. Please try again.")
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		fail(w, 415, "Send application/json.")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		fail(w, 400, "The request contains invalid or unexpected fields.")
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		fail(w, 400, "Send a single JSON object.")
		return false
	}
	return true
}
func token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func tokenHash(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }
func validToken(t string) bool  { b, err := hex.DecodeString(t); return err == nil && len(b) == 32 }

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validUUID(v string) bool { return uuidPattern.MatchString(v) }

// Cookie-authenticated writes require a custom header and an explicit origin.
// CORS never grants a different website permission to supply that header.
func (s *Server) BrowserWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if r.Header.Get("X-Madamgy-Request") != "1" || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != s.options.Origin) {
				fail(w, 403, "Request origin is not allowed.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type Actor struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Active bool   `json:"active"`
}
type actorKey struct{}

func Current(ctx context.Context) Actor { a, _ := ctx.Value(actorKey{}).(Actor); return a }

const sessionCookie = "madamgy_session"
const sessionLifetime = 12 * time.Hour

func requestToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}
func (s *Server) setCookie(w http.ResponseWriter, value string, maxAge int) {
	c := &http.Cookie{Name: sessionCookie, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: s.options.SecureCookies, SameSite: http.SameSiteLaxMode}
	if maxAge < 0 {
		c.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, c)
}
func (s *Server) RequireStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		v := requestToken(r)
		if !validToken(v) {
			fail(w, 401, "Sign in to continue.")
			return
		}
		var a Actor
		err := s.pool.QueryRow(r.Context(), `SELECT a.id, a.name, a.email, a.role, a.active
			FROM app_private.staff_session s JOIN app_private.staff_account a ON a.id=s.staff_id
			WHERE s.token_hash=$1 AND s.expires_at > now() AND a.active`, tokenHash(v)).Scan(&a.ID, &a.Name, &a.Email, &a.Role, &a.Active)
		if errors.Is(err, pgx.ErrNoRows) {
			s.setCookie(w, "", -1)
			fail(w, 401, "Your session ended. Sign in again.")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	})
}
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Current(r.Context()).Role != "admin" {
			fail(w, 403, "Administrator access is required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func CanAccessProfile(ctx context.Context, pool *pgxpool.Pool, childID string) (bool, error) {
	a := Current(ctx)
	if a.Role == "admin" {
		return true, nil
	}
	if a.ID == "" {
		return false, nil
	}
	var allowed bool
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_private.consultation_registration WHERE child_id=$1 AND assigned_doctor_id=$2)`, childID, a.ID).Scan(&allowed)
	return allowed, err
}
func (s *Server) RequireProfile(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, err := CanAccessProfile(r.Context(), s.pool, chi.URLParam(r, "childID"))
		if err != nil {
			serverError(w, err)
			return
		}
		if !allowed {
			fail(w, 404, "Child record not found.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type limitEntry struct {
	count int
	until time.Time
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]limitEntry
}

func (l *limiter) allow(key string, maximum int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.entries) >= 10000 {
		for k, e := range l.entries {
			if !now.Before(e.until) {
				delete(l.entries, k)
			}
		}
		if len(l.entries) >= 10000 {
			return false
		}
	}
	e := l.entries[key]
	if !now.Before(e.until) {
		e = limitEntry{until: now.Add(time.Minute)}
	}
	e.count++
	l.entries[key] = e
	return e.count <= maximum
}
func (s *Server) Limit(bucket string, maximum int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			// Forwarded addresses are intentionally not trusted. Deployment proxies should
			// additionally rate-limit at their verified client address boundary.
			if !s.limits.allow(bucket+":"+host, maximum) {
				w.Header().Set("Retry-After", "60")
				fail(w, 429, "Too many requests. Please try again in a minute.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	if !s.options.Identity.Enabled() {
		fail(w, 503, "Staff sign-in is not configured yet.")
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	if !validEmail(body.Email) || len(body.Password) == 0 || len(body.Password) > 256 {
		fail(w, 400, "Enter an email address and password.")
		return
	}
	if !s.limits.allow("email:"+tokenHash(body.Email), 10) {
		fail(w, 429, "Too many sign-in attempts. Try again in a minute.")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	// Serialize session creation with password resets and account deactivation.
	// A login already checking an old password must not outlive their revocation.
	var a Actor
	err = tx.QueryRow(r.Context(), `SELECT id,name,email,role,active FROM app_private.staff_account WHERE email=$1 AND active FOR SHARE`, body.Email).Scan(&a.ID, &a.Name, &a.Email, &a.Role, &a.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 401, "Email or password is incorrect, or staff access is inactive.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	identity, err := s.options.Identity.SignIn(r.Context(), body.Email, body.Password)
	if errors.Is(err, ErrCredentials) || (err == nil && identity.ID != a.ID) {
		fail(w, 401, "Email or password is incorrect.")
		return
	}
	if err != nil {
		slog.Error("staff sign-in", "error", err)
		fail(w, 502, "Sign-in is temporarily unavailable.")
		return
	}
	v := token()
	_, err = tx.Exec(r.Context(), `INSERT INTO app_private.staff_session(token_hash,staff_id,expires_at) VALUES ($1,$2,$3)`, tokenHash(v), a.ID, time.Now().Add(sessionLifetime))
	if err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	if _, err := s.pool.Exec(r.Context(), `DELETE FROM app_private.staff_session WHERE expires_at < now()`); err != nil {
		slog.Warn("expired session cleanup", "error", err)
	}
	s.setCookie(w, v, int(sessionLifetime.Seconds()))
	respond(w, 200, a)
}
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	if _, err := s.pool.Exec(r.Context(), `DELETE FROM app_private.staff_session WHERE token_hash=$1`, tokenHash(requestToken(r))); err != nil {
		serverError(w, err)
		return
	}
	s.setCookie(w, "", -1)
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) Me(w http.ResponseWriter, r *http.Request) { respond(w, 200, Current(r.Context())) }

func (s *Server) Routes(r chi.Router) {
	r.Post("/api/payments/webhook", s.Webhook)
	r.Group(func(r chi.Router) {
		r.Use(s.BrowserWrite)
		r.With(s.Limit("login", 30)).Post("/api/auth/login", s.Login)
		r.With(s.Limit("intake", 60)).Post("/api/public/registrations", s.Register)
		r.Get("/api/public/settings", s.PublicSettings)
		r.With(s.Limit("checkout", 120)).Route("/api/public/registrations/{id}", func(r chi.Router) {
			r.Get("/", s.PublicRegistration)
			r.Post("/order", s.CreateOrder)
			r.Post("/verify", s.VerifyPayment)
		})
		r.Group(func(r chi.Router) {
			r.Use(s.RequireStaff)
			r.Get("/api/auth/me", s.Me)
			r.Post("/api/auth/logout", s.Logout)
			r.Post("/api/auth/password", s.ChangePassword)
			r.Get("/api/registrations", s.ListRegistrations)
			r.Get("/api/registrations/{id}", s.GetRegistration)
			r.Patch("/api/registrations/{id}", s.UpdateRegistration)
			r.Route("/api/admin", func(r chi.Router) {
				r.Use(AdminOnly)
				r.Get("/staff", s.ListStaff)
				r.Post("/staff", s.CreateStaff)
				r.Patch("/staff/{id}", s.UpdateStaff)
				r.Post("/staff/{id}/password", s.ResetPassword)
				r.Get("/settings", s.AdminSettings)
				r.Put("/settings", s.SaveSettings)
			})
		})
	})
}

func notFound(w http.ResponseWriter, err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "Record not found.")
		return true
	}
	if err != nil {
		serverError(w, fmt.Errorf("load record: %w", err))
		return true
	}
	return false
}
