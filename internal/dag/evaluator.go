package dag

import (
	"fmt"

	"github.com/gyaneshwarpardhi/ifttt/internal/event"
)

// ActionMatch records a triggered action during DFS traversal.
type ActionMatch struct {
	ScenarioID string
	Node       *ActionNode
}

// Evaluate runs DFS over the graph for the given event and returns matched actions.
func Evaluate(g *Graph, ev *event.Event) ([]ActionMatch, []string, error) {
	ctx := &EvalContext{
		Event:   ev,
		Results: make(map[string]interface{}),
	}

	var matches []ActionMatch
	var scenariosMatched []string
	var evalErr error

	for _, root := range g.Roots() {
		ok, err := root.Evaluate(ctx)
		if err != nil {
			ctx.Errors = append(ctx.Errors, fmt.Errorf("scenario %s: %w", root.ID(), err))
			continue
		}
		if !ok {
			continue
		}
		// DFS from this scenario's children.
		actions, err := dfs(g, ctx, root.ID(), root.ID())
		if err != nil {
			ctx.Errors = append(ctx.Errors, err)
			continue
		}
		if len(actions) > 0 {
			scenariosMatched = append(scenariosMatched, root.ID())
			matches = append(matches, actions...)
		}
	}

	if len(ctx.Errors) > 0 {
		evalErr = ctx.Errors[0] // surface first error; all are in ctx
	}
	return matches, scenariosMatched, evalErr
}

// dfs does a depth-first traversal with early branch pruning.
// Returns all ActionNodes reachable from parentID whose entire ancestor chain passed.
func dfs(g *Graph, ctx *EvalContext, parentID, scenarioID string) ([]ActionMatch, error) {
	var results []ActionMatch
	for _, child := range g.Children(parentID) {
		ok, err := child.Evaluate(ctx)
		if err != nil {
			ctx.Errors = append(ctx.Errors, fmt.Errorf("node %s: %w", child.ID(), err))
			continue // fail-open: skip this branch
		}
		if !ok {
			continue // prune this branch
		}
		if an, isAction := child.(*ActionNode); isAction {
			results = append(results, ActionMatch{ScenarioID: scenarioID, Node: an})
		} else {
			sub, err := dfs(g, ctx, child.ID(), scenarioID)
			if err != nil {
				return results, err
			}
			results = append(results, sub...)
		}
	}
	return results, nil
}
