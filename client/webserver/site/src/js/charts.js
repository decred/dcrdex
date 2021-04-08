import Doc from './doc'
import State from './state'

const bind = Doc.bind
const unbind = Doc.unbind
const PIPI = 2 * Math.PI
const plusChar = String.fromCharCode(59914)
const minusChar = String.fromCharCode(59915)

const darkTheme = {
  axisLabel: '#b1b1b1',
  gridBorder: '#3a3a3a',
  gridLines: '#2a2a2a',
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

const lightTheme = {
  axisLabel: '#1b1b1b',
  gridBorder: '#3a3a3a',
  gridLines: '#dadada',
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

// DepthChart is a javascript Canvas-based depth chart renderer.
export class DepthChart {
  constructor (parent, reporters, zoom) {
    this.theme = State.isDark() ? darkTheme : lightTheme
    this.canvas = document.createElement('canvas')
    this.parent = parent
    this.reporters = reporters
    this.ctx = this.canvas.getContext('2d')
    this.ctx.textAlign = 'center'
    this.ctx.textBaseline = 'middle'
    this.book = null
    this.dataExtents = null
    this.zoomLevel = zoom
    this.lotSize = null
    this.rateStep = null
    this.lines = []
    this.markers = {
      buys: [],
      sells: []
    }
    parent.appendChild(this.canvas)
    // Mouse handling
    this.mousePos = null
    bind(this.canvas, 'mousemove', e => {
      // this.rect will be set in resize().
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
    // Scrolling by wheel is smoother when the rate is slightly limited.
    this.wheelLimiter = null
    this.wheeled = () => {
      this.wheelLimiter = setTimeout(() => { this.wheelLimiter = null }, 100)
    }

    bind(this.canvas, 'wheel', e => { this.wheel(e) })
    this.resize = h => { this.resize_(h) }
    bind(window, 'resize', () => { this.resize(parent.clientHeight) })
    bind(this.canvas, 'click', e => { this.click(e) })
    this.resize(parent.clientHeight)
  }

  // The market handler will call unattach when the markets page is unloaded.
  unattach () {
    unbind(window, 'resize', this.resize)
  }

  // resize_ is a 'resize' event handler.
  resize_ (parentHeight) {
    this.canvas.width = this.parent.clientWidth
    this.canvas.height = parentHeight - 20 // magic number derived from a soup of css values.
    const xLblHeight = 30
    const yGuess = 40 // y label width guess. Will be adjusted when drawn.
    const plotExtents = new Extents(yGuess, this.canvas.width, 10, this.canvas.height - xLblHeight)
    const xLblExtents = new Extents(yGuess, this.canvas.width, this.canvas.height - xLblHeight, this.canvas.height)
    const yLblExtents = new Extents(0, yGuess, 10, this.canvas.height - xLblHeight)
    this.plotRegion = new Region(this.ctx, plotExtents)
    this.xRegion = new Region(this.ctx, xLblExtents)
    this.yRegion = new Region(this.ctx, yLblExtents)
    // The button region extents are set during drawing.
    this.zoomInBttn = new Region(this.ctx, new Extents(0, 0, 0, 0))
    this.zoomOutBttn = new Region(this.ctx, new Extents(0, 0, 0, 0))
    this.rect = this.canvas.getBoundingClientRect()
    if (this.book) this.draw()
  }

  // wheel is a mousewheel event handler.
  wheel (e) {
    this.zoom(e.deltaY < 0)
  }

  // zoom zooms the current view in or out. bigger=true is zoom in.
  zoom (bigger) {
    if (!this.zoomLevel) return
    if (this.wheelLimiter) return
    if (!this.book.buys || !this.book.sells) return
    this.wheeled()
    // Zoom in to 66%, but out to 150% = 1 / (2/3) so that the same zoom levels
    // are hit when reversing direction.
    this.zoomLevel *= bigger ? 2 / 3 : 3 / 2
    this.zoomLevel = clamp(this.zoomLevel, 0.005, 2)
    this.draw()
    this.reporters.zoom(this.zoomLevel)
  }

  // click is the canvas 'click' event handler.
  click (e) {
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
  set (book, lotSize, rateStep) {
    this.book = book
    this.lotSize = lotSize / 1e8
    this.rateStep = rateStep / 1e8
    this.baseTicker = book.baseSymbol.toUpperCase()
    this.quoteTicker = book.quoteSymbol.toUpperCase()
    if (!this.zoomLevel) {
      const [midGap, gapWidth] = this.gap()
      // Default to 5% zoom, but with a minimum of 5 * midGap, but still observing
      // the hard cap of 200%.
      const minZoom = Math.max(gapWidth / midGap * 5, 0.05)
      this.zoomLevel = Math.min(minZoom, 2)
    }
    this.draw()
  }

  // Draw the chart.
  // 1. Calculate the data extents and translate the order book data to a
  //    cumulative form.
  // 2. Draw axis ticks and grid, mid-gap line and value, zoom buttons, mouse
  //    position indicator...
  // 4. Tick labels.
  // 5. Data.
  // 6. Epoch line legend.
  // 7. Hover legend.
  draw () {
    this.clear()
    // if (!this.book || this.book.empty()) return

    this.ctx.textAlign = 'center'
    this.ctx.textBaseline = 'middle'
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
    const markers = []

    const buyDepth = []
    const buyEpoch = []
    const sellDepth = []
    const sellEpoch = []
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

    // Draw the axis tick labels.
    const ctx = this.ctx
    ctx.font = '12px \'sans\', sans-serif'
    ctx.fillStyle = this.theme.axisLabel

    const yLabels = makeLabels(ctx, this.plotRegion.height(), dataExtents.y.min,
      dataExtents.y.max, 50, this.lotSize, this.book.baseSymbol)
    // Reassign the width of the y-label column to accommodate the widest text.
    const yAxisWidth = yLabels.widest * 1.5
    this.yRegion.extents.x.max = yAxisWidth
    this.plotRegion.extents.x.min = yAxisWidth
    this.xRegion.extents.x.min = yAxisWidth
    const xLabels = makeLabels(ctx, this.plotRegion.width(), dataExtents.x.min,
      dataExtents.x.max, 100, this.rateStep, `${this.book.quoteSymbol}/${this.book.baseSymbol}`)

    // A function to be run at the end if there is legend data to display.
    let mouseData

    // Draw the grid.
    ctx.lineWidth = 1
    this.plotRegion.plot(dataExtents, (ctx, tools) => {
      // first, a square around the plot area.
      ctx.strokeStyle = this.theme.gridBorder
      const extX = dataExtents.x
      const extY = dataExtents.y
      ctx.beginPath()
      tools.dataCoords(() => {
        ctx.moveTo(extX.min, extY.min)
        ctx.lineTo(extX.min, extY.max)
        ctx.lineTo(extX.max, extY.max)
        ctx.lineTo(extX.max, extY.min)
        ctx.lineTo(extX.min, extY.min)
      })
      ctx.stroke()
      // for each x label, draw a vertical line
      ctx.strokeStyle = this.theme.gridLines
      xLabels.lbls.forEach(lbl => {
        line(ctx, tools.x(lbl.val), tools.y(0), tools.x(lbl.val), tools.y(extY.max))
      })
      // horizontal lines for y labels.
      yLabels.lbls.forEach(lbl => {
        line(ctx, tools.x(extX.min), tools.y(lbl.val), tools.x(extX.max), tools.y(lbl.val))
      })
      // draw a line to indicate mid-gap
      ctx.lineWidth = 2.5
      ctx.strokeStyle = this.theme.gapLine
      line(ctx, tools.x(midGap), tools.y(0), tools.x(midGap), tools.y(0.3 * extY.max))

      ctx.font = '30px \'demi-sans\', sans-serif'
      ctx.fillStyle = this.theme.value
      const y = 0.5 * extY.max
      ctx.fillText(formatLabelValue(midGap), tools.x(midGap), tools.y(y))
      ctx.font = '12px \'sans\', sans-serif'
      // ctx.fillText('mid-market price', tools.x(midGap), tools.y(y) + 24)
      ctx.fillText(`${(gapWidth / midGap * 100).toFixed(2)}% spread`,
        tools.x(midGap), tools.y(y) + 24)

      // Draw zoom buttons.
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
      this.zoomOutBttn.plot(new Extents(0, 1, 0, 1), (ctx, tools) => {
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
      this.zoomInBttn.plot(new Extents(0, 1, 0, 1), (ctx, tools) => {
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
      const drawLine = (x, color) => {
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
      let trigger = (ptX) => ptX >= dataX
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
      drawLine(dataX, this.theme.crosshairs, true)
      mouseData = {
        rate: dataX,
        depth: bestDepth[1],
        dotColor: dotColor,
        yAxisWidth: yAxisWidth,
        hoverMarkers: hoverMarkers
      }
    })

    // Print the y labels.
    this.yRegion.plot(new Extents(0, 1, 0, maxY), (ctx, tools) => {
      const centerY = maxY / 2
      let lastY = 0
      let unitCenter = centerY
      yLabels.lbls.forEach(lbl => {
        ctx.fillText(lbl.txt, tools.x(0.5), tools.y(lbl.val))
        if (centerY >= lastY && centerY < lbl.val) {
          unitCenter = (lastY + lbl.val) / 2
        }
        lastY = lbl.val
      })
      ctx.fillText(this.baseTicker, tools.x(0.5), tools.y(unitCenter))
    }, true)

    // Print the x labels
    this.xRegion.plot(new Extents(low, high, 0, 1), (ctx, tools) => {
      const centerX = (high + low) / 2
      let lastX = low
      let unitCenter = centerX
      xLabels.lbls.forEach(lbl => {
        ctx.fillText(lbl.txt, tools.x(lbl.val), tools.y(0.5))
        if (centerX >= lastX && centerX < lbl.val) {
          unitCenter = (lastX + lbl.val) / 2
        }
        lastX = lbl.val
      })
      ctx.font = '11px \'sans\', sans-serif'
      ctx.fillText(`${this.quoteTicker}/`, tools.x(unitCenter), tools.y(0.63))
      ctx.fillText(this.baseTicker, tools.x(unitCenter), tools.y(0.23))
    }, true)

    // Draw the epoch lines
    this.ctx.lineWidth = 1.5
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
    this.ctx.lineWidth = 2.5
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
        dot(ctx, tools.x(mouseData.rate), tools.y(mouseData.depth), mouseData.dotColor, 5)
      })
    }

    // Report the book volumes.
    this.reporters.volume(volumeReport)
    this.reporters.mouse(mouseData)
  }

  // Draw a single side's depth chart data.
  drawDepth (depth) {
    const firstPt = depth[0]
    let y = firstPt[1]
    let x
    this.plotRegion.plot(this.dataExtents, (ctx, tools) => {
      tools.dataCoords(() => {
        ctx.beginPath()
        ctx.moveTo(firstPt[0], firstPt[1])
        for (let i = 0; i < depth.length; i++) {
          // Set x, but don't set y until we draw the horizontal line.
          x = depth[i][0]
          ctx.lineTo(x, y)
          // If this is past the render edge, quit drawing.
          y = depth[i][1]
          ctx.lineTo(x, y)
        }
      })
      ctx.stroke()
      tools.dataCoords(() => {
        ctx.lineTo(x, 0)
        ctx.lineTo(firstPt[0], 0)
      })
      ctx.closePath()
      ctx.globalAlpha = 0.25
      ctx.fill()
    })
  }

  gap () {
    const [b, s] = [this.book.bestGapBuy(), this.book.bestGapSell()]
    if (!b) {
      if (!s) return [1, 0]
      return [s.rate, 0]
    } else if (!s) return [b.rate, 0]
    return [(s.rate + b.rate) / 2, s.rate - b.rate]
  }

  setLines (lines) {
    this.lines = lines
  }

  setMarkers (markers) {
    this.markers = markers
  }
}

// Extents holds a min and max in both the x and y directions, and provides
// getters for related data.
class Extents {
  constructor (xMin, xMax, yMin, yMax) {
    this.setExtents(xMin, xMax, yMin, yMax)
  }

  setExtents (xMin, xMax, yMin, yMax) {
    this.x = {
      min: xMin,
      max: xMax
    }
    this.y = {
      min: yMin,
      max: yMax
    }
  }

  get xRange () {
    return this.x.max - this.x.min
  }

  get midX () {
    return (this.x.max + this.x.min) / 2
  }

  get yRange () {
    return this.y.max - this.y.min
  }

  get midY () {
    return (this.y.max + this.y.min) / 2
  }
}

// Region applies an Extents to the canvas, providing utilities for coordinate
// transformations and restricting drawing to a specified region of the canvas.
class Region {
  constructor (context, extents) {
    this.context = context
    this.extents = extents
  }

  setExtents (xMin, xMax, yMin, yMax) {
    this.extents.setExtents(xMin, xMax, yMin, yMax)
  }

  width () {
    return this.extents.xRange
  }

  height () {
    return this.extents.yRange
  }

  contains (x, y) {
    const ext = this.extents
    return (x < ext.x.max && x > ext.x.min &&
      y < ext.y.max && y > ext.y.min)
  }

  // A translator provides 4 function for coordinate transformations. x and y
  // translate data coordinates to canvas coordinates for the specified data
  // Extents. unx and uny translate canvas coordinates to data coordinates.
  translator (dataExtents) {
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
      x: x => (x - xMin) * xFactor + screenMinX,
      y: y => screenMaxY - (y - yMin) * yFactor,
      unx: x => (x - screenMinX) / xFactor + xMin,
      uny: y => yMin - (y - screenMaxY) / yFactor
    }
  }

  // Clear the region.
  clear () {
    const ext = this.extents
    this.ctx.clearRect(ext.x.min, ext.y.min, ext.xRange, ext.yRange)
  }

  // plot allows some drawing to be performed directly in data coordinates.
  // Most actual drawing functions like ctx.stroke and ctx.fillRect should not
  // be called from inside the provided drawFunc, but ctx.moveTo and ctx.LineTo
  // are fine.
  plot (dataExtents, drawFunc, skipMask) {
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
    const yRange = dataExtents.yRange
    const xFactor = region.xRange / dataExtents.xRange
    const yFactor = region.yRange / yRange
    const xMin = dataExtents.x.min
    const yMin = dataExtents.y.min
    // These translation factors are complicated because the (0, 0) of the
    // region is not necessarily the (0, 0) of the canvas.
    const tx = (region.x.min + xMin) - xMin * xFactor
    const ty = -region.y.min - (yRange - yMin) * yFactor
    const setTransform = () => {
      // Data coordinates are flipped about y. Flip the coordinates and
      // translate top left corner to canvas (0, 0).
      ctx.transform(1, 0, 0, -1, -xMin, yMin)
      // Scale to data coordinates and shift into place for the region's offset
      // on the canvas.
      ctx.transform(xFactor, 0, 0, yFactor, tx, ty)
    }
    // Provide drawCoords as a tool to enable inline drawing.
    tools.dataCoords = f => {
      ctx.save()
      setTransform()
      f()
      ctx.restore()
    }

    drawFunc(this.context, tools)
    ctx.restore()
  }
}

// makeLabels attempts to create the appropriate labels for the specified
// screen size, context, and label spacing.
function makeLabels (ctx, screenW, min, max, spacingGuess, step, unit) {
  const n = screenW / spacingGuess
  const diff = max - min
  const tickGuess = diff / n
  // make the tick spacing a multiple of the step
  const tick = tickGuess + step - (tickGuess % step)
  let x = min + tick - (min % tick)
  const absMax = Math.max(Math.abs(max), Math.abs(min))
  // The Math.round part is the minimum precision required to see the change in the numbers.
  // The 2 accounts for the precision of the tick.
  const sigFigs = Math.round(Math.log10(absMax / tick)) + 2
  const pts = []
  let widest = 0
  while (x < max) {
    x = Number(x.toPrecision(sigFigs))
    const lbl = formatLabelValue(x)
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

// The last element of an array.
function last (arr) {
  return arr[arr.length - 1]
}

// line draws a line with the provided context.
function line (ctx, x0, y0, x1, y1, skipStroke) {
  ctx.beginPath()
  ctx.moveTo(x0, y0)
  ctx.lineTo(x1, y1)
  if (!skipStroke) ctx.stroke()
}

// dot draws a circle with the provided context.
function dot (ctx, x, y, color, radius) {
  ctx.fillStyle = color
  ctx.beginPath()
  ctx.arc(x, y, radius, 0, PIPI)
  ctx.fill()
}

function clamp (v, min, max) {
  if (v < min) return min
  if (v > max) return max
  return v
}

// labelSpecs is specifications for axis tick labels.
const labelSpecs = {
  minimumSignificantDigits: 4,
  maximumSignificantDigits: 5
}

// formatLabelValue formats the provided value using the labelSpecs format.
function formatLabelValue (x) {
  return x.toLocaleString('en-us', labelSpecs)
}

function floatCompare (a, b) {
  return withinTolerance(a, b, 1e-8)
}

function withinTolerance (a, b, tolerance) {
  return Math.abs(a - b) < Math.abs(tolerance)
}
