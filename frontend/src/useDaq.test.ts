import { create } from '@bufbuild/protobuf'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { DaqApi } from './api'
import {
  AppliedConfigurationSource,
  HistogramKind,
  SystemState,
  RunSummarySchema,
  SearchRunsRequestSchema,
  TelemetrySnapshotSchema,
  type StartRunRequest,
  type StopRunRequest,
  type TelemetrySnapshot,
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
    connectHardware: vi
      .fn()
      .mockResolvedValue(create(TelemetrySnapshotSchema, { state: SystemState.READY })),
    disconnectHardware: vi
      .fn()
      .mockResolvedValue(create(TelemetrySnapshotSchema, { state: SystemState.DISCONNECTED })),
    discoverHardware: vi
      .fn()
      .mockResolvedValue(create(TelemetrySnapshotSchema, { state: SystemState.DISCONNECTED })),
    validate: vi.fn().mockResolvedValue({ valid: true, issues: [] }),
    applyConfiguration: vi.fn().mockResolvedValue({}),
    start: vi.fn().mockResolvedValue({}),
    stop: vi.fn().mockResolvedValue({}),
    setHighVoltage: vi
      .fn()
      .mockResolvedValue(create(TelemetrySnapshotSchema, { state: SystemState.READY })),
    listRuns: vi.fn().mockResolvedValue({ runs: [], nextPageToken: '' }),
    searchRuns: vi.fn().mockResolvedValue({ runs: [], nextPageToken: '' }),
    runConfiguration: vi.fn().mockResolvedValue(''),
    downloadArtifact: vi.fn().mockResolvedValue(new Blob()),
    histograms: vi.fn().mockResolvedValue([]),
    startStaircase: vi.fn().mockResolvedValue(undefined),
    startHoldDelay: vi.fn().mockResolvedValue(undefined),
    cancelScan: vi.fn().mockResolvedValue(undefined),
    listScans: vi.fn().mockResolvedValue({ scans: [], totalCount: 0 }),
    staircase: vi.fn().mockResolvedValue(undefined),
    holdDelay: vi.fn().mockResolvedValue(undefined),
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

  it('applies a validated configuration and tracks later editor changes', async () => {
    const applied = create(TelemetrySnapshotSchema, {
      state: SystemState.READY,
      appliedConfiguration: {
        appliedAt: { seconds: 1_800_000_000n, nanos: 5 },
        requestedBy: 'operator',
        source: AppliedConfigurationSource.APPLY,
        boardCount: 4,
      },
    })
    const applyConfiguration = vi.fn().mockResolvedValue({
      appliedConfiguration: applied.appliedConfiguration,
      snapshot: applied,
    })
    const api = fakeApi({ applyConfiguration })
    const { store, wrapper } = mountStore(api)

    expect(store.configurationStatus('HG_ShapingTime 50 ns')).toBe('none')
    expect(await store.applyConfiguration('HG_ShapingTime 50 ns')).toBe(true)

    expect(api.validate).toHaveBeenCalledWith('HG_ShapingTime 50 ns')
    expect(applyConfiguration).toHaveBeenCalledWith(
      'operator',
      'HG_ShapingTime 50 ns',
      expect.stringMatching(/^[0-9a-f]{32}$/),
    )
    expect(store.appliedConfiguration.value?.source).toBe(AppliedConfigurationSource.APPLY)
    expect(store.configurationStatus('HG_ShapingTime 50 ns')).toBe('current')
    expect(store.configurationStatus('HG_ShapingTime 75 ns')).toBe('changed')
    expect(store.busy.value).toBe(false)
    expect(store.applyingConfiguration.value).toBe(false)
    wrapper.unmount()
  })

  it('cannot compare the editor with a configuration applied outside this page', async () => {
    const api = fakeApi({
      snapshot: vi.fn().mockResolvedValue(
        create(TelemetrySnapshotSchema, {
          state: SystemState.READY,
          appliedConfiguration: {
            appliedAt: { seconds: 1_800_000_000n, nanos: 0 },
            requestedBy: 'other-session',
            source: AppliedConfigurationSource.RUN,
            runId: '17',
          },
        }),
      ),
    })
    const { store, wrapper } = mountStore(api)
    void store.connect()
    await vi.waitFor(() => expect(store.appliedConfiguration.value?.runId).toBe('17'))

    expect(store.configurationStatus('anything')).toBe('unknown')
    wrapper.unmount()
  })

  it('does not apply an invalid configuration and reports apply failures', async () => {
    const applyConfiguration = vi.fn().mockRejectedValue(new Error('[SCAN_ACTIVE] scan running'))
    const invalid = fakeApi({
      validate: vi.fn().mockResolvedValue({ valid: false, issues: [] }),
      applyConfiguration,
    })
    const first = mountStore(invalid)
    expect(await first.store.applyConfiguration('invalid')).toBe(false)
    expect(applyConfiguration).not.toHaveBeenCalled()
    first.wrapper.unmount()

    const second = mountStore(fakeApi({ applyConfiguration }))
    expect(await second.store.applyConfiguration('Open[0] usb:host:tdl:0:0')).toBe(false)
    expect(second.store.error.value).toBe('[SCAN_ACTIVE] scan running')
    expect(second.store.busy.value).toBe(false)
    second.wrapper.unmount()
  })

  describe.each(['apply', 'run'] as const)('%s request correlation', (action) => {
    it.each(['own', 'another page', 'older client'] as const)(
      'only adopts its own write after a failure: %s record',
      async (record) => {
        const afterWrite = create(TelemetrySnapshotSchema, {
          state: SystemState.READY,
          appliedConfiguration: {
            appliedAt: { seconds: 1_800_000_100n, nanos: 0 },
            requestedBy: 'operator', // All real browser sessions share this identity.
            source:
              action === 'apply'
                ? AppliedConfigurationSource.APPLY
                : AppliedConfigurationSource.RUN,
          },
        })
        const snapshot = vi
          .fn()
          .mockResolvedValueOnce(create(TelemetrySnapshotSchema, { state: SystemState.READY }))
          .mockResolvedValue(afterWrite)
        const reject = (requestId: string) => {
          expect(requestId).toMatch(/^[0-9a-f]{32}$/)
          afterWrite.appliedConfiguration!.configurationRequestId =
            record === 'own' ? requestId : record === 'older client' ? '' : 'another-request'
          return Promise.reject(
            new Error(
              record === 'own'
                ? '[CONFIGURATION_AUDIT_FAILED] audit rejected after write'
                : '[CONCURRENT_OPERATION] another page is applying different settings',
            ),
          )
        }
        const api = fakeApi({
          snapshot,
          applyConfiguration: vi.fn((_actor, _text, requestId) => reject(requestId)),
          start: vi.fn((request) => reject(request.configurationRequestId)),
        })
        const { store, wrapper } = mountStore(api)
        try {
          void store.connect()
          await vi.waitFor(() => expect(store.snapshot.value?.state).toBe(SystemState.READY))
          const configuration = 'HG_ShapingTime 50 ns'
          if (action === 'apply') {
            expect(await store.applyConfiguration(configuration)).toBe(false)
          } else {
            await store.startRun({
              configuration,
              captureRaw: false,
              journalTransport: false,
              persistHistograms: false,
              hdf5SegmentSizeMb: 500,
              hdf5Compression: 'none',
            })
          }
          expect(store.error.value).toContain(
            record === 'own' ? 'CONFIGURATION_AUDIT_FAILED' : 'CONCURRENT_OPERATION',
          )
          expect(store.configurationStatus(configuration)).toBe(
            record === 'own' ? 'current' : 'unknown',
          )
          expect(store.configurationStatus('HG_ShapingTime 75 ns')).toBe(
            record === 'own' ? 'changed' : 'unknown',
          )
        } finally {
          wrapper.unmount()
        }
      },
    )
  })

  it('uses a fresh correlation ID for every apply', async () => {
    const api = fakeApi()
    const { store, wrapper } = mountStore(api)
    try {
      await store.applyConfiguration('HG_ShapingTime 50 ns')
      await store.applyConfiguration('HG_ShapingTime 50 ns')
      const calls = vi.mocked(api.applyConfiguration).mock.calls
      expect(calls[0]![2]).toMatch(/^[0-9a-f]{32}$/)
      expect(calls[1]![2]).not.toBe(calls[0]![2])
    } finally {
      wrapper.unmount()
    }
  })

  it('does not start a run when configuration validation fails', async () => {
    const start = vi.fn<(request: StartRunRequest) => Promise<Record<string, never>>>()
    const api = fakeApi({
      validate: vi.fn().mockResolvedValue({ valid: false, issues: [] }),
      start,
    })
    const { store, wrapper } = mountStore(api)

    await store.startRun({
      configuration: 'invalid',
      captureRaw: true,
      journalTransport: true,
      persistHistograms: true,
      hdf5SegmentSizeMb: 500,
      hdf5Compression: 'blosc-lz4-level4-bitshuffle',
    })

    expect(api.validate).toHaveBeenCalledWith('invalid')
    expect(start).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps an explicit starting state while the hardware start request is pending', async () => {
    let finishStart!: (value: { snapshot: TelemetrySnapshot }) => void
    const start = vi.fn(
      () =>
        new Promise<{ snapshot: TelemetrySnapshot }>((resolve) => {
          finishStart = resolve
        }),
    )
    const { store, wrapper } = mountStore(fakeApi({ start }))

    const pending = store.startRun({
      configuration: 'Open[0] usb:host:tdl:0:0',
      captureRaw: false,
      journalTransport: false,
      persistHistograms: true,
      hdf5SegmentSizeMb: 500,
      hdf5Compression: 'blosc-lz4-level4-bitshuffle',
    })
    await vi.waitFor(() => expect(start).toHaveBeenCalled())
    expect(store.startingRun.value).toBe(true)

    finishStart({
      snapshot: create(TelemetrySnapshotSchema, {
        state: SystemState.RUNNING,
        currentRun: { runId: 'run-55' },
      }),
    })
    await pending
    expect(store.startingRun.value).toBe(false)
    wrapper.unmount()
  })

  it('keeps a command error visible while telemetry continues', async () => {
    let publishTelemetry!: (snapshot: TelemetrySnapshot) => void
    const telemetry = vi.fn(
      () =>
        ({
          [Symbol.asyncIterator]() {
            return {
              next: () =>
                new Promise<IteratorResult<TelemetrySnapshot>>((resolve) => {
                  publishTelemetry = (snapshot) => resolve({ value: snapshot, done: false })
                }),
            }
          },
        }) as AsyncIterable<TelemetrySnapshot>,
    )
    const api = fakeApi({
      telemetry,
      start: vi
        .fn()
        .mockRejectedValue(
          new Error(
            '[failed_precondition] [CONFIGURATION_AUDIT_FAILED] line 2 DigitalProbe0: unsupported firmware',
          ),
        ),
    })
    const { store, wrapper } = mountStore(api)
    void store.connect()
    await vi.waitFor(() => expect(telemetry).toHaveBeenCalled())

    await store.startRun({
      configuration: 'Open[0] usb:host:tdl:0:0',
      captureRaw: false,
      journalTransport: false,
      persistHistograms: false,
      hdf5SegmentSizeMb: 500,
      hdf5Compression: 'none',
    })
    expect(store.error.value).toContain('CONFIGURATION_AUDIT_FAILED')

    publishTelemetry(create(TelemetrySnapshotSchema, { state: SystemState.READY }))
    await vi.waitFor(() => expect(store.snapshot.value?.state).toBe(SystemState.READY))
    expect(store.error.value).toContain('CONFIGURATION_AUDIT_FAILED')
    wrapper.unmount()
  })

  it('connects and disconnects hardware without stopping backend telemetry', async () => {
    const api = fakeApi({
      snapshot: vi
        .fn()
        .mockResolvedValue(create(TelemetrySnapshotSchema, { state: SystemState.DISCONNECTED })),
    })
    const { store, wrapper } = mountStore(api)
    void store.connect()
    await vi.waitFor(() => expect(store.canConnectHardware.value).toBe(true))

    await store.connectHardware('Open[0] usb:host:tdl:0:0')
    expect(api.connectHardware).toHaveBeenCalledWith('operator', 'Open[0] usb:host:tdl:0:0')
    expect(store.snapshot.value?.state).toBe(SystemState.READY)

    await store.disconnectHardware()
    expect(api.disconnectHardware).toHaveBeenCalledWith('operator')
    expect(store.snapshot.value?.state).toBe(SystemState.DISCONNECTED)
    expect(store.connected.value).toBe(true)
    wrapper.unmount()
  })

  it('discovers cards while leaving hardware disconnected', async () => {
    const discovered = create(TelemetrySnapshotSchema, {
      state: SystemState.DISCONNECTED,
      chains: [{ index: 0, enabled: true, boards: [{ node: 0 }, { node: 1 }] }],
    })
    const api = fakeApi({
      snapshot: vi
        .fn()
        .mockResolvedValue(create(TelemetrySnapshotSchema, { state: SystemState.DISCONNECTED })),
      discoverHardware: vi.fn().mockResolvedValue(discovered),
    })
    const { store, wrapper } = mountStore(api)
    void store.connect()
    await vi.waitFor(() => expect(store.canDiscoverHardware.value).toBe(true))

    await store.discoverHardware()

    expect(api.discoverHardware).toHaveBeenCalledWith('operator')
    expect(store.snapshot.value?.state).toBe(SystemState.DISCONNECTED)
    expect(store.snapshot.value?.chains[0].boards).toHaveLength(2)
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

    await store.stopRun()

    expect(stop).toHaveBeenCalledWith(
      expect.objectContaining({ runId: 'run-55', requestedBy: 'operator' }),
    )
    expect(store.latestCompletedRun.value).toEqual(completed)
    wrapper.unmount()
  })

  it('releases run controls without waiting for history refresh', async () => {
    let resolveHistory!: (value: { runs: []; nextPageToken: string }) => void
    const history = new Promise<{ runs: []; nextPageToken: string }>((resolve) => {
      resolveHistory = resolve
    })
    const api = fakeApi({
      snapshot: vi.fn().mockResolvedValue(
        create(TelemetrySnapshotSchema, {
          state: SystemState.RUNNING,
          currentRun: { runId: 'run-55' },
        }),
      ),
      stop: vi.fn().mockResolvedValue({
        snapshot: create(TelemetrySnapshotSchema, { state: SystemState.READY }),
      }),
      listRuns: vi
        .fn()
        .mockResolvedValueOnce({ runs: [], nextPageToken: '' })
        .mockReturnValueOnce(history),
    })
    const { store, wrapper } = mountStore(api)
    void store.connect()
    await vi.waitFor(() => expect(store.snapshot.value?.currentRun?.runId).toBe('run-55'))

    await store.stopRun()

    expect(store.busy.value).toBe(false)
    expect(store.canStart.value).toBe(true)
    expect(store.runHistoryLoading.value).toBe(true)
    resolveHistory({ runs: [], nextPageToken: '' })
    await vi.waitFor(() => expect(store.runHistoryLoading.value).toBe(false))
    wrapper.unmount()
  })

  it('does not surface an in-flight histogram failure when the run is stopped', async () => {
    let rejectHistograms!: (reason: Error) => void
    const histograms = vi.fn().mockReturnValue(
      new Promise((_, reject) => {
        rejectHistograms = reject
      }),
    )
    let resolveStop!: (value: Record<string, never>) => void
    const stop = vi.fn().mockReturnValue(
      new Promise<Record<string, never>>((resolve) => {
        resolveStop = resolve
      }),
    )
    const api = fakeApi({
      snapshot: vi.fn().mockResolvedValue(
        create(TelemetrySnapshotSchema, {
          state: SystemState.RUNNING,
          currentRun: { runId: 'run-55' },
        }),
      ),
      histograms,
      stop,
    })
    const { store, wrapper } = mountStore(api)
    void store.connect()
    await vi.waitFor(() => expect(store.snapshot.value?.currentRun?.runId).toBe('run-55'))

    const histogramRequest = store.loadHistograms('42', HistogramKind.PHA_HIGH_GAIN, [])
    const stopRequest = store.stopRun()
    rejectHistograms(new Error('[failed_precondition] requested run is not active'))
    await histogramRequest

    expect(store.error.value).toBe('')
    expect(store.histogramsLoading.value).toBe(false)
    await store.loadHistograms('42', HistogramKind.PHA_HIGH_GAIN, [])
    expect(histograms).toHaveBeenCalledTimes(1)

    resolveStop({})
    await stopRequest
    wrapper.unmount()
  })

  it('ignores stale searches and appends the requested next page', async () => {
    const newest = create(RunSummarySchema, { runId: 'newest' })
    const next = create(RunSummarySchema, { runId: 'next' })
    type SearchResponse = { runs: (typeof newest)[]; nextPageToken: string }
    let resolveFirst!: (value: SearchResponse) => void
    const first = new Promise<SearchResponse>((resolve) => {
      resolveFirst = resolve
    })
    const searchRuns = vi
      .fn()
      .mockReturnValueOnce(first)
      .mockResolvedValueOnce({ runs: [newest], nextPageToken: 'page-2' })
      .mockResolvedValueOnce({ runs: [next], nextPageToken: '' })
    const { store, wrapper } = mountStore(fakeApi({ searchRuns }))
    const request = create(SearchRunsRequestSchema, { limit: 20 })

    void store.searchRuns(request)
    await store.searchRuns(request)
    resolveFirst({ runs: [], nextPageToken: 'stale' })
    await Promise.resolve()

    expect(store.searchResults.value.map((run) => run.runId)).toEqual(['newest'])
    expect(store.searchNextPageToken.value).toBe('page-2')
    await store.searchRuns(
      create(SearchRunsRequestSchema, { limit: 20, pageToken: 'page-2' }),
      true,
    )
    expect(store.searchResults.value.map((run) => run.runId)).toEqual(['newest', 'next'])
    wrapper.unmount()
  })

  it('loads run history through server-provided continuation tokens', async () => {
    const newest = create(RunSummarySchema, { runId: 'newest' })
    const older = create(RunSummarySchema, { runId: 'older' })
    const listRuns = vi
      .fn()
      .mockResolvedValueOnce({ runs: [newest], nextPageToken: 'page-2' })
      .mockResolvedValueOnce({ runs: [older], nextPageToken: '' })
    const { store, wrapper } = mountStore(fakeApi({ listRuns }))

    await store.refreshHistory()
    expect(listRuns).toHaveBeenCalledWith(20)
    expect(store.runHistory.value.map((run) => run.runId)).toEqual(['newest'])
    expect(store.runHistoryNextPageToken.value).toBe('page-2')

    await store.loadMoreHistory()
    expect(listRuns).toHaveBeenLastCalledWith(20, 'page-2')
    expect(store.runHistory.value.map((run) => run.runId)).toEqual(['newest', 'older'])
    expect(store.runHistoryNextPageToken.value).toBe('')
    wrapper.unmount()
  })

  it('rejects an unfiltered response to a run-number search', async () => {
    const searchRuns = vi.fn().mockResolvedValue({
      runs: [create(RunSummarySchema, { runId: '32' })],
      nextPageToken: '',
    })
    const { store, wrapper } = mountStore(fakeApi({ searchRuns }))

    await store.searchRuns(create(SearchRunsRequestSchema, { runNumber: 28n }))

    expect(store.searchResults.value).toEqual([])
    expect(store.searchError.value).toContain('did not apply the run-number filter')
    wrapper.unmount()
  })
})
