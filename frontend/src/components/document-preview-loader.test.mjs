import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const here = dirname(fileURLToPath(import.meta.url))
const src = readFileSync(join(here, 'document-preview.vue'), 'utf8')
const drawer = readFileSync(join(here, '../views/chat/components/ChatArtifactsDrawer.vue'), 'utf8')

test('custom blob loader overrides the default authenticated preview sources', () => {
  const loader = src.indexOf('if (props.loadBlob)')
  assert.ok(loader > -1)
  assert.ok(loader < src.indexOf('previewKnowledgeFile(props.knowledgeId)'))
  assert.ok(loader < src.indexOf('downloadArtifact(props.sessionId'))
  assert.match(drawer, /:load-blob="embeddedMode \? loadPreviewBlob : undefined"/)
  assert.match(drawer, /return downloadEmbedMessageArtifact\(/)
})
