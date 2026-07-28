import { describe, expect, it } from 'bun:test';
import {
    InternClientError,
    createInternClient,
    createInternTransport,
    parseInternSseBlock,
    readInternSseStream,
    redactInternSecrets,
    safeInternStringify,
} from '../src';

function fetchLike(
    handler: (
        input: string | URL | Request,
        init?: RequestInit
    ) => Promise<Response>
): typeof globalThis.fetch {
    return handler as typeof globalThis.fetch;
}

function sseResponse(
    chunks: readonly string[],
    status = 200
): Response {
    const encoder = new TextEncoder();
    return new Response(
        new ReadableStream<Uint8Array>({
            start(controller) {
                for (const chunk of chunks) {
                    controller.enqueue(encoder.encode(chunk));
                }
                controller.close();
            },
        }),
        {
            status,
            headers: { 'Content-Type': 'text/event-stream' },
        }
    );
}

async function collect<T>(values: AsyncIterable<T>): Promise<T[]> {
    const output: T[] = [];
    for await (const value of values) output.push(value);
    return output;
}

describe('@or3/intern-client transport', () => {
    it('injects header auth, JSON bodies, callbacks, and never puts tokens in URLs', async () => {
        const calls: Array<{
            url: string;
            init?: RequestInit;
        }> = [];
        const responses: number[] = [];
        const transport = createInternTransport({
            baseUrl: 'https://host.example/',
            resolveAuth: async () => ({
                token: 'super-secret-token',
                headers: { 'X-Or3-Session': 'session-header' },
            }),
            fetch: fetchLike(async (input, init) => {
                calls.push({ url: String(input), init });
                return Response.json({ ok: true, future: 1 });
            }),
        });

        const result = await transport.request<{
            ok: boolean;
            future: number;
        }>('/internal/v1/example?limit=2', {
            method: 'POST',
            body: { message: 'hello' },
            headers: { 'X-Client': 'or3-chat' },
            onResponse: ({ response }) => {
                responses.push(response.status);
            },
        });

        expect(result).toEqual({ ok: true, future: 1 });
        expect(calls).toHaveLength(1);
        expect(calls[0]?.url).toBe(
            'https://host.example/internal/v1/example?limit=2'
        );
        expect(calls[0]?.url).not.toContain('super-secret-token');
        const headers = new Headers(calls[0]?.init?.headers);
        expect(headers.get('Authorization')).toBe(
            'Bearer super-secret-token'
        );
        expect(headers.get('X-Or3-Session')).toBe('session-header');
        expect(headers.get('X-Client')).toBe('or3-chat');
        expect(calls[0]?.init?.body).toBe('{"message":"hello"}');
        expect(responses).toEqual([200]);
    });

    it('does not resolve or send credentials when auth is disabled', async () => {
        let authCalls = 0;
        let sentHeaders = new Headers();
        const transport = createInternTransport({
            baseUrl: 'https://host.example',
            resolveAuth: () => {
                authCalls += 1;
                return { token: 'must-not-be-sent' };
            },
            fetch: fetchLike(async (_input, init) => {
                sentHeaders = new Headers(init?.headers);
                return Response.json({ paired: true });
            }),
        });

        await transport.request('/pair', { requireAuth: false });
        expect(authCalls).toBe(0);
        expect(sentHeaders.has('Authorization')).toBeFalse();
    });

    it('passes FormData through without forcing a JSON content type', async () => {
        const form = new FormData();
        form.set('session_key', 'session');
        form.set('file', new Blob(['hello'], { type: 'text/plain' }), 'a.txt');
        let body: BodyInit | null | undefined;
        let headers = new Headers();
        const transport = createInternTransport({
            baseUrl: 'https://host.example',
            fetch: fetchLike(async (_input, init) => {
                body = init?.body;
                headers = new Headers(init?.headers);
                return Response.json({ ok: true });
            }),
        });
        await transport.request('/upload', {
            method: 'POST',
            body: form,
            requireAuth: false,
        });
        expect(body).toBe(form);
        expect(headers.has('Content-Type')).toBeFalse();
    });

    it('rejects credential-bearing host and request URLs', () => {
        expect(() =>
            createInternTransport({
                baseUrl: 'https://user:password@host.example',
            }).buildUrl('/health')
        ).toThrow('credentials');
        const transport = createInternTransport({
            baseUrl: 'https://host.example',
        });
        expect(() =>
            transport.buildUrl('/health?access_token=secret')
        ).toThrow('never query parameters');
        expect(() =>
            transport.buildUrl('https://other.example/health')
        ).toThrow('relative path');
    });

    it('preserves response status/payload in typed errors after redaction', async () => {
        const token = 'token-that-must-not-leak';
        const transport = createInternTransport({
            baseUrl: 'https://host.example',
            resolveAuth: () => ({ token }),
            fetch: fetchLike(async () =>
                Response.json(
                    {
                        code: 'invalid_token',
                        error: `Bearer ${token} is invalid`,
                        token,
                        future_error_field: 'preserved',
                    },
                    {
                        status: 401,
                        headers: { 'X-Request-Id': 'req_fixture' },
                    }
                )
            ),
        });

        let failure: unknown;
        try {
            await transport.request('/internal/v1/health');
        } catch (error) {
            failure = error;
        }
        expect(failure).toBeInstanceOf(InternClientError);
        const typed = failure as InternClientError;
        expect(typed.code).toBe('unauthorized');
        expect(typed.status).toBe(401);
        expect(typed.remoteCode).toBe('invalid_token');
        expect(typed.requestId).toBe('req_fixture');
        expect(typed.details).toMatchObject({
            status: 401,
            payload: {
                token: '[REDACTED]',
                future_error_field: 'preserved',
            },
        });
        expect(`${typed.message} ${JSON.stringify(typed.details)}`).not.toContain(
            token
        );
    });

    it('classifies timeouts and caller aborts independently', async () => {
        const hangingFetch = fetchLike(
            async (_input, init) =>
                await new Promise<Response>((_resolve, reject) => {
                    if (init?.signal?.aborted) {
                        reject(init.signal.reason);
                        return;
                    }
                    init?.signal?.addEventListener(
                        'abort',
                        () => reject(init.signal?.reason),
                        { once: true }
                    );
                })
        );
        const transport = createInternTransport({
            baseUrl: 'https://host.example',
            fetch: hangingFetch,
        });

        await expect(
            transport.request('/slow', {
                requireAuth: false,
                timeoutMs: 5,
            })
        ).rejects.toMatchObject({ code: 'timeout', retryable: true });

        const controller = new AbortController();
        const pending = transport.request('/abort', {
            requireAuth: false,
            timeoutMs: 1_000,
            signal: controller.signal,
        });
        controller.abort();
        await expect(pending).rejects.toMatchObject({ code: 'aborted' });
    });
});

describe('@or3/intern-client SSE', () => {
    it('parses standard fields, multiline data, comments, retry, and chunk boundaries', async () => {
        expect(
            parseInternSseBlock(
                'id: 7\nevent: delta\nretry: 1500\ndata: first\ndata: second'
            )
        ).toEqual({
            id: '7',
            cursor: '7',
            event: 'delta',
            retry: 1500,
            data: 'first\nsecond',
            json: undefined,
        });

        const events = await collect(
            readInternSseStream<{ value: number }>(
                sseResponse([
                    ': keepalive\r\n\r',
                    '\nevent: one\r\ndata: {"value":1}\r\n\r\n',
                    'event: two\ndata: {"value":2}\n',
                    '\n',
                ]).body!
            )
        );
        expect(events.map((event) => event.json?.value)).toEqual([1, 2]);
    });

    it('reconnects with after_seq, deduplicates replay, and stops on done', async () => {
        const urls: string[] = [];
        const authHeaders: string[] = [];
        const delays: number[] = [];
        const openStatuses: number[] = [];
        let call = 0;
        const client = createInternClient({
            baseUrl: 'https://host.example',
            resolveAuth: () => ({ token: 'stream-secret' }),
            fetch: fetchLike(async (input, init) => {
                urls.push(String(input));
                authHeaders.push(
                    new Headers(init?.headers).get('Authorization') ?? ''
                );
                call += 1;
                if (call === 1) {
                    return sseResponse([
                        'event: content.delta\ndata: {"id":1,"turn_id":"turn","seq":1,"ts":1,"type":"content.delta","text":"a"}\n\n',
                        'event: content.delta\ndata: {"id":2,"turn_id":"turn","seq":2,"ts":2,"type":"content.delta","text":"b"}\n\n',
                    ]);
                }
                return sseResponse([
                    'event: content.delta\ndata: {"id":2,"turn_id":"turn","seq":2,"ts":2,"type":"content.delta","text":"b"}\n\n',
                    'event: content.delta\ndata: {"id":3,"turn_id":"turn","seq":3,"ts":3,"type":"content.delta","text":"c","future_event_field":true}\n\n',
                    'event: done\ndata: {"status":"succeeded","future_done_field":true}\n\n',
                ]);
            }),
            clock: {
                random: () => 0.5,
                sleep: async (milliseconds) => {
                    delays.push(milliseconds);
                },
            },
        });

        const events = await collect(
            client.streamTurn('session', 'turn', {
                onResponse: ({ response }) => {
                    openStatuses.push(response.status);
                },
                reconnect: {
                    maxAttempts: 2,
                    minDelayMs: 10,
                    maxDelayMs: 10,
                    jitter: 0,
                },
            })
        );

        expect(events.map((event) => event.event)).toEqual([
            'content.delta',
            'content.delta',
            'content.delta',
            'done',
        ]);
        expect(events[2]?.json).toMatchObject({
            seq: 3,
            future_event_field: true,
        });
        expect(events[3]?.json).toMatchObject({
            status: 'succeeded',
            future_done_field: true,
        });
        expect(urls).toEqual([
            'https://host.example/internal/v1/runner-chat/sessions/session/turns/turn/stream',
            'https://host.example/internal/v1/runner-chat/sessions/session/turns/turn/stream?after_seq=2',
        ]);
        expect(urls.join(' ')).not.toContain('stream-secret');
        expect(authHeaders).toEqual([
            'Bearer stream-secret',
            'Bearer stream-secret',
        ]);
        expect(delays).toEqual([10]);
        expect(openStatuses).toEqual([200, 200]);
    });

    it('uses Last-Event-ID when requested and bounds reconnect attempts', async () => {
        const headers: Array<string | null> = [];
        let calls = 0;
        const transport = createInternTransport({
            baseUrl: 'https://host.example',
            fetch: fetchLike(async (_input, init) => {
                calls += 1;
                headers.push(
                    new Headers(init?.headers).get('Last-Event-ID')
                );
                return sseResponse([
                    'id: 9\nevent: delta\ndata: {"id":9}\n\n',
                ]);
            }),
            clock: {
                random: () => 0.5,
                sleep: async () => undefined,
            },
        });

        await expect(
            collect(
                transport.stream('/events', {
                    requireAuth: false,
                    reconnect: {
                        maxAttempts: 1,
                        minDelayMs: 0,
                        maxDelayMs: 0,
                        jitter: 0,
                    },
                    resume: {
                        initialCursor: 8,
                        queryParameter: 'after',
                        sendLastEventId: true,
                    },
                    dedupe: true,
                    isTerminal: () => false,
                })
            )
        ).rejects.toMatchObject({ code: 'offline' });
        expect(calls).toBe(2);
        expect(headers).toEqual(['8', '9']);
    });

    it('times out a stream connection before response headers arrive', async () => {
        const hangingFetch = fetchLike(
            async (_input, init) =>
                await new Promise<Response>((_resolve, reject) => {
                    if (init?.signal?.aborted) {
                        reject(init.signal.reason);
                        return;
                    }
                    init?.signal?.addEventListener(
                        'abort',
                        () => reject(init.signal?.reason),
                        { once: true }
                    );
                })
        );
        const transport = createInternTransport({
            baseUrl: 'https://host.example',
            fetch: hangingFetch,
            streamConnectTimeoutMs: 5,
        });
        await expect(
            collect(
                transport.stream('/events', {
                    requireAuth: false,
                })
            )
        ).rejects.toMatchObject({ code: 'timeout', retryable: true });
    });
});

describe('@or3/intern-client redaction', () => {
    it('redacts structured fields, bearer values, URL params, explicit secrets, and cycles', () => {
        const circular: Record<string, unknown> = {
            Authorization: 'Bearer abc',
            nested: {
                pairing_secret: 'pair',
                paired_token: 'paired-value',
                url: 'https://host/path?access_token=url-secret&limit=1',
                message: 'token=plain-secret',
            },
        };
        circular.self = circular;
        const redacted = redactInternSecrets(circular, ['plain-secret']);
        const serialized = safeInternStringify(redacted);
        expect(serialized).not.toContain('abc');
        expect(serialized).not.toContain(':"pair"');
        expect(serialized).not.toContain('url-secret');
        expect(serialized).not.toContain('paired-value');
        expect(serialized).not.toContain('plain-secret');
        expect(serialized).toContain('[REDACTED]');
        expect(serialized).toContain('[Circular]');
    });
});
