package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewSessionReaper(t *testing.T) {
	r := NewSessionReaper(nil, nil, "/tmp", time.Minute)
	if r == nil {
		t.Fatal("NewSessionReaper returned nil")
	}
	if r.interval != time.Minute {
		t.Errorf("interval = %v, want 1m", r.interval)
	}
	r.Stop()
}

func TestSessionReaper_DefaultInterval(t *testing.T) {
	r := NewSessionReaper(nil, nil, "/tmp", 0)
	if r.interval != time.Minute {
		t.Errorf("default interval = %v, want 1m", r.interval)
	}
}

func TestNewConsistencyScanner(t *testing.T) {
	s := NewConsistencyScanner(nil, nil, "/tmp/data", "/tmp/tmp")
	if s == nil {
		t.Fatal("NewConsistencyScanner returned nil")
	}
}

func TestScanner_ScanOrphanParts(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "orphan1.part"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "orphan2.part"), []byte("x"), 0644)

	s := NewConsistencyScanner(nil, nil, "/tmp/data", dir)
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	report := result.(*ScannerReport)
	if report.OrphanParts != 2 {
		t.Errorf("OrphanParts = %d, want 2", report.OrphanParts)
	}
}

func TestScanner_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	s := NewConsistencyScanner(nil, nil, "/tmp/data", dir)
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	report := result.(*ScannerReport)
	if report.OrphanParts != 0 {
		t.Errorf("OrphanParts = %d, want 0", report.OrphanParts)
	}
}

func TestSessionReaper_StartStop(t *testing.T) {
	dir := t.TempDir()
	r := NewSessionReaper(nil, nil, dir, 100*time.Millisecond)
	r.Start()
	time.Sleep(250 * time.Millisecond)
	r.Stop()
}
