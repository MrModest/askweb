# ADR-0001: Elicitation-gated approval for non-whitelisted hosts

**Status:** Accepted (mechanism superseded by [ADR-0005](./0005-multi-round-trip-input-requests.md); the reading of a malformed answer narrowed by [ADR-0008](./0008-accept-is-the-approval.md))
**Date:** 2026-08-01

## Context

Hermes's built-in `web` toolset has no domain restriction and no approval hook,
so it was disabled to remove prompt-injection risk. Controlled web access is
still wanted: a static domain whitelist, plus a way for a human to approve
one-off exceptions from Telegram with real button taps.

Third-party wrapper and gateway tools were evaluated for this — MCP Guardian,
AgentLock, lasso-security/mcp-gateway. The upstream
`modelcontextprotocol/servers` fetch server was also considered, but it has no
domain allowlist (unresolved upstream issue).

## Decision

Write a plain, spec-compliant MCP server and use MCP's native
`elicitation/create` for approval. No third-party wrapper or gateway.

When a requested host is not on the whitelist, the server elicits with a flat
enum schema (`once` / `always` / `deny`). The client renders it as an approval
prompt — Telegram buttons under Hermes, a dialog under Claude Code CLI.

**The model must never be able to influence the approval decision.** The MCP
client-server RPC design keeps the model out of the elicitation response loop:
it cannot see or fabricate the human's answer. That guarantee only holds if no
model-controlled input reaches the decision. Therefore:

- The whitelist write is gated strictly on an elicitation response of
  `action == "accept"` with enum `"always"`.
- The `web_fetch` tool schema must **not** carry an `always_allow` parameter or
  any equivalent. Such a parameter would let the model self-approve and defeat
  the entire design.

Every path that is not an explicit approval is a denial — `decline`, `cancel`,
timeout, transport error, malformed response, unrecognized enum value. Fail
closed. ([ADR-0008](./0008-accept-is-the-approval.md) later narrows one case: an
`accept` that carries no choice at all is an approval of that single call, since
the action is itself the human's answer.)

Elicitation is an optional client capability. If the connected client does not
declare it, unknown hosts are denied rather than silently fetched.

## Consequences

- No dependency on any third-party security wrapper.
- The server is client-agnostic and portable to any elicitation-capable client.
  Only deployment config (`mcp_servers.<name>.elicitation.*` in Hermes's
  `config.yaml`) is Hermes-specific.
- Approval requires a live human in the loop; unattended runs against unknown
  hosts will block and then deny on timeout. This is intended.
- Anyone adding parameters to `web_fetch` must re-check the invariant above.
  It is the single most load-bearing property of this design.
