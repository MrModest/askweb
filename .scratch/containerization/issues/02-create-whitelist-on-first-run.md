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

- [x] A writable directory with no whitelist file gets an empty `[]` file at startup
- [x] That case starts normally and refuses every host until one is approved
- [x] An existing whitelist is left exactly as it is, not rewritten or reordered
- [x] A directory the server cannot write fails at startup, naming the path
- [x] An existing whitelist that cannot be written fails at startup, naming the path
- [x] A save failure at runtime still lets the approved call succeed, as before
- [x] A non-canonical entry in an existing file is still a startup error
- [x] Covered by tests, including the unwritable-directory case

## Comments

Implemented as a new `whitelist.Open`, called by the server at startup;
`whitelist.Load` is unchanged. Recorded as
[ADR-0007](../../../docs/adr/0007-create-the-whitelist-at-startup.md).

The check deliberately does **not** live in `Load`. `Load` never writes, which is
exactly why a running server can keep using it when the path underneath it is
not writable — the runtime rule that an unsaved *always* still allows the call
depends on that, and two existing server tests load a path inside a directory
that does not exist. A writability check in `Load` would have failed them, and
would have turned an unsaveable approval into a denial.

Writability is proven the way `save` needs it: create a file beside the
whitelist and remove it. Saving replaces the file by atomic rename, so the
directory is the real requirement. Opening the whitelist itself for writing
would have refused startup on a `0400` whitelist in a writable directory, which
saves fine today.

Creating the file does not widen ADR-0001's gate: it is written with an empty
list, which approves nothing, and only a human choosing *always* ever adds a
host. Verified by mutation — a mutant seeding a host into the created file is
caught, as are mutants dropping the writability check and rewriting an existing
file.

One deliberate behaviour change for operators: running against a whitelist that
cannot be written is now refused at startup rather than accepted and quietly
forgotten. ADR-0007 records why that configuration is not supported.

`gofmt -l .` silent, `go vet ./...` clean, `go test ./... -race` green across all
six packages.
