package transport

import (
	"io"
	"net/http"
	"strings"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// BatchHandler 批量操作 HTTP 处理器
// 使用表驱动调度：POST /v1/batch/{action}
type BatchHandler struct {
	batchSvc *domain.BatchService
}

// NewBatchHandler 创建批量操作处理器
func NewBatchHandler(batchSvc *domain.BatchService) *BatchHandler {
	return &BatchHandler{batchSvc: batchSvc}
}

// batchRequest 批量操作通用请求体
type batchRequest struct {
	IDs         []string `json:"ids"`
	TargetDirID string   `json:"target_dir_id,omitempty"`
	Format      string   `json:"format,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// batchAction 表驱动调度中的一个动作
type batchAction struct {
	// validate 验证请求体字段，返回错误码和错误
	validate func(req batchRequest) error
	// execute 执行业务操作
	execute func(h *BatchHandler, w http.ResponseWriter, r *http.Request, req batchRequest) error
}

// batchActions 动作调度表
var batchActions = map[string]batchAction{
	"delete": {
		validate: func(req batchRequest) error {
			if len(req.IDs) == 0 {
				return domain.ErrInvalidArgument
			}
			return nil
		},
		execute: func(h *BatchHandler, w http.ResponseWriter, r *http.Request, req batchRequest) error {
			namespace := GetNamespace(r.Context())
			result, err := h.batchSvc.BatchDelete(r.Context(), req.IDs, namespace)
			if err != nil {
				return err
			}
			respondJSON(w, http.StatusOK, result)
			return nil
		},
	},
	"download": {
		validate: func(req batchRequest) error {
			if len(req.IDs) == 0 {
				return domain.ErrInvalidArgument
			}
			return nil
		},
		execute: func(h *BatchHandler, w http.ResponseWriter, r *http.Request, req batchRequest) error {
			format := domain.CompZip
			if req.Format != "" {
				format = domain.CompressionFormat(req.Format)
			}
			namespace := GetNamespace(r.Context())
			return h.streamDownload(w, r, req.IDs, namespace, format)
		},
	},
	"move": {
		validate: func(req batchRequest) error {
			if len(req.IDs) == 0 {
				return domain.ErrInvalidArgument
			}
			return nil
		},
		execute: func(h *BatchHandler, w http.ResponseWriter, r *http.Request, req batchRequest) error {
			namespace := GetNamespace(r.Context())
			if err := h.batchSvc.BatchMove(r.Context(), req.IDs, req.TargetDirID, namespace); err != nil {
				return err
			}
			respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return nil
		},
	},
	"copy": {
		validate: func(req batchRequest) error {
			if len(req.IDs) == 0 {
				return domain.ErrInvalidArgument
			}
			return nil
		},
		execute: func(h *BatchHandler, w http.ResponseWriter, r *http.Request, req batchRequest) error {
			namespace := GetNamespace(r.Context())
			if err := h.batchSvc.BatchCopy(r.Context(), req.IDs, req.TargetDirID, namespace); err != nil {
				return err
			}
			respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return nil
		},
	},
	"tags": {
		validate: func(req batchRequest) error {
			if len(req.IDs) == 0 {
				return domain.ErrInvalidArgument
			}
			return nil
		},
		execute: func(h *BatchHandler, w http.ResponseWriter, r *http.Request, req batchRequest) error {
			namespace := GetNamespace(r.Context())
			if err := h.batchSvc.BatchTag(r.Context(), req.IDs, req.Tags, namespace); err != nil {
				return err
			}
			respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return nil
		},
	},
}

// BatchHandle POST /v1/batch/{action}
// 表驱动调度：根据 action 参数分发到对应的处理逻辑。
func (h *BatchHandler) BatchHandle(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	act, ok := batchActions[action]
	if !ok {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	var req batchRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	if err := act.validate(req); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	if err := act.execute(h, w, r, req); err != nil {
		respondError(w, domainErrorToStatus(err), err)
	}
}

// streamDownload 流式响应批量下载（被表驱动和 GET handler 共用）
func (h *BatchHandler) streamDownload(w http.ResponseWriter, r *http.Request, ids []string, namespace string, format domain.CompressionFormat) error {
	reader, err := h.batchSvc.BatchDownload(r.Context(), ids, namespace, format)
	if err != nil {
		return err
	}
	defer reader.Close()

	filename := "batch." + string(format)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("Content-Type", contentTypeForFormat(format))

	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, reader)
	return err
}

// BatchDownloadGet GET /v1/batch/download — ids 用逗号分隔的查询参数
func (h *BatchHandler) BatchDownloadGet(w http.ResponseWriter, r *http.Request) {
	idsParam := r.URL.Query().Get("ids")
	if idsParam == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	ids := splitCSV(idsParam)
	if len(ids) == 0 {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	format := domain.CompZip
	if f := r.URL.Query().Get("format"); f != "" {
		format = domain.CompressionFormat(f)
	}

	namespace := GetNamespace(r.Context())
	if err := h.streamDownload(w, r, ids, namespace, format); err != nil {
		respondError(w, domainErrorToStatus(err), err)
	}
}

// contentTypeForFormat 返回压缩格式对应的 Content-Type
func contentTypeForFormat(format domain.CompressionFormat) string {
	switch format {
	case domain.CompZip:
		return "application/zip"
	case domain.CompTarGz:
		return "application/gzip"
	case domain.CompTarZst:
		return "application/zstd"
	default:
		return "application/octet-stream"
	}
}

// splitCSV 将逗号分隔的字符串拆分为切片
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
