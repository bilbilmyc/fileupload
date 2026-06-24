// Package metrics 集中管理 Prometheus 指标。
//
// 所有指标在 init() 中注册到默认 Registerer。Promhttp 端点（/metrics）
// 通过 promhttp.Handler() 自动采集。
//
// 指标命名约定：
//   fileupload_<领域>_<量>_<单位>_<labels>
//   例：fileupload_uploads_total{result="success"}
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Result label 取值（用于 Counter / Gauge 的 result 标签）
const (
	ResultSuccess = "success"
	ResultFailed  = "failed"
)

var (
	// UploadsTotal 上传创建会话总数（按 result 区分成功/失败）
	UploadsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fileupload_uploads_total",
			Help: "Total upload sessions created.",
		},
		[]string{"result"},
	)

	// UploadBytesTotal 上传字节总数（成功上传的字节）
	UploadBytesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "fileupload_upload_bytes_total",
			Help: "Total bytes uploaded successfully.",
		},
	)

	// DownloadsTotal 下载总数
	DownloadsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fileupload_downloads_total",
			Help: "Total file/dir downloads.",
		},
		[]string{"kind", "result"}, // kind: file|dir|batch
	)

	// BatchOpsTotal 批量操作总数（按 operation + result 区分）
	BatchOpsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fileupload_batch_operations_total",
			Help: "Total batch operations invoked.",
		},
		[]string{"operation", "result"}, // operation: copy|move|delete|tag|download
	)

	// BatchOpItems 批量操作中处理的条目数（按 operation + item_result 区分）
	BatchOpItems = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fileupload_batch_operation_items_total",
			Help: "Total items processed by batch operations.",
		},
		[]string{"operation", "item_result"}, // item_result: success|failed|skipped
	)

	// ReaperCleanupsTotal Reaper 清理总数（按 kind 区分过期/孤儿）
	ReaperCleanupsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fileupload_reaper_cleanups_total",
			Help: "Total items cleaned by the session reaper.",
		},
		[]string{"kind"}, // kind: expired_session|orphan_part
	)

	// HealthStatus 各组件健康状态（1=ok, 0=error）
	HealthStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "fileupload_health_status",
			Help: "Health status of components (1=ok, 0=error).",
		},
		[]string{"component"}, // component: storage|metadata
	)
)