# ADR-0008: Accepting the prompt is the approval

**Status:** Accepted
**Date:** 2026-08-22

## Context

[ADR-0001](./0001-elicitation-gated-approval.md) settled that a human decides,
and that every path which is not an explicit approval is a denial — naming a
malformed response among them. [ADR-0005](./0005-multi-round-trip-input-requests.md)
moved the question onto a multi-round-trip input request without changing that.

Both assumed a client that fills in the form it was handed. Not every client
does.

Hermes — the deployment askweb was written for — routes MCP form elicitation
through the approval surface it already had for dangerous shell commands: a
message and approve/refuse buttons on Telegram. Its handler reads the requested
schema only to render a human-readable summary of the fields, and on approval
answers `{"action": "accept", "content": {}}` for every schema it is ever sent.
It has no path by which a field value could come back: its buttons resolve to a
fixed vocabulary shared with the command guard, and the server's identity is
gone by the time the answer is normalized to accept / decline / cancel. This is
upstream bug [NousResearch/hermes-agent#58778](https://github.com/NousResearch/hermes-agent/issues/58778),
open since July 2026, whose fix has been stalled in review since.

Against askweb's schema, which required `decision`, that answer was rejected by
the SDK before the tool handler ran, and the whole `tools/call` failed:

```
multi-round-trip: fulfilling input request "askweb-host-approval:github.com":
elicitation result content does not match requested schema:
validating root: required: missing properties: ["decision"]
```

So a human who tapped **Allow Once** on their phone got a protocol error, and
nothing was fetched. The prompt worked; reading the answer did not.

Dropping `required` alone does not fix it — the answer then validates but still
carries no choice, and ADR-0001's reading turns that into a denial. The question
is what an accept without a choice means.

## Decision

**The action is the approval. The choice only says how long it lasts.**

- `action: "accept"` allows the call. It is what a human acted on, and the
  message they acted on named this host and asked whether it may be fetched.
- The `decision` field is offered as an enum but is **not required**.
- `always` — and only the literal word — also writes the host to the whitelist.
- A stated choice matching none of the ones offered is a denial, as before. Only
  an *absent* choice falls back, and it falls back to `once`: the least the
  accept could have meant, granting this call and nothing beyond it.
- Everything that was a denial for reasons other than schema shape stays one:
  decline, cancel, timeout, transport failure, an answer about another host, a
  response that is not an elicitation result.

A client that cannot collect a choice therefore cannot widen the whitelist. It
can approve one fetch at a time, and it will be asked again next time.

## Consequences

- ADR-0001's "malformed response ⇒ denial" line is **narrowed**: an accept whose
  content does not carry the choice is now an approval of this one call, not a
  refusal. Its substance is unchanged — a human still decides, the model still
  cannot influence the answer, and the whitelist still grows only on an explicit
  `always`.
- The invariant that carries the design is untouched and still the load-bearing
  one: `web_fetch` takes a `url` and nothing else, so the answer travels in
  protocol-level fields written by the client. What changed is only how much of
  that answer askweb insists on.
- This trusts the client to report its human faithfully. That was always true —
  a client that fabricates an `accept` defeats elicitation whatever schema is
  asked for — and no server-side check can authenticate a human through a client
  that misrepresents them. The threat model is a model manipulated by fetched
  content, not a hostile client.
- Under a consent-only client the whitelist can only grow by hand-editing the
  file. That is a real loss of convenience, kept deliberately rather than
  guessed around: a persistent grant needs a human who was actually offered it.
- Clients that do fill the form — Claude Code among them — are unaffected and
  keep all three choices.
- Should the enum ever be reordered or renamed, the fallback stays `once` by
  name, not by position. Nothing here may infer a choice from where it sits in
  the list.
