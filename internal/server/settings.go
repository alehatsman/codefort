package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

// handleGetAgentSettings reports the operator-facing agent config: whether a
// global Claude token is configured (write-only — never the token itself) and
// the default execution model.
func (s *Server) handleGetAgentSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.agentSettings())
}

// handleUpdateAgentSettings sets, clears, or leaves the global agent Claude
// token and default execution model. For each field a nil pointer leaves it
// unchanged; "" clears it; any other value sets it (the model is validated).
func (s *Server) handleUpdateAgentSettings(w http.ResponseWriter, r *http.Request) {
	var req api.UpdateAgentSettingsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ClaudeToken != nil {
		if err := s.setOrClearSetting(storage.SettingAgentClaudeToken, strings.TrimSpace(*req.ClaudeToken)); err != nil {
			s.logger.Error("agent settings update token", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if req.ExecutionModel != nil {
		v := strings.TrimSpace(*req.ExecutionModel)
		if v != "" && !storage.ValidExecutionModel(v) {
			writeError(w, http.StatusBadRequest, "invalid execution model")
			return
		}
		if err := s.setOrClearSetting(storage.SettingAgentExecutionModel, v); err != nil {
			s.logger.Error("agent settings update model", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	writeJSON(w, http.StatusOK, s.agentSettings())
}

// setOrClearSetting deletes a setting when value is empty, else sets it.
func (s *Server) setOrClearSetting(key, value string) error {
	if value == "" {
		return storage.DeleteSetting(s.db, key)
	}
	return storage.SetSetting(s.db, key, value)
}

func (s *Server) agentSettings() api.AgentSettings {
	return api.AgentSettings{
		ClaudeTokenSet: s.agentClaudeTokenSet(),
		ExecutionModel: storage.SettingValue(s.rdb, storage.SettingAgentExecutionModel),
	}
}

func (s *Server) agentClaudeTokenSet() bool {
	_, err := storage.GetSetting(s.rdb, storage.SettingAgentClaudeToken)
	return !errors.Is(err, storage.ErrNotFound) && err == nil
}
