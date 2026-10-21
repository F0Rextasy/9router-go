import { describe, expect, test } from 'bun:test'
import {
  headerNameError,
  headerValueError,
  MAX_OVERRIDE_VALUE_LENGTH,
} from './headerOverrideValidation'

describe('headerNameError', () => {
  test('accepts a name built from the RFC 7230 token punctuation', () => {
    expect(headerNameError("X-!#$%&'*+-.^_`|~")).toBe('')
    expect(headerNameError('X-Custom-Header')).toBe('')
    expect(headerNameError('x-token')).toBe('')
  })

  test('rejects a name with a space', () => {
    expect(headerNameError('X Custom')).not.toBe('')
  })

  test('rejects a name with a colon', () => {
    expect(headerNameError('X-Token: v')).not.toBe('')
  })

  test('rejects a name with a parenthesis', () => {
    expect(headerNameError('X-Token(v2)')).not.toBe('')
  })

  test('rejects a name with a non-ASCII letter', () => {
    expect(headerNameError('X-Tökén')).not.toBe('')
  })

  test('an empty name is a row being typed, not an error', () => {
    expect(headerNameError('')).toBe('')
    expect(headerNameError('   ')).toBe('')
  })
})

describe('headerValueError', () => {
  test('rejects a value carrying CR', () => {
    expect(headerValueError('a\r b')).not.toBe('')
  })

  test('rejects a value carrying LF', () => {
    expect(headerValueError('a\nb')).not.toBe('')
  })

  test('rejects a value carrying NUL', () => {
    expect(headerValueError('a\u0000b')).not.toBe('')
  })

  test('rejects a value carrying DEL', () => {
    expect(headerValueError('a\u007fb')).not.toBe('')
  })

  test('rejects the other C0 controls too', () => {
    expect(headerValueError('a\u0001b')).not.toBe('')
  })

  test('accepts a value carrying TAB', () => {
    expect(headerValueError('a\tb')).toBe('')
  })

  test('accepts an ordinary value', () => {
    expect(headerValueError('Bearer sk-abc123')).toBe('')
  })

  test('rejects a value longer than the cap', () => {
    expect(headerValueError('a'.repeat(MAX_OVERRIDE_VALUE_LENGTH))).toBe('')
    expect(headerValueError('a'.repeat(MAX_OVERRIDE_VALUE_LENGTH + 1))).not.toBe('')
  })
})
