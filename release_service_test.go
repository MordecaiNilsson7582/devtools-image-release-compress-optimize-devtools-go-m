package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReleaseDecision(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event buildEvent
		valid bool
	}{
		{"stable key", buildEvent{"cli-42", "YWJj", 640}, true},
		{"missing build", buildEvent{"", "YWJj", 640}, false},
		{"invalid width", buildEvent{"cli-42", "YWJj", 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.valid {
				if _, err := (service{}).publish(context.Background(), tc.event); err == nil {
					t.Fatal("expected validation error")
				}
				return
			}
			first := objectKey(tc.event)
			if first != objectKey(tc.event) || !strings.HasPrefix(first, "build-cli-42-") || !strings.HasSuffix(first, ".webp") {
				t.Fatalf("unexpected key %q", first)
			}
			changed := tc.event
			changed.Width++
			if first == objectKey(changed) {
				t.Fatal("new transform must create a new asset key")
			}
		})
	}
}

func TestEnvelopeAndRetry(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("request boundary")
		}
		w.Header().Set("Content-Type", "application/json")
		if count == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(429)
			w.Write([]byte(`{"ok":false,"error":{"code":"RATE_LIMITED","message":"retry"}}`))
			return
		}
		w.WriteHeader(400)
		w.Write([]byte(`{"ok":false,"error":{"code":"INVALID_ARGUMENT","message":"bad input"}}`))
	}))
	defer server.Close()
	var delay time.Duration
	c := &client{key: "test-key", base: server.URL, http: server.Client(), wait: func(_ context.Context, d time.Duration) error { delay = d; return nil }}
	var out json.RawMessage
	err := c.call(context.Background(), http.MethodPost, "/v1/image/resize", map[string]any{"image": "YWJj", "width": 640}, &out)
	apiErr, ok := err.(*apiError)
	if !ok || apiErr.Code != "INVALID_ARGUMENT" || apiErr.Status != 400 || count != 2 || delay != 2*time.Second {
		t.Fatalf("error=%v calls=%d delay=%v", err, count, delay)
	}
}
