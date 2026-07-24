import { create } from '@bufbuild/protobuf'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { DaqApi } from './api'
import {
  BoardSchema,
  HistogramDatasetSchema,
  HistogramKind,
  RunSummarySchema,
} from './gen/pet/caen/daq/v1/system_pb'
import PlotWorkspace from './PlotWorkspace.vue'

function apiWithRuns(...runs: ReturnType<typeof createRun>[]): DaqApi {
  return {
    searchRuns: vi.fn().mockResolvedValue({ runs, nextPageToken: '' }),
  } as unknown as DaqApi
}

function createRun(runId: string) {
  return create(RunSummarySchema, {
    runId,
    artifacts: [
      {
        $typeName: 'pet.caen.daq.v1.Artifact',
        kind: 'histograms',
        name: `run_${runId}.histograms.h5`,
        sizeBytes: 1n,
        sha256: 'hash',
      },
    ],
  })
}

describe('PlotWorkspace', () => {
  it('requests the latest persisted run from the paginated picker on page load', async () => {
    const run = createRun('42')
    const wrapper = mount(PlotWorkspace, {
      props: {
        api: apiWithRuns(run),
        boards: [{ chain: 2, ...create(BoardSchema, { node: 3 }) }],
        running: false,
        loading: false,
        datasets: [],
        theme: 'light',
      },
      global: {
        stubs: {
          HistogramPlot: { template: '<div />' },
        },
      },
    })
    await flushPromises()

    expect(wrapper.emitted('request')).toHaveLength(1)
    expect(wrapper.emitted('request')?.[0]).toEqual([
      '42',
      HistogramKind.PHA_HIGH_GAIN,
      [expect.objectContaining({ chain: 2, node: 3, channel: 0 })],
    ])
  })

  it('switches from a persisted run to the live plot when a run starts', async () => {
    const persistedRun = createRun('41')
    const wrapper = mount(PlotWorkspace, {
      props: {
        api: apiWithRuns(persistedRun),
        boards: [{ chain: 0, ...create(BoardSchema, { node: 0 }) }],
        running: false,
        loading: false,
        datasets: [],
        theme: 'light',
      },
      global: {
        stubs: {
          HistogramPlot: { template: '<div />' },
        },
      },
    })
    await flushPromises()

    expect(wrapper.get('.plot-run-source').text()).toContain('Run 41')

    await wrapper.setProps({
      activeRunId: '42',
      running: true,
    })

    expect(wrapper.get('.plot-run-source').text()).toContain('Run 42 · live')
    expect(wrapper.text()).not.toContain('Viewing persisted histograms')
  })

  it('requests selected channel sets and presents returned bins to the live plot', async () => {
    const wrapper = mount(PlotWorkspace, {
      props: {
        api: apiWithRuns(createRun('41')),
        boards: [
          { chain: 1, ...create(BoardSchema, { node: 2 }) },
          { chain: 3, ...create(BoardSchema, { node: 0 }) },
        ],
        activeRunId: '42',
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
    expect(wrapper.findAll('.histogram-board-selector header .secondary')).toHaveLength(4)
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
    expect(request?.[0]).toBe('42')
    expect(request?.[1]).toBe(HistogramKind.PHA_HIGH_GAIN)
    expect(request?.[2]).toEqual([
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
          bins: [0, 3, 0],
        }),
      ],
    })
    expect(wrapper.get('[aria-label="Histogram datasets"]').text()).toContain('3 entries')
    expect(wrapper.get('[aria-label="Histogram datasets"]').text()).toContain(
      '1 populated bins · peak 3',
    )
    wrapper.get('[aria-label="Live selected-channel histogram plot"]')
    expect(wrapper.text()).not.toContain('First populated bins')

    await wrapper.setProps({
      activeRunId: undefined,
      running: false,
    })
    await flushPromises()
    expect(wrapper.get('.plot-run-source').text()).toContain('Run 41')
    expect(wrapper.text()).toContain('Viewing persisted histograms from run 41.')
    wrapper.get('[aria-label="Live selected-channel histogram plot"]')
  })

  it('keeps the manual request button stable during automatic refresh', async () => {
    const wrapper = mount(PlotWorkspace, {
      props: {
        api: apiWithRuns(),
        boards: [{ chain: 0, ...create(BoardSchema, { node: 0 }) }],
        activeRunId: '42',
        running: true,
        loading: false,
        datasets: [],
        theme: 'dark',
      },
      global: {
        stubs: {
          HistogramPlot: { template: '<div />' },
        },
      },
    })
    const requestButton = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Request data')!

    await wrapper.setProps({ loading: true })

    expect(requestButton.text()).toBe('Request data')
    expect(requestButton.attributes('disabled')).toBeUndefined()
    expect(requestButton.attributes('aria-busy')).toBe('true')
  })

  it('enforces the 64-channel request limit in the selector', async () => {
    const wrapper = mount(PlotWorkspace, {
      props: {
        api: apiWithRuns(),
        boards: [
          { chain: 0, ...create(BoardSchema, { node: 0 }) },
          { chain: 1, ...create(BoardSchema, { node: 0 }) },
        ],
        activeRunId: '42',
        running: true,
        loading: false,
        datasets: [],
        theme: 'light',
      },
      global: {
        stubs: {
          HistogramPlot: { template: '<div />' },
        },
      },
    })

    await wrapper.get('[aria-haspopup="true"]').trigger('click')
    const allButtons = wrapper
      .findAll('.histogram-board-selector header button')
      .filter((button) => button.text() === 'All')
    await allButtons[0].trigger('click')

    expect(wrapper.get('[aria-haspopup="true"]').text()).toContain('64 / 64 selected')
    expect(wrapper.get('[role="alert"]').text()).toContain('limited to 64 channels')
    expect(
      wrapper.get('[aria-label="Board 1 node 0 channel 0"]').attributes('disabled'),
    ).toBeDefined()

    await allButtons[1].trigger('click')
    const requestButton = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Request data')
    await requestButton!.trigger('click')
    expect(wrapper.emitted('request')?.[0]?.[2]).toHaveLength(64)
  })
})
