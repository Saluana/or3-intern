import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { EventEmitter } from 'node:events';
import { chmod, mkdir, readFile, stat, symlink, utimes, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';

import {
    CLOUDFLARED_VERSION,
    cachedVersionMatches,
    downloadVerifiedToFile,
    ensureCloudflared,
    requestWithRetry,
    runCLI,
    withInstallLock,
} from '../bin/or3.js';

test('installed npm bin symlink invokes the bootstrap', async (t) => {
    const directory = await makeTestDirectory(t);
    const intern = join(directory, 'or3-intern');
    await writeFile(intern, '#!/bin/sh\nprintf "forwarded:%s\\n" "$*"\n', { mode: 0o755 });
    await chmod(intern, 0o755);

    const link = join(directory, 'or3');
    await symlink(new URL('../bin/or3.js', import.meta.url), link);
    const result = spawnSync(link, ['status'], {
        encoding: 'utf8',
        env: {
            ...process.env,
            OR3_INTERN_BIN: intern,
        },
    });

    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /OR3 Connect/);
    assert.match(result.stdout, /forwarded:connect status/);
});

test('rejects an unsupported nested connect runtime before bootstrap side effects', async () => {
    let output = '';
    const code = await runCLI(['connect', 'unknown-runtime'], {
        stdout: { write(value) { output += value; } },
        stderr: { write(value) { output += value; } },
    });

    assert.equal(code, 2);
    assert.match(output, /intern \[command\]/);
});

test('routes explicitly configured external runtime commands through connect', async (t) => {
    const installDir = await makeTestDirectory(t);
    const binary = join(installDir, 'or3-intern');
    await writeFile(binary, '#!/bin/sh\nprintf "forwarded:%s\\n" "$*"\n', { mode: 0o755 });
    await chmod(binary, 0o755);
    const cloudflared = join(installDir, 'cloudflared');
    await writeFile(cloudflared, '#!/bin/sh\nexit 0\n', { mode: 0o755 });
    await chmod(cloudflared, 0o755);
    await writeFile(`${cloudflared}.version`, `${CLOUDFLARED_VERSION}\n`, { mode: 0o600 });
    const previousIntern = process.env.OR3_INTERN_BIN;
    process.env.OR3_INTERN_BIN = binary;
    t.after(() => {
        if (previousIntern === undefined) delete process.env.OR3_INTERN_BIN;
        else process.env.OR3_INTERN_BIN = previousIntern;
    });
    let output = '';
    const code = await runCLI(['hermes', '--cloud-url', 'https://staging.example.test'], {
        installDir,
        fetchImpl: async () => { throw new Error('offline'); },
        stdout: { write(value) { output += value; } },
        stderr: { write(value) { output += value; } },
        spawnImpl: (_command, args) => {
            output += `forwarded:${args.join(' ')}\n`;
            const child = new EventEmitter();
            queueMicrotask(() => child.emit('exit', 0, null));
            return child;
        },
    });

    assert.equal(code, 0);
    assert.match(output, /forwarded:connect hermes/);
});

test('withholds the default remote bootstrap before downloading anything', async () => {
    let output = '';
    let fetches = 0;
    const code = await runCLI([], {
        fetchImpl: async () => {
            fetches++;
            throw new Error('the withheld path must not download releases');
        },
        stdout: { write(value) { output += value; } },
        stderr: { write(value) { output += value; } },
    });

    assert.equal(code, 2);
    assert.equal(fetches, 0);
    assert.match(output, /not enabled by default/);
});

test('runs Intern setup without Go, a PATH change, or a Cloudflare download', async (t) => {
    const installDir = await makeTestDirectory(t);
    const binary = join(installDir, 'or3-intern');
    await writeFile(binary, '#!/bin/sh\nprintf "forwarded:%s\\n" "$*"\n', { mode: 0o755 });
    await chmod(binary, 0o755);
    const previousIntern = process.env.OR3_INTERN_BIN;
    process.env.OR3_INTERN_BIN = binary;
    t.after(() => {
        if (previousIntern === undefined) delete process.env.OR3_INTERN_BIN;
        else process.env.OR3_INTERN_BIN = previousIntern;
    });

    let output = '';
    let fetches = 0;
    const code = await runCLI(['intern'], {
        installDir,
        fetchImpl: async () => {
            fetches++;
            throw new Error('Cloudflare download should not run');
        },
        stdout: { write(value) { output += value; } },
        stderr: { write(value) { output += value; } },
        spawnImpl: (_command, args) => {
            output += `forwarded:${args.join(' ')}\n`;
            const child = new EventEmitter();
            queueMicrotask(() => child.emit('exit', 0, null));
            return child;
        },
    });

    assert.equal(code, 0);
    assert.equal(fetches, 0);
    assert.match(output, /OR3 Intern/);
    assert.match(output, /forwarded:setup/);
});

test('all cached management commands start offline without a shell PATH install', async (t) => {
    const installDir = await makeTestDirectory(t);
    const binary = join(installDir, 'or3-intern');
    await writeFile(binary, '#!/bin/sh\nexit 0\n', { mode: 0o755 });
    await chmod(binary, 0o755);
    await writeFile(`${binary}.version`, 'v0.1.1\n', { mode: 0o600 });
    const cloudflared = join(installDir, 'cloudflared');
    await writeFile(cloudflared, '#!/bin/sh\nexit 0\n', { mode: 0o755 });
    await chmod(cloudflared, 0o755);
    await writeFile(`${cloudflared}.version`, `${CLOUDFLARED_VERSION}\n`, { mode: 0o600 });

    let fetches = 0;
    const invocations = [];
    const spawnImpl = (command, args, options) => {
        invocations.push({ command, args, options });
        const child = new EventEmitter();
        queueMicrotask(() => child.emit('exit', 0, null));
        return child;
    };
    const output = { write() {} };
    for (const subcommand of ['status', 'doctor', 'disconnect', 'uninstall']) {
        const code = await runCLI([subcommand], {
            installDir,
            fetchImpl: async () => {
                fetches++;
                throw new Error('offline');
            },
            spawnImpl,
            stdout: output,
            stderr: output,
        });
        assert.equal(code, 0);
    }
    assert.equal(fetches, 0);
    assert.deepEqual(
        invocations.map((invocation) => invocation.args),
        [
            ['connect', 'status'],
            ['connect', 'doctor'],
            ['connect', 'disconnect'],
            ['connect', 'uninstall'],
        ]
    );
    for (const invocation of invocations) {
        assert.equal(invocation.command, binary);
        assert.match(invocation.options.env.PATH, new RegExp(`^${escapeRegExp(installDir)}`));
    }
});

test('cache validation uses file fingerprints and accepts a legacy version cache', async (t) => {
    const directory = await makeTestDirectory(t);
    const binary = join(directory, 'or3-intern');
    const versionFile = `${binary}.version`;
    await writeFile(binary, Buffer.alloc(4 * 1024 * 1024, 7), { mode: 0o711 });
    await chmod(binary, 0o711);
    await writeFile(versionFile, 'v-test\n', { mode: 0o600 });

    assert.equal(await cachedVersionMatches(versionFile, 'v-test', binary), true);
    assert.equal(await cachedVersionMatches(versionFile, 'v-other', binary), false);
});

test('shared install lock serializes concurrent installers', async (t) => {
    const directory = await makeTestDirectory(t);
    const order = [];
    let releaseFirst;
    const firstMayFinish = new Promise((resolvePromise) => {
        releaseFirst = resolvePromise;
    });
    let firstEntered;
    const entered = new Promise((resolvePromise) => {
        firstEntered = resolvePromise;
    });

    const first = withInstallLock(directory, async () => {
        order.push('first-enter');
        firstEntered();
        await firstMayFinish;
        order.push('first-exit');
    }, { timeoutMs: 1_000, pollMs: 5 });
    await entered;
    const second = withInstallLock(directory, async () => {
        order.push('second-enter');
    }, { timeoutMs: 1_000, pollMs: 5 });
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 30));
    assert.deepEqual(order, ['first-enter']);
    releaseFirst();
    await Promise.all([first, second]);
    assert.deepEqual(order, ['first-enter', 'first-exit', 'second-enter']);
});

test('installer recovers only a stale lock owned by a dead process', async (t) => {
    const directory = await makeTestDirectory(t);
    const lockPath = join(directory, '.install.lock');
    await mkdir(lockPath, { mode: 0o700 });
    const exitedOwner = spawnSync(process.execPath, ['-e', '']);
    assert.equal(exitedOwner.status, 0);
    await writeFile(
        join(lockPath, 'owner.json'),
        `${JSON.stringify({
            pid: exitedOwner.pid,
            startedAt: '2000-01-01T00:00:00.000Z',
        })}\n`,
        { mode: 0o600 }
    );
    const old = new Date('2000-01-01T00:00:00.000Z');
    await utimes(lockPath, old, old);
    let entered = false;
    await withInstallLock(directory, async () => {
        entered = true;
    }, { timeoutMs: 500, staleMs: 1, pollMs: 5 });
    assert.equal(entered, true);
});

test('concurrent bootstraps produce one valid atomic cache', async (t) => {
    const previousOverride = process.env.OR3_CONNECT_CLOUDFLARED_BIN;
    delete process.env.OR3_CONNECT_CLOUDFLARED_BIN;
    t.after(() => {
        if (previousOverride === undefined) {
            delete process.env.OR3_CONNECT_CLOUDFLARED_BIN;
        } else {
            process.env.OR3_CONNECT_CLOUDFLARED_BIN = previousOverride;
        }
    });
    const installDir = await makeTestDirectory(t);
    const sourceDir = join(installDir, 'fixture');
    await mkdir(sourceDir, { mode: 0o700 });
    const expectedBody = Buffer.from('#!/bin/sh\necho cloudflared fixture\n');
    await writeFile(join(sourceDir, 'cloudflared'), expectedBody, { mode: 0o755 });

    let assetBody = expectedBody;
    const architecture = process.arch === 'x64' ? 'amd64' : process.arch;
    let assetName = `cloudflared-linux-${architecture}`;
    if (process.platform === 'darwin') {
        assetName = `cloudflared-darwin-${architecture}.tgz`;
        const archive = join(installDir, 'cloudflared-fixture.tgz');
        const result = spawnSync('tar', ['-czf', archive, '-C', sourceDir, 'cloudflared']);
        assert.equal(result.status, 0, result.stderr?.toString());
        assetBody = await readFile(archive);
    }
    const digest = createHash('sha256').update(assetBody).digest('hex');
    let releaseFetches = 0;
    let assetFetches = 0;
    const fetchImpl = async (url) => {
        if (String(url).includes('api.github.com')) {
            releaseFetches++;
            return new Response(JSON.stringify({
                tag_name: CLOUDFLARED_VERSION,
                assets: [{
                    name: assetName,
                    browser_download_url: 'https://downloads.example/cloudflared',
                    digest: `sha256:${digest}`,
                    size: assetBody.length,
                }],
            }), { status: 200 });
        }
        assetFetches++;
        return new Response(assetBody, { status: 200 });
    };
    const options = {
        installDir,
        fetchImpl,
        stdout: { write() {} },
        requestPolicy: { retries: 0, timeoutMs: 2_000 },
        lockPolicy: { timeoutMs: 2_000, pollMs: 5 },
    };
    const [first, second] = await Promise.all([
        ensureCloudflared(options),
        ensureCloudflared(options),
    ]);

    assert.equal(first, join(installDir, 'cloudflared'));
    assert.equal(second, first);
    assert.equal(releaseFetches, 1);
    assert.equal(assetFetches, 1);
    assert.deepEqual(await readFile(first), expectedBody);
    assert.equal(
        await cachedVersionMatches(
            `${first}.version`,
            CLOUDFLARED_VERSION,
            first,
            `${first}.metadata.json`
        ),
        true
    );
});

test('verified downloads stream to disk without arrayBuffer buffering', async (t) => {
    const directory = await makeTestDirectory(t);
    const destination = join(directory, 'asset.bin');
    const chunks = Array.from({ length: 32 }, (_, index) => Buffer.alloc(64 * 1024, index));
    const expectedBody = Buffer.concat(chunks);
    const digest = createHash('sha256').update(expectedBody).digest('hex');
    let index = 0;
    const responseBody = new ReadableStream({
        pull(controller) {
            if (index === chunks.length) {
                controller.close();
                return;
            }
            controller.enqueue(chunks[index++]);
        },
    });
    const fetchImpl = async () => ({
        ok: true,
        status: 200,
        body: responseBody,
        arrayBuffer() {
            throw new Error('arrayBuffer must not be called');
        },
    });
    await downloadVerifiedToFile({
        name: 'asset.bin',
        browser_download_url: 'https://downloads.example/asset.bin',
        digest: `sha256:${digest}`,
        size: expectedBody.length,
    }, destination, {
        fetchImpl,
        requestPolicy: { retries: 0, timeoutMs: 1_000 },
    });

    assert.deepEqual(await readFile(destination), expectedBody);
    assert.equal((await stat(destination)).mode & 0o777, 0o600);
});

test('verified download aborts a stalled body on its deadline and removes partial data', async (t) => {
    const directory = await makeTestDirectory(t);
    const destination = join(directory, 'asset.bin');
    let cancelled = false;
    const fetchImpl = async () => ({
        ok: true,
        status: 200,
        body: new ReadableStream({
            start(controller) {
                controller.enqueue(new Uint8Array([1, 2, 3]));
            },
            cancel() {
                cancelled = true;
            },
        }),
    });
    const started = Date.now();
    await assert.rejects(
        downloadVerifiedToFile({
            name: 'asset.bin',
            browser_download_url: 'https://downloads.example/asset.bin',
            digest: `sha256:${'0'.repeat(64)}`,
            size: 10,
        }, destination, {
            fetchImpl,
            requestPolicy: { retries: 0, timeoutMs: 30 },
        }),
        /network deadline exceeded/
    );
    assert.ok(Date.now() - started < 1_000);
    assert.equal(cancelled, true);
    await assert.rejects(stat(destination), { code: 'ENOENT' });
});

test('safe release requests retry transient HTTP failures with bounded backoff', async () => {
    let attempts = 0;
    const result = await requestWithRetry('https://api.example/release', {
        fetchImpl: async () => {
            attempts++;
            if (attempts === 1) {
                return new Response('', { status: 503 });
            }
            return new Response('{"tag":"v1"}', { status: 200 });
        },
        requestPolicy: { retries: 1, timeoutMs: 500, backoffMs: 1 },
        failureMessage: 'release request failed',
        consume: async (response) => await response.json(),
    });
    assert.deepEqual(result, { tag: 'v1' });
    assert.equal(attempts, 2);
});

async function makeTestDirectory(t) {
    const directory = join(
        tmpdir(),
        `or3-package-test-${process.pid}-${Date.now()}-${Math.random().toString(16).slice(2)}`
    );
    await mkdir(directory, { recursive: true, mode: 0o700 });
    t.after(async () => {
        const { rm } = await import('node:fs/promises');
        await rm(directory, { recursive: true, force: true });
    });
    return directory;
}

function escapeRegExp(value) {
    return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
