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
	// Season 3 shares FALL 2026 with the movie The Late Lady's Treasure, which
	// search ranks higher; the TV season must win.
	firstEpisodeDateApothecaryS3, _ := time.Parse("2006-01-02", "2026-10-02")
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
		{"The Apothecary Diaries", &firstEpisodeDateApothecaryS3, 0, "https://anilist.co/anime/195516", 0, false, false},
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

func TestSendRequestRetriesTransientRefusals(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests <= MaxRetries {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"User":{"id":7}}}`))
	}))
	defer server.Close()

	result, err := sendRequest(server.URL, "query", nil, "")
	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	if result.Data.User.ID != 7 {
		t.Errorf("expected the retried response to be returned, got %+v", result.Data.User)
	}
	if requests != MaxRetries+1 {
		t.Errorf("expected %d attempts, got %d", MaxRetries+1, requests)
	}
}

func TestSendRequestRetriesServerErrors(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	if _, err := sendRequest(server.URL, "query", nil, ""); err == nil {
		t.Fatal("expected an error once the retries are exhausted")
	}
	if requests != MaxRetries+1 {
		t.Errorf("expected %d attempts, got %d", MaxRetries+1, requests)
	}
}

func TestSendRequestDoesNotRetryClientErrors(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	if _, err := sendRequest(server.URL, "query", nil, ""); err == nil {
		t.Fatal("expected an error")
	}
	if requests != 1 {
		t.Errorf("expected a client error not to be retried, got %d attempts", requests)
	}
}

// Waiting minutes belongs to the caller's backoff, not to a library call.
func TestSendRequestDoesNotSleepThroughLongRetryAfter(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	started := time.Now()
	if _, err := sendRequest(server.URL, "query", nil, ""); err == nil {
		t.Fatal("expected an error")
	}
	if requests != 1 {
		t.Errorf("expected no retry for a long Retry-After, got %d attempts", requests)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("expected the call to return promptly, took %v", elapsed)
	}
}

func TestSendRequestStopsRetryingWhenContextIsCancelled(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := sendRequestContext(ctx, server.URL, "query", nil, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if requests > MaxRetries+1 {
		t.Errorf("expected at most %d attempts, got %d", MaxRetries+1, requests)
	}
}

func TestRetryDelay(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		attempt     int
		wantRetry   bool
		wantAtLeast time.Duration
		wantAtMost  time.Duration
	}{
		{"plain error", errors.New("boom"), 0, false, 0, 0},
		{"not found", &APIError{StatusCode: http.StatusNotFound}, 0, false, 0, 0},
		{"rate limited", &APIError{StatusCode: http.StatusTooManyRequests}, 0, true, RetryBaseDelay, 2 * RetryBaseDelay},
		{"rate limited, second attempt", &APIError{StatusCode: http.StatusTooManyRequests}, 1, true, 2 * RetryBaseDelay, 4 * RetryBaseDelay},
		{"server error", &APIError{StatusCode: http.StatusInternalServerError}, 0, true, RetryBaseDelay, 2 * RetryBaseDelay},
		{"graphql rate limit", &APIError{StatusCode: http.StatusTooManyRequests, FromGraphQL: true}, 0, true, RetryBaseDelay, 2 * RetryBaseDelay},
		{"short Retry-After is honoured", &APIError{StatusCode: http.StatusTooManyRequests, RetryAfter: 2 * time.Second}, 0, true, 2 * time.Second, 2 * time.Second},
		{"long Retry-After is left to the caller", &APIError{StatusCode: http.StatusTooManyRequests, RetryAfter: time.Minute}, 0, false, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay, retryable := retryDelay(tt.err, tt.attempt)
			if retryable != tt.wantRetry {
				t.Fatalf("retryable = %v, want %v", retryable, tt.wantRetry)
			}
			if !tt.wantRetry {
				return
			}
			if delay < tt.wantAtLeast || delay > tt.wantAtMost {
				t.Errorf("delay = %v, want between %v and %v", delay, tt.wantAtLeast, tt.wantAtMost)
			}
			if delay > MaxRetryDelay+time.Duration(RetryJitter*float64(MaxRetryDelay)) {
				t.Errorf("delay = %v exceeds the cap", delay)
			}
		})
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
