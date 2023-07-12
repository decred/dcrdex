import Doc, { Animation } from './doc'
import { RateEncodingFactor } from './orderutil'
import OrderBook from './orderbook'
import State from './state'
import { UnitInfo, Market, Candle, CandlesPayload } from './registry'

const bind = Doc.bind
const PIPI = 2 * Math.PI
const plusChar = String.fromCharCode(59914)
const minusChar = String.fromCharCode(59915)

interface Point {
  x: number
  y: number
}

interface MinMax {
  min: number
  max: number
}

interface Label {
  val: number
  txt: string
}

interface LabelSet {
  widest?: number
  lbls: Label[]
}

interface Translator {
    x: (x: number) => number
    y: (y: number) => number
    unx: (x: number) => number
    uny: (y: number) => number
    w: (w: number) => number
    h: (h: number) => number
}

export interface MouseReport {
  rate: number
  depth: number
  dotColor: string
  hoverMarkers: number[]
}

export interface VolumeReport {
  buyBase: number
  buyQuote: number
  sellBase: number
  sellQuote: number
}

export interface DepthReporters {
  mouse: (r: MouseReport | null) => void
  click: (x: number) => void
  volume: (r: VolumeReport) => void
  zoom: (z: number) => void
}

export interface CandleReporters {
  mouse: (r: Candle | null) => void
}

export interface ChartReporters {
  resize: () => void,
  click: (e: MouseEvent) => void,
  zoom: (bigger: boolean) => void
}

export interface DepthLine {
  rate: number
  color: string
}

export interface DepthMarker {
  rate: number
  active: boolean
}

interface DepthMark extends DepthMarker {
  qty: number
  sell: boolean
}

interface Theme {
  axisLabel: string
  gridBorder: string
  gridLines: string
  gapLine: string
  value: string
  zoom: string
  zoomHover: string
  sellLine: string
  buyLine: string
  sellFill: string
  buyFill: string
  crosshairs: string
  legendFill: string
  legendText: string
}

const darkTheme: Theme = {
  axisLabel: '#b1b1b1',
  gridBorder: '#383f4b',
  gridLines: '#383f4b',
  gapLine: '#6b6b6b',
  value: '#9a9a9a',
  zoom: '#5b5b5b',
  zoomHover: '#aaa',
  sellLine: '#ae3333',
  buyLine: '#05a35a',
  sellFill: '#591a1a',
  buyFill: '#02572f',
  crosshairs: '#888',
  legendFill: 'black',
  legendText: '#d5d5d5'
}

const lightTheme: Theme = {
  axisLabel: '#1b1b1b',
  gridBorder: '#ddd',
  gridLines: '#ddd',
  gapLine: '#595959',
  value: '#4d4d4d',
  zoom: '#777',
  zoomHover: '#333',
  sellLine: '#99302b',
  buyLine: '#207a46',
  sellFill: '#bd5959',
  buyFill: '#4cad75',
  crosshairs: '#595959',
  legendFill: '#e6e6e6',
  legendText: '#1b1b1b'
}

// Chart is the base class for charts.
export class Chart {
  parent: HTMLElement
  report: ChartReporters
  theme: Theme
  canvas: HTMLCanvasElement
  visible: boolean
  renderScheduled: boolean
  ctx: CanvasRenderingContext2D
  mousePos: Point | null
  rect: DOMRect
  wheelLimiter: number | null
  boundResizer: () => void
  plotRegion: Region
  xRegion: Region
  yRegion: Region
  dataExtents: Extents
  unattachers: (() => void)[]

  constructor (parent: HTMLElement, reporters: ChartReporters) {
    this.parent = parent
    this.report = reporters
    this.theme = State.isDark() ? darkTheme : lightTheme
    this.canvas = document.createElement('canvas')
    this.visible = true
    parent.appendChild(this.canvas)
    const ctx = this.canvas.getContext('2d')
    if (!ctx) {
      console.error('error getting canvas context')
      return
    }
    this.ctx = ctx
    this.ctx.textAlign = 'center'
    this.ctx.textBaseline = 'middle'
    // Mouse handling
    this.mousePos = null
    bind(this.canvas, 'mousemove', (e: MouseEvent) => {
      // this.rect will be set in resize().
      if (!this.rect) return
      this.mousePos = {
        x: e.clientX - this.rect.left,
        y: e.clientY - this.rect.y
      }
      this.draw()
    })
    bind(this.canvas, 'mouseleave', () => {
      this.mousePos = null
      this.draw()
    })

    // Bind resize.
    const resizeObserver = new ResizeObserver(() => this.resize())
    resizeObserver.observe(this.parent)

    // Scrolling by wheel is smoother when the rate is slightly limited.
    this.wheelLimiter = null
    bind(this.canvas, 'wheel', (e: WheelEvent) => { this.wheel(e) })
    bind(this.canvas, 'click', (e: MouseEvent) => { this.click(e) })
    const setVis = () => {
      this.visible = document.visibilityState !== 'hidden'
      if (this.visible && this.renderScheduled) {
        this.renderScheduled = false
        this.draw()
      }
    }
    bind(document, 'visibilitychange', setVis)
    this.unattachers = [() => { Doc.unbind(document, 'visibilitychange', setVis) }]
  }

  wheeled () {
    this.wheelLimiter = window.setTimeout(() => { this.wheelLimiter = null }, 100)
  }

  /* clear the canvas. */
  clear () {
    this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height)
  }

  /* draw calls the child class's render method. */
  draw () {
    this.render()
  }

  /* click is the handler for a click event on the canvas. */
  click (e: MouseEvent) {
    this.report.click(e)
  }

  /* wheel is a mousewheel event handler. */
  wheel (e: WheelEvent) {
    this.zoom(e.deltaY < 0)
    e.preventDefault()
  }

  /*
   * resize updates the chart size. The parentHeight is an argument to support
   * updating the height programmatically after the caller sets a style.height
   * but before the clientHeight has been updated.
   */
  resize () {
    this.canvas.width = this.parent.clientWidth
    this.canvas.height = this.parent.clientHeight
    const xLblHeight = 30
    const yGuess = 40 // y label width guess. Will be adjusted when drawn.
    const plotExtents = new Extents(yGuess, this.canvas.width, 10, this.canvas.height - xLblHeight)
    const xLblExtents = new Extents(yGuess, this.canvas.width, this.canvas.height - xLblHeight, this.canvas.height)
    const yLblExtents = new Extents(0, yGuess, 10, this.canvas.height - xLblHeight)
    this.plotRegion = new Region(this.ctx, plotExtents)
    this.xRegion = new Region(this.ctx, xLblExtents)
    this.yRegion = new Region(this.ctx, yLblExtents)
    // After changing the visibility, this.canvas.getBoundingClientRect will
    // return nonsense until a render.
    window.requestAnimationFrame(() => {
      this.rect = this.canvas.getBoundingClientRect()
      this.report.resize()
    })
  }

  /* zoom is called when the user scrolls the mouse wheel on the canvas. */
  zoom (bigger: boolean) {
    if (this.wheelLimiter) return
    this.report.zoom(bigger)
  }

  /* The market handler will call unattach when the markets page is unloaded. */
  unattach () {
    for (const u of this.unattachers) u()
    this.unattachers = []
  }

  /* render must be implemented by the child class. */
  render () {
    console.error('child class must override render method')
  }

  /* applyLabelStyle applies the style used for axis tick labels. */
  applyLabelStyle (fontSize?: number) {
    this.ctx.textAlign = 'center'
    this.ctx.textBaseline = 'middle'
    this.ctx.font = `${fontSize ?? '14'}px 'sans', sans-serif`
    this.ctx.fillStyle = this.theme.axisLabel
  }

  /* plotXLabels applies the provided labels to the x axis and draws the grid. */
  plotXLabels (labels: LabelSet, minX: number, maxX: number, unitLines: string[]) {
    const extents = new Extents(minX, maxX, 0, 1)
    this.xRegion.plot(extents, (ctx: CanvasRenderingContext2D, tools: Translator) => {
      this.applyLabelStyle()
      const centerX = (maxX + minX) / 2
      let lastX = minX
      let unitCenter = centerX
      labels.lbls.forEach(lbl => {
        ctx.fillText(lbl.txt, tools.x(lbl.val), tools.y(0.5))
        if (centerX >= lastX && centerX < lbl.val) {
          unitCenter = (lastX + lbl.val) / 2
        }
        lastX = lbl.val
      })
      ctx.font = '11px \'sans\', sans-serif'
      if (unitLines.length === 2) {
        ctx.fillText(unitLines[0], tools.x(unitCenter), tools.y(0.63))
        ctx.fillText(unitLines[1], tools.x(unitCenter), tools.y(0.23))
      } else if (unitLines.length === 1) {
        ctx.fillText(unitLines[0], tools.x(unitCenter), tools.y(0.5))
      }
    }, true)
    this.plotRegion.plot(extents, (ctx: CanvasRenderingContext2D, tools: Translator) => {
      ctx.lineWidth = 1
      ctx.strokeStyle = this.theme.gridLines
      labels.lbls.forEach(lbl => {
        line(ctx, tools.x(lbl.val), tools.y(0), tools.x(lbl.val), tools.y(1))
      })
    }, true)
  }

  /*
   * plotYLabels applies the y labels based on the provided plot region, and
   * draws the grid.
   */
  plotYLabels (region: Region, labels: LabelSet, minY: number, maxY: number, unit: string) {
    const extents = new Extents(0, 1, minY, maxY)
    this.yRegion.plot(extents, (ctx: CanvasRenderingContext2D, tools: Translator) => {
      this.applyLabelStyle()
      const centerY = maxY / 2
      let lastY = 0
      let unitCenter = centerY
      labels.lbls.forEach(lbl => {
        ctx.fillText(lbl.txt, tools.x(0.5), tools.y(lbl.val))
        if (centerY >= lastY && centerY < lbl.val) {
          unitCenter = (lastY + lbl.val) / 2
        }
        lastY = lbl.val
      })
      ctx.fillText(unit, tools.x(0.5), tools.y(unitCenter))
    }, true)
    region.plot(extents, (ctx: CanvasRenderingContext2D, tools: Translator) => {
      ctx.lineWidth = 1
      ctx.strokeStyle = this.theme.gridLines
      labels.lbls.forEach(lbl => {
        line(ctx, tools.x(0), tools.y(lbl.val), tools.x(1), tools.y(lbl.val))
      })
    }, true)
  }

  /*
   * doYLabels generates and applies the y-axis labels, based upon the
   * provided plot region.
   */
  doYLabels (region: Region, step: number, unit: string, valFmt?: (v: number) => string) {
    this.applyLabelStyle()
    const yLabels = makeLabels(this.ctx, region.height(), this.dataExtents.y.min,
      this.dataExtents.y.max, 50, step, unit, valFmt)

    // Reassign the width of the y-label column to accommodate the widest text.
    const yAxisWidth = (yLabels.widest || 0) + 20 /* x padding */
    this.yRegion.extents.x.max = yAxisWidth
    this.yRegion.extents.y.max = region.extents.y.max

    this.plotRegion.extents.x.min = yAxisWidth
    this.xRegion.extents.x.min = yAxisWidth
    // Print the y labels.
    this.plotYLabels(region, yLabels, this.dataExtents.y.min, this.dataExtents.y.max, unit)
    return yLabels
  }

  // drawFrame draws an outline around the plotRegion.
  drawFrame () {
    this.plotRegion.plot(new Extents(0, 1, 0, 1), (ctx: CanvasRenderingContext2D, tools: Translator) => {
      ctx.lineWidth = 1
      ctx.strokeStyle = this.theme.gridBorder
      ctx.beginPath()
      ctx.moveTo(tools.x(0), tools.y(0))
      ctx.lineTo(tools.x(0), tools.y(1))
      ctx.lineTo(tools.x(1), tools.y(1))
      ctx.lineTo(tools.x(1), tools.y(0))
      ctx.lineTo(tools.x(0), tools.y(0))
      ctx.stroke()
    })
  }
}

/* DepthChart is a javascript Canvas-based depth chart renderer. */
export class DepthChart extends Chart {
  reporters: DepthReporters
  book: OrderBook
  zoomLevel: number
  lotSize: number
  conventionalRateStep: number
  lines: DepthLine[]
  markers: Record<string, DepthMarker[]>
  zoomInBttn: Region
  zoomOutBttn: Region
  baseUnit: string
  quoteUnit: string

  constructor (parent: HTMLElement, reporters: DepthReporters, zoom: number) {
    super(parent, {
      resize: () => this.resized(),
      click: (e: MouseEvent) => this.clicked(e),
      zoom: (bigger: boolean) => this.zoomed(bigger)
    })
    this.reporters = reporters
    this.zoomLevel = zoom
    this.lines = []
    this.markers = {
      buys: [],
      sells: []
    }
    this.setZoomBttns() // can't wait for requestAnimationFrame -> resized
    this.resize()
  }

  // setZoomBttns creates new regions for zoom in and zoom out buttons. It is
  // used in initiation of the buttons and resizing.
  setZoomBttns () {
    this.zoomInBttn = new Region(this.ctx, new Extents(0, 0, 0, 0))
    this.zoomOutBttn = new Region(this.ctx, new Extents(0, 0, 0, 0))
  }

  /* resized is called when the window or parent element are resized. */
  resized () {
    // The button region extents are set during drawing.
    this.setZoomBttns()
    if (this.book) this.draw()
  }

  /* zoomed zooms the current view in or out. bigger=true is zoom in. */
  zoomed (bigger: boolean) {
    if (!this.zoomLevel) return
    if (!this.book.buys || !this.book.sells) return
    this.wheeled()
    // Zoom in to 66%, but out to 150% = 1 / (2/3) so that the same zoom levels
    // are hit when reversing direction.
    this.zoomLevel *= bigger ? 2 / 3 : 3 / 2
    this.zoomLevel = clamp(this.zoomLevel, 0.005, 2)
    this.draw()
    this.reporters.zoom(this.zoomLevel)
  }

  /* clicked is the canvas 'click' event handler. */
  clicked (e: MouseEvent) {
    if (!this.dataExtents) return
    const x = e.clientX - this.rect.left
    const y = e.clientY - this.rect.y
    if (this.zoomInBttn.contains(x, y)) { this.zoom(true); return }
    if (this.zoomOutBttn.contains(x, y)) { this.zoom(false); return }
    const translator = this.plotRegion.translator(this.dataExtents)
    this.reporters.click(translator.unx(x))
  }

  // clear the canvas.
  clear () {
    this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height)
  }

  // set sets the current data set and draws.
  set (book: OrderBook, lotSize: number, rateStepEnc: number, baseUnitInfo: UnitInfo, quoteUnitInfo: UnitInfo) {
    this.book = book
    this.lotSize = lotSize / baseUnitInfo.conventional.conversionFactor
    this.conventionalRateStep = Doc.conventionalRateStep(rateStepEnc, baseUnitInfo, quoteUnitInfo)
    this.baseUnit = baseUnitInfo.conventional.unit
    this.quoteUnit = quoteUnitInfo.conventional.unit
    if (!this.zoomLevel) {
      const [midGap, gapWidth] = this.gap()
      // Default to 5% zoom, but with a minimum of 5 * midGap, but still observing
      // the hard cap of 200%.
      const minZoom = Math.max(gapWidth / midGap * 5, 0.05)
      this.zoomLevel = Math.min(minZoom, 2)
    }
    this.draw()
  }

  /*
   * render draws the chart.
   * 1. Calculate the data extents and translate the order book data to a
   *    cumulative form.
   * 2. Draw axis ticks and grid, mid-gap line and value, zoom buttons, mouse
   *    position indicator...
   * 4. Tick labels.
   * 5. Data.
   * 6. Epoch line legend.
   * 7. Hover legend.
   */
  render () {
    // if connection fails it is not possible to get book.
    if (!this.book || !this.visible || this.canvas.width === 0) {
      this.renderScheduled = true
      return
    }

    this.clear()
    // if (!this.book || this.book.empty()) return
    const ctx = this.ctx
    const mousePos = this.mousePos
    const buys = this.book.buys
    const sells = this.book.sells

    const [midGap, gapWidth] = this.gap()

    const halfWindow = this.zoomLevel * midGap / 2
    const high = midGap + halfWindow
    const low = midGap - halfWindow

    // Get a sorted copy of the markers list.
    const buyMarkers = [...this.markers.buys]
    const sellMarkers = [...this.markers.sells]
    buyMarkers.sort((a, b) => b.rate - a.rate)
    sellMarkers.sort((a, b) => a.rate - b.rate)
    const markers: DepthMark[] = []

    const buyDepth: [number, number][] = []
    const buyEpoch: [number, number][] = []
    const sellDepth: [number, number][] = []
    const sellEpoch: [number, number][] = []
    const volumeReport = {
      buyBase: 0,
      buyQuote: 0,
      sellBase: 0,
      sellQuote: 0
    }
    let sum = 0
    // The epoch line is above the non-epoch region, so the epochSum y value
    // must account for non-epoch orders too.
    let epochSum = 0

    for (let i = 0; i < buys.length; i++) {
      const ord = buys[i]
      epochSum += ord.qty
      if (ord.rate >= low) buyEpoch.push([ord.rate, epochSum])
      if (ord.epoch) continue
      sum += ord.qty
      buyDepth.push([ord.rate, sum])
      volumeReport.buyBase += ord.qty
      volumeReport.buyQuote += ord.qty * ord.rate
      while (buyMarkers.length && floatCompare(buyMarkers[0].rate, ord.rate)) {
        const mark = buyMarkers.shift()
        if (!mark) continue
        markers.push({
          rate: mark.rate,
          qty: ord.epoch ? epochSum : sum,
          sell: ord.sell,
          active: mark.active
        })
      }
    }
    const buySum = buyDepth.length ? last(buyDepth)[1] : 0
    buyDepth.push([low, buySum])
    const epochBuySum = buyEpoch.length ? last(buyEpoch)[1] : 0
    buyEpoch.push([low, epochBuySum])

    epochSum = sum = 0
    for (let i = 0; i < sells.length; i++) {
      const ord = sells[i]
      epochSum += ord.qty
      if (ord.rate <= high) sellEpoch.push([ord.rate, epochSum])
      if (ord.epoch) continue
      sum += ord.qty
      sellDepth.push([ord.rate, sum])
      volumeReport.sellBase += ord.qty
      volumeReport.sellQuote += ord.qty * ord.rate
      while (sellMarkers.length && floatCompare(sellMarkers[0].rate, ord.rate)) {
        const mark = sellMarkers.shift()
        if (!mark) continue
        markers.push({
          rate: mark.rate,
          qty: ord.epoch ? epochSum : sum,
          sell: ord.sell,
          active: mark.active
        })
      }
    }
    // Add a data point going to the left so that the data doesn't end with a
    // vertical line.
    const sellSum = sellDepth.length ? last(sellDepth)[1] : 0
    sellDepth.push([high, sellSum])
    const epochSellSum = sellEpoch.length ? last(sellEpoch)[1] : 0
    sellEpoch.push([high, epochSellSum])

    // Add ~30px padding to the top of the chart.
    const h = this.xRegion.extents.y.min
    const growthFactor = (h + 30) / h
    const maxY = (epochSellSum && epochBuySum ? Math.max(epochBuySum, epochSellSum) : epochSellSum || epochBuySum || 1) * growthFactor

    const dataExtents = new Extents(low, high, 0, maxY)
    this.dataExtents = dataExtents

    this.doYLabels(this.plotRegion, this.lotSize, this.baseUnit)

    // Print the x labels
    const xLabels = makeLabels(ctx, this.plotRegion.width(), dataExtents.x.min,
      dataExtents.x.max, 100, this.conventionalRateStep, '')

    this.plotXLabels(xLabels, low, high, [`${this.quoteUnit}/`, this.baseUnit])

    // A function to be run at the end if there is legend data to display.
    let mouseData: MouseReport | null = null

    // Draw the grid.
    this.drawFrame()
    this.plotRegion.plot(dataExtents, (ctx, tools) => {
      ctx.lineWidth = 1
      // first, a square around the plot area.
      ctx.strokeStyle = this.theme.gridBorder
      // draw a line to indicate mid-gap
      ctx.lineWidth = 2.5
      ctx.strokeStyle = this.theme.gapLine
      line(ctx, tools.x(midGap), tools.y(0), tools.x(midGap), tools.y(0.3 * dataExtents.y.max))

      ctx.font = '30px \'demi-sans\', sans-serif'
      ctx.textAlign = 'center'
      ctx.textBaseline = 'middle'
      ctx.fillStyle = this.theme.value
      const y = 0.5 * dataExtents.y.max
      ctx.fillText(Doc.formatFourSigFigs(midGap), tools.x(midGap), tools.y(y))
      ctx.font = '12px \'sans\', sans-serif'
      // ctx.fillText('mid-market price', tools.x(midGap), tools.y(y) + 24)
      ctx.fillText(`${(gapWidth / midGap * 100).toFixed(2)}% spread`,
        tools.x(midGap), tools.y(y) + 24)

      // Draw zoom buttons.
      ctx.textAlign = 'center'
      ctx.textBaseline = 'middle'
      const topCenterX = this.plotRegion.extents.midX
      const topCenterY = tools.y(maxY * 0.9)
      const zoomPct = dataExtents.xRange / midGap * 100
      const zoomText = `${zoomPct.toFixed(1)}%`
      const w = ctx.measureText(zoomText).width
      ctx.font = '13px \'sans\', sans-serif'
      ctx.fillText(zoomText, topCenterX, topCenterY + 1)
      // define the region for the zoom button
      const bttnSize = 20
      const xPad = 10
      let bttnLeft = topCenterX - w / 2 - xPad - bttnSize
      const bttnTop = topCenterY - bttnSize / 2
      this.zoomOutBttn.setExtents(
        bttnLeft,
        bttnLeft + bttnSize,
        bttnTop,
        bttnTop + bttnSize
      )
      let hover = mousePos && this.zoomOutBttn.contains(mousePos.x, mousePos.y)
      this.zoomOutBttn.plot(new Extents(0, 1, 0, 1), ctx => {
        ctx.font = '12px \'icomoon\''
        ctx.fillStyle = this.theme.zoom
        if (hover) {
          ctx.fillStyle = this.theme.zoomHover
          ctx.font = '13px \'icomoon\''
        }
        ctx.fillText(minusChar, this.zoomOutBttn.extents.midX, this.zoomOutBttn.extents.midY)
      })
      bttnLeft = topCenterX + w / 2 + xPad
      this.zoomInBttn.setExtents(
        bttnLeft,
        bttnLeft + bttnSize,
        bttnTop,
        bttnTop + bttnSize
      )
      hover = mousePos && this.zoomInBttn.contains(mousePos.x, mousePos.y)
      this.zoomInBttn.plot(new Extents(0, 1, 0, 1), ctx => {
        ctx.font = '12px \'icomoon\''
        ctx.fillStyle = this.theme.zoom
        if (hover) {
          ctx.fillStyle = this.theme.zoomHover
          ctx.font = '14px \'icomoon\''
        }
        ctx.fillText(plusChar, this.zoomInBttn.extents.midX, this.zoomInBttn.extents.midY)
      })

      // Draw a dotted vertical line where the mouse is, and a dot at the level
      // of the depth line.
      const drawLine = (x: number, color: string) => {
        if (x > high || x < low) return
        ctx.save()
        ctx.setLineDash([3, 5])
        ctx.lineWidth = 1.5
        ctx.strokeStyle = color
        line(ctx, tools.x(x), tools.y(0), tools.x(x), tools.y(maxY))
        ctx.restore()
      }

      for (const line of this.lines || []) {
        drawLine(line.rate, line.color)
      }

      const tolerance = (high - low) * 0.005
      const hoverMarkers = []
      for (const marker of markers || []) {
        const hovered = (mousePos && withinTolerance(marker.rate, tools.unx(mousePos.x), tolerance))
        if (hovered) hoverMarkers.push(marker.rate)
        ctx.save()
        ctx.lineWidth = (hovered || marker.active) ? 5 : 3
        ctx.strokeStyle = marker.sell ? this.theme.sellLine : this.theme.buyLine
        ctx.fillStyle = marker.sell ? this.theme.sellFill : this.theme.buyFill
        const size = (hovered || marker.active) ? 10 : 8
        ctx.beginPath()
        const tip = {
          x: tools.x(marker.rate),
          y: tools.y(marker.qty) - 8
        }
        const top = tip.y - (Math.sqrt(3) * size / 2) // cos(30)
        ctx.moveTo(tip.x, tip.y)
        ctx.lineTo(tip.x - size / 2, top)
        ctx.lineTo(tip.x + size / 2, top)
        ctx.closePath()
        ctx.stroke()
        ctx.fill()
        ctx.restore()
      }

      // If the mouse is in the chart area, draw the crosshairs.
      if (!mousePos) return
      if (!this.plotRegion.contains(mousePos.x, mousePos.y)) return
      // The mouse is in the plot region. Get the data coordinates and find the
      // side and depth for the x value.
      const dataX = tools.unx(mousePos.x)
      let evalSide = sellDepth
      let trigger = (ptX: number) => ptX >= dataX
      let dotColor = this.theme.sellLine
      if (dataX < midGap) {
        evalSide = buyDepth
        trigger = (ptX) => ptX <= dataX
        dotColor = this.theme.buyLine
      }
      let bestDepth = evalSide[0]
      for (let i = 0; i < evalSide.length; i++) {
        const pt = evalSide[i]
        if (trigger(pt[0])) break
        bestDepth = pt
      }
      drawLine(dataX, this.theme.crosshairs)
      mouseData = {
        rate: dataX,
        depth: bestDepth[1],
        dotColor: dotColor,
        hoverMarkers: hoverMarkers
      }
    })

    // Draw the epoch lines
    ctx.lineWidth = 1.5
    ctx.setLineDash([3, 3])
    // epoch sells
    ctx.fillStyle = this.theme.sellFill
    ctx.strokeStyle = this.theme.sellLine
    this.drawDepth(sellEpoch)
    // epoch buys
    ctx.fillStyle = this.theme.buyFill
    ctx.strokeStyle = this.theme.buyLine
    this.drawDepth(buyEpoch)

    // Draw the book depth.
    ctx.lineWidth = 2.5
    ctx.setLineDash([])
    // book sells
    ctx.fillStyle = this.theme.sellFill
    ctx.strokeStyle = this.theme.sellLine
    this.drawDepth(sellDepth)
    // book buys
    ctx.fillStyle = this.theme.buyFill
    ctx.strokeStyle = this.theme.buyLine
    this.drawDepth(buyDepth)

    // Display the dot at the intersection of the mouse hover line and the depth
    // line. This should be drawn after the depths.
    if (mouseData) {
      this.plotRegion.plot(dataExtents, (ctx, tools) => {
        if (!mouseData) return // For TypeScript. Duh.
        dot(ctx, tools.x(mouseData.rate), tools.y(mouseData.depth), mouseData.dotColor, 5)
      })
    }

    // Report the book volumes.
    this.reporters.volume(volumeReport)
    this.reporters.mouse(mouseData)
  }

  /* drawDepth draws a single side's depth chart data. */
  drawDepth (depth: [number, number][]) {
    const firstPt = depth[0]
    let x: number
    this.plotRegion.plot(this.dataExtents, (ctx, tools) => {
      const yZero = tools.y(0)
      let y = tools.y(firstPt[1])
      ctx.beginPath()
      ctx.moveTo(tools.x(firstPt[0]), tools.y(firstPt[1]))
      for (let i = 0; i < depth.length; i++) {
        // Set x, but don't set y until we draw the horizontal line.
        x = tools.x(depth[i][0])
        ctx.lineTo(x, y)
        // If this is past the render edge, quit drawing.
        y = tools.y(depth[i][1])
        ctx.lineTo(x, y)
      }
      ctx.stroke()
      ctx.lineTo(x, yZero)
      ctx.lineTo(tools.x(firstPt[0]), yZero)
      ctx.closePath()
      ctx.globalAlpha = 0.25
      ctx.fill()
    })
  }

  /* returns the mid-gap rate and gap width as a tuple. */
  gap () {
    const [b, s] = [this.book.bestGapBuy(), this.book.bestGapSell()]
    if (!b) {
      if (!s) return [1, 0]
      return [s.rate, 0]
    } else if (!s) return [b.rate, 0]
    return [(s.rate + b.rate) / 2, s.rate - b.rate]
  }

  /* setLines stores the indicator lines to draw. */
  setLines (lines: DepthLine[]) {
    this.lines = lines
  }

  /* setMarkers sets the indicator markers to draw. */
  setMarkers (markers: Record<string, DepthMarker[]>) {
    this.markers = markers
  }
}

/* CandleChart is a candlestick data renderer. */
export class CandleChart extends Chart {
  reporters: CandleReporters
  data: CandlesPayload
  zoomLevel: number
  numToShow: number
  candleRegion: Region
  volumeRegion: Region
  resizeTimer: number
  zoomLevels: number[]
  market: Market
  rateConversionFactor: number

  constructor (parent: HTMLElement, reporters: CandleReporters) {
    super(parent, {
      resize: () => this.resized(),
      click: (/* e: MouseEvent */) => { this.clicked() },
      zoom: (bigger: boolean) => this.zoomed(bigger)
    })
    this.reporters = reporters
    this.zoomLevel = 1
    this.numToShow = 100
    this.resize()
  }

  /* resized is called when the window or parent element are resized. */
  resized () {
    const ext = this.plotRegion.extents
    const candleExtents = new Extents(ext.x.min, ext.x.max, ext.y.min, ext.y.min + ext.yRange * 0.85)
    this.candleRegion = new Region(this.ctx, candleExtents)
    const volumeExtents = new Extents(ext.x.min, ext.x.max, ext.y.min + 0.85 * ext.yRange, ext.y.max)
    this.volumeRegion = new Region(this.ctx, volumeExtents)
    // Set a delay on the render to prevent lag.
    if (this.resizeTimer) clearTimeout(this.resizeTimer)
    this.resizeTimer = window.setTimeout(() => this.draw(), 100)
  }

  clicked (/* e: MouseEvent */) {
    // handle clicks
  }

  /* zoomed zooms the current view in or out. bigger=true is zoom in. */
  zoomed (bigger: boolean) {
    // bigger actually means fewer candles -> reduce zoomLevels index.
    const idx = this.zoomLevels.indexOf(this.numToShow)
    if (bigger) {
      if (idx === 0) return
      this.numToShow = this.zoomLevels[idx - 1]
    } else {
      if (this.zoomLevels.length <= idx + 1 || this.numToShow > this.data.candles.length) return
      this.numToShow = this.zoomLevels[idx + 1]
    }
    this.draw()
  }

  /* render draws the chart */
  render () {
    const data = this.data
    if (!data || !this.visible || this.canvas.width === 0) {
      this.renderScheduled = true
      return
    }
    const candleWidth = data.ms
    const mousePos = this.mousePos
    const allCandles = data.candles || []

    const n = Math.min(this.numToShow, allCandles.length)
    const candles = allCandles.slice(allCandles.length - n)

    this.clear()

    // If there are no candles. just don't draw anything.
    if (n === 0) return

    // padding definition and some helper functions to parse candles.
    const candleWidthPadding = 0.2
    const start = (c: Candle) => truncate(c.endStamp, candleWidth)
    const end = (c: Candle) => start(c) + candleWidth
    const paddedStart = (c: Candle) => start(c) + candleWidthPadding * candleWidth
    const paddedWidth = (1 - 2 * candleWidthPadding) * candleWidth

    const first = candles[0]
    const last = candles[n - 1]

    let [high, low, highVol] = [first.highRate, first.lowRate, first.matchVolume]
    for (const c of candles) {
      if (c.highRate > high) high = c.highRate
      if (c.lowRate < low) low = c.lowRate
      if (c.matchVolume > highVol) highVol = c.matchVolume
    }

    // Calculate data extents and store them. They are used to apply labels.
    const rateStep = this.market.ratestep
    const dataExtents = new Extents(start(first), end(last), low, high)
    if (low === high) {
      // If there is no price movement at all in the window, show a little more
      // top and bottom so things render nicely.
      dataExtents.y.min -= rateStep
      dataExtents.y.max += rateStep
    }
    this.dataExtents = dataExtents

    // Apply labels.
    const rFactor = this.rateConversionFactor
    this.doYLabels(this.candleRegion, rateStep, this.market.quotesymbol, v => Doc.formatFourSigFigs(v / rFactor))
    this.candleRegion.extents.x.min = this.yRegion.extents.x.max
    this.volumeRegion.extents.x.min = this.yRegion.extents.x.max

    const xLabels = makeCandleTimeLabels(candles, candleWidth, this.plotRegion.width(), 100)

    this.plotXLabels(xLabels, start(first), end(last), [])

    this.drawFrame()

    // Highlight the candle if the user mouse is over the canvas.
    let mouseCandle: Candle | null = null
    if (mousePos) {
      this.plotRegion.plot(new Extents(dataExtents.x.min, dataExtents.x.max, 0, 1), (ctx, tools) => {
        const selectedStartStamp = truncate(tools.unx(mousePos.x), candleWidth)
        for (const c of candles) {
          if (start(c) === selectedStartStamp) {
            mouseCandle = c
            ctx.fillStyle = this.theme.gridLines
            ctx.fillRect(tools.x(start(c)), tools.y(0), tools.w(candleWidth), tools.h(1))
            break
          }
        }
      })
      if (mouseCandle) {
        const yExt = this.xRegion.extents.y
        this.xRegion.plot(new Extents(dataExtents.x.min, dataExtents.x.max, yExt.min, yExt.max), (ctx, tools) => {
          if (!mouseCandle) return // For TypeScript. Duh.
          this.applyLabelStyle()
          const rangeTxt = `${new Date(start(mouseCandle)).toLocaleString()} - ${new Date(end(mouseCandle)).toLocaleString()}`
          const [xPad, yPad] = [25, 2]
          const rangeWidth = ctx.measureText(rangeTxt).width + 2 * xPad
          const rangeHeight = 16
          let centerX = tools.x((start(mouseCandle) + end(mouseCandle)) / 2)
          let left = centerX - rangeWidth / 2
          const xExt = this.xRegion.extents.x
          if (left < xExt.min) left = xExt.min
          else if (left + rangeWidth > xExt.max) left = xExt.max - rangeWidth
          centerX = left + rangeWidth / 2
          const top = yExt.min + (this.xRegion.height() - rangeHeight) / 2
          ctx.fillStyle = this.theme.legendFill
          ctx.strokeStyle = this.theme.gridBorder
          const rectArgs: [number, number, number, number] = [left - xPad, top - yPad, rangeWidth + 2 * xPad, rangeHeight + 2 * yPad]
          ctx.fillRect(...rectArgs)
          ctx.strokeRect(...rectArgs)
          this.applyLabelStyle()
          ctx.fillText(rangeTxt, centerX, this.xRegion.extents.midY, rangeWidth)
        })
      }
    }

    // Draw the volume bars.
    const volDataExtents = new Extents(start(first), end(last), 0, highVol)
    this.volumeRegion.plot(volDataExtents, (ctx, tools) => {
      ctx.fillStyle = this.theme.gridBorder
      for (const c of candles) {
        ctx.fillRect(tools.x(paddedStart(c)), tools.y(0), tools.w(paddedWidth), tools.h(c.matchVolume))
      }
    })

    // Draw the candles.
    this.candleRegion.plot(dataExtents, (ctx, tools) => {
      ctx.lineWidth = 1
      for (const c of candles) {
        const desc = c.startRate > c.endRate
        const [x, y, w, h] = [tools.x(paddedStart(c)), tools.y(c.startRate), tools.w(paddedWidth), tools.h(c.endRate - c.startRate)]
        const [high, low, cx] = [tools.y(c.highRate), tools.y(c.lowRate), w / 2 + x]
        ctx.strokeStyle = desc ? this.theme.sellLine : this.theme.buyLine
        ctx.fillStyle = desc ? this.theme.sellFill : this.theme.buyFill

        ctx.beginPath()
        ctx.moveTo(cx, high)
        ctx.lineTo(cx, low)
        ctx.stroke()

        ctx.fillRect(x, y, w, h)
        ctx.strokeRect(x, y, w, h)
      }
    })

    // Report the mouse candle.
    this.reporters.mouse(mouseCandle)
  }

  /* setCandles sets the candle data and redraws the chart. */
  setCandles (data: CandlesPayload, market: Market, baseUnitInfo: UnitInfo, quoteUnitInfo: UnitInfo) {
    this.data = data
    if (!data.candles) return
    this.market = market
    const [qFactor, bFactor] = [quoteUnitInfo.conventional.conversionFactor, baseUnitInfo.conventional.conversionFactor]
    this.rateConversionFactor = RateEncodingFactor * qFactor / bFactor
    let n = 25
    this.zoomLevels = []
    const maxCandles = Math.max(data.candles.length, 1000)
    while (n < maxCandles) {
      this.zoomLevels.push(n)
      n *= 2
    }
    this.numToShow = 100
    this.draw()
  }
}

interface WaveOpts {
  message?: string
  backgroundColor?: string | boolean // true for <body> background color
}

/* Wave is a loading animation that displays a colorful line that oscillates */
export class Wave extends Chart {
  ani: Animation
  size: [number, number]
  region: Region
  colorShift: number
  opts: WaveOpts
  msgRegion: Region
  fontSize: number

  constructor (parent: HTMLElement, opts?: WaveOpts) {
    super(parent, {
      resize: () => this.resized(),
      click: (/* e: MouseEvent */) => { /* pass */ },
      zoom: (/* bigger: boolean */) => { /* pass */ }
    })
    this.canvas.classList.add('fill-abs')
    this.canvas.style.zIndex = '5'

    this.opts = opts ?? {}

    const period = 1500 // ms
    const start = Math.random() * period
    this.colorShift = Math.random() * 360

    // y = A*cos(k*x + theta*t + c)
    // combine three waves with different periods and speeds and phases.
    const amplitudes = [1, 0.65, 0.75]
    const ks = [3, 3, 2]
    const speeds = [Math.PI, Math.PI * 10 / 9, Math.PI / 2.5]
    const phases = [0, 0, Math.PI * 1.5]
    const n = 75
    const single = (n: number, angularX: number, angularTime: number): number => {
      return amplitudes[n] * Math.cos(ks[n] * angularX + speeds[n] * angularTime + phases[n])
    }
    const value = (x: number, angularTime: number): number => {
      const angularX = x * Math.PI * 2
      return (single(0, angularX, angularTime) + single(1, angularX, angularTime) + single(2, angularX, angularTime)) / 3
    }
    this.resize()
    this.ani = new Animation(Animation.Forever, () => {
      const angularTime = (new Date().getTime() - start) / period * Math.PI * 2
      const values = []
      for (let i = 0; i < n; i++) {
        values.push(value(i / (n - 1), angularTime))
      }
      this.drawValues(values)
    })
  }

  resized () {
    const opts = this.opts
    const [maxW, maxH] = [150, 100]
    const [cw, ch] = [this.canvas.width, this.canvas.height]
    let [w, h] = [cw * 0.8, ch * 0.8]
    if (w > maxW) w = maxW
    if (h > maxH) h = maxH
    let [l, t] = [(cw - w) / 2, (ch - h) / 2]
    if (opts.message) {
      this.fontSize = clamp(h * 0.15, 10, 14)
      this.applyLabelStyle(this.fontSize)
      const ypad = this.fontSize * 0.5
      const halfH = (this.fontSize / 2) + ypad
      t -= halfH
      this.msgRegion = new Region(this.ctx, new Extents(0, cw, t + h, t + h + 2 * halfH))
    }
    this.region = new Region(this.ctx, new Extents(l, l + w, t, t + h))
  }

  drawValues (values: number[]) {
    if (!this.region) return
    this.clear()
    const hsl = (h: number) => `hsl(${h}, 35%, 50%)`

    const { region, msgRegion, canvas: { width: w, height: h }, opts: { backgroundColor: bg, message: msg }, colorShift, ctx } = this

    if (bg) {
      if (bg === true) ctx.fillStyle = window.getComputedStyle(document.body, null).getPropertyValue('background-color')
      else ctx.fillStyle = bg
      ctx.fillRect(0, 0, w, h)
    }

    region.plot(new Extents(0, 1, -1, 1), (ctx: CanvasRenderingContext2D, t: Translator) => {
      ctx.lineWidth = 4
      ctx.lineCap = 'round'

      const shift = colorShift + (new Date().getTime() % 2000) / 2000 * 360 // colors move with frequency 1 / 2s
      const grad = ctx.createLinearGradient(t.x(0), 0, t.x(1), 0)
      grad.addColorStop(0, hsl(shift))
      ctx.strokeStyle = grad

      ctx.beginPath()
      ctx.moveTo(t.x(0), t.y(values[0]))
      for (let i = 1; i < values.length; i++) {
        const prog = i / (values.length - 1)
        grad.addColorStop(prog, hsl(prog * 300 + shift))
        ctx.lineTo(t.x(prog), t.y(values[i]))
      }
      ctx.stroke()
    })
    if (!msg) return
    msgRegion.plot(new Extents(0, 1, 0, 1), (ctx: CanvasRenderingContext2D, t: Translator) => {
      this.applyLabelStyle(this.fontSize)
      ctx.fillText(msg, t.x(0.5), t.y(0.5), this.msgRegion.width())
    })
  }

  render () { /* pass */ }

  stop () {
    this.ani.stop()
    this.canvas.remove()
  }
}

/*
 * Extents holds a min and max in both the x and y directions, and provides
 * getters for related data.
 */
class Extents {
  x: MinMax
  y: MinMax

  constructor (xMin: number, xMax: number, yMin: number, yMax: number) {
    this.setExtents(xMin, xMax, yMin, yMax)
  }

  setExtents (xMin: number, xMax: number, yMin: number, yMax: number) {
    this.x = {
      min: xMin,
      max: xMax
    }
    this.y = {
      min: yMin,
      max: yMax
    }
  }

  get xRange (): number {
    return this.x.max - this.x.min
  }

  get midX (): number {
    return (this.x.max + this.x.min) / 2
  }

  get yRange (): number {
    return this.y.max - this.y.min
  }

  get midY (): number {
    return (this.y.max + this.y.min) / 2
  }
}

/*
 * Region applies an Extents to the canvas, providing utilities for coordinate
 * transformations and restricting drawing to a specified region of the canvas.
 */
class Region {
  context: CanvasRenderingContext2D
  extents: Extents

  constructor (context: CanvasRenderingContext2D, extents: Extents) {
    this.context = context
    this.extents = extents
  }

  setExtents (xMin: number, xMax: number, yMin: number, yMax: number) {
    this.extents.setExtents(xMin, xMax, yMin, yMax)
  }

  width (): number {
    return this.extents.xRange
  }

  height (): number {
    return this.extents.yRange
  }

  contains (x: number, y: number): boolean {
    const ext = this.extents
    return (x < ext.x.max && x > ext.x.min &&
      y < ext.y.max && y > ext.y.min)
  }

  /*
   * A translator provides 4 function for coordinate transformations. x and y
   * translate data coordinates to canvas coordinates for the specified data
   * Extents. unx and uny translate canvas coordinates to data coordinates.
   */
  translator (dataExtents: Extents): Translator {
    const region = this.extents
    const xMin = dataExtents.x.min
    // const xMax = dataExtents.x.max
    const yMin = dataExtents.y.min
    // const yMax = dataExtents.y.max
    const yRange = dataExtents.yRange
    const xRange = dataExtents.xRange
    const screenMinX = region.x.min
    const screenW = region.x.max - screenMinX
    const screenMaxY = region.y.max
    const screenH = screenMaxY - region.y.min
    const xFactor = screenW / xRange
    const yFactor = screenH / yRange
    return {
      x: (x: number) => (x - xMin) * xFactor + screenMinX,
      y: (y: number) => screenMaxY - (y - yMin) * yFactor,
      unx: (x: number) => (x - screenMinX) / xFactor + xMin,
      uny: (y: number) => yMin - (y - screenMaxY) / yFactor,
      w: (w: number) => w / xRange * screenW,
      h: (h: number) => -h / yRange * screenH
    }
  }

  /* clear clears the region. */
  clear () {
    const ext = this.extents
    this.context.clearRect(ext.x.min, ext.y.min, ext.xRange, ext.yRange)
  }

  /* plot prepares tools for drawing using data coordinates. */
  plot (dataExtents: Extents, drawFunc: (ctx: CanvasRenderingContext2D, tools: Translator) => void, skipMask?: boolean) {
    const ctx = this.context
    const region = this.extents
    ctx.save() // Save the original state
    if (!skipMask) {
      ctx.beginPath()
      ctx.rect(region.x.min, region.y.min, region.xRange, region.yRange)
      ctx.clip()
    }

    // The drawFunc will be passed a set of tool that can be used to assist
    // drawing. The tools start with the transformation functions.
    const tools = this.translator(dataExtents)

    // Create a transformation that allows drawing in data coordinates. It's
    // not advisable to stroke or add text with this transform in place, as the
    // result will be distorted. You can however use ctx.moveTo and ctx.lineTo
    // with this transform in place using data coordinates, and remove the
    // transform before stroking. The dataCoords method of the supplied tool
    // provides this functionality.

    // TODO: Figure out why this doesn't work on WebView.
    // const yRange = dataExtents.yRange
    // const xFactor = region.xRange / dataExtents.xRange
    // const yFactor = region.yRange / yRange
    // const xMin = dataExtents.x.min
    // const yMin = dataExtents.y.min
    // // These translation factors are complicated because the (0, 0) of the
    // // region is not necessarily the (0, 0) of the canvas.
    // const tx = (region.x.min + xMin) - xMin * xFactor
    // const ty = -region.y.min - (yRange - yMin) * yFactor
    // const setTransform = () => {
    //   // Data coordinates are flipped about y. Flip the coordinates and
    //   // translate top left corner to canvas (0, 0).
    //   ctx.transform(1, 0, 0, -1, -xMin, yMin)
    //   // Scale to data coordinates and shift into place for the region's offset
    //   // on the canvas.
    //   ctx.transform(xFactor, 0, 0, yFactor, tx, ty)
    // }
    // // dataCoords allows some drawing to be performed directly in data
    // // coordinates. Most actual drawing functions like ctx.stroke and
    // // ctx.fillRect should not be called from inside dataCoords, but
    // // ctx.moveTo and ctx.LineTo are fine.
    // tools.dataCoords = f => {
    //   ctx.save()
    //   setTransform()
    //   f()
    //   ctx.restore()
    // }

    drawFunc(this.context, tools)
    ctx.restore()
  }
}

/*
 * makeLabels attempts to create the appropriate labels for the specified
 * screen size, context, and label spacing.
 */
function makeLabels (
  ctx: CanvasRenderingContext2D,
  screenW: number,
  min: number,
  max: number,
  spacingGuess: number,
  step: number,
  unit: string,
  valFmt?: (v: number) => string
): LabelSet {
  valFmt = valFmt || Doc.formatFourSigFigs
  const n = screenW / spacingGuess
  const diff = max - min
  if (n < 1 || diff <= 0) return { lbls: [] }
  const tickGuess = diff / n
  // make the tick spacing a multiple of the step
  const tick = tickGuess + step - (tickGuess % step)
  let x = min + tick - (min % tick)
  const absMax = Math.max(Math.abs(max), Math.abs(min))
  // The Math.round part is the minimum precision required to see the change in the numbers.
  // The 2 accounts for the precision of the tick.
  const sigFigs = Math.round(Math.log10(absMax / tick)) + 2
  const pts: Label[] = []
  let widest = 0
  while (x < max) {
    x = Number(x.toPrecision(sigFigs))
    const lbl = valFmt(x)
    widest = Math.max(widest, ctx.measureText(lbl).width)
    pts.push({
      val: x,
      txt: lbl
    })
    x += tick
  }
  const unitW = ctx.measureText(unit).width
  if (unitW > widest) widest = unitW
  return {
    widest: widest,
    lbls: pts
  }
}

const months = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec']

/* makeCandleTimeLabels prepares labels for candlestick data. */
function makeCandleTimeLabels (candles: Candle[], dur: number, screenW: number, spacingGuess: number): LabelSet {
  const first = candles[0]
  const last = candles[candles.length - 1]
  const start = truncate(first.endStamp, dur)
  const end = truncate(last.endStamp, dur) + dur
  const diff = end - start
  const n = Math.min(candles.length, screenW / spacingGuess)
  const tick = truncate(diff / n, dur)
  if (tick === 0) {
    console.error('zero tick', dur, diff, n) // probably won't happen, but it'd suck if it did
    return { lbls: [] }
  }
  let x = start
  const zoneOffset = new Date().getTimezoneOffset()
  const dayStamp = (x: number) => {
    x = x - zoneOffset * 60000
    return x - (x % 86400000)
  }
  let lastDay = dayStamp(start)
  let lastYear = 0 // new Date(start).getFullYear()
  if (dayStamp(first.endStamp) === dayStamp(last.endStamp)) lastDay = 0 // Force at least one day stamp.
  const pts = []
  let label
  if (dur < 86400000) {
    label = (d: Date, x: number) => {
      const day = dayStamp(x)
      if (day !== lastDay) return `${months[d.getMonth()]}${d.getDate()} ${d.getHours()}:${String(d.getMinutes()).padStart(2, '0')}`
      else return `${d.getHours()}:${String(d.getMinutes()).padStart(2, '0')}`
    }
  } else {
    label = (d: Date) => {
      const year = d.getFullYear()
      if (year !== lastYear) return `${months[d.getMonth()]}${d.getDate()} '${String(year).slice(2, 4)}`
      else return `${months[d.getMonth()]}${d.getDate()}`
    }
  }
  while (x <= end) {
    const d = new Date(x)
    pts.push({
      val: x,
      txt: label(d, x)
    })
    lastDay = dayStamp(x)
    lastYear = d.getFullYear()
    x += tick
  }
  return { lbls: pts }
}

/* The last element of an array. */
function last (arr: any[]): any {
  return arr[arr.length - 1]
}

/* line draws a line with the provided context. */
function line (ctx: CanvasRenderingContext2D, x0: number, y0: number, x1: number, y1: number, skipStroke?: boolean) {
  ctx.beginPath()
  ctx.moveTo(x0, y0)
  ctx.lineTo(x1, y1)
  if (!skipStroke) ctx.stroke()
}

/* dot draws a circle with the provided context. */
function dot (ctx: CanvasRenderingContext2D, x: number, y: number, color: string, radius: number) {
  ctx.fillStyle = color
  ctx.beginPath()
  ctx.arc(x, y, radius, 0, PIPI)
  ctx.fill()
}

/* clamp returns v if min <= v <= max, else min or max. */
function clamp (v: number, min: number, max: number): number {
  if (v < min) return min
  if (v > max) return max
  return v
}

/* floatCompare compares two floats to within a tolerance of  1e-8. */
function floatCompare (a: number, b: number) {
  return withinTolerance(a, b, 1e-8)
}

/*
 * withinTolerance returns true if the difference between a and b are with
 * the specified tolerance.
 */
function withinTolerance (a: number, b: number, tolerance: number) {
  return Math.abs(a - b) < Math.abs(tolerance)
}

function truncate (v: number, w: number): number {
  return v - (v % w)
}
