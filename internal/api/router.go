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
	"github.com/madamgy/recipie/internal/api/handlers"
)

// NewRouter builds the full route table. Middleware order: recover, logger, CORS -- auth
// is deliberately absent; see docs/superpowers/plans/2026-08-16-backend-engine-api.md,
// "Architecture", for why.
func NewRouter(pool *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"}, // internal tool, no browser cookie auth to protect; tighten if this ever leaves a private network
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		MaxAge:           300,
	}))
	r.Use(middleware.Timeout(30 * time.Second))

	h := handlers.New(pool)

	r.Get("/healthz", h.Healthz)
	r.Post("/api/search", h.Search)
	r.Get("/api/recipes/{recipeID}", h.RecipeDetail)
	r.Get("/api/ingredients", h.Ingredients)
	r.Get("/api/audit/nutrition", h.NutritionAudit)
	r.Get("/api/gaps", h.Gaps)
	r.Get("/api/runs", h.Runs)
	r.Get("/api/reference/regions", h.ReferenceRegions)
	r.Get("/api/reference/cuisines", h.ReferenceCuisines)
	r.Get("/api/reference/nutrition-targets", h.ReferenceNutritionTargets)
	r.Get("/api/reference/allergens", h.ReferenceAllergens)
	r.Get("/api/reference/clinical-markers", h.ReferenceClinicalMarkers)
	r.Get("/api/reference/enums", h.ReferenceEnums)

	return r
}
