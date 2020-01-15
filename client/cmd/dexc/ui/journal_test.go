// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ui

import (
	"math/rand"
	"testing"

	"github.com/gdamore/tcell"
)

func randByteString() []byte {
	length := rand.Intn(150) + 10
	b := make([]byte, length)
	rand.Read(b)
	return b
}

func TestJournal(t *testing.T) {
	// journalTrimSize
	// maxJournalLines
	j := newJournal("test", func(event *tcell.EventKey) *tcell.EventKey { return nil })
	for i := 0; i < maxJournalLines-1; i++ {
		j.Write(randByteString())
	}
	if len(j.history) != maxJournalLines-1 {
		t.Fatalf("wrong journal line count before trim. Expected %d, got %d", len(j.history), maxJournalLines-1)
	}
	// Writing one more line should for a trim.
	j.Write(randByteString())
	if len(j.history) != maxJournalLines-journalTrimSize {
		t.Fatalf("wrong journal line count after trim. Expected %d, got %d", len(j.history), maxJournalLines-journalTrimSize)
	}

}
