package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestE2E_UploadFile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("HEAD /v1/files", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("POST /uploads", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/uploads/sess-1")
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PATCH /uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Upload-Offset", r.Header.Get("Upload-Offset"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/uploads/{id}/finalize", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"file_id":"fid","sha256":"abc123","size":5,"name":"hello.txt"}`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(file, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClient(srv.URL, "default")
	info, err := c.UploadFile(context.Background(), file, UploadOptions{
		ChunkSize:   1024,
		Concurrency: 1,
		Compress:    "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.FileID != "fid" {
		t.Fatalf("expected fid, got %s", info.FileID)
	}
	if info.SHA256 != "abc123" {
		t.Fatalf("expected sha256 abc123, got %s", info.SHA256)
	}
}

func TestE2E_UploadDir(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("HEAD /v1/files", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("POST /uploads", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/uploads/sess-1")
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PATCH /uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Upload-Offset", r.Header.Get("Upload-Offset"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/uploads/{id}/finalize", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"file_id":"fid","sha256":"sha","size":5,"name":"file.txt"}`)
	})
	mux.HandleFunc("POST /v1/dirs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"file_id":"dir-1","sha256":"","size":0,"name":"testdir"}`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "a.txt"), []byte("aaa"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "b.txt"), []byte("bbb"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClient(srv.URL, "default")
	info, err := c.UploadDir(context.Background(), dir, UploadOptions{
		ChunkSize:   1024,
		Concurrency: 1,
		Compress:    "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.FileID != "dir-1" {
		t.Fatalf("expected dir-1, got %s", info.FileID)
	}
}
