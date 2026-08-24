package photomatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchIndianFoodsDownloadsOnlyMappedLabels(t *testing.T) {
	// A tiny fake of datasets-server's /rows response shape, with one mapped label
	// (biryani -> bowl-grain) and one explicitly-excluded label (dal, no candidate).
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("fake-jpeg-bytes"))
	}))
	defer imgSrv.Close()

	rowsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"features": []map[string]any{
				{"feature_idx": 0, "name": "image", "type": map[string]any{"_type": "Image"}},
				{"feature_idx": 1, "name": "label", "type": map[string]any{
					"names": []string{"biryani", "dal"}, "_type": "ClassLabel",
				}},
			},
			"rows": []map[string]any{
				{"row_idx": 0, "row": map[string]any{
					"image": map[string]any{"src": imgSrv.URL + "/0.jpg"}, "label": 0,
				}},
				{"row_idx": 1, "row": map[string]any{
					"image": map[string]any{"src": imgSrv.URL + "/1.jpg"}, "label": 1,
				}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer rowsSrv.Close()

	labels := LabelMap{{Dataset: "BHARAT-INDIAN-FOODS", Label: "biryani"}: "bowl-grain"}
	dir := t.TempDir()

	rows, err := fetchIndianFoodsFrom(context.Background(), rowsSrv.Client(), rowsSrv.URL, dir, labels, 10)
	if err != nil {
		t.Fatalf("fetchIndianFoodsFrom: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d manifest rows, want 1 (dal has no mapped archetype and must be skipped)", len(rows))
	}
	if rows[0].MarkID != "bowl-grain" || rows[0].SourceLabel != "biryani" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
}

func TestFetchFoodBDOnlyTakesSingleItemPlates(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/meta.csv", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("filename,instances,Environmental conditions,preprocessing ,types\n" +
			"FoodBD-0120.jpg,\"khichuri\",indoor,resized,train\n" +
			"FoodBD-3026.jpg,\"fish,rice,shak,vaji\",indoor,resized,train\n"))
	})
	mux.HandleFunc("/0120.jpg", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("fake-jpeg")) })
	mux.HandleFunc("/3026.jpg", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("fake-jpeg")) })
	imgSrv2 := httptest.NewServer(mux)
	defer imgSrv2.Close()

	apiSrv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"files": []map[string]any{
				{"filename": "FoodBD_Meta_data.csv", "content_details": map[string]any{"download_url": imgSrv2.URL + "/meta.csv"}},
				{"filename": "FoodBD-0120.jpg", "content_details": map[string]any{"download_url": imgSrv2.URL + "/0120.jpg"}},
				{"filename": "FoodBD-3026.jpg", "content_details": map[string]any{"download_url": imgSrv2.URL + "/3026.jpg"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer apiSrv2.Close()

	labels := LabelMap{{Dataset: "FOODBD", Label: "khichuri"}: "pot-khichdi"}
	dir := t.TempDir()

	rows, err := fetchFoodBDFrom(context.Background(), apiSrv2.Client(), apiSrv2.URL, dir, labels, 10)
	if err != nil {
		t.Fatalf("fetchFoodBDFrom: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d manifest rows, want 1 (the multi-item plate must be rejected)", len(rows))
	}
	if rows[0].SourceLabel != "khichuri" || rows[0].MarkID != "pot-khichdi" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
}
