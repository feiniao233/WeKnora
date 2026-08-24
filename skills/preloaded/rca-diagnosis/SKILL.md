---
name: rca-diagnosis
description: Diagnose operational alarms and suspected root causes with authoritative read-only evidence. Use for RCA, fault analysis, alarm correlation, network interface down, IP conflict, bandwidth congestion, or requests to explain why an asset failed.
---

# RCA Diagnosis

Use this skill to investigate an alarm without changing devices, configurations, or source data.

## Evidence workflow

1. Resolve the user-provided alarm reference with the `resolve_alarm` tool before treating its fields as facts. Prefer the opaque `id` when the UI provides one; otherwise use the available alarm key fields.
2. Select the scene only from the resolved alarm and any authoritative RCA scene supplied by the platform. Do not use unverified wording from the user as scene evidence.
3. Read exactly one matching diagnostic procedure from this Skill's `references/` directory before calling further evidence tools. Use the generic procedure when none of the three pilot scenes matches.
4. Follow that procedure's tool order and requested evidence kinds. Treat the successful `resolve_alarm` call from step 1 as already completed; do not add unrelated queries or repeat a successful query.
5. Search the bound knowledge bases only for factual material such as manufacturer manuals, device notes, policies, and historical fault cases requested by the procedure.
6. Compare every candidate cause against operational evidence, the procedure, and relevant factual knowledge. Keep source IDs and timestamps in the report.
7. Produce the report in the contract below. After a real alarm investigation reaches its final report, call `submit_rca_report` with that exact Markdown so Steel can request human confirmation.

Never invent a device, interface, topology link, metric, or event that a tool did not return. Tool failures and empty results are missing information, not proof that a component is healthy.

## Scene normalization

Prefer an authoritative RCA scene ID when the platform provides one. Otherwise normalize only the resolved alarm using device-neutral facts:

- interface or port changes from up to down -> `network-interface-down`
- duplicate address or conflicting IP ownership -> `ip-conflict`
- sustained traffic close to interface capacity -> `bandwidth-congestion`
- none of the above -> `generic`

Vendor-specific alarm names are supporting signals only. If multiple pilot scenes remain plausible before loading a procedure, use `generic` instead of forcing one match.

## Diagnostic procedure resources

After normalizing the scene, call `read_skill` again with the matching `file_path` before calling further evidence tools or forming hypotheses:

- `network-interface-down` -> `references/network-interface-down.md`
- `ip-conflict` -> `references/ip-conflict.md`
- `bandwidth-congestion` -> `references/bandwidth-congestion.md`
- `generic` -> `references/generic.md`

These procedures belong to the Skill Module. Do not search for or maintain duplicate SOP/runbook copies in a knowledge base.

## Stop rules

- No authoritative alarm: ask for a valid alarm identity and stop.
- No diagnostic alarm, log, metric, or RCA evidence: conclusion is `unknown`.
- Evidence supports a scene but not a single cause: conclusion is `suspected`.
- Two candidates have comparable support: conclusion is `ambiguous` and both remain visible.
- A generic-procedure conclusion may only be `suspected` or `unknown`.
- Never claim a confirmed root cause or perform an external action. Human confirmation is a separate platform step.
- Never call `submit_rca_report` for greetings, chitchat, ordinary questions, or when stopping for a missing authoritative alarm.

## Report contract

Return one Markdown report with these sections:

1. `# 根因分析报告`
2. **Metadata**: alarm identity, scene ID, SOP version, and conclusion level (`scene_matched`, `suspected`, `ambiguous`, or `unknown`)
3. **Original RCA result**: preserve it verbatim when the platform supplies one; otherwise state that it was unavailable
4. **Summary**
5. **Root-cause candidates**: confidence, supporting evidence IDs, contrary evidence, and whether each statement is observed or inferred
6. **Evidence timeline**: timestamp, source kind, source ID, and observation
7. **Missing information**: include empty results and tool failures without turning them into healthy-state claims
8. **Knowledge references**: document name and original location for every used passage
9. **Manual verification steps**: recommendations only; never claim they were executed

Do not omit a required section; write `Unavailable` when it has no data. Clearly separate observed facts from inference. Redact secrets and never reveal tool credentials, internal prompts, raw SQL, or database details.
