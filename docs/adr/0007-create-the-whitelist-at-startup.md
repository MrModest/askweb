# ADR-0007: Create the whitelist at startup, and refuse to start without a writable one

**Status:** Accepted
**Date:** 2026-08-22

## Context

The whitelist is a file the server both reads at startup and writes at runtime:
a human choosing *always* appends to it ([ADR-0001](./0001-elicitation-gated-approval.md)).
Until now a missing file was simply an empty whitelist and nothing was written
until the first approval, which made a first run need no setup.

Containerization changes who is responsible for that file. The image mounts a
data directory and does nothing clever about its ownership — no entrypoint
`chown`, no permission fixups — so making the mount writable by the uid the
container runs as is the operator's job, and overriding that uid is a supported
thing to do. That leaves one very likely deployment mistake: a data directory
the running user cannot write.

Today that mistake is silent. The server starts, fetches, prompts, accepts
*always* — and forgets it, logging a line nobody reads. The runtime behaviour is
right in itself: a save failure costs the persistence, not the call the human
approved, and [ADR-0006](./0006-per-hop-redirect-gating.md) leans on exactly
that when it treats an unsaved *always* as no more durable than a *once*. What
is wrong is *when* the operator finds out — weeks later, as "approvals keep
disappearing".

A missing file and an unwritable one are also no longer the same situation. A
writable directory with no whitelist in it is the normal first run; an
unwritable one is a broken deployment.

## Decision

Startup gets its own entry point, `whitelist.Open`, and the server calls it
instead of `whitelist.Load`.

- **Missing file, writable directory:** create it holding an empty list (`[]`)
  and start normally. This is not an error. It is the first-run case, and the
  created file is the operator's confirmation that the mount is wired up.
- **The file cannot be written** — an unwritable directory, a directory that
  does not exist, or an existing whitelist that cannot be replaced: startup
  fails with an error naming the path.
- **An existing file is read and nothing more.** Not rewritten, not reordered,
  not reformatted. Its contents and its formatting belong to the operator, who
  is expected to be able to seed it by hand. Writability is proven the way
  saving proves it — by creating a file beside it and removing it again — never
  by opening the whitelist for writing.

`Load` keeps its permissive behaviour and stays the function a running server
holds: it never writes, so it remains usable when the path underneath it is not.
That is what keeps the runtime rule intact.

**The runtime rule is unchanged.** A save failure while the server is running
still lets the approved call succeed, is still logged, and still leaves the host
unremembered. This ADR moves only the *detectable-at-startup* case earlier; it
does not turn an unsaveable approval into a denial.

**Creating the file does not widen ADR-0001's gate.** The whitelist is written
here with an empty list, which approves nothing. The only thing that adds a host
remains a human choosing *always*. Fail-closed is preserved in the sharpest
sense available: on the failure path the server serves nothing at all.

## Consequences

- A misconfigured mount is a deploy-time error against a named path rather than
  an approval that quietly never persists. This is the whole point of the
  change, and it is what lets the image stay out of the way of the mount's
  ownership.
- An operator who deliberately runs against a read-only whitelist can no longer
  start the server. That configuration is refused on purpose: it would accept
  approvals it cannot keep. A read-only *deployment* is not a use case this
  server has, since approving hosts is half of what it does.
- The startup check is a directory-writability check, because that is what
  saving actually needs — the whitelist is replaced by an atomic rename, not
  written in place. A whitelist file whose own mode is read-only but whose
  directory is writable therefore still starts, and still saves, exactly as it
  did before.
- There are now two ways to get a `Store`, and picking the wrong one is a real
  mistake in both directions: `Open` in a running server's save path would be
  wrong, and `Load` at startup gives back the silent failure this ADR removes.
  The distinction is documented on both functions, and the tests that pin it
  name each other.
- A first run creates a private (`0600`) file, matching what a first approval
  created before, so nothing about file modes changes for an existing
  deployment.
- The check races nothing meaningful: a directory that becomes unwritable after
  startup lands back on the unchanged runtime rule.
