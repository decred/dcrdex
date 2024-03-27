import Application from './js/app'
import { registerApplication } from './js/registry'
import './css/application.scss'

const app = new Application()
registerApplication(app)
app.start()

if (module.hot) {
  module.hot.accept('./css/application.scss')
}
