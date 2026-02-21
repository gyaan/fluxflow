package config

// RuleConfig is the top-level YAML structure.
type RuleConfig struct {
	Version   string     `yaml:"version"`
	Engine    EngineConf `yaml:"engine"`
	Scenarios []Scenario `yaml:"scenarios"`
}

// EngineConf holds tunable concurrency settings.
type EngineConf struct {
	EventWorkers   int `yaml:"event_workers"`
	ActionWorkers  int `yaml:"action_workers"`
	QueueDepth     int `yaml:"queue_depth"`
	EventTimeoutMs int `yaml:"event_timeout_ms"`
	FailOpen       bool `yaml:"fail_open"`
}

// Scenario is an entry point that filters events by type and source.
type Scenario struct {
	ID          string    `yaml:"id"`
	Description string    `yaml:"description"`
	Enabled     bool      `yaml:"enabled"`
	EventTypes  []string  `yaml:"event_types"`
	Sources     []string  `yaml:"sources"` // empty = all sources
	Children    []NodeRef `yaml:"children"`
}

// NodeRef is a discriminated union: exactly one of Condition or Action is set.
type NodeRef struct {
	Condition *ConditionDef `yaml:"condition,omitempty"`
	Action    *ActionDef    `yaml:"action,omitempty"`
}

// ConditionDef holds an expression and nested children.
type ConditionDef struct {
	ID         string    `yaml:"id"`
	Expression string    `yaml:"expression"`
	Children   []NodeRef `yaml:"children"`
}

// ActionDef is a leaf node that specifies an action to execute.
type ActionDef struct {
	ID     string                 `yaml:"id"`
	Type   string                 `yaml:"type"`
	Params map[string]interface{} `yaml:"params"`
}
