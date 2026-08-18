import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'

test('secure token endpoint receives configured Steel authorization header', async () => {
  const elements = []
  const created = []
  let request
  const body = {
    appendChild(element) {
      element.parentNode = body
      elements.push(element)
    },
    removeChild(element) {
      const index = elements.indexOf(element)
      if (index >= 0) elements.splice(index, 1)
    },
  }
  const document = {
    currentScript: null,
    body,
    createElement(tag) {
      const element = {
        tag,
        style: {},
        listeners: {},
        contentWindow: tag === 'iframe' ? { postMessage() {} } : undefined,
        setAttribute() {},
        getAttribute() { return null },
        appendChild() {},
        addEventListener(name, handler) { this.listeners[name] = handler },
      }
      created.push(element)
      return element
    },
  }
  const context = {
    URL,
    document,
    location: { origin: 'https://steel.example', href: 'https://steel.example/' },
    console,
    setTimeout: () => 1,
    clearTimeout() {},
    addEventListener() {},
    removeEventListener() {},
    fetch: async (url, options) => {
      request = { url, options }
      return { ok: true, json: async () => ({ token: 'ems_test', expiresIn: 1800 }) }
    },
  }
  context.window = context
  vm.runInNewContext(readFileSync(new URL('../../public/weknora-widget.js', import.meta.url), 'utf8'), context)

  context.WeKnora.init({
    baseUrl: 'https://weknora.example',
    channel: 'steel',
    tokenEndpoint: '/back/rca/embed-token',
    tokenEndpointHeaders: { Authorization: 'steel-session-token' },
  })
  created.find((element) => element.tag === 'iframe').listeners.load()
  await new Promise((resolve) => setImmediate(resolve))

  assert.equal(request.url, '/back/rca/embed-token')
  assert.equal(request.options.credentials, 'include')
  assert.equal(request.options.headers.Authorization, 'steel-session-token')
  assert.equal(request.options.headers.Accept, 'application/json')
})
