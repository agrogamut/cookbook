// internal/photomatch/fetch.go
package photomatch

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// indianFoodsDataset is verified live against the real endpoint while writing the
	// design spec this package implements -- see the spec's Decision 1.
	indianFoodsDataset = "bharat-raghunathan/indian-foods-dataset"
	datasetsServerBase = "https://datasets-server.huggingface.co"
	rowsPageSize       = 100
	foodBDDatasetID    = "xh3ghf3jbg"
	mendeleyAPIBase    = "https://data.mendeley.com/public-api/datasets"

	// maxDownloadBytes caps a single fetched image. Bigger than internal/book/photo.go's
	// 8 MB cover-photo cap (maxPhotoBytes) because these are dataset photos rather than a
	// phone-camera cover shot and can run a bit larger, but still bounded -- an unbounded
	// io.Copy from a network response, in a batch job that runs unattended, is exactly the
	// resource-exhaustion risk a size cap exists to close.
	maxDownloadBytes = 20 << 20 // 20 MB

	// maxRedirects bounds how many redirect hops any request this package makes will
	// follow, applied via safeClient's CheckRedirect. A small, explicit cap rather than
	// leaving net/http's own default in place -- these are one-shot batch fetches against
	// two known APIs, not general browsing, and a response that needs more than a handful
	// of hops to reach an image is behaving unexpectedly.
	maxRedirects = 5
)

// allowedDownloadMediaTypes mirrors dish_format_photo's own media_type CHECK constraint
// (migration 0026_dish_format_photo.up.sql) -- there is no point accepting a content type
// here that the database would reject on insert, and checking it before the write means a
// mislabelled response never lands on disk looking like a jpeg it isn't.
var allowedDownloadMediaTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// allowedURLSchemes is the scheme allowlist enforced on every outbound request this
// package makes -- the initial request and every redirect hop -- so that a malicious or
// merely corrupted API response cannot hand back a file://, gopher:// or similar
// non-network scheme for an http.Client to dereference.
//
// Deliberately scoped to scheme only: no loopback/private/link-local IP blocking. Every
// URL this package requests comes from one of two curated, versioned third-party API
// responses (HuggingFace's datasets-server, Mendeley's public dataset API), fetched by an
// operator-run offline batch command -- not a live handler processing arbitrary
// end-user input. Full SSRF hardening (IP-range blocking) is a different scope than this
// fix covers, and it would also break every test in fetch_test.go, which necessarily
// points every URL at an httptest.Server on 127.0.0.1.
var allowedURLSchemes = map[string]bool{"http": true, "https": true}

// validateURLScheme rejects any URL whose scheme isn't http or https. Applied before every
// request this package builds and, via safeClient, before every redirect it follows.
func validateURLScheme(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("photomatch: parse URL %q: %w", rawURL, err)
	}
	if !allowedURLSchemes[strings.ToLower(u.Scheme)] {
		return fmt.Errorf("photomatch: URL %q has disallowed scheme %q", rawURL, u.Scheme)
	}
	return nil
}

// newRequest builds a GET request after checking its scheme, so no caller in this package
// can accidentally construct a request against a validated-nowhere URL.
func newRequest(ctx context.Context, method, rawURL string) (*http.Request, error) {
	if err := validateURLScheme(rawURL); err != nil {
		return nil, err
	}
	return http.NewRequestWithContext(ctx, method, rawURL, nil)
}

// safeClient returns a shallow copy of client with a CheckRedirect policy that
// re-validates the scheme on every hop and caps the chain at maxRedirects.
//
// A copy, not a mutation: cmd/photomatch/main.go constructs one *http.Client and passes it
// to every fetch function (for testability -- fetch_test.go passes httptest.Server's own
// client), so setting CheckRedirect directly on the caller's client would silently change
// behavior anywhere else that client is used. The copy is shallow on purpose -- it shares
// the same Transport, which is the expensive, connection-pooling part.
func safeClient(client *http.Client) *http.Client {
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("photomatch: stopped after %d redirects", maxRedirects)
		}
		return validateURLScheme(req.URL.String())
	}
	return &c
}

// isSafeRelativePath guards against path traversal on a filename this package did not
// generate itself. Two independent inputs reach a filesystem path this way: a "filename"
// column read out of FoodBD's own metadata CSV in fetchFoodBDFrom (an external file this
// package does not control), and a LocalFile field read back out of a previously-written
// manifest in match.go's Match (meant to be a trusted, locally-produced artifact, but a
// tampered or corrupted one is still a file on disk by the time it gets here). Neither is
// safe to filepath.Join and use blindly: a "../../../etc/passwd"-shaped value would let
// either a bad CSV row write, or a bad manifest row read, outside the intended directory.
//
// Rejects an absolute path and any path containing a ".." segment -- the standard, minimal
// check for this class of bug.
func isSafeRelativePath(p string) bool {
	if p == "" || filepath.IsAbs(p) {
		return false
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part == ".." {
			return false
		}
	}
	return true
}

// FetchIndianFoods downloads up to perLabelCap candidate images per mapped
// BHARAT-INDIAN-FOODS label into outDir/BHARAT-INDIAN-FOODS/, via HuggingFace's public
// datasets-server rows API. Unmapped labels (LabelMap.MarkID returns ok=false) are never
// downloaded -- fetching an image this pipeline could never use would just be storage
// spent on nothing.
func FetchIndianFoods(ctx context.Context, client *http.Client, outDir string, labels LabelMap, perLabelCap int) ([]ManifestRow, error) {
	return fetchIndianFoodsFrom(ctx, client, datasetsServerBase, outDir, labels, perLabelCap)
}

// fetchIndianFoodsFrom takes the API base URL as a parameter so tests can point it at an
// httptest.Server instead of the real internet.
func fetchIndianFoodsFrom(ctx context.Context, client *http.Client, base, outDir string, labels LabelMap, perLabelCap int) ([]ManifestRow, error) {
	type rowsResponse struct {
		Features []struct {
			Name string `json:"name"`
			Type struct {
				Names []string `json:"names"`
			} `json:"type"`
		} `json:"features"`
		Rows []struct {
			RowIdx int `json:"row_idx"`
			Row    struct {
				Image struct {
					Src string `json:"src"`
				} `json:"image"`
				Label int `json:"label"`
			} `json:"row"`
		} `json:"rows"`
	}

	subdir := filepath.Join(outDir, "BHARAT-INDIAN-FOODS")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		return nil, fmt.Errorf("photomatch: mkdir %s: %w", subdir, err)
	}

	client = safeClient(client)

	var classNames []string
	var rows []ManifestRow
	counts := map[string]int{}

	for offset := 0; ; offset += rowsPageSize {
		q := url.Values{
			"dataset": {indianFoodsDataset},
			"config":  {"default"},
			"split":   {"train"},
			"offset":  {strconv.Itoa(offset)},
			"length":  {strconv.Itoa(rowsPageSize)},
		}
		req, err := newRequest(ctx, http.MethodGet, base+"/rows?"+q.Encode())
		if err != nil {
			return nil, fmt.Errorf("photomatch: build rows request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("photomatch: fetch rows page at offset %d: %w", offset, err)
		}
		var page rowsResponse
		decErr := json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if decErr != nil {
			return nil, fmt.Errorf("photomatch: decode rows page at offset %d: %w", offset, decErr)
		}
		if len(page.Rows) == 0 {
			break // ran off the end of the dataset
		}
		if classNames == nil {
			for _, f := range page.Features {
				if f.Name == "label" {
					classNames = f.Type.Names
				}
			}
		}

		for _, r := range page.Rows {
			if r.Row.Label < 0 || r.Row.Label >= len(classNames) {
				continue
			}
			label := classNames[r.Row.Label]
			markID, ok := labels.MarkID("BHARAT-INDIAN-FOODS", label)
			if !ok || counts[label] >= perLabelCap {
				continue
			}

			localName := fmt.Sprintf("%d.jpg", r.RowIdx)
			mediaType, err := downloadTo(ctx, client, r.Row.Image.Src, filepath.Join(subdir, localName))
			if err != nil {
				return nil, fmt.Errorf("photomatch: download row %d: %w", r.RowIdx, err)
			}
			rows = append(rows, ManifestRow{
				SourceDataset: "BHARAT-INDIAN-FOODS",
				SourceRowID:   strconv.Itoa(r.RowIdx),
				SourceLabel:   label,
				MarkID:        markID,
				LocalFile:     filepath.Join("BHARAT-INDIAN-FOODS", localName),
				MediaType:     mediaType,
			})
			counts[label]++
		}

		if len(page.Rows) < rowsPageSize {
			break // short page means this was the last one
		}
	}
	return rows, nil
}

// downloadTo streams an HTTP response body to a local file, capped at maxDownloadBytes and
// gated on an allowed image Content-Type. Used for both datasets' image bytes. Returns the
// response's own validated media type -- callers no longer hardcode "image/jpeg" for
// every download regardless of what the server actually sent.
func downloadTo(ctx context.Context, client *http.Client, srcURL, destPath string) (string, error) {
	req, err := newRequest(ctx, http.MethodGet, srcURL)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get %s: status %d", srcURL, resp.StatusCode)
	}

	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]))
	if !allowedDownloadMediaTypes[mediaType] {
		return "", fmt.Errorf("get %s: content-type %q is not an accepted image type", srcURL, mediaType)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", destPath, err)
	}

	// io.LimitReader is set one byte over the cap so a source sitting exactly at the
	// boundary isn't mistaken for one over it, and so a response strictly larger than the
	// cap is detected below rather than silently truncated to disk.
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxDownloadBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(destPath)
		return "", fmt.Errorf("write %s: %w", destPath, copyErr)
	}
	if closeErr != nil {
		os.Remove(destPath)
		return "", fmt.Errorf("close %s: %w", destPath, closeErr)
	}
	if n > maxDownloadBytes {
		os.Remove(destPath)
		return "", fmt.Errorf("get %s: image exceeds the %d byte (%d MB) limit", srcURL, maxDownloadBytes, maxDownloadBytes>>20)
	}
	return mediaType, nil
}

// FetchFoodBD downloads up to perLabelCap candidate images per mapped FOODBD label into
// outDir/FOODBD/, via Mendeley's public dataset API. Only images whose
// FoodBD_Meta_data.csv "instances" column names exactly one dish are candidates -- most
// FoodBD photos are whole meal plates with several items, and a multi-item photo
// doesn't represent any single archetype cleanly (see the spec's Decision 1).
func FetchFoodBD(ctx context.Context, client *http.Client, outDir string, labels LabelMap, perLabelCap int) ([]ManifestRow, error) {
	return fetchFoodBDFrom(ctx, client, mendeleyAPIBase, outDir, labels, perLabelCap)
}

func fetchFoodBDFrom(ctx context.Context, client *http.Client, apiBase, outDir string, labels LabelMap, perLabelCap int) ([]ManifestRow, error) {
	type contentDetails struct {
		DownloadURL string `json:"download_url"`
	}
	type fileEntry struct {
		Filename       string         `json:"filename"`
		ContentDetails contentDetails `json:"content_details"`
	}
	type datasetResponse struct {
		Files []fileEntry `json:"files"`
	}

	client = safeClient(client)

	req, err := newRequest(ctx, http.MethodGet, apiBase+"/"+foodBDDatasetID)
	if err != nil {
		return nil, fmt.Errorf("photomatch: build FoodBD dataset request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("photomatch: fetch FoodBD dataset listing: %w", err)
	}
	var listing datasetResponse
	decErr := json.NewDecoder(resp.Body).Decode(&listing)
	resp.Body.Close()
	if decErr != nil {
		return nil, fmt.Errorf("photomatch: decode FoodBD dataset listing: %w", decErr)
	}

	byName := make(map[string]fileEntry, len(listing.Files))
	for _, f := range listing.Files {
		byName[f.Filename] = f
	}
	metaEntry, ok := byName["FoodBD_Meta_data.csv"]
	if !ok {
		return nil, fmt.Errorf("photomatch: FoodBD dataset listing has no FoodBD_Meta_data.csv")
	}

	metaReq, err := newRequest(ctx, http.MethodGet, metaEntry.ContentDetails.DownloadURL)
	if err != nil {
		return nil, fmt.Errorf("photomatch: build metadata request: %w", err)
	}
	metaResp, err := client.Do(metaReq)
	if err != nil {
		return nil, fmt.Errorf("photomatch: fetch FoodBD metadata: %w", err)
	}
	defer metaResp.Body.Close()

	r := csv.NewReader(metaResp.Body)
	head, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("photomatch: read FoodBD metadata header: %w", err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[h] = i
	}
	filenameCol, ok := col["filename"]
	if !ok {
		return nil, fmt.Errorf("photomatch: FoodBD metadata missing filename column")
	}
	instancesCol, ok := col["instances"]
	if !ok {
		return nil, fmt.Errorf("photomatch: FoodBD metadata missing instances column")
	}

	subdir := filepath.Join(outDir, "FOODBD")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		return nil, fmt.Errorf("photomatch: mkdir %s: %w", subdir, err)
	}

	var rows []ManifestRow
	counts := map[string]int{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("photomatch: read FoodBD metadata row: %w", err)
		}

		instances := strings.Split(rec[instancesCol], ",")
		if len(instances) != 1 {
			continue // a multi-item plate; not a candidate for any single archetype
		}
		label := strings.TrimSpace(instances[0])
		markID, ok := labels.MarkID("FOODBD", label)
		if !ok || counts[label] >= perLabelCap {
			continue
		}

		filename := rec[filenameCol]
		if !isSafeRelativePath(filename) {
			continue // metadata names an unsafe path; skip this row rather than risk writing outside subdir
		}
		imgEntry, ok := byName[filename]
		if !ok {
			continue // metadata names a file the dataset listing doesn't have; skip rather than guess a URL
		}

		mediaType, err := downloadTo(ctx, client, imgEntry.ContentDetails.DownloadURL, filepath.Join(subdir, filename))
		if err != nil {
			return nil, fmt.Errorf("photomatch: download %s: %w", filename, err)
		}
		rows = append(rows, ManifestRow{
			SourceDataset: "FOODBD",
			SourceRowID:   filename,
			SourceLabel:   label,
			MarkID:        markID,
			LocalFile:     filepath.Join("FOODBD", filename),
			MediaType:     mediaType,
		})
		counts[label]++
	}
	return rows, nil
}
