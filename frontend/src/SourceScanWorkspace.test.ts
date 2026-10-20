import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import SourceScanWorkspace from './SourceScanWorkspace.vue'
import { SystemState } from './gen/pet/caen/daq/v1/system_pb'

const api = vi.hoisted(() => ({
  getSourceScan: vi.fn().mockResolvedValue({ deviceConfigured: true }),
  listSourceScans: vi.fn().mockResolvedValue({ scans: [], totalCount: 0 }),
  startSourceScan: vi.fn().mockResolvedValue({}),
}))
vi.mock('@connectrpc/connect', () => ({ createClient: () => api }))
afterEach(() => vi.clearAllMocks())

it('explains invalid pitch, blocks submission and clears errors when corrected', async () => {
  const wrapper = mount(SourceScanWorkspace, {
    props: {
      configuration: 'PET configuration',
      systemState: SystemState.READY,
      captureRaw: false,
      journalTransport: false,
      persistHistograms: true,
      hdf5SegmentSizeMb: 500,
      hdf5Compression: 'none',
    },
  })
  try {
    await flushPromises()
    await wrapper.get('.scan-card-toggle').trigger('click')
    const input = (label: string) =>
      wrapper
        .findAll('label')
        .find((l) => l.text().startsWith(label))!
        .get('input')
    await input('X minimum').setValue('-5.5')
    await input('X maximum').setValue('5.5')
    await input('I placed').setValue(true)
    expect(wrapper.get('#source-start-reasons').text()).toContain('X span is 11 mm')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(api.startSourceScan).not.toHaveBeenCalled()
    await input('Grid pitch').setValue('1')
    expect(wrapper.find('#source-start-reasons').exists()).toBe(false)
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ systemState: SystemState.DISCONNECTED })
    expect(wrapper.get('#source-start-reasons').text()).toContain('connected and Ready')
  } finally {
    wrapper.unmount()
  }
})
