// esbuild-plugin-stimulus.d.ts

declare module 'stimulus:*' {
  import type { Definition } from '@hotwired/stimulus'
  export const definitions: Definition[]
}
