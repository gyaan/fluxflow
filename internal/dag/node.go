package dag

import (
	"fmt"
	"strings"

	"github.com/gyaneshwarpardhi/ifttt/internal/condition"
	"github.com/gyaneshwarpardhi/ifttt/internal/event"
)

// NodeType discriminates the three kinds of DAG nodes.
type NodeType string

const (
	NodeTypeScenario  NodeType = "scenario"
	NodeTypeCondition NodeType = "condition"
	NodeTypeAction    NodeType = "action"
)

// Node is the common interface for all DAG nodes.
type Node interface {
	ID() string
	Type() NodeType
	Evaluate(ctx *EvalContext) (bool, error)
}

// EvalContext carries per-event state through the DFS traversal.
type EvalContext struct {
	Event   *event.Event
	Results map[string]interface{}
	Errors  []error
}

// Resolve implements condition.EvalContext.
// It walks a dot-separated path into the event's fields.
func (c *EvalContext) Resolve(path []string) (interface{}, bool) {
	if len(path) == 0 {
		return nil, false
	}
	switch path[0] {
	case "payload":
		if c.Event.Payload == nil {
			return nil, false
		}
		return resolveMap(c.Event.Payload, path[1:])
	case "meta":
		if c.Event.Meta == nil {
			return nil, false
		}
		m := make(map[string]interface{}, len(c.Event.Meta))
		for k, v := range c.Event.Meta {
			m[k] = v
		}
		return resolveMap(m, path[1:])
	case "event":
		if len(path) < 2 {
			return nil, false
		}
		switch path[1] {
		case "type":
			return c.Event.Type, true
		case "source":
			return c.Event.Source, true
		case "actor_id":
			return c.Event.ActorID, true
		case "id":
			return c.Event.ID, true
		}
	}
	return nil, false
}

func resolveMap(m map[string]interface{}, path []string) (interface{}, bool) {
	if len(path) == 0 {
		return nil, false
	}
	val, ok := m[path[0]]
	if !ok {
		return nil, false
	}
	if len(path) == 1 {
		return val, true
	}
	sub, ok := val.(map[string]interface{})
	if !ok {
		return nil, false
	}
	return resolveMap(sub, path[1:])
}

// -----------------------------------------------------------------------
// ScenarioNode
// -----------------------------------------------------------------------

// ScenarioNode is the root entry point for a scenario.
// It passes when the event type and source match.
type ScenarioNode struct {
	id         string
	eventTypes map[string]struct{}
	sources    map[string]struct{} // empty = all sources allowed
}

func NewScenarioNode(id string, eventTypes, sources []string) *ScenarioNode {
	et := make(map[string]struct{}, len(eventTypes))
	for _, t := range eventTypes {
		et[strings.ToLower(t)] = struct{}{}
	}
	src := make(map[string]struct{}, len(sources))
	for _, s := range sources {
		src[strings.ToLower(s)] = struct{}{}
	}
	return &ScenarioNode{id: id, eventTypes: et, sources: src}
}

func (n *ScenarioNode) ID() string       { return n.id }
func (n *ScenarioNode) Type() NodeType   { return NodeTypeScenario }

func (n *ScenarioNode) Evaluate(ctx *EvalContext) (bool, error) {
	if _, ok := n.eventTypes[strings.ToLower(ctx.Event.Type)]; !ok {
		return false, nil
	}
	if len(n.sources) > 0 {
		if _, ok := n.sources[strings.ToLower(ctx.Event.Source)]; !ok {
			return false, nil
		}
	}
	return true, nil
}

// -----------------------------------------------------------------------
// ConditionNode
// -----------------------------------------------------------------------

// ConditionNode holds a pre-compiled expression AST.
type ConditionNode struct {
	id   string
	expr condition.Expr // compiled once at startup
}

func NewConditionNode(id string, expr condition.Expr) *ConditionNode {
	return &ConditionNode{id: id, expr: expr}
}

func (n *ConditionNode) ID() string     { return n.id }
func (n *ConditionNode) Type() NodeType { return NodeTypeCondition }

func (n *ConditionNode) Evaluate(ctx *EvalContext) (bool, error) {
	return condition.Evaluate(n.expr, ctx)
}

// -----------------------------------------------------------------------
// ActionNode
// -----------------------------------------------------------------------

// ActionNode is a leaf that holds action type and params.
// Evaluate always returns true (it is the engine's responsibility to execute).
type ActionNode struct {
	id         string
	actionType string
	params     map[string]interface{}
}

func NewActionNode(id, actionType string, params map[string]interface{}) *ActionNode {
	return &ActionNode{id: id, actionType: actionType, params: params}
}

func (n *ActionNode) ID() string         { return n.id }
func (n *ActionNode) Type() NodeType     { return NodeTypeAction }
func (n *ActionNode) ActionType() string { return n.actionType }
func (n *ActionNode) Params() map[string]interface{} { return n.params }

func (n *ActionNode) Evaluate(ctx *EvalContext) (bool, error) {
	// ActionNodes are leaves; "evaluation" just signals the engine to execute.
	if ctx.Results == nil {
		return false, fmt.Errorf("nil results map")
	}
	return true, nil
}
