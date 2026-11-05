import { useState } from 'react'
import { getTheme, setTheme, type Theme } from '../lib/theme'
import { Segmented } from '../ui/Segmented'
import { Group, Row, Section, useSettingId } from './parts'

const themes: readonly (readonly [Theme, string])[] = [
  ['system', 'System'],
  ['light', 'Light'],
  ['dark', 'Dark'],
]

// Appearance picks light, dark or the system's setting, on this device. It
// applies at once.
export function Appearance() {
  const [theme, set] = useState(getTheme)
  return (
    <Section title="Appearance">
      <Group>
        <Row id={useSettingId('theme')} label="Theme" sub="Just on this device.">
          <Segmented
            label="Theme"
            size="sm"
            value={theme}
            options={themes}
            onChange={(t) => {
              setTheme(t)
              set(t)
            }}
          />
        </Row>
      </Group>
    </Section>
  )
}
