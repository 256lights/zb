import { Controller } from '@hotwired/stimulus'

export default class LogStreamController extends Controller {
  static targets = ['content']
  declare readonly contentTarget: Element

  static values = {
    href: String,
    nextByte: Number,
    chunkSize: { type: Number, default: 256 * 1024 },
    backoff: { type: Number, default: 250 },
    jitter: { type: Number, default: 500 },
  }
  declare hrefValue: string
  declare nextByteValue: number
  declare chunkSizeValue: number
  declare backoffValue: number
  declare jitterValue: number

  private abortController?: AbortController
  private decoder?: TextDecoder

  connect(): void {
    if (this.abortController) {
      this.abortController.abort()
      this.abortController = undefined
    }
    this.resume()
  }

  disconnect(): void {
    this.pause()
    this.decoder = undefined
  }

  resume(): void {
    if (!this.abortController) {
      this.abortController = new AbortController()
      this.stream(this.abortController.signal)
    }
  }

  pause(): void {
    if (this.abortController) {
      this.abortController.abort()
      this.abortController = undefined
    }
  }

  private async stream(abortSignal: AbortSignal): Promise<void> {
    let hasMore = true
    let size: number | null = null
    while (hasMore) {
      ;[size, hasMore] = await this.streamNext(abortSignal, size)
    }
  }

  private async streamNext(
    signal: AbortSignal,
    resourceSize: number | null,
  ): Promise<[number | null, boolean]> {
    if (this.nextByteValue < 0) {
      return [null, false]
    }

    const maxRangeEnd = this.nextByteValue + this.chunkSizeValue - 1
    const rangeEnd =
      resourceSize !== null && resourceSize - 1 < maxRangeEnd
        ? resourceSize - 1
        : maxRangeEnd
    let response: Response
    try {
      response = await window.fetch(this.hrefValue, {
        headers: {
          Accept: 'text/plain',
          Range: `bytes=${this.nextByteValue}-${rangeEnd}`,
        },
        signal,
      })
    } catch (err) {
      console.warn('Log stream error:', err)
      return [resourceSize, await this.backoff(signal)]
    }

    switch (response.status) {
      case 200: {
        let text: string
        try {
          text = await response.text()
        } catch (err) {
          console.warn('Log stream error:', err)
          return [resourceSize, await this.backoff(signal)]
        }
        this.contentTarget.replaceChildren(text)
        this.nextByteValue = -1
        return [null, false]
      }
      case 206: {
        let bytes: ArrayBuffer
        try {
          bytes = await response.arrayBuffer()
        } catch (err) {
          console.warn('Log stream error:', err)
          return [resourceSize, await this.backoff(signal)]
        }
        this.nextByteValue += bytes.byteLength
        const newSize = getSize(response) ?? resourceSize
        let hasMore = newSize === null || this.nextByteValue < newSize

        if (!this.decoder) {
          this.decoder = new TextDecoder('utf-8')
        }
        const newTextNode = this.decoder.decode(bytes, { stream: hasMore })
        this.contentTarget.append(newTextNode)
        if (!hasMore) {
          this.nextByteValue = -1
        }

        if (hasMore && bytes.byteLength === 0) {
          // No progress: back off for a little bit.
          hasMore = await this.backoff(signal)
        }
        return [newSize, hasMore]
      }
      default:
        // TODO(someday): Handle 416 to compute new end.
        throw new Error(`stream log status: ${response.statusText}`)
    }
  }

  private backoff(signal: AbortSignal): Promise<boolean> {
    return sleep(
      this.backoffValue + this.jitterValue * Math.random(),
      signal,
    ).then(
      () => true,
      () => false,
    )
  }
}

function sleep(millis: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const abortListener = () => {
      window.clearTimeout(id)
      reject(new Error('sleep aborted'))
    }
    const id = window.setTimeout(() => {
      signal.removeEventListener('abort', abortListener)
      resolve()
    }, millis)
    signal.addEventListener('abort', abortListener)
  })
}

function getSize(response: Pick<Response, 'headers'>): number | undefined {
  const contentRangeHeader = response.headers.get('Content-Range')
  if (!contentRangeHeader) {
    return undefined
  }
  const matches = contentRangeHeader.match(/bytes .*\/([0-9]+)/)
  const end = matches?.[1]
  return end ? Number.parseInt(end, 10) : undefined
}
