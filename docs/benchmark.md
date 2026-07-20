# 可重复压测报告

> 目的：提供可提交到 CI/发布说明中的、可重复的基线；结果不是对所有生产硬件的性能承诺。

## 测试环境与口径

| 项目 | 值 |
|------|-----|
| 测试日期 | 2026-07-19 |
| 主机 | Windows 本地开发机 |
| 服务端 | 当前工作树构建的 `server.exe` |
| 存储 | LocalFS，本地磁盘 |
| 数据库 | SQLite |
| Redis | miniredis，本机 `127.0.0.1:16379` |
| worker pool | 8 |
| 并发 | 8 |
| 随机种子 | `20260719` |

### 实测命令

```powershell
fileupload.exe --server http://127.0.0.1:18080 --namespace benchmark `
  bench --files 50 --size 1m --concurrency 8 --seed 20260719 --cleanup --json
```

`--seed` 保证压测输入可重复；`--cleanup` 会在压测结束后删除并清理回收站中的测试文件。压测失败或清理失败都会返回非零退出码，适合接入 CI 或发布前检查。

## 结果

| 指标 | 结果 |
|------|------:|
| 请求文件数 | 50 |
| 成功 / 失败 | 50 / 0 |
| 上传总量 | 50 MiB |
| 总耗时 | 0.739 s |
| 吞吐 | **67.68 MiB/s** |
| 错误率 | **0%** |
| 延迟最小 / 平均 | 47.0 / 113.2 ms |
| 延迟 p50 / p95 / p99 | 114.0 / 180.6 / 185.5 ms |
| 延迟最大 | 219.9 ms |
| 清理失败数 | 0 |

原始 JSON 结果由 CLI 直接输出，结果字段包含 `started_at`、`bytes_uploaded`、`throughput_mib_per_second`、`error_rate`、完整延迟分位数与清理状态。

## 解释与生产使用建议

- 该结果只代表“Windows + 本地 SQLite + 本机 Redis + 本地磁盘 + 1 MiB 文件”的回环基线，不能直接外推公网、容器、PostgreSQL 或 S3 性能。
- 上线前应在目标机器使用相同 `--seed` 和业务代表性文件大小重复测试，并记录 CPU、内存、磁盘、Redis、数据库和网络指标。
- 小规模内部使用优先关注 `error_rate`、p95/p99 延迟、磁盘剩余空间、Redis 连接状态、数据库连接池和队列积压，而不是只看吞吐。
- CLI 可使用 `--max-error-rate` 与 `--min-throughput-mibps` 作为发布门槛，例如：

```powershell
fileupload.exe --server http://127.0.0.1:8080 bench `
  --files 100 --size 10m --concurrency 8 --seed 20260719 --cleanup `
  --max-error-rate 0 --min-throughput-mibps 20 --json
```

## 复现前提

1. 启动 Redis（生产建议使用 Redis 7+，配置认证和持久化）。
2. 启动 fileupload，确保 `storage.data_dir`、`storage.temp_dir` 已创建且服务账号可读写。
3. 使用独立 namespace，并启用 `--cleanup`，避免污染真实数据。
4. 将 JSON 保存到构建产物或 CI artifact，和版本、配置摘要一同归档。