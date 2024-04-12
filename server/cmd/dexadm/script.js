async function requestJSON (method, addr, opts) {
  opts = opts || {}
  const req = {
    method: method,
    headers: {  'Content-Type': opts.contentType ?? 'application/json' },
    body: opts.reqBody
  }
  const resp = await window.fetch(addr, req)
  if (resp.status !== 200) { throw new Error(`${resp.status}: ${resp.statusText}`) }
  const body = await resp.text()
  if (body.length === 0) return "OK"
  if (opts.raw) return body
  return JSON.parse(body)
}

(async () => {
  const page = {}
  for (const el of document.querySelectorAll('[id]')) page[el.id] = el

  page.responseTmpl.remove()
  page.responseTmpl.removeAttribute('id')

  const doRequest = async (method, path, opts) => {
    const div = page.responseTmpl.cloneNode(true)
    const tmpl = Array.from(div.querySelectorAll('[data-tmpl]')).reduce((d, el) => {
      d[el.dataset.tmpl] = el
      return d
    }, {})
    page.responses.prepend(div)
    tmpl.path.textContent = path
    tmpl.close.addEventListener('click', () => div.remove())
    try {
      const resp = await requestJSON(method, path, opts)
      if (opts?.raw) tmpl.response.textContent = resp
      else tmpl.response.textContent = JSON.stringify(resp, null, 4)
    } catch (e) {
      tmpl.response.textContent = e.toString()
    } finally {
      while (page.responses.children.length > 20) page.respones.removeChild(page.respones.lastChild)
      page.responses.scrollTo(0, 0)
    }
  }

  const get = (path, opts) => doRequest('GET', path, opts)
  const post = (path, reqBody, contentType) => doRequest('POST', path, { reqBody, contentType })

  page.assetBttn.addEventListener('click', () => get(`/asset/${page.assetInput.value}`))
  page.feeScaleBttn.addEventListener('click', () => get(`/asset/${page.assetInput.value}/setfeescale/${page.feeScaleInput.value}`))
  page.configBttn.addEventListener('click', () => get('/config'))
  page.listAccountsBttn.addEventListener('click', () => get('/accounts'))
  page.accountInfoBttn.addEventListener('click', () => get(`/account/${page.accountIDInput.value}`))
  page.accountOutcomesBttn.addEventListener('click', () => get(`/account/${page.accountIDInput.value}/outcomes?n=100`))
  page.forgiveMatchBttn.addEventListener('click', () => get(`/account/${page.accountIDInput.value}/forgive_match/${page.forgiveMatchIDInput.value}`))
  page.notifyAccountBttn.addEventListener('click', () => post(`/account/${page.accountIDInput.value}/notify`, page.notifyAccountInput.value, 'text/plain'))
  page.broadcastBttn.addEventListener('click', () => post(`/notifyall`, page.broadcastInput.value, 'text/plain'))
  page.viewMarketsBttn.addEventListener('click', () => get('/markets'))
  page.marketInfoBttn.addEventListener('click', () => get(`/market/${page.marketIDInput.value}`))
  page.marketBookBttn.addEventListener('click', () => get(`/market/${page.marketIDInput.value}/orderbook`))
  page.marketEpochBttn.addEventListener('click', () => get(`/market/${page.marketIDInput.value}/epochorders`))
  page.marketMatchesBttn.addEventListener('click', () => {
    const uri = `/market/${page.marketIDInput.value}/matches?n=100&includeinactive=${page.includeInactiveMatches.checked ? 'true' : 'false'}`
    get(uri, { raw: true })
  })
  page.suspendBttn.addEventListener('click', () => get(`/market/${page.marketIDInput.value}/suspend`))
  page.resumeBttn.addEventListener('click', () => get(`/market/${page.marketIDInput.value}/resume`))
  page.generatePrepaidBondsBttn.addEventListener('click', () => {
    const [n, days, strength] = [page.prepaidBondCountInput.value, page.prepaidBondDaysInput.value, page.prepaidBondStrengthInput.value]
    get(`/prepaybonds?n=${n}&days=${days}&strength=${strength}`)
  })
})()
