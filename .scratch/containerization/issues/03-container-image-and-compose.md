# 03: Container image and compose stack with a persistent whitelist

**What to build:** An operator runs `docker compose up -d`, points a client at
`http://host:8080/mcp`, fetches a whitelisted host, approves an unknown one with
*always*, restarts the stack, and finds that host still approved.

The image is a multi-stage build on an **Alpine** runtime carrying the binary, a
CA bundle, and a non-root user. The certificate store is required for *outbound*
TLS — `askweb` verifies the hosts it fetches — and without it every fetch fails
with `x509: certificate signed by unknown authority` while the server looks
healthy. Install `ca-certificates` explicitly rather than copying a path out of
the build stage.

The container never runs as root, and an operator must be able to override the
user with any uid and gid (`user: "1003:1002"`) and still have everything work,
including persisting approvals. Nothing may depend on that user existing in
`/etc/passwd`, on `$HOME`, or on a uid known at build time, and the binary must
be readable and executable by any uid — otherwise an overridden user cannot
start the container at all. Follow the arbitrary-uid technique in
`apps/frontend/Dockerfile` of `MrModest/reisenotiz`: make what the runtime user
touches world-readable at build time, under a temporary root user, then drop
back to non-root.

Matching the mounted directory's ownership is the **operator's** job. The image
creates `/app/data` owned by its default user and does nothing further: no
entrypoint `chown`, no gid-`0` group trick, no startup permission fixups, since
each needs root or invents policy for a `chown` the operator can do once on the
host. Document that requirement where an operator will see it.

Compose mounts the **directory** `/app/data`, not the whitelist file: the
atomic save writes a temp file alongside the target and renames it into place,
so it needs the directory writable, and a first run needs somewhere to create
the file. Configuration goes through the environment, and nothing binds below
port 1024 since a non-root user cannot.

Follow the reference Dockerfile's conventions: the `# syntax` directive,
banner-commented stages, explicit `COPY` of the paths needed rather than
`COPY . .`, OCI `org.opencontainers.image.*` labels, and `EXPOSE`.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [x] `docker build` produces an image that starts and serves MCP at `/mcp`
- [x] Fetching a whitelisted `https` host from inside the container succeeds
- [x] The container runs as a non-root user by default
- [x] `user: "1003:1002"` starts and serves, given a data directory writable by it
- [x] `docker compose up -d` starts the server with the port published
- [x] The whitelist path defaults to `/app/data/whitelist.json` via the environment
- [x] Compose mounts the `/app/data` directory, not the whitelist file
- [x] An *always* approval survives `docker compose restart` and `down` then `up`
- [x] The host port and listen address are configurable without editing the image
- [x] The image carries OCI labels tracing back to the source repository
- [x] A `.dockerignore` keeps the build context free of the binary, git, and scratch files
- [x] The compose file documents that mount ownership must match the chosen user

## Comments

Implemented as `Dockerfile`, `docker-compose.yml`, and `.dockerignore`. All
twelve criteria were verified against a running daemon, not reasoned about.

Builder is `golang:1.26.5-alpine3.23` with `CGO_ENABLED=0` and
`ARG TARGETOS/TARGETARCH`, so ticket 05 can cross-build both architectures from
one builder with no emulator. Runtime is `alpine:3.23` with
`apk add --no-cache ca-certificates` — installed, never copied out of the
builder, where it would silently stop matching if the builder image changed.
Image is 26 MB.

Arbitrary-uid support follows the reference Dockerfile: as a temporary root
`USER`, `a+r` every file and `a+rx` every directory under `/app` plus the
binary, then drop back to `USER askweb` (10001:10001, `-S -H -D`, no home). An
overridden uid owns nothing in the image and is in no group there, so it gets
only the *other* bits — which is why they have to be set at build time.

`/app/data` is created owned by the default user and nothing further: no
entrypoint `chown`, no gid-0 trick, no startup fixup. Making it writable by an
unknown future uid has no build-time answer short of `0777`, which is not a
reasonable mode for the directory holding the security boundary.

Independently re-verified after the fact: default user is `uid=10001`; a real
`web_fetch` of an `https` host returned the page body with no `x509` error; a
non-whitelisted host on a client that cannot prompt was refused; `--user
1003:1002` against an empty mount started and created `whitelist.json` as `[]`
at mode `0600`; and an unwritable mount refused to start, naming the path. The
last two are ticket 02 and this ticket meeting: the ownership mistake this
image deliberately does not paper over now fails at deploy time instead of
silently dropping approvals.

`/data` added to `.gitignore` so a default `docker compose up` does not leave
the operator's whitelist state showing as untracked.

Two notes carried forward:

- On macOS, Docker Desktop bind mounts go through virtiofs and are more
  permissive about host ownership than a Linux host, so the *negative*
  ownership case is only partly reproducible here. It was reproduced by
  mounting a `0555` directory instead.
- A bare `docker compose up -d` with no `ASKWEB_DATA_DIR` lets Docker create
  `./data` owned by root, which uid 10001 cannot write. The compose file calls
  this out with the `chown` fix; the server now refuses to start rather than
  forgetting approvals.
