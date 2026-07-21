import { create } from '@bufbuild/protobuf'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import type { DaqApi } from './api'
import { HealthStatus, SystemState, TelemetrySnapshotSchema } from './gen/pet/caen/daq/v1/system_pb'

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

    await wrapper.get('input[placeholder="run-0055"]').setValue('run-55')
    await wrapper.get('#configuration').setValue('Open TDlink 0 0')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()

    expect(api.validate).toHaveBeenCalledWith('Open TDlink 0 0')
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
