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

The registry check on 2026-08-07 returned:

| Artifact | Published immutable version | Current source version | Launch status |
| --- | --- | --- | --- |
| `@or3/intern-client` | `0.1.1` | `0.1.1` | Keep; never republish this version. |
| `@or3/connect` | `0.1.0` only | `0.1.1` | The `0.1.1` source package is not published yet. |
| `or3-intern` GitHub release assets | `v0.1.0` only | Bootstrap source targets `v0.1.1` | A matching `v0.1.1` asset release is still required. |

Consequently, do not claim that `npx @or3/connect@0.1.1 intern` is available
until both the exact npm package and the matching GitHub release assets resolve.
Do not republish `@or3/intern-client@0.1.1`. Any release that contains the
pending bootstrap changes must use one new coordinated version for the package,
source tag, and binary assets, then verify exact `npm view` and `npx` results
after registry propagation.

## Supported commands today

From an `or3-intern` source checkout, local setup does not use Connect or a
central endpoint:

```bash
go run ./cmd/or3-intern setup
go run ./cmd/or3-intern chat
```

For a verified staging or self-hosted endpoint only, an operator can opt into
the advanced flow explicitly:

```bash
or3-intern connect --cloud-url https://staging.example.test
```

Never paste a token, edit generated `.env` files, or use the old bare
`npx @or3/connect` command as the managed Cloud recommendation.

