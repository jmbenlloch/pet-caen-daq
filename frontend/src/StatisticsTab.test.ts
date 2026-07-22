import { create } from '@bufbuild/protobuf'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { StatisticsTelemetrySchema } from './gen/pet/caen/daq/v1/system_pb'
import StatisticsTab from './StatisticsTab.vue'

function sample(elapsed: bigint, triggerCount: bigint, channelCount: bigint) {
  return create(StatisticsTelemetrySchema, {
    elapsedMilliseconds: elapsed,
    boards: [
      {
        chain: 0,
        timestamp: 123n,
        triggerId: 9n,
        triggerCount,
        eventBuildCount: triggerCount,
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

    await wrapper.setProps({ statistics: sample(2000n, 15n, 7n) })
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    expect(wrapper.get('[aria-label="Board 0 channel statistics"]').text()).toContain('3.0 Hz')

    await wrapper.get('input[type="checkbox"]').setValue(true)
    expect(wrapper.get('.channel-statistic').text()).toBe('CH 07')

    await wrapper.get('select').setValue('phaCounts')
    expect(wrapper.text()).toContain('PHA integrated count')
  })

  it('keeps the last measured rate when a final snapshot has the same elapsed time', async () => {
    const wrapper = mount(StatisticsTab, { props: { statistics: sample(1000n, 10n, 4n) } })
    await wrapper.setProps({ statistics: sample(2000n, 15n, 7n) })
    expect(wrapper.text()).toContain('5.0 Hz')

    await wrapper.setProps({ statistics: sample(2000n, 15n, 7n) })
    expect(wrapper.text()).toContain('5.0 Hz')
  })
})
