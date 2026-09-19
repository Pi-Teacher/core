package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Pi-Teacher/server/internal/application/apperr"
)

// settingsResponse 是通用设置端点响应体.
type settingsResponse struct {
	Settings map[string]any `json:"settings"`
}

// handleGetSettings 实现 GET /api/web/settings.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil {
		writeJSON(w, http.StatusOK, settingsResponse{Settings: map[string]any{}})
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse{Settings: s.Settings.Values()})
}

// handlePatchSettings 实现 PATCH /api/web/settings.
// 请求体是范围内 key 的 JSON 对象子集, 值类型必须与登记类型一致.
func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	if s.Settings == nil {
		writeError(w, apperr.New(apperr.CodeInternal, "设置服务未启用"))
		return
	}
	var raw map[string]json.RawMessage
	if err := decodeJSON(w, r, &raw); err != nil {
		writeError(w, err)
		return
	}
	if err := s.Settings.Patch(r.Context(), raw); err != nil {
		writeError(w, err)
		return
	}
	s.logEntityEvent(r, "settings_updated", "settings", 0)
	writeJSON(w, http.StatusOK, settingsResponse{Settings: s.Settings.Values()})
}

// trimSpace 去除首尾空白.
func trimSpace(s string) string { return strings.TrimSpace(s) }
