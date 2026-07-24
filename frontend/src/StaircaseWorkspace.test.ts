import { create } from '@bufbuild/protobuf'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { DaqApi } from './api'
import StaircaseWorkspace from './StaircaseWorkspace.vue'
import {
  ScanState,
  ScanSummarySchema,
  StaircaseScanSchema,
  SystemState,
} from './gen/pet/caen/daq/v1/system_pb'
import { localDateTime } from './presentation'

vi.mock('uplot', () => {
  class MockPlot {
    setData = vi.fn()
    setSize = vi.fn()
    destroy = vi.fn()
  }
  return { default: MockPlot }
})

const stored = create(StaircaseScanSchema, {
  summary: {
    scanId: '42',
    board: 0,
    startedAt: { seconds: 1_784_741_400n, nanos: 0 },
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
    listScans: vi.fn().mockResolvedValue({ scans: [stored.summary], totalCount: 1 }),
    staircase: vi.fn().mockResolvedValue(stored),
    startStaircase: vi.fn().mockResolvedValue(undefined),
    cancelScan: vi.fn().mockResolvedValue(undefined),
  } as unknown as DaqApi
}

describe('StaircaseWorkspace', () => {
  it('uses the standard plots panel treatment and primary scan action', () => {
    const wrapper = mount(StaircaseWorkspace, {
      props: { api: api(), systemState: SystemState.READY, theme: 'dark' },
    })

    expect(wrapper.get('section').classes()).toEqual(
      expect.arrayContaining(['panel', 'plots', 'staircase-workspace']),
    )
    expect(wrapper.get('button.primary').text()).toBe('Start scan')
    expect(wrapper.get('.scan-history-title button').classes()).toContain('link-button')
    expect(wrapper.text()).toContain('Dwell time is the counting interval at each threshold')
  })

  it('loads and plots a finalized scan', async () => {
    const client = api()
    const wrapper = mount(StaircaseWorkspace, {
      props: { api: client, systemState: SystemState.READY, theme: 'dark' },
    })
    await flushPromises()
    await wrapper.get('.scan-history button').trigger('click')
    await flushPromises()
    expect(client.listScans).toHaveBeenCalledWith(8, 0, undefined)
    expect(client.staircase).toHaveBeenCalledWith('42')
    expect(wrapper.get('.staircase-plot').attributes('aria-label')).toContain(
      'trigger rate by coarse threshold',
    )
    expect(wrapper.text()).toContain('Run 42')
    expect(wrapper.text()).toContain(localDateTime(stored.summary?.startedAt))
    expect(wrapper.get('.scan-history-header').text()).toContain('Started')
    expect(wrapper.get('.scan-history-header').text()).toContain('Board')
    expect(wrapper.get('.scan-history > button').text()).toContain('Completed')
    expect(wrapper.text()).toContain('2 / 2 points')
  })

  it('paginates finalized scans eight at a time', async () => {
    const client = api()
    const scans = Array.from({ length: 9 }, (_, index) =>
      create(ScanSummarySchema, {
        scanId: String(100 - index),
        startedAt: { seconds: BigInt(1_784_741_400 - index), nanos: 0 },
      }),
    )
    vi.mocked(client.listScans).mockImplementation(async (limit = 8, offset = 0) => ({
      scans: scans.slice(offset, offset + limit),
      totalCount: scans.length,
    }))
    const wrapper = mount(StaircaseWorkspace, {
      props: { api: client, systemState: SystemState.READY, theme: 'dark' },
    })
    await flushPromises()

    expect(wrapper.findAll('.scan-history > button')).toHaveLength(8)
    expect(wrapper.text()).toContain('Page 1 of 2')
    await wrapper.get('[aria-label="Scan history pages"] button:last-child').trigger('click')
    await flushPromises()
    expect(client.listScans).toHaveBeenLastCalledWith(8, 8, undefined)
    expect(wrapper.findAll('.scan-history > button')).toHaveLength(1)
    expect(wrapper.text()).toContain('Run 92')
  })

  it('filters scan history by board before pagination', async () => {
    const client = api()
    const scans = [
      create(ScanSummarySchema, { scanId: '12', board: 0 }),
      create(ScanSummarySchema, { scanId: '11', board: 2 }),
      create(ScanSummarySchema, { scanId: '10', board: 2 }),
    ]
    vi.mocked(client.listScans).mockImplementation(async (_limit, _offset, boardFilter) => {
      const matching =
        boardFilter === undefined ? scans : scans.filter((scan) => scan.board === boardFilter)
      return { scans: matching, totalCount: matching.length }
    })
    const wrapper = mount(StaircaseWorkspace, {
      props: { api: client, systemState: SystemState.READY, theme: 'dark' },
    })
    await flushPromises()

    const filter = wrapper.get('[aria-label="Filter scans by board"]')
    expect(filter.findAll('option').map((option) => option.text())).toEqual([
      'Any board',
      'Board 0',
      'Board 1',
      'Board 2',
      'Board 3',
    ])
    await filter.setValue('2')
    await flushPromises()
    expect(client.listScans).toHaveBeenLastCalledWith(8, 0, 2)
    expect(wrapper.findAll('.scan-history > button')).toHaveLength(2)
    expect(wrapper.text()).toContain('Run 11')
    expect(wrapper.text()).toContain('Run 10')
    expect(wrapper.text()).not.toContain('Run 12')
  })

  it('plots live telemetry points', () => {
    const live = create(StaircaseScanSchema, {
      summary: { scanId: 'live', state: ScanState.RUNNING, completedPoints: 1, totalPoints: 10 },
      points: [{ threshold: 250, channelRatesCps: [42] }],
    })
    const wrapper = mount(StaircaseWorkspace, {
      props: { api: api(), systemState: SystemState.SCANNING, theme: 'dark', live },
    })
    expect(wrapper.find('.staircase-plot').exists()).toBe(true)
    expect(wrapper.text()).toContain('1 / 10 points')
  })
})
