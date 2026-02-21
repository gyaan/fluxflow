package dag

import (
	"fmt"

	"github.com/gyaneshwarpardhi/ifttt/internal/condition"
	"github.com/gyaneshwarpardhi/ifttt/internal/config"
)

// Build constructs a DAG from a validated RuleConfig.
// All expressions are compiled into ASTs here; zero parsing happens at evaluation time.
func Build(cfg *config.RuleConfig) (*Graph, error) {
	g := NewGraph()
	for _, sc := range cfg.Scenarios {
		if !sc.Enabled {
			continue
		}
		sn := NewScenarioNode(sc.ID, sc.EventTypes, sc.Sources)
		g.AddNode(sn)
		if err := buildChildren(g, sc.ID, sc.Children); err != nil {
			return nil, fmt.Errorf("scenario %s: %w", sc.ID, err)
		}
	}
	return g, nil
}

func buildChildren(g *Graph, parentID string, refs []config.NodeRef) error {
	for _, ref := range refs {
		switch {
		case ref.Condition != nil:
			c := ref.Condition
			ast, err := condition.Parse(c.Expression)
			if err != nil {
				return fmt.Errorf("condition %s: parse %q: %w", c.ID, c.Expression, err)
			}
			cn := NewConditionNode(c.ID, ast)
			g.AddNode(cn)
			g.AddEdge(parentID, cn)
			if err := buildChildren(g, c.ID, c.Children); err != nil {
				return fmt.Errorf("condition %s: %w", c.ID, err)
			}
		case ref.Action != nil:
			a := ref.Action
			an := NewActionNode(a.ID, a.Type, a.Params)
			g.AddNode(an)
			g.AddEdge(parentID, an)
			// Actions are leaves; they have no children.
		}
	}
	return nil
}
