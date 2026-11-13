package tui

import (
	"net/url"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"baryon/internal/events"
	"baryon/internal/proxy"
)

func createTestModel() (Model, *proxy.Backend, *proxy.Backend, *events.EventBus) {
	u1, _ := url.Parse("http://localhost:8081")
	u2, _ := url.Parse("http://localhost:8082")

	b1 := proxy.NewBackend("backend-1", u1)
	b2 := proxy.NewBackend("backend-2", u2)

	eb := events.NewEventBus(10)
	m := NewModel([]*proxy.Backend{b1, b2}, eb, 50*time.Millisecond)
	return m, b1, b2, eb
}

func TestTUIModelInit(t *testing.T) {
	m, _, _, eb := createTestModel()
	defer eb.Close()

	if m.proxyStatus != "RUNNING" {
		t.Errorf("expected RUNNING status, got %s", m.proxyStatus)
	}
	if len(m.backends) != 2 {
		t.Errorf("expected 2 backends, got %d", len(m.backends))
	}
}

func TestTUIEventLogFIFO(t *testing.T) {
	m, _, _, eb := createTestModel()
	defer eb.Close()

	// Push 7 request messages
	for i := 1; i <= 7; i++ {
		reqMsg := RequestMsg(events.RequestEvent{
			Time:      time.Now(),
			ClientIP:  "192.168.1.1",
			BackendID: "backend-1",
			Path:      "/",
		})
		updated, _ := m.Update(reqMsg)
		m = updated.(Model)
	}

	// Should be strictly capped at 5
	if len(m.recentLogs) != 5 {
		t.Fatalf("expected exactly 5 logs, got %d", len(m.recentLogs))
	}
}

func TestTUIKeyboardControls(t *testing.T) {
	m, _, _, eb := createTestModel()
	defer eb.Close()

	killed := false
	restarted := false
	quit := false

	m.OnKillBackend = func(name string) error {
		killed = true
		return nil
	}
	m.OnRestartBackend = func(name string) error {
		restarted = true
		return nil
	}
	m.OnQuit = func() {
		quit = true
	}

	// Press K
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	if !killed {
		t.Errorf("expected OnKillBackend to be called on 'k'")
	}
	if !strings.Contains(m.actionFeedback, "stopped") {
		t.Errorf("expected feedback message about stopped backend")
	}

	// Press R
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if !restarted {
		t.Errorf("expected OnRestartBackend to be called on 'r'")
	}

	// Press Q
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !quit {
		t.Errorf("expected OnQuit to be called on 'q'")
	}
	if cmd == nil {
		t.Errorf("expected tea.Quit command on 'q'")
	}
}

func TestTUIThroughputCalculation(t *testing.T) {
	m, b1, _, eb := createTestModel()
	defer eb.Close()

	// Simulate requests
	for i := 0; i < 50; i++ {
		b1.IncRequests()
	}

	// Tick after 100ms
	time.Sleep(100 * time.Millisecond)
	updated, _ := m.Update(TickMsg(time.Now()))
	m = updated.(Model)

	if m.totalRequests != 50 {
		t.Errorf("expected total requests 50, got %d", m.totalRequests)
	}
	if m.measuredRPS <= 0 {
		t.Errorf("expected positive measured RPS, got %f", m.measuredRPS)
	}
}

func TestTUIViewRendering(t *testing.T) {
	m, b1, b2, eb := createTestModel()
	defer eb.Close()

	b1.SetAlive(true)
	b2.SetAlive(false)

	viewOutput := m.View()

	if !strings.Contains(viewOutput, "BARYON") {
		t.Errorf("rendered view missing BARYON title")
	}
	if !strings.Contains(viewOutput, "backend-1") || !strings.Contains(viewOutput, "backend-2") {
		t.Errorf("rendered view missing backend names")
	}
	if !strings.Contains(viewOutput, "ONLINE") || !strings.Contains(viewOutput, "OFFLINE") {
		t.Errorf("rendered view missing ONLINE/OFFLINE badges")
	}
	if !strings.Contains(viewOutput, "[K] Kill") || !strings.Contains(viewOutput, "[R] Restart") {
		t.Errorf("rendered view missing keyboard shortcuts")
	}
}

