import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import { parseConfiguration } from './configuration'
import HvBiasSummary from './HvBiasSummary.vue'

it('updates board estimates as settings change and opens channel inspection', async () => {
  const fields = (extra = '') =>
    parseConfiguration(`HV_Vbias 55\nHV_Adjust_Range 2.5\nHV_IndivAdj 255\n${extra}`).fields
  const wrapper = mount(HvBiasSummary, { props: { fields: fields(), boards: [0, 2] } })
  expect(wrapper.get('[aria-label="B0 estimated channel bias"]').text()).toContain('55.00 V')
  await wrapper.setProps({ fields: fields('HV_IndivAdj[2][0] 55') })
  expect(wrapper.get('[aria-label="B2 estimated channel bias"]').text()).toContain('53.04–55.00 V')
  await wrapper.setProps({ fields: fields('HV_Vbias[2] 56') })
  expect(wrapper.get('[aria-label="B2 estimated channel bias"]').text()).toContain('56.00 V')
  await wrapper.setProps({ fields: fields('HV_Vbias[2] bad') })
  expect(wrapper.get('[aria-label="B2 estimated channel bias"]').text()).toContain('Unavailable')
  await wrapper.get('button').trigger('click')
  expect(wrapper.emitted('inspect')).toHaveLength(1)
})
