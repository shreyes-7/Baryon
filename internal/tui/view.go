package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Styling palettes
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00E5FF")).
			Align(lipgloss.Center)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#555555")).
			Padding(0, 1)

	sectionHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFD600"))

	statusOnlineStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#00E676"))

	statusOfflineStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FF1744"))

	barFillStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00B0FF"))

	barEmptyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#333333"))

	highlightStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	eventArrowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF9100"))

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA"))

	feedbackStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFD600"))
)

// View renders the complete BARYON terminal dashboard.
func (m Model) View() string {
	width := m.Width
	if width < 76 {
		width = 76
	}

	contentWidth := width - 4
	if contentWidth < 70 {
		contentWidth = 70
	}

	var sections []string

	// 1. Header & Overview Section
	uptime := time.Since(m.startTime).Round(time.Second)
	hours := int(uptime.Hours())
	mins := int(uptime.Minutes()) % 60
	secs := int(uptime.Seconds()) % 60
	uptimeStr := fmt.Sprintf("%02d:%02d:%02d", hours, mins, secs)

	title := titleStyle.Width(contentWidth).Render("BARYON — CONSISTENT HASHING LOAD BALANCER")
	sections = append(sections, title)
	sections = append(sections, strings.Repeat("─", contentWidth))

	overviewLine1 := fmt.Sprintf("PROXY: %s   UPTIME: %s   REQUESTS/SEC: %s",
		statusOnlineStyle.Render("RUNNING"),
		highlightStyle.Render(uptimeStr),
		highlightStyle.Render(fmt.Sprintf("%7.0f", m.measuredRPS)),
	)
	overviewLine2 := fmt.Sprintf("TOTAL REQUESTS: %s",
		highlightStyle.Render(fmt.Sprintf("%d", m.totalRequests)),
	)
	sections = append(sections, overviewLine1)
	sections = append(sections, overviewLine2)
	sections = append(sections, strings.Repeat("─", contentWidth))

	// 2. Backend Status & Traffic Distribution Section
	sections = append(sections, sectionHeaderStyle.Render("BACKEND STATUS & TRAFFIC DISTRIBUTION"))
	sections = append(sections, "")

	for _, b := range m.backends {
		alive := b.IsAlive()
		total, active := b.GetStats()

		statusTag := statusOnlineStyle.Render("ONLINE ")
		if !alive {
			statusTag = statusOfflineStyle.Render("OFFLINE")
		}

		var sharePct float64
		if m.totalRequests > 0 {
			sharePct = (float64(total) / float64(m.totalRequests)) * 100.0
		}

		// 15-character traffic distribution bar
		const maxBarWidth = 15
		fillWidth := int((sharePct / 100.0) * float64(maxBarWidth))
		if fillWidth > maxBarWidth {
			fillWidth = maxBarWidth
		}
		if fillWidth < 0 {
			fillWidth = 0
		}
		emptyWidth := maxBarWidth - fillWidth

		bar := barFillStyle.Render(strings.Repeat("█", fillWidth)) +
			barEmptyStyle.Render(strings.Repeat("░", emptyWidth))

		addrStr := b.URL.Host
		if len(addrStr) > 15 {
			addrStr = addrStr[:15]
		}

		backendLine := fmt.Sprintf("%-10s %-15s %s  %s  %5.1f%%  total: %-8d  active: %-4d",
			b.Name,
			dimStyle.Render(addrStr),
			statusTag,
			bar,
			sharePct,
			total,
			active,
		)
		sections = append(sections, backendLine)
	}

	sections = append(sections, "")
	sections = append(sections, strings.Repeat("─", contentWidth))

	// 3. Recent Routing Log Section (Last 5 events)
	sections = append(sections, sectionHeaderStyle.Render("RECENT ROUTING LOG (LATEST 5)"))

	if len(m.recentLogs) == 0 {
		sections = append(sections, dimStyle.Render("  Waiting for traffic events..."))
	} else {
		for _, log := range m.recentLogs {
			if log.IsStatus {
				sections = append(sections, fmt.Sprintf("  %s  %s",
					dimStyle.Render(log.Timestamp),
					feedbackStyle.Render(log.StatusMsg),
				))
			} else {
				sections = append(sections, fmt.Sprintf("  %s  %-15s %s %s",
					dimStyle.Render(log.Timestamp),
					highlightStyle.Render(log.ClientIP),
					eventArrowStyle.Render("->"),
					log.Target,
				))
			}
		}
	}

	// Pad logs display so height remains stable
	for i := len(m.recentLogs); i < 5; i++ {
		sections = append(sections, "")
	}

	sections = append(sections, strings.Repeat("─", contentWidth))

	// 4. Feedback & Keyboard Controls
	if m.actionFeedback != "" {
		sections = append(sections, feedbackStyle.Render("  "+m.actionFeedback))
	} else {
		sections = append(sections, "")
	}

	footer := footerStyle.Render("[K] Kill backend-1    [R] Restart backend-1    [Q] Quit")
	sections = append(sections, footer)

	content := strings.Join(sections, "\n")
	return boxStyle.Width(contentWidth).Render(content)
}
