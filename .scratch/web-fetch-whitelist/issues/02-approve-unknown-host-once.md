# 02 — Approve an unknown host for one call

**What to build:** Asking for a host that is not on the whitelist now prompts a
human instead of being refused outright. The prompt names the host and offers
three choices: allow once, allow always, deny. Choosing "once" fetches the page
and remembers nothing.

Every other outcome refuses and fetches nothing — declining, cancelling, letting
it time out, a transport failure, or a response that doesn't match any of the
three choices. Persisting an "always" answer is ticket 03; for now it behaves
as "once".

The human's answer is the only thing that can grant access. The model asking for
the fetch must have no way to influence, observe, or supply that answer — see
ADR-0001. In particular the tool must not gain a parameter that lets the caller
assert its own approval.

If the connected client never declared that it can prompt a human, an unknown
host is refused rather than fetched.

**Blocked by:** 01.

**Status:** ready-for-agent

- [ ] A non-whitelisted host triggers a prompt naming that host and offering exactly the three choices
- [ ] Choosing "allow once" fetches the page and records nothing
- [ ] Choosing "allow always" fetches the page; persistence is out of scope here
- [ ] Choosing "deny" performs no fetch
- [ ] Cancelling or letting the prompt expire performs no fetch
- [ ] A transport failure while prompting performs no fetch and is not retried
- [ ] A response that matches none of the three choices performs no fetch
- [ ] A client that did not declare the prompting capability gets a refusal, never a silent fetch
- [ ] A whitelisted host still fetches without prompting
- [ ] The tool's parameters give the caller no way to approve its own request
- [ ] The full prompt-and-answer round trip is covered by an in-process integration test with a scripted answer, exercising each outcome above
