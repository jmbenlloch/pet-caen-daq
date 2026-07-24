import { create } from '@bufbuild/protobuf'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { StaircasePointSchema } from './gen/pet/caen/daq/v1/system_pb'
import StaircasePlot from './StaircasePlot.vue'

const constructed = vi.hoisted(() => vi.fn())
const setData = vi.hoisted(() => vi.fn())

vi.mock('uplot', () => {
  class MockPlot {
    constructor(options: unknown, data: unknown) {
      constructed(options, data)
    }
    setData = setData
    setSize = vi.fn()
    destroy = vi.fn()
  }
  return { default: MockPlot }
})

describe('StaircasePlot', () => {
  beforeEach(() => {
    constructed.mockClear()
    setData.mockClear()
  })

  it('plots sorted staircase rates with uPlot and resets the complete scale', async () => {
    const points = [
      create(StaircasePointSchema, { threshold: 300, channelRatesCps: [20] }),
      create(StaircasePointSchema, { threshold: 200, channelRatesCps: [100] }),
    ]
    const wrapper = mount(StaircasePlot, {
      props: { points, seriesKey: 'channel:0', theme: 'light' },
    })

    const [options, data] = constructed.mock.calls[0]
    expect(data).toEqual([
      [200, 300],
      [100, 20],
    ])
    expect(options.axes[0].label).toBe('Coarse threshold (DAC)')
    expect(options.axes[1].label).toBe('Rate (cps)')

    await wrapper.get('button').trigger('click')
    expect(setData).toHaveBeenCalledWith(
      [
        [200, 300],
        [100, 20],
      ],
      true,
    )
  })

  it('updates the data and scale as live staircase points arrive', async () => {
    const wrapper = mount(StaircasePlot, {
      props: {
        points: [create(StaircasePointSchema, { threshold: 500, channelRatesCps: [2] })],
        seriesKey: 'channel:0',
        theme: 'light',
      },
    })

    await wrapper.setProps({
      points: [
        create(StaircasePointSchema, { threshold: 500, channelRatesCps: [2] }),
        create(StaircasePointSchema, { threshold: 495, channelRatesCps: [2_000] }),
      ],
    })

    expect(setData).toHaveBeenCalledWith(
      [
        [495, 500],
        [2_000, 2],
      ],
      true,
    )
  })
})
