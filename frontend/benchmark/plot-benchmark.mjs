/* global process */
import { chromium } from '@playwright/test'

const baseURL = process.env.BENCHMARK_URL ?? 'http://127.0.0.1:4174/benchmark.html'
const browser = await chromium.launch({
  headless: true,
  args: ['--enable-precise-memory-info', '--js-flags=--expose-gc'],
})
const cases = [
  [1, 4096],
  [8, 4096],
  [32, 8192],
  [64, 16384],
]
const results = []
const themes = (process.env.BENCHMARK_THEMES ?? 'dark,light').split(',')
for (const library of ['echarts', 'uplot']) {
  for (const theme of themes) {
    for (const [series, bins] of cases) {
      const page = await browser.newPage({ viewport: { width: 1280, height: 720 } })
      await page.goto(baseURL)
      const result = await page.evaluate((input) => globalThis.runPlotBenchmark(input), {
        library,
        theme,
        series,
        bins,
      })
      results.push(result)
      process.stdout.write(`${JSON.stringify(result)}\n`)
      await page.close()
    }
  }
}
await browser.close()

const aggregate = results.reduce((groups, result) => {
  ;(groups[result.library] ??= []).push(result)
  return groups
}, {})
for (const [library, values] of Object.entries(aggregate)) {
  const worst = values.find(
    (value) => value.series === 64 && value.bins === 16384 && value.theme === 'dark',
  )
  process.stdout.write(`SUMMARY ${library} worst=${JSON.stringify(worst)}\n`)
}
