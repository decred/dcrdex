package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/decred/slog"
	"github.com/joegruffins/termdash"
	"github.com/joegruffins/termdash/cell"
	"github.com/joegruffins/termdash/container"
	"github.com/joegruffins/termdash/container/grid"
	"github.com/joegruffins/termdash/keyboard"
	"github.com/joegruffins/termdash/linestyle"
	"github.com/joegruffins/termdash/terminal/termbox"
	"github.com/joegruffins/termdash/terminal/terminalapi"
	"github.com/joegruffins/termdash/widgets/menu"
	"github.com/joegruffins/termdash/widgets/text"
	"github.com/joegruffins/termdash/widgets/textinput"
)

const (
	// rootID is the ID assigned to the root container.
	rootID = "root"
	// redrawInterval is how often termdash redraws the screen.
	redrawInterval = 250 * time.Millisecond
)

type layout string

const (
	menuID    layout = "menu"
	chartsID  layout = "charts"
	tablesID  layout = "tables"
	consoleID layout = "console"
	inputID   layout = "input"
)

type position struct {
	X int
	Y int
}

var (
	wg                = new(sync.WaitGroup)
	currentPosition   = &position{0, 0}
	possiblePositions = [][]layout{}
)

// widgets holds all widgets used by the client.
type widgets struct {
	text    *text.Text
	input   *textinput.TextInput
	console *text.Text
	menu    *menu.Menu
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.NewBackend(os.Stderr).Logger("tui")
	log.SetLevel(slog.LevelTrace)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tb, err := termbox.New(termbox.ColorMode(terminalapi.ColorMode256))
	if err != nil {
		return err
	}
	defer tb.Close()
	populatePossiblePositions()
	c, err := container.New(tb, container.ID(rootID))
	if err != nil {
		return err
	}

	w, err := newWidgets(ctx, cancel, c)
	if err != nil {
		return err
	}

	gridOpts, err := gridLayout(w)
	if err != nil {
		return err
	}

	if err := c.Update(rootID, gridOpts...); err != nil {
		return err
	}

	quitter := func(k *terminalapi.Keyboard) {
		var err error
		switch k.Key {
		case keyboard.KeyEsc, keyboard.KeyCtrlC:
			cancel()
		case keyboard.KeyArrowUp:
			err = c.FocusID(string(upArrow()))
		case keyboard.KeyArrowDown:
			err = c.FocusID(string(downArrow()))
		case keyboard.KeyArrowLeft:
			err = c.FocusID(string(leftArrow()))
		case keyboard.KeyArrowRight:
			err = c.FocusID(string(rightArrow()))
		}
		if err != nil {
			panic(err)
		}
	}
	termdash.Run(ctx, tb, c, termdash.KeyboardSubscriber(quitter), termdash.RedrawInterval(redrawInterval))
	wg.Wait()
	return nil
}

func upArrow() layout {
	cp := currentPosition
	if cp.Y > 0 {
		cp.Y -= 1
	}
	return possiblePositions[cp.Y][cp.X]
}

func downArrow() layout {
	cp := currentPosition
	pp := possiblePositions
	if cp.Y < len(pp)-1 {
		cp.Y += 1
	}
	return pp[cp.Y][cp.X]
}

func leftArrow() layout {
	cp := currentPosition
	if cp.X > 0 {
		cp.X -= 1
	}
	return possiblePositions[cp.Y][cp.X]
}

func rightArrow() layout {
	cp := currentPosition
	pp := possiblePositions
	if cp.X < len(pp[0])-1 {
		cp.X += 1
	}
	return pp[cp.Y][cp.X]
}

func populatePossiblePositions() {
	possiblePositions = [][]layout{
		{menuID, chartsID, tablesID},
		{consoleID, consoleID, consoleID},
		{inputID, inputID, inputID},
	}
}

// newWidgets creates all widgets used by the client.
func newWidgets(ctx context.Context, cancel context.CancelFunc, c *container.Container) (*widgets, error) {
	text, err := newTextWgt(ctx, "hi der")
	if err != nil {
		return nil, err
	}

	consoleInput, consoleOutput := cmdListener(ctx, wg, cancel)

	input, err := newInputWgt(ctx, consoleInput)
	if err != nil {
		return nil, err
	}
	console, err := newConsoleWgt(ctx, wg, consoleOutput)
	if err != nil {
		return nil, err
	}

	menu, err := newMenuWgt(ctx, "menuItemOne\nmenuItemTwo\nmenuItemThree\nmenuItemFour\nmenuItemFive")
	if err != nil {
		return nil, err
	}

	return &widgets{
		text:    text,
		input:   input,
		console: console,
		menu:    menu,
	}, nil
}

func gridLayout(w *widgets) ([]container.Option, error) {
	charts := []grid.Element{
		grid.ColWidthPerc(20,
			grid.Widget(w.menu,
				container.Border(linestyle.Light),
				container.BorderTitle("menu"),
				container.ID(string(menuID)),
			),
		),
		grid.ColWidthPerc(40,
			grid.Widget(w.text,
				container.Border(linestyle.Light),
				container.BorderTitle("charts"),
				container.ID(string(chartsID)),
			),
		),
		grid.ColWidthPerc(40,
			grid.Widget(w.text,
				container.Border(linestyle.Light),
				container.BorderTitle("tables"),
				container.ID(string(tablesID)),
			),
		),
	}
	cosole := grid.Widget(
		w.console, container.Border(linestyle.Light),
		container.BorderTitle("console"),
		container.ID(string(consoleID)),
	)
	input := grid.Widget(
		w.input,
		container.BorderTitle("input"),
		container.ID(string(inputID)),
	)
	builder := grid.New()
	builder.Add(
		grid.RowHeightPerc(60, charts...),
		grid.RowHeightPerc(30, cosole),
		grid.RowHeightPerc(10, input),
	)
	gridOpts, err := builder.Build()
	if err != nil {
		return nil, err
	}
	return gridOpts, nil
}

func newTextWgt(ctx context.Context, s string) (*text.Text, error) {
	wgt, err := text.New()
	if err != nil {
		return nil, err
	}
	if err = wgt.Write(s); err != nil {
		return nil, err
	}
	return wgt, nil

}

func newMenuWgt(ctx context.Context, s string) (*menu.Menu, error) {
	wgt, err := menu.New()
	if err != nil {
		return nil, err
	}
	if err = wgt.Write(s); err != nil {
		return nil, err
	}
	return wgt, nil

}

func newConsoleWgt(ctx context.Context, wg *sync.WaitGroup, ch <-chan *response) (*text.Text, error) {
	wgt, err := text.New(
		text.RollContent(),
	)
	if err != nil {
		return nil, err
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case res := <-ch:
				out := res.msg
				if res.err != nil {
					out = res.err.Error()
				}
				out += "\n"
				if err = wgt.Write(out); err != nil {
					panic(err)
				}
			}
		}
	}()

	return wgt, nil
}

func newInputWgt(ctx context.Context, ch chan<- string) (*textinput.TextInput, error) {
	wgt, err := textinput.New(
		textinput.Label("$", cell.FgColor(cell.ColorBlue)),
		textinput.Border(linestyle.Light),
		textinput.PlaceHolder("enter command"),
		textinput.OnSubmit(func(text string) error {
			ch <- text
			return nil
		}),
		textinput.ClearOnSubmit(),
	)
	if err != nil {
		return nil, err
	}
	return wgt, nil
}
