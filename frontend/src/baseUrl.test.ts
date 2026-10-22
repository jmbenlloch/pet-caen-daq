import { describe, expect, it } from 'vitest'
import { daqBaseUrl } from './baseUrl'

describe('DAQ mount path', () => {
  it.each([
    ['https://pet.example/', 'https://pet.example/'],
    ['https://pet.example/daq/', 'https://pet.example/daq/'],
    ['https://pet.example/daq/?tab=hv#status', 'https://pet.example/daq/'],
  ])('keeps RPCs beside the UI at %s', (page, expected) => {
    expect(daqBaseUrl(page)).toBe(expected)
  })
})
