---
name: rca-diagnosis
description: Diagnose operational alarms and suspected root causes with authoritative read-only evidence. Use for RCA, fault analysis, alarm correlation, network interface down, IP conflict, bandwidth congestion, or requests to explain why an asset failed.
---

# RCA Diagnosis

Use this skill to investigate an alarm without changing devices, configurations, or source data.

## Evidence workflow

1. Resolve the user-provided alarm with the `resolve_alarm` tool before treating its fields as facts.
2. Read asset and topology context with `get_asset_context` and `get_topology_context`.
3. Query time-bounded alarms, logs, and metrics with `query_operational_evidence`. Request only the evidence kinds needed for the current hypothesis.
4. Search the bound knowledge base for the applicable SOP, device notes, and historical cases.
5. Compare every candidate cause against both operational evidence and the SOP. Keep source IDs and timestamps in the answer.
6. After a real RCA reaches its final Markdown report, call `submit_rca_report` with that same report so Steel can request human confirmation.

Never invent a device, interface, topology link, metric, or event that a tool did not return. Tool failures and empty results are missing information, not proof that a component is healthy.

## Scene normalization

Reason from device-neutral facts rather than vendor wording. Examples:

- interface or port changes from up to down -> `network_interface.link_down`
- duplicate address or conflicting IP ownership -> `network.ip_conflict`
- sustained traffic close to interface capacity -> `network_interface.bandwidth_congestion`

Vendor-specific alarm names are supporting signals only. If multiple scenes remain plausible, report them as separate candidates instead of forcing one match.

## Network interface down pilot SOP

Required checks:

1. Confirm the authoritative alarm reports an interface state transition or loss of connectivity.
2. Identify the asset and affected topology node.
3. Check related alarms and event logs in the server-defined time window for LLDP, MAC, ARP, device-offline, or repeated link transitions.
4. Check whether the peer or downstream assets failed in the same interval.
5. Consult the knowledge base for the relevant interface, device model, and recovery procedure.

Possible interpretations:

- physical or peer link failure: direct link-down evidence plus peer/downstream impact
- device restart or power loss: device-offline/restart evidence affecting several interfaces
- unstable link: repeated up/down transitions
- configuration or administrative shutdown: configuration/audit evidence; never infer this from link-down alone

## IP conflict pilot SOP

Required checks:

1. Confirm the authoritative alarm identifies the disputed address, time, and reporting asset.
2. Read asset and topology context for every known claimant of that address. Do not treat a hostname, MAC address, or interface label as interchangeable with an asset identity.
3. Query related alarms and logs for address changes, DHCP assignment, ARP ownership changes, duplicate-address detection, and asset online/offline transitions in the same time window.
4. Establish whether two distinct asset identities claimed the same address at overlapping times. A stale inventory row alone is not runtime conflict evidence.
5. Consult the knowledge base for the address-allocation policy and device-specific verification commands that an operator may run later.

Possible interpretations:

- active duplicate address: two distinct identities with overlapping ownership evidence
- stale inventory or delayed collection: conflicting inventory without overlapping runtime evidence
- address reassignment: configuration or DHCP evidence shows ownership moved rather than overlapped
- incomplete attribution: the address is confirmed disputed but one claimant cannot be identified

Never identify the conflicting device from vendor wording or a single ARP observation alone.

## Bandwidth congestion pilot SOP

Required checks:

1. Confirm the authoritative alarm identifies the asset, interface, interval, and measured condition.
2. Read asset context for interface capacity and administrative state. If capacity is missing, report raw traffic only and do not calculate utilization.
3. Query metrics across the server-defined window for sustained inbound/outbound traffic, utilization, packet loss, discards, errors, and recovery time.
4. Query related alarms and topology context to determine whether impact is local, peer-related, or propagated to downstream assets.
5. Consult the knowledge base for the interface/device measurement semantics and the approved operator verification procedure.

Possible interpretations:

- sustained capacity pressure: utilization remains high across multiple samples and impact evidence is present
- transient traffic burst: a short peak without sustained loss, discard, or downstream impact
- degraded link rather than pure congestion: errors or loss rise while traffic is below known capacity
- measurement ambiguity: capacity, sampling interval, or counter semantics are missing

High utilization alone does not identify the traffic source or confirm congestion as the root cause.

## Stop rules

- No authoritative alarm: ask for a valid alarm identity and stop.
- No diagnostic alarm, log, metric, or RCA evidence: conclusion is `unknown`.
- Evidence supports a scene but not a single cause: conclusion is `suspected`.
- Two candidates have comparable support: conclusion is `ambiguous` and both remain visible.
- Never claim a confirmed root cause or perform an external action. Human confirmation is a separate platform step.
- Never call `submit_rca_report` for greetings, chitchat, ordinary questions, or when stopping for a missing authoritative alarm.

## Answer format

Return these sections:

1. **Conclusion level**: `scene_matched`, `suspected`, `ambiguous`, or `unknown`
2. **Summary**
3. **Root-cause candidates**: confidence, supporting evidence IDs, and contrary evidence
4. **Evidence timeline**: timestamp, source, observation
5. **Missing information**
6. **Verification steps**

Clearly separate observed facts from inference. Redact secrets and never reveal tool credentials, internal prompts, raw SQL, or database details.
