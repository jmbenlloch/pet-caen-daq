import { create } from '@bufbuild/protobuf'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { DaqApi } from './api'
import {
  RunSummarySchema,
  StatisticsTelemetrySchema,
  type RunSummary,
} from './gen/pet/caen/daq/v1/system_pb'
import StatisticsTab from './StatisticsTab.vue'

function apiWithRuns(...runs: RunSummary[]): DaqApi {
  return {
    searchRuns: vi.fn().mockResolvedValue({ runs, nextPageToken: '' }),
  } as unknown as DaqApi
}

function sample(elapsed: bigint, triggerCount: bigint, channelCount: bigint, chain = 0) {
  return create(StatisticsTelemetrySchema, {
    elapsedMilliseconds: elapsed,
    boards: [
      {
        logicalIndex: chain,
        chain,
        timestamp: 125_000_000n,
        triggerId: 9n,
        triggerCount,
        lostTriggerCount: 2n,
        tOrCount: triggerCount * 2n,
        dataBytes: triggerCount * 100n,
        channelTriggerCounts: [channelCount, ...Array(63).fill(0n)],
        timestampCounts: Array(64).fill(2n),
        phaCounts: Array(64).fill(1n),
      },
    ],
  })
}

describe('StatisticsTab', () => {
  it('distinguishes multiple logical boards on the same daisy chain', async () => {
    const statistics = create(StatisticsTelemetrySchema, {
      elapsedMilliseconds: 1000n,
      boards: [
        { logicalIndex: 4, chain: 1, node: 0, channelTriggerCounts: Array(64).fill(0n) },
        { logicalIndex: 5, chain: 1, node: 1, channelTriggerCounts: Array(64).fill(0n) },
      ],
    })
    const wrapper = mount(StatisticsTab, {
      props: { api: apiWithRuns(), statistics },
    })

    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs.map((tab) => tab.text())).toEqual(['All boards', 'Board 4', 'Board 5'])
    await tabs[2].trigger('click')
    expect(wrapper.get('[aria-label="Board 5 channel statistics"]').isVisible()).toBe(true)
  })

  it('switches between all-board, per-channel, interval, and integral views', async () => {
    const wrapper = mount(StatisticsTab, {
      props: { api: apiWithRuns(), statistics: sample(1000n, 10n, 4n) },
    })
    expect(wrapper.text()).toContain('Trigger ID')
    expect(wrapper.text()).toContain('1.000 s')
    expect(wrapper.text()).toContain('Estimated lost triggers')
    expect(wrapper.text()).toContain('T-OR rate')
    expect(wrapper.text()).not.toContain('Event build')
    expect(wrapper.text()).toContain('Select a board for per-channel metrics.')
    expect(wrapper.find('.statistics-channel-toolbar select').exists()).toBe(false)

    await wrapper.setProps({ statistics: sample(2000n, 15n, 7n) })
    expect(wrapper.text()).toContain('10.0 Hz')
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    expect(wrapper.text()).toContain('Per-channel metric')
    expect(wrapper.text()).toContain('Discriminator firings reported for each detector channel.')
    expect(wrapper.text()).toContain('How to read and configure this panel')
    expect(wrapper.get('[aria-label="Board 0 channel statistics"]').text()).toContain('3.0 Hz')

    await wrapper.get('input[type="checkbox"]').setValue(true)
    expect(wrapper.get('.channel-statistic').text()).toBe('CH 07')

    await wrapper.get('.statistics-channel-toolbar select').setValue('phaCounts')
    expect(wrapper.text()).toContain('PHA integrated count')
  })

  it('keeps the last measured rate when a final snapshot has the same elapsed time', async () => {
    const wrapper = mount(StatisticsTab, {
      props: { api: apiWithRuns(), statistics: sample(1000n, 10n, 4n) },
    })
    await wrapper.setProps({ statistics: sample(2000n, 15n, 7n) })
    expect(wrapper.text()).toContain('5.0 Hz')

    await wrapper.setProps({ statistics: sample(2000n, 15n, 7n) })
    expect(wrapper.text()).toContain('5.0 Hz')
  })

  it('returns to all boards when the selected board disappears from telemetry', async () => {
    const wrapper = mount(StatisticsTab, {
      props: { api: apiWithRuns(), statistics: sample(1000n, 10n, 4n) },
    })
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    expect(wrapper.get('[aria-label="Board 0 channel statistics"]').isVisible()).toBe(true)

    await wrapper.setProps({ statistics: sample(2000n, 15n, 7n, 1) })

    expect(wrapper.get('[role="tab"][aria-selected="true"]').text()).toBe('All boards')
    expect(wrapper.text()).toContain('Board 1')
    expect(wrapper.find('.channel-statistics').exists()).toBe(false)
  })

  it('selects a completed run and presents its fixed final statistics', async () => {
    const historical = sample(10_000n, 40n, 12n)
    const historicalRun = create(RunSummarySchema, {
      runId: '42',
      eventCount: 40n,
      finalStatistics: historical,
    })
    const wrapper = mount(StatisticsTab, {
      props: {
        api: apiWithRuns(historicalRun, create(RunSummarySchema, { runId: '41' })),
        statistics: sample(2_000n, 15n, 7n),
        liveRunId: '43',
      },
    })
    await flushPromises()

    const picker = wrapper.get('[aria-label="Select run with final statistics"]')
    expect(picker.findAll('.run-data-row')).toHaveLength(2)
    expect(wrapper.get('.statistics-live-source').text()).toContain('Run 43')
    expect(picker.text()).toContain('No final statistics')

    await picker.findAll('.run-data-row')[0].trigger('click')
    expect(wrapper.text()).toContain('Final run snapshot')
    expect(wrapper.text()).toContain('40 decoded events')
    expect(wrapper.text()).toContain('4.0 Hz')
    expect(wrapper.text()).toContain('40 total')

    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    expect(wrapper.get('.channel-statistic').text()).toBe('CH 01.2 Hz')
    expect(wrapper.text()).toContain('average rate over the completed run')

    await wrapper.get('input[type="checkbox"]').setValue(true)
    expect(wrapper.get('.channel-statistic').text()).toBe('CH 012')
    expect(wrapper.text()).toContain('integrated count')
  })

  it('returns to live statistics when a new run starts', async () => {
    const historicalRun = create(RunSummarySchema, {
      runId: '42',
      eventCount: 40n,
      finalStatistics: sample(10_000n, 40n, 12n),
    })
    const wrapper = mount(StatisticsTab, {
      props: {
        api: apiWithRuns(historicalRun),
        statistics: sample(2_000n, 15n, 7n),
      },
    })
    await flushPromises()

    await wrapper.get('.run-data-row').trigger('click')
    expect(wrapper.text()).toContain('Final run snapshot')
    expect(wrapper.get('.statistics-live-source').attributes('aria-pressed')).toBe('false')

    await wrapper.setProps({ liveRunId: '43', statistics: sample(1_000n, 2n, 1n) })

    expect(wrapper.text()).toContain('Live runtime view')
    expect(wrapper.get('.statistics-live-source').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('.statistics-live-source').text()).toContain('Run 43')
  })
})
