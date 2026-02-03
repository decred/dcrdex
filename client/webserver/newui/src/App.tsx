import { useEffect, useState } from 'react'
import app from './js/application'
import UnlockForm from './components/UnlockForm'
import MainFrame from './components/MainFrame'
import InitWizard from './components/InitWizard'
import Loading from './components/Loading'
import {
  AppState,
  PageData
} from './js/registry'

// Disable form submissions (we handle them ourselves) at a global level.
document.addEventListener('submit', e => e.preventDefault())

export const App = () => {
  const [appState, setAppState] = useState<AppState | null>(null)
  const [pageData, setPageData] = useState<PageData | null>(null)

  const resetAppState = async () => {
    setAppState(await app.fetchAppState())
  }

  const start = async () => {
    await app.start(setPageData)
    await resetAppState()
    if (!app.user && !pageData) app.loadPage('') // reset URL
  }

  useEffect(() => { start() }, [])

  if (appState === null) return <Loading />


  if (appState.user) {
    return <MainFrame pd={pageData} />
  } else if (appState.inited) {
    return <UnlockForm setUnlocked={() => { resetAppState(); app.loadPage('portfolio') }} />
  } else return <InitWizard setInited={() => { resetAppState(); app.loadPage('portfolio') }} />
}