package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"baryon/internal/events"
	"baryon/internal/proxy"
)

// LogEntry represents an entry in the recent routing event log.
type LogEntry struct {
	Timestamp string
	ClientIP  string
	Target    string
	IsStatus  bool
	StatusMsg string
}

// Model represents the state of the Bubble Tea TUI dashboard.
type Model struct {
	startTime       time.Time
	refreshInterval time.Duration
	backends        []*proxy.Backend
	eventBus        *events.EventBus

	// Lifecycle callbacks
	OnKillBackend    func(string) error
	OnRestartBackend func(string) error
	OnQuit           func()

	// Metrics state
	proxyStatus     string
	lastTotal       uint64
	lastTime        time.Time
	measuredRPS     float64
	totalRequests   uint64

	// Display logs (capped at 5)
	recentLogs      []LogEntry

	// User action feedback
	actionFeedback  string
	feedbackExpiry  time.Time

	// Terminal size
	Width  int
	Height int
}

// NewModel creates a new TUI model initialized with backend references and event bus.
func NewModel(backends []*proxy.Backend, eventBus *events.EventBus, refreshInterval time.Duration) Model {
	if refreshInterval <= 0 {
		refreshInterval = 100 * time.Millisecond
	}
	now := time.Now()
	return Model{
		startTime:       now,
		refreshInterval: refreshInterval,
		backends:        backends,
		eventBus:        eventBus,
		proxyStatus:     "RUNNING",
		lastTime:        now,
		recentLogs:      make([]LogEntry, 0, 5),
		Width:           80,
		Height:          24,
	}
}

// Init sets up initial subscriptions and periodic tickers.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(m.refreshInterval),
		listenRequestsCmd(m.eventBus),
		listenStatusCmd(m.eventBus),
	)
}

func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func listenRequestsCmd(eb *events.EventBus) tea.Cmd {
	if eb == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-eb.RequestChannel()
		if !ok {
			return nil
		}
		return RequestMsg(evt)
	}
}

func listenStatusCmd(eb *events.EventBus) tea.Cmd {
	if eb == nil {
		return nil
	}
	return func() tea.Msg {
		evt, ok := <-eb.StatusChannel()
		if !ok {
			return nil
		}
		return NodeStatusMsg(evt)
	}
}

