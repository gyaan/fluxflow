package dag_test

import (
	"testing"
	"time"

	"github.com/gyaneshwarpardhi/ifttt/internal/config"
	"github.com/gyaneshwarpardhi/ifttt/internal/dag"
	"github.com/gyaneshwarpardhi/ifttt/internal/event"
)

func makeEvent(typ, source string, payload map[string]interface{}) *event.Event {
	return &event.Event{
		ID:         "test-evt",
		Type:       typ,
		Source:     source,
		ActorID:    "user_42",
		OccurredAt: time.Now(),
		ReceivedAt: time.Now(),
		Payload:    payload,
	}
}

func buildTestGraph(t *testing.T) *dag.Graph {
	t.Helper()
	cfg := &config.RuleConfig{
		Version: "v1",
		Scenarios: []config.Scenario{
			{
				ID:         "sc_food_high",
				Enabled:    true,
				EventTypes: []string{"transaction"},
				Sources:    []string{"pos-system"},
				Children: []config.NodeRef{
					{Condition: &config.ConditionDef{
						ID:         "cond_food",
						Expression: `payload.category == "food"`,
						Children: []config.NodeRef{
							{Condition: &config.ConditionDef{
								ID:         "cond_amount",
								Expression: "payload.amount > 1000",
								Children: []config.NodeRef{
									{Action: &config.ActionDef{
										ID:   "act_bonus",
										Type: "reward_points",
										Params: map[string]interface{}{
											"operation": "award",
											"points":    float64(100),
										},
									}},
								},
							}},
						},
					}},
				},
			},
			{
				ID:         "sc_login",
				Enabled:    true,
				EventTypes: []string{"login"},
				Children: []config.NodeRef{
					{Action: &config.ActionDef{
						ID:   "act_welcome",
						Type: "reward_points",
						Params: map[string]interface{}{
							"operation": "award",
							"points":    float64(50),
						},
					}},
				},
			},
		},
	}
	g, err := dag.Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	return g
}

func TestEvaluate_ScenarioMatch(t *testing.T) {
	g := buildTestGraph(t)

	ev := makeEvent("transaction", "pos-system", map[string]interface{}{
		"amount":   float64(1500),
		"category": "food",
	})
	actions, scenarios, err := dag.Evaluate(g, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scenarios) != 1 || scenarios[0] != "sc_food_high" {
		t.Errorf("expected [sc_food_high], got %v", scenarios)
	}
	if len(actions) != 1 || actions[0].Node.ID() != "act_bonus" {
		t.Errorf("expected act_bonus, got %v", actions)
	}
}

func TestEvaluate_ConditionPrune(t *testing.T) {
	g := buildTestGraph(t)

	// amount < 1000 → cond_amount fails → act_bonus should NOT be triggered
	ev := makeEvent("transaction", "pos-system", map[string]interface{}{
		"amount":   float64(500),
		"category": "food",
	})
	actions, scenarios, err := dag.Evaluate(g, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scenarios) != 0 {
		t.Errorf("expected no scenarios, got %v", scenarios)
	}
	if len(actions) != 0 {
		t.Errorf("expected no actions, got %v", actions)
	}
}

func TestEvaluate_WrongEventType(t *testing.T) {
	g := buildTestGraph(t)

	ev := makeEvent("login", "", nil)
	actions, scenarios, err := dag.Evaluate(g, ev)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scenarios) != 1 || scenarios[0] != "sc_login" {
		t.Errorf("expected [sc_login], got %v", scenarios)
	}
	if len(actions) != 1 || actions[0].Node.ID() != "act_welcome" {
		t.Errorf("expected act_welcome, got %v", actions)
	}
}

func TestEvaluate_WrongSource(t *testing.T) {
	g := buildTestGraph(t)

	// Source "mobile-app" is not in sc_food_high sources list → no match
	ev := makeEvent("transaction", "mobile-app", map[string]interface{}{
		"amount":   float64(1500),
		"category": "food",
	})
	_, scenarios, _ := dag.Evaluate(g, ev)
	for _, s := range scenarios {
		if s == "sc_food_high" {
			t.Errorf("sc_food_high should not match source mobile-app")
		}
	}
}

func TestEvaluate_DisabledScenario(t *testing.T) {
	cfg := &config.RuleConfig{
		Version: "v1",
		Scenarios: []config.Scenario{
			{
				ID:         "sc_disabled",
				Enabled:    false, // disabled
				EventTypes: []string{"transaction"},
				Children: []config.NodeRef{
					{Action: &config.ActionDef{
						ID:   "act_never",
						Type: "reward_points",
						Params: map[string]interface{}{
							"operation": "award",
							"points":    float64(99),
						},
					}},
				},
			},
		},
	}
	g, err := dag.Build(cfg)
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	ev := makeEvent("transaction", "", map[string]interface{}{})
	_, scenarios, _ := dag.Evaluate(g, ev)
	if len(scenarios) != 0 {
		t.Errorf("disabled scenario should not match, got %v", scenarios)
	}
}
