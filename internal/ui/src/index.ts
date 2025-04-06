// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

import { Application } from '@hotwired/stimulus'
import '@hotwired/turbo'

import { definitions } from 'stimulus:./controllers'

declare global {
  interface Window {
    Stimulus?: Application
  }
}

window.Stimulus = Application.start()
window.Stimulus.registerActionOption(
  'documentHidden',
  ({ value }) => document.hidden === value,
)
window.Stimulus.load(definitions)
