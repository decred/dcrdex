import Application from './js/app'
import { registerApplication } from './js/registry'
import './css/bootstrap.scss'
import './css/application.scss'

const app = new Application()
registerApplication(app)
app.start()
