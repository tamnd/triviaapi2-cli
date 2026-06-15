package triviaapi2

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0 // no pacing in the test

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestGetQuestionsDecoding(t *testing.T) {
	payload := []map[string]any{
		{
			"id":               "abc-123",
			"type":             "text_choice",
			"difficulty":       "easy",
			"category":         "Science",
			"tags":             []string{"biology"},
			"question":         map[string]any{"text": "What is the powerhouse of the cell?"},
			"correctAnswer":    "Mitochondria",
			"incorrectAnswers": []string{"Nucleus", "Ribosome", "Golgi apparatus"},
			"isNiche":          false,
		},
	}
	raw, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	c.Rate = 0

	qs, err := c.GetQuestions(context.Background(), 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 1 {
		t.Fatalf("got %d questions, want 1", len(qs))
	}
	q := qs[0]
	if q.ID != "abc-123" {
		t.Errorf("ID = %q, want abc-123", q.ID)
	}
	if q.Question != "What is the powerhouse of the cell?" {
		t.Errorf("Question = %q", q.Question)
	}
	if q.CorrectAnswer != "Mitochondria" {
		t.Errorf("CorrectAnswer = %q, want Mitochondria", q.CorrectAnswer)
	}
	if len(q.IncorrectAnswers) != 3 {
		t.Errorf("IncorrectAnswers len = %d, want 3", len(q.IncorrectAnswers))
	}
	if q.Difficulty != "easy" {
		t.Errorf("Difficulty = %q, want easy", q.Difficulty)
	}
	if q.Category != "Science" {
		t.Errorf("Category = %q, want Science", q.Category)
	}
	if q.Type != "text_choice" {
		t.Errorf("Type = %q, want text_choice", q.Type)
	}
	if len(q.Tags) != 1 || q.Tags[0] != "biology" {
		t.Errorf("Tags = %v, want [biology]", q.Tags)
	}
	if q.IsNiche {
		t.Errorf("IsNiche = %v, want false", q.IsNiche)
	}
}

func TestGetCategoriesDecoding(t *testing.T) {
	// The /v2/categories response is a map of slug -> array (subcategories).
	payload := map[string]any{
		"science":             []string{},
		"arts_and_literature": []string{},
	}
	raw, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	c.Rate = 0

	cats, err := c.GetCategories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 {
		t.Fatalf("got %d categories, want 2", len(cats))
	}
	byID := make(map[string]string)
	for _, ci := range cats {
		byID[ci.ID] = ci.Name
	}
	if byID["science"] == "" {
		t.Errorf("science category missing")
	}
	if byID["arts_and_literature"] == "" {
		t.Errorf("arts_and_literature category missing")
	}
}

func TestGetQuestionsQueryParams(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	c.Rate = 0

	_, err := c.GetQuestions(context.Background(), 5, "science,history", "easy")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"limit=5", "difficulties=easy"} {
		if !searchStr(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
	if !searchStr(gotQuery, "categories=") {
		t.Errorf("query %q missing categories param", gotQuery)
	}
}

func TestSlugToName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"science", "Science"},
		{"arts_and_literature", "Arts And Literature"},
		{"film_and_tv", "Film And Tv"},
		{"general_knowledge", "General Knowledge"},
	}
	for _, tc := range cases {
		got := slugToName(tc.in)
		if got != tc.want {
			t.Errorf("slugToName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestJoinCategories(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Science", "Science"},
		{"Science,History", "Science,History"},
		{" Science , History ", "Science,History"},
		{"", ""},
		{"  ", ""},
	}
	for _, tc := range cases {
		got := joinCategories(tc.in)
		if got != tc.want {
			t.Errorf("joinCategories(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func searchStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
