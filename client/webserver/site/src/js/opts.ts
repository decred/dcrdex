import Doc from './doc'
import {
  PageElement,
  XYRange,
  OrderOption
} from './registry'

interface OptionsReporters {
  enable: () => void
  disable: () => void
}

// Having the caller set these vars on load using an exported function makes
// life easier.
let orderOptTmpl: HTMLElement, booleanOptTmpl: HTMLElement, rangeOptTmpl: HTMLElement

// setOptionTemplates sets the package vars for the templates and application.
export function setOptionTemplates (page: Record<string, PageElement>): void {
  [booleanOptTmpl, rangeOptTmpl, orderOptTmpl] = [page.booleanOptTmpl, page.rangeOptTmpl, page.orderOptTmpl]
}

const threeSigFigs = new Intl.NumberFormat((navigator.languages as string[]), {
  minimumSignificantDigits: 3,
  maximumSignificantDigits: 3
})

/*
 * Option is a base class for option elements. Option stores some common
 * parameters and monitors the toggle switch, calling the child class's
 * enable/disable methods when the user manually turns the option on or off.
 */
export class Option {
  opt: OrderOption
  node: HTMLElement
  tmpl: Record<string, PageElement>
  on: boolean

  constructor (opt: OrderOption, symbol: string, report: OptionsReporters) {
    this.opt = opt
    const node = this.node = orderOptTmpl.cloneNode(true) as HTMLElement
    const tmpl = this.tmpl = Doc.parseTemplate(node)

    tmpl.optName.textContent = opt.displayname
    tmpl.tooltip.dataset.tooltip = opt.description

    // const isBaseChain = (isSwapOption && order.sell) || (!isSwapOption && !order.sell)
    // const symbol = isBaseChain ? this.baseSymbol() : this.quoteSymbol()
    if (symbol) tmpl.chainIcon.src = Doc.logoPath(symbol)
    else Doc.hide(tmpl.chainIcon)

    this.on = false
    Doc.bind(node, 'click', () => {
      if (this.on) return
      this.on = true
      node.classList.add('selected')
      report.enable()
    })
    Doc.bind(tmpl.toggle, 'click', e => {
      if (!this.on) return
      e.stopPropagation()
      this.on = false
      node.classList.remove('selected')
      report.disable()
    })
  }
}

/*
 * BooleanOption is a simple on/off option with a short summary of it's effects.
 * BooleanOrderOption is the handler for a *BooleanConfig from client/asset.
 */
export class BooleanOption extends Option {
  control: HTMLElement
  changed: () => void
  dict: Record<string, any>

  constructor (opt: OrderOption, symbol: string, dict: Record<string, any>, changed: () => void) {
    super(opt, symbol, {
      enable: () => this.enable(),
      disable: () => this.disable()
    })
    this.dict = dict
    this.changed = () => changed()
    if (opt.boolean === undefined) throw Error('not a boolean opt')
    const cfg = opt.boolean
    const control = this.control = booleanOptTmpl.cloneNode(true) as HTMLElement
    // Append to parent's options div.
    this.tmpl.controls.appendChild(control)
    const tmpl = Doc.parseTemplate(control)
    tmpl.reason.textContent = cfg.reason
    this.on = typeof dict[opt.key] !== 'undefined' ? dict[opt.key] : opt.default
    if (this.on) this.node.classList.add('selected')
  }

  store (): void {
    if (this.on === this.opt.default) delete this.dict[this.opt.key]
    else this.dict[this.opt.key] = this.on
    this.changed()
  }

  enable (): void {
    this.store()
  }

  disable (): void {
    this.store()
  }
}

/*
 * XYRangeOption is an order option that contains an XYRangeHandler. The logic
 * for handling the slider to is defined in XYRangeHandler so that the slider
 * can be used without being contained in an order option.
 */
export class XYRangeOption extends Option {
  handler: XYRangeHandler
  x: number
  changed: () => void
  dict: Record<string, any>

  constructor (opt: OrderOption, symbol: string, dict: Record<string, any>, changed: () => void) {
    super(opt, symbol, {
      enable: () => this.enable(),
      disable: () => this.disable()
    })
    this.dict = dict
    this.changed = changed
    if (opt.xyRange === undefined) throw Error('not an xy range opt')
    const cfg = opt.xyRange
    const setVal = dict[opt.key]
    this.on = typeof setVal !== 'undefined'
    if (this.on) {
      this.node.classList.add('selected')
      this.x = setVal
    } else {
      this.x = opt.default
    }
    const selected = () => { this.node.classList.add('selected') }
    this.handler = new XYRangeHandler(cfg, this.x, { changed, selected, settingsDict: dict, settingsKey: opt.key })
    this.tmpl.controls.appendChild(this.handler.control)
  }

  enable (): void {
    this.dict[this.opt.key] = this.x
    this.changed()
  }

  disable (): void {
    delete this.dict[this.opt.key]
    this.changed()
  }

  setValue (x: number): void {
    this.handler.setValue(x)
    this.on = true
    this.node.classList.add('selected')
  }
}

interface AcceptOpts {
  skipChange?: boolean
  skipUpdate?: boolean // Implies skipChange
}

interface RangeHandlerOpts {
  roundY?: boolean
  roundX?: boolean
  updated?: (x:number, y:number) => void, // fires while dragging.
  changed?: () => void, // does not fire while dragging but does when dragging ends.
  selected?: () => void,
  disabled?: boolean
  settingsDict?: {[key: string]: any}
  settingsKey?: string
  dictValueAsString?: boolean
  convert?: (x: any) => any
}

/*
 * XYRangeHandler is the handler for an *XYRange from client/asset. XYRange
 * has a slider which allows adjusting the x and y, linearly between two limits.
 * The user can also manually enter values for x or y.
 */
export class XYRangeHandler {
  control: HTMLElement
  range: XYRange
  tmpl: Record<string, PageElement>
  initVal: number
  settingsDict?: {[key: string]: any}
  settingsKey: string
  x: number
  scrollingX: number
  y: number
  r: number
  roundX: boolean
  roundY: boolean
  disabled: boolean
  updated: (x:number, y:number) => void
  changed: () => void
  selected: () => void
  convert: (x: number) => any

  constructor (
    range: XYRange,
    initVal: number,
    opts: RangeHandlerOpts
  ) {
    const control = this.control = rangeOptTmpl.cloneNode(true) as HTMLElement
    const tmpl = this.tmpl = Doc.parseTemplate(control)
    tmpl.rangeLblStart.textContent = range.start.label
    tmpl.rangeLblEnd.textContent = range.end.label
    tmpl.xUnit.textContent = range.xUnit
    tmpl.yUnit.textContent = range.yUnit
    this.range = range
    this.initVal = initVal
    this.settingsDict = opts.settingsDict
    this.settingsKey = opts.settingsKey ?? ''
    this.roundX = Boolean(opts.roundX)
    this.roundY = Boolean(opts.roundY)

    this.setDisabled(Boolean(opts.disabled))
    this.changed = opts.changed ?? (() => { /* pass */ })
    this.selected = opts.selected ?? (() => { /* pass */ })
    this.updated = opts.updated ?? (() => { /* pass */ })
    this.convert = opts.dictValueAsString ? (x: number) => String(x) : (x: number) => x

    const { slider, handle } = tmpl
    const rangeX = range.end.x - range.start.x
    const rangeY = range.end.y - range.start.y
    const normalizeX = (x: number) => (x - range.start.x) / rangeX

    // r, x, and y will be updated by the various input event handlers. r is
    // x (or y) normalized on its range, e.g. [x_min, x_max] -> [0, 1]
    this.r = normalizeX(initVal)
    this.scrollingX = this.x = initVal
    this.y = this.r * rangeY + range.start.y
    this.accept(this.scrollingX, { skipUpdate: true })

    // Set up the handlers for the x and y text input fields.
    const clickOutX = (e: MouseEvent) => {
      if (this.disabled) return
      if (e.type !== 'change' && e.target === tmpl.xInput) return
      const s = tmpl.xInput.value
      if (s) {
        const xx = parseFloat(s)
        if (!isNaN(xx)) {
          this.scrollingX = clamp(xx, range.start.x, range.end.x)
          this.r = normalizeX(this.scrollingX)
          this.y = this.r * rangeY + range.start.y
          this.accept(this.scrollingX)
        }
      }
      Doc.hide(tmpl.xInput)
      Doc.show(tmpl.x)
      Doc.unbind(document, 'click', clickOutX)
      this.changed()
    }

    Doc.bind(tmpl.x, 'click', e => {
      if (this.disabled) return
      Doc.hide(tmpl.x)
      Doc.show(tmpl.xInput)
      tmpl.xInput.focus()
      tmpl.xInput.value = threeSigFigs.format(this.scrollingX)
      Doc.bind(document, 'click', clickOutX)
      e.stopPropagation()
    })

    Doc.bind(tmpl.xInput, 'change', clickOutX)

    const clickOutY = (e: MouseEvent) => {
      if (this.disabled) return
      if (e.type !== 'change' && e.target === tmpl.yInput) return
      const s = tmpl.yInput.value
      if (s) {
        const yy = parseFloat(s)
        if (!isNaN(yy)) {
          this.y = clamp(yy, range.start.y, range.end.y)
          this.r = (this.y - range.start.y) / rangeY
          this.scrollingX = range.start.x + this.r * rangeX
          this.accept(this.scrollingX)
        }
      }
      Doc.hide(tmpl.yInput)
      Doc.show(tmpl.y)
      Doc.unbind(document, 'click', clickOutY)
      this.changed()
    }

    Doc.bind(tmpl.y, 'click', e => {
      if (this.disabled) return
      Doc.hide(tmpl.y)
      Doc.show(tmpl.yInput)
      tmpl.yInput.focus()
      tmpl.yInput.value = threeSigFigs.format(this.y)
      Doc.bind(document, 'click', clickOutY)
      e.stopPropagation()
    })

    Doc.bind(tmpl.yInput, 'change', clickOutY)

    // Read the slider.
    Doc.bind(handle, 'mousedown', (e: MouseEvent) => {
      if (this.disabled) return
      if (e.button !== 0) return
      e.preventDefault()
      e.stopPropagation()
      this.selected()
      const startX = e.pageX
      const w = slider.clientWidth - handle.offsetWidth
      const startLeft = normalizeX(this.scrollingX) * w
      const left = (ee: MouseEvent) => Math.max(Math.min(startLeft + (ee.pageX - startX), w), 0)
      const trackMouse = (ee: MouseEvent, emit?: boolean) => {
        ee.preventDefault()
        this.r = left(ee) / w
        this.scrollingX = this.r * rangeX + range.start.x
        this.y = this.r * rangeY + range.start.y
        this.accept(this.scrollingX, { skipChange: !emit })
      }
      const mouseUp = (ee: MouseEvent) => {
        trackMouse(ee, true)
        Doc.unbind(document, 'mousemove', trackMouse)
        Doc.unbind(document, 'mouseup', mouseUp)
        this.changed()
      }
      Doc.bind(document, 'mousemove', trackMouse)
      Doc.bind(document, 'mouseup', mouseUp)
    })

    Doc.bind(tmpl.sliderBox, 'click', (e: MouseEvent) => {
      if (this.disabled) return
      if (e.button !== 0) return
      const x = e.pageX
      const m = Doc.layoutMetrics(tmpl.slider)
      this.r = clamp((x - m.bodyLeft) / m.width, 0, 1)
      this.scrollingX = this.r * rangeX + range.start.x
      this.y = this.r * rangeY + range.start.y
      this.accept(this.scrollingX)
    })
  }

  setDisabled (disabled: boolean) {
    this.control.classList.toggle('disabled', disabled)
    this.disabled = disabled
  }

  setXLabel (s: string) {
    this.tmpl.x.textContent = s
  }

  setYLabel (s: string) {
    this.tmpl.y.textContent = s
  }

  accept (x: number, cfg?: AcceptOpts): void {
    const tmpl = this.tmpl
    if (this.roundX) x = Math.round(x)
    if (this.roundY) this.y = Math.round(this.y)
    tmpl.x.textContent = threeSigFigs.format(x)
    tmpl.y.textContent = threeSigFigs.format(this.y)
    if (this.roundY) tmpl.y.textContent = `${this.y}`
    const rEffective = clamp(this.r, 0, 1)
    tmpl.handle.style.left = `calc(${rEffective * 100}% - ${rEffective * 14}px)`
    this.x = x
    this.scrollingX = x
    cfg = cfg ?? {}
    if (this.settingsDict) this.settingsDict[this.settingsKey] = this.convert(this.x)
    if (!cfg.skipUpdate) {
      this.updated(x, this.y)
      if (!cfg.skipChange) this.changed()
    }
  }

  setValue (x: number, skipUpdate?: boolean) {
    const range = this.range
    this.r = (x - range.start.x) / (range.end.x - range.start.x)
    this.y = range.start.y + this.r * (range.end.y - range.start.y)
    this.accept(x, { skipUpdate })
  }

  modified (): boolean {
    return this.x !== this.initVal
  }

  reset () {
    this.setValue(this.initVal, true)
  }
}

const clamp = (v: number, min: number, max: number): number => v < min ? min : v > max ? max : v
