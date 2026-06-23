package transport

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// ShareHandler 分享链接 HTTP 处理器
type ShareHandler struct {
	shareSvc   *domain.ShareService
	downloadSvc *domain.DownloadService
}

// NewShareHandler 创建分享处理器
func NewShareHandler(shareSvc *domain.ShareService, downloadSvc *domain.DownloadService) *ShareHandler {
	return &ShareHandler{shareSvc: shareSvc, downloadSvc: downloadSvc}
}

// CreateShare POST /v1/share
func (h *ShareHandler) CreateShare(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	namespace := GetNamespace(r.Context())
	entry, err := h.shareSvc.CreateShare(r.Context(), req, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusCreated, entry)
}

// AccessShare GET /s/{token}
func (h *ShareHandler) AccessShare(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/s/")
	if token == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	password := r.Header.Get("X-Share-Password")

	entry, err := h.shareSvc.AccessShare(r.Context(), token, password)
	if err != nil {
		if err == domain.ErrForbidden {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "密码错误", "code": "share_password_required"})
			return
		}
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	http.Redirect(w, r, "/v1/files/"+entry.FileID+"?namespace="+entry.Namespace, http.StatusFound)
}
