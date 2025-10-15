import Doc from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import * as intl from './locales'
import {
  bind as bindForm,
  slideSwap
} from './forms'
import { Wave } from './charts'
import {
  app,
  PageElement,
  SupportedAsset,
  User,
  WalletInfo,
  WalletDefinition,
  ConfigOption,
  APIResponse
} from './registry'

interface InitResponse extends APIResponse {
  hosts: string[]
  mnemonic?: string
}

/*
 * InitPage is the page handler for the /init view. InitPage is essentially a
 * form handler. There are no non-form elements on /init. InitPage additionally
 * has a role caching the initialization password. A couple of notes about
 * InitPage.
 *   1) There is no going backwards. Once you set a password, you can't go back
 *      to the password form. If you refresh, you won't end up on /init, so
 *      won't have access to the QuickConfigForm or SeedBackupForm . Once you
 *      submit your auto-config choices, you can't change them. This has
 *      implications for coding and UI. There are no "go back" or "close form"
 *      elements.
 *   2) The user can preclude auto-config and seed backup by clicking an
 *      available header link after password init, e.g. Wallets, in the page
 *      header. NOTE: Regardless of what the user does after setting the app
 *      pass, they will receive a notification reminding them to back up their
 *      seed. Perhaps it would be better to somehow delay that message until
 *      they choose to ignore the seed backup dialog, but having more reminders
 *      is also okay.
 */
export default class InitPage extends BasePage {
  body: HTMLElement
  page: Record<string, PageElement>
  initForm: AppInitForm
  quickConfigForm: QuickConfigForm
  seedBackupForm: SeedBackupForm
  mnemonic?: string

  constructor (body: HTMLElement) {
    super()
    this.body = body
    const page = this.page = Doc.idDescendants(body)
    this.initForm = new AppInitForm(page.appPWForm, (pw: string, hosts: string[], mnemonic?: string) => { this.appInited(pw, hosts, mnemonic) })
    this.quickConfigForm = new QuickConfigForm(page.quickConfigForm, () => this.quickConfigDone())
    this.seedBackupForm = new SeedBackupForm(page.seedBackupForm, () => this.seedBackedUp())
  }

  async appInited (pw: string, hosts: string[], mnemonic?: string) {
    this.mnemonic = mnemonic
    const page = this.page
    await this.quickConfigForm.update(pw, hosts)
    if (mnemonic) this.seedBackupForm.update(mnemonic)
    slideSwap(page.appPWForm, page.quickConfigForm)
  }

  quickConfigDone () {
    if (!this.mnemonic) app().loadPage('wallets')
    else slideSwap(this.page.quickConfigForm, this.page.seedBackupForm)
  }

  seedBackedUp () {
    app().loadPage('wallets')
  }
}

/*
 * The AppInitForm handles the form that sets the app password, accepts an
 * optional seed, and initializes the app.
 */
class AppInitForm {
  form: PageElement
  page: Record<string, PageElement>
  success: (pw: string, hosts: string[], mnemonic?: string) => void

  constructor (form: PageElement, success: (pw: string, hosts: string[], mnemonic?: string) => void) {
    this.form = form
    this.success = success
    const page = this.page = Doc.idDescendants(form)
    bindForm(form, page.appPWSubmit, () => this.setAppPass())
    bindForm(form, page.toggleSeedInput, () => {
      if (Doc.isHidden(page.seedInputBox)) {
        page.toggleSeedInputIcon.classList.remove('ico-plus')
        page.toggleSeedInputIcon.classList.add('ico-minus')
        Doc.show(page.seedInputBox)
      } else {
        page.toggleSeedInputIcon.classList.remove('ico-minus')
        page.toggleSeedInputIcon.classList.add('ico-plus')
        Doc.hide(page.seedInputBox)
      }
    })
  }

  /* Set the application password. Attached to form submission. */
  async setAppPass () {
    const page = this.page
    Doc.hide(page.appPWErrMsg)
    const pw = page.appPW.value || ''
    const pwAgain = page.appPWAgain.value
    if (pw === '') {
      page.appPWErrMsg.textContent = intl.prep(intl.ID_NO_PASS_ERROR_MSG)
      Doc.show(page.appPWErrMsg)
      return
    }
    if (pw !== pwAgain) {
      page.appPWErrMsg.textContent = intl.prep(intl.ID_PASSWORD_NOT_MATCH)
      Doc.show(page.appPWErrMsg)
      return
    }

    page.appPW.value = ''
    page.appPWAgain.value = ''
    const loaded = app().loading(this.form)
    // const seed = page.seedInput.value?.replace(/\s+/g, '') // strip whitespace
    const seed = page.seedInput.value ?? ''
    const res: InitResponse = await postJSON('/api/init', {
      pass: pw,
      seed: seed
    })
    loaded()
    if (!app().checkResponse(res)) {
      page.appPWErrMsg.textContent = res.msg
      Doc.show(page.appPWErrMsg)
      return
    }
    this.success(pw, res.hosts, res.mnemonic)
  }
}

// HostConfigRow is used by the QuickConfigForm to track the user's choices.
interface HostConfigRow {
  host: string
  checkbox: HTMLInputElement
}

// WalletConfigRow is used by the QuickConfigForm to track the user's choices.
interface WalletConfigRow {
  asset: SupportedAsset
  type: string
  checkbox: HTMLInputElement
}

let rowIDCounter = 0

/*
 * QuickConfigForm handles the form that allows users to quickly configure
 * view-only servers and native wallets (that don't require any configuration).
 */
class QuickConfigForm {
  page: Record<string, PageElement>
  form: PageElement
  servers: HostConfigRow[]
  wallets: WalletConfigRow[]
  pw: string
  success: () => void

  constructor (form: PageElement, success: () => void) {
    this.form = form
    this.success = success
    const page = this.page = Doc.idDescendants(form)
    Doc.cleanTemplates(page.qcServerTmpl, page.qcWalletTmpl)
    bindForm(form, page.quickConfigSubmit, () => { this.submit() })
    bindForm(form, page.qcErrAck, () => { this.success() })
  }

  async update (pw: string, hosts: string[]) {
    this.pw = pw
    const page = this.page

    this.servers = []
    for (const host of hosts) {
      const row = page.qcServerTmpl.cloneNode(true) as PageElement
      page.qcServersBox.appendChild(row)
      const tmpl = Doc.parseTemplate(row)
      rowIDCounter++
      const rowID = `qcsrow${rowIDCounter}`
      row.htmlFor = rowID
      tmpl.checkbox.id = rowID
      tmpl.host.textContent = host
      this.servers.push({ host, checkbox: tmpl.checkbox as HTMLInputElement })
    }

    const u = await app().fetchUser() as User
    this.wallets = []
    for (const a of Object.values(u.assets)) {
      if (a.token) continue
      const winfo = a.info as WalletInfo
      let autoConfigurable: WalletDefinition | null = null
      for (const wDef of winfo.availablewallets) {
        if (!wDef.seeded) continue
        if (wDef.configopts && wDef.configopts.some((opt: ConfigOption) => opt.required)) continue
        autoConfigurable = wDef
        break
      }
      if (!autoConfigurable) continue
      const row = page.qcWalletTmpl.cloneNode(true) as PageElement
      page.qcWalletsBox.appendChild(row)
      const tmpl = Doc.parseTemplate(row)
      rowIDCounter++
      const rowID = `qcwrow${rowIDCounter}`
      row.htmlFor = rowID
      tmpl.checkbox.id = rowID
      tmpl.icon.src = Doc.logoPath(a.symbol)
      tmpl.name.textContent = a.name
      this.wallets.push({
        asset: a,
        type: autoConfigurable.type,
        checkbox: tmpl.checkbox as HTMLInputElement
      })
    }
  }

  async submit () {
    const [failedHosts, failedWallets]: [string[], string[]] = [[], []]
    const ani = new Wave(this.form, { backgroundColor: true, message: '...' })
    ani.opts.message = intl.prep(intl.ID_ADDING_SERVERS)
    const connectServer = async (srvRow: HostConfigRow) => {
      if (!srvRow.checkbox.checked) return
      const req = {
        addr: srvRow.host,
        appPW: this.pw
      }
      const res = await postJSON('/api/adddex', req) // DRAFT NOTE: ignore errors ok?
      if (!app().checkResponse(res)) failedHosts.push(srvRow.host)
    }
    await Promise.all(this.servers.map(connectServer))

    ani.opts.message = intl.prep(intl.ID_CREATING_WALLETS)
    const createWallet = async (walletRow: WalletConfigRow) => {
      const { asset: a, type, checkbox } = walletRow
      if (!checkbox.checked) return
      const config: Record<string, string> = {}
      const walletDef = app().walletDefinition(a.id, type)
      for (const opt of (walletDef.configopts ?? [])) {
        if (!opt.default) continue
        if (opt.isboolean) {
          config[opt.key] = opt.default ? '1' : '0'
          continue
        }
        if (opt.repeatable && config[opt.key]) config[opt.key] += opt.repeatable + opt.default
        else config[opt.key] = String(opt.default)
      }
      const createForm = {
        assetID: a.id,
        appPass: this.pw,
        config: config,
        walletType: type
      }
      const res = await postJSON('/api/newwallet', createForm)
      if (!app().checkResponse(res)) failedWallets.push(a.name)
    }
    await Promise.all(this.wallets.map(createWallet))

    ani.stop()
    await app().fetchUser() // Calls updateMenuItemsDisplay internally
    if (failedWallets.length + failedHosts.length === 0) return this.success()

    const page = this.page
    Doc.hide(page.qcChoices)
    Doc.show(page.qcErrors)

    if (failedHosts.length) {
      for (const host of failedHosts) {
        page.qcServerErrorList.appendChild(document.createTextNode(host))
        page.qcServerErrorList.appendChild(document.createElement('br'))
      }
    } else Doc.hide(page.qcServerErrors)

    if (failedWallets.length) {
      for (const name of failedWallets) {
        page.qcWalletErrorList.appendChild(document.createTextNode(name))
        page.qcWalletErrorList.appendChild(document.createElement('br'))
      }
    } else Doc.hide(page.qcWalletErrors)
  }
}

/*
 * SeedBackupForm handles the form that allows the user to back up their seed
 * during initialization.
 */
class SeedBackupForm {
  form: PageElement
  page: Record<string, PageElement>
  mnemonic: string

  constructor (form: PageElement, success: () => void) {
    this.form = form
    const page = this.page = Doc.idDescendants(form)
    bindForm(form, page.seedAck, () => success())
    bindForm(form, page.showSeed, () => this.showSeed())
  }

  update (mnemonic: string) {
    this.mnemonic = mnemonic
  }

  showSeed () {
    const page = this.page
    page.mnemonic.textContent = this.mnemonic // `${words.slice(0, 5).join(' ')}\n${words.slice(5, 10).join(' ')}\n${words.slice(10,15).join(' ')}`
    Doc.hide(page.sbWanna)
    Doc.show(page.sbSeed)
  }
}
