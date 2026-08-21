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
2. a whitelisted host is fetched — which proves CA certificates are present,
3. an *always* approval is written to the mounted whitelist and survives a
   container restart — which proves the volume and file ownership are right.

That is one seam, not three, and it is a shell-level test against the image's
external interface. It asserts nothing about how the image is layered.

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
10. As an operator, I want the container to run as a non-root user, so that a
    compromise of the fetch path is not a root process.
11. As an operator, I want the non-root user to be able to write the whitelist,
    so that *always* approvals are actually persisted rather than silently lost.
12. As an operator, I want a clear startup failure when the whitelist is
    unwritable, so that I find out at deploy time rather than when an approval
    quietly fails to save.
13. As an operator, I want the listen address configurable through the
    environment, so that compose can set it without a custom command.
14. As an operator, I want outbound HTTPS to work from inside the container, so
    that fetching a whitelisted host does not fail on certificate verification.
15. As an operator, I want the image to contain no shell or package manager it
    does not need, so that the attack surface stays small.
16. As an operator, I want the image to carry provenance labels back to the
    commit it was built from, so that I can trace a running container to source.
17. As a maintainer, I want the test suite run on every pull request, so that a
    regression in the whitelist gate cannot merge.
18. As a maintainer, I want the race detector enabled in CI, so that concurrent
    access to the whitelist store stays safe.
19. As a maintainer, I want formatting and vet checked in CI, so that style
    review is not a human job.
20. As a maintainer, I want the container smoke test in CI, so that a broken
    image is caught before it is published rather than by an operator.
21. As a maintainer, I want third-party actions pinned by digest, so that the
    release pipeline is not a supply-chain hole in a security tool.
22. As a maintainer, I want build cache between runs, so that CI stays fast.
23. As a maintainer, I want the image built on pull requests without being
    pushed, so that a Dockerfile break is caught without publishing.
24. As a Hermes operator, I want `askweb` reachable on a compose network by
    service name, so that I can attach it to the Hermes stack.
25. As a maintainer, I want the README to document the container path, so that
    the compose file is not the only place the deployment is described.

## Implementation Decisions

**Image.** Multi-stage build. A Go build stage compiles a static binary with
CGO disabled; the final stage carries the binary, CA certificates, and nothing
else. The final stage should be `scratch` or a distroless static base — the
decision hinges only on whether a shell is wanted for debugging, and the default
is no shell. **CA certificates must be copied in explicitly**; without them
every `https` fetch fails certificate verification, which is the product's only
job. This is the single easiest way to ship a broken image, and the smoke test
exists largely to catch it.

**Non-root.** The image runs as a fixed non-root uid/gid. Because the whitelist
is written at runtime, that uid must own — or be able to write — the mounted
whitelist path. The interaction between a fixed image uid and host bind-mount
ownership is the sharpest edge in this work and should be decided explicitly:
either a named volume initialised at first run, or a documented `user:` override
in compose. A bind-mount of a host file owned by another uid will produce a
server that runs fine and silently forgets every approval.

**Whitelist location.** The container's default whitelist path should be an
absolute path under a dedicated data directory rather than the working
directory's relative `whitelist.json`, so that the mount point is unambiguous.
Set through `ASKWEB_WHITELIST`, not a baked-in flag, so an operator can still
override it.

**Fail loudly on an unwritable whitelist.** Today an unsaveable approval is
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
The decision is therefore **one multi-arch build job publishing a single
manifest list**, so that operators pull one tag and get the right architecture.
Suffixed per-arch tags are not published. If a future dependency introduces cgo,
this decision needs revisiting.

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
above. Its three assertions map to the three ways packaging can break while the
unit tests stay green: wrong port or bind address, missing CA certificates, and
an unwritable or non-persistent whitelist mount. The restart assertion is the
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

The reference the workflows are modelled on is
`https://github.com/MrModest/reisenotiz/tree/main/.github/workflows`. Its
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
