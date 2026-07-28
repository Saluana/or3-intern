import { describe, expect, it } from 'bun:test';
import {
    InternUnavailableError,
    createInternClient,
    findInternRunner,
    requireInternCapability,
    requireInternRunner,
    toInternResult,
    type InternRunnerList,
} from '../src';

type Fixture = {
    session_response: Record<string, unknown>;
    session_list_response: Record<string, unknown>;
    turn_response: Record<string, unknown>;
    started_turn_response: Record<string, unknown>;
    turn_list_response: Record<string, unknown>;
    event_list_response: Record<string, unknown>;
    turn_abort_response: Record<string, unknown>;
    turn_decision_response: Record<string, unknown>;
    approval_deny_response: Record<string, unknown>;
    approval_list_response: Record<string, unknown>;
    pairing_response: Record<string, unknown>;
    artifact_response: Record<string, unknown>;
    runner_list_response: Record<string, unknown>;
    readiness_response: Record<string, unknown>;
};

async function fixture(): Promise<Fixture> {
    return (await Bun.file(
        new URL(
            '../../../cmd/or3-intern/testdata/service_contract/external-agent-protocol.json',
            import.meta.url
        )
    ).json()) as Fixture;
}

function fetchLike(
    handler: (
        input: string | URL | Request,
        init?: RequestInit
    ) => Promise<Response>
): typeof globalThis.fetch {
    return handler as typeof globalThis.fetch;
}

describe('@or3/intern-client high-level routes', () => {
    it('uses the current service routes and validates shared fixtures', async () => {
        const shared = await fixture();
        const requests: Array<{
            method: string;
            path: string;
            body?: unknown;
            authorization: string | null;
        }> = [];
        const capabilityResponse = {
            runtimeProfile: 'local-dev',
            hosted: false,
            hostId: 'host_fixture',
            approvalBroker: {},
            approvals: {},
            execAvailable: true,
            sandboxEnabled: false,
            sandboxRequired: false,
            networkPolicy: {},
            future_capability: true,
        };
        const healthResponse = {
            status: 'ok',
            runtimeAvailable: true,
            jobRegistryAvailable: true,
            approvalBrokerAvailable: true,
            processId: 123,
            startedAt: '2026-07-27T10:00:00Z',
        };

        const client = createInternClient({
            baseUrl: 'https://host.example',
            resolveAuth: () => ({ token: 'fixture-token' }),
            fetch: fetchLike(async (input, init) => {
                const url = new URL(String(input));
                const method = init?.method ?? 'GET';
                const body =
                    typeof init?.body === 'string'
                        ? JSON.parse(init.body)
                        : undefined;
                requests.push({
                    method,
                    path: `${url.pathname}${url.search}`,
                    body,
                    authorization:
                        new Headers(init?.headers).get('Authorization'),
                });

                const route = `${method} ${url.pathname}`;
                switch (route) {
                    case 'GET /internal/v1/health':
                        return Response.json(healthResponse);
                    case 'GET /internal/v1/readiness':
                        return Response.json(shared.readiness_response, {
                            status: 503,
                        });
                    case 'GET /internal/v1/capabilities':
                        return Response.json(capabilityResponse);
                    case 'GET /internal/v1/app/bootstrap':
                        return Response.json({
                            host: { id: 'host_fixture' },
                            future_bootstrap: true,
                        });
                    case 'GET /internal/v1/chat-runners':
                        return Response.json(shared.runner_list_response);
                    case 'GET /internal/v1/runner-chat/sessions':
                        return Response.json(shared.session_list_response);
                    case 'POST /internal/v1/runner-chat/sessions':
                    case 'GET /internal/v1/runner-chat/sessions/rcs_fixture':
                        return Response.json(shared.session_response, {
                            status: method === 'POST' ? 201 : 200,
                        });
                    case 'GET /internal/v1/runner-chat/sessions/rcs_fixture/turns':
                        return Response.json(shared.turn_list_response);
                    case 'POST /internal/v1/runner-chat/sessions/rcs_fixture/turns':
                        return Response.json(shared.started_turn_response, {
                            status: 202,
                        });
                    case 'GET /internal/v1/runner-chat/sessions/rcs_fixture/turns/rct_fixture':
                        return Response.json(shared.turn_response);
                    case 'GET /internal/v1/runner-chat/sessions/rcs_fixture/turns/rct_fixture/events':
                        return Response.json(shared.event_list_response);
                    case 'POST /internal/v1/runner-chat/sessions/rcs_fixture/turns/rct_fixture/abort':
                        return Response.json(shared.turn_abort_response, {
                            status: 202,
                        });
                    case 'POST /internal/v1/runner-chat/sessions/rcs_fixture/turns/rct_fixture/approve':
                        return Response.json(shared.turn_decision_response, {
                            status: 202,
                        });
                    case 'GET /internal/v1/artifacts/artifact_fixture':
                        return Response.json(shared.artifact_response);
                    case 'GET /internal/v1/approvals':
                        return Response.json(shared.approval_list_response);
                    case 'POST /internal/v1/approvals/42/deny':
                        return Response.json(shared.approval_deny_response);
                    case 'POST /internal/v1/secure-connections/pairing/approve':
                        return Response.json(shared.pairing_response, {
                            status: 201,
                        });
                    default:
                        return Response.json(
                            { error: `Unhandled test route ${route}` },
                            { status: 500 }
                        );
                }
            }),
        });

        expect(await client.health()).toMatchObject({ status: 'ok' });
        expect(await client.readiness()).toMatchObject({
            ready: false,
            future_readiness_field: 'preserved',
        });
        expect(
            await client.capabilities({ channel: 'web', trigger: 'manual' })
        ).toMatchObject({
            hostId: 'host_fixture',
            future_capability: true,
        });
        expect(await client.appBootstrap()).toMatchObject({
            future_bootstrap: true,
        });
        const runners = await client.listRunners();
        expect(await client.requireRunner('codex')).toMatchObject({
            id: 'codex',
            future_runner_field: 'preserved',
        });
        expect(findInternRunner(runners, 'codex').id).toBe('codex');

        expect(
            await client.listSessions({
                appSessionKeyPrefix: 'or3-chat:workspace-1:',
                limit: 50,
            })
        ).toMatchObject({ sessions: [{ id: 'rcs_fixture' }] });
        expect(
            await client.createSession({
                app_session_key: 'or3-chat:workspace-1',
                runner_id: 'codex',
            })
        ).toMatchObject({ id: 'rcs_fixture' });
        expect(await client.getSession('rcs_fixture')).toMatchObject({
            runner_id: 'codex',
        });
        expect(
            await client.listTurns('rcs_fixture', { limit: 10 })
        ).toMatchObject({ turns: [{ id: 'rct_fixture' }] });
        expect(
            await client.startTurn('rcs_fixture', {
                user_message: 'inspect',
            })
        ).toMatchObject({ turn_id: 'rct_fixture' });
        expect(
            await client.getTurn('rcs_fixture', 'rct_fixture')
        ).toMatchObject({ status: 'succeeded' });
        expect(
            await client.listTurnEvents(
                'rcs_fixture',
                'rct_fixture',
                { afterSeq: 2, limit: 20 }
            )
        ).toMatchObject({ events: [{ seq: 3 }] });
        expect(
            await client.abortTurn('rcs_fixture', 'rct_fixture')
        ).toMatchObject({ status: 'aborting' });
        expect(
            await client.decideTurn(
                'rcs_fixture',
                'rct_fixture',
                'approve',
                { allow_session: true }
            )
        ).toMatchObject({ decision: 'approve' });
        expect(
            await client.readArtifact('artifact_fixture', {
                sessionKey: 'session_fixture',
                offset: 0,
                maxBytes: 1000,
            })
        ).toMatchObject({
            id: 'artifact_fixture',
            future_artifact_field: 'preserved',
        });
        expect(
            await client.listApprovals({
                status: 'pending',
                type: 'runner_permission',
            })
        ).toMatchObject({ items: [{ id: 42 }] });
        expect(
            await client.decideApproval(42, 'deny', { note: 'no' })
        ).toMatchObject({ request_id: 42, status: 'denied' });
        expect(
            await client.pair({
                rendezvous_id: 'rendezvous',
                pairing_secret: 'pairing-secret',
                proposal: { device_name: 'OR3 Chat' },
                trust_level: 'trusted',
            })
        ).toMatchObject({ certificate_hash: 'certificate_fixture_hash' });

        expect(requests).toContainEqual(
            expect.objectContaining({
                method: 'GET',
                path: '/internal/v1/readiness',
            })
        );
        expect(requests).toContainEqual(
            expect.objectContaining({
                method: 'GET',
                path: '/internal/v1/capabilities?channel=web&trigger=manual',
            })
        );
        expect(requests).toContainEqual(
            expect.objectContaining({
                method: 'GET',
                path: '/internal/v1/runner-chat/sessions?app_session_key_prefix=or3-chat%3Aworkspace-1%3A&limit=50',
            })
        );
        expect(requests).toContainEqual(
            expect.objectContaining({
                method: 'GET',
                path: '/internal/v1/runner-chat/sessions/rcs_fixture/turns?limit=10',
            })
        );
        expect(requests).toContainEqual(
            expect.objectContaining({
                method: 'GET',
                path: '/internal/v1/runner-chat/sessions/rcs_fixture/turns/rct_fixture/events?after_seq=2&limit=20',
            })
        );
        expect(requests).toContainEqual(
            expect.objectContaining({
                method: 'GET',
                path: '/internal/v1/artifacts/artifact_fixture?session_key=session_fixture&offset=0&max_bytes=1000',
            })
        );
        expect(
            requests.find(
                (item) =>
                    item.path ===
                    '/internal/v1/secure-connections/pairing/approve'
            )?.authorization
        ).toBeNull();
        expect(
            requests
                .filter(
                    (item) =>
                        item.path !==
                        '/internal/v1/secure-connections/pairing/approve'
                )
                .every(
                    (item) => item.authorization === 'Bearer fixture-token'
                )
        ).toBeTrue();
    });

    it('returns typed unavailable results for absent capabilities and providers', async () => {
        const list: InternRunnerList = {
            runners: [],
            future_runner_list_field: 'preserved',
        };
        expect(() => findInternRunner(list, 'future-provider')).toThrow(
            InternUnavailableError
        );
        expect(() =>
            requireInternRunner(
                {
                    runners: [
                        {
                            id: 'future-provider',
                            display_name: 'Future Provider',
                            status: 'future_status',
                            auth_status: 'unknown',
                            supports: {},
                        },
                    ],
                },
                'future-provider'
            )
        ).toThrow(InternUnavailableError);
        const result = requireInternCapability(
            undefined,
            'runner:future-provider'
        );
        expect(result).toMatchObject({
            ok: false,
            error: {
                code: 'unavailable',
                capability: 'runner:future-provider',
            },
        });
        const caught = await toInternResult(
            Promise.reject(
                new InternUnavailableError('Not advertised', {
                    capability: 'dangerous-mode',
                })
            )
        );
        expect(caught).toMatchObject({
            ok: false,
            error: {
                code: 'unavailable',
                capability: 'dangerous-mode',
            },
        });
    });

    it('validates numeric query inputs before transport', async () => {
        const client = createInternClient({
            baseUrl: 'https://host.example',
            fetch: fetchLike(async () => Response.json({})),
        });
        await expect(
            client.listTurnEvents('session', 'turn', { afterSeq: -1 })
        ).rejects.toMatchObject({ code: 'validation_failed' });
        await expect(
            client.listTurns('session', { limit: 0 })
        ).rejects.toMatchObject({ code: 'validation_failed' });
        await expect(
            client.listSessions({ limit: 101 })
        ).rejects.toMatchObject({ code: 'validation_failed' });
        await expect(
            client.listSessions({ appSessionKeyPrefix: '   ' })
        ).rejects.toMatchObject({ code: 'validation_failed' });
        await expect(
            client.listSessions({
                appSessionKeyPrefix: 'x'.repeat(257),
            })
        ).rejects.toMatchObject({ code: 'validation_failed' });
    });
});
