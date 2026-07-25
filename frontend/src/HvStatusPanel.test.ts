import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import HvStatusPanel from './HvStatusPanel.vue'

const board = (chain: number, state: 'off' | 'on' | 'ramping' | 'fault') => ({
  chain,
  node: 0,
  hvVoltageV: state === 'off' ? 4 : state === 'ramping' ? 25 : 45,
  hvTargetVoltageV: 45,
  hvOn: state === 'on' || state === 'ramping' || state === 'fault',
  hvRamping: state === 'ramping',
  hvOverCurrent: state === 'fault',
  hvOverVoltage: false,
})

describe('HV status panel', () => {
  it('shows independent board LEDs with fault priority globally', () => {
    const wrapper = mount(HvStatusPanel, {
      props: { boards: [board(0, 'off'), board(1, 'on'), board(2, 'ramping'), board(3, 'fault')] },
    })

    expect(wrapper.get('[aria-label="Chain 0 node 0 HV: Off"]').classes()).toContain('off')
    expect(wrapper.get('[aria-label="Chain 1 node 0 HV: On"]').classes()).toContain('on')
    expect(wrapper.get('[aria-label="Chain 2 node 0 HV: Ramping"]').classes()).toContain('ramping')
    expect(wrapper.get('[aria-label="Chain 3 node 0 HV: Fault"]').classes()).toContain('fault')
    expect(wrapper.get('[aria-label="HV summary: Fault"]').classes()).toContain('fault')
  })

  it('reports ramping globally before an on board', () => {
    const wrapper = mount(HvStatusPanel, {
      props: { boards: [board(0, 'on'), board(1, 'ramping')] },
    })

    expect(wrapper.get('[aria-label="HV summary: Ramping"]').classes()).toContain('ramping')
  })

  it('does not describe a partially enabled system as all on', () => {
    const wrapper = mount(HvStatusPanel, {
      props: { boards: [board(0, 'off'), board(1, 'on'), board(2, 'off'), board(3, 'off')] },
    })

    expect(wrapper.get('[aria-label="HV summary: 1/4 on"]').text()).toBe('1/4 on')
  })

  it('keeps ramp-up visible after the hardware ramp bit clears', () => {
    const rising = { ...board(0, 'on'), hvVoltageV: 17.7, hvRamping: false }
    const wrapper = mount(HvStatusPanel, { props: { boards: [rising] } })

    expect(wrapper.get('[aria-label="Chain 0 node 0 HV: Ramping"]').classes()).toContain('ramping')
  })

  it('keeps ramp-down visible until the measured voltage is near the off baseline', () => {
    const falling = { ...board(0, 'off'), hvVoltageV: 20, hvRamping: false }
    const wrapper = mount(HvStatusPanel, { props: { boards: [falling] } })

    expect(wrapper.get('[aria-label="Chain 0 node 0 HV: Ramping"]').classes()).toContain('ramping')
  })
})
