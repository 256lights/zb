import { Controller } from '@hotwired/stimulus'

export default class LogStreamController extends Controller {
  static targets = ['content']
  declare readonly contentTarget: Element

  static values = {
    href: String,
    nextByte: Number,
    backoff: { type: Number, default: 1000 },
    jitter: { type: Number, default: 500 },
  }
  declare hrefValue: string
  declare nextByteValue: number
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

  private async stream(signal: AbortSignal): Promise<void> {
    for (;;) {
      try {
        if (!(await this.readRemainingContent(signal))) {
          return
        }
      } catch (e) {
        console.warn('Log stream interrupted: %s', e)
      }
      await this.backoff(signal)
    }
  }

  /**
   * @return whether there is potentially more content
   */
  private async readRemainingContent(signal: AbortSignal): Promise<boolean> {
    const response = await window.fetch(this.hrefValue, {
      headers: {
        Accept: 'text/plain, text/*;q=0.9',
        Range: `bytes=${this.nextByteValue}-`,
      },
      signal,
    })

    switch (response.status) {
      case 200: {
        if (!response.body) {
          throw new Error('log stream response missing body')
        }
        await this.streamBytes(
          signal,
          response.body.pipeThrough(skipFirst(this.nextByteValue)),
        )
        return false
      }
      case 206: {
        if (!response.body) {
          throw new Error('log stream response missing body')
        }
        await this.streamBytes(signal, response.body)
        return getSize(response) === '*'
      }
      case 404:
        // Don't retry a 404.
        return false
      case 416:
        // If we got Range Not Satisfiable, it's because our starting byte
        // is beyond the end. Stop reading data.
        return false
      default:
        throw new Error(`log stream status: ${response.statusText}`)
    }
  }

  private async streamBytes(
    signal: AbortSignal,
    stream: ReadableStream<Uint8Array>,
  ): Promise<void> {
    const reader = stream.getReader()
    try {
      for (;;) {
        const { value: chunk } = await readWithAbort(signal, reader)
        if (!chunk) {
          break
        }
        this.nextByteValue += chunk.byteLength
        if (!this.decoder) {
          this.decoder = new TextDecoder('utf-8')
        }
        this.contentTarget.append(this.decoder.decode(chunk, { stream: true }))
      }
    } finally {
      await reader.cancel()
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

function readWithAbort<T>(
  signal: AbortSignal,
  stream: ReadableStreamDefaultReader<T>,
): Promise<ReadableStreamReadResult<T>> {
  let f: EventListener
  const signalPromise = new Promise<never>((_, reject) => {
    f = () => {
      reject(new Error('abort read'))
    }
    signal.addEventListener('abort', f)
  })
  return Promise.race([
    stream.read().finally(() => signal.removeEventListener('abort', f)),
    signalPromise,
  ])
}

function skipFirst(n: number): TransformStream<Uint8Array, Uint8Array> {
  let bytesSeen = 0
  return new TransformStream({
    start() {
      bytesSeen = 0
    },

    transform(chunk, controller) {
      const chunkEnd = bytesSeen + chunk.byteLength
      if (chunkEnd > n) {
        controller.enqueue(bytesSeen >= n ? chunk : chunk.slice(n - bytesSeen))
      }
      bytesSeen += chunk.byteLength
    },
  })
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

function getSize(
  response: Pick<Response, 'headers'>,
): number | '*' | undefined {
  const contentRangeHeader = response.headers.get('Content-Range')
  if (!contentRangeHeader) {
    return undefined
  }
  const matches = contentRangeHeader.match(/bytes .*\/(\*|0|[1-9][0-9]*)/)
  const end = matches?.[1]
  if (end === '*') {
    return '*'
  }
  return end ? Number.parseInt(end, 10) : undefined
}
