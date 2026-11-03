import { describe, expect, it } from 'vitest'
import { zoneCity, zoneLabel, zoneOffset, zoneOptions } from './zones'

const summer = Date.UTC(2026, 6, 1)
const winter = Date.UTC(2026, 0, 15)

describe('zones', () => {
  it('name a zone by its place and offset', () => {
    expect(zoneCity('America/Los_Angeles')).toBe('Los Angeles')
    expect(zoneCity('UTC')).toBe('UTC')
    expect(zoneLabel('America/Los_Angeles', summer)).toBe('Los Angeles (GMT−7)')
    expect(zoneLabel('America/Los_Angeles', winter)).toBe('Los Angeles (GMT−8)')
    expect(zoneLabel('Asia/Kolkata', summer)).toBe('Kolkata (GMT+5:30)')
    expect(zoneOffset('Not/AZone', summer)).toBe('')
    expect(zoneLabel('Not/A_Zone', summer)).toBe('A Zone')
  })

  it('list zones by place, telling apart two of the same name', () => {
    const list = zoneOptions(['Europe/Lisbon', 'America/Indiana/Indianapolis', 'America/Indianapolis', 'America/Los_Angeles'], winter)
    expect(list.map((o) => o.label)).toEqual(['Indianapolis, America (GMT−5)', 'Indianapolis, America / Indiana (GMT−5)', 'Lisbon (GMT)', 'Los Angeles (GMT−8)'])
    expect(list.find((o) => o.label.startsWith('Lisbon'))?.value).toBe('Europe/Lisbon')
  })
})
