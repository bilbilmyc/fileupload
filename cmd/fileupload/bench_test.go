package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecuteBenchReportsLatencyAndCleansUp(t *testing.T) {
	var sessions atomic.Int64
	var deleted atomic.Int64
	var purged atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("HEAD /v1/files", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("POST /uploads", func(w http.ResponseWriter, _ *http.Request) {
		id := sessions.Add(1)
		w.Header().Set("Location", fmt.Sprintf("/uploads/session-%d", id))
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PATCH /uploads/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /v1/uploads/{id}/finalize", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"file_id":"file-%s","sha256":"sha","size":1024,"name":"bench"}`, strings.TrimPrefix(r.PathValue("id"), "session-"))
	})
	mux.HandleFunc("DELETE /v1/files/{id}", func(w http.ResponseWriter, _ *http.Request) {
		deleted.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /v1/trash/{id}", func(w http.ResponseWriter, _ *http.Request) {
		purged.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(server.URL, "bench-test")
	result, err := executeBench(context.Background(), client, benchOptions{
		Files: 4, FileSize: 1024, Concurrency: 2, Seed: 42, Cleanup: true,
	})
	if err != nil {
		t.Fatalf("executeBench error = %v", err)
	}
	if result.Succeeded != 4 || result.Failed != 0 || result.BytesUploaded != 4096 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Latency.Max <= 0 || result.Latency.P95 <= 0 {
		t.Fatalf("latency summary missing: %#v", result.Latency)
	}
	if deleted.Load() != 4 || purged.Load() != 4 || result.CleanupFailed != 0 {
		t.Fatalf("cleanup deleted=%d purged=%d result=%#v", deleted.Load(), purged.Load(), result)
	}
}

func TestSummarizeBenchLatencyPercentiles(t *testing.T) {
	result := summarizeBenchLatency([]time.Duration{
		100 * time.Millisecond, 10 * time.Millisecond, 50 * time.Millisecond, 20 * time.Millisecond,
	})
	if result.Min != 10 || result.P50 != 20 || result.P95 != 100 || result.P99 != 100 || result.Max != 100 {
		t.Fatalf("unexpected latency summary: %#v", result)
	}
}

func TestExecuteBenchRejectsInvalidOptions(t *testing.T) {
	client := NewClient("http://localhost", "default")
	if _, err := executeBench(context.Background(), client, benchOptions{}); err == nil {
		t.Fatal("expected validation error")
	}
}
