package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// This file tests HTTP-layer error classification in doJSON.
//
// Channel adapters handle platform-specific business codes (DingTalk errcode,
// Feishu code, and Telegram ok), while doJSON classifies HTTP responses. These
// are separate defenses: a 503 gateway response must be retried, whereas a 403
// should not waste three retry rounds.

func replyServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDoJSONClassifiesHTTPStatus(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		permanent bool
	}{
		{"200 success", 200, false},
		{"429 rate limit is retryable", 429, false},
		{"408 timeout is retryable", 408, false},
		{"500 server error is retryable", 500, false},
		{"502 gateway error is retryable", 502, false},
		{"503 unavailable is retryable", 503, false},
		{"400 bad request is permanent", 400, true},
		{"401 unauthorized is permanent", 401, true},
		{"403 forbidden is permanent", 403, true},
		{"404 not found is permanent", 404, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := replyServer(t, tc.status, `{"detail":"upstream says no"}`)
			_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
			if tc.status < 300 {
				if err != nil {
					t.Fatalf("2xx should not return an error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("non-2xx should return an error")
			}
			if got := IsPermanent(err); got != tc.permanent {
				t.Fatalf("HTTP %d permanence: want %v, got %v (%v)",
					tc.status, tc.permanent, got, err)
			}
			// Include the status code so users can distinguish bad configuration
			// from an upstream failure. The numeric code is language independent.
			if !strings.Contains(err.Error(), strconv.Itoa(tc.status)) {
				t.Errorf("error should contain HTTP status %d, got %v", tc.status, err)
			}
		})
	}
}

// TestDoJSONIncludesResponseSnippet ensures upstream error details are retained
// so users can understand why a request was rejected.
func TestDoJSONIncludesResponseSnippet(t *testing.T) {
	srv := replyServer(t, 400, `{"error":"invalid webhook token"}`)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "invalid webhook token") {
		t.Errorf("error should include upstream details, got %v", err)
	}
}

// TestDoJSONSnippetIsSingleLineAndBounded constrains snippets stored in
// last_error and shown in the UI; multiline or long responses would break it.
func TestDoJSONSnippetIsSingleLineAndBounded(t *testing.T) {
	// Respond with line breaks, a tab, and 5,000 extra characters.
	long := strings.Repeat("x", 5000)
	srv := replyServer(t, 500, "line1\nline2\r\n\tline3 "+long)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if strings.ContainsAny(msg, "\r\n\t") {
		t.Errorf("error should be a single line, got %q", msg)
	}
	// The 200-character snippet limit plus prefix must stay well below the response size.
	if len(msg) > 400 {
		t.Errorf("error is too long (%d bytes); snippet should be truncated: %q", len(msg), msg)
	}
}

// TestDoJSONRejectsOversizedResponse ensures unusually large upstream responses
// are not fully read into memory (each delivery history entry stores last_error).
func TestDoJSONRejectsOversizedResponse(t *testing.T) {
	huge := strings.Repeat("A", 1<<20) // 1 MiB
	srv := replyServer(t, 400, huge)
	_, err := doJSON(context.Background(), "GET", srv.URL, nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(err.Error()) > 400 {
		t.Errorf("oversized response should be truncated; error length is %d", len(err.Error()))
	}
}
