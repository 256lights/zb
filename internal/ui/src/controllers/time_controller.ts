import { Controller } from '@hotwired/stimulus'

/**
 * Formats a `<time>` element in the user's locale/timezone.
 */
export default class TimeController extends Controller<HTMLTimeElement> {
  static values = {
    dateStyle: { type: String, default: 'medium' },
    timeStyle: { type: String, default: 'medium' },
  }
  declare dateStyleValue: string
  declare timeStyleValue: string

  connect() {
    const dtString = this.element.getAttribute('datetime') ?? ''
    if (!dtString) {
      return
    }
    const format = new Intl.DateTimeFormat(undefined, {
      dateStyle: coerceStyle(this.dateStyleValue),
      timeStyle: coerceStyle(this.timeStyleValue),
    })
    const date = new Date(dtString)
    this.element.replaceChildren(format.format(date))
  }
}

function coerceStyle(
  s: string,
): 'full' | 'long' | 'medium' | 'short' | undefined {
  return s === 'full' || s === 'long' || s === 'medium' || s === 'short'
    ? s
    : undefined
}
