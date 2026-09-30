import { create } from '@bufbuild/protobuf'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { HistogramDatasetSchema } from './gen/pet/caen/daq/v1/system_pb'
import HistogramPlot from './HistogramPlot.vue'

const constructed = vi.hoisted(() => vi.fn())
const setData = vi.hoisted(() => vi.fn())
const setScale = vi.hoisted(() => vi.fn())
const bars = vi.hoisted(() => vi.fn(() => () => null))

vi.mock('uplot', () => {
  class MockPlot {
    static paths = { bars }
    constructor(options: unknown, data: unknown) {
      constructed(options, data)
    }
    setData = setData
    setScale = setScale
    setSize = vi.fn()
    destroy = vi.fn()
  }
  return { default: MockPlot }
})

describe('HistogramPlot', () => {
  it('plots the server board sum alongside channel zero without summing in the browser', () => {
    const channel = create(HistogramDatasetSchema, { chain: 1, node: 2, bins: [1, 2], binWidth: 1 })
    const sum = create(HistogramDatasetSchema, {
      chain: 1,
      node: 2,
      boardSum: true,
      boardSumBins: [4294967296n, 9n],
      binWidth: 1,
    })
    const wrapper = mount(HistogramPlot, {
      props: { datasets: [channel, sum], theme: 'light', logarithmic: false },
    })
    expect(constructed.mock.calls.at(-1)?.[1]).toEqual([
      [0.5, 1.5],
      [1, 2],
      [4294967296, 9],
    ])
    expect(constructed.mock.calls.at(-1)?.[0].series[2].label).toContain('Node 2 · Board sum')
    wrapper.unmount()
  })
  it('adjusts the count scale when live or stored histogram data changes', async () => {
    const initial = create(HistogramDatasetSchema, {
      chain: 0,
      node: 0,
      channel: 4,
      minimum: 0,
      binWidth: 1,
      bins: [0, 3, 1],
    })
    const wrapper = mount(HistogramPlot, {
      props: { datasets: [initial], theme: 'dark', logarithmic: false },
    })

    setData.mockClear()
    setScale.mockClear()
    const updated = create(HistogramDatasetSchema, {
      chain: 0,
      node: 0,
      channel: 4,
      minimum: 0,
      binWidth: 1,
      bins: [0, 30, 1],
    })
    await wrapper.setProps({ datasets: [updated] })

    expect(setData).toHaveBeenCalledWith(
      [
        [0.5, 1.5, 2.5],
        [0, 30, 1],
      ],
      false,
    )
    expect(setScale).toHaveBeenCalledWith('y', { min: 0, max: 31.5 })
  })

  it('resets both plot scales to the complete dataset', async () => {
    const dataset = create(HistogramDatasetSchema, {
      chain: 0,
      node: 0,
      channel: 4,
      minimum: 0,
      binWidth: 1,
      bins: [0, 3, 1],
    })
    const wrapper = mount(HistogramPlot, {
      props: { datasets: [dataset], theme: 'dark', logarithmic: false },
    })

    expect(bars).toHaveBeenCalledWith({ size: [0.9, 8, 1] })
    setData.mockClear()
    await wrapper.get('button').trigger('click')
    expect(setData).toHaveBeenCalledWith(
      [
        [0.5, 1.5, 2.5],
        [0, 3, 1],
      ],
      true,
    )
  })
})
