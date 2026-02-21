package event

import "time"

// Event is the canonical input model for all incoming events.
type Event struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`        // "transaction", "login", etc.
	OccurredAt time.Time              `json:"occurred_at"`
	ReceivedAt time.Time              `json:"-"`
	Source     string                 `json:"source"`
	ActorID    string                 `json:"actor_id"`
	Payload    map[string]interface{} `json:"payload"` // arbitrary event data
	Meta       map[string]string      `json:"meta"`    // tenant, region, etc.
}
