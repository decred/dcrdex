import { useState, useEffect } from 'react'
import app from '../js/application'
import Header from './Header'
import Sidebar from './Sidebar'
import Portfolio from './Portfolio'
import Trade from './Trade'
import Settings from './Settings'
import Modals from './Modals'
import { PageData, FormData } from '../js/registry'

interface MainFrameParams {
  pd: PageData
}

export default function MainFrame ({ pd }: MainFrameParams) {
  const [formData, setFormData] = useState<FormData>({ form: '', data: {} })

  useEffect(() => {
    app.registerFormSetter(setFormData)
    return () => app.unregisterFormSetter()
  }, [])

  const renderContent = () => {
    switch (pd.pageRoot) {
      case 'portfolio': return <Portfolio />
      case 'trade': return <Trade />
      case 'settings': return <Settings />
      default: return (<div className="flex-center">Unknown page: {pd.pageRoot}</div>)
    }
  }
  return (
    <div className="d-flex flex-column h-100">
      <Header />
      <div className="d-flex flex-grow-1 align-items-stretch">
        <Sidebar />
        <div className="flex-grow-1 border-left border-top position-relative">{renderContent()}</div>
      </div>
      <Modals fd={formData} />
    </div>
  )
}