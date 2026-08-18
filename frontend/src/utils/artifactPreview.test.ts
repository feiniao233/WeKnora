import assert from 'node:assert/strict'
import test from 'node:test'

import { buildArtifactPreviewDocument, isHTMLArtifact } from './artifactPreview.ts'

test('HTML artifact preview is isolated and strips active content', () => {
  assert.equal(isHTMLArtifact('report.HTML'), true)
  assert.equal(isHTMLArtifact('report.pdf'), false)

  const document = buildArtifactPreviewDocument(
    '<style>h1{color:red}</style><h1>Report</h1><script>alert(1)</script><form action="https://example.com"><button>send</button></form>',
  )
  assert.match(document, /Content-Security-Policy/)
  assert.match(document, /default-src 'none'/)
  assert.doesNotMatch(document, /<script\b/i)
  assert.doesNotMatch(document, /<form\b/i)
})
