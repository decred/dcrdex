// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// lexidbexplorer is an interactive CLI tool for exploring lexi databases.
// It provides arrow-key navigation through tables, indexes, and entries
// with human-readable display of values.
//
// Usage:
//
//	go run ./dex/lexi/cmd/lexidbexplorer /path/to/lexi.db
package main

import (
	"fmt"
	"os"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/lexi"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: lexidbexplorer <path-to-lexi-db>")
		os.Exit(1)
	}

	dbPath := os.Args[1]

	// Check if path exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: database path does not exist: %s\n", dbPath)
		os.Exit(1)
	}

	// Open the lexi database
	log := dex.StdOutLogger("LEXI", dex.LevelOff) // Suppress logging
	db, err := lexi.New(&lexi.Config{
		Path: dbPath,
		Log:  log,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Initialize the model
	m, err := initModel(db, db.DB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing explorer: %v\n", err)
		os.Exit(1)
	}

	// Run the bubbletea program
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running explorer: %v\n", err)
		os.Exit(1)
	}
}
