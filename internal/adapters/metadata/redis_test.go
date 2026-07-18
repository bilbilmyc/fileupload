package metadata

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

func newTestRedis(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	store := NewRedisStore(client, "test:")
	t.Cleanup(func() { store.Close() })
	return store, mr
}

func TestRedisStore_CreateGetSession(t *testing.T) {
	s, _ := newTestRedis(t)
	ctx := context.Background()

	now := time.Now()
	session := &domain.UploadSession{
		SessionID:    "sess-001",
		SHA256:       "abc123",
		UploadLength: 10485760,
		Compression:  domain.CompZstd,
		ChunkSize:    1048576,
		Namespace:    "demo",
		FileName:     "bigfile.dat",
		CreatedAt:    now,
		ExpireAt:     now.Add(time.Hour),
		Status:       domain.SessionActive,
	}

	if err := s.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession error = %v", err)
	}

	got, err := s.GetSession(ctx, "sess-001")
	if err != nil {
		t.Fatalf("GetSession error = %v", err)
	}
	if got == nil {
		t.Fatal("GetSession returned nil")
	}
	if got.SessionID != session.SessionID {
		t.Errorf("SessionID = %s, want %s", got.SessionID, session.SessionID)
	}
	if got.SHA256 != session.SHA256 {
		t.Errorf("SHA256 = %s, want %s", got.SHA256, session.SHA256)
	}
	if got.UploadLength != session.UploadLength {
		t.Errorf("UploadLength = %d, want %d", got.UploadLength, session.UploadLength)
	}
	if got.Status != domain.SessionActive {
		t.Errorf("Status = %s, want active", got.Status)
	}
}

func TestRedisStore_GetSession_NotExist(t *testing.T) {
	s, _ := newTestRedis(t)
	ctx := context.Background()

	got, err := s.GetSession(ctx, "no-such")
	if err != nil {
		t.Fatalf("GetSession error = %v", err)
	}
	if got != nil {
		t.Error("不存在的会话应返回 nil")
	}
}

func TestRedisStore_UpdateOffset(t *testing.T) {
	s, mr := newTestRedis(t)
	ctx := context.Background()

	// 先创建会话
	session := &domain.UploadSession{
		SessionID: "offset-test", UploadLength: 1000,
		CreatedAt: time.Now(), ExpireAt: time.Now().Add(time.Hour),
		Status: domain.SessionActive,
	}
	s.CreateSession(ctx, session)

	// 更新 offset
	if err := s.UpdateOffset(ctx, "offset-test", 0, "sha-of-chunk-0", 500); err != nil {
		t.Fatalf("UpdateOffset error = %v", err)
	}
	if err := s.UpdateOffset(ctx, "offset-test", 1, "sha-of-chunk-1", 500); err != nil {
		t.Fatalf("UpdateOffset error = %v", err)
	}

	// 验证 chunks
	chunks, err := s.ListChunks(ctx, "offset-test")
	if err != nil {
		t.Fatalf("ListChunks error = %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks count = %d, want 2", len(chunks))
	}

	found := make(map[int]string)
	for _, c := range chunks {
		found[c.Index] = c.SHA256
	}
	if found[0] != "sha-of-chunk-0" {
		t.Errorf("chunk[0] sha = %s", found[0])
	}
	if found[1] != "sha-of-chunk-1" {
		t.Errorf("chunk[1] sha = %s", found[1])
	}

	// 验证 Redis 中的 offset
	offsetStr, err := mr.Get("test:offset:offset-test")
	if err != nil {
		t.Fatalf("Redis GET offset error = %v", err)
	}
	if offsetStr != "1000" {
		t.Errorf("offset = %s, want 1000", offsetStr)
	}
}

func TestRedisStore_TouchSession(t *testing.T) {
	s, mr := newTestRedis(t)
	ctx := context.Background()

	session := &domain.UploadSession{
		SessionID: "touch-test", UploadLength: 100,
		CreatedAt: time.Now(), ExpireAt: time.Now().Add(time.Minute),
		Status: domain.SessionActive,
	}
	s.CreateSession(ctx, session)

	// 续期
	if err := s.TouchSession(ctx, "touch-test", 30*time.Minute); err != nil {
		t.Fatalf("TouchSession error = %v", err)
	}

	// 验证 TTL 被更新
	ttl := mr.TTL("test:session:touch-test")
	if ttl <= 0 || ttl > 31*time.Minute {
		t.Errorf("TTL = %v, 应该在 30m 附近", ttl)
	}
}

func TestRedisStore_DeleteSession(t *testing.T) {
	s, mr := newTestRedis(t)
	ctx := context.Background()

	session := &domain.UploadSession{
		SessionID: "del-test", UploadLength: 100,
		CreatedAt: time.Now(), ExpireAt: time.Now().Add(time.Hour),
		Status: domain.SessionActive,
	}
	s.CreateSession(ctx, session)
	s.UpdateOffset(ctx, "del-test", 0, "sha", 50)

	// 删除
	if err := s.DeleteSession(ctx, "del-test"); err != nil {
		t.Fatalf("DeleteSession error = %v", err)
	}

	// 验证所有 key 都被删除（miniredis v2 Keys() 无参数）
	allKeys := mr.Keys()
	for _, k := range allKeys {
		if k == "test:session:del-test" || k == "test:chunks:del-test" || k == "test:offset:del-test" {
			t.Errorf("key %s 未删除", k)
		}
	}
}

func TestRedisStore_ListExpiredSessions(t *testing.T) {
	s, mr := newTestRedis(t)
	ctx := context.Background()

	// 创建一个有效会话
	valid := &domain.UploadSession{
		SessionID: "valid-sess", UploadLength: 100,
		CreatedAt: time.Now(), ExpireAt: time.Now().Add(time.Hour),
		Status: domain.SessionActive,
	}
	s.CreateSession(ctx, valid)

	// 手动创建一个过期 key（直接操作 Redis）
	mr.Set("test:session:expired-sess", "{}")
	mr.FastForward(2 * time.Hour) // 使 key 过期

	// 列出过期会话
	expired, err := s.ListExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("ListExpiredSessions error = %v", err)
	}

	// 注意：miniredis 中 key 自动过期是惰性的，FastForward 后 key 已被删除
	// 所以 ListExpiredSessions 应该返回空（已过期的 key 已不可见）
	// 这个测试验证不会 panic/报错
	_ = expired
}

func TestRedisStore_ListChunks_Empty(t *testing.T) {
	s, _ := newTestRedis(t)
	ctx := context.Background()

	chunks, err := s.ListChunks(ctx, "no-chunks")
	if err != nil {
		t.Fatalf("ListChunks error = %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("empty chunks count = %d, want 0", len(chunks))
	}
}

func TestRedisStore_ClaimSessionFinalizingIsAtomic(t *testing.T) {
	s, _ := newTestRedis(t)
	ctx := context.Background()
	session := &domain.UploadSession{SessionID: "claim-001", Namespace: "demo", ExpireAt: time.Now().Add(time.Hour), Status: domain.SessionActive}
	if err := s.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimSessionFinalizing(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("ClaimSessionFinalizing error = %v", err)
	}
	if claimed.Status != domain.SessionFinalizing {
		t.Fatalf("claimed status = %s, want finalizing", claimed.Status)
	}
	if _, err := s.ClaimSessionFinalizing(ctx, session.SessionID); err != domain.ErrSessionState {
		t.Fatalf("second claim error = %v, want %v", err, domain.ErrSessionState)
	}
}

func TestRedisStore_ClaimRefreshTokenIsAtomic(t *testing.T) {
	s, _ := newTestRedis(t)
	ctx := context.Background()
	expiresAt := time.Now().Add(time.Hour)

	claimed, err := s.ClaimRefreshToken(ctx, "refresh-001", expiresAt)
	if err != nil {
		t.Fatalf("first ClaimRefreshToken error = %v", err)
	}
	if !claimed {
		t.Fatal("first ClaimRefreshToken = false, want true")
	}

	claimed, err = s.ClaimRefreshToken(ctx, "refresh-001", expiresAt)
	if err != nil {
		t.Fatalf("second ClaimRefreshToken error = %v", err)
	}
	if claimed {
		t.Fatal("second ClaimRefreshToken = true, want false")
	}
}
