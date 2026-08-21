# 02: Create the whitelist on first run, fail fast when it cannot be written

**What to build:** An operator who mounts an empty data directory gets a
whitelist file created for them, and an operator who mounts a directory the
server cannot write is told so at startup instead of discovering it weeks later.

Today a missing whitelist is treated as an empty whitelist and nothing is
written until the first *always* approval. That stays true in spirit — a missing
file is still not an error — but the file is now created empty (`[]`) at
startup when the directory allows it, so the operator gets immediate
confirmation that the mount is correct.

The failure this guards against is silent and is the most likely deployment
mistake: a data directory the running user cannot write produces a server that
starts, fetches, prompts, accepts *always* — and forgets it, logging a line
nobody reads. Refusing to start instead turns that into a deploy-time error
naming the path.

The runtime rule is unchanged and must stay unchanged: if a save fails while the
server is running, the call the human approved still succeeds, the failure is
logged, and the host is simply not remembered. This ticket only moves the
*detectable-at-startup* case earlier.

Consider whether this warrants an ADR, since it sharpens the existing contract
that an approval which cannot be saved is not a persistent approval.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] A writable directory with no whitelist file gets an empty `[]` file at startup
- [ ] That case starts normally and refuses every host until one is approved
- [ ] An existing whitelist is left exactly as it is, not rewritten or reordered
- [ ] A directory the server cannot write fails at startup, naming the path
- [ ] An existing whitelist that cannot be written fails at startup, naming the path
- [ ] A save failure at runtime still lets the approved call succeed, as before
- [ ] A non-canonical entry in an existing file is still a startup error
- [ ] Covered by tests, including the unwritable-directory case
