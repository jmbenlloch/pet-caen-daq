import { create } from '@bufbuild/protobuf'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { BoardSchema, HistogramDatasetSchema, HistogramKind } from './gen/pet/caen/daq/v1/system_pb'
import PlotWorkspace from './PlotWorkspace.vue'

describe('PlotWorkspace', () => {
  it('requests selected channel sets and presents returned bins to the live plot', async () => {
    const wrapper = mount(PlotWorkspace, {
      props: {
        boards: [
          { chain: 1, ...create(BoardSchema, { node: 2 }) },
          { chain: 3, ...create(BoardSchema, { node: 0 }) },
        ],
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
    await wrapper.get('[aria-haspopup="true"]').trigger('click')
    await wrapper.get('[aria-label="Board 1 node 2 channel 2"]').trigger('click')
    await wrapper.get('[aria-label="Board 1 node 2 channel 8"]').trigger('click')
    await wrapper.get('[aria-label="Board 1 node 2 channel 9"]').trigger('click')
    await wrapper.get('[aria-label="Board 3 node 0 channel 4"]').trigger('click')
    const requestButton = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Request data')
    expect(requestButton).toBeDefined()
    await requestButton!.trigger('click')
    const request = wrapper.emitted('request')?.[0]
    expect(request?.[0]).toBe(HistogramKind.PHA_HIGH_GAIN)
    expect(request?.[1]).toEqual([
      expect.objectContaining({ chain: 1, node: 2, channel: 0 }),
      expect.objectContaining({ channel: 2 }),
      expect.objectContaining({ channel: 8 }),
      expect.objectContaining({ channel: 9 }),
      expect.objectContaining({ chain: 3, node: 0, channel: 4 }),
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
    expect(wrapper.get('[aria-label="Histogram datasets"]').text()).toContain('3 entries')
    wrapper.get('[aria-label="Live selected-channel histogram plot"]')
    expect(wrapper.text()).not.toContain('First populated bins')

    await wrapper.setProps({ running: false })
    expect(wrapper.text()).toContain('Showing the last requested histogram from the completed run.')
    wrapper.get('[aria-label="Live selected-channel histogram plot"]')
  })
})
