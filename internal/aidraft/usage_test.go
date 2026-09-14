package aidraft

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"google.golang.org/genai"
)

func usageClient(t *testing.T, handler http.HandlerFunc) *geminiClient {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	c, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey: "test-key", Backend: genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{BaseURL: s.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &geminiClient{client: c}
}

func usageResponse(w http.ResponseWriter, text string, metadata bool) {
	resp := map[string]any{
		"candidates": []any{map[string]any{"content": map[string]any{
			"role": "model", "parts": []any{map[string]string{"text": text}},
		}}},
	}
	if metadata {
		resp["usageMetadata"] = map[string]int{
			"promptTokenCount": 100, "candidatesTokenCount": 20,
			"thoughtsTokenCount": 30, "cachedContentTokenCount": 40,
			"totalTokenCount": 150,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func TestUsageAllOperationsAndNoPrivateTextInLogs(t *testing.T) {
	d := usageClient(t, func(w http.ResponseWriter, r *http.Request) {
		usageResponse(w, `{"note":"PRIVATE_RESPONSE","texts":["PRIVATE_RESPONSE"],"rows":[],"ingredients":[]}`, true)
	})
	var logs bytes.Buffer
	ctx, usage := WithUsage(context.Background(), slog.New(slog.NewJSONHandler(&logs, nil)))
	ctx = WithUsageBook(ctx, "book1")
	for _, call := range []func() error{
		func() error {
			_, e := d.DraftModificationNote(ctx, ModificationRequest{RecipeName: "PRIVATE_PROMPT"})
			return e
		},
		func() error { _, e := d.DraftDoctorApproachNote(ctx, DoctorApproachRequest{}); return e },
		func() error { _, e := d.DraftFoodGroupPriorities(ctx, FoodGroupPriorityRequest{}); return e },
		func() error { _, e := d.DraftInventedRecipe(ctx, InventedRecipeRequest{}); return e },
		func() error {
			_, e := d.TranslateTexts(ctx, TranslateRequest{Texts: []string{"PRIVATE_PROMPT"}})
			return e
		},
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	s := usage.Snapshot()
	if s.Total.Calls != 5 || s.Total.PromptTokens != 500 || s.Total.OutputTokens != 100 ||
		s.Total.ThoughtTokens != 150 || s.Total.CachedTokens != 200 || s.Total.TotalTokens != 750 ||
		s.Total.MissingUsage != 0 || len(s.Groups) != 5 || len(s.Books) != 1 || s.Books[0].TotalTokens != 750 {
		t.Fatalf("wrong totals: %+v", s)
	}
	for _, group := range s.Groups {
		if group.Book != "book1" || group.Calls != 1 {
			t.Fatalf("wrong group: %+v", group)
		}
		wantModel := modelName
		if group.Operation == "translation" {
			wantModel = translateModelName
		}
		if group.Model != wantModel {
			t.Fatalf("wrong model: %+v", group)
		}
	}
	if strings.Count(logs.String(), `"msg":"generation_call"`) != 5 ||
		strings.Contains(logs.String(), "PRIVATE_") || strings.Contains(logs.String(), "test-key") {
		t.Fatalf("missing call logs or leaked private data: %s", logs.String())
	}
}

func TestUsageTranslationRetriesAndMissingMetadata(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		firstText                          string
		firstMetadata                      bool
		firstStatus                        int
		wantCalls, wantMissing, wantErrors int
		wantTokens                         int64
	}{
		{"decode retry counts both responses", "invalid-json", true, 200, 2, 0, 0, 300},
		{"request error leaves unknown usage", "", false, 400, 2, 1, 1, 150},
		{"missing metadata is not free usage", `{"texts":["result"]}`, false, 200, 1, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			d := usageClient(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					if tc.firstStatus != 200 {
						w.WriteHeader(tc.firstStatus)
						_, _ = io.WriteString(w, `{"error":{"message":"private error","code":400}}`)
						return
					}
					usageResponse(w, tc.firstText, tc.firstMetadata)
					return
				}
				usageResponse(w, `{"texts":["result"]}`, true)
			})
			ctx, u := WithUsage(context.Background(), slog.New(slog.NewJSONHandler(io.Discard, nil)))
			if _, err := d.TranslateTexts(ctx, TranslateRequest{Texts: []string{"source"}}); err != nil {
				t.Fatal(err)
			}
			s := u.Snapshot().Total
			if s.Calls != tc.wantCalls || s.MissingUsage != tc.wantMissing || s.RequestErrors != tc.wantErrors || s.TotalTokens != tc.wantTokens {
				t.Fatalf("wrong usage: %+v", s)
			}
		})
	}
}

func TestUsageConcurrentRunsStayIsolated(t *testing.T) {
	d := usageClient(t, func(w http.ResponseWriter, r *http.Request) {
		usageResponse(w, `{"texts":["result"]}`, true)
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ctx1, u1 := WithUsage(context.Background(), logger)
	ctx2, u2 := WithUsage(context.Background(), logger)
	var wg sync.WaitGroup
	for _, ctx := range []context.Context{WithUsageBook(ctx1, "book1"), WithUsageBook(ctx2, "book2")} {
		for range 24 {
			wg.Go(func() {
				if _, err := d.TranslateTexts(ctx, TranslateRequest{Texts: []string{"source"}}); err != nil {
					t.Error(err)
				}
				_ = u1.Snapshot()
			})
		}
	}
	wg.Wait()
	for i, u := range []*UsageRecorder{u1, u2} {
		s := u.Snapshot()
		if s.Total.Calls != 24 || s.Total.TotalTokens != 3600 || len(s.Groups) != 1 || s.Groups[0].Book != []string{"book1", "book2"}[i] {
			t.Fatalf("concurrent usage incorrect: %+v", s)
		}
		s.Groups[0].Calls = -1
		if u.Snapshot().Groups[0].Calls != 24 {
			t.Fatal("snapshot exposed mutable recorder state")
		}
	}
	if u1.Snapshot().RunID == u2.Snapshot().RunID {
		t.Fatal("two runs share a log identifier")
	}
}
