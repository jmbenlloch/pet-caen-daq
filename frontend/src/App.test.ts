import { create } from '@bufbuild/protobuf'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import type { DaqApi } from './api'
import {
  HealthStatus,
  RunSummarySchema,
  SystemState,
  TelemetrySnapshotSchema,
} from './gen/pet/caen/daq/v1/system_pb'

async function* pendingTelemetry() {
  yield* []
  await new Promise(() => undefined)
}

function dashboardApi(): DaqApi {
  return {
    snapshot: vi.fn().mockResolvedValue(
      create(TelemetrySnapshotSchema, {
        instanceId: 'backend-test',
        sequence: 7n,
        state: SystemState.READY,
        chains: [
          {
            index: 0,
            enabled: true,
            health: HealthStatus.OK,
            boards: [
              {
                node: 0,
                productId: 5202,
                fpgaFirmware: 0x800,
                health: HealthStatus.OK,
                boardTemperatureC: 24.5,
              },
            ],
          },
        ],
      }),
    ),
    configurationTemplate: vi
      .fn()
      .mockResolvedValue(
        '# Run control\nPresetTime 15 # Preset Time, Range=[1 s, 3600 s]\nEnableJobs 0 # Enable Jobs',
      ),
    telemetry: pendingTelemetry,
    validate: vi.fn().mockResolvedValue({ valid: true, issues: [] }),
    start: vi.fn().mockResolvedValue({
      snapshot: create(TelemetrySnapshotSchema, {
        instanceId: 'backend-test',
        sequence: 8n,
        state: SystemState.RUNNING,
        currentRun: { runId: 'run-55' },
      }),
    }),
    stop: vi.fn().mockResolvedValue({}),
    setHighVoltage: vi
      .fn()
      .mockResolvedValue(create(TelemetrySnapshotSchema, { state: SystemState.READY })),
    listRuns: vi.fn().mockResolvedValue([
      create(RunSummarySchema, {
        runId: 'run-54',
        eventCount: 256n,
        terminationReason: 'operator_stop',
        artifacts: [{ kind: 'decoded_events', name: 'events.jsonl', sizeBytes: 4096n }],
      }),
    ]),
    downloadArtifact: vi.fn().mockResolvedValue(new Blob()),
  }
}

describe('operator dashboard', () => {
  it('renders discovered hardware and submits validated run controls', async () => {
    const api = dashboardApi()
    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    expect(wrapper.get('#system-heading').text()).toBe('Ready')
    expect(wrapper.text()).toContain('DT5202 · node 0')
    expect(wrapper.text()).toContain('24.5 °C')
    expect(wrapper.get('#history-heading').text()).toBe('Run history')
    expect(wrapper.text()).toContain('run-54')
    expect(wrapper.text()).toContain('events.jsonl · 4.0 KiB')

    await wrapper.get('input[placeholder="run-0055"]').setValue('run-55')
    expect(wrapper.text()).toContain('PresetTime')
    expect(wrapper.text()).toContain('EnableJobs')
    expect(
      wrapper.findAll('button').some((button) => button.text().includes('Configure channels')),
    ).toBe(true)
    await wrapper.get('input[id^="PresetTime"]').setValue('30')
    await wrapper.get('input[id^="PresetTime"]').trigger('change')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()

    expect(api.validate).toHaveBeenCalledOnce()
    expect(vi.mocked(api.validate).mock.calls[0][0]).toMatch(/PresetTime\s+30/)
    expect(api.start).toHaveBeenCalledWith(
      expect.objectContaining({
        runId: 'run-55',
        requestedBy: 'operator',
        captureRaw: true,
        journalTransport: true,
      }),
    )
    expect(wrapper.get('#system-heading').text()).toBe('Running')
    wrapper.unmount()
  })
})
