package tui

import (
	"time"

	"baryon/internal/events"
)

// TickMsg is delivered periodically to recalculate throughput metrics and trigger UI repaints.
type TickMsg time.Time

// RequestMsg wraps an incoming RequestEvent for the Bubble Tea update loop.
type RequestMsg events.RequestEvent

// NodeStatusMsg wraps a NodeStatusEvent indicating backend state transitions.
type NodeStatusMsg events.NodeStatusEvent

// ActionFeedbackMsg displays temporary status messages in the footer (e.g. "Killing backend-1...").
type ActionFeedbackMsg struct {
	Message string
	IsError bool
}

