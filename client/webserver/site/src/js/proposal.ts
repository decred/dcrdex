import BasePage from './basepage'
import Doc, { Animation } from './doc'
import * as intl from './locales'
import * as forms from './forms'
import { postJSON } from './http'
import {
  app,
  PageElement
} from './registry'

const animationLength = 300

export default class ProposalPage extends BasePage {
  body: HTMLElement
  forms: PageElement[]
  currentForm: PageElement
  animation: Animation
  page: Record<string, PageElement>

  constructor (body: HTMLElement) {
    super()
    this.body = body
    const page = this.page = Doc.idDescendants(body)
    this.forms = Doc.applySelector(page.forms, ':scope > form')
    Doc.applySelector(page.forms, '.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { this.closePopups() })
    })
    Doc.bind(page.goBackToProposals, 'click', (e: Event) => {
      e.preventDefault()
      const loaded = app().loading(body)
      app().loadPage('proposals')
      loaded()
    })
    Doc.applySelector(page.proposalInfo, '.vote-bar').forEach(bar => {
      bar.style.setProperty('--yes', bar.dataset.yes + '%')
      bar.style.setProperty('--no', bar.dataset.no + '%')
      bar.style.setProperty('--approval-threshold', bar.dataset.threshold || '60')
    })
    Doc.applySelector(page.voteForm, '.vote-btn').forEach(el => {
      Doc.bind(el, 'click', (e: Event) => {
        e.preventDefault()
        Doc.applySelector(page.voteForm, '.vote-btn').forEach(el => { el.classList.remove('active') })
        el.classList.add('active')
      })
    })
    // Handle external links in proposal content.
    Doc.safeSelector(page.proposalInfo, '.proposal-content').querySelectorAll('a').forEach(a => {
      a.target = '_blank'
      a.rel = 'noopener noreferrer'
    })
    Doc.bind(page.viewVoteFormBtn, 'click', (e: Event) => {
      e.preventDefault()
      page.voteFormError.textContent = ''
      Doc.hide(this.page.voteFormError)
      this.showForm(page.voteForm)
    })
    Doc.bind(page.voteSubmit, 'click', async () => this.vote())
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form: HTMLElement) {
    const page = this.page
    this.currentForm = form
    this.forms.forEach(form => Doc.hide(form))
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0'
  }

  closePopups () {
    Doc.hide(this.page.forms)
    if (this.animation) this.animation.stop()
  }

  async showSuccess (msg: string) {
    this.forms.forEach(form => Doc.hide(form))
    this.currentForm = this.page.checkmarkForm
    this.animation = forms.showSuccess(this.page, msg)
    await this.animation.wait()
    this.animation = new Animation(1500, () => { /* pass */ }, '', () => {
      if (this.currentForm === this.page.checkmarkForm) this.closePopups()
    })
  }

  async vote () {
    const page = this.page
    const votingPower = Number(page.proposalInfo.dataset.votingpower) || 0
    if (votingPower < 1) return
    const bit = Doc.safeSelector(page.voteForm, '.vote-btn.active').dataset.value
    if (!bit) return

    const req = {
      token: page.proposalInfo.dataset.token,
      assetID: Number(page.proposalInfo.dataset.assetid),
      bit: bit
    }

    const loaded = app().loading(page.voteForm)
    const res = await postJSON('/api/castvote', req)
    loaded()
    if (res.msg) {
      page.voteFormError.textContent = res.msg
      Doc.show(page.voteFormError)
      return
    }
    this.closePopups()
    this.showSuccess(intl.prep(intl.ID_VOTE_CAST_MESSAGE))
  }

  async loadProposal (token: string) {
    const assetID = 42 // dcr asset ID
    const loaded = app().loading(this.page.tabContent)
    const data: Record<string, string> = {
      assetID: assetID.toString()
    }
    await app().loadPage(`proposal/${token}`, data)
    loaded()
  }
}
