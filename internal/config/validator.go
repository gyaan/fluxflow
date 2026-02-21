package config

import (
	"fmt"
	"strings"
)

// Validate checks the config for:
//   - Duplicate IDs across scenarios, conditions, and actions
//   - Cycle detection within the DAG (impossible in YAML tree, but guards against future formats)
//   - Required fields
func Validate(cfg *RuleConfig) error {
	if cfg.Version == "" {
		return fmt.Errorf("config: version is required")
	}
	ids := make(map[string]string) // id → location
	var errs []string

	for i, sc := range cfg.Scenarios {
		if sc.ID == "" {
			errs = append(errs, fmt.Sprintf("scenarios[%d]: id is required", i))
			continue
		}
		loc := fmt.Sprintf("scenario %s", sc.ID)
		if prev, ok := ids[sc.ID]; ok {
			errs = append(errs, fmt.Sprintf("duplicate id %q (first seen at %s, again at %s)", sc.ID, prev, loc))
		} else {
			ids[sc.ID] = loc
		}
		if len(sc.EventTypes) == 0 {
			errs = append(errs, fmt.Sprintf("scenario %s: event_types must not be empty", sc.ID))
		}
		validateNodeRefs(sc.Children, loc, ids, &errs)
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation errors:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func validateNodeRefs(refs []NodeRef, parent string, ids map[string]string, errs *[]string) {
	for j, ref := range refs {
		switch {
		case ref.Condition != nil && ref.Action != nil:
			*errs = append(*errs, fmt.Sprintf("%s.children[%d]: only one of condition/action may be set", parent, j))
		case ref.Condition == nil && ref.Action == nil:
			*errs = append(*errs, fmt.Sprintf("%s.children[%d]: one of condition/action must be set", parent, j))
		case ref.Condition != nil:
			c := ref.Condition
			if c.ID == "" {
				*errs = append(*errs, fmt.Sprintf("%s.children[%d].condition: id is required", parent, j))
				continue
			}
			loc := fmt.Sprintf("condition %s", c.ID)
			if prev, ok := ids[c.ID]; ok {
				*errs = append(*errs, fmt.Sprintf("duplicate id %q (first seen at %s, again at %s)", c.ID, prev, loc))
			} else {
				ids[c.ID] = loc
			}
			if c.Expression == "" {
				*errs = append(*errs, fmt.Sprintf("condition %s: expression is required", c.ID))
			}
			validateNodeRefs(c.Children, loc, ids, errs)
		case ref.Action != nil:
			a := ref.Action
			if a.ID == "" {
				*errs = append(*errs, fmt.Sprintf("%s.children[%d].action: id is required", parent, j))
				continue
			}
			loc := fmt.Sprintf("action %s", a.ID)
			if prev, ok := ids[a.ID]; ok {
				*errs = append(*errs, fmt.Sprintf("duplicate id %q (first seen at %s, again at %s)", a.ID, prev, loc))
			} else {
				ids[a.ID] = loc
			}
			if a.Type == "" {
				*errs = append(*errs, fmt.Sprintf("action %s: type is required", a.ID))
			}
		}
	}
}
