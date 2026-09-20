import assert from 'node:assert/strict'
import test from 'node:test'
import { submittedRCAReportFromSteps } from './agent-history'

const report = '# 根因分析报告\n\n## 元数据\n\n- 场景 ID：ip-conflict'

test('restores the report from a successful historical MCP submission', () => {
  assert.equal(
    submittedRCAReportFromSteps([
      {
        tool_calls: [
          {
            name: 'mcp_ops_submit_rca_report',
            args: { report: `  ${report}\n` },
            result: { success: true, output: '{"已提交":true}' },
          },
        ],
      },
    ]),
    report,
  )
})

test('supports the direct and double-underscore historical tool names', () => {
  for (const name of ['submit_rca_report', 'ops__submit_rca_report']) {
    assert.equal(
      submittedRCAReportFromSteps([
        { tool_calls: [{ name, args: { report }, result: { success: true } }] },
      ]),
      report,
    )
  }
})

test('does not promote a report when submission failed or was not completed', () => {
  assert.equal(
    submittedRCAReportFromSteps([
      {
        tool_calls: [
          { name: 'mcp_ops_submit_rca_report', args: { report }, result: { success: false } },
          { name: 'mcp_ops_submit_rca_report', args: { report } },
        ],
      },
    ]),
    '',
  )
})

test('uses the latest successful submitted report', () => {
  assert.equal(
    submittedRCAReportFromSteps([
      {
        tool_calls: [
          {
            name: 'mcp_ops_submit_rca_report',
            args: { report: 'first report' },
            result: { success: true },
          },
        ],
      },
      {
        tool_calls: [
          {
            name: 'mcp_ops_submit_rca_report',
            args: { report: 'latest report' },
            result: { success: true },
          },
        ],
      },
    ]),
    'latest report',
  )
})
