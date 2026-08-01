# askweb — Whitelisted Web-Access MCP Server

**Date:** 2026-08-01
**Status:** Approved design, not yet implemented

## Problem

Hermes's built-in `web` toolset has no domain restriction and no approval hook,
so it was disabled to remove prompt-injection risk. The agent still needs
controlled web access: a static domain whitelist, plus the ability to approve
one-off exceptions from Telegram with real human button taps.

## Approach

A standalone, spec-compliant MCP server exposing a single `web_fetch` tool.
Unknown hostnames trigger MCP's native `elicitation/create`, which the client
(Hermes, Claude Code) renders as an approval prompt. No third-party gateway or
wrapper is involved.

The server is deliberately client-agnostic. Only deployment configuration
(`mcp_servers.<name>.elicitation.*` in Hermes's `config.yaml`) is
Hermes-specific; the server code is portable to any elicitation-capable client.

### Rejected alternatives

MCP Guardian, AgentLock, and lasso-security/mcp-gateway were evaluated and
rejected as unnecessary wrappers. The upstream `modelcontextprotocol/servers`
fetch server has no domain allowlist (unresolved upstream issue), so it cannot
be used as-is.

## Technology

- **Language:** Go 1.26
- **SDK:** `github.com/modelcontextprotocol/go-sdk` — the official SDK,
  maintained with Google. Verified 2026-08-01 to provide both
  `StreamableHTTPHandler` and server-initiated elicitation
  (`ServerSession.Elicit`), so the community `mark3labs/mcp-go` fork is not
  needed.
- **Transport:** Streamable HTTP, served at `/mcp`. Chosen over stdio to match
  the existing `hatchdoor` sibling MCP server's deployment pattern and to allow
  running as its own container on `notes-net` without a transport rewrite.
- **IDN normalization:** `golang.org/x/net/idna`

## Architecture

Single binary, four internal units with distinct responsibilities:

### 1. Host normalizer

Pure function: URL string → canonical hostname, or error.

Parses the URL, extracts the host, lowercases it, and converts to punycode via
`idna`. Rejects URLs with no host, and non-`http`/`https` schemes.

This unit exists separately because hostname canonicalization is the security
boundary. It has no dependencies and is exhaustively unit-testable.

### 2. Whitelist store

In-memory `map[string]struct{}` guarded by a mutex, backed by a flat JSON file
(array of canonical hostnames). Loaded once at startup. `Add` mutates the map
and flushes the file.

Interface: `Allowed(host string) bool`, `Add(host string) error`.

**Matching is exact only.** The store compares full canonical hostnames. It
never does substring, prefix, or suffix matching — `evil-example.com` must not
match `example.com`. Hosts are stored already-canonical, so no normalization
happens inside the store; callers pass output of the normalizer.

### 3. Approval gate

Wraps the elicitation round-trip. Given a session and a host, returns one of
`allowOnce`, `allowAlways`, `deny`.

Sends `elicitation/create` with a flat enum schema:

```
{ "action": { "type": "string", "enum": ["once", "always", "deny"] } }
```

The message names the raw hostname being requested.

### 4. Tool handler

Orchestrates the above and performs the HTTP fetch.

## Data flow — `web_fetch(url)`

1. Normalize URL to canonical host. Parse failure → error result, no fetch.
2. `store.Allowed(host)` → fetch, return body.
3. Not allowed → check the negotiated client capabilities for `elicitation`.
   Absent → **deny**, no fetch. (Elicitation is an optional client capability;
   an unknown host must never be fetched silently just because the client can't
   ask.)
4. Present → approval gate elicits.
5. `accept` + `once` → fetch. Do not persist.
6. `accept` + `always` → `store.Add(host)`, then fetch.
7. Anything else — `decline`, `cancel`, timeout, transport error, malformed
   response, unrecognized enum value → **deny**, no fetch.

### Critical invariant

The whitelist write is gated *strictly* behind an elicitation response of
`action == "accept"` with enum `"always"`. It is never gated behind any
tool-call argument.

The MCP client-server RPC design keeps the model out of the elicitation
response loop — it cannot see or fabricate the human's answer. That guarantee
only holds if no model-controlled input can reach the approval decision.
Concretely: **do not add an `always_allow` parameter (or any equivalent) to the
`web_fetch` tool schema.** That would let the model self-approve and defeat the
entire design.

## Error handling

Fail-closed everywhere. Any path that is not an explicit approval is a denial.

- Denial returns a tool error result naming only the blocked host — not raw
  network errors or internal detail.
- `Elicit` call errors are treated as denial, not retried.
- Whitelist file write failure is logged; the current call still proceeds as if
  `once` was granted (the human did approve it), but the server does not crash
  and the host is not treated as persistently allowed.
- Fetch failures return the HTTP status / transport error to the caller. These
  are not security-relevant and need no special handling.

## Configuration

- `--addr` flag (env fallback `ASKWEB_ADDR`), default `:8080`. Not hardcoded, so
  containerization later requires no code change.
- `--whitelist` flag (env fallback `ASKWEB_WHITELIST`), default
  `./whitelist.json`.

## Testing

Test-first, starting with the normalizer and store — exact-match versus
substring versus IDN normalization is precisely the class of bug that must be
pinned by tests before implementation.

**Unit — host normalizer:**
scheme rejection, missing host, port stripping, case folding, IDN → punycode,
homograph inputs that must not collapse into an existing whitelist entry.

**Unit — whitelist store:**
exact match only (`evil-example.com` vs `example.com`, `example.com.evil.com`,
`sub.example.com`), load from file, add-and-flush round trip, concurrent access.

**Unit — approval gate:**
each response shape maps to the right decision; every non-`accept`/non-`always`
shape denies.

**Integration:**
`mcp.NewInMemoryTransports` with a scripted elicitation handler on the client
side. Covers: whitelisted host fetches without eliciting; unknown host elicits;
`always` persists and a second call does not re-elicit; `deny` and `cancel`
produce no fetch; a client declaring no elicitation capability gets a denial.

Fetches in tests hit `httptest` servers, never the real network.

## Out of scope

Deliberately excluded from this prototype:

- `web_search` — a second tool with its own search-API and result-whitelisting
  questions. Add only after the core loop is proven.
- Containerization and the Ansible role wiring back into `homeserver`.
  Downstream work in that repo, not here.
- Response size limits, content-type filtering, redirect-chain re-validation.
  Redirects following to a non-whitelisted host is a real gap and should be
  revisited, but the first slice proves the approval loop.

## Known unknowns

Not resolvable without a live Hermes deployment; test empirically once the
prototype runs. Do not re-research from scratch.

- Hermes's elicitation timeout and decline defaults are undocumented.
- Whether Hermes's `approvals.mode: off` (YOLO mode) also bypasses elicitation
  is undocumented.
- Whether Hermes's Telegram elicitation buttons share the "Approve
  session/Always button no-op" bug reported against its *separate*
  dangerous-command approval subsystem — different subsystem, same rendering
  machinery.

Claude Code CLI (2.1.76+) supports elicitation and is a valid local test client
for the wire protocol, but cannot answer any of the three questions above.
