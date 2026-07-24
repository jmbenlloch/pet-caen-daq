import { create } from '@bufbuild/protobuf'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { RunSummarySchema, StatisticsTelemetrySchema } from './gen/pet/caen/daq/v1/system_pb'
import StatisticsTab from './StatisticsTab.vue'

function sample(elapsed: bigint, triggerCount: bigint, channelCount: bigint, chain = 0) {
  return create(StatisticsTelemetrySchema, {
    elapsedMilliseconds: elapsed,
    boards: [
      {
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
  it('switches between all-board, per-channel, interval, and integral views', async () => {
    const wrapper = mount(StatisticsTab, { props: { statistics: sample(1000n, 10n, 4n) } })
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
    const wrapper = mount(StatisticsTab, { props: { statistics: sample(1000n, 10n, 4n) } })
    await wrapper.setProps({ statistics: sample(2000n, 15n, 7n) })
    expect(wrapper.text()).toContain('5.0 Hz')

    await wrapper.setProps({ statistics: sample(2000n, 15n, 7n) })
    expect(wrapper.text()).toContain('5.0 Hz')
  })

  it('returns to all boards when the selected board disappears from telemetry', async () => {
    const wrapper = mount(StatisticsTab, { props: { statistics: sample(1000n, 10n, 4n) } })
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    expect(wrapper.get('[aria-label="Board 0 channel statistics"]').isVisible()).toBe(true)

    await wrapper.setProps({ statistics: sample(2000n, 15n, 7n, 1) })

    expect(wrapper.get('[role="tab"][aria-selected="true"]').text()).toBe('All boards')
    expect(wrapper.text()).toContain('B1')
    expect(wrapper.find('.channel-statistics').exists()).toBe(false)
  })

  it('selects a completed run and presents its fixed final statistics', async () => {
    const historical = sample(10_000n, 40n, 12n)
    const wrapper = mount(StatisticsTab, {
      props: {
        statistics: sample(2_000n, 15n, 7n),
        liveRunId: '43',
        runs: [
          create(RunSummarySchema, {
            runId: '42',
            eventCount: 40n,
            finalStatistics: historical,
          }),
          create(RunSummarySchema, { runId: 'legacy' }),
        ],
      },
    })

    const source = wrapper.get('#statistics-run')
    expect(source.findAll('option')).toHaveLength(2)
    expect(source.text()).toContain('Live · Run 43')
    expect(source.text()).toContain('Run 42 · final')

    await source.setValue('42')
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
})
