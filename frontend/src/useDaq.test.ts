import { create } from '@bufbuild/protobuf'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { DaqApi } from './api'
import {
  SystemState,
  RunSummarySchema,
  TelemetrySnapshotSchema,
  type StartRunRequest,
  type StopRunRequest,
} from './gen/pet/caen/daq/v1/system_pb'
import { useDaq } from './useDaq'

function deferredStream() {
  return async function* () {
    yield* []
    await new Promise(() => undefined)
  }
}

function mountStore(api: DaqApi) {
  let store!: ReturnType<typeof useDaq>
  const wrapper = mount(
    defineComponent({
      setup() {
        store = useDaq(api)
        return () => null
      },
    }),
  )
  return { store, wrapper }
}

function fakeApi(overrides: Partial<DaqApi> = {}): DaqApi {
  return {
    snapshot: vi.fn().mockResolvedValue(
      create(TelemetrySnapshotSchema, {
        instanceId: 'backend-a',
        sequence: 1n,
        state: SystemState.READY,
      }),
    ),
    configurationTemplate: vi.fn().mockResolvedValue('Open[0] usb:host:tdl:0:0'),
    telemetry: deferredStream(),
    validate: vi.fn().mockResolvedValue({ valid: true, issues: [] }),
    start: vi.fn().mockResolvedValue({}),
    stop: vi.fn().mockResolvedValue({}),
    setHighVoltage: vi
      .fn()
      .mockResolvedValue(create(TelemetrySnapshotSchema, { state: SystemState.READY })),
    listRuns: vi.fn().mockResolvedValue([]),
    downloadArtifact: vi.fn().mockResolvedValue(new Blob()),
    ...overrides,
  }
}

afterEach(() => {
  vi.useRealTimers()
})

describe('useDaq', () => {
  it('accepts the initial complete snapshot and marks it stale after the deadline', async () => {
    vi.useFakeTimers()
    const { store, wrapper } = mountStore(fakeApi())

    void store.connect()
    await vi.waitFor(() => expect(store.snapshot.value?.instanceId).toBe('backend-a'))
    expect(store.connected.value).toBe(true)
    expect(store.stale.value).toBe(false)

    await vi.advanceTimersByTimeAsync(5_001)
    expect(store.stale.value).toBe(true)
    wrapper.unmount()
  })

  it('does not start a run when configuration validation fails', async () => {
    const start = vi.fn<(request: StartRunRequest) => Promise<Record<string, never>>>()
    const api = fakeApi({
      validate: vi.fn().mockResolvedValue({ valid: false, issues: [] }),
      start,
    })
    const { store, wrapper } = mountStore(api)

    await store.startRun({
      runId: 'run-55',
      requestedBy: 'operator',
      configuration: 'invalid',
      captureRaw: true,
      journalTransport: true,
    })

    expect(api.validate).toHaveBeenCalledWith('invalid')
    expect(start).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('stops the exact active run identity', async () => {
    const completed = create(RunSummarySchema, {
      runId: 'run-55',
      terminationReason: 'operator stop',
      eventCount: 42n,
    })
    const stop = vi.fn<(request: StopRunRequest) => Promise<{ run: typeof completed }>>()
    const api = fakeApi({
      snapshot: vi.fn().mockResolvedValue(
        create(TelemetrySnapshotSchema, {
          state: SystemState.RUNNING,
          currentRun: { runId: 'run-55' },
        }),
      ),
      stop: stop.mockResolvedValue({ run: completed }),
    })
    const { store, wrapper } = mountStore(api)
    void store.connect()
    await vi.waitFor(() => expect(store.snapshot.value?.currentRun?.runId).toBe('run-55'))

    await store.stopRun('operator')

    expect(stop).toHaveBeenCalledWith(
      expect.objectContaining({ runId: 'run-55', requestedBy: 'operator' }),
    )
    expect(store.latestCompletedRun.value).toEqual(completed)
    wrapper.unmount()
  })
})
