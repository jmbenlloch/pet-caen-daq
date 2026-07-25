import { create, fromJson } from '@bufbuild/protobuf'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import RunHistoryTable from './RunHistoryTable.vue'
import { RunSummarySchema, RunType } from './gen/pet/caen/daq/v1/system_pb'
import { TimestampSchema } from '@bufbuild/protobuf/wkt'

function timestamp(value: string) {
  return fromJson(TimestampSchema, value)
}

describe('run history table', () => {
  it('presents the server-provided runs with complete readable details', async () => {
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

    expect(wrapper.get('[aria-label="Stored runs"]').findAll('.run-row')).toHaveLength(12)
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

    expect(wrapper.find('.run-pagination').exists()).toBe(false)
  })

  it('expands hexadecimal masks into decimal channel numbers', async () => {
    const wrapper = mount(RunHistoryTable, {
      props: {
        runs: [create(RunSummarySchema, { runId: '7' })],
        configuration: vi
          .fn()
          .mockResolvedValue('# Data analysis\nChEnableMask0 0x00000005\nChEnableMask1 0x80000001'),
        downloadArtifact: vi.fn(),
      },
    })

    await wrapper.get('.run-link').trigger('click')
    await flushPromises()
    const masks = wrapper.findAll('.mask-value')
    expect(masks).toHaveLength(2)
    expect(masks[0].text()).toContain('Channels: 0, 2')
    expect(masks[1].text()).toContain('Channels: 32, 63')
  })

  it('labels staircase scans and does not request a run configuration', async () => {
    const configuration = vi.fn()
    const downloadArtifact = vi.fn()
    const wrapper = mount(RunHistoryTable, {
      props: {
        runs: [
          create(RunSummarySchema, {
            runId: 'scan-1',
            runType: RunType.STAIRCASE,
            terminationReason: 'completed',
            artifacts: [{ name: 'staircase.h5', kind: 'staircase-hdf5', sizeBytes: 1024n }],
          }),
        ],
        configuration,
        downloadArtifact,
      },
    })
    expect(wrapper.get('.run-type').text()).toBe('Staircase scan')
    expect(wrapper.get('.run-row').text()).toContain('—')
    await wrapper.get('.run-link').trigger('click')
    expect(configuration).not.toHaveBeenCalled()
    expect(wrapper.get('[aria-label="Details for run scan-1"]').text()).toContain('Scans workspace')
    await wrapper.get('.artifact-download').trigger('click')
    expect(downloadArtifact).toHaveBeenCalledWith('scan-1', 'staircase.h5')
  })

  it('labels hold-delay scans separately and does not request a run configuration', async () => {
    const configuration = vi.fn()
    const wrapper = mount(RunHistoryTable, {
      props: {
        runs: [
          create(RunSummarySchema, {
            runId: 'scan-2',
            runType: RunType.HOLD_DELAY_SCAN,
            terminationReason: 'completed',
          }),
        ],
        configuration,
        downloadArtifact: vi.fn(),
      },
    })

    expect(wrapper.get('.run-type').text()).toBe('Hold-delay scan')
    expect(wrapper.get('.run-row').text()).toContain('—')
    await wrapper.get('.run-link').trigger('click')
    expect(configuration).not.toHaveBeenCalled()
    expect(wrapper.get('[aria-label="Details for run scan-2"]').text()).toContain(
      'hold-delay spectra',
    )
  })
})
