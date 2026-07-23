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
        statistics: {
          elapsedMilliseconds: 2000n,
          boards: [
            {
              chain: 0,
              triggerId: 12n,
              triggerCount: 10n,
              eventBuildCount: 10n,
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
    searchRuns: vi.fn().mockResolvedValue({ runs: [], nextPageToken: '' }),
    downloadArtifact: vi.fn().mockResolvedValue(new Blob()),
    histograms: vi.fn().mockResolvedValue([]),
  }
}

describe('operator dashboard', () => {
  it('separates operator tasks into keyboard-accessible workspace tabs', async () => {
    const wrapper = mount(App, { attachTo: document.body, props: { api: dashboardApi() } })
    await flushPromises()

    const acquisition = wrapper.get('#workspace-tab-acquisition')
    const monitoring = wrapper.get('#workspace-tab-monitoring')
    expect(acquisition.attributes('aria-selected')).toBe('true')
    expect(wrapper.get('#workspace-panel-acquisition').isVisible()).toBe(true)
    expect(wrapper.get('#workspace-panel-monitoring').isVisible()).toBe(false)

    await acquisition.trigger('keydown', { key: 'ArrowRight' })
    await new Promise((resolve) => requestAnimationFrame(resolve))
    expect(monitoring.attributes('aria-selected')).toBe('true')
    expect((wrapper.get('#workspace-panel-acquisition').element as HTMLElement).style.display).toBe(
      'none',
    )
    expect(
      (wrapper.get('#workspace-panel-monitoring').element as HTMLElement).style.display,
    ).not.toBe('none')
    expect(document.activeElement).toBe(monitoring.element)

    await monitoring.trigger('keydown', { key: 'End' })
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

    const options = wrapper.findAll('.options input[type="checkbox"]')
    expect(options).toHaveLength(2)
    expect(options.every((option) => !(option.element as HTMLInputElement).checked)).toBe(true)
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
    expect(wrapper.text()).toContain('DT5202 · node 0')
    expect(wrapper.text()).toContain('24.5 °C')
    expect(wrapper.get('#history-heading').text()).toBe('Run history')
    expect(wrapper.get('#statistics-heading').text()).toBe('Statistics')
    expect(wrapper.text()).toContain('Trigger ID')
    expect(wrapper.text()).toContain('run-54')
    expect(wrapper.text()).toContain('events.jsonl · 4.0 KiB')

    expect(wrapper.text()).not.toContain('Requested by')
    expect(wrapper.text()).not.toContain('Run ID')
    await wrapper
      .findAll('.section-tabs [role="tab"]')
      .find((tab) => tab.text() === 'All')!
      .trigger('click')
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
        requestedBy: 'operator',
        captureRaw: false,
        journalTransport: false,
      }),
    )
    expect(wrapper.get('#system-heading').text()).toBe('Running')
    wrapper.unmount()
  })

  it('commits a preset count before the numeric control loses focus', async () => {
    const api = dashboardApi()
    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    await wrapper.get('select').setValue('PRESET_COUNTS')
    const presetCounts = wrapper.get('input[type="number"][min="1"]')
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
      runs: [create(RunSummarySchema, { runId: 'matching-run', eventCount: 120n })],
      nextPageToken: '',
      $typeName: 'pet.caen.daq.v1.SearchRunsResponse',
    })
    const wrapper = mount(App, { props: { api } })
    await flushPromises()

    await wrapper.get('[aria-label="Parameter 1"]').setValue('TD_CoarseThreshold')
    await wrapper.get('[aria-label="Scope 1"]').setValue('channel')
    await wrapper.get('[aria-label="Board 1"]').setValue('2')
    await wrapper.get('[aria-label="Channel 1"]').setValue('17')
    await wrapper.get('[aria-label="Type 1"]').setValue('integer')
    await wrapper.get('[aria-label="Match 1"]').setValue('range')
    await wrapper.get('[aria-label="Value 1"]').setValue('200')
    await wrapper.get('[aria-label="Maximum 1"]').setValue('240')
    await wrapper.get('form[aria-label="Search stored runs"]').trigger('submit')
    await flushPromises()

    expect(api.searchRuns).toHaveBeenCalledWith(
      expect.objectContaining({
        configuration: [
          expect.objectContaining({
            parameter: 'TD_CoarseThreshold',
            scope: expect.objectContaining({
              scope: expect.objectContaining({
                case: 'channel',
                value: expect.objectContaining({ board: 2, channel: 17 }),
              }),
            }),
            comparison: expect.objectContaining({
              case: 'integer',
              value: expect.objectContaining({ minimum: 200n, maximum: 240n }),
            }),
          }),
        ],
      }),
    )
    expect(wrapper.get('[aria-label="Search results"]').text()).toContain('matching-run')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Clear')!
      .trigger('click')
    expect(wrapper.find('[aria-label="Search results"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
