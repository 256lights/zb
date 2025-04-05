// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

// @ts-check

import { exec } from 'node:child_process'
import { argv } from 'node:process'

import * as esbuild from 'esbuild'
import { stimulusPlugin } from 'esbuild-plugin-stimulus'

const isWatch = argv[2] === 'watch'
const mode = argv[2] === 'dev' || isWatch ? 'dev' : 'prod'

/**
 * @param {string} command
 * @return {Promise<void>}
 */
function execPromise(command) {
  return new Promise((resolve, reject) => {
    exec(command, {}, (error) => {
      if (error) {
        reject(error)
      } else {
        resolve()
      }
    })
  })
}

const minify = mode === 'prod'
const sourcemap = mode === 'dev'
const target = ['chrome111', 'firefox128']

if (!isWatch) {
  await execPromise(
    'tailwindcss --input src/index.css --output build/index.css',
  )
  await esbuild.build({
    entryPoints: ['build/index.css'],
    outfile: 'public/index.css',
    bundle: true,
    minify,
    sourcemap,
    target,
  })
}

const ctx = await esbuild.context({
  entryPoints: ['src/index.ts'],
  outfile: 'public/index.js',
  bundle: true,
  minify,
  sourcemap,
  target,
  plugins: [stimulusPlugin()],
})

if (isWatch) {
  const tailwind = execPromise(
    'tailwindcss --watch --input src/index.css --output public/index.css',
  )
  console.log('Watching for changes...')
  await Promise.all([ctx.watch(), tailwind])
} else {
  await ctx.rebuild()
  await ctx.dispose()
}
