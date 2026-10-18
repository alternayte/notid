import { useEffect, useState } from 'react'
import { applyTheme, readTheme, type Theme } from '~/theme'
import { ToggleGroup, ToggleGroupItem } from './primitives/toggle-group'

export function ThemeSwitch() {
  const [theme, setTheme] = useState<Theme>('system')
  useEffect(() => setTheme(readTheme()), [])
  return (
    <ToggleGroup
      type="single"
      aria-label="Theme"
      value={theme}
      onValueChange={(value) => {
        if (value !== 'light' && value !== 'dark' && value !== 'system') return
        applyTheme(value)
        setTheme(value)
      }}
    >
      <ToggleGroupItem value="light">Day</ToggleGroupItem>
      <ToggleGroupItem value="dark">Night</ToggleGroupItem>
      <ToggleGroupItem value="system">Auto</ToggleGroupItem>
    </ToggleGroup>
  )
}
