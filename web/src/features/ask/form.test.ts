import { describe, expect, it } from 'vitest'
import type { FormField } from '../../api/blocks'
import { atMax, atMin, check, hint, initial, initialValues, isSecret, safeFields, step, submitted, summary, validate } from './form'

// The ask's small form (WP-10): which fields are drawn, their values,
// inline validation and what a submit sends.

const field = (f: Partial<FormField> & Pick<FormField, 'name' | 'type'>): FormField => ({ label: f.name, ...f }) as FormField

describe('isSecret', () => {
  it('refuses anything that looks like a credential', () => {
    for (const f of [
      { name: 'password' },
      { name: 'pass_word' },
      { name: 'pin_code' },
      { name: 'pin' },
      { name: 'cvv' },
      { name: 'otp' },
      { name: 'verification_code' },
      { name: 'card', label: 'Card number' },
      { name: 'ssn' },
      { name: 'x', label: 'Two-factor code' },
      { name: 'x', label: 'API token' },
      { name: 'x', type: 'password' },
    ])
      expect(isSecret(f), JSON.stringify(f)).toBe(true)
  })
  it('keeps ordinary fields that only look close', () => {
    for (const f of [{ name: 'passengers' }, { name: 'passport_name', label: 'Name on passport' }, { name: 'spin_class' }, { name: 'pinned' }, { name: 'guests' }, { name: 'tokyo' }])
      expect(isSecret(f), JSON.stringify(f)).toBe(false)
  })
})

describe('safeFields', () => {
  it('drops secrets, unknown types, bad names and duplicates, and counts them', () => {
    const { fields, dropped } = safeFields([
      field({ name: 'name', type: 'text' }),
      field({ name: 'password', type: 'text' }),
      { name: 'secret', label: 'Secret', type: 'password' } as unknown as FormField,
      field({ name: 'cvv', type: 'number' }),
      field({ name: 'colour', type: 'slider' as FormField['type'] }),
      field({ name: 'Bad-Name', type: 'text' }),
      field({ name: 'name', type: 'text' }),
      field({ name: 'passengers', type: 'stepper' }),
    ])
    expect(fields.map((f) => f.name)).toEqual(['name', 'passengers'])
    expect(dropped).toBe(6)
  })
  it('keeps at most six fields, trims labels and options', () => {
    const many = Array.from({ length: 8 }, (_, i) => field({ name: `f_${'abcdefgh'[i]}`, type: 'text' }))
    const r = safeFields(many)
    expect(r.fields).toHaveLength(6)
    expect(r.dropped).toBe(2)
    const one = safeFields([field({ name: 'area', label: 'A label that is much too long to fit', type: 'select', options: ['a', ' ', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i'] })]).fields[0]
    expect(one.label).toHaveLength(24)
    expect(one.options).toEqual(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'])
  })
  it('copes with nothing', () => {
    expect(safeFields(undefined)).toEqual({ fields: [], dropped: 0 })
  })
})

describe('initial', () => {
  it('starts each type from a default that fits, or empty', () => {
    expect(initial(field({ name: 'a', type: 'toggle', value: 'true' }))).toBe(true)
    expect(initial(field({ name: 'a', type: 'toggle' }))).toBe(false)
    expect(initial(field({ name: 'a', type: 'stepper', min: 1 }))).toBe(1)
    expect(initial(field({ name: 'a', type: 'stepper', value: 12, max: 8 }))).toBe(8)
    expect(initial(field({ name: 'a', type: 'number', value: 40 }))).toBe('40')
    expect(initial(field({ name: 'a', type: 'number', value: 'x' }))).toBe('')
    expect(initial(field({ name: 'a', type: 'select', options: ['A', 'B'], value: 'C' }))).toBe('')
    expect(initial(field({ name: 'a', type: 'select', options: ['A', 'B'], value: 'B' }))).toBe('B')
    expect(initial(field({ name: 'a', type: 'multiselect', options: ['A', 'B'], value: ['B', 'Z'] }))).toEqual(['B'])
    expect(initial(field({ name: 'a', type: 'date', value: '2026-11-14' }))).toBe('2026-11-14')
  })
  it('gives every field a value', () => {
    expect(initialValues([field({ name: 'a', type: 'text' }), field({ name: 'b', type: 'toggle' })])).toEqual({ a: '', b: false })
  })
})

describe('check', () => {
  it('says what a required field is missing', () => {
    expect(check(field({ name: 'a', type: 'text', required: true }), '')).toBe('Required')
    expect(check(field({ name: 'a', type: 'select', required: true, options: ['A'] }), '')).toBe('Pick one')
    expect(check(field({ name: 'a', type: 'multiselect', required: true, options: ['A'] }), [])).toBe('Pick at least one')
    expect(check(field({ name: 'a', type: 'text' }), '')).toBe('')
  })
  it('checks numbers against their bounds', () => {
    const f = field({ name: 'a', type: 'number', min: 40, max: 900 })
    expect(check(f, 'abc')).toBe('Enter a number')
    expect(check(f, '12')).toBe('At least 40')
    expect(check(f, '1200')).toBe('At most 900')
    expect(check(f, '120')).toBe('')
  })
  it('refuses a select value that wasn’t offered', () => {
    expect(check(field({ name: 'a', type: 'select', options: ['A'] }), 'B')).toBe('Pick one')
  })
  it('never errors on a toggle or a stepper', () => {
    expect(check(field({ name: 'a', type: 'toggle', required: true }), false)).toBe('')
    expect(check(field({ name: 'a', type: 'stepper', required: true }), 0)).toBe('')
  })
  it('collects the errors of a whole form', () => {
    const fields = [field({ name: 'a', type: 'text', required: true }), field({ name: 'b', type: 'text' })]
    expect(validate(fields, { a: '', b: '' })).toEqual({ a: 'Required' })
  })
})

describe('step', () => {
  it('moves within bounds', () => {
    const f = field({ name: 'a', type: 'stepper', min: 1, max: 3 })
    expect(step(f, 1, 1)).toBe(2)
    expect(step(f, 3, 1)).toBe(3)
    expect(step(f, 1, -1)).toBe(1)
    expect(atMin(f, 1)).toBe(true)
    expect(atMax(f, 3)).toBe(true)
    expect(atMax(f, 2)).toBe(false)
    expect(step(field({ name: 'a', type: 'stepper', step: 5 }), 10, 1)).toBe(15)
  })
})

describe('submitted', () => {
  it('types the values and leaves out empty text', () => {
    const fields = [
      field({ name: 'name', type: 'text' }),
      field({ name: 'note', type: 'text' }),
      field({ name: 'budget', type: 'number' }),
      field({ name: 'guests', type: 'stepper' }),
      field({ name: 'breakfast', type: 'toggle' }),
      field({ name: 'rooms', type: 'multiselect', options: ['Twin', 'Double'] }),
    ]
    expect(submitted(fields, { name: '  Sam ', note: '', budget: '120', guests: 2, breakfast: false, rooms: ['Twin'] })).toEqual({ name: 'Sam', budget: 120, guests: 2, breakfast: false, rooms: ['Twin'] })
  })
})

describe('summary', () => {
  it('reads as a short line, counts number first', () => {
    const fields = [
      field({ name: 'guests', label: 'Guests', type: 'stepper' }),
      field({ name: 'nights', label: 'Nights', type: 'stepper' }),
      field({ name: 'area', label: 'Area', type: 'select', options: ['Alfama'] }),
      field({ name: 'breakfast', label: 'Breakfast included', type: 'toggle' }),
      field({ name: 'parking', label: 'Parking', type: 'toggle' }),
      field({ name: 'name', label: 'Name', type: 'text' }),
      field({ name: 'budget', label: 'Max per night', type: 'number' }),
      field({ name: 'rooms', label: 'Rooms', type: 'multiselect', options: ['Twin', 'Double'] }),
    ]
    const s = summary(fields, { guests: 2, nights: 1, area: 'Alfama', breakfast: true, parking: false, name: 'Sam', budget: '120', rooms: ['Twin', 'Double'] })
    expect(s).toBe('2 guests · 1 night · Area: Alfama · Breakfast included · Name: Sam · Max per night: 120 · Rooms: Twin, Double')
  })
  it('shows dates with their label', () => {
    const s = summary([field({ name: 'check_in', label: 'Check-in', type: 'date' })], { check_in: '2026-11-14' })
    expect(s.startsWith('Check-in: ')).toBe(true)
    expect(s).not.toContain('2026-11-14')
  })
  it('never runs a label into its value', () => {
    const s = summary(
      [
        field({ name: 'city', label: 'City', type: 'text' }),
        field({ name: 'budget', label: 'Budget per night (USD)', type: 'number' }),
        field({ name: 'breakfast', label: 'Breakfast', type: 'select', options: ['Yes', 'No'] }),
      ],
      { city: 'Porto', budget: '180', breakfast: 'Yes' },
    )
    expect(s).toBe('City: Porto · Budget per night (USD): 180 · Breakfast: Yes')
  })
  it('falls back to the submit label when nothing was filled', () => {
    expect(summary([field({ name: 'a', type: 'text' })], { a: '' }, 'Search')).toBe('Search')
    expect(summary([], {})).toBe('Done')
  })
})

describe('hint', () => {
  it('reads a bare placeholder as an example, so it never looks filled in', () => {
    expect(hint('Lisbon')).toBe('e.g. Lisbon')
    expect(hint(' 2 ')).toBe('e.g. 2')
  })
  it('leaves one that is already an example or an instruction', () => {
    expect(hint('e.g. Lisbon')).toBe('e.g. Lisbon')
    expect(hint('eg Porto')).toBe('eg Porto')
    expect(hint('For example, Rome')).toBe('For example, Rome')
    expect(hint('Your city')).toBe('Your city')
    expect(hint('Type a city…')).toBe('Type a city…')
    expect(hint('Egypt')).toBe('e.g. Egypt')
    expect(hint('€')).toBe('€')
  })
  it('has nothing to show for nothing', () => {
    expect(hint(undefined)).toBeUndefined()
    expect(hint('  ')).toBeUndefined()
  })
})
