package handlers

import (
	"net/http"
	"time"
)

// Runs returns import_run joined to its per-table stats, so the /runs screen can show
// content hashes and rows-skipped without a second round trip.
func (h *Handlers) Runs(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT ir.run_id, ir.started_at, ir.finished_at, ir.source_dir, ir.ok,
		       its.table_name, its.rows_read, its.rows_written, its.rows_skipped, its.content_hash
		FROM import_run ir
		JOIN import_table_stat its ON its.run_id = ir.run_id
		ORDER BY ir.run_id DESC, its.table_name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "run history failed: "+err.Error())
		return
	}
	defer rows.Close()

	type tableStat struct {
		TableName    string `json:"table_name"`
		RowsRead     int    `json:"rows_read"`
		RowsWritten  int    `json:"rows_written"`
		RowsSkipped  int    `json:"rows_skipped"`
		ContentHash  string `json:"content_hash"`
	}
	type run struct {
		RunID      int64       `json:"run_id"`
		StartedAt  time.Time   `json:"started_at"`
		FinishedAt *time.Time  `json:"finished_at"`
		SourceDir  string      `json:"source_dir"`
		OK         bool        `json:"ok"`
		Tables     []tableStat `json:"tables"`
	}

	byID := map[int64]*run{}
	var order []int64
	for rows.Next() {
		var runID int64
		var startedAt time.Time
		var sourceDir string
		var finishedAt *time.Time
		var ok bool
		var ts tableStat
		if err := rows.Scan(&runID, &startedAt, &finishedAt, &sourceDir, &ok,
			&ts.TableName, &ts.RowsRead, &ts.RowsWritten, &ts.RowsSkipped, &ts.ContentHash); err != nil {
			writeError(w, http.StatusInternalServerError, "run history scan failed: "+err.Error())
			return
		}
		if _, seen := byID[runID]; !seen {
			byID[runID] = &run{RunID: runID, StartedAt: startedAt, FinishedAt: finishedAt, SourceDir: sourceDir, OK: ok}
			order = append(order, runID)
		}
		byID[runID].Tables = append(byID[runID].Tables, ts)
	}

	out := make([]*run, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	writeJSON(w, http.StatusOK, out)
}
