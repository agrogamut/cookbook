// Package api wires the chi router and holds one handler file per resource, following
// CLAUDE.md's internal/api/handlers/ convention.
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/api/handlers"
	"github.com/madamgy/recipie/internal/portal"
)

// Staff routes require a revocable session. Intake, settings and signed payment
// notifications have their own public access rules.
// printTimeout bounds a PDF request. Generous because it covers the slowest real case --
// two books printed in one request on a small instance -- and because the alternative to
// waiting is an operator retrying a request that was going to succeed. Raised from 180s after
// two rounds of live testing against the real Gemini API (not aidraft.Disabled), each worse
// than the last:
//
//  1. A synthetic worst-case profile -- two allergen exclusions plus an active clinical
//     condition, narrow enough to trip the AI-invented-recipe fallback in more than one
//     chapter -- measured real assembly at 3m17s.
//  2. A real operator intake (West Bengal, the region this project's own coverage numbers
//     already name as the corpus's weakest -- 12.8%, CLAUDE.md's "What the external join
//     actually achieved") combined with a restricted diet and two active conditions. Four of
//     seven Book 2 chapters had zero real corpus matches at all and needed full AI invention,
//     and roughly a third of those invented-recipe calls hit a transient 503 from Gemini
//     ("model is currently experiencing high demand") -- each one absorbed cleanly (the
//     chapter just reports itself short rather than retrying or hanging; see topUpInvented's
//     own doc comment), but real assembly still ran 11m7s end to end.
//
// 1200s leaves real margin above the worse of those two measured cases. A future fix worth
// doing is bounding topUpInvented's per-slot calls with the same concurrency draftConcurrently
// already gives doctor/modification/food-group drafting, which would cut a fully-invented
// chapter's wall time by roughly maxDraftConcurrency; not done here because topUpInvented's
// sequential "stop at the first failure, exclude what this call already served" design is
// deliberate (see its own doc comment) and reworking it needs more thought than a timeout
// bump does. See aidraft.perCallTimeout for the per-call bound that keeps any single stalled
// upstream call from silently consuming this whole budget the way one did before that fix
// existed.
//
// It stays below any sensible proxy timeout, so a caller sees this service's own error
// rather than a gateway's.
const printTimeout = 1200 * time.Second

func NewRouter(pool *pgxpool.Pool, drafter aidraft.Drafter, access ...*portal.Server) http.Handler {
	security := portal.New(pool, portal.Options{})
	if len(access) > 0 && access[0] != nil {
		security = access[0]
	}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(portal.RequestLogger)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{security.Origin()},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "X-Madamgy-Request", "X-Registration-Token"},
		AllowCredentials: true,
		// A browser hides every non-simple response header unless it is named here. The
		// omissions list is what a book does not contain, so a frontend that cannot read
		// it would render a book as though nothing had been left out.
		ExposedHeaders: []string{"X-Book-Omissions"},
		MaxAge:         300,
	}))
	h := handlers.New(pool, drafter)
	r.Get("/healthz", h.Healthz)
	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(30 * time.Second))
		security.Routes(r)
	})

	// Two sibling groups, not nested: a chi middleware.Timeout wraps the request context in
	// context.WithTimeout, and two of those nested inside one another compose as the
	// *minimum* of the two deadlines -- an outer 30s always wins over an inner 180s
	// regardless of which route the inner one names. Book1/generate.zip's own doc comment
	// once (correctly) diagnosed this exact failure and (incorrectly) "fixed" it by nesting
	// a longer Timeout inside the 30s one, which never actually took effect; it only stayed
	// invisible because no request got close enough to 30s to expose it, until Gemini
	// drafting (BookGenerate's per-block/per-recipe DraftDoctorApproachNote/
	// DraftModificationNote/DraftFoodGroupPriorities calls, serial, real network round
	// trips) started pushing real generation time past it. Two groups off the bare router,
	// each stacking only Recoverer/Logger/CORS plus its own single Timeout, actually gives
	// the print routes the full printTimeout.
	r.Group(func(r chi.Router) {
		// 30s suits every JSON endpoint here.
		r.Use(middleware.Timeout(30 * time.Second))
		r.Use(security.BrowserWrite)
		r.Use(security.RequireStaff)

		r.Post("/api/search", h.Search)
		r.Get("/api/recipes/{recipeID}", h.RecipeDetail)
		r.Get("/api/ingredients", h.Ingredients)
		r.With(portal.AdminOnly).Get("/api/audit/nutrition", h.NutritionAudit)
		r.With(portal.AdminOnly).Get("/api/gaps", h.Gaps)
		r.With(portal.AdminOnly).Get("/api/runs", h.Runs)
		r.Get("/api/reference/regions", h.ReferenceRegions)
		r.Get("/api/reference/cuisines", h.ReferenceCuisines)
		r.Get("/api/reference/nutrition-targets", h.ReferenceNutritionTargets)
		r.Get("/api/reference/allergens", h.ReferenceAllergens)
		r.Get("/api/reference/clinical-markers", h.ReferenceClinicalMarkers)
		r.Get("/api/reference/enums", h.ReferenceEnums)
		r.Get("/api/reference/book1-blocks", h.ReferenceBook1Blocks)
		r.Get("/api/reference/special-care-conditions", h.ReferenceSpecialCareConditions)
		r.With(security.RequireProfile).Put("/api/profiles/{childID}", h.PutProfile)
		r.With(security.RequireProfile).Get("/api/profiles/{childID}", h.GetProfile)
		r.With(security.RequireProfile).Get("/api/profiles/{childID}/engine-input", h.GetProfileEngineInput)
		r.Get("/api/profile-matches", h.MatchProfiles)
		// The set is the primary surface: one run, both books, one profile read. The
		// per-book routes below remain for fetching one book directly.
		// Generation from inline inputs: no child id, nothing persisted. This is what the
		// console calls. The {childID} routes below serve a profile already in the database.
		r.Post("/api/books/generate", h.BookGenerate)
		r.With(security.RequireProfile).Get("/api/books/{childID}/preview", h.BookSetPreview)
		r.With(security.RequireProfile).Get("/api/books/{childID}/{book}/preview", h.BookPreview)
	})

	// Printing gets its own timeout. Launching a browser and laying out a 22-page book is
	// slow work in a way a database query is not, and with Gemini drafting now wired in
	// (see the comment above), assembly itself can take real, serial network time on top of
	// that. Generous because it covers the slowest real case -- two books, several drafted
	// notes, printed in one request on a small instance -- and because the alternative to
	// waiting is an operator retrying a request that was going to succeed. Scoped to these
	// four routes rather than raised globally, so a hung query on any other endpoint still
	// fails in 30s instead of holding a connection for three minutes.
	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(printTimeout))
		r.Use(security.BrowserWrite)
		r.Use(security.RequireStaff)
		r.Post("/api/books/generate.zip", h.BookGenerateZip)
		r.Post("/api/books/generate/{book}.pdf", h.BookGenerateOne)
		r.With(security.RequireProfile).Get("/api/books/{childID}/books.zip", h.BookSetDownload)
		r.With(security.RequireProfile).Get("/api/books/{childID}/{book}.pdf", h.BookDownload)
	})

	return r
}
