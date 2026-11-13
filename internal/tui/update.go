package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Update handles incoming messages, keyboard events, and metrics calculations.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.OnQuit != nil {
				m.OnQuit()
			}
			return m, tea.Quit

		case "k", "K":
			if m.OnKillBackend != nil {
				if err := m.OnKillBackend("backend-1"); err != nil {
					m.actionFeedback = fmt.Sprintf("Error killing backend-1: %v", err)
				} else {
					m.actionFeedback = "Action: Backend-1 stopped (simulating failure)"
				}
				m.feedbackExpiry = time.Now().Add(3 * time.Second)
			}
			return m, nil

		case "r", "R":
			if m.OnRestartBackend != nil {
				if err := m.OnRestartBackend("backend-1"); err != nil {
					m.actionFeedback = fmt.Sprintf("Error restarting backend-1: %v", err)
				} else {
					m.actionFeedback = "Action: Backend-1 restarted (awaiting health recovery)"
				}
				m.feedbackExpiry = time.Now().Add(3 * time.Second)
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case TickMsg:
		now := time.Now()
		var currentTotal uint64
		for _, b := range m.backends {
			total, _ := b.GetStats()
			currentTotal += total
		}
		m.totalRequests = currentTotal

		elapsed := now.Sub(m.lastTime).Seconds()
		if elapsed > 0 {
			if currentTotal >= m.lastTotal {
				delta := currentTotal - m.lastTotal
				m.measuredRPS = float64(delta) / elapsed
			}
			m.lastTotal = currentTotal
			m.lastTime = now
		}

		if m.actionFeedback != "" && now.After(m.feedbackExpiry) {
			m.actionFeedback = ""
		}

		return m, tickCmd(m.refreshInterval)

	case RequestMsg:
		m.appendLog(LogEntry{
			Timestamp: msg.Time.Format("15:04:05"),
			ClientIP:  msg.ClientIP,
			Target:    msg.BackendID,
			IsStatus:  false,
		})
		return m, listenRequestsCmd(m.eventBus)

	case NodeStatusMsg:
		status := "ONLINE / RECOVERED"
		if !msg.Alive {
			status = "OFFLINE / FAILED"
		}
		m.appendLog(LogEntry{
			Timestamp: msg.Time.Format("15:04:05"),
			Target:    msg.BackendID,
			IsStatus:  true,
			StatusMsg: fmt.Sprintf("[%s] %s", msg.BackendID, status),
		})
		return m, listenStatusCmd(m.eventBus)
	}

	return m, nil
}

func (m *Model) appendLog(entry LogEntry) {
	if len(m.recentLogs) >= 5 {
		// Shift elements left to maintain FIFO of 5 items
		m.recentLogs = append(m.recentLogs[1:], entry)
	} else {
		m.recentLogs = append(m.recentLogs, entry)
	}
}

