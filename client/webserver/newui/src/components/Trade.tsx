import { useState, useEffect } from 'react'
import t from '../js/intl'
import app from '../js/application'
import State from '../js/state'
import { Market, PageData } from '../js/registry'
import { postJSON } from '../js/http'
import Loading from './Loading'
import Doc from '../js/doc'

export default function Trade () {
  const [market, setMarket] = useState<[string, string] | undefined>(extractMarket(app.pageData))

  useEffect(() => {
    if (!market && marketsAvailable()) {
      loadBestMarket(setMarket)
    }
  }, [])

  if (!market) {
    if (marketsAvailable()) {
      return <Loading />
    } else {
      return <NoMarkets setMarket={setMarket} />
    }
  }

  const [host, mktName] = market

  return (
    <div className="fill-abs flex-center">Trade {mktName} @ {host}</div>
  )
}

interface NoMarketsParams {
  setMarket: (market: [string, string]) => void
}

function NoMarkets ({ setMarket }: NoMarketsParams) {
  const [err, setErr] = useState<string | null>(null)

  return (
    <div className="fill-abs flex-center">
      <div className="flex-stretch-column">
        <button className="feature" onClick={() => registerWithDecredDEX(setMarket, setErr)}>{t('Register With Decred DEX')}</button>
        <button className="mt-2">{t('Add a Custom Exchange')}</button>
        {err && <div className="errcolor mt-2">{err}</div>}
      </div>
    </div>
  )
}

// hostMarkets generates a mapping of host to markets using the shortened
// Doc.addrHost host name as the keys.
function hostMarkets (): Record<string, Record<string, Market>> {
  const d: Record<string, Record<string, Market>> = {}
  for (const xc of Object.values(app.user.exchanges)) {
    if (Object.keys(xc.markets).length === 0) continue
    d[Doc.addrHost(xc.host)] = xc.markets
  }
  return d
}

function marketsAvailable () {
  return Object.values(app.user.exchanges).some(xc => Object.values(xc.markets).length > 0)
}

function extractMarket (pd: PageData): ([string, string] | undefined) {
  if (pd.pageParts.length !== 3) return
  const mkts = hostMarkets()
  const [_, host, mktName] = pd.pageParts
  if (mkts[host]?.[mktName]) return [host, mktName]
}

function loadBestMarket (setMarket: (market: [string, string]) => void) {
  const mkts = hostMarkets()
  const lastMkt = State.getLastMarket()
  if (lastMkt) {
    const [host, mktName] = lastMkt
    if (mkts[host]?.[mktName]) {
      setMarket([host, mktName])
      app.loadPage(`trade/${host}/${mktName}`)
      return
    }
  }
  const [host, mktName] = bestMarketDCRFirst()
  setMarket([host, mktName])
  app.loadPage(`trade/${host}/${mktName}`)
}

function bestMarketDCRFirst (): [string, string] {
  let best: [string, string, number] | null = null
  // Find best DCR market by volume
  for (const xc of Object.values(app.user.exchanges)) {
    for (const mkt of Object.values(xc.markets)) {
      if (mkt.baseid === 42 /* DCR */) {
        if (!best) {
          best = [Doc.addrHost(xc.host), mkt.name, mkt.spot.vol24]
        } else if (mkt.spot.vol24 > best[2]) {
          best = [Doc.addrHost(xc.host), mkt.name, mkt.spot.vol24]
        }
      }
    }
  }
  if (best) return [best[0], best[1]]
  // Fallback to first market
  const first = Object.values(app.user.exchanges)[0]
  return [Doc.addrHost(first.host), Object.values(first.markets)[0].name]
}

function defaultDEXHost () {
  switch (app.user.net) {
    case 0: return 'dex.decred.org:7232'
    case 1: return 'bison.exchange:17232'
    default: return '127.0.0.1:17273'
  }
}

async function registerWithDecredDEX (setMarket: (market: [string, string]) => void, setErr: (err: string | null) => void) {
  const r = await postJSON('/api/adddex', { addr: defaultDEXHost() })
  if (r.ok) setMarket([Doc.addrHost(defaultDEXHost()), 'dcr_btc'])
  else setErr(r.errMsg)
}