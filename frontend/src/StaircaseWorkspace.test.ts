import { create } from '@bufbuild/protobuf'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { DaqApi } from './api'
import StaircaseWorkspace from './StaircaseWorkspace.vue'
import { ScanState, StaircaseScanSchema, SystemState } from './gen/pet/caen/daq/v1/system_pb'

const stored = create(StaircaseScanSchema, {
  summary: {
    scanId: 'scan-1',
    board: 0,
    state: ScanState.COMPLETED,
    completedPoints: 2,
    totalPoints: 2,
  },
  points: [
    { threshold: 300, channelRatesCps: [20], tOrRateCps: 21, qOrRateCps: 22 },
    { threshold: 200, channelRatesCps: [100], tOrRateCps: 101, qOrRateCps: 102 },
  ],
})

function api(): DaqApi {
  return {
    listScans: vi.fn().mockResolvedValue([stored.summary]),
    staircase: vi.fn().mockResolvedValue(stored),
    startStaircase: vi.fn().mockResolvedValue(undefined),
    cancelScan: vi.fn().mockResolvedValue(undefined),
  } as unknown as DaqApi
}

describe('StaircaseWorkspace', () => {
  it('loads and plots a finalized scan', async () => {
    const client = api()
    const wrapper = mount(StaircaseWorkspace, {
      props: { api: client, systemState: SystemState.READY },
    })
    await flushPromises()
    await wrapper.get('.scan-history button').trigger('click')
    await flushPromises()
    expect(client.staircase).toHaveBeenCalledWith('scan-1')
    expect(wrapper.get('.staircase-plot polyline').attributes('points')).toContain(',')
    expect(wrapper.text()).toContain('2 / 2 points')
  })

  it('plots live telemetry points', () => {
    const live = create(StaircaseScanSchema, {
      summary: { scanId: 'live', state: ScanState.RUNNING, completedPoints: 1, totalPoints: 10 },
      points: [{ threshold: 250, channelRatesCps: [42] }],
    })
    const wrapper = mount(StaircaseWorkspace, {
      props: { api: api(), systemState: SystemState.SCANNING, live },
    })
    expect(wrapper.find('.staircase-plot').exists()).toBe(true)
    expect(wrapper.text()).toContain('1 / 10 points')
  })
})
