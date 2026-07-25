import { create } from '@bufbuild/protobuf'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { DaqApi } from './api'
import { HoldDelayScanSchema, ScanState, SystemState } from './gen/pet/caen/daq/v1/system_pb'
import HoldDelayWorkspace from './HoldDelayWorkspace.vue'

vi.mock('uplot', () => {
  class MockPlot {
    setData = vi.fn()
    setScale = vi.fn()
    setSize = vi.fn()
    destroy = vi.fn()
  }
  return { default: MockPlot }
})

function api(): DaqApi {
  return {
    listScans: vi.fn().mockResolvedValue({ scans: [], totalCount: 0 }),
  } as unknown as DaqApi
}

describe('HoldDelayWorkspace', () => {
  it('is collapsed by default and mounts the live heatmap only when expanded', async () => {
    const live = create(HoldDelayScanSchema, {
      summary: { scanId: '61', state: ScanState.RUNNING, completedPoints: 1, totalPoints: 33 },
      points: [
        {
          effectiveDelayNs: 0,
          channels: [{ channel: 0, highGainBins: [0, 3] }],
        },
      ],
    })
    const wrapper = mount(HoldDelayWorkspace, {
      props: { api: api(), systemState: SystemState.SCANNING, theme: 'light', live },
    })

    expect(wrapper.get('.scan-card-toggle').attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('.hold-delay-plot').exists()).toBe(false)

    await wrapper.get('.scan-card-toggle').trigger('click')

    expect(wrapper.get('.scan-card-toggle').attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('.hold-delay-plot').attributes('aria-label')).toContain('channel 0')
    expect(wrapper.find('.heatmap-scale').exists()).toBe(true)
  })

  it('mounts the heatmap before the first live point arrives', async () => {
    const live = create(HoldDelayScanSchema, {
      summary: { scanId: '64', state: ScanState.RUNNING, completedPoints: 0, totalPoints: 33 },
      minimumDelayNs: 0,
      maximumDelayNs: 256,
    })
    const wrapper = mount(HoldDelayWorkspace, {
      props: { api: api(), systemState: SystemState.SCANNING, theme: 'light', live },
    })

    await wrapper.get('.scan-card-toggle').trigger('click')

    expect(wrapper.get('.hold-delay-plot').attributes('aria-label')).toContain('channel 0')
    expect(wrapper.find('.empty').exists()).toBe(false)
  })
})
