import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'CAEN DAQ',
  description: 'Operation, telemetry, configuration, and hardware evidence for the PET DAQ.',
  lang: 'en-US',
  base: '/docs/',
  lastUpdated: true,
  // VitePress treats loopback URLs as internal; this is an example DAQ endpoint.
  ignoreDeadLinks: [/^http:\/\/localhost:8081\/?$/],
  head: [['meta', { name: 'robots', content: 'noindex, nofollow' }]],
  themeConfig: {
    nav: [
      { text: 'Operator guide', link: '/guide/' },
      { text: 'Full manual', link: '/operator-user-manual' },
      { text: 'Engineering', link: '/architecture' },
    ],
    search: { provider: 'local' },
    outline: [2, 3],
    sidebar: [
      { text: 'Operate the DAQ', items: [
        { text: 'Start here', link: '/guide/' },
        { text: 'Reading telemetry', link: '/guide/telemetry' },
        { text: 'Temperatures and HV', link: '/guide/temperatures-and-hv' },
        { text: 'HV bias and channel offset', link: '/guide/hv-bias-and-offset' },
        { text: 'Trigger configuration', link: '/guide/triggers' },
        { text: 'Hardware caveats', link: '/guide/caveats' },
        { text: 'Complete operator manual', link: '/operator-user-manual' },
        { text: 'Source scans', link: '/source-scans' },
        { text: 'Recovery procedures', link: '/hardware-operations' },
      ] },
      { text: 'Data and engineering', collapsed: true, items: [
        { text: 'HDF5 schema', link: '/hdf5-schema-examples' },
        { text: 'Run catalog', link: '/run-catalog-operations' },
        { text: 'Architecture', link: '/architecture' },
        { text: 'Protocol evidence', link: '/daq_protocol_notes' },
        { text: 'Hardware captures', link: '/real-hardware-capture-evidence' },
        { text: 'Architecture decisions', link: '/adr/README' },
        { text: 'Testing', link: '/testing-strategy' },
      ] },
      { text: 'Maintain this site', items: [
        { text: 'Editing and publishing', link: '/documentation-site' },
      ] },
    ],
  },
})
