import { create, fromJson } from '@bufbuild/protobuf'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import RunHistoryTable from './RunHistoryTable.vue'
import { RunSummarySchema } from './gen/pet/caen/daq/v1/system_pb'
import { TimestampSchema } from '@bufbuild/protobuf/wkt'

function timestamp(value: string) {
  return fromJson(TimestampSchema, value)
}

describe('run history table', () => {
  it('paginates runs and presents complete details with readable configuration', async () => {
    const runs = Array.from({ length: 12 }, (_, index) =>
      create(RunSummarySchema, {
        runId: String(12 - index),
        startedAt: timestamp('2026-07-24T10:00:00Z'),
        completedAt: timestamp('2026-07-24T10:02:05Z'),
        eventCount: 1200n,
        rawBatchCount: 42n,
        terminationReason: 'operator_stop',
        stopMode: 'MANUAL',
        artifacts: [{ name: 'run.h5', kind: 'hdf5', sizeBytes: 5n * 1024n * 1024n }],
      }),
    )
    const configuration = vi
      .fn()
      .mockResolvedValue(
        '# Run control\nStopRunMode MANUAL # Run Stop Mode\n# HV bias\nHV_Vbias 55.0 # Bias voltage',
      )
    const downloadArtifact = vi.fn().mockResolvedValue(undefined)
    const wrapper = mount(RunHistoryTable, {
      props: { runs, configuration, downloadArtifact },
    })

    expect(wrapper.get('[aria-label="Stored runs"]').findAll('.run-row')).toHaveLength(10)
    expect(wrapper.text()).toContain('Page 1 of 2 · 12 runs')
    expect(wrapper.text()).toContain('2m 5s')
    expect(wrapper.text()).toContain('5.0 MiB')

    await wrapper.get('.run-link').trigger('click')
    await flushPromises()
    const details = wrapper.get('[aria-label="Details for run 12"]')
    expect(configuration).toHaveBeenCalledWith('12')
    expect(details.text()).toContain('42')
    expect(details.text()).toContain('StopRunMode')
    expect(details.text()).toContain('RunCtrl')
    expect(details.text()).toContain('HV_bias')
    expect(details.find('pre').exists()).toBe(false)

    await wrapper
      .findAll('.run-pagination button')
      .find((button) => button.text() === 'Next')!
      .trigger('click')
    expect(wrapper.get('[aria-label="Stored runs"]').findAll('.run-row')).toHaveLength(2)
    expect(wrapper.text()).toContain('Page 2 of 2 · 12 runs')
  })
})
