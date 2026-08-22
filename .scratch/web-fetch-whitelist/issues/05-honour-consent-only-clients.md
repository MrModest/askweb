# 05 — Honour clients that answer with the action alone

**What to build:** An approval prompt answered by a client that cannot fill in
the choice must still work. Some clients render the prompt as a plain
approve/refuse — a pair of buttons on a chat platform — and answer with
`{"action": "accept", "content": {}}`, no `decision` field, whatever schema they
were sent.

Today that answer never reaches the handler: the requested schema marks
`decision` required, so the SDK rejects it and the whole `tools/call` fails with
a protocol error. A human taps **Allow Once** on their phone and gets:

```
multi-round-trip: fulfilling input request "askweb-host-approval:github.com":
elicitation result content does not match requested schema:
validating root: required: missing properties: ["decision"]
```

Reported against Hermes v0.18.2 over Telegram. The client-side bug is
[NousResearch/hermes-agent#58778](https://github.com/NousResearch/hermes-agent/issues/58778),
open with its fix stalled in review — so askweb has to read what such clients
actually send.

The rule: accepting is the approval, and the choice only says how long it lasts.
An accept with no choice grants this one call and nothing more. Only the literal
`always` widens the whitelist, so a client that cannot collect a choice cannot
widen it either. Every other refusal stays a refusal — see ADR-0008.

**Status:** ready-for-agent

- [x] The requested schema still offers once/always/deny, and no longer requires it
- [x] An accept carrying no choice fetches the page
- [x] An accept carrying no choice leaves the whitelist file untouched, and the
      next call for that host asks again
- [x] A decline from the same client fetches nothing
- [x] A stated choice matching none of the three is still a denial
- [x] once / always / deny from a client that does fill the form are unchanged
- [x] The regression is covered end to end, and fails with the reported protocol
      error if `required` comes back

## Comments

**2026-08-22** — Implemented on `claude/mcp-askweb-approval-schema-clv6jp`.
`approval.Request` drops `Required`; `approval.Decide` reads an absent choice as
`once` and keeps every stated one strict. Recorded as ADR-0008, which narrows
ADR-0001's "malformed response is a denial" line and says why. Two unit cases
that asserted the old reading moved from the deny table to the new one.
