# 04: Smoke-test the container in CI before anything is published

**What to build:** CI proves the built image actually works before it can be
pushed, catching the packaging breaks that the Go suite structurally cannot see.

Added as a **GitHub Actions** job in the workflow from ticket 01, gated with
`needs:` on the test job so it never runs against code that failed its tests.
The image is built and loaded locally for the test rather than pulled, so the
test covers the commit under CI, not whatever was published last.

Four assertions, each mapping to a way packaging breaks while every unit test
stays green:

1. the server answers on the published port,
2. a whitelisted host is fetched — proving the certificate store is present,
3. an *always* approval is written to the mounted whitelist and survives a
   container restart — proving the volume and ownership are right,
4. all of the above still hold under an overridden uid and gid that exist
   nowhere in the image, given a data directory the test has made writable for
   that user, which is the arrangement an operator is expected to make.

The fourth earns its keep: an arbitrary-uid override is the case most likely to
be broken by a plausible Dockerfile and least likely to be noticed, because the
server still starts, still fetches, and still accepts approvals — it just cannot
save them.

This is a shell-level test against the image's external interface. It must not
assert base image names, layer counts, image size, or paths inside the image.
`TestServesOverStreamableHTTPAtMCPPath` is the shape to copy for the MCP
sequence; `TestAlwaysApprovalSurvivesRestart` is the shape for persistence, made
literal by restarting a container instead of reloading a store.

The Go suite gains no dependency on Docker and must keep passing on a machine
with none installed.

**Blocked by:** 01, 03.

**Status:** ready-for-agent

- [x] The job builds the image from the commit under test and runs it
- [x] It fails if the server does not answer MCP on the published port
- [x] It fails if fetching a whitelisted `https` host fails
- [x] It fails if an *always* approval does not survive a container restart
- [x] It repeats the above under an overridden uid and gid, and fails if they break
- [x] It is gated on the test job and does not run when tests fail
- [x] It asserts nothing about how the image is built or layered
- [x] `go test ./...` still passes on a machine with no Docker installed

## Comments

Implemented as `scripts/smoke-test.sh`, run by a `smoke` job in the ticket 01
workflow with `needs: test`. The script takes an image tag, so it runs
identically in CI and by hand. Every assertion goes through the published port,
MCP at `/mcp`, and the mounted directory; nothing touches the base image, the
layers, the size, or a path inside the container. No Go file mentions Docker,
so the Go suite still passes on a machine without it.

**The client speaks protocol 2025-06-18, deliberately.** A client asking for
2026-07-28 is refused outright — the SDK serves that version only on a stateless
handler, and this server is stateful. On 2025-06-18 the SDK bridges an input
request to a server-initiated `elicitation/create` over the event stream, so the
script performs the exchange a real client performs: it streams the tool call,
reads the question off the stream, answers it as a JSON-RPC response, and reads
the completed call off the same stream. Discovered by trying the other way
first, which hung and then failed with `protocol version "2026-07-28" is only
supported on stateless HTTP servers`.

Both passes end with a control: an unapproved host is still refused, so a pass
cannot come from a server that allows everything.

**Mutation-tested — what it catches and what it does not.** Two mutants are
caught:

- `/app` usable by the image's default user only (`chown -R askweb && chmod -R
  750`): the default-user pass stays fully green and only the overridden-uid
  pass fails. This is the regression the ticket is about, and it is caught
  precisely.
- `/app` not traversable at all: fails in the first pass.

Two mutants are **not** caught, both worth knowing:

- **Removing `apk add --no-cache ca-certificates` changes nothing**, because
  `alpine:3.23` already ships `/etc/ssl/certs/ca-certificates.crt` via
  `ca-certificates-bundle` in the base image. The explicit install is kept — it
  states the dependency, and a base image is free to drop a bundle it never
  promised — but the certificate assertion cannot detect its removal on this
  base. It would still catch a move to `scratch` or a distroless base without
  roots, which is the failure the assertion was written for.
- **A binary that is not world-executable still starts.** `runc` execs the
  entrypoint while it still holds `CAP_DAC_OVERRIDE`, so `chmod 750` on the
  binary does not stop the container; the process then runs with `CapEff: 0` and
  is enforced normally for everything it does afterwards. Confirmed by isolating
  read from exec inside the container: a shell there gets `exit 126` on the same
  binary Docker started happily. So the world-executable bit is real but is not
  what the uid pass proves — the pass proves the directories are traversable and
  the data directory is writable, which is what actually breaks.
