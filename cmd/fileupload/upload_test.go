package main

import "testing"

func TestParseSizeFlag(t *testing.T) {
	if got := parseSizeFlag("5m", 10); got != 5*1024*1024 {
		t.Fatalf("expected 5MB, got %d", got)
	}
	if got := parseSizeFlag("1g", 10); got != 1024*1024*1024 {
		t.Fatalf("expected 1GB, got %d", got)
	}
	if got := parseSizeFlag("", 10); got != 10 {
		t.Fatalf("expected default 10, got %d", got)
	}
}
