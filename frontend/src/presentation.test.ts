import { describe, expect, it } from 'vitest'
import { localDateTime } from './presentation'

describe('localDateTime', () => {
  it('formats protobuf timestamps in the browser locale and handles missing values', () => {
    const date = new Date('2026-07-22T17:30:00.125Z')
    expect(localDateTime({ seconds: 1784741400n, nanos: 125_000_000 })).toBe(
      date.toLocaleString(),
    )
    expect(localDateTime(undefined)).toBe('Not reported')
  })
})
