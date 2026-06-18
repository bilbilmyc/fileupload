package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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