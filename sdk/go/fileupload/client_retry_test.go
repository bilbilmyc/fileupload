package fileupload

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientDoReturnsFinalRetryableResponse(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test")
	_, err := client.CheckExists(context.Background(), "sha256", "file.txt")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("CheckExists error = %v, want final 500 response", err)
	}
	if got := attempts.Load(); got != MaxRetries {
		t.Fatalf("attempts = %d, want %d", got, MaxRetries)
	}
}

func TestClientDoRetryWaitHonorsContext(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	client := NewClient(server.URL, "test")
	started := time.Now()
	_, err := client.CheckExists(ctx, "sha256", "file.txt")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CheckExists error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
		t.Fatalf("context cancellation took %v, want <= 300ms", elapsed)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}
