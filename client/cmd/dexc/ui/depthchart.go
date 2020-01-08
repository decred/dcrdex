// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ui

import (
	"fmt"
	"math"
	"sync"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

var (
	chartColor = tcell.GetColor("#0c456b")
)

type depthPoint struct {
	x float64
	y float64
}

// depthChart is a simple character-based chart. Right now, the chart only
// works for a fixed chart size which cannot be changed.
type depthChart struct {
	*tview.Box
	focus bool
	// The chart data is protected by a mutex since more than one thread could be
	// accessing simultaneously. That is not the case for focus, which is only
	// accessed during calls initiated by tview, so assumed to be sequenced.
	mtx      sync.RWMutex
	seq      uint64
	drawID   uint64
	runeRows []string
}

func newDepthChart() *depthChart {
	dc := &depthChart{
		Box: tview.NewBox(),
		seq: 1,
	}
	dc.SetBorder(true)
	dc.runeRows = dc.calcRows()
	return dc
}

// Need to handle concurrency for chart updates.
func (c *depthChart) newScreenRows() []string {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if !c.focus {
		return nil
	}
	if c.seq == c.drawID {
		// Nothing to redraw.
		return nil
	}
	c.runeRows = c.calcRows()
	c.seq = c.drawID
	return c.runeRows
}

// Draw hijacks the embedded Box method to draw on the screen.
func (c *depthChart) Draw(screen tcell.Screen) {
	screenX, screenY, width, _ := c.GetRect()
	rows := c.newScreenRows()
	for y, row := range rows {
		tview.Print(screen, row, 0+screenX, y+screenY, width, tview.AlignLeft, chartColor)
	}
}

// refresh recalculates the chart rune rows.
//
// DRAFT NOTE: This method is currently just a demo of charting. It will need to
// be a lot smarter when real data is plotted.
func (c *depthChart) calcRows() []string {
	rotations := float64(2.5)
	numPts := 400
	amp := float64(5)
	pts := make([]depthPoint, 0, numPts)
	for i := 0; i < numPts; i++ {
		x := float64(i) / float64(numPts) * rotations * 2 * math.Pi
		pts = append(pts, depthPoint{
			x: float64(i),
			y: math.Sin(x) * amp,
		})
	}
	width, height := marketChartWidth, marketChartHeight
	if width == 0 || height == 0 {
		return nil
	}
	runeRows := make([]string, 0, height)
	edge, err := interpolate(pts, width, height)
	if err != nil {
		// Don't update the UI from a Draw method, so we won't print an error here.
		// TODO: Add a file-only logger for logging these errors?
		return nil
	}

	for iy := 0; iy < height; iy++ {
		y := height - iy - 1
		row := make([]rune, width)
		for ix := 0; ix < width; ix++ {
			pt := edge[ix]
			switch {
			case pt.y < y:
				row = append(row, uniSpace)
			case pt.y == y:
				// fmt.Printf("-- drawing at %d\n", y)
				row = append(row, pt.ch)
			default:
				row = append(row, fullBlock)
			}
		}
		runeRows = append(runeRows, string(row))
	}
	return runeRows
}

// Focus is to the tview.Box Focus method, wrapped here to force a redraw of
// the chart.
func (c *depthChart) Focus(delegate func(p tview.Primitive)) {
	c.focus = true
	// Have to increment the seqID on focus to force redraw.
	c.mtx.Lock()
	c.seq++
	c.mtx.Unlock()
	// Pass the delegate to the embedded Box's method.
	c.Box.Focus(delegate)
}

// Blur is to the tview.Box Focus method. Used to set the focus field.
func (c *depthChart) Blur() {
	c.focus = false
	c.Box.Blur()
}

// See the unicode block elements at
// https://en.wikipedia.org/wiki/Box-drawing_character#Unicode
const (
	uniSpace          rune = '\u0020'
	bottomEight       rune = '\u2581'
	bottomQuarter     rune = '\u2582'
	bottomThreeEights rune = '\u2583'
	bottomHalf        rune = '\u2584'
	bottomFiveEights  rune = '\u2585'
	bottomTwoThirds   rune = '\u2586'
	bottomSevenEights rune = '\u2587'
	fullBlock         rune = '\u2588'
	// The remaining runes are not used right now, but they may be handy for
	// additional sub-character detail in the future.
	// bottomRightHeavy rune = '\u259f'
	// bottomRightLite  rune = '\u2597'
	// bottomLeftHeavy  rune = '\u2599'
	// bottomLeftLite   rune = '\u2596'
	// topRightHeavy    rune = '\u259c'
	// topRightLite     rune = '\u259d'
	// topLeftHeavy     rune = '\u259b'
	// topLeftLite      rune = '\u2598'
	// shadedBlock      rune = '\u2593'
)

// An edge point is a character and it's height on the grid..
type edgePoint struct {
	y  int
	ch rune
}

// Translate the x,y chart data into width edgePoints, each edgePoint with
// 0 < y < height, and an appropriate character.
//
// The algorithm projects the line graph onto the width x height grid, and finds
// the points where the charted line intersects the cell edges. The value
// assigned to a column is the average of the intersects of the left and
// right sides.
func interpolate(pts []depthPoint, width, height int) ([]edgePoint, error) {
	xMin, xMax, yMin, yMax, err := readLimits(pts)
	if err != nil {
		return nil, err
	}
	numPts := len(pts)
	yRange := yMax - yMin
	xRange := xMax - xMin
	yScaler, xScaler := yRange/float64(height), xRange/float64(width)
	lastY := pts[0].y / yScaler
	intersections := append(make([]float64, 0, numPts+1), lastY-yMin/yScaler)
	lastX := int(float64(pts[0].x) / xScaler)
	for i := 0; i < len(pts)-1; i++ {
		left, right := pts[i], pts[i+1]
		y0 := (left.y - yMin) / yScaler
		y1 := (right.y - yMin) / yScaler
		x0 := (left.x - xMin) / xScaler
		x1 := (right.x - xMin) / xScaler
		rightIdx := int(x1)
		if rightIdx > lastX {
			for xIdx := lastX + 1; xIdx <= rightIdx; xIdx++ {
				x := float64(xIdx)
				intersections = append(intersections, (x-x0)/(x1-x0)*(y1-y0)+y0)
			}
		}
		lastX = rightIdx
	}
	if len(intersections) < width+1 {
		if len(intersections) != width {
			return nil, fmt.Errorf("expected %d or %d points after interpolation, got %d",
				numPts, numPts+1, len(intersections))
		}
		intersections = append(intersections, float64(pts[len(pts)-1].y-yMin)/yScaler)
	}
	return roughEdge(intersections)
}

// Rough edge translates the intersections into edgePoints with 1 of 9
// characters of appropriate character height.
func roughEdge(intersections []float64) ([]edgePoint, error) {
	edge := make([]edgePoint, 0, len(intersections)-1)
	for i := 0; i < len(intersections)-1; i++ {
		left, right := intersections[i], intersections[i+1]
		yFlt, partial := math.Modf((left + right) / 2)
		var block rune
		sixteenths := int(partial * 16)
		switch sixteenths {
		case 0:
			block = uniSpace
		case 1, 2:
			block = bottomEight
		case 3, 4:
			block = bottomQuarter
		case 5, 6:
			block = bottomThreeEights
		case 7, 8:
			block = bottomHalf
		case 9, 10:
			block = bottomFiveEights
		case 11, 12:
			block = bottomTwoThirds
		case 13, 14:
			block = bottomSevenEights
		default:
			block = fullBlock
		}
		edge = append(edge, edgePoint{
			y:  int(yFlt),
			ch: block,
		})
	}
	return edge, nil
}

// readLimits reads the data, performs a couple of basic checks, and extracts
// the data extents.
func readLimits(pts []depthPoint) (float64, float64, float64, float64, error) {
	if len(pts) == 0 {
		return 0, 0, 0, 0, fmt.Errorf("no points")
	}
	var yMin float64 = math.MaxFloat64
	var yMax float64 = -math.MaxFloat64
	xMin, xMax := yMin, yMax
	lastX := pts[0].x
	for _, pt := range pts {
		x, y := pt.x, pt.y
		if x < lastX {
			return 0, 0, 0, 0, fmt.Errorf("non-increasing x not allowed")
		}
		if y > yMax {
			yMax = y
		}
		if y < yMin {
			yMin = y
		}
		if x > xMax {
			xMax = x
		}
		if x < xMin {
			xMin = x
		}
	}
	return xMin, xMax, yMin, yMax, nil
}
