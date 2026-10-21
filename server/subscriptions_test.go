package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubscriptionRoutesRequireARTEXAuthentication(t *testing.T) {
	s := &Server{}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		called := false
		h := s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/llm/subscriptions/chatgpt", nil)
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || called {
			t.Fatal("subscription route bypassed authentication")
		}
	}
}
