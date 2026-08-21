# 05: Publish a multi-arch image to ghcr.io on merges to main

**What to build:** Merging to `main` publishes a container image an operator can
pull and run, on both `amd64` and `arm64`, without a Go toolchain on the host.

Added as a **GitHub Actions** job in the workflow from ticket 01, gated with
`needs:` on both the test job and the smoke-test job. A failing test or a broken
image must mean nothing is published — the registry should never carry an image
that did not pass its own smoke test. Pull requests run the whole pipeline
except the push, so a broken Dockerfile is caught without publishing.

**One build job produces both architectures.** Go cross-compiles, so varying
`GOARCH` needs no emulation and no per-architecture runners. Publish a single
manifest list so operators pull one tag and get the right architecture; no
`-amd64` / `-arm64` suffixed tags. The reference repo splits per architecture
because Node builds under QEMU are slow, which does not apply here. If a future
dependency introduces cgo this needs revisiting, since cross-compiling would
then need a C toolchain per target.

Tagging follows `docker/metadata-action` as the reference repo uses it: a moving
`edge` tag, a short commit SHA, and semver tags on version tags. Authentication
uses the workflow's own token. Third-party actions are pinned by commit digest —
this is a security tool and its pipeline should not be its weakest link.

**Blocked by:** 01, 03, 04.

**Status:** ready-for-agent

- [ ] A merge to `main` publishes the image to `ghcr.io` under the repository owner
- [ ] The published tag runs on both `amd64` and `arm64` from a single manifest list
- [ ] `edge` and a short-SHA tag are published on merges to `main`
- [ ] Pushing a version tag publishes the corresponding semver tags
- [ ] A pull request builds the image but publishes nothing
- [ ] A failing test job means no image is built or pushed
- [ ] A failing smoke test means no image is pushed
- [ ] Third-party actions are pinned by commit digest
- [ ] Build cache is reused between runs
