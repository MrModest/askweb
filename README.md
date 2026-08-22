# askweb

A whitelisted web-access MCP server. It exposes a single `web_fetch` tool whose
reach is limited to an operator-controlled list of hostnames. Any other host has
to be approved by a human, and is refused without that approval.

It exists because general-purpose web tools give an agent unrestricted outbound
access, which is a prompt-injection surface. `askweb` narrows that to hosts you
named, and puts everything else in front of you before it is fetched.

## Status

The server is complete: whitelisted fetch over Streamable HTTP, a human
approval prompt for unknown hosts, persistence of hosts approved with
**always**, and re-validation of every redirect hop. It ships as a container
image built and smoke-tested on every merge.

See `.scratch/web-fetch-whitelist/` and `.scratch/containerization/` for the
PRDs.

## Install and run

Requires Go 1.26 or newer.

```sh
go build ./cmd/askweb
./askweb
```

The server listens on `:8080` and serves MCP over Streamable HTTP at `/mcp`.

## Running in Docker

An image is published for every merge to `main`, for `linux/amd64` and
`linux/arm64` under one tag — pull it and your client picks the right one.

```sh
docker pull ghcr.io/mrmodest/askweb:edge
```

| Tag | Moves? | Use it for |
|---|---|---|
| `edge` | yes, every merge to `main` | trying it out, or a stack you redeploy deliberately |
| `sha-<short>` | never | pinning a deployment to an exact commit |
| `1.2.3`, `1.2` | on releases | pinning to a release once one is tagged |

Pin `sha-` or a semver tag for anything you care about. `edge` moving under you
is the point of `edge`.

The compose file in this repository runs it:

```sh
mkdir -p ./data && sudo chown 10001:10001 ./data
docker compose up -d
```

That publishes `8080` and mounts `./data`. Override any of it from the
environment without touching the image: `ASKWEB_HOST_PORT`,
`ASKWEB_CONTAINER_PORT`, `ASKWEB_ADDR`, `ASKWEB_WHITELIST`, `ASKWEB_DATA_DIR`.

**The container speaks plain HTTP and holds no certificate.** Reach it by
service name on a private compose network, or put a reverse proxy that
terminates TLS in front of it. It does verify the certificates of the hosts it
fetches — that is a separate concern, and the image carries the root store for
it.

### The data directory

The whitelist lives at `/app/data/whitelist.json`, and it is the **directory**
that is mounted, never the file. Approvals are saved by writing a temporary file
alongside the target and renaming it into place, so the directory has to be
writable — and on a first run there has to be somewhere to create the file at
all. Bind-mounting `whitelist.json` itself gives you a server that cannot save a
single approval.

### Running as your own user

The image runs as uid:gid `10001:10001` and never as root. Any other user works:

```yaml
    user: "1003:1002"
```

Nothing in the image depends on that user existing in `/etc/passwd`, on `$HOME`,
or on a uid fixed at build time.

**Matching the mounted directory's ownership is your job.** The image does not
`chown` anything at startup — that would need root, in a container whose whole
point is not having it. Give the directory to whichever user you chose:

```sh
mkdir -p ./data && sudo chown 1003:1002 ./data
```

Get it wrong and the server tells you at startup rather than discovering it
weeks later as "approvals keep disappearing":

```
2026/08/21 23:01:51 askweb: creating whitelist /app/data/whitelist.json: open /app/data/whitelist.json.2539359718.tmp: permission denied
```

An existing whitelist it cannot replace fails the same way, naming the path. If
Docker creates the bind-mount directory for you it will belong to root, and this
is the error you will get.

### Editing the whitelist by hand

The same rule as the binary, and it bites more easily here: the file is
rewritten from what was loaded at startup plus everything approved since, so
edits made while the stack is running are lost at the next *always*. Stop it
first:

```sh
docker compose stop askweb
sudo $EDITOR ./data/whitelist.json
docker compose start askweb
```

The file belongs to whichever user the container runs as, not to you, which is
why that needs `sudo` — and why the server can write it and you cannot.

Restarting drops MCP sessions, so connected clients reconnect afterwards.

## Configuration

Each setting takes a flag, falling back to an environment variable, falling back
to a default. Nothing is baked into the binary.

| Flag | Environment | Default | Meaning |
|---|---|---|---|
| `--addr` | `ASKWEB_ADDR` | `:8080` | Listen address for the MCP HTTP server |
| `--whitelist` | `ASKWEB_WHITELIST` | `whitelist.json` | Path to the allowed-hostnames file |

```sh
./askweb --addr 127.0.0.1:9000 --whitelist /etc/askweb/hosts.json
```

## The whitelist file

A flat JSON array of hostnames, read once at startup:

```json
["example.com", "go.dev", "pkg.go.dev"]
```

Two rules govern it.

**Entries must already be canonical** — lowercase, punycode, no scheme, no port,
no path. `askweb` refuses to start on an entry that isn't, naming the offender
and the form it should take. This is deliberate: lookup performs no
normalization, so `Example.COM` would otherwise sit in the file granting nothing
at all, silently.

```
2026/08/01 13:06:38 askweb: whitelist hosts.json: entry "Example.COM" is not canonical, write it as "example.com"
```

**Subdomains are not implied.** An entry for `example.com` grants `example.com`
and nothing else. `www.example.com` needs its own line.

A missing file is not an error. At startup the server creates it holding an
empty list, so a first run needs no setup and you get immediate confirmation
that the path — or the mount — is the one you meant. An empty whitelist still
refuses everything.

What it will not do is start against a whitelist it cannot write. A directory it
cannot create the file in, or an existing file it cannot replace, is a startup
error naming the path, because the alternative is a server that accepts
approvals and quietly forgets them.

Approving a host with **always** appends it here, sorted and canonical. The file
is rewritten atomically — written alongside, flushed, and renamed into place — so
a reader only ever sees the whole old file or the whole new one, never a
partially written whitelist that the next start would refuse to parse. Existing
file permissions are carried over; a whitelist created by a first approval is
private. Only a human choosing *always* ever writes to it.

The file is rewritten from the set loaded at startup plus whatever has been
approved since, so edit it while the server is running and your edits will be
overwritten by the next *always*. Stop the server to change it by hand.

If the file cannot be written, the call the human just approved still succeeds —
they did approve it — but the host is not remembered, the failure is logged, and
the server keeps running:

```
2026/08/21 22:41:03 askweb: not persisting approval for "docs.example.org": saving whitelist /etc/askweb/hosts.json: ...
```

## Connecting a client

For Claude Code:

```sh
claude mcp add --transport http askweb http://localhost:8080/mcp
```

Restarting the binary drops the MCP session, so reconnect afterwards.

The server advertises one tool:

**`web_fetch`** — takes a single `url` argument and returns the response body.
A whitelisted host is fetched straight away. Any other host prompts you first;
without your approval it returns an error naming only the blocked host.

## Approving an unknown host

A host that is not on the whitelist does not fail outright — it asks you:

> Allow fetching from `docs.example.org`? It is not on the whitelist.
>
> **once** · **always** · **deny**

- **once** — fetches this one time, remembers nothing
- **always** — fetches, and adds the host to the whitelist file, so it is never
  asked again — including after a restart
- **deny** — fetches nothing

Everything that is not one of the first two is a denial: declining, cancelling,
letting the prompt expire, a transport failure, or picking something that is
none of the choices. Nothing is retried.

### Clients that only ask yes or no

Some clients don't render the three choices. They put the prompt to you as a
plain approve/refuse — buttons on a chat platform, say — and answer with that
action alone, filling in no choice at all. Hermes is one of these.

Approving on such a client fetches the page. It counts as **once**: the whitelist
is not written, and the next call asks you again. Refusing fetches nothing, as
always.

The reason is that accepting is itself the answer — you were asked whether this
host may be fetched, and you said yes — while the choice only decides how long
that yes lasts. With no choice to read, askweb takes the smallest thing your
approval could have meant. **Growing the whitelist from one of these clients
means editing `whitelist.json` by hand** (stop the server first, as above); a
persistent grant needs a human who was actually offered one. See
[ADR-0008](docs/adr/0008-accept-is-the-approval.md).

Your client has to be able to show the prompt. If it never declared that
capability, an unknown host is refused rather than fetched — the question could
not be put, so nobody approved it.

The prompt is carried as a multi-round-trip input request (SEP-2322): the tool
returns a request for input, your client asks you, and the call is retried with
your answer. See [ADR-0005](docs/adr/0005-multi-round-trip-input-requests.md).

## Redirects

A redirect is not a shortcut around the whitelist. Redirects are followed one
hop at a time, and each hop is checked exactly like the URL you asked for: a
whitelisted target is followed, an unknown one asks you first, and a refusal
ends the whole request with no body at all. The gate runs before each hop is
requested, so a host you turn down is never contacted.

Choosing **always** on a redirect saves the host that redirect points at — the
one you were asked about — not the one that sent you there.

Chains are bounded at ten hops, so a site that redirects to itself fails instead
of spinning.

If a chain needs you to approve more than one host, you are asked about the
earlier ones again alongside the new one — two unknown hops means three prompts
over three rounds. That repetition is deliberate: your client sends back only
the answers to the questions it was last asked, so a host left out of a later
prompt would have its approval dropped and the chain would stall
([ADR-0006](docs/adr/0006-per-hop-redirect-gating.md)).

**Prefer `always` for chains crossing more than one unknown host.** An `always`
is saved before the call is retried, so the earlier host is whitelisted rather
than asked about again, and every round carries a single question.

That is not only tidier — today it is the only thing that works. A round
carrying two questions at once is well-formed under SEP-2322, but a client on a
protocol older than 2026-07-28 never gets to see it: the SDK bridges the first
question to the client and then reinvokes the tool exactly once, so a second
round comes back as an empty result the client cannot act on. Answering `always`
keeps each round to a single question, which every client handles.

## How matching works

Every URL is canonicalized by one pure function before anything else happens: it
parses the URL, rejects any scheme other than `http`/`https`, rejects URLs with
no host, lowercases the host, strips the port, and converts to punycode. The
result is compared against the whitelist by exact string equality.

Exactness is the whole design. Each of these is refused against a whitelisted
`example.com`:

| Requested host | Why it's refused |
|---|---|
| `sub.example.com` | Subdomains are not implied by a parent entry |
| `example.com.evil.com` | Suffix extension |
| `evil-example.com` | Prefix extension |
| `аpple.com` (Cyrillic `а`) | Canonicalizes to `xn--pple-43d.com`, a different name |

Substring, prefix, and suffix matching are exactly the failure modes this server
exists to prevent, so matching is never anything but exact.

## Security notes

- **The model cannot approve its own fetches.** `web_fetch` takes a `url` and
  nothing else, by design. The approval answer travels in protocol-level fields
  written by your client, never in the tool's arguments. A parameter that could
  influence whether a host is allowed would defeat the whitelist, so any change
  to the tool's schema has to preserve this. See ADR-0001 and ADR-0005.
- **Fail closed.** Any path that is not an approval is a denial — and only your
  client's `accept` is an approval.
- **Only a human widens the whitelist.** `always` is the sole path that writes to
  the file, and an approval that cannot be saved is not remembered at all —
  never allowed in memory while missing from disk.
- **Refusals disclose only the blocked host** — never the whitelist's contents.
- **Redirects are checked hop by hop.** A whitelisted host that redirects
  elsewhere does not smuggle that host past the gate — see below.
- **No response size limit or content-type filtering** yet — out of scope for
  now.

## Design decisions

Recorded as ADRs in [`docs/adr/`](docs/adr/):

- [ADR-0001](docs/adr/0001-elicitation-gated-approval.md) — approval is gated on
  MCP elicitation; the model must never influence it
- [ADR-0002](docs/adr/0002-exact-hostname-matching.md) — exact canonical
  hostname matching, punycode-normalized
- [ADR-0003](docs/adr/0003-official-go-mcp-sdk.md) — the official Go MCP SDK
- [ADR-0004](docs/adr/0004-streamable-http-transport.md) — Streamable HTTP at
  `/mcp`
- [ADR-0005](docs/adr/0005-multi-round-trip-input-requests.md) — approval is
  carried as a multi-round-trip input request, superseding ADR-0001's mechanism
- [ADR-0006](docs/adr/0006-per-hop-redirect-gating.md) — every redirect hop is
  checked against the whitelist
- [ADR-0007](docs/adr/0007-create-the-whitelist-at-startup.md) — the whitelist is
  created at startup, and a server that cannot write it refuses to start
- [ADR-0008](docs/adr/0008-accept-is-the-approval.md) — accepting the prompt is
  the approval; the choice only says how long it lasts

## Development

```sh
go test ./...
```

The Go suite needs no Docker and never touches the network. The container is
covered separately by a smoke test that runs against a built image — the same
one CI runs before anything is published:

```sh
docker build -t askweb:smoke . && scripts/smoke-test.sh askweb:smoke
```

It asserts what an operator can see from outside: that the server answers on the
published port, fetches a whitelisted `https` host, keeps an *always* approval
across a container restart, and does all of it again under an overridden uid and
gid. It reaches `example.com`, since proving the certificate store is present is
the point of one of those assertions.

Layout:

| Package | Responsibility |
|---|---|
| `internal/hostname` | URL to canonical hostname. Pure, no dependencies |
| `internal/approval` | The human approval prompt and how its answer is read |
| `internal/whitelist` | The allowed set, loaded from JSON. Normalizes nothing |
| `internal/config` | Flag and environment resolution |
| `internal/server` | The MCP server and the `web_fetch` handler |
| `cmd/askweb` | Entry point |

No test touches the real network. MCP round trips use the SDK's in-memory
transports, and outbound fetches go through a client whose dialer redirects
every connection to a local TLS server — which is why the tests can use
realistic `https://example.com` URLs while staying entirely in-process.
