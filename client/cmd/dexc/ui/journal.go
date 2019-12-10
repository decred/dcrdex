// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ui

import (
	"sync"

	"github.com/rivo/tview"
)

// A journal is a TextView with concurrency protection and buffer maintenance.
type journal struct {
	*tview.TextView
	history [][]byte
	mtx     sync.Mutex
}

func newJournal(title string, keyFunc inputCapture) *journal {
	txtView := tview.NewTextView().
		SetScrollable(true).
		SetWordWrap(true).
		SetDynamicColors(true)
	txtView.SetBorderColor(blurColor).
		SetInputCapture(keyFunc).
		SetBorder(true).
		SetTitle(title).
		SetBorderPadding(1, 3, 1, 3)

	j := &journal{
		TextView: txtView,
		history:  make([][]byte, 0),
	}
	j.SetChangedFunc(func() {
		if j.HasFocus() {
			app.Draw()
		}
	})
	return j
}

// The TextView's performance begins to lag after too many entries are added.
// Maintain a buffer that drops some log messages off of the front when the
// it gets too long.
func (j *journal) Write(p []byte) {
	j.mtx.Lock()
	defer j.mtx.Unlock()
	j.history = append(j.history, p)
	var err error
	if len(j.history) >= 150 {
		j.history = j.history[50:]
		j.Clear()
		for _, entry := range j.history {
			_, err = j.TextView.Write(entry)
			if err != nil {
				break
			}
		}
	} else {
		_, err = j.TextView.Write(p)
	}
	if err != nil {
		log.Errorf("error writing to the journal: %v", err)
	}
}

func (j *journal) AddFocus() {
	j.SetBorderColor(focusColor)
	app.SetFocus(j)
}

func (j *journal) RemoveFocus() {
	j.SetBorderColor(blurColor)
}
