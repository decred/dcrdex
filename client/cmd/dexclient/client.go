package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/decred/slog"
	"github.com/mum4k/termdash"
	"github.com/mum4k/termdash/cell"
	"github.com/mum4k/termdash/container"
	"github.com/mum4k/termdash/container/grid"
	"github.com/mum4k/termdash/keyboard"
	"github.com/mum4k/termdash/linestyle"
	"github.com/mum4k/termdash/terminal/termbox"
	"github.com/mum4k/termdash/terminal/terminalapi"
	"github.com/mum4k/termdash/widgets/button"
	"github.com/mum4k/termdash/widgets/text"
	"github.com/mum4k/termdash/widgets/textinput"
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
	tOne    *button.Button
	tTwo    *button.Button
	tThree  *button.Button
	tFour   *button.Button
	tFive   *button.Button
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

	tOne, err := newTabWgt(consoleInput, "one")
	if err != nil {
		return nil, err
	}

	tTwo, err := newTabWgt(consoleInput, "two")
	if err != nil {
		return nil, err
	}

	tThree, err := newTabWgt(consoleInput, "three")
	if err != nil {
		return nil, err
	}

	tFour, err := newTabWgt(consoleInput, "four")
	if err != nil {
		return nil, err
	}

	tFive, err := newTabWgt(consoleInput, "five")
	if err != nil {
		return nil, err
	}

	return &widgets{
		text:    text,
		console: console,
		input:   input,
		tOne:    tOne,
		tTwo:    tTwo,
		tThree:  tThree,
		tFour:   tFour,
		tFive:   tFive,
	}, nil
}

func gridLayout(w *widgets) ([]container.Option, error) {
	tabs := []grid.Element{
		grid.ColWidthPerc(20, grid.Widget(w.tOne)),
		grid.ColWidthPerc(20, grid.Widget(w.tTwo)),
		grid.ColWidthPerc(20, grid.Widget(w.tThree)),
		grid.ColWidthPerc(20, grid.Widget(w.tFour)),
		grid.ColWidthPerc(20, grid.Widget(w.tFive)),
	}
	charts := []grid.Element{
		grid.ColWidthPerc(50,
			grid.Widget(w.text,
				container.Border(linestyle.Light),
				container.BorderTitle("charts"),
			),
		),
		grid.ColWidthPerc(50,
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
		grid.RowHeightPerc(10, tabs...),
		grid.RowHeightPerc(50, charts...),
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

func newTabWgt(ch chan<- string, name string) (*button.Button, error) {
	wgt, err := button.New(name, func() error {
		ch <- fmt.Sprintf("%v pressed", name)
		return nil
	},
		button.FillColor(cell.ColorNumber(220)),
		button.TextColor(cell.ColorNumber(27)),
		button.Height(1),
		button.Width(15),
	)
	if err != nil {
		return nil, err
	}
	return wgt, nil
}
