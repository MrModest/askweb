# 01: Run formatting, vet, and the test suite in GitHub Actions

**What to build:** Every push to `main` and every pull request against it runs
the existing checks automatically, so a regression in the whitelist gate cannot
merge unnoticed. A maintainer opening a pull request sees a pass or fail without
running anything locally.

Implemented as a **GitHub Actions** workflow at `.github/workflows/`. This
workflow is the one the later container work extends: the image build and the
push are added to it as further jobs, gated on this one with `needs:`, so a red
test job means no image is ever built or published. Name the job accordingly —
later tickets depend on it by name.

Third-party actions are pinned by commit digest, matching the convention in
`MrModest/reisenotiz`. The Go toolchain version comes from `go.mod` rather than
being hardcoded in two places.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [x] A push to `main` runs the checks and reports status on the commit
- [x] A pull request against `main` runs the checks and reports status on the PR
- [x] `gofmt` non-compliance fails the run and names the offending files
- [x] `go vet ./...` failure fails the run
- [x] `go test ./... -race` runs the whole suite and its failure fails the run
- [x] The Go version is taken from `go.mod`, not duplicated in the workflow
- [x] Third-party actions are pinned by commit digest
- [x] Module downloads are cached between runs
- [x] The test job is named so later jobs can gate on it with `needs:`

## Comments

Implemented as `.github/workflows/ci.yml`: one job, id `test`, running `gofmt`,
`go vet ./...` and `go test ./... -race` on pushes to `main` and PRs against it.
Tickets 04 and 05 gate on it with `needs: test`.

`gofmt -l` is captured and checked rather than run bare, because bare `gofmt -l`
exits 0 even when it lists files — the naked command would report green on
unformatted code. The failure path was exercised against a deliberately
malformed file, not just assumed.

The Go version comes from `go-version-file: go.mod`, so it is not duplicated.
Module caching is `setup-go`'s own, keyed off the existing `go.sum`; confirmed by
reading `action.yml` at the pinned SHA rather than trusting the documented
default.

Both actions are pinned by full commit digest with the tag in a trailing
comment, following `reisenotiz`'s `_docker-build.yml` rather than its `ci.yml`,
which pins only to `@v4`. Digests were verified twice, independently.

Workflow-level `permissions: contents: read`. Ticket 05's push job has to add
job-level `packages: write` for ghcr.io — the workflow-level floor will not
grant it.

Locally green at implementation time: `gofmt -l .` silent, `go vet ./...` clean,
`go test ./... -race` 144 tests across 6 packages. The two criteria about status
appearing on a commit or PR are satisfied by the triggers but only observable
once this is pushed to GitHub.
