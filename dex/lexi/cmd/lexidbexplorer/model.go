// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"sort"

	"decred.org/dcrdex/dex/lexi"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/dgraph-io/badger"
)

// viewState represents the current view in the application.
type viewState int

const (
	viewTables      viewState = iota // List of tables
	viewTableDetail                  // Table details (indexes + view all)
	viewEntries                      // Iterating entries
	viewEntryDetail                  // Viewing a single entry in detail
)

const (
	maxKeyDisplay   = 40 // Max characters for key in list
	maxValuePreview = 60 // Max characters for value preview

	// UI layout constants
	entriesViewOverhead   = 8  // Lines used by header/footer in entries view
	detailViewOverhead    = 6  // Lines used by header/footer in detail view
	minVisibleLines       = 5  // Minimum visible content lines
	minDetailVisibleLines = 10 // Minimum visible lines in detail view
)

// model is the bubbletea model for the DB explorer.
type model struct {
	db         *lexi.DB
	bdb        *badger.DB
	tables     map[string][]string // tableName -> indexNames
	tableNames []string            // sorted table names

	state  viewState
	cursor int

	// Current selection context
	currentTable string
	currentIndex string // empty = iterate table directly

	// Entry viewing
	entries       []entryData
	entriesOffset int
	selectedEntry int
	loading       bool

	// Entry detail scrolling
	detailScrollOffset int
	detailTotalLines   int
	detailLines        []string

	// UI dimensions
	width, height int

	// Error message
	err error
}

var _ tea.Model = (*model)(nil)

// visibleEntriesLines returns the number of visible lines for the entries list.
func (m model) visibleEntriesLines() int {
	return max(minVisibleLines, m.height-entriesViewOverhead)
}

// visibleDetailLines returns the number of visible lines for entry detail view.
func (m model) visibleDetailLines() int {
	return max(minDetailVisibleLines, m.height-detailViewOverhead)
}

// maxDetailScroll returns the maximum scroll offset for the detail view.
func (m model) maxDetailScroll() int {
	return max(0, m.detailTotalLines-m.visibleDetailLines())
}

// Init initializes the model.
func (m model) Init() tea.Cmd {
	return nil
}

// initModel creates a new model with the given database.
func initModel(db *lexi.DB, bdb *badger.DB) (model, error) {
	tables, err := listTablesAndIndexes(bdb)
	if err != nil {
		return model{}, err
	}

	// Sort table names for consistent display
	tableNames := make([]string, 0, len(tables))
	for name := range tables {
		tableNames = append(tableNames, name)
	}
	sort.Strings(tableNames)

	return model{
		db:         db,
		bdb:        bdb,
		tables:     tables,
		tableNames: tableNames,
		state:      viewTables,
		width:      80,
		height:     24,
	}, nil
}

// Update handles messages and updates the model.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case entriesLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.entries = msg.entries
		return m, nil
	}
	return m, nil
}

// entriesLoadedMsg is sent when entries are loaded from the database.
type entriesLoadedMsg struct {
	entries []entryData
	err     error
}

// handleKeyPress processes keyboard input.
func (m model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "up", "k":
		return m.moveCursorUp()

	case "down", "j":
		return m.moveCursorDown()

	case "enter", " ":
		return m.selectItem()

	case "esc", "backspace":
		return m.goBack()

	case "home", "g":
		m.cursor = 0
		m.entriesOffset = 0
		m.detailScrollOffset = 0
		return m, nil

	case "end", "G":
		return m.goToEnd()

	case "pgup", "ctrl+u":
		return m.pageUp()

	case "pgdown", "ctrl+d":
		return m.pageDown()
	}
	return m, nil
}

// moveCursorUp moves the cursor up.
func (m model) moveCursorUp() (tea.Model, tea.Cmd) {
	if m.state == viewEntryDetail {
		if m.detailScrollOffset > 0 {
			m.detailScrollOffset--
		}
		return m, nil
	}
	if m.cursor > 0 {
		m.cursor--
		if m.state == viewEntries && m.cursor < m.entriesOffset {
			m.entriesOffset = m.cursor
		}
	}
	return m, nil
}

// moveCursorDown moves the cursor down.
func (m model) moveCursorDown() (tea.Model, tea.Cmd) {
	if m.state == viewEntryDetail {
		if m.detailScrollOffset < m.maxDetailScroll() {
			m.detailScrollOffset++
		}
		return m, nil
	}
	maxCursor := m.getMaxCursor()
	if m.cursor < maxCursor {
		m.cursor++
		visibleLines := m.visibleEntriesLines()
		if m.state == viewEntries && m.cursor >= m.entriesOffset+visibleLines {
			m.entriesOffset = m.cursor - visibleLines + 1
		}
	}
	return m, nil
}

// goToEnd moves cursor to the end of the current list.
func (m model) goToEnd() (tea.Model, tea.Cmd) {
	if m.state == viewEntryDetail {
		m.detailScrollOffset = m.maxDetailScroll()
		return m, nil
	}
	m.cursor = m.getMaxCursor()
	if m.state == viewEntries {
		visibleLines := m.visibleEntriesLines()
		if m.cursor >= visibleLines {
			m.entriesOffset = m.cursor - visibleLines + 1
		}
	}
	return m, nil
}

// pageUp scrolls up by a page.
func (m model) pageUp() (tea.Model, tea.Cmd) {
	if m.state == viewEntryDetail {
		m.detailScrollOffset = max(0, m.detailScrollOffset-m.visibleDetailLines())
		return m, nil
	}
	if m.state == viewEntries {
		m.cursor = max(0, m.cursor-m.visibleEntriesLines())
		m.entriesOffset = m.cursor
	}
	return m, nil
}

// pageDown scrolls down by a page.
func (m model) pageDown() (tea.Model, tea.Cmd) {
	if m.state == viewEntryDetail {
		m.detailScrollOffset = min(m.maxDetailScroll(), m.detailScrollOffset+m.visibleDetailLines())
		return m, nil
	}
	if m.state == viewEntries {
		visibleLines := m.visibleEntriesLines()
		m.cursor = min(m.getMaxCursor(), m.cursor+visibleLines)
		if m.cursor >= m.entriesOffset+visibleLines {
			m.entriesOffset = m.cursor - visibleLines + 1
		}
	}
	return m, nil
}

// getMaxCursor returns the maximum valid cursor position.
func (m model) getMaxCursor() int {
	switch m.state {
	case viewTables:
		return max(0, len(m.tableNames)-1)
	case viewTableDetail:
		// "View all entries" + indexes
		return len(m.tables[m.currentTable])
	case viewEntries:
		return max(0, len(m.entries)-1)
	case viewEntryDetail:
		return 0
	}
	return 0
}

// selectItem selects the current item based on state.
func (m model) selectItem() (tea.Model, tea.Cmd) {
	switch m.state {
	case viewTables:
		if len(m.tableNames) == 0 {
			return m, nil
		}
		m.currentTable = m.tableNames[m.cursor]
		m.state = viewTableDetail
		m.cursor = 0
		return m, nil

	case viewTableDetail:
		indexes := m.tables[m.currentTable]
		if m.cursor == 0 {
			// "View all entries" selected
			m.currentIndex = ""
		} else {
			// An index was selected
			m.currentIndex = indexes[m.cursor-1]
		}
		m.state = viewEntries
		m.entries = nil
		m.entriesOffset = 0
		m.cursor = 0
		m.loading = true
		return m, m.loadEntries()

	case viewEntries:
		if len(m.entries) == 0 {
			return m, nil
		}
		m.selectedEntry = m.cursor
		m.state = viewEntryDetail
		m.detailScrollOffset = 0
		// Cache the rendered lines so line-count and rendering never drift.
		entry := m.entries[m.cursor]
		m.detailLines = buildEntryDetailLines(entry)
		m.detailTotalLines = len(m.detailLines)
		return m, nil

	case viewEntryDetail:
		// Press enter to go back from detail view
		m.state = viewEntries
		m.cursor = m.selectedEntry
		m.detailLines = nil
		m.detailTotalLines = 0
		return m, nil
	}
	return m, nil
}

// goBack returns to the previous view.
func (m model) goBack() (tea.Model, tea.Cmd) {
	switch m.state {
	case viewTables:
		return m, tea.Quit
	case viewTableDetail:
		m.state = viewTables
		m.cursor = 0
		for i, name := range m.tableNames {
			if name == m.currentTable {
				m.cursor = i
				break
			}
		}
		m.currentTable = ""
		return m, nil
	case viewEntries:
		m.state = viewTableDetail
		m.cursor = 0
		m.entries = nil
		m.currentIndex = ""
		return m, nil
	case viewEntryDetail:
		m.state = viewEntries
		m.cursor = m.selectedEntry
		m.detailLines = nil
		m.detailTotalLines = 0
		return m, nil
	}
	return m, nil
}

// loadEntries returns a command to load entries from the database.
// We load all entries at once (limit=0) since lexi doesn't have built-in
// pagination with seek.
func (m model) loadEntries() tea.Cmd {
	return func() tea.Msg {
		var entries []entryData
		var err error
		if m.currentIndex == "" {
			entries, err = iterateTable(m.db, m.currentTable, 0) // 0 = no limit
		} else {
			entries, err = iterateIndex(m.bdb, m.db, m.currentTable, m.currentIndex, 0) // 0 = no limit
		}
		return entriesLoadedMsg{entries: entries, err: err}
	}
}

// View renders the current view.
func (m model) View() string {
	if m.err != nil {
		return renderError(m)
	}

	switch m.state {
	case viewTables:
		return renderTablesView(m)
	case viewTableDetail:
		return renderTableDetailView(m)
	case viewEntries:
		return renderEntriesView(m)
	case viewEntryDetail:
		return renderEntryDetailView(m)
	}
	return ""
}
