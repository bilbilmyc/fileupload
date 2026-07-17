package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	defaultSharePasswordMaxFailures = 5
	defaultSharePasswordCooldown    = 15 * time.Minute
	defaultSharePasswordMaxEntries  = 10_000
)

type sharePasswordAttempt struct {
	failures    int
	lastFailure time.Time
	lockedUntil time.Time
}

// sharePasswordLimiter 对同一客户端访问同一公开分享的失败密码验证限流。
// 映射键是 SHA-256 摘要，进程内不会保留原始分享 token 或客户端地址。
type sharePasswordLimiter struct {
	mu          sync.Mutex
	attempts    map[string]sharePasswordAttempt
	now         func() time.Time
	maxFailures int
	cooldown    time.Duration
	maxEntries  int
}

func newSharePasswordLimiter(maxFailures int, cooldown time.Duration, now func() time.Time) *sharePasswordLimiter {
	if maxFailures < 1 {
		maxFailures = defaultSharePasswordMaxFailures
	}
	if cooldown <= 0 {
		cooldown = defaultSharePasswordCooldown
	}
	if now == nil {
		now = time.Now
	}
	return &sharePasswordLimiter{
		attempts:    make(map[string]sharePasswordAttempt),
		now:         now,
		maxFailures: maxFailures,
		cooldown:    cooldown,
		maxEntries:  defaultSharePasswordMaxEntries,
	}
}

// Allow 返回是否允许继续进行密码验证；仅在冷却期内返回非零重试等待时间。
func (l *sharePasswordLimiter) Allow(token, client string) (time.Duration, bool) {
	if l == nil {
		return 0, true
	}
	now := l.now()
	key := l.key(token, client)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupLocked(now)
	attempt, ok := l.attempts[key]
	if !ok {
		return 0, true
	}
	if attempt.lockedUntil.After(now) {
		return attempt.lockedUntil.Sub(now), false
	}
	if !attempt.lockedUntil.IsZero() {
		delete(l.attempts, key)
	}
	return 0, true
}

// RecordFailure 记录一次密码验证失败。达到最大次数后立即进入冷却期，
// 使最后一次被拒绝的请求直接返回 429。
func (l *sharePasswordLimiter) RecordFailure(token, client string) (time.Duration, bool) {
	if l == nil {
		return 0, false
	}
	now := l.now()
	key := l.key(token, client)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupLocked(now)

	attempt := l.attempts[key]
	attempt.failures++
	attempt.lastFailure = now
	if attempt.failures >= l.maxFailures {
		attempt.lockedUntil = now.Add(l.cooldown)
		attempt.failures = 0
		l.attempts[key] = attempt
		return l.cooldown, true
	}
	l.attempts[key] = attempt
	return 0, false
}

// Reset 在密码验证成功后清除失败记录。
func (l *sharePasswordLimiter) Reset(token, client string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.attempts, l.key(token, client))
	l.mu.Unlock()
}

func (l *sharePasswordLimiter) key(token, client string) string {
	digest := sha256.Sum256([]byte(client + "\x00" + token))
	return hex.EncodeToString(digest[:])
}

func (l *sharePasswordLimiter) cleanupLocked(now time.Time) {
	staleAfter := l.cooldown * 3
	for key, attempt := range l.attempts {
		if !attempt.lockedUntil.IsZero() && !attempt.lockedUntil.After(now) {
			delete(l.attempts, key)
			continue
		}
		if !attempt.lastFailure.IsZero() && now.Sub(attempt.lastFailure) > staleAfter {
			delete(l.attempts, key)
		}
	}
	if len(l.attempts) < l.maxEntries {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, attempt := range l.attempts {
		if oldestKey == "" || attempt.lastFailure.Before(oldest) {
			oldestKey, oldest = key, attempt.lastFailure
		}
	}
	if oldestKey != "" {
		delete(l.attempts, oldestKey)
	}
}

func shareClientAddress(r *http.Request) string {
	address := r.RemoteAddr
	if host, _, err := net.SplitHostPort(address); err == nil && host != "" {
		return host
	}
	if address == "" {
		return "unknown"
	}
	return address
}
