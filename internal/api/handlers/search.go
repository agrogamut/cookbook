package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/models"
)

// Search runs the full 14-step engine against the posted child profile. AgeMonths is
// required because every other step depends on it; every other field is optional.
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	var p models.ChildProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if p.AgeMonths <= 0 {
		writeError(w, http.StatusBadRequest, "age_months is required and must be positive")
		return
	}

	result, err := engine.Run(r.Context(), h.pool, p)
	if err != nil {
		// An unrecognized allergen, clinical flag key or cuisine code is an operator
		// input mistake, not a server failure -- report it as 400 so it reads as "fix
		// your request" rather than "the server is broken".
		if errors.Is(err, engine.ErrInvalidProfile) {
			writeError(w, http.StatusBadRequest, "invalid profile: "+err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "search failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
