import Doc from './doc'
import BasePage from './basepage'
import * as OrderUtil from './orderutil'
import { postJSON } from './http'
import {
  app,
  PageElement,
  OrderFilter,
  Order
} from './registry'

const orderBatchSize = 50

export default class OrdersPage extends BasePage {
  main: HTMLElement
  offset: string
  loading: boolean
  orderTmpl: PageElement
  filterState: OrderFilter
  page: Record<string, PageElement>

  constructor (main: HTMLElement) {
    super()
    this.main = main
    // if offset is '', there are no more orders available to auto-load for
    // never-ending scrolling.
    this.offset = ''
    this.loading = false
    const page = this.page = Doc.idDescendants(main)
    this.orderTmpl = page.rowTmpl
    this.orderTmpl.remove()

    // filterState will store arrays of strings. The assets and statuses
    // sub-filters will need to be converted to ints for JSON encoding.
    const filterState: OrderFilter = this.filterState = {
      hosts: [],
      assets: [],
      statuses: []
    }

    const search = new URLSearchParams(window.location.search)
    const readFilter = (form: HTMLElement, filterKey: string) => {
      const v = search.get(filterKey)
      if (!v || v.length === 0) return
      const subFilter = v.split(',')
      if (v) {
        (filterState as any)[filterKey] = subFilter // Kinda janky
      }
      form.querySelectorAll('input').forEach(bttn => {
        if (subFilter.indexOf(bttn.value) >= 0) bttn.checked = true
      })
    }
    readFilter(page.hostFilter, 'hosts')
    readFilter(page.assetFilter, 'assets')
    readFilter(page.statusFilter, 'statuses')

    const applyButtons: HTMLElement[] = []
    const monitorFilter = (form: HTMLElement, filterKey: string) => {
      const applyBttn = form.querySelector('.apply-bttn') as HTMLElement
      applyButtons.push(applyBttn)
      Doc.bind(applyBttn, 'click', () => {
        this.submitFilter()
        applyButtons.forEach(bttn => Doc.hide(bttn))
      })
      form.querySelectorAll('input').forEach(bttn => {
        Doc.bind(bttn, 'change', () => {
          const subFilter = parseSubFilter(form)
          if (compareSubFilter(subFilter, (filterState as any)[filterKey])) {
            // Same as currently loaded. Hide the apply button.
            Doc.hide(applyBttn)
          } else {
            Doc.show(applyBttn)
          }
        })
      })
    }

    monitorFilter(page.hostFilter, 'hosts')
    monitorFilter(page.assetFilter, 'assets')
    monitorFilter(page.statusFilter, 'statuses')

    Doc.bind(this.main, 'scroll', () => {
      if (this.loading) return
      const belowBottom = page.ordersTable.offsetHeight - this.main.offsetHeight - this.main.scrollTop
      if (belowBottom < 0) {
        this.nextPage()
      }
    })

    Doc.bind(page.exportOrders, 'click', () => {
      this.exportOrders()
    })

    this.submitFilter()
  }

  /* setOrders empties the order table and appends the specified orders. */
  setOrders (orders: Order[]) {
    Doc.empty(this.page.tableBody)
    this.appendOrders(orders)
  }

  /* appendOrders appends orders to the orders table. */
  appendOrders (orders: Order[]) {
    const tbody = this.page.tableBody
    for (const ord of orders) {
      const tr = this.orderTmpl.cloneNode(true) as HTMLElement
      const set = (tmplID: string, s: string) => { Doc.tmplElement(tr, tmplID).textContent = s }
      const mktID = `${ord.baseSymbol.toUpperCase()}-${ord.quoteSymbol.toUpperCase()}`
      set('host', `${mktID} @ ${ord.host}`)
      let from, to, fromQty
      let toQty = ''
      const [baseUnitInfo, quoteUnitInfo] = [app().unitInfo(ord.baseID), app().unitInfo(ord.quoteID)]
      if (ord.sell) {
        [from, to] = [ord.baseSymbol, ord.quoteSymbol]
        fromQty = Doc.formatCoinValue(ord.qty, baseUnitInfo)
        if (ord.type === OrderUtil.Limit) {
          toQty = Doc.formatCoinValue(ord.qty / OrderUtil.RateEncodingFactor * ord.rate, quoteUnitInfo)
        }
      } else {
        [from, to] = [ord.quoteSymbol, ord.baseSymbol]
        if (ord.type === OrderUtil.Market) {
          fromQty = Doc.formatCoinValue(ord.qty, baseUnitInfo)
        } else {
          fromQty = Doc.formatCoinValue(ord.qty / OrderUtil.RateEncodingFactor * ord.rate, quoteUnitInfo)
          toQty = Doc.formatCoinValue(ord.qty, baseUnitInfo)
        }
      }

      set('fromQty', fromQty)
      Doc.tmplElement(tr, 'fromLogo').src = Doc.logoPath(from)
      set('fromSymbol', from)
      set('toQty', toQty)
      Doc.tmplElement(tr, 'toLogo').src = Doc.logoPath(to)
      set('toSymbol', to)
      set('type', `${OrderUtil.typeString(ord)} ${OrderUtil.sellString(ord)}`)
      set('rate', Doc.formatCoinValue(app().conventionalRate(ord.baseID, ord.quoteID, ord.rate)))
      set('status', OrderUtil.statusString(ord))
      set('filled', `${(ord.filled / ord.qty * 100).toFixed(1)}%`)
      set('settled', `${(OrderUtil.settled(ord) / ord.qty * 100).toFixed(1)}%`)
      const dateTime = new Date(ord.submitTime).toLocaleString()
      set('time', `${Doc.timeSince(ord.submitTime)} ago, ${dateTime}`)
      const link = Doc.tmplElement(tr, 'link')
      link.href = `order/${ord.id}`
      app().bindInternalNavigation(tr)
      tbody.appendChild(tr)
    }
    if (orders.length === orderBatchSize) {
      this.offset = orders[orders.length - 1].id
    } else {
      this.offset = ''
    }
  }

  /* submitFilter submits the current filter and reloads the order table. */
  async submitFilter () {
    const page = this.page
    this.offset = ''
    const filterState = this.filterState
    filterState.hosts = parseSubFilter(page.hostFilter)
    filterState.assets = parseSubFilter(page.assetFilter).map((s: string) => parseInt(s))
    filterState.statuses = parseSubFilter(page.statusFilter).map((s: string) => parseInt(s))
    this.setOrders(await this.fetchOrders())
  }

  /* fetchOrders fetches orders using the current filter. */
  async fetchOrders () {
    const loaded = app().loading(this.main)
    const res = await postJSON('/api/orders', this.currentFilter())
    loaded()
    return res.orders
  }

  /* exportOrders downloads a csv of the user's orders based on the current filter. */
  exportOrders () {
    this.offset = ''
    const filterState = this.currentFilter()
    const url = new URL(window.location.href)
    const search = new URLSearchParams('')
    const setQuery = (k: string) => {
      const subFilter = (filterState as any)[k]
      subFilter.forEach((v: any) => {
        search.append(k, v)
      })
    }
    setQuery('hosts')
    setQuery('assets')
    setQuery('statuses')
    url.search = search.toString()
    url.pathname = '/orders/export'
    window.open(url.toString())
  }

  /*
   * currentFilter converts the local filter type (which is all strings) to the
   * server's filter type.
   */
  currentFilter (): OrderFilter {
    const filterState = this.filterState
    return {
      hosts: filterState.hosts,
      assets: filterState.assets.map((s: any) => parseInt(s)),
      statuses: filterState.statuses.map((s: any) => parseInt(s)),
      n: orderBatchSize,
      offset: this.offset
    }
  }

  /*
   * nextPage resubmits the filter with the offset set to the last loaded order.
   */
  async nextPage () {
    if (this.offset === '' || this.loading) return
    this.loading = true
    Doc.show(this.page.orderLoader)
    const orders = await this.fetchOrders()
    this.loading = false
    Doc.hide(this.page.orderLoader)
    this.appendOrders(orders)
  }
}

/*
 * parseSubFilter parses a bool-map from the checkbox inputs in the specified
 * ancestor element.
 */
function parseSubFilter (form: HTMLElement): string[] {
  const entries: string[] = []
  form.querySelectorAll('input').forEach(box => {
    if (box.checked) entries.push(box.value)
  })
  return entries
}

/* compareSubFilter compares the two filter arrays for unordered equivalence. */
function compareSubFilter (filter1: any[], filter2: any[]): boolean {
  if (filter1.length !== filter2.length) return false
  for (const entry of filter1) {
    if (filter2.indexOf(entry) === -1) return false
  }
  return true
}
