async function requestJSON (method, addr, opts) {
  opts = opts || {}
  const req = {
    method: method,
    headers: {  'Content-Type': opts.contentType ?? 'application/json' },
    body: opts.reqBody
  }
  const resp = await window.fetch(addr, req)
  const body = await resp.text()
  if (resp.status !== 200) { console.log(resp); throw new Error(`${resp.status}: ${resp.statusText} : ${body}`) }
  if (body.length === 0) return "OK"
  if (opts.raw) return body
  return JSON.parse(body)
}

(async () => {
  const page = {}
  for (const el of document.querySelectorAll('[id]')) page[el.id] = el

  page.responseTmpl.remove()
  page.responseTmpl.removeAttribute('id')

  const writeResult = (path, res, isError) => {
    const div = page.responseTmpl.cloneNode(true)
    const tmpl = Array.from(div.querySelectorAll('[data-tmpl]')).reduce((d, el) => {
      d[el.dataset.tmpl] = el
      return d
    }, {})
    page.responses.prepend(div)
    tmpl.path.textContent = path
    tmpl.close.addEventListener('click', () => div.remove())
    tmpl.response.textContent = res
    if (isError) tmpl.response.classList.add('errcolor')
    while (page.responses.children.length > 20) page.responses.removeChild(page.respones.lastChild)
    page.responses.scrollTo(0, 0)
  }

  const doRequest = async (method, path, opts) => {
    try {
      const resp = await requestJSON(method, path, opts)
      if (opts?.raw) tmpl.response.textContent = resp
      else writeResult(path, JSON.stringify(resp, null, 4))
    } catch (e) {
      writeResult(path, e.toString(), true)
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
  page.matchFailsBttn.addEventListener('click', () => get(`/account/${page.accountIDInput.value}/fails?n=100`))
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
  page.suspendTimeCheckbox.addEventListener('change', () => page.suspendTimeInput.classList.toggle('d-none', !page.suspendTimeCheckbox.checked))
  page.unsuspendTimeCheckbox.addEventListener('change', () => page.unsuspendTimeInput.classList.toggle('d-none', !page.unsuspendTimeCheckbox.checked))
  const susun = (tag, withTime, timeV) => {
    if (!page.marketIDInput.value) return writeResult('/market', "no market specified", true)
    if (withTime && timeV === '') return writeResult('/market', "datetime not set", true)
    const params = new URLSearchParams()
    if (tag === 'suspend') params.append('persist', `${page.persistBook.checked ? 'true' : 'false'}`)
    if (withTime && timeV) params.append('t', (new Date(timeV)).getTime())
    get(`/market/${page.marketIDInput.value}/${tag}?${params.toString()}`)
  }
  page.suspendBttn.addEventListener('click', () => susun('suspend', page.suspendTimeCheckbox.checked, page.suspendTimeInput.value))
  page.resumeBttn.addEventListener('click', () => susun('resume', page.unsuspendTimeCheckbox.checked, page.unsuspendTimeInput.value))
  page.generatePrepaidBondsBttn.addEventListener('click', () => {
    const [n, days, strength] = [page.prepaidBondCountInput.value, page.prepaidBondDaysInput.value, page.prepaidBondStrengthInput.value]
    get(`/prepaybonds?n=${n}&days=${days}&strength=${strength}`)
  })
})()
