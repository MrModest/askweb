# askweb — container image, compose file, and release pipeline

Status: ready-for-agent

## Problem

`askweb` is currently a binary you build and run yourself. Every deployment is a
`go build` on the target host, which means the host needs a Go toolchain, the
run command and its flags live in someone's shell history, and there is no
artifact to roll back to. Hermes is meant to reach this server as a long-running
service alongside it; there is nothing to point a compose stack at.

There is also no CI. Correctness here rests on a test suite that is deliberately
sharp — fail-closed paths, mutation-tested approval logic — and nothing runs it
except a human remembering to. A regression in the whitelist gate would reach
`main` unnoticed.

## Solution

Three things, from an operator's point of view:

- A **container image** holding the `askweb` binary and nothing else it does not
  need, published to `ghcr.io` on every merge to `main`.
- A **compose file** that starts the server, publishes its port, and keeps the
  whitelist on a volume so approvals survive `docker compose down`.
- A **CI workflow** that runs the existing test suite on every push and pull
  request, so the gate cannot regress silently, and a **release workflow** that
  builds and pushes the image.

An operator should be able to run `docker compose up -d`, point a client at
`http://host:8080/mcp`, approve a host with *always*, restart the stack, and find
that host still approved.

## Decisions already made

Settled elsewhere; read before building, do not relitigate.

- [ADR-0004](../../docs/adr/0004-streamable-http-transport.md) — Streamable HTTP
  at `/mcp`, configurable listen address.
- The whitelist file is the security boundary and is **written at runtime**:
  choosing *always* appends to it (README, "The whitelist file"). It is loaded
  once at startup, and rewritten atomically, carrying the existing file mode
  over.
- Configuration is flag, then environment variable, then default:
  `--addr` / `ASKWEB_ADDR` / `:8080`, and `--whitelist` / `ASKWEB_WHITELIST` /
  `whitelist.json`.
- A missing whitelist file is an empty whitelist, not an error. A non-canonical
  entry **is** a startup error.
- An approval that cannot be saved is logged and forgotten, and the call still
  succeeds. Silent degradation is therefore the failure mode to design against.

## Seams

The existing seam is the Go test suite in `internal/`, which covers behaviour
through the MCP client session and never touches the network or the filesystem
outside a temp dir. **Nothing in this work changes that seam, and no Go test
should grow a dependency on Docker.**

One new seam is proposed, at the highest point available: a **smoke test against
a running container**, exercised in CI. It starts the published image with a
whitelist mounted in, speaks MCP over HTTP at `/mcp`, and asserts the packaging
decisions that unit tests structurally cannot reach:

1. the server answers on the published port,
2. a whitelisted host is fetched — which proves the certificate store is present,
3. an *always* approval is written to the mounted whitelist and survives a
   container restart — which proves the volume and file ownership are right,
4. all of the above still hold when the container is started with an overridden
   uid and gid that exist nowhere in the image, given a data directory the
   test has made writable by that user — the arrangement an operator is
   expected to make for themselves.

That is one seam, not four, and it is a shell-level test against the image's
external interface. It asserts nothing about how the image is layered.

The fourth assertion is the one that earns its keep. An arbitrary-uid override
is the case most likely to be broken by a plausible Dockerfile and least likely
to be noticed, because the server still starts, still fetches, and still accepts
approvals — it just cannot save them.

Explicitly **not** proposed as seams: linting the Dockerfile, asserting image
size, asserting layer count, or unit-testing the workflow YAML. These couple to
implementation and rot.

## User Stories

1. As an operator, I want a published container image, so that I can deploy
   `askweb` without a Go toolchain on the target host.
2. As an operator, I want the image built on every merge to `main`, so that what
   I deploy matches what was reviewed.
3. As an operator, I want images tagged by short commit SHA, so that I can name
   the exact build I am running.
4. As an operator, I want a moving `edge` tag, so that I can track `main` without
   editing a version each time.
5. As an operator, I want semver tags when I push a version tag, so that I can
   pin a release.
6. As an operator, I want the image to run on both `amd64` and `arm64`, so that
   it works on a cloud VM and on a Raspberry Pi or Apple Silicon host alike.
7. As an operator, I want a compose file, so that starting the server is one
   command with no remembered flags.
8. As an operator, I want the whitelist kept outside the container, so that
   approvals survive a restart, a recreate, and an image upgrade.
9. As an operator, I want to edit the whitelist by hand on the host, so that I
   can seed it without going through an approval prompt.
10. As an operator, I want the container to run as a non-root user by default,
    so that a compromise of the fetch path is not a root process.
11. As an operator, I want to override the container's uid and gid to arbitrary
    values from my own compose file, so that it matches the service account my
    host already uses.
12. As an operator, I want approvals to persist under whatever uid and gid I
    chose, so that overriding the user does not silently break *always*.
13. As an operator, I want a shell in the image, so that I can debug outbound
    connectivity from inside the container when a fetch fails.
14. As an operator, I want the binary readable and executable by any uid, so
    that overriding the user does not leave the container unable to start.
15. As an operator, I want a clear startup failure when the whitelist is
    unwritable, so that I find out at deploy time rather than when an approval
    quietly fails to save.
16. As an operator, I want the listen address configurable through the
    environment, so that compose can set it without a custom command.
17. As an operator, I want outbound HTTPS to work from inside the container, so
    that fetching a whitelisted host does not fail on certificate verification.
18. As an operator, I want the image to carry no build toolchain, source, or
    package cache, so that the attack surface stays small.
19. As an operator, I want the image to carry provenance labels back to the
    commit it was built from, so that I can trace a running container to source.
20. As a maintainer, I want the test suite run on every pull request, so that a
    regression in the whitelist gate cannot merge.
21. As a maintainer, I want the race detector enabled in CI, so that concurrent
    access to the whitelist store stays safe.
22. As a maintainer, I want formatting and vet checked in CI, so that style
    review is not a human job.
23. As a maintainer, I want the container smoke test in CI, so that a broken
    image is caught before it is published rather than by an operator.
24. As a maintainer, I want third-party actions pinned by digest, so that the
    release pipeline is not a supply-chain hole in a security tool.
25. As a maintainer, I want build cache between runs, so that CI stays fast.
26. As a maintainer, I want the image built on pull requests without being
    pushed, so that a Dockerfile break is caught without publishing.
27. As a Hermes operator, I want `askweb` reachable on a compose network by
    service name, so that I can attach it to the Hermes stack.
28. As a maintainer, I want the README to document the container path, so that
    the compose file is not the only place the deployment is described.

## Implementation Decisions

**Image.** Multi-stage build. A Go build stage compiles a static binary with
CGO disabled; the final stage is **Alpine**, carrying the binary, a CA bundle,
and a non-root user.

Alpine over `scratch` or distroless deliberately. The image needs a root
certificate store either way (see below), and on Alpine that is
`apk add --no-cache ca-certificates` — explicit, self-documenting, and hard to
get wrong — rather than a `COPY` of a path from the build stage that silently
stops matching when the builder image changes. It also brings a shell, which is
worth having when debugging a server whose whole job is reaching a network you
cannot see from outside the container, and makes adding a non-root user a single
instruction. The size difference over distroless is a few megabytes and buys
real operability.

**A root certificate store is required.** This is about *outbound* TLS, not
inbound: `askweb` verifies the certificates of the whitelisted hosts it fetches,
and Go reads the root store from disk — `/etc/ssl/certs/ca-certificates.crt`,
`/etc/ssl/cert.pem`, and a handful of distro alternatives. An image without one
fails every fetch with `x509: certificate signed by unknown authority` while the
server itself looks perfectly healthy. Verified locally: the same binary against
the same URL returns `200 OK` with a system pool and that error with an empty
one. Alpine ships a bundle in its base, but the package should still be
installed explicitly so the dependency is stated rather than inherited.

**No inbound TLS.** The server speaks plain HTTP on `/mcp` and is reached either
over a private compose network or through a reverse proxy that terminates TLS.
The image holds no server certificate and no TLS configuration.

**Non-root, with an overridable uid and gid.** The image ships a default
non-root user and never runs as root. An operator must be able to override it
with any uid and gid — `user: "1003:1002"` in their own compose file, or
`--user` on `docker run` — and have the server work, including persisting
approvals. That is a hard requirement, not a documented nicety.

It constrains the image: nothing may depend on the default user existing in
`/etc/passwd`, on `$HOME`, or on a uid known at build time. The binary needs no
identity of its own.

**Matching the mount's ownership is the operator's job, and the image does
nothing clever about it.** The image creates `/app/data` owned by its default
non-root user, and that is the whole of its responsibility. An operator who
overrides the uid and gid is responsible for making the directory they mount
writable by the user they chose. No entrypoint `chown`, no gid-`0` group trick,
no permission fixups at startup — every one of those either needs root or
invents policy on the operator's behalf, and neither is warranted for a
`chown` the operator can do once on the host.

This keeps the image simple and honest, but it makes the failure mode the
operator's to hit, and that failure is silent: a data directory the running user
cannot write produces a server that starts, fetches, prompts, accepts *always* —
and forgets it. That is the case for the startup writability check below, which
turns a mismatched mount into a clear error at deploy time rather than a
mystery weeks later. The two decisions are a pair: the image stays out of the
way, so it owes the operator a loud failure when they get it wrong.

The compose file should document the requirement where an operator will see it,
and the README should state it alongside the `user:` override.

**Dockerfile conventions.** Follow the reference repo's frontend Dockerfile
(`apps/frontend/Dockerfile` in `reisenotiz`): the `# syntax=docker/dockerfile:1`
directive, banner-commented build and runtime stages, explicit `COPY` of the
paths needed rather than `COPY . .`, OCI `org.opencontainers.image.*` labels for
traceability, and an `EXPOSE` matching the default listen port.

**Arbitrary-uid support, borrowed from that reference and where it stops.** The
reference makes everything the runtime user touches world-readable at build
time — `chmod -R a+r`, plus `a+rx` on directories — under a temporary
`USER root`, then drops back to a non-root `USER`. That is what lets it run
under a uid nobody knew at build time, and the same treatment applies here to
the binary and the directory holding it: an image whose binary is readable and
executable only by its default user cannot run under `user: "1003:1002"` at all.

It does not extend to the data directory. Nginx only reads its files, so
world-readable is enough; `askweb` writes the whitelist, and no build-time mode
makes a path writable by an unknown future uid short of `0777` — which is not a
reasonable mode for the directory holding the security boundary. This is the
concrete reason the mount's ownership sits with the operator: the read half of
arbitrary-uid support is the image's job and is handled the reference's way, and
the write half is a `chown` only the operator can make correctly.

**Unprivileged port.** The default listen port stays `8080` and nothing binds
below 1024, since a non-root user cannot. The reference base image
(`nginx-unprivileged`) makes the same choice for the same reason.

**Whitelist location.** The image defaults `ASKWEB_WHITELIST` to an absolute
path under a dedicated data directory — `/app/data/whitelist.json` — rather than
the working directory's relative `whitelist.json`, so the mount point is
unambiguous. Set through the environment variable, not a baked-in flag, so an
operator can still override it.

Compose mounts the **directory**, `/app/data`, not the file. That matters: the
server writes the whitelist by creating a temp file alongside it and renaming
into place, so it needs write permission on the containing directory, not just
on the file. Mounting a single file also means a first run with no whitelist has
nowhere to create one.

**Fail loudly on an unwritable whitelist.** This is now load-bearing rather than
a nicety, because the ownership contract above puts the mount in the operator's
hands. Today an unsaveable approval is
logged and dropped, which is correct at runtime but invisible at deploy time.
A startup writability check is proposed so a misconfigured mount fails fast.
This changes server behaviour and touches the whitelist module, so it should be
its own ticket and its own decision — it may warrant an ADR, since it modifies
the "an approval that cannot be saved is not a persistent approval" contract at
the edges. It must not change the runtime rule that a human's approval still
allows the call in flight.

**Compose.** One service, published port, whitelist volume, restart policy, and
environment-based configuration. No reverse proxy and no TLS termination — the
server speaks plain HTTP and is expected to sit behind whatever the operator
already runs. Compose should express the port as host-configurable rather than
hard-coding `8080` on the host side.

**Registry and tagging.** `ghcr.io`, image named for the repository owner and
`askweb`, authenticated with the workflow's own `GITHUB_TOKEN`. Tags follow the
reference repo's `docker/metadata-action` scheme: `edge`, short SHA, and semver
on version tags.

**Architecture strategy — a deliberate divergence from the reference.** The
`reisenotiz` workflows build each architecture on its own native runner and
publish `-amd64` / `-arm64` suffixed tags, because Node builds under emulation
are painfully slow. That constraint does not apply here: Go cross-compiles, so a
single job can produce both platforms by varying `GOARCH` with no emulation.
The decision, confirmed with the maintainer, is **one multi-arch build job
publishing a single manifest list**, so operators pull one tag and get the right
architecture. No emulation, no per-arch suffixed tags, one workflow. If a future
dependency introduces cgo this needs revisiting, since cross-compiling would
then need a C toolchain per target.

**Workflow layout.** Two workflows, matching the reference repo's separation of
concerns: a `ci` workflow running formatting, vet, and tests on pushes and pull
requests to `main`; and a build-and-push workflow triggered on merges to `main`
and on version tags. The build workflow builds without pushing on pull requests
so a broken Dockerfile is caught early. Third-party actions are pinned by commit
digest, as the reference does — this is a security tool and its pipeline should
not be its weakest link. Build cache uses the GitHub Actions cache backend.

**No reusable workflow.** The reference factors a `_docker-build.yml` because it
builds several apps from a monorepo. This repository builds one image, so the
indirection would cost more than it saves.

## Testing Decisions

A good test here asserts what an operator can observe from outside the
container: that it answers, that it fetches, that approvals persist. It must not
assert how the image is built. Tests that pin base image names, layer counts, or
file paths inside the image will break on every legitimate change and teach
people to ignore failures.

**The existing Go suite is unchanged** and stays the primary safety net. It runs
in CI with `-race`, exactly as it runs locally. No Go test gains a Docker
dependency; the suite must keep passing on a machine with no Docker installed.

**The container smoke test** is a shell-level test in CI, at the seam described
above. Its assertions map to the ways packaging can break while the unit tests
stay green: wrong port or bind address, a missing certificate store, an
unwritable or non-persistent whitelist mount, and a data directory that only the
image's default user can write. The restart assertion is the
important one — it is the only place the volume and uid decisions are actually
verified, and it is the failure that would otherwise reach an operator as
"approvals keep disappearing".

**Prior art.** `TestServesOverStreamableHTTPAtMCPPath` is the shape to copy: it
connects a real MCP client over Streamable HTTP to `/mcp`, lists tools, and
fetches. The smoke test is that same sequence against a container instead of an
in-process server. `TestAlwaysApprovalSurvivesRestart` is the shape for the
persistence assertion — it already models a restart as "a new store reading the
saved file", and the container version makes the restart literal.

If the startup writability check is built, it needs a unit test in the whitelist
module for the unwritable case, alongside the existing coverage of a save
failure at runtime.

## Out of Scope

- TLS termination, reverse proxying, and authentication in front of `/mcp`.
- Kubernetes manifests, Helm charts, or any orchestrator beyond compose.
- Publishing to any registry other than `ghcr.io`.
- Signing images or generating SBOMs. Worth doing later for a security tool;
  not a prerequisite for a first image.
- A healthcheck. `/mcp` requires a POST with a session, so a naive `GET`
  healthcheck reports unhealthy on a working server. Doing this properly means
  deciding what "healthy" means for an MCP endpoint, which is its own question.
- Release automation beyond building an image: no changelog generation, no
  version bumping, no GitHub Releases.
- Any change to the approval flow, the whitelist matching rules, or the redirect
  gate.

## Further Notes

The reference for both the workflows and the Dockerfile is
`https://github.com/MrModest/reisenotiz` — `.github/workflows` and
`apps/frontend/Dockerfile`. Its
conventions worth carrying over: `ghcr.io`, `docker/metadata-action` tagging,
digest-pinned third-party actions, GitHub Actions build cache, and path filters
on triggers. Its conventions worth dropping: the reusable workflow indirection
and per-architecture native runners, both of which exist for a monorepo of Node
apps.

One consequence of the whitelist being runtime state deserves repeating, because
it will bite whoever writes the compose file: the server rewrites the file from
the set loaded at startup plus whatever has been approved since. Editing it on
the host while the container runs means those edits are overwritten by the next
*always*. The compose documentation should say to stop the stack before editing
by hand, exactly as the README already says for the binary.
