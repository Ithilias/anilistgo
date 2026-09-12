package anilistgo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFindAnilistItem(t *testing.T) {
	skipIntegrationTest(t)

	firstEpisodeDateFirstSeasonAoT, _ := time.Parse("2006-01-02", "2013-04-07")
	firstEpisodeDateLastSeasonAoT, _ := time.Parse("2006-01-02", "2020-12-07")
	firstEpisodeDate21SeasonOnePiece, _ := time.Parse("2006-01-02", "2021-10-10")
	tests := []struct {
		title            string
		firstEpisodeDate *time.Time
		offset           int
		expectedURL      string
		expectedScore    int
		expectScore      bool
		expectError      bool
	}{
		{"Attack on Titan", &firstEpisodeDateFirstSeasonAoT, 0, "https://anilist.co/anime/16498", 0, false, false},
		{"Attack on Titan", &firstEpisodeDateLastSeasonAoT, 0, "https://anilist.co/anime/110277", 0, false, false},
		{"One Piece", &firstEpisodeDate21SeasonOnePiece, 0, "", 0, true, false},
		{"One Piece", nil, 0, "https://anilist.co/anime/21", 0, false, false},
	}

	for _, tt := range tests {
		result, err := FindAnilistItem(tt.title, tt.firstEpisodeDate, tt.offset)

		if err != nil && !tt.expectError {
			t.Errorf("expected no error but got: %v", err)
		}

		if err == nil && tt.expectError {
			t.Errorf("expected an error but got none")
		}

		if result.URL != tt.expectedURL {
			t.Errorf("expected URL %v but got %v", tt.expectedURL, result.URL)
		}

		if tt.expectScore && result.Score != tt.expectedScore {
			t.Errorf("expected score %v but got %v", tt.expectedScore, result.Score)
		}
	}
}

func TestGetFollowingNames(t *testing.T) {
	skipIntegrationTest(t)

	result, err := GetFollowingNames("Ithilias")
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	if len(result) == 0 {
		t.Errorf("expected result but got empty array %v", result)
	}
}

func TestGetAnilistItemByID(t *testing.T) {
	skipIntegrationTest(t)

	result, err := GetAnilistItemByID(161645)
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	if result.URL != "https://anilist.co/anime/161645" {
		t.Errorf("expected URL https://anilist.co/anime/161645 but got %v", result.URL)
	}
}

func TestGetUpdates(t *testing.T) {
	skipIntegrationTest(t)

	result, err := GetUpdates("Ithilias", MediaTypeAnime, nil, nil)
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}
	if len(result) == 0 {
		t.Errorf("expected result but got empty array %v", result)
	}
}

func TestSendRequestReturnsGraphQLError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":[{"message":"not found","extensions":{"status":404}}]}`))
	}))
	defer server.Close()

	_, err := sendRequest(server.URL, "query", nil, "")
	if err == nil {
		t.Fatal("expected GraphQL error")
	}
	if !strings.Contains(err.Error(), "not found (status 404)") {
		t.Fatalf("expected formatted GraphQL error, got %v", err)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if !apiErr.FromGraphQL {
		t.Error("expected FromGraphQL to be set")
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404 but got %d", apiErr.StatusCode)
	}
}

func TestSendRequestReturnsHTTPErrorForNonJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer server.Close()

	_, err := sendRequest(server.URL, "query", nil, "")
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if !strings.Contains(err.Error(), "status 429") {
		t.Fatalf("expected status code in error, got %v", err)
	}
	if strings.Contains(err.Error(), "X-RateLimit-Limit=") {
		t.Fatalf("expected empty rate-limit headers to be omitted, got %v", err)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected status 429 but got %d", apiErr.StatusCode)
	}
	if apiErr.RetryAfter != 0 {
		t.Errorf("expected no Retry-After hint but got %v", apiErr.RetryAfter)
	}
	if apiErr.Limit != -1 || apiErr.Remaining != -1 {
		t.Errorf("expected absent rate-limit headers to be -1, got %d/%d", apiErr.Limit, apiErr.Remaining)
	}
	if apiErr.Body != "rate limited" {
		t.Errorf("expected body to be captured, got %q", apiErr.Body)
	}
}

func TestSendRequestIncludesRateLimitHeadersWhenPresent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "90")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer server.Close()

	_, err := sendRequest(server.URL, "query", nil, "")
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	for _, expected := range []string{"status 429", "X-RateLimit-Limit=90", "X-RateLimit-Remaining=0", "Retry-After=60", "body=rate limited"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected %q in error, got %v", expected, err)
		}
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.RetryAfter != time.Minute {
		t.Errorf("expected a one minute Retry-After but got %v", apiErr.RetryAfter)
	}
	if apiErr.Limit != 90 || apiErr.Remaining != 0 {
		t.Errorf("expected limit 90/remaining 0 but got %d/%d", apiErr.Limit, apiErr.Remaining)
	}
}

// AniList reports rate limiting in the GraphQL payload as well as the status
// line, but only the headers carry Retry-After, so the HTTP error must win.
func TestSendRequestPrefersHTTPErrorOverGraphQLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Too Many Requests","status":429}]}`))
	}))
	defer server.Close()

	_, err := sendRequest(server.URL, "query", nil, "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.FromGraphQL {
		t.Error("expected the HTTP error to take precedence")
	}
	if apiErr.RetryAfter != 30*time.Second {
		t.Errorf("expected a 30s Retry-After but got %v", apiErr.RetryAfter)
	}
	if len(apiErr.GraphQLMessages) != 1 {
		t.Fatalf("expected the GraphQL messages to be kept, got %v", apiErr.GraphQLMessages)
	}
	if !strings.Contains(err.Error(), "graphql=Too Many Requests (status 429)") {
		t.Errorf("expected GraphQL messages in the error text, got %v", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	httpDate := time.Now().UTC().Add(90 * time.Second).Format(http.TimeFormat)

	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"absent", "", 0},
		{"seconds", "45", 45 * time.Second},
		{"padded seconds", " 45 ", 45 * time.Second},
		{"zero", "0", 0},
		{"negative", "-5", 0},
		{"garbage", "soon", 0},
		{"past http date", "Mon, 02 Jan 2006 15:04:05 GMT", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfter(tt.value); got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}

	// An absolute date resolves relative to now, so allow for clock drift.
	if got := parseRetryAfter(httpDate); got < 80*time.Second || got > 90*time.Second {
		t.Errorf("parseRetryAfter(%q) = %v, want roughly 90s", httpDate, got)
	}
}

func TestFetchUpdatesDataRequiresCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer server.Close()

	originalBaseAPIURL := baseAPIURL
	baseAPIURL = server.URL
	t.Cleanup(func() {
		baseAPIURL = originalBaseAPIURL
	})

	_, err := fetchUpdatesData("query", nil)
	if err == nil {
		t.Fatal("expected missing collection error")
	}
	if !strings.Contains(err.Error(), "media list collection") {
		t.Fatalf("expected missing collection error, got %v", err)
	}
}

func TestSendRequestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sendRequestContext(ctx, "http://example.invalid", "query", nil, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func skipIntegrationTest(t *testing.T) {
	t.Helper()
	if os.Getenv("ANILISTGO_INTEGRATION") != "1" {
		t.Skip("set ANILISTGO_INTEGRATION=1 to run live AniList integration tests")
	}
}
