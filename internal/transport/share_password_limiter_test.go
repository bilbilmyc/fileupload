package transport

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestSharePasswordLimiter_LocksOnlyMatchingClientAndShare(t *testing.T) {
	now := time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC)
	limiter := newSharePasswordLimiter(2, time.Minute, func() time.Time { return now })

	if retry, locked := limiter.RecordFailure("s-one", "198.51.100.10"); locked || retry != 0 {
		t.Fatalf("first failure = retry %s, locked %t; want unlocked", retry, locked)
	}
	if retry, locked := limiter.RecordFailure("s-one", "198.51.100.10"); !locked || retry != time.Minute {
		t.Fatalf("second failure = retry %s, locked %t; want 1m lock", retry, locked)
	}

	if retry, allowed := limiter.Allow("s-one", "198.51.100.10"); allowed || retry != time.Minute {
		t.Fatalf("locked pair Allow = retry %s, allowed %t", retry, allowed)
	}
	if _, allowed := limiter.Allow("s-one", "198.51.100.11"); !allowed {
		t.Fatal("a different client should not be locked")
	}
	if _, allowed := limiter.Allow("s-two", "198.51.100.10"); !allowed {
		t.Fatal("a different share should not be locked")
	}

	now = now.Add(time.Minute + time.Second)
	if retry, allowed := limiter.Allow("s-one", "198.51.100.10"); !allowed || retry != 0 {
		t.Fatalf("expired lock Allow = retry %s, allowed %t; want allowed", retry, allowed)
	}
}

func TestSharePasswordLimiter_ResetAndClientAddress(t *testing.T) {
	now := time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC)
	limiter := newSharePasswordLimiter(2, time.Minute, func() time.Time { return now })
	limiter.RecordFailure("s-one", "198.51.100.10")
	limiter.Reset("s-one", "198.51.100.10")
	if retry, locked := limiter.RecordFailure("s-one", "198.51.100.10"); locked || retry != 0 {
		t.Fatalf("failure after reset = retry %s, locked %t; want unlocked", retry, locked)
	}

	req := httptest.NewRequest("GET", "/s/s-one", nil)
	req.RemoteAddr = "198.51.100.10:43123"
	if got := shareClientAddress(req); got != "198.51.100.10" {
		t.Fatalf("shareClientAddress() = %q, want host only", got)
	}
	req.RemoteAddr = "not-a-host-port"
	if got := shareClientAddress(req); got != "not-a-host-port" {
		t.Fatalf("shareClientAddress() fallback = %q", got)
	}
}
