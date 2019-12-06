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

var wg = new(sync.WaitGroup)

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
		if k.Key == keyboard.KeyEsc || k.Key == keyboard.KeyCtrlC {
			cancel()
		}
	}
	termdash.Run(ctx, tb, c, termdash.KeyboardSubscriber(quitter), termdash.RedrawInterval(redrawInterval))
	wg.Wait()
	return nil
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
			),
		),
		grid.ColWidthPerc(40,
			grid.Widget(w.text,
				container.Border(linestyle.Light),
				container.BorderTitle("charts"),
			),
		),
		grid.ColWidthPerc(40,
			grid.Widget(w.text,
				container.Border(linestyle.Light),
				container.BorderTitle("tables"),
			),
		),
	}
	cosole := grid.Widget(w.console, container.Border(linestyle.Light), container.BorderTitle("console"))
	input := grid.Widget(w.input, container.BorderTitle("input"))
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
