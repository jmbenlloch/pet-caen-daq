import { create } from '@bufbuild/protobuf'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { DaqApi } from './api'
import {
  RunSummarySchema,
  StatisticsTelemetrySchema,
  type SearchRunsRequest,
} from './gen/pet/caen/daq/v1/system_pb'
import RunDataPicker from './RunDataPicker.vue'

function statisticsRun(runId: string) {
  return create(RunSummarySchema, {
    runId,
    eventCount: 10n,
    finalStatistics: create(StatisticsTelemetrySchema),
  })
}

describe('RunDataPicker', () => {
  it('navigates server pages in both directions and selects an available run', async () => {
    const first = statisticsRun('102')
    const unavailable = create(RunSummarySchema, { runId: '101' })
    const second = statisticsRun('94')
    const searchRuns = vi.fn(async (request: SearchRunsRequest) =>
      request.pageToken
        ? { runs: [second], nextPageToken: '' }
        : { runs: [first, unavailable], nextPageToken: 'page-2' },
    )
    const wrapper = mount(RunDataPicker, {
      props: {
        api: { searchRuns } as unknown as DaqApi,
        capability: 'statistics',
      },
    })
    await flushPromises()

    expect(wrapper.get('details').attributes('open')).toBeUndefined()
    await wrapper.get('summary').trigger('click')
    expect(wrapper.get('details').attributes('open')).toBe('')
    expect(searchRuns).toHaveBeenCalledWith(expect.objectContaining({ limit: 8, pageToken: '' }))
    expect(searchRuns.mock.calls[0][0].runNumber).toBeUndefined()
    expect(wrapper.findAll('.run-data-row')).toHaveLength(2)
    expect(wrapper.findAll('.run-data-row')[1].attributes('disabled')).toBeDefined()

    await wrapper.findAll('.run-data-row')[0].trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual([first])

    await wrapper.get('.run-pagination button:last-child').trigger('click')
    await flushPromises()
    expect(wrapper.get('.run-pagination').text()).toContain('Page 2')
    expect(searchRuns).toHaveBeenLastCalledWith(expect.objectContaining({ pageToken: 'page-2' }))

    await wrapper.get('.run-pagination button:first-child').trigger('click')
    await flushPromises()
    expect(wrapper.get('.run-pagination').text()).toContain('Page 1')
    expect(searchRuns).toHaveBeenLastCalledWith(expect.objectContaining({ pageToken: '' }))
  })

  it('searches an exact run number and clears back to the paginated list', async () => {
    const searchRuns = vi.fn().mockResolvedValue({ runs: [], nextPageToken: '' })
    const wrapper = mount(RunDataPicker, {
      props: {
        api: { searchRuns } as unknown as DaqApi,
        capability: 'histograms',
      },
    })
    await flushPromises()

    await wrapper.get('input[type="number"]').setValue('42')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(searchRuns).toHaveBeenLastCalledWith(expect.objectContaining({ runNumber: 42n }))
    expect(wrapper.text()).toContain('Run not found.')

    await wrapper.findAll('.run-data-search button')[1].trigger('click')
    await flushPromises()
    expect(searchRuns.mock.lastCall?.[0].runNumber).toBeUndefined()
  })
})
