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

- [ ] `docker build` produces an image that starts and serves MCP at `/mcp`
- [ ] Fetching a whitelisted `https` host from inside the container succeeds
- [ ] The container runs as a non-root user by default
- [ ] `user: "1003:1002"` starts and serves, given a data directory writable by it
- [ ] `docker compose up -d` starts the server with the port published
- [ ] The whitelist path defaults to `/app/data/whitelist.json` via the environment
- [ ] Compose mounts the `/app/data` directory, not the whitelist file
- [ ] An *always* approval survives `docker compose restart` and `down` then `up`
- [ ] The host port and listen address are configurable without editing the image
- [ ] The image carries OCI labels tracing back to the source repository
- [ ] A `.dockerignore` keeps the build context free of the binary, git, and scratch files
- [ ] The compose file documents that mount ownership must match the chosen user
