import { create } from '@bufbuild/protobuf'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { BoardSchema, HistogramDatasetSchema, HistogramKind } from './gen/pet/caen/daq/v1/system_pb'
import PlotWorkspace from './PlotWorkspace.vue'

describe('PlotWorkspace', () => {
  it('requests selected channel sets and presents returned bins to the live plot', async () => {
    const wrapper = mount(PlotWorkspace, {
      props: {
        boards: [{ chain: 1, ...create(BoardSchema, { node: 2 }) }],
        running: true,
        loading: false,
        datasets: [],
        theme: 'dark',
      },
      global: {
        stubs: {
          HistogramPlot: { template: '<div aria-label="Live selected-channel histogram plot" />' },
        },
      },
    })
    await wrapper.get('input[placeholder="0, 2, 8-15"]').setValue('0, 2, 8-9')
    await wrapper.get('button').trigger('click')
    const request = wrapper.emitted('request')?.[0]
    expect(request?.[0]).toBe(HistogramKind.PHA_HIGH_GAIN)
    expect(request?.[1]).toEqual([
      expect.objectContaining({ chain: 1, node: 2, channel: 0 }),
      expect.objectContaining({ channel: 2 }),
      expect.objectContaining({ channel: 8 }),
      expect.objectContaining({ channel: 9 }),
    ])

    await wrapper.setProps({
      datasets: [
        create(HistogramDatasetSchema, {
          chain: 1,
          node: 2,
          channel: 0,
          binWidth: 4,
          entries: 3n,
          bins: [0n, 3n],
        }),
      ],
    })
    expect(wrapper.get('[aria-label="Histogram datasets"]').text()).toContain('1:3')
    wrapper.get('[aria-label="Live selected-channel histogram plot"]')
    expect(wrapper.get('[aria-label="Populated bin preview"]').text()).toContain('1:3')

    await wrapper.setProps({ running: false })
    expect(wrapper.text()).toContain('Showing the last requested histogram from the completed run.')
    wrapper.get('[aria-label="Live selected-channel histogram plot"]')
  })
})
