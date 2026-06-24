package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// RedisStore 热数据存储：上传会话/分片状态/offset
type RedisStore struct {
	client *redis.Client
	prefix string // key 前缀，如 "upload:"
	nowFn  func() time.Time
}

// NewRedisStore 创建 RedisStore
func NewRedisStore(client *redis.Client, keyPrefix string) *RedisStore {
	if keyPrefix == "" {
		keyPrefix = "upload:"
	}
	return &RedisStore{
		client: client,
		prefix: keyPrefix,
		nowFn:  time.Now,
	}
}

func (r *RedisStore) sessionKey(id string) string {
	return r.prefix + "session:" + id
}

func (r *RedisStore) chunksKey(id string) string {
	return r.prefix + "chunks:" + id
}

func (r *RedisStore) offsetKey(id string) string {
	return r.prefix + "offset:" + id
}

// CreateSession 创建上传会话
func (r *RedisStore) CreateSession(ctx context.Context, s *domain.UploadSession) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("序列化会话: %w", err)
	}

	ttl := time.Until(s.ExpireAt)
	if ttl <= 0 {
		ttl = time.Hour
	}

	return r.client.Set(ctx, r.sessionKey(s.SessionID), data, ttl).Err()
}

// GetSession 获取上传会话
func (r *RedisStore) GetSession(ctx context.Context, id string) (*domain.UploadSession, error) {
	data, err := r.client.Get(ctx, r.sessionKey(id)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取会话: %w", err)
	}

	var s domain.UploadSession
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("反序列化会话: %w", err)
	}
	return &s, nil
}

// UpdateOffset 原子更新 offset 和分片信息（Lua 脚本）
func (r *RedisStore) UpdateOffset(ctx context.Context, id string, sliceIndex int, sliceSha string, addBytes int64) error {
	// Lua 脚本：原子更新 chunks hash 和 offset
	script := `
		local chunksKey = KEYS[1]
		local offsetKey = KEYS[2]
		local index = ARGV[1]
		local sha = ARGV[2]
		local bytes = tonumber(ARGV[3])

		-- 更新分片信息
		redis.call("HSET", chunksKey, index, sha)

		-- 更新 offset（原子增）
		local newOffset = redis.call("INCRBY", offsetKey, bytes)

		return newOffset
	`

	keys := []string{r.chunksKey(id), r.offsetKey(id)}
	args := []any{strconv.Itoa(sliceIndex), sliceSha, addBytes}

	return r.client.Eval(ctx, script, keys, args...).Err()
}

// ListChunks 列举已落盘分片
func (r *RedisStore) ListChunks(ctx context.Context, id string) ([]domain.ChunkInfo, error) {
	result, err := r.client.HGetAll(ctx, r.chunksKey(id)).Result()
	if err != nil {
		return nil, fmt.Errorf("读取分片列表: %w", err)
	}

	var chunks []domain.ChunkInfo
	for idxStr, sha256 := range result {
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			continue
		}
		chunks = append(chunks, domain.ChunkInfo{
			Index:  idx,
			SHA256: sha256,
		})
	}
	return chunks, nil
}

// DeleteSession 删除会话所有相关 key
func (r *RedisStore) DeleteSession(ctx context.Context, id string) error {
	keys := []string{
		r.sessionKey(id),
		r.chunksKey(id),
		r.offsetKey(id),
	}
	return r.client.Del(ctx, keys...).Err()
}

// TouchSession 续约会话 TTL
func (r *RedisStore) TouchSession(ctx context.Context, id string, ttl time.Duration) error {
	return r.client.Expire(ctx, r.sessionKey(id), ttl).Err()
}

// ListExpiredSessions 列出过期会话（reaper 用）
// 注意：Redis 不会主动返回已过期 key，这里扫描所有 session 看 expireAt
// 实际实现通过 SCAN 查找所有 upload:session:* key 并检查 TTL
func (r *RedisStore) ListExpiredSessions(ctx context.Context) ([]string, error) {
	var expired []string
	iter := r.client.Scan(ctx, 0, r.prefix+"session:*", 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		ttl, err := r.client.TTL(ctx, key).Result()
		if err != nil {
			continue
		}
		if ttl <= 0 {
			// 从 key 中提取 sessionID
			id := key[len(r.prefix+"session:"):]
			expired = append(expired, id)
		}
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("扫描会话: %w", err)
	}
	return expired, nil
}

// Close 关闭 Redis 连接
func (r *RedisStore) Close() error {
	return r.client.Close()
}

// HealthCheck 检查 Redis 连接。
func (r *RedisStore) HealthCheck(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}
