# ADR-0003: Use the official Go MCP SDK

**Status:** Accepted (elicitation mechanism superseded by [ADR-0005](./0005-multi-round-trip-input-requests.md))
**Date:** 2026-08-01

## Context

The server needs two capabilities from its SDK: an HTTP-based server transport,
and the ability for the server to initiate `elicitation/create` toward the
client (see [ADR-0001](./0001-elicitation-gated-approval.md)).

Two Go SDKs were candidates:

- `github.com/modelcontextprotocol/go-sdk` — official, maintained with Google
- `github.com/mark3labs/mcp-go` — community, historically the one with HTTP
  transports

Some sources report the official SDK as stdio-and-command only, which would
have forced the community fork.

## Decision

Use `github.com/modelcontextprotocol/go-sdk`.

Verified against its current docs on 2026-08-01: it provides both
`StreamableHTTPHandler` and server-initiated elicitation via
`ServerSession.Elicit`, so the reported HTTP limitation is stale. The community
fork is not needed.

## Consequences

- Spec compliance tracks upstream directly, with no fork lag.
- `mcp.NewInMemoryTransports` is available, so the elicitation round trip can be
  integration-tested in-process with a scripted client-side handler, without
  network or a live Hermes.
- The claim that the official SDK lacks HTTP transport will keep resurfacing in
  search results and training data. It was wrong as of this date; re-verify
  against the docs rather than secondary sources.
