package dashboard

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// Handler serves the dashboard API. The store holds the polled snapshots.
type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// Routes returns the http mux with all /api routes registered.
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", h.handleHealth)
	mux.HandleFunc("/api/overview", h.handleOverview)
	mux.HandleFunc("/api/viberayd/configs", h.handleViberaydConfigs)
	mux.HandleFunc("/api/viberayd/urls", h.handleViberaydURLs)
	mux.HandleFunc("/api/viberoxy/metrics", h.handleViberoxyMetrics)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write json", "error", err)
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "ok",
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handler) handleOverview(w http.ResponseWriter, r *http.Request) {
	snap := h.store.Get()

	vd := map[string]interface{}{
		"reachable": snap.ViberaydUp,
	}
	if snap.Viberayd != nil {
		vd["stats"] = snap.Viberayd
	}
	if snap.SubURL != "" {
		vd["sub_url"] = snap.SubURL
	}

	vx := map[string]interface{}{
		"reachable": snap.ViberoxyUp,
	}
	if snap.Viberoxy != nil {
		vx["wans"] = map[string]interface{}{
			"active": snap.Viberoxy.WansActive,
			"slots":  snap.Viberoxy.Slots,
		}
		vx["proxy"] = snap.Viberoxy.Proxy
		if snap.Viberoxy.BuildVersion != "" {
			vx["build_info"] = map[string]string{"version": snap.Viberoxy.BuildVersion}
		}
	}
	vx["health"] = map[string]string{
		"healthz": snap.ViberoxyHealth,
		"readyz":  snap.ViberoxyReady,
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"generated_at": snap.GeneratedAt.UTC().Format(time.RFC3339),
		"viberayd":     vd,
		"viberoxy":     vx,
	})
}

func (h *Handler) handleViberaydConfigs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	state := r.URL.Query().Get("state")

	p, err := h.store.FetchConfigs(r.Context(), page, perPage, state)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error":   "viberayd unreachable",
			"details": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) handleViberaydURLs(w http.ResponseWriter, r *http.Request) {
	urls, err := h.store.FetchURLs(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error":   "viberayd unreachable",
			"details": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"urls": urls})
}

func (h *Handler) handleViberoxyMetrics(w http.ResponseWriter, r *http.Request) {
	snap := h.store.Get()
	if !snap.ViberoxyUp || snap.Viberoxy == nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "viberoxy unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, snap.Viberoxy)
}
