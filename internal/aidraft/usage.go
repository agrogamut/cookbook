package aidraft

import (
	"context"
	"crypto/rand"
	"log/slog"
	"sort"
	"sync"
	"time"

	"google.golang.org/genai"
)

// UsageCounts contains reported usage only. MissingUsage counts calls whose token
// cost is unknown, including requests that failed without response metadata.
// CachedTokens is part of PromptTokens, and must not be added to TotalTokens.
type UsageCounts struct {
	Calls         int   `json:"calls"`
	RequestErrors int   `json:"request_errors"`
	MissingUsage  int   `json:"missing_usage"`
	PromptTokens  int64 `json:"prompt_tokens"`
	OutputTokens  int64 `json:"output_tokens"`
	ThoughtTokens int64 `json:"thought_tokens"`
	CachedTokens  int64 `json:"cached_tokens"`
	TotalTokens   int64 `json:"total_tokens"`
	CallMillis    int64 `json:"call_ms"`
}

type UsageGroup struct {
	Book      string `json:"book"`
	Operation string `json:"operation"`
	Model     string `json:"model"`
	UsageCounts
}

type BookUsage struct {
	Book string `json:"book"`
	UsageCounts
}

type UsageSnapshot struct {
	RunID  string       `json:"run_id"`
	Total  UsageCounts  `json:"total"`
	Books  []BookUsage  `json:"books"`
	Groups []UsageGroup `json:"groups"`
}

// UsageRecorder belongs to one generation request, never to the shared client.
// Concurrent book workers record counters without retaining prompts or responses.
type UsageRecorder struct {
	mu     sync.Mutex
	runID  string
	logger *slog.Logger
	total  UsageCounts
	groups map[usageKey]UsageCounts
}

type usageKey struct{ book, operation, model string }
type usageContextKey struct{}
type bookContextKey struct{}

func WithUsage(ctx context.Context, logger *slog.Logger) (context.Context, *UsageRecorder) {
	if logger == nil {
		logger = slog.Default()
	}
	u := &UsageRecorder{runID: rand.Text(), logger: logger, groups: make(map[usageKey]UsageCounts)}
	return context.WithValue(ctx, usageContextKey{}, u), u
}

// WithUsageBook labels downstream requests without changing their cancellation or
// deadline. Callers use book1/book2 rather than a child's identifying information.
func WithUsageBook(ctx context.Context, book string) context.Context {
	return context.WithValue(ctx, bookContextKey{}, book)
}

func (u *UsageRecorder) Snapshot() UsageSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := UsageSnapshot{RunID: u.runID, Total: u.total, Groups: make([]UsageGroup, 0, len(u.groups))}
	books := make(map[string]UsageCounts)
	for key, counts := range u.groups {
		s.Groups = append(s.Groups, UsageGroup{key.book, key.operation, key.model, counts})
		book := books[key.book]
		book.add(counts)
		books[key.book] = book
	}
	s.Books = make([]BookUsage, 0, len(books))
	for book, counts := range books {
		s.Books = append(s.Books, BookUsage{book, counts})
	}
	sort.Slice(s.Books, func(i, j int) bool { return s.Books[i].Book < s.Books[j].Book })
	sort.Slice(s.Groups, func(i, j int) bool {
		a, b := s.Groups[i], s.Groups[j]
		if a.Book != b.Book {
			return a.Book < b.Book
		}
		if a.Operation != b.Operation {
			return a.Operation < b.Operation
		}
		return a.Model < b.Model
	})
	return s
}

func (c UsageCounts) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("calls", c.Calls), slog.Int("request_errors", c.RequestErrors),
		slog.Int("missing_usage", c.MissingUsage), slog.Int64("prompt_tokens", c.PromptTokens),
		slog.Int64("output_tokens", c.OutputTokens), slog.Int64("thought_tokens", c.ThoughtTokens),
		slog.Int64("cached_tokens", c.CachedTokens), slog.Int64("total_tokens", c.TotalTokens),
		slog.Int64("call_ms", c.CallMillis),
	)
}

func (s UsageSnapshot) LogValue() slog.Value {
	books := make([]slog.Attr, 0, len(s.Books))
	for _, b := range s.Books {
		books = append(books, slog.Any(b.Book, b.UsageCounts))
	}
	groups := make([]slog.Attr, 0, len(s.Groups))
	for _, g := range s.Groups {
		groups = append(groups, slog.Any(g.Book+"/"+g.Operation+"/"+g.Model, g.UsageCounts))
	}
	return slog.GroupValue(slog.String("run_id", s.RunID), slog.Any("total", s.Total),
		slog.Attr{Key: "books", Value: slog.GroupValue(books...)},
		slog.Attr{Key: "groups", Value: slog.GroupValue(groups...)})
}

func (c *UsageCounts) add(v UsageCounts) {
	c.Calls += v.Calls
	c.RequestErrors += v.RequestErrors
	c.MissingUsage += v.MissingUsage
	c.PromptTokens += v.PromptTokens
	c.OutputTokens += v.OutputTokens
	c.ThoughtTokens += v.ThoughtTokens
	c.CachedTokens += v.CachedTokens
	c.TotalTokens += v.TotalTokens
	c.CallMillis += v.CallMillis
}

// generate measures each SDK invocation, including translation retries. Recording
// happens before decoding so an unusable response still contributes its real usage.
func (g *geminiClient) generate(ctx context.Context, operation, model, prompt string, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	started := time.Now()
	resp, err := g.client.Models.GenerateContent(ctx, model, genai.Text(prompt), cfg)
	u, _ := ctx.Value(usageContextKey{}).(*UsageRecorder)
	if u == nil {
		return resp, err
	}
	counts := UsageCounts{Calls: 1, CallMillis: time.Since(started).Milliseconds()}
	if err != nil {
		counts.RequestErrors = 1
	}
	if resp == nil || resp.UsageMetadata == nil {
		counts.MissingUsage = 1
	} else {
		m := resp.UsageMetadata
		counts.PromptTokens = int64(m.PromptTokenCount)
		counts.OutputTokens = int64(m.CandidatesTokenCount)
		counts.ThoughtTokens = int64(m.ThoughtsTokenCount)
		counts.CachedTokens = int64(m.CachedContentTokenCount)
		counts.TotalTokens = int64(m.TotalTokenCount)
	}
	book, _ := ctx.Value(bookContextKey{}).(string)
	key := usageKey{book, operation, model}
	u.mu.Lock()
	u.total.add(counts)
	group := u.groups[key]
	group.add(counts)
	u.groups[key] = group
	u.mu.Unlock()
	u.logger.InfoContext(ctx, "generation_call", "run_id", u.runID,
		"book", book, "operation", operation, "model", model, "usage", counts)
	return resp, err
}
