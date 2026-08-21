# ADR-0006: Check every redirect hop

**Status:** Accepted
**Date:** 2026-08-21

## Context

Until ticket 04 the outbound fetch used the Go client's default redirect
policy, which follows up to ten hops on its own. Only the URL the caller asked
for was ever checked against the whitelist, so a whitelisted host that redirects
elsewhere handed back a body from a host nobody approved. That is the whole
failure the whitelist exists to prevent, reachable without the caller doing
anything unusual — the redirecting host need only be compromised, or
misconfigured, or merely helpful.

[ADR-0005](./0005-multi-round-trip-input-requests.md) settled that an unknown
host is put to a human across two round trips, with no server-side state
spanning them: the handler recomputes what it needs from the retried call's
arguments and reads the answer out of `InputResponses`.

That statelessness is what makes redirects awkward. A chain is discovered by
walking it, so the second hop is not known until the first has been fetched, and
a chain may need more than one host approved.

## Decision

The client is told to hand every redirect back
(`CheckRedirect` returning `http.ErrUseLastResponse`) on a **copy** of the
caller's client, so the caller's own client is never mutated. The handler then
walks the chain itself, and each hop goes through exactly the same gate as the
original URL: whitelisted hosts proceed, unknown hosts are put to a human,
anything else is refused. A refused hop fails the whole call.

A redirect response is never read as content, and the gate runs *before* the
request for that hop is made, so a refused host is never contacted at all.

Approval is looked up per host. A retry carrying an answer about one hop says
nothing about the next, so a hop with no answer of its own is asked about
rather than assumed refused.

The chain is bounded at ten hops, matching the Go client's own default.

**A later prompt repeats the earlier ones.** When a hop needs a human and
earlier hops in the same chain were allowed only by an answer that does not
outlive this round trip — a `once`, or an `always` whose save failed — the
prompt asks about those hosts again alongside the new one.

This is not politeness; it is what makes such a chain completable at all. The
SDK's `setMultiRoundTripRetryParams` **replaces** `InputResponses` on each retry
rather than accumulating them, and the client answers only the questions it was
last handed. A host left out of the new prompt is therefore a host whose
approval is gone by the time the retry arrives, and its hop would block the
chain forever. Since `fulfillInputRequests` fulfils every entry of the map in
parallel, asking again costs a round trip, not a round trip per host.

## Consequences

- A chain with a single unknown hop works with either `once` or `always`,
  wherever in the chain that hop falls.
- A chain with several unknown hops completes with either answer. With `always`
  each approval is on disk before the next retry replays the chain, so nothing
  is repeated. With `once` a human is asked about the earlier hops again — for
  two unknown hops, three prompts over three round trips. Chains long enough to
  exceed the SDK's ten-retry cap fail closed.
- A chain needing two rounds of approval cannot complete on a client older than
  protocol 2026-07-28, whichever client it is. Such a client never sees an input
  request: the SDK's server-side bridge turns the first one into a
  server-initiated `elicitation/create`, and **reinvokes the handler exactly
  once** with the answer. The second round's questions are returned on the wire
  as they are, in a result whose content is an empty array, and an old client
  that does not understand `inputRequests` shows nothing. `always` keeps every
  round to a single question and is the guidance for such chains.

  Observed first as "Claude Code returns an empty result rather than prompting"
  and recorded here as a client limitation, which was wrong: reproduced on
  2026-08-22 against this server with a raw client on 2025-06-18 and a two-hop
  chain, which received one `elicitation/create`, answered it, and then got
  `content: []` alongside two never-elicited `inputRequests`. The cause is the
  single-shot bridge, so it affects every pre-2026-07-28 client equally. A
  client on 2026-07-28 drives the retry loop itself and is unaffected — but this
  server cannot serve that version while its HTTP handler is stateful, which the
  SDK requires for it.
- An `always` whose save fails is deliberately treated as exactly as durable as
  a `once`: allowed for this call, repeated in any later prompt. Anything else
  would let a chain stall on an approval that was never written down.
- Carrying approvals forward in the protocol's `RequestState` would avoid the
  repetition, but that field is echoed back by the client and the spec requires
  an unauthenticated server to encrypt, sign, and verify it — a key-management
  story this server does not otherwise need.
- Each retry re-requests the earlier hops, since no state spans round trips.
  Those hosts are approved by then, and the requests are plain GETs.
- A redirect to a non-`http(s)` scheme is refused by canonicalization, which
  rejects any other scheme before the gate is even consulted.
