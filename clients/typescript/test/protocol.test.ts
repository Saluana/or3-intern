import { describe, expect, it } from 'bun:test';
import {
    InternProtocolError,
    parseInternActionAcknowledgement,
    parseInternApprovalList,
    parseInternArtifact,
    parseInternApprovalDecision,
    parseInternCapabilities,
    parseInternEvent,
    parseInternEventList,
    parseInternHealth,
    parseInternPairResult,
    parseInternReadiness,
    parseInternRunnerList,
    parseInternSession,
    parseInternSessionList,
    parseInternStartedTurn,
    parseInternTurn,
    parseInternTurnDecision,
    parseInternTurnList,
} from '../src';

type Fixture = {
    session_response: unknown;
    session_list_response: unknown;
    turn_response: unknown;
    started_turn_response: unknown;
    turn_list_response: unknown;
    event_response: unknown;
    event_list_response: unknown;
    turn_abort_response: unknown;
    turn_decision_response: unknown;
    approval_deny_response: unknown;
    approval_list_response: unknown;
    pairing_response: unknown;
    artifact_response: unknown;
    runner_list_response: unknown;
    readiness_response: unknown;
    health_response: unknown;
    capabilities_response: unknown;
    capabilities_required: string[];
    health_required: string[];
};

async function fixture(): Promise<Fixture> {
    return (await Bun.file(
        new URL(
            '../../../cmd/or3-intern/testdata/service_contract/external-agent-protocol.json',
            import.meta.url
        )
    ).json()) as Fixture;
}

describe('@or3/intern-client protocol schemas', () => {
    it('parses the shared session, event, and approval fixtures', async () => {
        const input = await fixture();
        expect(parseInternSession(input.session_response)).toMatchObject({
            id: 'rcs_fixture',
            runner_id: 'codex',
            mode: 'review',
        });
        expect(
            parseInternSessionList(input.session_list_response)
        ).toMatchObject({
            sessions: [{ id: 'rcs_fixture', runner_id: 'codex' }],
        });
        expect(parseInternEvent(input.event_response)).toMatchObject({
            turn_id: 'rct_fixture',
            seq: 3,
            type: 'content.delta',
        });
        expect(
            parseInternApprovalDecision(input.approval_deny_response)
        ).toEqual({
            request_id: 42,
            status: 'denied',
            token: undefined,
            allowlist_id: undefined,
            session_key: undefined,
        });
        expect(parseInternTurn(input.turn_response)).toMatchObject({
            id: 'rct_fixture',
            status: 'succeeded',
            assistant_message_id: 12,
        });
        expect(parseInternStartedTurn(input.started_turn_response)).toEqual({
            job_id: 'job_fixture',
            session_id: 'rcs_fixture',
            status: 'queued',
            turn_id: 'rct_fixture',
        });
        expect(parseInternTurnList(input.turn_list_response).turns).toHaveLength(
            1
        );
        expect(
            parseInternEventList(input.event_list_response).events
        ).toHaveLength(1);
        expect(
            parseInternActionAcknowledgement(input.turn_abort_response).status
        ).toBe('aborting');
        expect(
            parseInternTurnDecision(input.turn_decision_response)
        ).toMatchObject({
            decision: 'approve',
            native_continued: true,
        });
        expect(
            parseInternApprovalList(input.approval_list_response).items[0]
        ).toMatchObject({
            id: 42,
            status: 'pending',
        });
        expect(parseInternPairResult(input.pairing_response)).toMatchObject({
            certificate_hash: 'certificate_fixture_hash',
        });
        expect(parseInternArtifact(input.artifact_response)).toMatchObject({
            id: 'artifact_fixture',
            future_artifact_field: 'preserved',
        });
        expect(parseInternReadiness(input.readiness_response)).toMatchObject({
            ready: false,
            future_readiness_field: 'preserved',
        });
        expect(parseInternRunnerList(input.runner_list_response)).toMatchObject({
            default_runner: 'codex',
            runners: [
                {
                    id: 'codex',
                    chat_capabilities: {
                        approvalDecisions: true,
                        cancel: true,
                        customCwd: true,
                    },
                    future_runner_field: 'preserved',
                },
            ],
            future_runner_list_field: true,
        });
        expect(parseInternHealth(input.health_response)).toMatchObject({
            status: 'ok',
            future_health_field: 'preserved',
        });
        expect(
            parseInternCapabilities(input.capabilities_response)
        ).toMatchObject({
            hostId: 'host_fixture',
            future_capability_field: 'preserved',
        });
    });

    it('validates required health and capability fields while preserving additions', async () => {
        const input = await fixture();
        const health = Object.fromEntries(
            input.health_required.map((key) => [
                key,
                key === 'processId'
                    ? 123
                    : key.endsWith('Available')
                      ? true
                      : key === 'startedAt'
                        ? '2026-07-27T10:00:00Z'
                        : 'ok',
            ])
        );
        const parsedHealth = parseInternHealth({
            ...health,
            future_field: 'preserved',
        });
        expect(parsedHealth.future_field).toBe('preserved');

        const capabilities = Object.fromEntries(
            input.capabilities_required.map((key) => [
                key,
                key === 'hosted' ||
                key === 'execAvailable' ||
                key === 'sandboxEnabled' ||
                key === 'sandboxRequired'
                    ? false
                    : key === 'approvalBroker' ||
                        key === 'approvals' ||
                        key === 'networkPolicy'
                      ? {}
                      : 'value',
            ])
        );
        expect(
            parseInternCapabilities({
                ...capabilities,
                future_capability: { enabled: true },
            }).future_capability
        ).toEqual({ enabled: true });
    });

    it('rejects malformed boundary data with a stable protocol error', () => {
        expect(() => parseInternSession({ id: 1 })).toThrow(
            InternProtocolError
        );
        expect(() =>
            parseInternHealth({
                status: 'ok',
                runtimeAvailable: 'yes',
            })
        ).toThrow('health.runtimeAvailable');
    });
});
