import { Controller } from '@hotwired/stimulus'

export default class TailController extends Controller {
  static targets = ['scroll', 'watch']
  declare scrollTarget: Element
  declare watchTargets: Element[]

  static values = {
    disabled: Boolean,
  }
  declare disabledValue: boolean

  private observers = new Map<Element, MutationObserver>()

  watchTargetConnected(el: (typeof this.watchTargets)[0]): void {
    const o = new MutationObserver(() => this.scrollToBottom())
    o.observe(el, {
      subtree: true,
      childList: true,
    })
  }

  watchTargetDisconnected(el: (typeof this.watchTargets)[0]): void {
    const o = this.observers.get(el)
    if (o) {
      o.disconnect()
      this.observers.delete(el)
    }
  }

  scrollToBottom(): void {
    if (!this.disabledValue) {
      this.scrollTarget.scroll({ top: this.scrollTarget.scrollHeight })
    }
  }

  enable(event?: Event): void {
    let wantDisabled = false
    if (event?.target instanceof HTMLInputElement) {
      wantDisabled = !event.target.checked
    }
    this.disabledValue = wantDisabled
  }

  disable(event?: Event): void {
    let wantDisabled = true
    if (event?.target instanceof HTMLInputElement) {
      wantDisabled = event.target.checked
    }
    this.disabledValue = true
  }
}
