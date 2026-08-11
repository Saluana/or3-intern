# OR3 Connect release status

This page is the source of truth for the current Connect capability and
package contract. It is intentionally explicit so a copied command cannot
silently select an unverified service.

## Production Cloud decision

Remote Connect is **withheld** from the managed Cloud launch. The central
`https://or3.chat` device endpoint has not passed the required public staging
device-flow smoke, and it is not a supported default.

The local Intern runtime remains supported. A verified staging or self-hosted
operator may use the advanced path with an explicit `--cloud-url`; that path
must not be presented as the managed Cloud setup.

## Immutable package and asset contract

The coordinated 2026-08-11 release target is:

| Artifact | Coordinated version | Current source version | Required outcome |
| --- | --- | --- | --- |
| `@or3/intern-client` | `0.1.2` | `0.1.2` | Publish once from tag `v0.1.2`; never republish. |
| `@or3/connect` | `0.1.2` | `0.1.2` | Publish once from tag `v0.1.2`; never republish. |
| `or3-intern` GitHub release assets | `v0.1.2` | Bootstrap targets `v0.1.2` | Produce all platform archives and checksums from the same tag. |

The release is complete only when the GitHub Actions run succeeds, both exact
npm versions resolve, the matching GitHub release assets resolve, and a clean
`npx @or3/connect@0.1.2 intern --help` smoke passes. A successful workflow and
npm propagation are separate checks.

## Supported commands today

Local setup does not use remote Connect or a central endpoint:

```bash
npx @or3/connect@0.1.2 intern
```

From an `or3-intern` source checkout:

```bash
./scripts/install-cli.sh
or3-intern setup
or3-intern chat
```

For a one-off source run, use `go run ./cmd/or3-intern setup` instead.

For a verified staging or self-hosted endpoint only, an operator can opt into
the advanced flow explicitly:

```bash
or3-intern connect --cloud-url https://staging.example.test
```

Never paste a token, edit generated `.env` files, or use the old bare
`npx @or3/connect` command as the managed Cloud recommendation.
