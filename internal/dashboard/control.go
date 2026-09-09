package dashboard

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/yolka-wiz/viber-console/internal/config"
	"github.com/yolka-wiz/viber-console/internal/supervisor"
)

// ControlHandler serves the control-plane API: config schema/values/update
// and process management.
type ControlHandler struct {
	store    *Store
	cfgStore *config.Store
	services []*supervisor.Service
	apiURL   string
	token    string // empty = localhost-only, no token required
	mode     string
}

func NewControlHandler(store *Store, cfgStore *config.Store, services []*supervisor.Service, apiURL, token string, modes ...string) *ControlHandler {
	mode := "supervise"
	if len(modes) > 0 {
		mode = modes[0]
	}
	return &ControlHandler{
		store:    store,
		cfgStore: cfgStore,
		services: services,
		apiURL:   apiURL,
		token:    token,
		mode:     mode,
	}
}

// Routes registers control endpoints on the mux.
func (h *ControlHandler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/config/schema", h.auth(h.handleSchema))
	mux.HandleFunc("/api/config/values", h.auth(h.handleValues))
	mux.HandleFunc("/api/processes", h.auth(h.handleProcesses))
	mux.HandleFunc("/api/control/restart", h.auth(h.handleRestart))
}

// auth wraps a handler with token checking. When token is empty, the
// console is considered localhost-only and no auth is enforced.
func (h *ControlHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" && !h.validToken(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (h *ControlHandler) validToken(r *http.Request) bool {
	return validAPIToken(r, h.token)
}

func (h *ControlHandler) handleSchema(w http.ResponseWriter, r *http.Request) {
	// Group the schema for the UI.
	byGroup := map[string][]config.Field{}
	for _, f := range config.Fields {
		byGroup[f.Group] = append(byGroup[f.Group], f)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"groups": byGroup})
}

func (h *ControlHandler) handleValues(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		out := map[string]map[string]string{}
		for _, group := range []string{"viberayd", "viberoxy"} {
			vals, err := h.cfgStore.Values(group)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			out[group] = vals
		}
		writeJSON(w, http.StatusOK, out)

	case http.MethodPost:
		if h.mode == "monitor" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "configuration changes are unavailable in monitor mode"})
			return
		}
		var req struct {
			Group   string            `json:"group"`
			Values  map[string]string `json:"values"`
			Restart bool              `json:"restart"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if req.Group != "viberayd" && req.Group != "viberoxy" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group must be viberayd or viberoxy"})
			return
		}

		path, err := h.cfgStore.Update(req.Group, req.Values)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		restarted := []string{}
		if req.Restart {
			restarted = h.restartGroup(req.Group)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"saved":     true,
			"path":      path,
			"restarted": restarted,
		})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (h *ControlHandler) handleProcesses(w http.ResponseWriter, r *http.Request) {
	if h.mode == "monitor" {
		snap := h.store.Get()
		statuses := []externalServiceStatus{
			externalStatus("viberayd", snap.ViberaydUp, !snap.ViberaydUp && snap.Starting),
			externalStatus("viberoxy", snap.ViberoxyUp, !snap.ViberoxyUp && snap.Starting),
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"mode": h.mode, "services": statuses})
		return
	}
	statuses := make([]supervisor.Status, 0, len(h.services))
	for _, s := range h.services {
		statuses = append(statuses, s.Status())
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"mode": h.mode, "services": statuses})
}

type externalServiceStatus struct {
	Name              string `json:"name"`
	State             string `json:"state"`
	Running           bool   `json:"running"`
	ExternallyManaged bool   `json:"externally_managed"`
	RestartAvailable  bool   `json:"restart_available"`
}

func externalStatus(name string, reachable, starting bool) externalServiceStatus {
	state := "unreachable"
	if starting {
		state = "starting"
	} else if reachable {
		state = "running"
	}
	return externalServiceStatus{
		Name:              name,
		State:             state,
		Running:           reachable,
		ExternallyManaged: true,
		RestartAvailable:  false,
	}
}

func (h *ControlHandler) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if h.mode == "monitor" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "service restart is unavailable in monitor mode"})
		return
	}
	var req struct {
		Service string `json:"service"` // viberayd | viberoxy | all
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	restarted := []string{}
	for _, s := range h.services {
		if req.Service == "all" || s.Name == req.Service {
			if err := s.Restart(ctx); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			restarted = append(restarted, s.Name)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"restarted": restarted})
}

// restartGroup restarts all services whose name matches the config group.
func (h *ControlHandler) restartGroup(group string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var restarted []string
	for _, s := range h.services {
		if s.Name == group {
			if err := s.Restart(ctx); err != nil {
				slog.Warn("restart failed", "service", s.Name, "error", err)
				continue
			}
			restarted = append(restarted, s.Name)
		}
	}
	return restarted
}
