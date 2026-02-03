import { MouseEvent } from 'react'
import Receive from './Receive'
import Send from './Send'
import Swap from './Swap'
import { FormData } from '../js/registry'
import app from '../js/application'

interface ModalsParams {
  fd: FormData
}

const onBackgroundClick = (e: MouseEvent) => {
  if (e.target === e.currentTarget) {
    app.setForm({ form: '', data: {} })
  }
}

export default function Modals ({ fd }: ModalsParams) {

  const renderContent = () => {
    switch (fd.form) {
      case 'receive': return <Receive />
      case 'send': return <Send />
      case 'swap': return <Swap />
      default: return (<div className="flex-center">Unknown form: {fd.form}</div>)
    }
  }

  return (
    <div className={`fill-abs flex-center modals ${fd.form ? '' : 'd-none'}`}
      onClick={onBackgroundClick}>
      {fd.form && renderContent()}
    </div>
  )
}