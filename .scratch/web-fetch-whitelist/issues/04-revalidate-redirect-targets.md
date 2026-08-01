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

- [ ] A redirect to a whitelisted host is followed and its body returned
- [ ] A redirect to a non-whitelisted host prompts the human before being followed
- [ ] Approving the hop follows it and returns the body; "always" persists the redirect target, not the original host
- [ ] Refusing the hop returns no body and no content from the unapproved host
- [ ] Every hop in a multi-hop chain is checked, not just the first
- [ ] An unknown hop is refused when the client cannot prompt a human
- [ ] There is a bound on how many hops are followed
- [ ] Redirect behaviour is covered by tests using local test servers that issue real redirects
