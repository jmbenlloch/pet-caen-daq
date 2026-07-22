import { create } from '@bufbuild/protobuf'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { HistogramDatasetSchema } from './gen/pet/caen/daq/v1/system_pb'
import HistogramPlot from './HistogramPlot.vue'

const setData = vi.hoisted(() => vi.fn())

vi.mock('uplot', () => {
  class MockPlot {
    static paths = { stepped: () => () => null }
    setData = setData
    setSize = vi.fn()
    destroy = vi.fn()
  }
  return { default: MockPlot }
})

describe('HistogramPlot', () => {
  it('resets both plot scales to the complete dataset', async () => {
    const dataset = create(HistogramDatasetSchema, {
      chain: 0,
      node: 0,
      channel: 4,
      minimum: 0,
      binWidth: 1,
      bins: [0n, 3n, 1n],
    })
    const wrapper = mount(HistogramPlot, {
      props: { datasets: [dataset], theme: 'dark', logarithmic: false },
    })

    setData.mockClear()
    await wrapper.get('button').trigger('click')
    expect(setData).toHaveBeenCalledWith([[0.5, 1.5, 2.5], [0, 3, 1]], true)
  })
})
