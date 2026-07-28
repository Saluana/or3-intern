# `@or3/intern-client`

Framework-free TypeScript contract and transport for the authenticated
`or3-intern service` API. The package uses only web-platform primitives and is
owned beside the Go service contract.

## Public boundaries

- `createInternTransport` provides generic JSON requests and SSE streams for
  compatibility with any current service route.
- `createInternClient` provides typed health, readiness, capabilities, runner
  discovery, runner-chat session listing/creation/turns, approval, artifact,
  cancellation, and secure-pairing operations.
- Protocol parsers validate stable fields while retaining unknown response
  fields for forward compatibility.
- `InternClientError`, `InternUnavailableError`, `toInternResult`, and
  `requireInternCapability` provide typed failure boundaries.

Consumers inject the host URL, Fetch implementation, and asynchronous auth
resolver. Credentials are added only as headers, never as URL parameters.
Pairing explicitly disables auth resolution because its one-time secret belongs
in the JSON body.

```ts
const client = createInternClient({
    baseUrl: () => selectedHost.baseUrl,
    resolveAuth: async ({ method, path, requireAuth }) =>
        requireAuth
            ? {
                  token: await credentials.tokenFor({ method, path }),
                  headers: { 'X-Or3-Auth-Method': 'paired-device' },
              }
            : undefined,
});
```

`streamTurn` reconnects with bounded exponential backoff, resumes with
`after_seq`, and deduplicates replayed persisted events. Generic
`transport.stream` exposes configurable cursor, `Last-Event-ID`, terminal, and
dedupe hooks for other SSE routes.

Active sessions can be rehydrated without discovering unrelated host work:

```ts
const sessions = await client.listSessions({
    appSessionKeyPrefix: `or3-chat:${workspaceScope}:`,
    limit: 50,
});
```

The service matches this prefix literally, orders by `updated_at` descending,
and rejects limits above 100.

`client.listTurns(sessionId, { limit })` returns the newest bounded turn window
in chronological order, so rehydration retains the current/active tail without
reversing timeline rendering.

Runner-chat actions are fail-closed. Consumers should read the canonical
`chat_capabilities.cancel`, `chat_capabilities.approvalDecisions`, and
`chat_capabilities.customCwd` booleans. Approval decisions also require the
host capability report to advertise an enabled and available approval broker.

## Verification

From this directory:

```bash
bun test
bun build ./src/index.ts --target=browser --outdir=/tmp/or3-intern-client-build
```

The shared Go fixtures are checked from the repository root:

```bash
go test ./cmd/or3-intern -run 'TestOr3NetCompatibilityFixtures|TestServiceRouteContracts_CurrentAppUsageRoutesStayRegistered'
```
