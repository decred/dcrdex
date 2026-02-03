import { useEffect, useState } from 'react'
import t from '../js/intl'
import app from '../js/application'
import Doc from '../js/doc'
import { TotalUSDBalance } from '../js/registry'


export default function Header () {
  const [usdBalance, setUSDBalance] = useState<TotalUSDBalance>(app.totalBalanceUSD())

  useEffect(() => {
    const balK = app.registerBalanceUpdater(() => setUSDBalance(app.totalBalanceUSD()))
    const noteK = app.registerFiatRateUpdater(() => setUSDBalance(app.totalBalanceUSD()))
    return () => {
      app.unregisterBalanceUpdater(balK)
      app.unregisterFiatRateUpdater(noteK)
    }
  }, [])

  const usdBal = usdBalance

  return (
    <div className="d-flex align-items-stretch">
      <div className="flex-center">
        <img className="logo-full medium me-2" />
      </div>
      <div className="flex-grow-1 d-flex align-items-stretch justify-content-end fs14 demi py-1">
        {
          usdBal.ok ? (
            <button className="flex-center flex-column demi me-2 subtle"
              onClick={() => app.loadPage('portfolio')}>
              <span>{Doc.formatFourSigFigs(usdBal.total, 2)} USD</span>
              <span>{usdBal.numContribs} {t('Assets')}</span>
            </button>
          ) : null
        }

        <button className="flex-center flex-column demi me-2 subtle"
          onClick={() => app.setForm({ form: 'receive', data: {} })}>
          <span className="ico-qrcode fs22" />
          <span>{t('Receive')}</span>
        </button>

        <button className="flex-center flex-column demi me-2 subtle"
          onClick={() => app.setForm({ form: 'send', data: {} })}>
          <span className="ico-send fs22" />
          <span>{t('Send')}</span>
        </button>

        <button className="flex-center flex-column demi me-2 subtle"
          onClick={() => app.setForm({ form: 'swap', data: {} })}>
          <span className="ico-swap fs22" />
          <span>{t('Swap')}</span>
        </button>

        <button className="flex-center flex-column demi me-2 noborder">
          <span className="ico-bell fs28" />
        </button>

        <button className="flex-center flex-column demi me-2 noborder"
          onClick={() => app.loadPage('settings')}>
          <span className="ico-gear fs28" />
        </button>
      </div>
    </div>
  )
}
