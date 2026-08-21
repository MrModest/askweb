# 06: Document the container deployment path in the README

**What to build:** An operator who has never seen this repository can deploy it
from the README alone: pull an image, start a compose stack, put the whitelist
somewhere durable, and run it as their own user.

The README currently documents only `go build` and running the binary. That path
stays, but it is no longer the only one, and the container path needs to read as
one coherent section rather than being scattered.

Cover: pulling by tag and what the tags mean, the compose stack, the data volume
and why the directory rather than the file is mounted, the `user:` override and
that matching the mount's ownership is the operator's job, and the startup error
they will see if they get it wrong.

Two existing README statements need to hold for the container path too, and
should be checked rather than assumed: that the server rewrites the whitelist
from the set loaded at startup plus whatever has been approved since — so
editing the mounted file while the stack runs means those edits are lost at the
next *always*, and the stack should be stopped first — and that restarting drops
MCP sessions, so clients reconnect.

**Blocked by:** 03, 05.

**Status:** ready-for-agent

- [x] The README documents pulling and running the published image
- [x] It explains the published tags and which to pin
- [x] It documents the compose stack and the data volume
- [x] It states that the `/app/data` directory is mounted, not the whitelist file
- [x] It documents the `user:` override and the ownership requirement
- [x] It shows the startup error for an unwritable data directory
- [x] It keeps the existing "stop before editing the whitelist by hand" caveat, for compose too
- [x] The existing binary-based instructions still stand alongside it

## Comments

Added a "Running in Docker" section to the README covering the published tags
and which to pin, the compose stack, the data directory, the `user:` override
and the ownership requirement, and the startup error an operator gets when the
mount is wrong. The binary instructions are untouched and still first.

Both existing statements the ticket asked to re-check do hold for the container
path, and both are now stated there: the whitelist is rewritten from what was
loaded at startup plus everything approved since, so the stack must be stopped
before editing the file by hand; and a restart drops MCP sessions, so clients
reconnect. The compose version of the hand-edit needs `sudo`, since the file
belongs to the container's user — called out, because it is the same fact that
makes the server able to write it.

Three stale claims elsewhere in the README were corrected while here, since the
ticket's own section would have contradicted them:

- "A missing file is treated as an empty whitelist rather than an error" is no
  longer the whole truth. It now says the file is created at startup, and that a
  whitelist the server cannot write is a startup error naming the path
  (ADR-0007).
- The status section still said "all four tickets are implemented", predating
  this effort.
- ADR-0007 was missing from the design-decisions list.

The Development section now documents `scripts/smoke-test.sh` alongside
`go test ./...`, including that it reaches `example.com` on purpose and that the
Go suite needs no Docker.

**Not verified live.** The documented commands were not re-run end to end: the
machine ran out of disk partway through this ticket, which took the Docker VM
down with `input/output error` and then failed Go's linker with `no space left
on device`. Every fact in the section was verified earlier against a running
container — the uid, the error text, the mount behaviour, the tag list against
the live registry — but the copy-paste `docker compose up -d` walkthrough
deserves one run on a machine with free space.
