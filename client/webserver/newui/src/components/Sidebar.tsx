import t from '../js/intl'
import app from '../js/application'

function sidebarButtonClass (page: string, button: string) {
  return `p-2 pe-3 d-flex align-items-center subtle mt-2 text-left fs18 ${page === button ? 'selected' : ''}`
}

export default function Sidebar () {
  const page = app.pageData.pageRoot
  return (
    <div className="flex-stretch-column p-3">
      <span className="fs12 grey">{t('VIEWS')}</span>
      <button className={sidebarButtonClass(page, 'portfolio')}
        onClick={() => app.loadPage('portfolio')}>
        <span className="ico-portfolio me-3" />
        <span>{t('Portfolio')}</span>
      </button>
      <button className={sidebarButtonClass(page, 'trade')}
        onClick={() => app.loadPage('trade')}>
        <span className="ico-barchart me-3" />
        <span>{t('Trade')}</span>
      </button>
      <span className="mt-4 fs12 grey">{t('FAVORITES')}</span>
      <button className={sidebarButtonClass(page, '')}>
        <span className="ico-add me-3" />
        <span>{t('New')}</span>
      </button>
    </div>
  )
}
