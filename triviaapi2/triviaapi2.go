// Package triviaapi2 is the library behind the triviaapi2 command line:
// the HTTP client, request shaping, and the typed data models for
// The Trivia API v2 (the-trivia-api.com).
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
package triviaapi2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Host is the site this client talks to.
const Host = "the-trivia-api.com"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host

// DefaultUserAgent identifies the client to The Trivia API v2.
const DefaultUserAgent = "triviaapi2-cli/0.1 (tamnd87@gmail.com)"

// Config holds tunable parameters for the client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   BaseURL,
		Rate:      500 * time.Millisecond,
		Timeout:   15 * time.Second,
		Retries:   3,
		UserAgent: DefaultUserAgent,
	}
}

// Client talks to The Trivia API v2 over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	BaseURL   string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: cfg.UserAgent,
		BaseURL:   cfg.BaseURL,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// --- output types ---

// Question is one trivia question with its answers and metadata.
type Question struct {
	ID               string   `json:"id"                kit:"id"`
	Category         string   `json:"category"`
	Difficulty       string   `json:"difficulty"`
	Question         string   `json:"question"`
	CorrectAnswer    string   `json:"correct_answer"`
	IncorrectAnswers []string `json:"incorrect_answers"`
	Tags             []string `json:"tags"`
	Type             string   `json:"type"`
	IsNiche          bool     `json:"is_niche"`
}

// CategoryInfo describes one trivia category.
type CategoryInfo struct {
	ID   string `json:"id"   kit:"id"`
	Name string `json:"name"`
}

// --- wire types ---

// wireQuestion is the JSON shape returned by /v2/questions.
type wireQuestion struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Difficulty string  `json:"difficulty"`
	Category string    `json:"category"`
	Tags     []string  `json:"tags"`
	Question wireQText `json:"question"`
	CorrectAnswer    string   `json:"correctAnswer"`
	IncorrectAnswers []string `json:"incorrectAnswers"`
	IsNiche bool      `json:"isNiche"`
}

type wireQText struct {
	Text string `json:"text"`
}

// --- API methods ---

// GetQuestions fetches trivia questions from /v2/questions.
// limit: number of questions (1-50, 0 = API default of 1).
// categories: comma-separated category slugs, empty = all.
// difficulty: "easy", "medium", "hard", or empty = all.
func (c *Client) GetQuestions(ctx context.Context, limit int, categories, difficulty string) ([]*Question, error) {
	u, _ := url.Parse(c.BaseURL + "/v2/questions")
	q := u.Query()
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	if categories != "" {
		q.Set("categories", categories)
	}
	if difficulty != "" {
		q.Set("difficulties", difficulty)
	}
	u.RawQuery = q.Encode()

	body, err := c.Get(ctx, u.String())
	if err != nil {
		return nil, err
	}

	var wire []wireQuestion
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode questions: %w", err)
	}

	out := make([]*Question, 0, len(wire))
	for _, w := range wire {
		out = append(out, &Question{
			ID:               w.ID,
			Category:         w.Category,
			Difficulty:       w.Difficulty,
			Question:         w.Question.Text,
			CorrectAnswer:    w.CorrectAnswer,
			IncorrectAnswers: w.IncorrectAnswers,
			Tags:             w.Tags,
			Type:             w.Type,
			IsNiche:          w.IsNiche,
		})
	}
	return out, nil
}

// GetCategories fetches all trivia categories from /v2/categories.
func (c *Client) GetCategories(ctx context.Context) ([]*CategoryInfo, error) {
	body, err := c.Get(ctx, c.BaseURL+"/v2/categories")
	if err != nil {
		return nil, err
	}

	// Response: {"arts_and_literature": [...], ...} — keys are category slugs.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode categories: %w", err)
	}

	out := make([]*CategoryInfo, 0, len(raw))
	for id := range raw {
		out = append(out, &CategoryInfo{ID: id, Name: slugToName(id)})
	}
	return out, nil
}

// --- HTTP transport ---

// Get fetches rawURL and returns the response body. It paces and retries
// according to the client's settings.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// joinCategories normalises the categories flag value: trims spaces and joins
// with a comma so a single ?categories=... param carries all values.
func joinCategories(s string) string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, ",")
}

// slugToName converts a category slug like "arts_and_literature" to a
// human-readable name "Arts And Literature". The API v2 categories endpoint
// returns only the slug keys, so we derive display names from the slugs.
func slugToName(slug string) string {
	parts := strings.Split(slug, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
