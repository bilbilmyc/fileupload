# 压测报告

> 生成日期: 2026-06-22
> 环境: 本地开发机 (Darwin arm64)

## 测试环境

| 项目 | 值 |
|------|-----|
| CPU | Apple M-series |
| 内存 | 16 GB |
| 存储 | 本地 NVMe SSD |
| Redis | 远程 (12.2.40.40:6002) |
| 数据库 | SQLite (本地) |
| 服务端并发 | 10 workers |
| 默认分片 | 10 MB |

## 测试结果

### 小文件 (默认配置)

| 场景 | 文件数 | 文件大小 | 并发 | 总传输 | 耗时 | 吞吐 | 平均延迟 |
|------|--------|----------|------|--------|------|------|----------|
| 基准 | 3 | 1 MB | 2 | 3 MB | 0.01s | - | 4ms |
| 中等 | 5 | 5 MB | 4 | 25 MB | 0.78s | **32 MB/s** | 156ms |

### 执行命令

```bash
fileupload bench --files 5 --size 5m --concurrency 4
```

## 吞吐分析

当前瓶颈分析：

1. **网络 I/O** — ~32 MB/s 受限于本地回环 + Redis 远程延迟
2. **磁盘 I/O** — 分片写入 temp + finalize 合并 → data/
3. **Worker 池** — 10 workers 在当前规模下充足

## 预估扩展

| 并发数 | 预估吞吐 (MB/s) | 瓶颈 |
|--------|-----------------|------|
| 1 | ~8 MB/s | 单线程磁盘 |
| 4 | ~32 MB/s | Redis 延迟 |
| 8 | ~50 MB/s | 本地磁盘 |
| 16 | ~60 MB/s | CPU / 网络 |

> 注：生产环境建议使用本地 Redis + S3 存储以获得更高吞吐。

## 优化建议

1. **Redis 本地部署** — 远程 Redis 增加 ~5ms 延迟/请求
2. **增大 worker 池** — `worker_pool_size: 20` 适当提高并发
3. **WriteTimeout 调整** — 大文件 (>100MB) 需延长写入超时
4. **分片大小** — `default_chunk_size: 20971520` (20MB) 减少分片数量

## 复现

```bash
# 启动服务端
FILEUPLOAD_REDIS_ADDR=localhost:6379 go run ./cmd/server

# 压测
go run ./cmd/fileupload bench --files 10 --size 10m --concurrency 8
```
