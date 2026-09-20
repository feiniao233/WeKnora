type HistoricalToolResult = {
  success?: boolean
}

type HistoricalToolCall = {
  name?: string
  args?: Record<string, unknown>
  result?: HistoricalToolResult | null
}

type HistoricalAgentStep = {
  tool_calls?: HistoricalToolCall[]
}

const isSubmitRCAReportTool = (name: unknown): boolean => {
  const normalized = typeof name === 'string' ? name.trim() : ''
  return normalized === 'submit_rca_report' || normalized.endsWith('submit_rca_report')
}

/**
 * Older agent messages can have an empty final content even though the report
 * submission succeeded. The submitted Markdown is persisted in the tool
 * arguments, so it is the authoritative answer for that historical turn.
 */
export const submittedRCAReportFromSteps = (steps: unknown): string => {
  if (!Array.isArray(steps)) return ''

  for (let stepIndex = steps.length - 1; stepIndex >= 0; stepIndex -= 1) {
    const step = steps[stepIndex] as HistoricalAgentStep | null
    const toolCalls = step?.tool_calls
    if (!Array.isArray(toolCalls)) continue

    for (let toolIndex = toolCalls.length - 1; toolIndex >= 0; toolIndex -= 1) {
      const toolCall = toolCalls[toolIndex]
      if (!isSubmitRCAReportTool(toolCall?.name) || toolCall?.result?.success !== true) {
        continue
      }

      const report = toolCall.args?.report
      if (typeof report === 'string' && report.trim()) return report.trim()
    }
  }

  return ''
}

