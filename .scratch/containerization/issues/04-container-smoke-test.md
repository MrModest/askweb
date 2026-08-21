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

- [ ] The job builds the image from the commit under test and runs it
- [ ] It fails if the server does not answer MCP on the published port
- [ ] It fails if fetching a whitelisted `https` host fails
- [ ] It fails if an *always* approval does not survive a container restart
- [ ] It repeats the above under an overridden uid and gid, and fails if they break
- [ ] It is gated on the test job and does not run when tests fail
- [ ] It asserts nothing about how the image is built or layered
- [ ] `go test ./...` still passes on a machine with no Docker installed
