# 01 — Whitelisted fetch over HTTP MCP

**What to build:** A running MCP server that an elicitation-capable client can
connect to over Streamable HTTP and call a `web_fetch` tool on. Asking for a
host that is on the whitelist returns that page's body. Asking for any other
host is refused, and nothing is fetched.

Hostname matching is exact and canonical from the outset — a whitelist entry
grants access to that one host, never to a subdomain, a lookalike, or a name
that merely contains it. Loose matching here is the vulnerability this whole
server exists to prevent, so it is not deferred.

Approval for unknown hosts arrives in ticket 02. Until then, refusal is the
correct and safe behaviour, which is what makes this ticket shippable alone.

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [ ] The binary starts and serves MCP over Streamable HTTP; a client can connect and see `web_fetch` in its tool list
- [ ] Listen address and whitelist file location are both configurable by flag with an environment-variable fallback, and neither is hardcoded
- [ ] The whitelist is read at startup from a flat JSON file of hostnames
- [ ] Requesting a whitelisted host returns the fetched body
- [ ] Requesting any non-whitelisted host is refused and performs no fetch; the refusal names only the blocked host
- [ ] Canonicalization lowercases the host, converts it to punycode, strips the port, rejects URLs with no host, and rejects any scheme other than HTTP and HTTPS
- [ ] Matching is exact: a name that is a subdomain of, a suffix of, a prefix of, or a homograph of a whitelisted entry is refused
- [ ] Canonicalization behaviour is pinned by unit tests written before its implementation
- [ ] Whitelist lookup behaviour is pinned by unit tests covering the rejection cases above
- [ ] No test reaches the real network
