# askweb

Whitelisted web-access MCP server. Exposes a `web_fetch` tool gated by an
exact-hostname whitelist. An unknown host is put to a human for approval, as a
multi-round-trip input request (see `docs/adr/0005-multi-round-trip-input-requests.md`).

## Agent skills

### Issue tracker

Issues live as markdown files under `.scratch/<feature-slug>/`. No remote
tracker; PRs are not a triage surface. See `docs/agents/issue-tracker.md`.

### Triage labels

Canonical defaults, unchanged. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See
`docs/agents/domain.md`.
