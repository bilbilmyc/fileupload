package main

import "testing"

func TestParseSize(t *testing.T) {
	flags := map[string]string{"chunk-size": "5m"}
	if got := parseSize(flags, "chunk-size", 10); got != 5*1024*1024 {
		t.Fatalf("expected 5MB, got %d", got)
	}
}

func TestGetFlag(t *testing.T) {
	flags := map[string]string{"namespace": "test-ns"}
	if got := getFlag(flags, "namespace", "default"); got != "test-ns" {
		t.Fatalf("expected test-ns, got %s", got)
	}
	if got := getFlag(flags, "missing", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback, got %s", got)
	}
}
