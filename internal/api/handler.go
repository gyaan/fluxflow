package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/gyaneshwarpardhi/ifttt/internal/config"
	"github.com/gyaneshwarpardhi/ifttt/internal/dag"
	"github.com/gyaneshwarpardhi/ifttt/internal/engine"
	"github.com/gyaneshwarpardhi/ifttt/internal/event"
	"github.com/gyaneshwarpardhi/ifttt/internal/metrics"
)

const maxBatchSize = 100

// Handler holds all HTTP handler dependencies.
type Handler struct {
	eng    *engine.Engine
	loader *config.Loader
	mux    *http.ServeMux
}

// New creates an HTTP handler and registers all routes.
func New(eng *engine.Engine, loader *config.Loader) http.Handler {
	h := &Handler{eng: eng, loader: loader, mux: http.NewServeMux()}

	h.mux.HandleFunc("POST /v1/events", h.ingestEvent)
	h.mux.HandleFunc("POST /v1/events/batch", h.ingestBatch)
	h.mux.HandleFunc("GET /v1/rules", h.listRules)
	h.mux.HandleFunc("POST /v1/rules/reload", h.reloadRules)
	h.mux.HandleFunc("GET /healthz", h.healthz)
	h.mux.HandleFunc("GET /readyz", h.readyz)
	h.mux.Handle("GET /metrics", promhttp.Handler())

	return loggingMiddleware(h.mux)
}

// POST /v1/events — synchronous single-event ingestion.
func (h *Handler) ingestEvent(w http.ResponseWriter, r *http.Request) {
	var ev event.Event
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %s", err))
		return
	}
	if ev.ID == "" {
		ev.ID = uuid.New().String()
	}
	if ev.Type == "" {
		writeError(w, http.StatusBadRequest, "event type is required")
		return
	}
	ev.ReceivedAt = time.Now()

	res, err := h.eng.ProcessSync(r.Context(), &ev)
	if err != nil {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	metrics.EventProcessingDuration.Observe(float64(res.DurationMs))
	writeJSON(w, http.StatusOK, res)
}

// POST /v1/events/batch — async batch ingestion (up to 100 events).
func (h *Handler) ingestBatch(w http.ResponseWriter, r *http.Request) {
	var events []*event.Event
	if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %s", err))
		return
	}
	if len(events) == 0 {
		writeError(w, http.StatusBadRequest, "batch must contain at least one event")
		return
	}
	if len(events) > maxBatchSize {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("batch size %d exceeds max %d", len(events), maxBatchSize))
		return
	}

	now := time.Now()
	jobID := uuid.New().String()
	queued := 0
	for _, ev := range events {
		if ev.ID == "" {
			ev.ID = uuid.New().String()
		}
		ev.ReceivedAt = now
		if h.eng.ProcessAsync(ev) {
			queued++
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"job_id":   jobID,
		"total":    len(events),
		"queued":   queued,
		"rejected": len(events) - queued,
	})
}

// GET /v1/rules — list loaded scenarios.
func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	cfg := h.loader.Config()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"version":   cfg.Version,
		"scenarios": cfg.Scenarios,
	})
}

// POST /v1/rules/reload — hot-reload rules from disk.
func (h *Handler) reloadRules(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.loader.Reload()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Rebuild and swap the DAG.
	g, err := dag.Build(cfg)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.eng.SwapGraph(g)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"reloaded":        true,
		"scenarios_count": len(cfg.Scenarios),
	})
}

// GET /healthz — always 200 (liveness probe).
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /readyz — 503 if event queue >80% full.
func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	util := h.eng.QueueUtilization()
	metrics.QueueUtilization.Set(util)
	if util > 0.8 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status":            "overloaded",
			"queue_utilization": util,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":            "ready",
		"queue_utilization": util,
	})
}
