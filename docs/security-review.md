# 安全审查记录

> 审查日期：2026-07-20
> 范围：当前工作树 Go 服务、CLI、前端依赖与 CI 配置。

## 已完成的加固

- 关键写操作、认证/授权失败、限流、管理操作和分享访问写入审计日志；日志 detail 为结构化 JSON，不记录原始 URL、查询参数或分享 token。
- JWT 用户名不存在时执行 dummy bcrypt 校验，降低用户名枚举的时序差异。
- 登录失败按“客户端 IP + 规范化用户名”限流：15 分钟内 5 次失败触发 429，并返回 `Retry-After`；成功登录会清除失败状态。
- 默认安全响应头：CSP、`X-Content-Type-Options`、`X-Frame-Options`、`Referrer-Policy`、`Permissions-Policy`；TLS 请求额外设置 HSTS。
- 控制面请求体限制为 1 MiB；上传会话和分片接口保留由上传处理器执行的大小限制。
- 自动数据库迁移使用版本表、逐版本事务；PostgreSQL 通过 advisory lock 串行化多实例启动迁移。
- CI 增加 `govulncheck` 与前端高危依赖审计门禁。

## 扫描结果

| 检查 | 结果 |
|------|------|
| `go vet ./...` | 通过 |
| `go test -race -count=1 -timeout 120s ./...` | 通过后作为发布门禁 |
| `govulncheck ./...` | 当前调用图无可达漏洞 |
| `pnpm audit --audit-level high` | 未发现已知高危漏洞 |

CI 固定使用 Go 1.25.12，覆盖本次扫描发现的旧标准库补丁版本问题。

本次 `govulncheck@v1.6.0` 报告 `GO-2026-5932` 位于 `golang.org/x/crypto@v0.53.0` 的未维护 `openpgp` 包；当前调用图不包含该包，且该漏洞暂无上游修复版本。项目只使用 `x/crypto` 的 bcrypt 等包，已确认无可达漏洞；依赖升级时继续复查。

## 已接受的残余风险

1. 登录和分享密码限流为进程内状态；多实例部署必须在网关/WAF 层补充分布式限流。
2. 当前浏览器鉴权方案仍由前端持有 token；CSP 可降低 XSS 风险，但不能替代 HttpOnly cookie 方案。若暴露给不完全信任的用户，应单独规划认证架构升级。
3. 审计写入失败不会阻断业务请求，会输出告警日志；若需要不可抵赖审计，应改为可靠队列/外部日志管道并增加告警。
4. SQLite 适合开发和小规模内部使用；多实例、高并发写入应使用 PostgreSQL，并按本文档执行备份和迁移。
5. 服务默认假设 TLS 在反向代理/Gateway 终止；公网部署必须禁止明文直连、限制 CORS、保护 `/metrics` 和调试端点。

## 上线前检查

- `server.environment=production`，关闭 `dev_admin_enabled`，配置至少 32 字符 JWT secret 和 bcrypt 用户密码。
- Redis 使用密码和持久化；数据库、配置密钥、对象数据均有备份和恢复演练。
- Prometheus/Alertmanager 已接入并验证健康、错误率、队列和存储告警。
- 使用 `docs/benchmark.md` 的命令在目标机器做一次代表性压测，不把本机基线当作容量承诺。