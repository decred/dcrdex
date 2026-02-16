import BasePage from './basepage'
import Doc from './doc'
import {
  app,
  PageElement
} from './registry'

const animationLength = 300

export default class ProposalsPage extends BasePage {
  body: HTMLElement
  forms: PageElement[]
  currentForm: PageElement
  page: Record<string, PageElement>

  constructor (body: HTMLElement) {
    super()
    this.body = body
    const page = this.page = Doc.idDescendants(body)
    this.forms = Doc.applySelector(page.forms, ':scope > form')
    Doc.applySelector(page.forms, '.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { this.closePopups() })
    })
    Doc.bind(page.filterIcon, 'click', () => {
      const activeStatus = page.filterForm.dataset.activestatus || 'all'
      Doc.applySelector(page.filterForm, '.voteStatus').forEach(el => {
        el.classList.remove('active-opt')
        if (el.dataset.status === activeStatus) {
          el.classList.add('active-opt')
        }
      })
      this.showForm(page.filterForm)
    })
    Doc.bind(page.searchProposals, 'click', () => {
      const query = page.proposalSearchInput.value || ''
      if (!query) return
      const loaded = app().loading(this.page.proposals)
      app().loadPage('proposals', { query })
      loaded()
    })
    Doc.bind(page.cancelSearch, 'click', () => {
      if (!page.proposalSearchInput.value) return
      page.proposalSearchInput.value = ''
      app().loadPage('proposals')
    })
    Doc.applySelector(page.filterForm, '.voteStatus').forEach(el => {
      Doc.bind(el, 'click', () => {
        Doc.applySelector(page.filterForm, '.voteStatus').forEach(el => { el.classList.remove('active-opt') })
        el.classList.add('active-opt')
        this.refreshWithFilter()
      })
    })
    Doc.applySelector(page.proposals, '.proposal').forEach(el => {
      Doc.bind(el, 'click', async () => await this.loadProposal(el.dataset.token || '', this.page.proposals))
    })
    Doc.applySelector(page.proposals, '.vote-bar').forEach(bar => {
      bar.style.setProperty('--yes', bar.dataset.yes + '%')
      bar.style.setProperty('--no', bar.dataset.no + '%')
      bar.style.setProperty('--approval-threshold', bar.dataset.threshold || '60')
    })
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
  }

  refreshWithFilter () {
    const status = Doc.safeSelector(this.page.filterForm, '.voteStatus.active-opt').dataset.status || ''
    const data: Record<string, string> = {}
    if (status && status !== 'all') {
      data.status = status
    }
    const query = this.page.proposalSearchInput.value || ''
    if (query) {
      data.query = query
    }
    data.page = '1'
    app().loadPage('proposals', data)
  }

  async loadProposal (token: string, displayedEl : PageElement) {
    const assetID = 42 // dcr asset ID
    const loaded = app().loading(displayedEl)
    const data: Record<string, string> = {
      assetID: assetID.toString()
    }
    await app().loadPage(`proposal/${token}`, data)
    loaded()
  }
}
