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

const threeSigFigs = new Intl.NumberFormat(Doc.languages(), {
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
    const onUpdate = (x: number) => {
      this.x = x
      this.dict[this.opt.key] = x
    }
    const onChange = () => { this.changed() }
    const selected = () => { this.node.classList.add('selected') }
    this.handler = new XYRangeHandler(cfg, this.x, onUpdate, onChange, selected)
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

/*
 * XYRangeHandler is the handler for an *XYRange from client/asset. XYRange
 * has a slider which allows adjusting the x and y, linearly between two limits.
 * The user can also manually enter values for x or y.
 */
export class XYRangeHandler {
  control: HTMLElement
  cfg: XYRange
  tmpl: Record<string, PageElement>
  x: number
  scrollingX: number
  y: number
  r: number
  roundY: boolean
  updated: (x:number, y:number) => void
  changed: () => void
  selected: () => void
  setConfig: (cfg: XYRange) => void

  constructor (cfg: XYRange, initVal: number, updated: (x:number, y:number) => void, changed: () => void, selected: () => void, roundY?: boolean) {
    const control = this.control = rangeOptTmpl.cloneNode(true) as HTMLElement
    const tmpl = this.tmpl = Doc.parseTemplate(control)
    this.roundY = Boolean(roundY)
    this.cfg = cfg

    this.changed = changed
    this.selected = selected
    this.updated = updated

    const { slider, handle } = tmpl

    let rangeX = cfg.end.x - cfg.start.x
    let rangeY = cfg.end.y - cfg.start.y
    const normalizeX = (x: number) => (x - cfg.start.x) / rangeX

    const setConfig = (newCfg: XYRange) => {
      rangeX = newCfg.end.x - newCfg.start.x
      rangeY = newCfg.end.y - newCfg.start.y
      cfg = this.cfg = newCfg
      tmpl.rangeLblStart.textContent = cfg.start.label
      tmpl.rangeLblEnd.textContent = cfg.end.label
      tmpl.xUnit.textContent = cfg.xUnit
      tmpl.yUnit.textContent = cfg.yUnit
      this.y = this.r * rangeY + cfg.start.y
      this.r = (this.y - cfg.start.y) / rangeY
      this.scrollingX = this.r * rangeX + cfg.start.x
    }
    setConfig(cfg)

    this.setConfig = (cfg: XYRange) => {
      setConfig(cfg)
      this.accept(this.scrollingX)
    }

    // r, x, and y will be updated by the various input event handlers. r is
    // x (or y) normalized on its range, e.g. [x_min, x_max] -> [0, 1]
    this.r = normalizeX(initVal)
    this.scrollingX = this.x = initVal
    this.y = this.r * rangeY + cfg.start.y

    // Set up the handlers for the x and y text input fields.
    const clickOutX = (e: MouseEvent) => {
      if (e.type !== 'change' && e.target === tmpl.xInput) return
      const s = tmpl.xInput.value
      if (s) {
        const xx = parseFloat(s)
        if (!isNaN(xx)) {
          this.scrollingX = clamp(xx, cfg.start.x, cfg.end.x)
          this.r = normalizeX(this.scrollingX)
          this.y = this.r * rangeY + cfg.start.y
          this.accept(this.scrollingX)
        }
      }
      Doc.hide(tmpl.xInput)
      Doc.show(tmpl.x)
      Doc.unbind(document, 'click', clickOutX)
      this.changed()
    }

    Doc.bind(tmpl.x, 'click', e => {
      Doc.hide(tmpl.x)
      Doc.show(tmpl.xInput)
      tmpl.xInput.focus()
      tmpl.xInput.value = threeSigFigs.format(this.scrollingX)
      Doc.bind(document, 'click', clickOutX)
      e.stopPropagation()
    })

    Doc.bind(tmpl.xInput, 'change', clickOutX)

    const clickOutY = (e: MouseEvent) => {
      if (e.type !== 'change' && e.target === tmpl.yInput) return
      const s = tmpl.yInput.value
      if (s) {
        const yy = parseFloat(s)
        if (!isNaN(yy)) {
          this.y = clamp(yy, cfg.start.y, cfg.end.y)
          this.r = (this.y - cfg.start.y) / rangeY
          this.scrollingX = cfg.start.x + this.r * rangeX
          this.accept(this.scrollingX)
        }
      }
      Doc.hide(tmpl.yInput)
      Doc.show(tmpl.y)
      Doc.unbind(document, 'click', clickOutY)
      this.changed()
    }

    Doc.bind(tmpl.y, 'click', e => {
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
      if (e.button !== 0) return
      e.preventDefault()
      this.selected()
      const startX = e.pageX
      const w = slider.clientWidth - handle.offsetWidth
      const startLeft = normalizeX(this.scrollingX) * w
      const left = (ee: MouseEvent) => Math.max(Math.min(startLeft + (ee.pageX - startX), w), 0)
      const trackMouse = (ee: MouseEvent) => {
        ee.preventDefault()
        this.r = left(ee) / w
        this.scrollingX = this.r * rangeX + cfg.start.x
        this.y = this.r * rangeY + cfg.start.y
        this.accept(this.scrollingX)
      }
      const mouseUp = (ee: MouseEvent) => {
        trackMouse(ee)
        Doc.unbind(document, 'mousemove', trackMouse)
        Doc.unbind(document, 'mouseup', mouseUp)
        this.changed()
      }
      Doc.bind(document, 'mousemove', trackMouse)
      Doc.bind(document, 'mouseup', mouseUp)
    })

    this.accept(this.scrollingX, true)
  }

  accept (x: number, skipUpdate?: boolean): void {
    const tmpl = this.tmpl
    if (this.roundY) this.y = Math.round(this.y)
    tmpl.x.textContent = threeSigFigs.format(x)
    tmpl.y.textContent = threeSigFigs.format(this.y)
    if (this.roundY) tmpl.y.textContent = `${this.y}`
    tmpl.handle.style.left = `calc(${this.r * 100}% - ${this.r * 14}px)`
    this.x = x
    this.scrollingX = x
    if (!skipUpdate) this.updated(x, this.y)
  }

  setValue (x: number) {
    const cfg = this.cfg
    this.r = (x - cfg.start.x) / (cfg.end.x - cfg.start.x)
    this.y = cfg.start.y + this.r * (cfg.end.y - cfg.start.y)
    this.accept(x, true)
  }
}

const clamp = (v: number, min: number, max: number): number => v < min ? min : v > max ? max : v
