import { create } from '@bufbuild/protobuf'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import type { DaqApi } from './api'
import {
  ConfigurationLayer,
  ConfigurationStage,
  HealthStatus,
  RunType,
  RunSummarySchema,
  SystemState,
  TelemetrySnapshotSchema,
} from './gen/pet/caen/daq/v1/system_pb'

async function* pendingTelemetry() {
  yield* []
  await new Promise(() => undefined)
}

function dashboardApi(state = SystemState.READY): DaqApi {
  return {
    snapshot: vi.fn().mockResolvedValue(
      create(TelemetrySnapshotSchema, {
        instanceId: 'backend-test',
        sequence: 7n,
        state,
        statistics: {
          elapsedMilliseconds: 2000n,
          boards: [
            {
              chain: 0,
              triggerId: 12n,
              triggerCount: 10n,
              tOrCount: 20n,
              dataBytes: 2048n,
              channelTriggerCounts: Array(64).fill(3n),
              timestampCounts: Array(64).fill(2n),
              phaCounts: Array(64).fill(1n),
            },
          ],
        },
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
          {
            index: 1,
            enabled: false,
            health: HealthStatus.UNKNOWN,
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
    connectHardware: vi.fn().mockResolvedValue(create(TelemetrySnapshotSchema)),
    disconnectHardware: vi.fn().mockResolvedValue(create(TelemetrySnapshotSchema)),
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
    listRuns: vi.fn().mockResolvedValue({
      runs: [
        create(RunSummarySchema, {
          runId: 'run-54',
          eventCount: 256n,
          terminationReason: 'operator_stop',
          artifacts: [{ kind: 'decoded_events', name: 'events.jsonl', sizeBytes: 4096n }],
        }),
      ],
      nextPageToken: '',
    }),
    searchRuns: vi.fn().mockResolvedValue({ runs: [], nextPageToken: '' }),
    runConfiguration: vi.fn().mockResolvedValue('HV_Vbias 55.0\nStopRunMode MANUAL'),
    downloadArtifact: vi.fn().mockResolvedValue(new Blob()),
    histograms: vi.fn().mockResolvedValue([]),
    startStaircase: vi.fn().mockResolvedValue(undefined),
    startHoldDelay: vi.fn().mockResolvedValue(undefined),
    cancelScan: vi.fn().mockResolvedValue(undefined),
    listScans: vi.fn().mockResolvedValue({ scans: [], totalCount: 0 }),
    staircase: vi.fn().mockResolvedValue(undefined),
    holdDelay: vi.fn().mockResolvedValue(undefined),
  }
}

describe('operator dashboard', () => {
  it('shows the storage error in the persistence card', async () => {
    const api = dashboardApi()
    vi.mocked(api.snapshot).mockResolvedValue(
      create(TelemetrySnapshotSchema, {
        instanceId: 'backend-test',
        state: SystemState.FAULT,
        storage: {
          health: HealthStatus.FAULT,
          runDirectory: '/var/lib/pet-caen/runs/run-36',
          bytesWritten: 167540439n,
          lastError: 'capture raw batch: no space left on device',
        },
      }),
    )

    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    const panel = wrapper.get('[aria-labelledby="storage-heading"]')
    expect(panel.get('[role="alert"]').text()).toBe('capture raw batch: no space left on device')
    wrapper.unmount()
  })

  it('distinguishes acquisition input from persistence backlog while draining', async () => {
    const api = dashboardApi(SystemState.DRAINING)
    vi.mocked(api.snapshot).mockResolvedValue(
      create(TelemetrySnapshotSchema, {
        instanceId: 'backend-test',
        state: SystemState.DRAINING,
        pipeline: {
          queueCapacity: 32n,
          queueDepth: 4n,
          rawQueueDepth: 2n,
          eventQueueDepth: 3n,
          receivedBatches: 120n,
          receivedEvents: 6000n,
          acceptedBatches: 120n,
          rawBatchesPersisted: 118n,
          eventBatchesPersisted: 110n,
          decodedEvents: 5500n,
          persistedEvents: 5498n,
        },
      }),
    )

    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    const panel = wrapper.get('[aria-labelledby="pipeline-heading"]')
    expect(panel.text()).toContain('Draining buffered data')
    expect(panel.text()).toContain('6,000 events · 120 batches')
    expect(panel.text()).toContain('5,498 / 5,500 decoded')
    expect(panel.text()).toContain('Batch backlog10')
    expect(panel.text()).toContain('raw 2 · events 3')
    wrapper.unmount()
  })

  it('shows structured hardware configuration progress', async () => {
    const api = dashboardApi(SystemState.CONFIGURING)
    vi.mocked(api.snapshot).mockResolvedValue(
      create(TelemetrySnapshotSchema, {
        instanceId: 'backend-test',
        state: SystemState.CONFIGURING,
        configurationProgress: {
          operationId: 'configuration-2',
          stage: ConfigurationStage.READING_REGISTERS,
          active: true,
          board: 1,
          chain: 1,
          node: 0,
          boardsCompleted: 1,
          boardsTotal: 4,
          completed: 150,
          total: 627,
          unit: 'registers',
          message: 'reading back and validating registers',
        },
      }),
    )

    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    const panel = wrapper.get('.configuration-progress-panel')
    expect(panel.text()).toContain('Register readback')
    expect(panel.text()).toContain('Board 2 of 4')
    expect(panel.text()).toContain('150 / 627 registers')
    expect(panel.text()).toContain('Chain 1, node 0')
    expect(panel.get('[aria-label="Register readback progress"]').attributes('value')).toBe('150')
    wrapper.unmount()
  })

  it('uses the native Save As picker when the browser supports it', async () => {
    const write = vi.fn().mockResolvedValue(undefined)
    const close = vi.fn().mockResolvedValue(undefined)
    const showSaveFilePicker = vi.fn().mockResolvedValue({
      createWritable: vi.fn().mockResolvedValue({ write, close }),
    })
    Object.defineProperty(window, 'showSaveFilePicker', {
      configurable: true,
      value: showSaveFilePicker,
    })
    const prompt = vi.spyOn(window, 'prompt')
    const wrapper = mount(App, { props: { api: dashboardApi() } })
    await flushPromises()

    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save config')!
      .trigger('click')
    await flushPromises()

    expect(showSaveFilePicker).toHaveBeenCalledWith({
      suggestedName: 'pet-caen-daq-configuration.txt',
      types: [{ description: 'Text file', accept: { 'text/plain': ['.txt'] } }],
    })
    expect(write).toHaveBeenCalledOnce()
    expect(await (write.mock.calls[0][0] as Blob).text()).toContain('Open[0]')
    expect(close).toHaveBeenCalledOnce()
    expect(prompt).not.toHaveBeenCalled()
    delete (window as Window & { showSaveFilePicker?: unknown }).showSaveFilePicker
    wrapper.unmount()
  })

  it('downloads the complete current configuration as a text file', async () => {
    const prompt = vi.spyOn(window, 'prompt').mockReturnValue('my-run-config')
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:configuration')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined)
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    const wrapper = mount(App, { props: { api: dashboardApi() } })
    await flushPromises()

    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save config')!
      .trigger('click')

    expect(wrapper.text()).not.toContain('Use backend config')
    expect(prompt).toHaveBeenCalledWith('Save configuration as', 'pet-caen-daq-configuration.txt')
    expect(createObjectURL).toHaveBeenCalledOnce()
    const blob = createObjectURL.mock.calls[0][0] as Blob
    expect(blob.type).toBe('text/plain;charset=utf-8')
    expect(await blob.text()).toContain('Open[0]')
    expect(click).toHaveBeenCalledOnce()
    expect((click.mock.instances[0] as HTMLAnchorElement).download).toBe('my-run-config.txt')
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:configuration')
    wrapper.unmount()
  })

  it('shows one hardware action matching the current connection state', async () => {
    const connectedApi = dashboardApi(SystemState.READY)
    const connected = mount(App, { props: { api: connectedApi } })
    await flushPromises()

    const disconnect = connected.get('.hardware-connection-action')
    expect(connected.get('.connection').text()).toContain('Backend online')
    expect(connected.get('.hardware-state').text()).toBe('Hardware connected')
    expect(connected.get('.hardware-status-dot').classes()).toContain('live')
    expect(connected.get('.connection').text()).not.toContain('backend-test')
    expect(connected.findAll('.hardware-connection-action')).toHaveLength(1)
    expect(disconnect.text()).toBe('Disconnect hardware')
    expect(disconnect.classes()).toContain('disconnect')
    await disconnect.trigger('click')
    expect(connectedApi.disconnectHardware).toHaveBeenCalledWith('operator')
    expect(connectedApi.connectHardware).not.toHaveBeenCalled()
    connected.unmount()

    const disconnectedApi = dashboardApi(SystemState.DISCONNECTED)
    const disconnected = mount(App, { props: { api: disconnectedApi } })
    await flushPromises()

    const connect = disconnected.get('.hardware-connection-action')
    expect(disconnected.get('.connection').text()).toContain('Backend online')
    expect(disconnected.get('.hardware-state').text()).toBe('Hardware disconnected')
    expect(disconnected.get('.hardware-status-dot').classes()).toContain('disconnected')
    expect(disconnected.findAll('.hardware-connection-action')).toHaveLength(1)
    expect(connect.text()).toBe('Connect hardware')
    expect(connect.classes()).toContain('connect')
    await connect.trigger('click')
    expect(disconnectedApi.connectHardware).toHaveBeenCalledWith('operator')
    expect(disconnectedApi.disconnectHardware).not.toHaveBeenCalled()
    disconnected.unmount()
  })

  it('shows only the run action relevant to the current state', async () => {
    const api = dashboardApi()
    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    expect(wrapper.findAll('button').some((button) => button.text() === 'Start run')).toBe(true)
    expect(wrapper.findAll('button').some((button) => button.text() === 'Stop and drain')).toBe(
      false,
    )

    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Start run')!
      .trigger('click')
    await flushPromises()

    expect(wrapper.findAll('button').some((button) => button.text() === 'Start run')).toBe(false)
    expect(wrapper.findAll('button').some((button) => button.text() === 'Stop and drain')).toBe(
      true,
    )
    wrapper.unmount()
  })

  it('reports an active scan instead of saying there is no active run', async () => {
    const wrapper = mount(App, { props: { api: dashboardApi(SystemState.SCANNING) } })
    await flushPromises()

    const status = wrapper.get('.run-now')
    expect(status.text()).toContain('Scan in progress')
    expect(status.text()).toContain('Acquisition run controls are unavailable during the scan')
    expect(status.text()).not.toContain('No active run')
    expect(status.text()).not.toContain('Automatic stop after')
    wrapper.unmount()
  })

  it('offers an immediate retry while automatic backend retries continue', async () => {
    const api = dashboardApi()
    vi.mocked(api.snapshot).mockRejectedValue(new Error('backend unavailable'))
    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    expect(wrapper.get('.backend-state').text()).toBe('Backend offline')
    const retry = wrapper.get('.backend-retry-action')
    expect(retry.text()).toBe('Retry backend')
    await retry.trigger('click')
    await flushPromises()
    expect(api.snapshot).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('separates operator tasks into keyboard-accessible workspace tabs', async () => {
    const wrapper = mount(App, { attachTo: document.body, props: { api: dashboardApi() } })
    await flushPromises()

    const acquisition = wrapper.get('#workspace-tab-acquisition')
    const statistics = wrapper.get('#workspace-tab-statistics')
    expect(acquisition.attributes('aria-selected')).toBe('true')
    expect(wrapper.get('#workspace-panel-acquisition').isVisible()).toBe(true)
    expect(wrapper.get('#workspace-panel-statistics').isVisible()).toBe(false)
    expect(wrapper.get('#workspace-panel-plots').isVisible()).toBe(false)

    await acquisition.trigger('keydown', { key: 'ArrowRight' })
    await new Promise((resolve) => requestAnimationFrame(resolve))
    expect(statistics.attributes('aria-selected')).toBe('true')
    expect((wrapper.get('#workspace-panel-acquisition').element as HTMLElement).style.display).toBe(
      'none',
    )
    expect(
      (wrapper.get('#workspace-panel-statistics').element as HTMLElement).style.display,
    ).not.toBe('none')
    expect(document.activeElement).toBe(statistics.element)

    await statistics.trigger('keydown', { key: 'End' })
    await new Promise((resolve) => requestAnimationFrame(resolve))
    expect(wrapper.get('#workspace-tab-runs').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('#workspace-panel-runs').isVisible()).toBe(true)
    wrapper.unmount()
  })

  it('keeps the raw configuration hidden until explicitly requested', async () => {
    const wrapper = mount(App, { props: { api: dashboardApi() } })
    await flushPromises()

    expect(wrapper.find('[aria-label="JANUS configuration source"]').exists()).toBe(false)
    expect(wrapper.get('[aria-label="Configuration parameters"]').isVisible()).toBe(true)

    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'View raw configuration')!
      .trigger('click')

    expect(wrapper.get('[aria-label="JANUS configuration source"]').isVisible()).toBe(true)
    expect(wrapper.find('[aria-label="Configuration parameters"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps optional evidence capture disabled by default', async () => {
    const wrapper = mount(App, { props: { api: dashboardApi() } })
    await flushPromises()

    await wrapper
      .findAll('.section-tabs [role="tab"]')
      .find((tab) => tab.text() === 'RunCtrl')!
      .trigger('click')
    const options = [wrapper.get('#capture-raw'), wrapper.get('#journal-transport')]
    expect(options).toHaveLength(2)
    expect(options.every((option) => !(option.element as HTMLInputElement).checked)).toBe(true)
    expect(wrapper.get('input[aria-label="HDF5 file size in MiB"]').isVisible()).toBe(true)
    expect(wrapper.get<HTMLSelectElement>('#hdf5-compression').element.value).toBe(
      'blosc-lz4-level4-bitshuffle',
    )
    wrapper.unmount()
  })

  it('opens configuration on Connect and only offers search from All parameters', async () => {
    const wrapper = mount(App, { props: { api: dashboardApi() } })
    await flushPromises()

    expect(wrapper.get('.section-tabs [role="tab"][aria-selected="true"]').text()).toBe('Connect')
    expect(wrapper.find('.parameter-toolbar input[type="search"]').exists()).toBe(false)
    const connectionFields = wrapper.findAll('.parameter-list input[id^="Open["]')
    expect(connectionFields).toHaveLength(4)
    expect(connectionFields.map((field) => (field.element as HTMLInputElement).value)).toEqual([
      'usb:172.16.0.11:tdl:0:0',
      'usb:172.16.0.11:tdl:1:0',
      'usb:172.16.0.11:tdl:2:0',
      'usb:172.16.0.11:tdl:3:0',
    ])
    expect(wrapper.text()).not.toContain('No parameters match this filter.')

    await wrapper
      .findAll('.section-tabs [role="tab"]')
      .find((tab) => tab.text() === 'All')!
      .trigger('click')
    expect(wrapper.get('.parameter-toolbar input[type="search"]').isVisible()).toBe(true)
    wrapper.unmount()
  })

  it('closes every configuration dialog with Escape', async () => {
    const wrapper = mount(App, { props: { api: dashboardApi() } })
    await flushPromises()

    await wrapper
      .findAll('.section-tabs [role="tab"]')
      .find((tab) => tab.text() === 'HV_bias')!
      .trigger('click')
    await wrapper.get('.board-overrides-button').trigger('click')
    expect(wrapper.get('.board-dialog').isVisible()).toBe(true)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.board-dialog').exists()).toBe(false)

    await wrapper.get('.channel-overrides-button').trigger('click')
    expect(wrapper.get('.channel-dialog').isVisible()).toBe(true)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.channel-dialog').exists()).toBe(false)

    await wrapper
      .findAll('.section-tabs [role="tab"]')
      .find((tab) => tab.text() === 'All')!
      .trigger('click')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Configure channels')!
      .trigger('click')
    expect(wrapper.get('.mask-dialog').isVisible()).toBe(true)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.mask-dialog').exists()).toBe(false)
    wrapper.unmount()
  })

  it('switches and persists the operator color theme', async () => {
    localStorage.removeItem('pet-caen-theme')
    const wrapper = mount(App, { props: { api: dashboardApi() } })
    await flushPromises()
    expect(document.documentElement.dataset.theme).toBe('dark')
    await wrapper.get('[aria-label="Switch to light theme"]').trigger('click')
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(localStorage.getItem('pet-caen-theme')).toBe('light')
    await wrapper.get('[aria-label="Switch to dark theme"]').trigger('click')
    expect(document.documentElement.dataset.theme).toBe('dark')
    wrapper.unmount()
  })

  it('renders discovered hardware and submits validated run controls', async () => {
    const api = dashboardApi()
    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    expect(wrapper.get('#system-heading').text()).toBe('Ready')
    expect(wrapper.text()).toContain('1 enabled link')
    expect(wrapper.text()).not.toContain('2 enabled links')
    expect(wrapper.text()).toContain('DT5202 · node 0')
    expect(wrapper.text()).toContain('24.5 °C')
    expect(wrapper.get('#history-heading').text()).toBe('Run history')
    expect(wrapper.get('#statistics-heading').text()).toBe('Statistics')
    expect(wrapper.text()).toContain('Trigger ID')
    expect(wrapper.text()).toContain('run-54')
    expect(wrapper.get('[aria-label="Stored runs"]').text()).toContain('Data size')
    await wrapper.get('.run-link').trigger('click')
    await flushPromises()
    expect(wrapper.get('[aria-label="Details for run run-54"]').text()).toContain('events.jsonl')
    expect(wrapper.get('[aria-label="Details for run run-54"]').text()).toContain('4.0 KiB')

    expect(wrapper.text()).not.toContain('Requested by')
    expect(wrapper.text()).not.toContain('Run ID')
    await wrapper
      .findAll('.section-tabs [role="tab"]')
      .find((tab) => tab.text() === 'All')!
      .trigger('click')
    expect(wrapper.text()).toContain('PresetTime')
    expect(wrapper.text()).not.toContain('EnableJobs')
    expect(
      wrapper.findAll('button').some((button) => button.text().includes('Configure channels')),
    ).toBe(true)
    await wrapper.get('input[id^="PresetTime"]').setValue('30')
    await wrapper.get('input[id^="PresetTime"]').trigger('change')
    await wrapper
      .findAll('.section-tabs [role="tab"]')
      .find((tab) => tab.text() === 'RunCtrl')!
      .trigger('click')
    const segmentSize = wrapper.get('input[aria-label="HDF5 file size in MiB"]')
    expect(segmentSize.element).toHaveProperty('value', '500')
    await segmentSize.setValue('128')
    await wrapper.get('#hdf5-compression').setValue('none')
    expect(wrapper.get<HTMLInputElement>('#persist-histograms').element.checked).toBe(true)
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()

    expect(api.validate).toHaveBeenCalledOnce()
    expect(vi.mocked(api.validate).mock.calls[0][0]).toMatch(/PresetTime\s+30/)
    expect(api.start).toHaveBeenCalledWith(
      expect.objectContaining({
        requestedBy: 'operator',
        captureRaw: false,
        journalTransport: false,
        persistHistograms: true,
        hdf5SegmentSizeMb: 128,
        hdf5Compression: 'none',
      }),
    )
    expect(wrapper.get('#system-heading').text()).toBe('Running')
    wrapper.unmount()
  })

  it('commits a preset count before the numeric control loses focus', async () => {
    const api = dashboardApi()
    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    await wrapper
      .findAll('.section-tabs [role="tab"]')
      .find((tab) => tab.text() === 'RunCtrl')!
      .trigger('click')
    await wrapper.get('select[id^="StopRunMode"]').setValue('PRESET_COUNTS')
    const presetCounts = wrapper.get('input[id^="PresetCounts"]')
    const presetCountsInput = presetCounts.element as HTMLInputElement
    presetCountsInput.value = '3'
    await presetCounts.trigger('input')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()

    expect(api.validate).toHaveBeenCalledOnce()
    expect(vi.mocked(api.validate).mock.calls[0][0]).toMatch(/StopRunMode\s+PRESET_COUNTS/)
    expect(vi.mocked(api.validate).mock.calls[0][0]).toMatch(/PresetCounts\s+3/)
    wrapper.unmount()
  })

  it('searches run configuration with typed scoped predicates and clears results', async () => {
    const api = dashboardApi()
    vi.mocked(api.searchRuns).mockResolvedValue({
      runs: [create(RunSummarySchema, { runId: '40', eventCount: 120n })],
      nextPageToken: '',
      $typeName: 'pet.caen.daq.v1.SearchRunsResponse',
    })
    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    const searchToggle = wrapper.get('[aria-controls="run-search-form"]')
    expect(searchToggle.attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('[aria-label="Search stored runs"]').exists()).toBe(false)
    await searchToggle.trigger('click')
    expect(searchToggle.attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('button.search-filter-button.secondary').text()).toBe('Add filter')
    await wrapper.get('button.search-filter-button.secondary').trigger('click')
    expect(wrapper.get('button.remove-filter.danger').text()).toBe('Remove')
    await wrapper.findAll('button.remove-filter').at(-1)!.trigger('click')
    await wrapper.get('[aria-label="Parameter 1"]').trigger('click')
    const parameterOptions = wrapper.get('[aria-label="Parameters 1"]')
    expect(parameterOptions.text()).toContain('TestPulsePreamp')
    expect(parameterOptions.findAll('[role="option"]').length).toBeGreaterThan(50)
    await parameterOptions
      .findAll('[role="option"]')
      .find((option) => option.text().includes('TD_CoarseThreshold'))!
      .trigger('click')
    expect(wrapper.get('[aria-label="Parameter 1"]').attributes('aria-expanded')).toBe('false')
    await wrapper.get('[aria-label="Board 1"]').setValue('2')
    await wrapper.get('[aria-label="Match 1"]').setValue('range')
    await wrapper.get('[aria-label="Value 1"]').setValue('200')
    await wrapper.get('[aria-label="Maximum 1"]').setValue('240')
    await wrapper.get('[aria-label="Run number"]').setValue('40')
    await wrapper.get('[aria-label="Maximum run number"]').setValue('42')
    await wrapper.get('form[aria-label="Search stored runs"]').trigger('submit')
    await flushPromises()

    expect(wrapper.find('.field-error').exists() ? wrapper.find('.field-error').text() : '').toBe(
      '',
    )
    expect(api.searchRuns).toHaveBeenCalledWith(
      expect.objectContaining({
        configuration: [
          expect.objectContaining({
            parameter: 'TD_CoarseThreshold',
            layer: ConfigurationLayer.RESOLVED,
            scope: expect.objectContaining({
              scope: expect.objectContaining({
                case: 'board',
                value: 2,
              }),
            }),
            comparison: expect.objectContaining({
              case: 'integer',
              value: expect.objectContaining({ minimum: 200n, maximum: 240n }),
            }),
          }),
        ],
        runNumber: 40n,
        maximumRunNumber: 42n,
      }),
    )
    expect(wrapper.get('[aria-label="Search results"]').text()).toContain('40')
    expect(wrapper.find('[aria-label="Stored runs"]').exists()).toBe(false)
    await wrapper.get('[aria-label="Search results"]').get('.run-link').trigger('click')
    await flushPromises()
    expect(api.runConfiguration).toHaveBeenCalledWith('40')
    const details = wrapper.get('[aria-label="Details for run 40"]')
    expect(details.text()).toContain('HV_Vbias')
    expect(details.text()).toContain('55.0')
    expect(details.text()).toContain('Download configuration (.txt)')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Clear')!
      .trigger('click')
    expect(wrapper.find('[aria-label="Search results"]').exists()).toBe(false)
    expect(wrapper.find('[aria-label="Stored runs"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('searches all, data, staircase, or hold-delay run types', async () => {
    const api = dashboardApi()
    const wrapper = mount(App, { props: { api } })
    await flushPromises()
    await wrapper.get('[aria-controls="run-search-form"]').trigger('click')
    const type = wrapper.get('[aria-label="Run type"]')
    expect(type.findAll('option').map((option) => option.text())).toEqual([
      'All types',
      'Data runs',
      'Staircase scans',
      'Hold-delay scans',
    ])
    await type.setValue(String(RunType.STAIRCASE))
    await wrapper.get('form[aria-label="Search stored runs"]').trigger('submit')
    await flushPromises()
    expect(api.searchRuns).toHaveBeenCalledWith(
      expect.objectContaining({ runType: RunType.STAIRCASE }),
    )
  })

  it('derives enum choices and hides implementation-only search fields', async () => {
    const wrapper = mount(App, { props: { api: dashboardApi() } })
    await flushPromises()

    await wrapper.get('[aria-controls="run-search-form"]').trigger('click')
    expect(wrapper.find('[aria-label="Layer 1"]').exists()).toBe(false)
    expect(wrapper.find('[aria-label="Type 1"]').exists()).toBe(false)
    await wrapper.get('[aria-label="Parameter 1"]').setValue('StopRunMode')
    await wrapper.get('[aria-label="Parameter 1"]').trigger('change')

    const value = wrapper.get('[aria-label="Value 1"]')
    expect(value.element.tagName).toBe('SELECT')
    expect(value.findAll('option').map((option) => option.text())).toContain('PRESET_COUNTS')
    wrapper.unmount()
  })

  it('searches channel parameters across all boards and channels by default', async () => {
    const api = dashboardApi()
    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    await wrapper.get('[aria-controls="run-search-form"]').trigger('click')
    await wrapper.get('[aria-label="Parameter 1"]').setValue('HG_Gain')
    await wrapper.get('[aria-label="Parameter 1"]').trigger('change')
    expect((wrapper.get('[aria-label="Board 1"]').element as HTMLSelectElement).value).toBe('')
    expect((wrapper.get('[aria-label="Channel 1"]').element as HTMLSelectElement).value).toBe('')
    expect(wrapper.get('[aria-label="Channel 1"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[aria-label="Match 1"]').setValue('range')
    await wrapper.get('[aria-label="Value 1"]').setValue('1')
    await wrapper.get('[aria-label="Maximum 1"]').setValue('63')
    await wrapper.get('form[aria-label="Search stored runs"]').trigger('submit')
    await flushPromises()

    expect(api.searchRuns).toHaveBeenCalledWith(
      expect.objectContaining({
        configuration: [
          expect.objectContaining({
            parameter: 'HG_Gain',
            layer: ConfigurationLayer.RESOLVED,
          }),
        ],
      }),
    )
    expect(vi.mocked(api.searchRuns).mock.calls[0][0].configuration[0].scope).toBeUndefined()
    wrapper.unmount()
  })

  it('shows the allowed range for numeric configuration parameters', async () => {
    const wrapper = mount(App, { props: { api: dashboardApi() } })
    await flushPromises()

    await wrapper.get('[aria-controls="run-search-form"]').trigger('click')
    await wrapper.get('[aria-label="Parameter 1"]').setValue('ZS_Threshold_HG')
    await wrapper.get('[aria-label="Parameter 1"]').trigger('change')

    expect(wrapper.get('.search-range-hint').text()).toBe('Allowed: 0–65535')
    expect(wrapper.get('[aria-label="Value 1"]').attributes('min')).toBe('0')
    expect(wrapper.get('[aria-label="Value 1"]').attributes('max')).toBe('65535')
    wrapper.unmount()
  })

  it('clearly distinguishes exact numeric searches from ranges', async () => {
    const api = dashboardApi()
    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    await wrapper.get('[aria-controls="run-search-form"]').trigger('click')
    await wrapper.get('[aria-label="Parameter 1"]').setValue('HV_Vbias')
    await wrapper.get('[aria-label="Parameter 1"]').trigger('change')
    expect((wrapper.get('[aria-label="Match 1"]').element as HTMLSelectElement).value).toBe('exact')
    expect(wrapper.find('[aria-label="Maximum 1"]').exists()).toBe(false)
    await wrapper.get('[aria-label="Value 1"]').setValue('20')
    await wrapper.get('form[aria-label="Search stored runs"]').trigger('submit')
    await flushPromises()

    const predicate = vi.mocked(api.searchRuns).mock.calls[0][0].configuration[0]
    expect(predicate.comparison).toEqual(
      expect.objectContaining({
        case: 'real',
        value: expect.objectContaining({ equal: 20 }),
      }),
    )
    if (predicate.comparison.case === 'real') {
      expect(predicate.comparison.value.minimum).toBeUndefined()
      expect(predicate.comparison.value.maximum).toBeUndefined()
    }

    await wrapper.get('[aria-label="Match 1"]').setValue('range')
    expect(wrapper.find('[aria-label="Maximum 1"]').exists()).toBe(true)
    expect(wrapper.get('[aria-label="Value 1"]').element.parentElement?.textContent).toContain(
      'Minimum',
    )
    wrapper.unmount()
  })

  it('supports open-ended numeric ranges', async () => {
    const api = dashboardApi()
    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    await wrapper.get('[aria-controls="run-search-form"]').trigger('click')
    await wrapper.get('[aria-label="Parameter 1"]').setValue('HV_Vbias')
    await wrapper.get('[aria-label="Parameter 1"]').trigger('change')
    await wrapper.get('[aria-label="Match 1"]').setValue('range')

    await wrapper.get('[aria-label="Value 1"]').setValue('20')
    await wrapper.get('form[aria-label="Search stored runs"]').trigger('submit')
    await flushPromises()
    let predicate = vi.mocked(api.searchRuns).mock.calls.at(-1)![0].configuration[0]
    expect(predicate.comparison).toEqual(
      expect.objectContaining({
        case: 'real',
        value: expect.objectContaining({ minimum: 20 }),
      }),
    )
    if (predicate.comparison.case === 'real')
      expect(predicate.comparison.value.maximum).toBeUndefined()

    await wrapper.get('[aria-label="Value 1"]').setValue('')
    await wrapper.get('[aria-label="Maximum 1"]').setValue('50')
    await wrapper.get('form[aria-label="Search stored runs"]').trigger('submit')
    await flushPromises()
    predicate = vi.mocked(api.searchRuns).mock.calls.at(-1)![0].configuration[0]
    expect(predicate.comparison).toEqual(
      expect.objectContaining({
        case: 'real',
        value: expect.objectContaining({ maximum: 50 }),
      }),
    )
    if (predicate.comparison.case === 'real')
      expect(predicate.comparison.value.minimum).toBeUndefined()

    await wrapper.get('[aria-label="Maximum 1"]').setValue('')
    await wrapper.get('form[aria-label="Search stored runs"]').trigger('submit')
    expect(wrapper.get('.field-error').text()).toBe(
      'Range filters need a minimum, a maximum, or both.',
    )
    expect(api.searchRuns).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
