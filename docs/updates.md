# Self-updates for fleets

GoForge apps are single binaries, which makes updating many instances on many
servers a distribution problem. The `update` module solves it Caddy-style: a
static, optionally-signed **release manifest** that instances poll and apply.

This is built for the Northplane/CMP model — lots of instances, on different
servers, on different versions — that need to converge safely.

## Publish a release

```sh
# once: generate a signing key. Keep the private key secret; never commit it.
forge release keygen
#   → forge-release.key (private)
#   → prints the public key (hex) — set it as the updates.publicKey setting

# build + sign + write a manifest
forge release \
  --version 1.4.0 \
  --url https://downloads.example.com/myapp \
  --key forge-release.key \
  --notes "Adds the reports module."
```

This cross-compiles for `linux/{amd64,arm64}`, `darwin/{amd64,arm64}` and
`windows/amd64` (customize with `--targets`), computes SHA-256 and an ed25519
signature per artifact, and writes `dist/release/manifest.json`:

```json
{
  "name": "myapp",
  "channels": {
    "stable": [{
      "version": "1.4.0",
      "date": "2026-07-01",
      "notes": "Adds the reports module.",
      "artifacts": {
        "linux/amd64":  {"url": "https://…/myapp_1.4.0_linux_amd64",  "sha256": "…", "signature": "…"},
        "darwin/arm64": {"url": "https://…/myapp_1.4.0_darwin_arm64", "sha256": "…", "signature": "…"}
      }
    }]
  }
}
```

Upload `dist/release/*` to that URL (any static host or object storage).

## Configure instances

In each instance's **Settings → Updates**:

- `updates.manifestURL` — `https://downloads.example.com/myapp/manifest.json`
- `updates.channel` — `stable` (point staging instances at a different channel
  for staged rollouts)
- `updates.publicKey` — the hex public key; when set, unsigned or mis-signed
  releases are **refused**
- `updates.autoCheck` — log when an update is available (daily)
- `updates.autoApply` — apply and restart automatically (off by default)

## Apply an update

Three ways, all equivalent:

- **Admin UI** → the Updates screen shows the available version and an *Apply* button.
- **CLI**: `app update` (or `app update --check` to only report).
- **Automatic**: enable `updates.autoApply`.

The updater downloads the platform artifact, verifies SHA-256 (always) and the
ed25519 signature (when a public key is configured), then does an **atomic swap**:
the running binary is renamed to `.bak` and the new one moved into place, followed
by a self-restart (`execve` on Unix). If anything fails verification, nothing is
touched.

## Versioning & guards

Versions are semver-ish (`1.4.0`, `v1.4.0-rc.1`); pre-releases sort before their
release. A release may set `minUpdate` to refuse jumps from too-old versions
(forcing an intermediate hop). The instance only updates when the manifest offers
a strictly newer version on its channel.

## Rollback

The previous binary remains as `<binary>.bak`. To roll back, stop the service,
move the `.bak` back into place, and restart. Combine with the `backups` module to
snapshot data before large upgrades.
