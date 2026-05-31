package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

// handleGetAgentSettings reports whether a global agent Claude token is
// configured — write-only: it never returns the token itself.
func (s *Server) handleGetAgentSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.AgentSettings{ClaudeTokenSet: s.agentClaudeTokenSet()})
}

// handleUpdateAgentSettings sets, clears, or leaves the global agent Claude
// token. nil leaves it unchanged; "" clears it; any other value sets it.
func (s *Server) handleUpdateAgentSettings(w http.ResponseWriter, r *http.Request) {
	var req api.UpdateAgentSettingsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ClaudeToken != nil {
		v := strings.TrimSpace(*req.ClaudeToken)
		var err error
		if v == "" {
			err = storage.DeleteSetting(s.db, storage.SettingAgentClaudeToken)
		} else {
			err = storage.SetSetting(s.db, storage.SettingAgentClaudeToken, v)
		}
		if err != nil {
			s.logger.Error("agent settings update", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	writeJSON(w, http.StatusOK, api.AgentSettings{ClaudeTokenSet: s.agentClaudeTokenSet()})
}

func (s *Server) agentClaudeTokenSet() bool {
	_, err := storage.GetSetting(s.rdb, storage.SettingAgentClaudeToken)
	return !errors.Is(err, storage.ErrNotFound) && err == nil
}
