import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import MaskEditor from './MaskEditor.vue'
import NumericField from './NumericField.vue'
import { parseConfiguration } from './configuration'

describe('parameter controls', () => {
  it('increments and decrements bounded integers while retaining native number input', async () => {
    const field = parseConfiguration('MajorityLevel 4 # Majority Level (1 to 64)').fields[0]
    const wrapper = mount(NumericField, {
      props: { field, constraint: { min: 1, max: 64, step: 1, integer: true } },
    })
    expect(wrapper.get('input').attributes()).toMatchObject({
      type: 'number',
      min: '1',
      max: '64',
      step: '1',
    })
    await wrapper.get('button[aria-label="Increase MajorityLevel"]').trigger('click')
    expect(wrapper.emitted('change')?.at(-1)).toEqual(['5'])
    await wrapper.get('input').setValue('12')
    expect(wrapper.emitted('change')?.at(-1)).toEqual(['12'])
  })

  it('edits 64 channels with bulk actions and emits paired hexadecimal masks', async () => {
    const wrapper = mount(MaskEditor, {
      props: { title: 'Channel mask', low: '0x00000000', high: '0x00000000' },
    })
    await wrapper.get('button[aria-label="Channel 0"]').trigger('click')
    await wrapper.get('button[aria-label="Channel 63"]').trigger('click')
    await wrapper.get('button.primary').trigger('click')
    expect(wrapper.emitted('apply')).toEqual([['0x00000001', '0x80000000']])
  })
})
