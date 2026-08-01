# ADR-0002: Exact hostname matching with punycode normalization

**Status:** Accepted
**Date:** 2026-08-01

## Context

The whitelist is the security boundary of this server (see [ADR-0001](./0001-elicitation-gated-approval.md)).
Loose matching would silently widen it. Two classic failure modes apply:

- Substring or suffix matching lets `evil-example.com` or `example.com.evil.com`
  satisfy an `example.com` entry.
- Internationalized domain names allow homographs that look identical to a
  whitelisted host but are distinct strings.

## Decision

Match full canonical hostnames, exactly. Never substring, prefix, or suffix.

Canonicalization happens in one place — a pure function that takes a URL string
and returns a canonical hostname or an error. It parses the URL, extracts the
host, lowercases it, and converts to punycode. It rejects URLs with no host and
any scheme other than `http`/`https`.

Whitelist entries are stored already-canonical, so the store performs no
normalization of its own; callers pass the normalizer's output.

Subdomains are *not* implied by a parent entry. `sub.example.com` requires its
own approval.

## Consequences

- Approving a site does not approve its subdomains, so CDN-hosted assets on a
  different host will each need approval. Accepted — the alternative is
  wildcard matching, which reintroduces the widening problem.
- Canonicalization is isolated and has no dependencies, so it is exhaustively
  unit-testable. It should be tested first, before implementation of anything
  that consumes it.
- Adds a dependency on `golang.org/x/net/idna`.
