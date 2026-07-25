import { create } from '@bufbuild/protobuf'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { HoldDelayPointSchema } from './gen/pet/caen/daq/v1/system_pb'
import HoldDelayPlot from './HoldDelayPlot.vue'

const constructed = vi.hoisted(() => vi.fn())
const setData = vi.hoisted(() => vi.fn())

vi.mock('uplot', () => {
  class MockPlot {
    constructor(options: unknown, data: unknown) {
      constructed(options, data)
    }
    setData = setData
    setScale = vi.fn()
    setSize = vi.fn()
    destroy = vi.fn()
  }
  return { default: MockPlot }
})

function point(delay: number, counts: number[]) {
  return create(HoldDelayPointSchema, {
    effectiveDelayNs: delay,
    channels: [{ channel: 0, highGainBins: counts }],
  })
}

describe('HoldDelayPlot', () => {
  beforeEach(() => {
    constructed.mockClear()
    setData.mockClear()
  })

  it('pads the delay scale to show complete heatmap cells and displays a color scale', () => {
    const wrapper = mount(HoldDelayPlot, {
      props: { points: [point(0, [0, 4]), point(8, [0, 20])], channel: 0, theme: 'light' },
    })

    const [options, data] = constructed.mock.calls[0]
    expect(data).toEqual([
      [0, 8],
      [0, 0],
    ])
    expect(options.scales.x.range(undefined, 0, 8)).toEqual([-4, 12])
    expect(wrapper.get('.heatmap-scale').attributes('aria-label')).toContain('Logarithmic')
    expect(wrapper.get('.heatmap-scale').text()).toContain('20')
    expect(wrapper.get('.heatmap-scale').text()).toContain('log scale')
  })

  it('updates the existing plot as live points arrive without rebuilding the canvas', async () => {
    const wrapper = mount(HoldDelayPlot, {
      props: { points: [point(0, [1])], channel: 0, theme: 'dark' },
    })

    await wrapper.setProps({ points: [point(0, [1]), point(8, [2])] })

    expect(constructed).toHaveBeenCalledTimes(1)
    expect(setData).toHaveBeenCalledWith(
      [
        [0, 8],
        [0, 0],
      ],
      true,
    )
  })
})
