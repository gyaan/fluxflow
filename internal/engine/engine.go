package engine

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gyaneshwarpardhi/ifttt/internal/action"
	"github.com/gyaneshwarpardhi/ifttt/internal/config"
	"github.com/gyaneshwarpardhi/ifttt/internal/dag"
	"github.com/gyaneshwarpardhi/ifttt/internal/event"
	"github.com/gyaneshwarpardhi/ifttt/internal/metrics"
)

// EventResult is the outcome of processing a single event.
type EventResult struct {
	EventID          string                `json:"event_id"`
	DurationMs       int64                 `json:"duration_ms"`
	ScenariosMatched []string              `json:"scenarios_matched"`
	ActionsExecuted  []*action.ActionResult `json:"actions_executed"`
	Error            string                `json:"error,omitempty"`
}

// Engine processes events through the DAG.
type Engine struct {
	graph      atomic.Pointer[dag.Graph]
	registry   *action.Registry
	eventPool  *workerPool[*eventWork, *EventResult]
	actionPool *workerPool[*actionWork, *action.ActionResult]
	conf       *config.EngineConf
}

type eventWork struct {
	ev      *event.Event
	resultC chan *EventResult
}

type actionWork struct {
	ctx      context.Context
	match    dag.ActionMatch
	evalCtx  *dag.EvalContext
	registry *action.Registry
	resultC  chan *action.ActionResult
}

// New creates an Engine using conf and starts worker pools.
func New(ctx context.Context, g *dag.Graph, reg *action.Registry, conf config.EngineConf) *Engine {
	e := &Engine{
		registry: reg,
		conf:     &conf,
	}
	e.graph.Store(g)

	// Start action pool first so event workers can submit to it.
	e.actionPool = newWorkerPool[*actionWork, *action.ActionResult](
		ctx,
		conf.ActionWorkers,
		conf.ActionWorkers*10,
		func(ctx context.Context, w *actionWork) (*action.ActionResult, error) {
			return e.executeAction(ctx, w)
		},
	)

	e.eventPool = newWorkerPool[*eventWork, *EventResult](
		ctx,
		conf.EventWorkers,
		conf.QueueDepth,
		func(ctx context.Context, w *eventWork) (*EventResult, error) {
			res := e.processEvent(ctx, w.ev)
			if w.resultC != nil {
				w.resultC <- res
			}
			return res, nil
		},
	)

	return e
}

// SwapGraph atomically replaces the DAG (used on hot-reload).
func (e *Engine) SwapGraph(g *dag.Graph) {
	e.graph.Store(g)
}

// ProcessSync processes an event synchronously and returns the result.
// Returns 429 error if the queue is full.
func (e *Engine) ProcessSync(ctx context.Context, ev *event.Event) (*EventResult, error) {
	resultC := make(chan *EventResult, 1)
	w := &eventWork{ev: ev, resultC: resultC}

	timeout := time.Duration(e.conf.EventTimeoutMs) * time.Millisecond
	if !e.eventPool.Submit(w) {
		metrics.EventsDropped.Inc()
		return nil, fmt.Errorf("event queue full (capacity %d)", e.conf.QueueDepth)
	}
	metrics.EventsEnqueued.Inc()

	select {
	case res := <-resultC:
		return res, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("event processing timeout after %v", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ProcessAsync enqueues an event for background processing. Returns false if the queue is full.
func (e *Engine) ProcessAsync(ev *event.Event) bool {
	w := &eventWork{ev: ev}
	if !e.eventPool.Submit(w) {
		metrics.EventsDropped.Inc()
		return false
	}
	metrics.EventsEnqueued.Inc()
	return true
}

// QueueUtilization returns queue used / capacity (0–1).
func (e *Engine) QueueUtilization() float64 {
	if e.eventPool.QueueCap() == 0 {
		return 0
	}
	return float64(e.eventPool.QueueLen()) / float64(e.eventPool.QueueCap())
}

func (e *Engine) processEvent(ctx context.Context, ev *event.Event) *EventResult {
	start := time.Now()
	g := e.graph.Load()

	matches, scenariosMatched, _ := dag.Evaluate(g, ev)

	result := &EventResult{
		EventID:          ev.ID,
		ScenariosMatched: scenariosMatched,
		ActionsExecuted:  make([]*action.ActionResult, 0, len(matches)),
	}

	if len(matches) > 0 {
		evalCtx := &dag.EvalContext{
			Event:   ev,
			Results: make(map[string]interface{}),
		}
		// Execute actions synchronously within the event worker.
		for _, m := range matches {
			ar := e.runAction(ctx, m, evalCtx)
			result.ActionsExecuted = append(result.ActionsExecuted, ar)
		}
	}

	result.DurationMs = time.Since(start).Milliseconds()

	// Metrics.
	metrics.EventsProcessed.Inc()
	for _, sc := range scenariosMatched {
		metrics.ScenariosMatched.WithLabelValues(sc).Inc()
	}

	return result
}

func (e *Engine) runAction(ctx context.Context, m dag.ActionMatch, evalCtx *dag.EvalContext) *action.ActionResult {
	exec, err := e.registry.Get(m.Node.ActionType())
	if err != nil {
		metrics.ActionsExecuted.WithLabelValues(m.Node.ActionType(), "error").Inc()
		return &action.ActionResult{
			ActionID: m.Node.ID(),
			Type:     m.Node.ActionType(),
			Success:  false,
			Message:  err.Error(),
		}
	}
	res, err := exec.Execute(ctx, m.Node.ID(), m.Node.Params(), evalCtx)
	if err != nil {
		metrics.ActionsExecuted.WithLabelValues(m.Node.ActionType(), "error").Inc()
		if res == nil {
			res = &action.ActionResult{
				ActionID: m.Node.ID(),
				Type:     m.Node.ActionType(),
				Success:  false,
				Message:  err.Error(),
			}
		}
		return res
	}
	status := "success"
	if !res.Success {
		status = "error"
	}
	metrics.ActionsExecuted.WithLabelValues(m.Node.ActionType(), status).Inc()
	return res
}

func (e *Engine) executeAction(ctx context.Context, w *actionWork) (*action.ActionResult, error) {
	exec, err := w.registry.Get(w.match.Node.ActionType())
	if err != nil {
		return nil, err
	}
	return exec.Execute(ctx, w.match.Node.ID(), w.match.Node.Params(), w.evalCtx)
}

// Shutdown drains both pools gracefully.
func (e *Engine) Shutdown() {
	e.eventPool.Drain()
	e.actionPool.Drain()
}
