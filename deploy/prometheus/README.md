# fileupload 监控部署（P0）

本目录提供 Prometheus 抓取配置、告警规则和 Alertmanager 路由。审计日志不属于本阶段范围。

## 1. 内网部署原则

当前按“小规模内部使用”配置，Prometheus 抓取 `/metrics` 时不携带 token。请确保 `/metrics` 只在内网或受防火墙保护的网络中可访问，不要直接暴露到公网。

如果后续需要把监控端点暴露到不可信网络，再启用 fileupload 的 `server.metrics_token`，并在 Prometheus `scrape_configs` 下增加：

```yaml
http_config:
  bearer_token_file: /etc/prometheus/secrets/fileupload_metrics_token
```

注意：当前代码在 `server.environment: production` 模式下仍会要求设置至少 24 个字符的 `metrics_token`；使用无 token 的内部部署时保持开发/内部环境配置即可。

## 2. 挂载配置

`prometheus.yml` 默认假设 Prometheus 容器使用以下路径：

- `/etc/prometheus/prometheus.yml`：本文件
- `/etc/prometheus/rules/alerts.yml`：`alerts.yml`

Prometheus 与 fileupload 在同一个 Docker Compose 网络时，默认 target `server:8080` 可直接使用。若 fileupload 通过 systemd 或独立主机运行，请将 `prometheus.yml` 中的 target 改成服务所在主机的 DNS 名称或 IP，例如 `fileupload.example.internal:8080`。

## 3. 启动前检查

1. 确认 Prometheus 可以访问 fileupload 的 `server:8080/metrics`。
2. 确认 Alertmanager 地址 `alertmanager:9093` 可从 Prometheus 容器访问。
3. 按实际通知渠道修改 `alertmanager.yml` 中的 receiver；当前文件中的 PagerDuty、Slack 和 SMTP 值是占位符。
4. 将配置文件挂载到上面的容器路径。

启动后验证服务端指标：

```powershell
curl.exe http://localhost:8080/metrics
```

在 Prometheus UI 中确认：

- `up{job="fileupload"} == 1`
- `fileupload_health_status{component="storage"} == 1`
- `fileupload_health_status{component="metadata"} == 1`
- `fileupload_uploads_total`、`fileupload_downloads_total` 等业务指标有数据

## 4. 告警范围

P0 告警覆盖：

- 服务不可达：`FileUploadDown`
- 存储或元数据后端异常：`FileUploadHealthDegraded`
- 上传失败率过高：`FileUploadErrorRateHigh`
- 长时间没有上传：`FileUploadStalled`
- 批量操作失败率过高：`FileUploadBatchErrorRateHigh`
- Reaper 长时间未清理：`FileUploadReaperStalled`

告警规则默认只做检测，不会替换通知渠道。小规模内部使用可以先使用 Alertmanager 的 webhook 或邮件；正式启用前请删除占位符并验证一条测试告警。
