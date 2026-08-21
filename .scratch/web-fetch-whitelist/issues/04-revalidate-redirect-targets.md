# 04 — Re-validate redirect targets against the whitelist

**What to build:** Close the gap where an approved host redirects somewhere that
was never approved. Today the fetch would follow that redirect and return
content from a host the whitelist does not cover, which defeats the gate.

Every hop of a redirect chain is checked the same way the original request is.
A hop to a whitelisted host follows normally. A hop to an unknown host prompts
the human, exactly as an unknown initial request does. Refusing a hop means the
whole request returns nothing — no partial content, no body from the
unapproved host.

The usual fail-closed rule applies: if a human cannot be prompted, an unknown
hop is refused rather than followed.

**Blocked by:** 02.

**Status:** ready-for-agent

- [x] A redirect to a whitelisted host is followed and its body returned
- [x] A redirect to a non-whitelisted host prompts the human before being followed
- [x] Approving the hop follows it and returns the body; "always" persists the redirect target, not the original host
- [x] Refusing the hop returns no body and no content from the unapproved host
- [x] Every hop in a multi-hop chain is checked, not just the first
- [x] An unknown hop is refused when the client cannot prompt a human
- [x] There is a bound on how many hops are followed
- [x] Redirect behaviour is covered by tests using local test servers that issue real redirects

## Comments

Implemented. Every hop goes through the same gate as the original URL, and the
gate runs before each hop is requested, so a refused host is never contacted.
Recorded as [ADR-0006](../../../docs/adr/0006-per-hop-redirect-gating.md).

A chain needing more than one host approved works by repeating the earlier
questions in each later prompt. The client replaces `InputResponses` on every
retry rather than accumulating them, so a host left out of a prompt has its
approval dropped by the time the retry arrives; asking again is what keeps it.
Two unknown hops costs three prompts over three round trips. Answering `always`
avoids the repetition, since the host is whitelisted by the next round.

An `always` whose save fails is treated as exactly as durable as a `once` —
allowed for this call, repeated in any later prompt — so a chain cannot stall on
an approval that was never written down.

A first draft refused these chains outright instead, on the grounds that the SDK
made a second question impossible. That was wrong: `fulfillInputRequests`
fulfils every entry of the returned map in parallel, so the server chooses what
to ask about, and repeating a question costs one round trip rather than one per
host. Caught in review.
