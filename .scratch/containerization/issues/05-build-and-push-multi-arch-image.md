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

- [x] A merge to `main` publishes the image to `ghcr.io` under the repository owner
- [x] The published tag runs on both `amd64` and `arm64` from a single manifest list
- [x] `edge` and a short-SHA tag are published on merges to `main`
- [x] Pushing a version tag publishes the corresponding semver tags
- [x] A pull request builds the image but publishes nothing
- [x] A failing test job means no image is built or pushed
- [x] A failing smoke test means no image is pushed
- [x] Third-party actions are pinned by commit digest
- [x] Build cache is reused between runs

## Comments

Implemented as a `publish` job in the ticket 01 workflow, `needs: [test, smoke]`.
The push trigger gained `tags: ["v*"]` so a version tag runs the same pipeline
and publishes semver tags.

One job builds both architectures with `platforms: linux/amd64,linux/arm64` and
publishes a single manifest list — no per-architecture tags. No emulator is
involved: the Dockerfile's build stage takes BuildKit's `TARGETARCH` and Go
cross-compiles. Verified directly, since the local Docker daemon could not do a
multi-platform build (see below): `CGO_ENABLED=0 GOOS=linux GOARCH=...` produces
`ELF 64-bit ... x86-64, statically linked` and `ELF 64-bit ... ARM aarch64,
statically linked` from this tree. The runtime stage only installs a package and
copies that binary, so it carries no architecture-specific step of its own.

Tags follow the reference repo: `type=edge,branch=main`, `type=sha,format=short`,
and two semver patterns. `docker/metadata-action` lowercases the image name, so
`ghcr.io/${{ github.repository }}` publishes as `ghcr.io/mrmodest/askweb`, which
is what `docker-compose.yml` already points at.

A pull request runs the whole job but with `push: false`, so a broken Dockerfile
or a broken cross-build fails the PR without publishing anything. The login step
is skipped on pull requests, which also keeps it working from forks, whose token
could not log in.

`packages: write` is set at the job level. The workflow floor is
`contents: read`, which does not grant it.

All four Docker actions are pinned by full commit digest, each reverse-verified
against the tag it claims. Build cache is `type=gha` with `mode=max`.

**What is not verified locally.** The publishing criteria are observable only
once this runs on GitHub: that a merge to `main` really pushes `edge` and the
short SHA, that a version tag pushes semver, and that the manifest list carries
both architectures. The gating is structural rather than observed — `needs:`
means a red test or smoke job cannot be followed by a push.

The local Docker daemon could not confirm the multi-platform build itself: the
`docker` driver refuses multi-platform builds, and a `docker-container` builder
failed with `input/output error` from the VM's overlay2 store, which also
reported negative container sizes. That is a sick Docker Desktop VM rather than
anything about this Dockerfile — the single-architecture build and the full
smoke test had both passed minutes earlier on the same daemon. Worth re-running
`docker buildx build --platform linux/amd64,linux/arm64` after a Docker Desktop
restart, before relying on the first CI run to find a problem.
