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

- [ ] A push to `main` runs the checks and reports status on the commit
- [ ] A pull request against `main` runs the checks and reports status on the PR
- [ ] `gofmt` non-compliance fails the run and names the offending files
- [ ] `go vet ./...` failure fails the run
- [ ] `go test ./... -race` runs the whole suite and its failure fails the run
- [ ] The Go version is taken from `go.mod`, not duplicated in the workflow
- [ ] Third-party actions are pinned by commit digest
- [ ] Module downloads are cached between runs
- [ ] The test job is named so later jobs can gate on it with `needs:`
