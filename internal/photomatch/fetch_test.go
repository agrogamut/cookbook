package photomatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchIndianFoodsDownloadsOnlyMappedLabels(t *testing.T) {
	// A tiny fake of datasets-server's /rows response shape, with one mapped label
	// (biryani -> bowl-grain) and one explicitly-excluded label (dal, no candidate).
	// downloadTo now sniffs the actual bytes (http.DetectContentType) rather than trusting
	// the Content-Type header -- found necessary against real production traffic, where
	// HuggingFace's datasets-server CDN serves real JPEGs under
	// Content-Type: binary/octet-stream. The Content-Type header below is set for realism
	// but is no longer what determines the result; the leading JPEG magic bytes are.
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("\xFF\xD8\xFFfake-jpeg-bytes"))
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
	mux.HandleFunc("/0120.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("\xFF\xD8\xFFfake-jpeg"))
	})
	mux.HandleFunc("/3026.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("\xFF\xD8\xFFfake-jpeg"))
	})
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

// TestFetchFoodBDRejectsPathTraversalFilename proves a malicious or corrupted
// FoodBD_Meta_data.csv row can't make fetchFoodBDFrom write outside its subdir: a
// "filename" of "../../../etc/passwd-shaped" value must be skipped, not joined into a
// path and downloaded.
func TestFetchFoodBDRejectsPathTraversalFilename(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/meta.csv", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("filename,instances,Environmental conditions,preprocessing ,types\n" +
			"FoodBD-0120.jpg,\"khichuri\",indoor,resized,train\n" +
			"../../../etc/passwd,\"khichuri\",indoor,resized,train\n"))
	})
	mux.HandleFunc("/0120.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("\xFF\xD8\xFFfake-jpeg"))
	})
	imgSrv := httptest.NewServer(mux)
	defer imgSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"files": []map[string]any{
				{"filename": "FoodBD_Meta_data.csv", "content_details": map[string]any{"download_url": imgSrv.URL + "/meta.csv"}},
				{"filename": "FoodBD-0120.jpg", "content_details": map[string]any{"download_url": imgSrv.URL + "/0120.jpg"}},
				// A malicious listing entry too, so a rejection at the filename check is
				// proven rather than an incidental miss on the byName lookup.
				{"filename": "../../../etc/passwd", "content_details": map[string]any{"download_url": imgSrv.URL + "/0120.jpg"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer apiSrv.Close()

	labels := LabelMap{{Dataset: "FOODBD", Label: "khichuri"}: "pot-khichdi"}
	dir := t.TempDir()

	rows, err := fetchFoodBDFrom(context.Background(), apiSrv.Client(), apiSrv.URL, dir, labels, 10)
	if err != nil {
		t.Fatalf("fetchFoodBDFrom: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d manifest rows, want 1 (the path-traversal row must be rejected, not downloaded)", len(rows))
	}
	if rows[0].LocalFile != filepath.Join("FOODBD", "FoodBD-0120.jpg") {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
	// The only files fetchFoodBDFrom is allowed to have written all live under
	// outDir/FOODBD -- confirm nothing escaped it.
	entries, err := os.ReadDir(filepath.Join(dir, "FOODBD"))
	if err != nil {
		t.Fatalf("read FOODBD subdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "FoodBD-0120.jpg" {
		t.Fatalf("unexpected FOODBD subdir contents: %v", entries)
	}
}

// TestDownloadToRejectsOversizedResponse proves the download size cap actually rejects an
// oversized body rather than silently truncating a corrupt file to disk.
func TestDownloadToRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(make([]byte, maxDownloadBytes+1))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "big.jpg")
	if _, err := downloadTo(context.Background(), srv.Client(), srv.URL, dest); err == nil {
		t.Fatalf("downloadTo: want error for a response over maxDownloadBytes, got nil")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("downloadTo: an oversized file was left on disk instead of being rejected")
	}
}

// TestDownloadToRejectsDisallowedContentType proves a response whose Content-Type isn't
// one of dish_format_photo's allowed image types is rejected rather than written to disk
// and later hardcoded as "image/jpeg" regardless of what the server actually sent.
func TestDownloadToRejectsDisallowedContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>not an image</html>"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "page.jpg")
	if _, err := downloadTo(context.Background(), srv.Client(), srv.URL, dest); err == nil {
		t.Fatalf("downloadTo: want error for a disallowed content-type, got nil")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("downloadTo: a file was written despite a disallowed content-type")
	}
}

// TestDownloadToRejectsNonHTTPScheme proves a non-http(s) URL (a malicious or corrupted
// API response handing back file://, gopher:// or similar) never reaches client.Do at all.
func TestDownloadToRejectsNonHTTPScheme(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "x.jpg")
	if _, err := downloadTo(context.Background(), http.DefaultClient, "file:///etc/passwd", dest); err == nil {
		t.Fatalf("downloadTo: want error for a non-http(s) scheme, got nil")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("downloadTo: a file was written despite a non-http(s) scheme")
	}
}

// TestSafeClientRejectsSchemeChangeOnRedirect proves the redirect policy re-validates the
// scheme on every hop, not just on the initial request -- a server redirecting to a
// file:// URL must not be followed.
func TestSafeClientRejectsSchemeChangeOnRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	}))
	defer srv.Close()

	client := safeClient(srv.Client())
	req, err := newRequest(context.Background(), http.MethodGet, srv.URL)
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	if _, err := client.Do(req); err == nil {
		t.Fatalf("client.Do: want error following a redirect to a non-http(s) scheme, got nil")
	}
}

// TestSafeClientCapsRedirectChain proves the redirect policy stops following redirects
// after maxRedirects hops rather than looping (or chaining) indefinitely.
func TestSafeClientCapsRedirectChain(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL, http.StatusFound) // redirects to itself, forever
	}))
	defer srv.Close()

	client := safeClient(srv.Client())
	req, err := newRequest(context.Background(), http.MethodGet, srv.URL)
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	if _, err := client.Do(req); err == nil {
		t.Fatalf("client.Do: want error after exceeding the redirect cap, got nil")
	}
}
