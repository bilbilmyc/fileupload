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