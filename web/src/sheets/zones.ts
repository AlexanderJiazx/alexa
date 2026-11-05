// Timezones as people name them: "Los Angeles (GMT−7)", not
// "America/Los_Angeles". The IANA id stays the value; this is only how it
// reads. Pure, so it's tested without a DOM.

// zoneCity is the place a zone is named after: its last part, in words.
export function zoneCity(tz: string) {
  return (tz.split('/').pop() ?? tz).replace(/_/g, ' ')
}

// zoneOffset is the zone's offset from GMT at a moment: "GMT−7", "GMT+5:30",
// or "GMT" itself. '' for a zone this browser doesn't know.
export function zoneOffset(tz: string, at = Date.now()) {
  try {
    const part = new Intl.DateTimeFormat('en-US', { timeZone: tz, timeZoneName: 'shortOffset' }).formatToParts(at).find((p) => p.type === 'timeZoneName')
    return (part?.value ?? '').replace('-', '−').replace(/^GMT[+−]0$/, 'GMT')
  } catch {
    return ''
  }
}

// zoneLabel is how a zone reads in a list: "Los Angeles (GMT−7)".
export function zoneLabel(tz: string, at = Date.now()) {
  const off = zoneOffset(tz, at)
  return off ? `${zoneCity(tz)} (${off})` : zoneCity(tz)
}

export type ZoneOption = { value: string; label: string }

// zoneOptions is a list of zones to pick from, by place name. Two places
// with the same name ("Indianapolis" twice under America) say where.
export function zoneOptions(zones: string[], at = Date.now()): ZoneOption[] {
  const seen = new Map<string, number>()
  for (const z of zones) seen.set(zoneCity(z), (seen.get(zoneCity(z)) ?? 0) + 1)
  return zones
    .map((z) => {
      const city = zoneCity(z)
      const off = zoneOffset(z, at)
      const where = (seen.get(city) ?? 0) > 1 ? `${city}, ${z.split('/').slice(0, -1).join(' / ').replace(/_/g, ' ')}` : city
      return { value: z, label: off ? `${where} (${off})` : where }
    })
    .sort((a, b) => a.label.localeCompare(b.label))
}
