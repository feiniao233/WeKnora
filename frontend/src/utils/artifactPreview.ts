import DOMPurify from 'dompurify'

const previewCSP = [
  "default-src 'none'",
  'img-src data: blob:',
  "style-src 'unsafe-inline'",
  'font-src data:',
  "form-action 'none'",
  "base-uri 'none'",
  "frame-src 'none'",
  "connect-src 'none'",
].join('; ')

export function isHTMLArtifact(fileName: string): boolean {
  return /\.html?$/i.test(fileName.trim())
}

export function buildArtifactPreviewDocument(html: string): string {
  const sanitized = typeof DOMPurify.sanitize === 'function'
    ? DOMPurify.sanitize(html, {
        ADD_TAGS: ['style'],
        ALLOW_DATA_ATTR: false,
        ALLOWED_URI_REGEXP: /^(?:(?:data|blob):|#)/i,
        FORBID_TAGS: ['script', 'iframe', 'object', 'embed', 'form', 'meta', 'base', 'link'],
        FORBID_ATTR: ['srcset'],
      })
    : html.replace(/[&<>"']/g, (char) => `&#${char.charCodeAt(0)};`)

  return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${previewCSP}"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body{margin:0;min-height:100%;background:#fff;color:#1f2329}body{padding:24px;box-sizing:border-box}</style></head><body>${sanitized}</body></html>`
}
