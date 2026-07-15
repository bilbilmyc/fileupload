package transport

import (
	"net/http"
	"strconv"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// RESTHandler 自定义 REST 分片上传处理器
type RESTHandler struct {
	uploadSvc           *domain.UploadService
	downloadSvc         *domain.DownloadService
	namespaceQuotaBytes int64
}

// NewRESTHandler 创建 REST 处理器
func NewRESTHandler(uploadSvc *domain.UploadService, downloadSvc *domain.DownloadService) *RESTHandler {
	return &RESTHandler{uploadSvc: uploadSvc, downloadSvc: downloadSvc}
}

// WithNamespaceQuota exposes the active server-side quota to the settings UI.
func (h *RESTHandler) WithNamespaceQuota(bytes int64) *RESTHandler {
	h.namespaceQuotaBytes = bytes
	return h
}

// InitUpload POST /v1/uploads/init
func (h *RESTHandler) InitUpload(w http.ResponseWriter, r *http.Request) {
	in, err := parseRestInit(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	namespace := GetNamespace(r.Context())

	session, err := h.uploadSvc.CreateSession(r.Context(), in.sha256, in.uploadLength,
		in.compression, in.chunkSize, namespace, in.fileName)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"session_id": session.SessionID,
		"chunk_size": session.ChunkSize,
	})
}

// UploadChunk PUT /v1/uploads/{id}/chunks/{index}
func (h *RESTHandler) UploadChunk(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	indexStr := r.PathValue("index")
	if sessionID == "" || indexStr == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	index, err := strconv.Atoi(indexStr)
	if err != nil || index < 0 {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	sliceSha256 := r.Header.Get("X-Slice-SHA256")
	namespace := GetNamespace(r.Context())

	// 绝对上限由服务层按协商 chunk_size 再次校验；这里提前拒绝畸形请求。
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)

	err = h.uploadSvc.AppendChunkForNamespace(r.Context(), sessionID, namespace, index, r.Body, sliceSha256)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetUploadStatus GET /v1/uploads/{id}/status
func (h *RESTHandler) GetUploadStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	namespace := GetNamespace(r.Context())
	chunks, total, err := h.uploadSvc.GetStatusForNamespace(r.Context(), sessionID, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"chunks":     chunks,
		"total":      total,
	})
}

// FinalizeUpload POST /v1/uploads/{id}/finalize
func (h *RESTHandler) FinalizeUpload(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	namespace := GetNamespace(r.Context())
	result, err := h.uploadSvc.FinalizeForNamespace(r.Context(), sessionID, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"file_id": result.FileID,
		"sha256":  result.SHA256,
		"size":    result.Size,
		"name":    result.Name,
	})
}

// SubmitDir POST /v1/dirs
func (h *RESTHandler) SubmitDir(w http.ResponseWriter, r *http.Request) {
	var manifest domain.DirManifest
	if err := decodeJSON(r.Body, &manifest); err != nil {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	namespace := GetNamespace(r.Context())

	dir, err := h.uploadSvc.SubmitDir(r.Context(), manifest, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{
		"file_id": dir.FileID,
	})
}

// DeleteFile DELETE /v1/files/{id}
func (h *RESTHandler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("id")
	if fileID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	namespace := GetNamespace(r.Context())

	if err := h.uploadSvc.Trash(r.Context(), fileID, namespace); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListDir GET /v1/ls
func (h *RESTHandler) ListDir(w http.ResponseWriter, r *http.Request) {
	parentID := r.URL.Query().Get("parent")
	search := r.URL.Query().Get("search")
	namespace := GetNamespace(r.Context())

	// 分页参数
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 {
		perPage = 50
	}
	if perPage > 200 {
		perPage = 200
	}
	sortBy := r.URL.Query().Get("sort_by")
	if sortBy == "" {
		sortBy = "name"
	}
	sortOrder := r.URL.Query().Get("sort_order")
	if sortOrder == "" {
		sortOrder = "asc"
	}

	dir, children, total, err := h.downloadSvc.ListDirPage(r.Context(), parentID, namespace, search, page, perPage, sortBy, sortOrder)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	// 递归计算子目录的总文件大小
	for i, child := range children {
		if child.IsDir {
			if size, err := h.downloadSvc.GetDirTotalSize(r.Context(), child.FileID); err == nil {
				children[i].Size = size
			}
		}
	}

	// 获取祖先链（面包屑导航用）
	var ancestors []*domain.FileMetadata
	if dir != nil && dir.ParentID != "" {
		ancestors, _ = h.downloadSvc.GetAncestors(r.Context(), dir.ParentID)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"dir":       dir,
		"children":  children,
		"ancestors": ancestors,
		"total":     total,
		"page":      page,
		"per_page":  perPage,
	})
}

// ListTrash GET /v1/trash — 列出当前命名空间的回收站内容。
func (h *RESTHandler) ListTrash(w http.ResponseWriter, r *http.Request) {
	items, err := h.uploadSvc.ListTrash(r.Context(), GetNamespace(r.Context()))
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	if items == nil {
		items = make([]*domain.FileMetadata, 0)
	}
	respondJSON(w, http.StatusOK, map[string]any{"items": items})
}

// RestoreTrash POST /v1/trash/{id}/restore — 恢复文件或整棵目录树。
func (h *RESTHandler) RestoreTrash(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	if err := h.uploadSvc.RestoreFromTrash(r.Context(), id, GetNamespace(r.Context())); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

// PurgeTrash DELETE /v1/trash/{id} — 彻底删除。调用者必须在 UI 中进行二次确认。
func (h *RESTHandler) PurgeTrash(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	if err := h.uploadSvc.PurgeFromTrash(r.Context(), id, GetNamespace(r.Context())); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetNamespaceUsage GET /v1/usage — 当前命名空间的逻辑容量统计。
func (h *RESTHandler) GetNamespaceUsage(w http.ResponseWriter, r *http.Request) {
	usage, err := h.downloadSvc.GetNamespaceUsage(r.Context(), GetNamespace(r.Context()))
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"file_count": usage.FileCount, "total_size": usage.TotalSize, "quota_bytes": h.namespaceQuotaBytes})
}

// RenameFile PATCH /v1/files/{id} — 重命名文件/目录
func (h *RESTHandler) RenameFile(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("id")
	if fileID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r.Body, &req); err != nil || req.Name == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	namespace := GetNamespace(r.Context())
	if err := h.uploadSvc.Rename(r.Context(), fileID, req.Name, req.Name, namespace); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// StatFile GET /v1/stat/{id}
func (h *RESTHandler) StatFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	namespace := GetNamespace(r.Context())

	file, blob, err := h.downloadSvc.Stat(r.Context(), id, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"file": file,
		"blob": blob,
	})
}
