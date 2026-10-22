import { mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import NumericField from './NumericField.vue'
import { numericConstraint, parseConfiguration } from './configuration'

function editor(value = '20') {
  return mount(
    defineComponent({
      components: { NumericField },
      setup() {
        const field = ref(parseConfiguration(`HV_Vbias ${value}`).fields[0]!)
        const constraint = () => numericConstraint(field.value)!
        return { field, constraint }
      },
      template:
        '<NumericField :field="field" :constraint="constraint()" @change="field.value = $event" />',
    }),
  )
}

describe('numeric field typing', () => {
  it('allows clearing and typing a voltage digit by digit without clamping', async () => {
    const wrapper = editor()
    const input = wrapper.get('input')
    await input.setValue('')
    expect(input.element.value).toBe('')
    expect(wrapper.text()).toContain('Enter a numeric value.')
    await input.setValue('4')
    expect(input.element.value).toBe('4')
    expect(wrapper.text()).toContain('Minimum: 20.')
    await input.setValue('45')
    await input.setValue('45.4')
    expect(input.element.value).toBe('45.4')
    expect(input.attributes('aria-invalid')).toBe('false')
  })

  it('preserves voltage units across clearing and validates typed limits', async () => {
    const wrapper = editor('45.4 V')
    const input = wrapper.get('input')
    await input.setValue('')
    await input.setValue('90')
    expect(input.element.value).toBe('90')
    expect(wrapper.text()).toContain('Maximum: 85 V.')
    await wrapper.get('button[aria-label="Decrease HV_Vbias"]').trigger('click')
    expect(input.element.value).toBe('85')
    await wrapper.get('button[aria-label="Increase HV_Vbias"]').trigger('click')
    expect(input.element.value).toBe('85')
    expect(wrapper.find('.unit').text()).toBe('V')
  })
})
