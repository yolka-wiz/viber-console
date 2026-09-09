package dashboard

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

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
	// ctx is the application-lifetime context. Supervised children are
	// spawned with exec.CommandContext(ctx, ...): restarting them under a
	// request-scoped (or function-scoped WithTimeout) context would SIGKILL
	// the child as soon as the surrounding function returns (context
	// canceled), and the supervisor's reaper would then refuse to
	// auto-restart it (ctx.Err() != nil).
	ctx context.Context
}

func NewControlHandler(store *Store, cfgStore *config.Store, services []*supervisor.Service, apiURL, token string, ctx context.Context) *ControlHandler {
	return &ControlHandler{
		store:    store,
		cfgStore: cfgStore,
		services: services,
		apiURL:   apiURL,
		token:    token,
		ctx:      ctx,
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
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") && strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == h.token {
		return true
	}
	if r.Header.Get("X-Console-Token") == h.token {
		return true
	}
	return false
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
	statuses := make([]supervisor.Status, 0, len(h.services))
	for _, s := range h.services {
		statuses = append(statuses, s.Status())
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"services": statuses})
}

func (h *ControlHandler) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Service string `json:"service"` // viberayd | viberoxy | all
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	// Restart under the application context, NOT r.Context(): Start()
	// spawns the child with exec.CommandContext(ctx, ...), so a
	// request-scoped ctx would kill the child the moment this handler
	// returns and permanently disable auto-restart for it. Stop() already
	// bounds its own wait (5s grace + SIGKILL) and Start() returns right
	// after spawn, so no extra timeout is needed here.
	restarted := []string{}
	for _, s := range h.services {
		if req.Service == "all" || s.Name == req.Service {
			if err := s.Restart(h.ctx); err != nil {
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
	// Application context (same reasoning as handleRestart): a
	// WithTimeout scoped to this function would SIGKILL the freshly spawned
	// child when it returns.
	var restarted []string
	for _, s := range h.services {
		if s.Name == group {
			if err := s.Restart(h.ctx); err != nil {
				slog.Warn("restart failed", "service", s.Name, "error", err)
				continue
			}
			restarted = append(restarted, s.Name)
		}
	}
	return restarted
}
