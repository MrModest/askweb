# ADR-0004: Streamable HTTP transport

**Status:** Accepted
**Date:** 2026-08-01

## Context

MCP servers can be reached over stdio or over HTTP. Stdio is simpler for local
prototyping. HTTP is required for the eventual deployment: the server will run
as its own container on `notes-net`, alongside the existing `hatchdoor` MCP
server, which already follows an HTTP + healthcheck + compose pattern.

## Decision

Serve Streamable HTTP at `/mcp` from the start, rather than prototyping on
stdio and porting later.

The listen address is configurable — `--addr` flag, `ASKWEB_ADDR` env fallback,
default `:8080`. Nothing about the address is hardcoded, so containerization
requires no code change.

## Consequences

- No transport rewrite between prototype and deployment.
- Slightly more setup than stdio during early development.
- Local testing works against Claude Code CLI via
  `claude mcp add --transport http askweb http://localhost:8080/mcp`.
  Restarting the binary drops the MCP session and requires a reconnect.
