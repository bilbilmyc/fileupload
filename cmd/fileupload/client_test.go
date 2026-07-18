package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ls", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("namespace") != "ns1" {
			t.Errorf("want namespace=ns1, got %s", r.URL.Query().Get("namespace"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"dir":      nil,
			"children": []any{},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "ns1")
	res, err := c.List(context.Background(), "/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || len(res.Children) != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestClientDelete(t *testing.T) {
	mux := http.NewServeMux()
	deleted := false
	mux.HandleFunc("DELETE /v1/files/{id}", func(w http.ResponseWriter, r *http.Request) {
		deleted = true
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "ns1")
	if err := c.Delete(context.Background(), "fid1"); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("handler not called")
	}
}

func TestClientScan(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/admin/scan", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"orphan_parts": 0})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "ns1")
	res, err := c.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := res["orphan_parts"], float64(0); got != want {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestClientRetry(t *testing.T) {
	attempts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ls", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"children": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	_, err := c.List(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestCompressZstd(t *testing.T) {
	in := []byte(strings.Repeat("hello world hello world ", 100))
	out, err := compressBuffer(in, "zstd")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) >= len(in) {
		t.Fatalf("compression did not reduce size: %d >= %d", len(out), len(in))
	}

	r, err := decompressReader(bytes.NewReader(out), "zstd")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	if !bytes.Equal(got, in) {
		t.Fatalf("roundtrip failed")
	}
}

func TestClientUploadFlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("HEAD /v1/files", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("POST /uploads", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/uploads/sess-123")
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PATCH /uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Upload-Offset", r.Header.Get("Upload-Offset"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/uploads/{id}/finalize", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"file_id":"fid","sha256":"sha","size":5,"name":"x"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	exists, err := c.CheckExists(context.Background(), "sha", "x")
	if err != nil {
		t.Fatalf("CheckExists unexpected err: %v", err)
	}
	if exists != nil {
		t.Fatalf("CheckExists should be nil for 404, got %+v", exists)
	}
	sid, err := c.CreateSession(context.Background(), 5, "sha", "none", 1024, "x")
	if err != nil || sid != "sess-123" {
		t.Fatalf("CreateSession: %s %v", sid, err)
	}
	if err := c.UploadChunk(context.Background(), sid, 0, []byte("hello"), 0); err != nil {
		t.Fatal(err)
	}
	info, err := c.Finalize(context.Background(), sid)
	if err != nil || info.FileID != "fid" {
		t.Fatalf("Finalize: %+v %v", info, err)
	}
}
