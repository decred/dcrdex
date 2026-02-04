// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("57")).
			Bold(true)

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1)

	keyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	indexKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208"))
)

func buildEntryDetailLines(entry entryData) []string {
	var lines []string

	// Key section
	lines = append(lines, headerStyle.Render("Key:"))
	for line := range strings.SplitSeq(formatKey(entry.key), "\n") {
		lines = append(lines, keyStyle.Render(line))
	}
	lines = append(lines, "")

	// Index key section (if present)
	if len(entry.indexKey) > 0 {
		lines = append(lines, headerStyle.Render("Index Key:"))
		for line := range strings.SplitSeq(formatKey(entry.indexKey), "\n") {
			lines = append(lines, indexKeyStyle.Render(line))
		}
		lines = append(lines, "")
	}

	// Value section
	lines = append(lines, headerStyle.Render("Value:"))
	for line := range strings.SplitSeq(formatValue(entry.value), "\n") {
		lines = append(lines, valueStyle.Render(line))
	}

	return lines
}

// renderError renders an error message.
func renderError(m model) string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Lexi DB Explorer"))
	sb.WriteString("\n\n")
	sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	sb.WriteString("\n\n")
	sb.WriteString(helpStyle.Render("Press q to quit"))
	return sb.String()
}

// renderTablesView renders the list of tables.
func renderTablesView(m model) string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Lexi DB Explorer - Tables"))
	sb.WriteString("\n\n")

	if len(m.tableNames) == 0 {
		sb.WriteString(dimStyle.Render("No tables found"))
	} else {
		for i, name := range m.tableNames {
			indexCount := len(m.tables[name])
			indexInfo := ""
			if indexCount > 0 {
				indexInfo = dimStyle.Render(fmt.Sprintf(" (%d indexes)", indexCount))
			}

			line := fmt.Sprintf("  %s%s", name, indexInfo)
			if i == m.cursor {
				sb.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", name)))
				sb.WriteString(indexInfo)
			} else {
				sb.WriteString(normalStyle.Render(line))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("↑/↓: navigate • enter: select • q: quit"))

	return sb.String()
}

// renderTableDetailView renders the table detail view with indexes.
func renderTableDetailView(m model) string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render(fmt.Sprintf("Table: %s", m.currentTable)))
	sb.WriteString("\n\n")

	indexes := m.tables[m.currentTable]

	// First item: View all entries
	viewAllLine := "View all entries"
	if m.cursor == 0 {
		sb.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", viewAllLine)))
	} else {
		sb.WriteString(normalStyle.Render(fmt.Sprintf("  %s", viewAllLine)))
	}
	sb.WriteString("\n")

	// List indexes
	if len(indexes) > 0 {
		sb.WriteString("\n")
		sb.WriteString(headerStyle.Render("Indexes:"))
		sb.WriteString("\n")
		for i, idxName := range indexes {
			if i+1 == m.cursor {
				sb.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", idxName)))
			} else {
				sb.WriteString(normalStyle.Render(fmt.Sprintf("  %s", idxName)))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("↑/↓: navigate • enter: select • esc: back • q: quit"))

	return sb.String()
}

// renderEntriesView renders the list of entries.
func renderEntriesView(m model) string {
	var sb strings.Builder

	// Header
	title := fmt.Sprintf("Table: %s", m.currentTable)
	if m.currentIndex != "" {
		title += fmt.Sprintf(" (Index: %s)", m.currentIndex)
	}
	sb.WriteString(titleStyle.Render(title))
	sb.WriteString("\n")

	entryCount := fmt.Sprintf("%d entries", len(m.entries))
	if m.loading {
		entryCount += " (loading...)"
	}
	sb.WriteString(dimStyle.Render(entryCount))
	sb.WriteString("\n\n")

	if len(m.entries) == 0 {
		if m.loading {
			sb.WriteString(dimStyle.Render("Loading entries..."))
		} else {
			sb.WriteString(dimStyle.Render("No entries found"))
		}
	} else {
		// Calculate visible range
		visibleLines := m.visibleEntriesLines()
		start := m.entriesOffset
		end := min(start+visibleLines, len(m.entries))

		for i := start; i < end; i++ {
			entry := m.entries[i]
			keyStr := formatKeyShort(entry.key, maxKeyDisplay)
			valueStr := formatValuePreview(entry.value, maxValuePreview)

			var line string
			if m.currentIndex != "" && len(entry.indexKey) > 0 {
				idxKeyStr := formatKeyShort(entry.indexKey, 20)
				line = fmt.Sprintf("%s %s → %s",
					keyStyle.Render(keyStr),
					indexKeyStyle.Render(fmt.Sprintf("[%s]", idxKeyStr)),
					dimStyle.Render(valueStr))
			} else {
				line = fmt.Sprintf("%s → %s",
					keyStyle.Render(keyStr),
					dimStyle.Render(valueStr))
			}

			if i == m.cursor {
				sb.WriteString(selectedStyle.Render("> "))
				sb.WriteString(line)
			} else {
				sb.WriteString("  ")
				sb.WriteString(line)
			}
			sb.WriteString("\n")
		}

		// Scroll indicator (always show position info)
		sb.WriteString(dimStyle.Render(fmt.Sprintf("\n[%d-%d of %d]", start+1, end, len(m.entries))))
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("↑/↓: navigate • enter: view details • esc: back • q: quit"))

	return sb.String()
}

// renderEntryDetailView renders a single entry in detail.
func renderEntryDetailView(m model) string {
	var sb strings.Builder

	if m.selectedEntry >= len(m.entries) {
		sb.WriteString(errorStyle.Render("Invalid entry selection"))
		return sb.String()
	}

	contentLines := m.detailLines
	if len(contentLines) == 0 {
		// Safety fallback (should normally be populated when entering detail view).
		contentLines = buildEntryDetailLines(m.entries[m.selectedEntry])
	}

	// Calculate visible range
	visibleLines := m.visibleDetailLines()
	totalLines := len(contentLines)
	start := min(m.detailScrollOffset, totalLines)
	end := min(start+visibleLines, totalLines)

	// Render header
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Entry %d of %d", m.selectedEntry+1, len(m.entries))))
	sb.WriteString("\n\n")

	// Render visible content
	for i := start; i < end; i++ {
		sb.WriteString(contentLines[i])
		sb.WriteString("\n")
	}

	// Scroll indicator
	if totalLines > visibleLines {
		scrollPct := 0
		if totalLines-visibleLines > 0 {
			scrollPct = (m.detailScrollOffset * 100) / (totalLines - visibleLines)
		}
		sb.WriteString(dimStyle.Render(fmt.Sprintf("\n[Line %d-%d of %d (%d%%)]",
			start+1, end, totalLines, scrollPct)))
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("↑/↓: scroll • PgUp/PgDn: page • g/G: top/bottom • esc: back"))

	return sb.String()
}
