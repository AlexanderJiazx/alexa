import type { FormField, FormFieldType } from '../../api/blocks'
import { fmtClock, fmtDate } from '../../lib/format'

// The small form an ask can carry (Appendix A.4): which fields are safe to
// draw, their starting values, inline validation, and what is sent. Pure, so
// the rules are tested without a DOM (form.test.ts).

export type Value = string | number | boolean | string[]
export type Values = Record<string, Value>
export type Errors = Record<string, string>

const types: FormFieldType[] = ['text', 'number', 'date', 'time', 'datetime', 'select', 'multiselect', 'toggle', 'stepper']
const nameRe = /^[a-z_]{1,24}$/

// Anything that looks like a secret never becomes a field: the server
// refuses such forms, and the client refuses them again so a bad or old
// server can't make the app collect a password. Secrets go through the
// sign-in card or a take-over.
const secretRe = /\bpass ?(word|code|phrase)s?\b|\bpasswd\b|\bpwd\b|\bpin\b|\bcvc\b|\bcvv\b|\bcsc\b|\bcard ?(number|no)\b|\bcredit ?card\b|\bssn\b|social ?security|\b2fa\b|two.?factor|\botp\b|one.?time ?(code|password|pin)|verification ?code|security ?code|auth(entication)? ?code|\bsecret\b|\btoken\b/i

export function isSecret(f: { name?: string; label?: string; type?: string }) {
  const words = (s?: string) => (s ?? '').replace(/[_-]+/g, ' ')
  return f.type === 'password' || secretRe.test(words(f.name)) || secretRe.test(words(f.label))
}

// safeFields keeps the fields this client can draw: a known type, a valid
// name, at most six, and never a secret. dropped says how many were left
// out, so the card can say so.
export function safeFields(fields: FormField[] | undefined): { fields: FormField[]; dropped: number } {
  const out: FormField[] = []
  let dropped = 0
  for (const f of fields ?? []) {
    if (!f || typeof f !== 'object' || isSecret(f) || !types.includes(f.type) || !nameRe.test(f.name) || out.some((x) => x.name === f.name)) {
      dropped++
      continue
    }
    if (out.length >= 6) {
      dropped++
      continue
    }
    out.push({ ...f, label: (f.label || f.name).slice(0, 24), options: f.options?.filter((o) => typeof o === 'string' && o.trim()).slice(0, 8) })
  }
  return { fields: out, dropped }
}

const num = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) ? v : typeof v === 'string' && v.trim() !== '' && Number.isFinite(Number(v)) ? Number(v) : undefined)

function clamp(n: number, f: FormField) {
  let x = n
  if (f.min !== undefined) x = Math.max(f.min, x)
  if (f.max !== undefined) x = Math.min(f.max, x)
  return x
}

// initial is a field's starting value: the model's default when it fits,
// else empty. Numbers are kept as typed text until submit; a stepper always
// holds a number.
export function initial(f: FormField): Value {
  const v = f.value
  switch (f.type) {
    case 'toggle':
      return v === true || v === 'true'
    case 'stepper':
      return clamp(num(v) ?? f.min ?? 0, f)
    case 'number':
      return num(v) !== undefined ? String(num(v)) : ''
    case 'multiselect': {
      const list = Array.isArray(v) ? v : typeof v === 'string' && v ? [v] : []
      return list.filter((x) => f.options?.includes(x))
    }
    case 'select':
      return typeof v === 'string' && f.options?.includes(v) ? v : ''
    default:
      return typeof v === 'string' ? v : typeof v === 'number' ? String(v) : ''
  }
}

export const initialValues = (fields: FormField[]): Values => Object.fromEntries(fields.map((f) => [f.name, initial(f)]))

const empty = (v: Value | undefined) => v === undefined || v === '' || (Array.isArray(v) && v.length === 0)

// check validates one field: an error to show under it, or ''.
export function check(f: FormField, v: Value | undefined): string {
  if (f.type === 'toggle' || f.type === 'stepper') return ''
  if (empty(v)) {
    if (!f.required) return ''
    return f.type === 'multiselect' ? 'Pick at least one' : f.type === 'select' ? 'Pick one' : 'Required'
  }
  if (f.type === 'number') {
    const n = num(v)
    if (n === undefined) return 'Enter a number'
    if (f.min !== undefined && n < f.min) return `At least ${f.min}`
    if (f.max !== undefined && n > f.max) return `At most ${f.max}`
  }
  if (f.type === 'select' && typeof v === 'string' && f.options && !f.options.includes(v)) return 'Pick one'
  return ''
}

export function validate(fields: FormField[], values: Values): Errors {
  const out: Errors = {}
  for (const f of fields) {
    const e = check(f, values[f.name])
    if (e) out[f.name] = e
  }
  return out
}

// step moves a stepper by one step, within its bounds.
export const step = (f: FormField, v: Value | undefined, dir: 1 | -1) => clamp((num(v) ?? f.min ?? 0) + dir * (f.step && f.step > 0 ? f.step : 1), f)

export const atMin = (f: FormField, v: Value | undefined) => f.min !== undefined && (num(v) ?? 0) <= f.min
export const atMax = (f: FormField, v: Value | undefined) => f.max !== undefined && (num(v) ?? 0) >= f.max

// submitted is what goes to the server as values: typed, empty text left
// out. The server checks them against the form again and builds the
// "[Form] name=value; …" line Alexa reads.
export function submitted(fields: FormField[], values: Values): Values {
  const out: Values = {}
  for (const f of fields) {
    const v = values[f.name]
    if (f.type === 'toggle') out[f.name] = v === true
    else if (f.type === 'stepper' || f.type === 'number') {
      const n = num(v)
      if (n !== undefined) out[f.name] = n
    } else if (!empty(v)) out[f.name] = typeof v === 'string' ? v.trim() : (v as Value)
  }
  return out
}

const lowerFirst = (s: string) => (/^[A-Z][a-z]/.test(s) ? s[0].toLowerCase() + s.slice(1) : s)

// Each part of a summary reads "Label: value", the same for every kind of
// field, so the line never runs a label into its value.
const labelled = (label: string, value: string) => `${label}: ${value}`

// A count reads best number first ("2 guests", "1 night"); a plural label
// marks one. Anything else is labelled like the rest.
function count(label: string, n: number) {
  if (!/^[A-Za-z]+s$/.test(label)) return labelled(label, String(n))
  const noun = lowerFirst(label)
  return `${n} ${n === 1 ? noun.replace(/s$/, '') : noun}`
}

// summary is the visible message a submitted form sends, and the chip the
// card shows once it's sent: "Check-in: Sat, Nov 14 · 2 guests · Area:
// Alfama · Breakfast included".
export function summary(fields: FormField[], values: Values, submit = 'Done'): string {
  const parts: string[] = []
  for (const f of fields) {
    const v = values[f.name]
    switch (f.type) {
      case 'toggle':
        if (v === true) parts.push(f.label)
        break
      case 'stepper':
      case 'number': {
        const n = num(v)
        if (n !== undefined) parts.push(count(f.label, n))
        break
      }
      case 'date':
        if (typeof v === 'string' && v) parts.push(labelled(f.label, fmtDate(v, 'day')))
        break
      case 'datetime':
        if (typeof v === 'string' && v) parts.push(labelled(f.label, fmtDate(v, 'dayTime')))
        break
      case 'time':
        if (typeof v === 'string' && v) parts.push(labelled(f.label, fmtClock(v)))
        break
      case 'multiselect':
        if (Array.isArray(v) && v.length) parts.push(labelled(f.label, v.join(', ')))
        break
      case 'select':
        if (typeof v === 'string' && v) parts.push(labelled(f.label, v))
        break
      default:
        if (typeof v === 'string' && v.trim()) parts.push(labelled(f.label, v.trim()))
    }
  }
  return parts.join(' · ') || submit
}

// hint is what an empty field shows: the model's placeholder, read as an
// example ("e.g. Lisbon") so it never looks like a value already filled
// in. One that already reads as an example or an instruction, or is only a
// unit ("€"), stays as it is.
export function hint(placeholder: string | undefined): string | undefined {
  const p = placeholder?.trim()
  if (!p) return undefined
  if (!/[\p{L}\p{N}]/u.test(p)) return p
  if (/^(e\.?g|for example|for instance|such as|like|type|enter|add|pick|choose|select|your|any|optional|search)\b/i.test(p) || /(…|\.\.\.)$/.test(p)) return p
  return `e.g. ${p}`
}
