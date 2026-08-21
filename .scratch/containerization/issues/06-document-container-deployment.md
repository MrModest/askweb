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

- [ ] The README documents pulling and running the published image
- [ ] It explains the published tags and which to pin
- [ ] It documents the compose stack and the data volume
- [ ] It states that the `/app/data` directory is mounted, not the whitelist file
- [ ] It documents the `user:` override and the ownership requirement
- [ ] It shows the startup error for an unwritable data directory
- [ ] It keeps the existing "stop before editing the whitelist by hand" caveat, for compose too
- [ ] The existing binary-based instructions still stand alongside it
