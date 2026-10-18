// RenderedHtml is HTML produced by the server's Markdown renderer and passed
// through its sanitizer (bluemonday). Only API responses carry this type, and
// this component is the only place the web app inserts raw HTML.
export type RenderedHtml = string & { readonly __rendered: unique symbol }

// fromRenderer marks a string from an API field that the server renders and sanitizes.
export function fromRenderer(html: string): RenderedHtml {
  return html as RenderedHtml
}

export function Rendered({ html, className }: { html: RenderedHtml; className?: string }) {
  // biome-ignore lint/security/noDangerouslySetInnerHtml: html is server-rendered and sanitized; see RenderedHtml
  return <div className={className} dangerouslySetInnerHTML={{ __html: html }} />
}
