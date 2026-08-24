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
)

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
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/rows?"+q.Encode(), nil)
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
			if err := downloadTo(ctx, client, r.Row.Image.Src, filepath.Join(subdir, localName)); err != nil {
				return nil, fmt.Errorf("photomatch: download row %d: %w", r.RowIdx, err)
			}
			rows = append(rows, ManifestRow{
				SourceDataset: "BHARAT-INDIAN-FOODS",
				SourceRowID:   strconv.Itoa(r.RowIdx),
				SourceLabel:   label,
				MarkID:        markID,
				LocalFile:     filepath.Join("BHARAT-INDIAN-FOODS", localName),
				MediaType:     "image/jpeg",
			})
			counts[label]++
		}

		if len(page.Rows) < rowsPageSize {
			break // short page means this was the last one
		}
	}
	return rows, nil
}

// downloadTo streams an HTTP response body to a local file. Used for both datasets'
// image bytes -- neither needs anything more than a plain GET and a file write.
func downloadTo(ctx context.Context, client *http.Client, srcURL, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get %s: status %d", srcURL, resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	return nil
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/"+foodBDDatasetID, nil)
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

	metaReq, err := http.NewRequestWithContext(ctx, http.MethodGet, metaEntry.ContentDetails.DownloadURL, nil)
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
		imgEntry, ok := byName[filename]
		if !ok {
			continue // metadata names a file the dataset listing doesn't have; skip rather than guess a URL
		}

		if err := downloadTo(ctx, client, imgEntry.ContentDetails.DownloadURL, filepath.Join(subdir, filename)); err != nil {
			return nil, fmt.Errorf("photomatch: download %s: %w", filename, err)
		}
		rows = append(rows, ManifestRow{
			SourceDataset: "FOODBD",
			SourceRowID:   filename,
			SourceLabel:   label,
			MarkID:        markID,
			LocalFile:     filepath.Join("FOODBD", filename),
			MediaType:     "image/jpeg",
		})
		counts[label]++
	}
	return rows, nil
}
