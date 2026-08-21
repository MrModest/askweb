# askweb — whitelisted `web_fetch` MCP tool

Status: ready-for-agent

## Problem

Hermes's built-in `web` toolset has no domain restriction and no approval hook,
so it was disabled to remove prompt-injection risk. The agent still needs
controlled web access: a static hostname whitelist, plus the ability for a human
to approve one-off exceptions from Telegram with real button taps.

## Decisions already made

These are settled. Read them before building; do not relitigate them here.

- [ADR-0001](../../docs/adr/0001-elicitation-gated-approval.md) — approval is
  gated on MCP elicitation, the model must never be able to influence it, and
  every non-approval path fails closed.
- [ADR-0002](../../docs/adr/0002-exact-hostname-matching.md) — exact canonical
  hostname matching, punycode-normalized, no subdomain implication.
- [ADR-0003](../../docs/adr/0003-official-go-mcp-sdk.md) — official Go MCP SDK.
- [ADR-0004](../../docs/adr/0004-streamable-http-transport.md) — Streamable HTTP
  at `/mcp`, configurable listen address.

## What to build

A single Go binary serving one MCP tool, `web_fetch`, which takes a URL and
returns the fetched body.

Four units with distinct responsibilities:

**Host normalizer.** Pure function: URL string in, canonical hostname or error
out. Behaviour specified in ADR-0002. No dependencies.

**Whitelist store.** In-memory set guarded by a mutex, backed by a flat JSON
file holding an array of canonical hostnames. Loaded once at startup. Adding a
host mutates the set and flushes the file, so approvals survive restart. Exposes
only "is this host allowed" and "add this host". Performs no normalization —
callers pass the normalizer's output.

**Approval gate.** Wraps the elicitation round trip. Given a session and a host,
returns one of three outcomes: allow once, allow always, deny. It elicits with a
flat single-field enum offering those three choices, and names the raw hostname
in the message.

**Tool handler.** Orchestrates the other three and performs the HTTP fetch.

## Behaviour

For a `web_fetch` call:

1. Normalize the URL to a canonical host. Parse failure returns an error result
   and fetches nothing.
2. If the host is on the whitelist, fetch and return the body.
3. If it is not, check whether the client declared the elicitation capability.
   If it did not, deny — never fetch an unknown host silently just because the
   client cannot ask.
4. Otherwise elicit.
5. Accepted as "once" — fetch, persist nothing.
6. Accepted as "always" — add the host to the store, then fetch.
7. Anything else — deny, fetch nothing.

## Error handling

Fail closed. Any path that is not an explicit approval is a denial.

- A denial returns a tool error naming only the blocked host, not raw network
  errors or internal detail.
- An elicitation transport error is a denial. Do not retry.
- A whitelist file write failure is logged. The current call still proceeds, as
  the human did approve it, but the host is not treated as persistently allowed
  and the server does not crash.
- Fetch failures return the underlying status or transport error to the caller.
  These are not security-relevant.

## Configuration

- Listen address: flag with env fallback, default port 8080.
- Whitelist file path: flag with env fallback, default to a file in the working
  directory.

## Acceptance

- A whitelisted host fetches without eliciting.
- An unknown host elicits.
- Approving "always" persists, and a second call to the same host does not
  elicit again — including after a restart.
- Approving "once" fetches but does not persist.
- Deny, cancel, and elicitation failure each fetch nothing.
- A client that declares no elicitation capability gets a denial for an unknown
  host.
- Hostname matching rejects the substring, suffix, and homograph cases in
  ADR-0002.

## Testing

Test-first, starting with the normalizer and the store — exact-match versus
substring versus IDN normalization is precisely the class of bug that must be
pinned by tests before implementation.

Integration coverage uses the SDK's in-memory transports with a scripted
elicitation handler on the client side, so the full round trip is exercised
in-process. Fetches in tests hit local test servers, never the real network.

## Out of scope

- `web_search` — a second tool with its own search-API and result-whitelisting
  questions. Only after the core loop is proven.
- Containerization and the Ansible role wiring back into the `homeserver` repo.
- Response size limits and content-type filtering.

Redirect handling was originally deferred here. A fetch that follows a redirect
to a non-whitelisted host bypasses the gate, which is a real gap rather than an
acceptable omission, so it is now filed as ticket 04 rather than left unscoped.
It is sequenced after the approval loop exists, not dropped.

## Known unknowns

Not resolvable without a live Hermes deployment. Test empirically once the
prototype runs; do not re-research from scratch.

- Hermes's elicitation timeout and decline defaults are undocumented.
- Whether Hermes's YOLO mode (`approvals.mode: off`) also bypasses elicitation
  is undocumented.
- Whether Hermes's Telegram elicitation buttons share the "Approve
  session/Always button no-op" bug reported against its separate
  dangerous-command approval subsystem — different subsystem, same rendering
  machinery.

Claude Code CLI 2.1.76+ supports elicitation and is a valid local test client
for the wire protocol, but cannot answer any of the three questions above.

### Resolved empirically — 2026-08-21, Claude Code CLI

Observed against the ticket-02 build, so these need no further research.

- **A server may not initiate elicitation while serving a request.** Protocol
  version 2026-07-28 rejects it outright; the interaction has to be returned
  from the call as an `InputRequests` map (SEP-2322). See
  [ADR-0005](../../docs/adr/0005-multi-round-trip-input-requests.md).
- **The flat enum renders as two levels, not three buttons.** Claude Code shows
  a collapsed `decision` field that expands (`→`) into radio options
  `once`/`always`/`deny`, with a separate `Accept`/`Decline` layer above it.
  ADR-0001 assumed a single flat prompt. Behaviour is unaffected, but *deny* is
  reachable two ways — choosing `deny` then Accept, or Decline — and this is the
  shape Hermes's Telegram buttons have to express.
- **The required field is enforced client-side.** Accept is refused while
  `decision` is unset, so an accepted-but-empty answer never reaches the server
  from this client. The server still denies that case; a button UI may not
  enforce it the same way.
- **Declining denies and fetches nothing**, as designed.

### Resolved empirically — 2026-08-22, Claude Code CLI

Observed against the ticket-04 build. The redirect gate itself behaves as
specified against a real client and real redirects: a whitelisted host that
redirects to an unapproved one is caught and refused naming only the redirect
target, and a redirect to a whitelisted host is followed with no second prompt.

- **This client does not surface a round carrying more than one input request.**
  A chain with two unknown hops makes the server return two questions in one
  round — the repeat that keeps the earlier approval alive
  ([ADR-0006](../../docs/adr/0006-per-hop-redirect-gating.md)). Claude Code
  returns an empty result instead of prompting: no content, no error, no
  questions shown. Verified with curl that the server's response is well-formed
  and that answering both questions completes the chain correctly, so this is a
  client limitation rather than a server defect. The Go SDK's client middleware
  fulfils every entry of the map in parallel; this client is a separate
  implementation.
- **Consequence: prefer `always` for chains that cross more than one unknown
  host.** An `always` is on disk by the retry, so the earlier hop is whitelisted
  rather than repeated and every round carries exactly one question. `once`
  works for any chain with a single unknown hop, wherever it falls.
- **Worth re-testing on Hermes.** If its client shares this limitation, the
  Telegram button UI needs a way to express several pending questions at once,
  or multi-hop chains are `always`-only there too.
