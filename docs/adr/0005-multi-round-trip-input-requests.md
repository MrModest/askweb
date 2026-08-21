# ADR-0005: Carry approval as a multi-round-trip input request

**Status:** Accepted
**Date:** 2026-08-21

## Context

[ADR-0001](./0001-elicitation-gated-approval.md) settled that approval for an
unknown host is gated on MCP elicitation, and [ADR-0003](./0003-official-go-mcp-sdk.md)
recorded that the official Go SDK exposes this as `ServerSession.Elicit`.

Implementing ticket 02 against SDK v1.7.0 showed that shape no longer works.
A session negotiated at protocol version 2026-07-28 or later rejects it:

```
"elicitation/create" cannot be sent while serving a request on protocol
version 2026-07-28: return an InputRequests map instead
(multi round-trip requests, SEP-2322)
```

SEP-2322 forbids server-initiated JSON-RPC requests for elicitation, sampling,
and roots while the server is serving a request. The interaction must instead be
embedded in the result of the `tools/call` that needs it.

## Decision

The `web_fetch` handler returns an `InputRequests` map rather than calling
`ServerSession.Elicit`.

The call then takes two round trips: the first returns the prompt, the client
puts it to a human, and the client retries the same call with the answer in
`InputResponses`. The handler recomputes the host from its arguments on the
retry, so no server-side state spans the two trips.

The input request is keyed by the host it asks about
(`askweb-host-approval:<host>`), and the retry looks the answer up under that
same key. An approval is therefore bound to the question it answers: a reply
granted for one host is simply absent when the retry concerns another, and the
default denial applies. Without that binding, recomputing the host from the
retry's arguments would let an answer given for A authorize B.

This is the portable shape, not merely the new one: the SDK's
`serverMultiRoundTripMiddleware` fulfills input requests on behalf of clients
still on older protocol versions, so one implementation serves both.

ADR-0001's substance is unchanged and still binding — a human decides, every
non-approval is a denial, and `web_fetch` exposes no parameter through which a
caller could assert its own approval. Only the transport of the question changes.

## Consequences

- ADR-0001's and ADR-0003's references to `ServerSession.Elicit` are superseded
  as to *mechanism*. Their reasoning about who may decide still stands.
- The answer now travels back inside the retried `tools/call` params rather than
  as a distinct server-initiated response. It is still written by the client,
  not by the model: the model supplies only the tool's declared arguments, and
  `url` remains the only one. The ADR-0001 invariant therefore holds, but it now
  rests on that argument rather than on the model being absent from the message
  flow entirely — worth re-checking whenever the tool's schema changes.
- The SDK validates the human's answer against the requested schema before the
  handler sees it, so an answer outside the three choices surfaces as a protocol
  error rather than a tool error. Both refuse and neither fetches, so tests
  accept either shape.
- A client that declares no elicitation capability is checked explicitly and
  denied before any prompt is built, rather than discovering the problem when
  the round trip fails.
