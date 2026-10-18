export type Theme = 'light' | 'dark' | 'system'

const key = 'oc-theme'

// Runs before first paint, so a dark reader never sees a light flash.
export const themeScript = `(function(){try{var t=localStorage.getItem('${key}')||'system';var d=t==='dark'||(t==='system'&&matchMedia('(prefers-color-scheme: dark)').matches);document.documentElement.classList.toggle('dark',d);document.documentElement.dataset.theme=t}catch(e){}})()`

export function readTheme(): Theme {
  const t = document.documentElement.dataset.theme
  return t === 'light' || t === 'dark' ? t : 'system'
}

export function applyTheme(theme: Theme) {
  localStorage.setItem(key, theme)
  const dark = theme === 'dark' || (theme === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.classList.toggle('dark', dark)
  document.documentElement.dataset.theme = theme
}
