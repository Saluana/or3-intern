#!/usr/bin/env node

import { createHash, randomUUID } from 'node:crypto';
import { spawn, spawnSync } from 'node:child_process';
import { createReadStream, createWriteStream, realpathSync } from 'node:fs';
import {
    chmod,
    lstat,
    mkdir,
    mkdtemp,
    open,
    rename,
    rm,
    stat,
    writeFile,
} from 'node:fs/promises';
import { homedir } from 'node:os';
import { basename, delimiter, dirname, join, resolve } from 'node:path';
import { Readable, Transform } from 'node:stream';
import { pipeline } from 'node:stream/promises';
import { fileURLToPath } from 'node:url';

const INTERN_REPOSITORY = 'Saluana/or3-intern';
const CLOUDFLARED_REPOSITORY = 'cloudflare/cloudflared';
export const CLOUDFLARED_VERSION = '2026.5.2';
const SUPPORTED_CONNECT_SUBCOMMANDS = new Set([
    'status',
    'doctor',
    'disconnect',
    'uninstall',
    'openclaw',
    'hermes',
    'run',
    'setup',
]);
const CONNECT_USAGE = 'Usage: npx @or3/connect intern [command] | npx @or3/connect [status|doctor|disconnect|uninstall] [options]\n';
const CONNECT_WITHHELD_MESSAGE = 'Remote OR3 Connect is not enabled by default. Use `npx @or3/connect intern` for local Intern, or pass an explicitly verified staging/self-hosted `--cloud-url` to the advanced Connect command.\n';

const DEFAULT_REQUEST_POLICY = Object.freeze({
    timeoutMs: 20_000,
    retries: 2,
    backoffMs: 250,
});
const DEFAULT_LOCK_POLICY = Object.freeze({
    timeoutMs: 45_000,
    staleMs: 5 * 60_000,
    pollMs: 100,
});
const MAX_RELEASE_ASSET_BYTES = 256 * 1024 * 1024;
const MAX_METADATA_BYTES = 8 * 1024;
const TAR_TIMEOUT_MS = 30_000;
const TAR_MAX_OUTPUT_BYTES = 1024 * 1024;

export async function runCLI(args = process.argv.slice(2), options = {}) {
    const stdout = options.stdout ?? process.stdout;
    const stderr = options.stderr ?? process.stderr;
    const command = args[0] || 'connect';
    const managementCommands = new Set(['status', 'doctor', 'disconnect', 'uninstall']);
    const runtimeCommands = new Set(['openclaw', 'hermes']);
    let forwardedArgs;
    const internCommand = command === 'intern';
    if (internCommand) {
        forwardedArgs = args.slice(1);
        if (forwardedArgs.length === 0) forwardedArgs = ['setup'];
    } else if (command === 'connect') {
        const requestedSubcommand = args[1];
        if (
            requestedSubcommand &&
            !requestedSubcommand.startsWith('-') &&
            !SUPPORTED_CONNECT_SUBCOMMANDS.has(requestedSubcommand)
        ) {
            stderr.write(CONNECT_USAGE);
            return 2;
        }
        forwardedArgs = args.length > 0 ? args : ['connect'];
    } else if (command.startsWith('-')) {
        forwardedArgs = ['connect', ...args];
    } else if (managementCommands.has(command) || runtimeCommands.has(command)) {
        forwardedArgs = ['connect', ...args];
    } else {
        stderr.write(CONNECT_USAGE);
        return 2;
    }

    if (remoteSetupNeedsExplicitCloudURL(args) && !hasExplicitCloudURL(args)) {
        stderr.write(CONNECT_WITHHELD_MESSAGE);
        return 2;
    }

    const installDir = options.installDir ?? getInstallDir();
    stdout.write(internCommand ? 'OR3 Intern\n\n' : 'OR3 Connect\n\n');
    await mkdir(installDir, { recursive: true, mode: 0o700 });

    const sharedOptions = {
        installDir,
        fetchImpl: options.fetchImpl,
        requestPolicy: options.requestPolicy,
        lockPolicy: options.lockPolicy,
        stdout,
    };
    const intern = await ensureIntern(sharedOptions);
    const subcommand = forwardedArgs[1] && !forwardedArgs[1].startsWith('-')
        ? forwardedArgs[1]
        : 'setup';
    let cloudflared = process.env.OR3_CONNECT_CLOUDFLARED_BIN?.trim();
    const isConnectCommand = forwardedArgs[0] === 'connect';
    if (!cloudflared && isConnectCommand && (subcommand === 'setup' || subcommand === 'openclaw' || subcommand === 'hermes' || subcommand === 'doctor' || subcommand === 'run')) {
        cloudflared = await ensureCloudflared(sharedOptions);
    }

    const childEnvironment = {
        ...process.env,
        PATH: `${installDir}${delimiter}${process.env.PATH || ''}`,
    };
    if (cloudflared) {
        childEnvironment.OR3_CONNECT_CLOUDFLARED_BIN = cloudflared;
    }
    return await spawnAndWait(
        options.spawnImpl ?? spawn,
        intern,
        forwardedArgs,
        {
            stdio: options.childStdio ?? 'inherit',
            env: childEnvironment,
        }
    );
}

function remoteSetupNeedsExplicitCloudURL(args) {
    const command = args[0] || 'connect';
    if (command === 'intern') return false;
    if (command === 'connect' || command.startsWith('-')) {
        const subcommand = command === 'connect' ? args[1] : command;
        return !subcommand || subcommand.startsWith('-') || ['setup', 'openclaw', 'hermes'].includes(subcommand);
    }
    return ['openclaw', 'hermes', 'setup'].includes(command);
}

function hasExplicitCloudURL(args) {
    if (process.env.OR3_CONNECT_CLOUD_URL?.trim()) return true;
    const index = args.indexOf('--cloud-url');
    return index >= 0 && Boolean(args[index + 1]?.trim());
}

export async function ensureIntern(options = {}) {
    const override = process.env.OR3_INTERN_BIN?.trim();
    if (override) return override;
    const installDir = options.installDir ?? getInstallDir();
    const packageMetadata = JSON.parse(
        await readSmallText(new URL('../package.json', import.meta.url), MAX_METADATA_BYTES)
    );
    const requestedVersion =
        options.version ??
        process.env.OR3_INTERN_VERSION?.trim() ??
        `v${packageMetadata.version}`;
    const target = join(installDir, 'or3-intern');
    if (await cachedVersionMatches(`${target}.version`, requestedVersion, target, `${target}.metadata.json`)) {
        return target;
    }

    return await withInstallLock(installDir, async () => {
        if (await cachedVersionMatches(`${target}.version`, requestedVersion, target, `${target}.metadata.json`)) {
            return target;
        }
        const release = await githubRelease(INTERN_REPOSITORY, requestedVersion, options);
        if (release.tag_name !== requestedVersion) {
            throw new Error(`release tag ${release.tag_name || '<missing>'} did not match ${requestedVersion}`);
        }
        const platform = normalizePlatform(process.platform);
        const architecture = normalizeArchitecture(process.arch);
        const asset = findAsset(
            release.assets,
            `or3-intern-${platform}-${architecture}.tar.gz`
        );
        options.stdout?.write(`Installing or3-intern ${release.tag_name}…\n`);
        const work = await mkdtemp(join(installDir, '.or3-intern-work-'));
        try {
            const archivePath = join(work, basename(asset.name));
            await downloadVerifiedToFile(asset, archivePath, options);
            const entries = runTar(['-tzf', archivePath]).split(/\r?\n/).filter(Boolean);
            const binaryEntry = validateTarEntries(entries, 'or3-intern');
            runTar(['-xzf', archivePath, '-C', work, binaryEntry]);
            const extracted = join(work, binaryEntry);
            const extractedInfo = await lstat(extracted);
            if (!extractedInfo.isFile() || extractedInfo.isSymbolicLink()) {
                throw new Error(`${asset.name} did not contain a regular or3-intern executable`);
            }
            const metadata = await atomicInstallFromFile(extracted, target, 0o755);
            await writeCacheRecords(target, requestedVersion, metadata);
        } finally {
            await rm(work, { recursive: true, force: true });
        }
        return target;
    }, options.lockPolicy);
}

export async function ensureCloudflared(options = {}) {
    const override = process.env.OR3_CONNECT_CLOUDFLARED_BIN?.trim();
    if (override) return override;
    const installDir = options.installDir ?? getInstallDir();
    const target = join(installDir, 'cloudflared');
    if (await cachedVersionMatches(`${target}.version`, CLOUDFLARED_VERSION, target, `${target}.metadata.json`)) {
        return target;
    }

    return await withInstallLock(installDir, async () => {
        if (await cachedVersionMatches(`${target}.version`, CLOUDFLARED_VERSION, target, `${target}.metadata.json`)) {
            return target;
        }
        const release = await githubRelease(CLOUDFLARED_REPOSITORY, CLOUDFLARED_VERSION, options);
        if (release.tag_name !== CLOUDFLARED_VERSION) {
            throw new Error(`release tag ${release.tag_name || '<missing>'} did not match ${CLOUDFLARED_VERSION}`);
        }
        const platform = normalizePlatform(process.platform);
        const architecture = normalizeArchitecture(process.arch);
        const assetName = platform === 'darwin'
            ? `cloudflared-darwin-${architecture}.tgz`
            : `cloudflared-linux-${architecture}`;
        const asset = findAsset(release.assets, assetName);
        options.stdout?.write(`Installing cloudflared ${CLOUDFLARED_VERSION}…\n`);
        const work = await mkdtemp(join(installDir, '.cloudflared-work-'));
        try {
            const downloadPath = join(work, basename(asset.name));
            await downloadVerifiedToFile(asset, downloadPath, options);
            let source = downloadPath;
            if (platform === 'darwin') {
                const entries = runTar(['-tzf', downloadPath]).split(/\r?\n/).filter(Boolean);
                const binaryEntry = validateTarEntries(entries, 'cloudflared');
                runTar(['-xzf', downloadPath, '-C', work, binaryEntry]);
                source = join(work, binaryEntry);
                const extractedInfo = await lstat(source);
                if (!extractedInfo.isFile() || extractedInfo.isSymbolicLink()) {
                    throw new Error(`${asset.name} did not contain a regular cloudflared executable`);
                }
            }
            const metadata = await atomicInstallFromFile(source, target, 0o755);
            await writeCacheRecords(target, CLOUDFLARED_VERSION, metadata);
        } finally {
            await rm(work, { recursive: true, force: true });
        }
        return target;
    }, options.lockPolicy);
}

export async function githubRelease(repository, version, options = {}) {
    const endpoint = version
        ? `https://api.github.com/repos/${repository}/releases/tags/${encodeURIComponent(version)}`
        : `https://api.github.com/repos/${repository}/releases/latest`;
    return await requestWithRetry(endpoint, {
        fetchImpl: options.fetchImpl,
        requestPolicy: options.requestPolicy,
        headers: {
            Accept: 'application/vnd.github+json',
            'User-Agent': 'or3-connect',
            'X-GitHub-Api-Version': '2022-11-28',
        },
        consume: async (response) => await response.json(),
        failureMessage: `could not find a compatible ${repository} release`,
    });
}

export function findAsset(assets, name) {
    const asset = Array.isArray(assets)
        ? assets.find((candidate) => candidate.name === name)
        : undefined;
    if (
        !asset?.browser_download_url ||
        !asset?.digest?.startsWith('sha256:') ||
        !Number.isSafeInteger(asset.size) ||
        asset.size <= 0 ||
        asset.size > MAX_RELEASE_ASSET_BYTES
    ) {
        throw new Error(`release is missing the verified ${name} asset`);
    }
    return asset;
}

export async function downloadVerifiedToFile(asset, destination, options = {}) {
    await rm(destination, { force: true });
    return await requestWithRetry(asset.browser_download_url, {
        fetchImpl: options.fetchImpl,
        requestPolicy: options.requestPolicy,
        headers: { 'User-Agent': 'or3-connect' },
        failureMessage: `could not download ${asset.name}`,
        consume: async (response, signal) => {
            if (!response.body) {
                throw new Error(`${asset.name} download returned an empty body`);
            }
            await rm(destination, { force: true });
            const expected = asset.digest.slice('sha256:'.length).toLowerCase();
            const hash = createHash('sha256');
            let received = 0;
            const verifier = new Transform({
                transform(chunk, _encoding, callback) {
                    received += chunk.length;
                    if (received > asset.size) {
                        callback(new Error(`${asset.name} exceeded its declared size`));
                        return;
                    }
                    hash.update(chunk);
                    callback(null, chunk);
                },
            });
            const source = Readable.fromWeb(response.body);
            const destinationStream = createWriteStream(destination, {
                flags: 'wx',
                mode: 0o600,
            });
            try {
                await pipeline(source, verifier, destinationStream, { signal });
                if (received !== asset.size) {
                    throw new Error(`${asset.name} did not match its declared size`);
                }
                const actual = hash.digest('hex');
                if (actual !== expected) {
                    throw new Error(`${asset.name} failed its integrity check`);
                }
                await syncFile(destination);
                return { bytes: received, sha256: actual };
            } catch (error) {
                await rm(destination, { force: true });
                throw error;
            }
        },
    });
}

export async function cachedVersionMatches(versionFile, version, binary, metadataFile = `${binary}.metadata.json`) {
    try {
        const [savedVersion, binaryInfo] = await Promise.all([
            readSmallText(versionFile, 256),
            lstat(binary),
        ]);
        if (
            savedVersion.trim() !== version ||
            !binaryInfo.isFile() ||
            binaryInfo.isSymbolicLink() ||
            (binaryInfo.mode & 0o111) === 0
        ) {
            return false;
        }
        try {
            const metadata = JSON.parse(await readSmallText(metadataFile, MAX_METADATA_BYTES));
            return metadata.version === version &&
                metadata.size === binaryInfo.size &&
                metadata.mtimeMs === binaryInfo.mtimeMs &&
                metadata.ctimeMs === binaryInfo.ctimeMs &&
                typeof metadata.sha256 === 'string' &&
                /^[a-f0-9]{64}$/.test(metadata.sha256);
        } catch (error) {
            if (error?.code === 'ENOENT') {
                // Accept caches created by older versions. New installs always
                // write fingerprint and digest metadata.
                return true;
            }
            return false;
        }
    } catch {
        return false;
    }
}

export async function withInstallLock(installDir, operation, policy = {}) {
    const settings = { ...DEFAULT_LOCK_POLICY, ...policy };
    const lockPath = join(installDir, '.install.lock');
    const deadline = Date.now() + settings.timeoutMs;
    await mkdir(installDir, { recursive: true, mode: 0o700 });

    for (;;) {
        try {
            await mkdir(lockPath, { mode: 0o700 });
            break;
        } catch (error) {
            if (error?.code !== 'EEXIST') throw error;
            let lockInfo;
            try {
                lockInfo = await lstat(lockPath);
            } catch (statError) {
                if (statError?.code === 'ENOENT') continue;
                throw statError;
            }
            if (!lockInfo.isDirectory() || lockInfo.isSymbolicLink()) {
                throw new Error(`installer lock is not a private directory: ${lockPath}`);
            }
            if (
                Date.now() - lockInfo.mtimeMs > settings.staleMs &&
                await staleInstallLockCanBeRemoved(lockPath, lockInfo)
            ) {
                await rm(lockPath, { recursive: true, force: true });
                continue;
            }
            if (Date.now() >= deadline) {
                throw new Error('another OR3 installation is still in progress');
            }
            const jitter = Math.floor(Math.random() * Math.max(1, settings.pollMs / 4));
            await delay(Math.min(settings.pollMs + jitter, Math.max(1, deadline - Date.now())));
        }
    }

    try {
        await writeFile(
            join(lockPath, 'owner.json'),
            `${JSON.stringify({ pid: process.pid, startedAt: new Date().toISOString() })}\n`,
            { mode: 0o600, flag: 'wx' }
        );
        return await operation();
    } finally {
        await rm(lockPath, { recursive: true, force: true });
    }
}

async function staleInstallLockCanBeRemoved(lockPath, observedInfo) {
    let owner;
    try {
        owner = JSON.parse(await readSmallText(join(lockPath, 'owner.json'), 1024));
    } catch {
        return false;
    }
    if (!Number.isSafeInteger(owner?.pid) || owner.pid <= 0 || processIsAlive(owner.pid)) {
        return false;
    }
    try {
        const currentInfo = await lstat(lockPath);
        return currentInfo.isDirectory() &&
            !currentInfo.isSymbolicLink() &&
            currentInfo.dev === observedInfo.dev &&
            currentInfo.ino === observedInfo.ino &&
            currentInfo.mtimeMs === observedInfo.mtimeMs;
    } catch {
        return false;
    }
}

function processIsAlive(pid) {
    try {
        process.kill(pid, 0);
        return true;
    } catch (error) {
        return error?.code !== 'ESRCH';
    }
}

export async function requestWithRetry(url, options) {
    const fetchImpl = options.fetchImpl ?? globalThis.fetch;
    if (typeof fetchImpl !== 'function') {
        throw new Error('this Node.js version does not provide fetch');
    }
    const policy = { ...DEFAULT_REQUEST_POLICY, ...options.requestPolicy };
    let lastError;
    for (let attempt = 0; attempt <= policy.retries; attempt++) {
        const controller = new AbortController();
        const timer = setTimeout(
            () => controller.abort(new Error(`request deadline exceeded after ${policy.timeoutMs}ms`)),
            policy.timeoutMs
        );
        try {
            const response = await fetchImpl(url, {
                redirect: 'follow',
                headers: options.headers,
                signal: controller.signal,
            });
            if (!response.ok) {
                const retryable = isRetryableStatus(response.status);
                await response.body?.cancel?.().catch(() => {});
                if (!retryable || attempt === policy.retries) {
                    throw new Error(options.failureMessage);
                }
                throw new RetryableRequestError(`HTTP ${response.status}`);
            }
            return await options.consume(response, controller.signal);
        } catch (error) {
            lastError = normalizeRequestError(error, options.failureMessage);
            if (attempt === policy.retries || !isRetryableRequestError(error, controller.signal)) {
                throw lastError;
            }
        } finally {
            clearTimeout(timer);
        }
        const backoff = policy.backoffMs * (2 ** attempt);
        const jitter = Math.floor(Math.random() * Math.max(1, backoff / 4));
        await delay(backoff + jitter);
    }
    throw lastError ?? new Error(options.failureMessage);
}

class RetryableRequestError extends Error {}

function isRetryableStatus(status) {
    return status === 408 ||
        status === 425 ||
        status === 429 ||
        status >= 500;
}

function isRetryableRequestError(error, signal) {
    if (error instanceof RetryableRequestError || signal.aborted) return true;
    return error instanceof TypeError ||
        error?.code === 'ECONNRESET' ||
        error?.code === 'ECONNREFUSED' ||
        error?.code === 'ENETUNREACH' ||
        error?.code === 'EAI_AGAIN' ||
        error?.code === 'ETIMEDOUT';
}

function normalizeRequestError(error, fallback) {
    if (error?.name === 'AbortError' || /deadline exceeded/i.test(error?.message || '')) {
        return new Error(`${fallback}: network deadline exceeded`);
    }
    if (error instanceof RetryableRequestError || error instanceof TypeError) {
        return new Error(`${fallback}: temporary network failure`);
    }
    return error instanceof Error ? error : new Error(fallback);
}

async function atomicInstallFromFile(source, target, mode) {
    const staging = `${target}.partial-${randomUUID()}`;
    const hash = createHash('sha256');
    let size = 0;
    const hasher = new Transform({
        transform(chunk, _encoding, callback) {
            size += chunk.length;
            hash.update(chunk);
            callback(null, chunk);
        },
    });
    try {
        await pipeline(
            createReadStream(source),
            hasher,
            createWriteStream(staging, { flags: 'wx', mode: 0o600 })
        );
        await chmod(staging, mode);
        await syncFile(staging);
        await rename(staging, target);
        await syncDirectory(dirname(target));
        const installed = await stat(target);
        return {
            sha256: hash.digest('hex'),
            size,
            mtimeMs: installed.mtimeMs,
            ctimeMs: installed.ctimeMs,
        };
    } finally {
        await rm(staging, { force: true });
    }
}

async function writeCacheRecords(target, version, metadata) {
    await atomicWriteFile(
        `${target}.metadata.json`,
        `${JSON.stringify({ version, ...metadata })}\n`,
        0o600
    );
    await atomicWriteFile(`${target}.version`, `${version}\n`, 0o600);
}

async function atomicWriteFile(target, contents, mode) {
    const staging = `${target}.partial-${randomUUID()}`;
    let handle;
    try {
        handle = await open(staging, 'wx', mode);
        await handle.writeFile(contents, 'utf8');
        await handle.sync();
        await handle.close();
        handle = undefined;
        await rename(staging, target);
        await syncDirectory(dirname(target));
    } finally {
        await handle?.close().catch(() => {});
        await rm(staging, { force: true });
    }
}

async function syncFile(path) {
    const handle = await open(path, 'r+');
    try {
        await handle.sync();
    } finally {
        await handle.close();
    }
}

async function syncDirectory(path) {
    let handle;
    try {
        handle = await open(path, 'r');
        await handle.sync();
    } catch (error) {
        if (!['EINVAL', 'ENOTSUP', 'EPERM', 'EISDIR'].includes(error?.code)) {
            throw error;
        }
    } finally {
        await handle?.close().catch(() => {});
    }
}

async function readSmallText(path, maxBytes) {
    const handle = await open(path, 'r');
    try {
        const buffer = Buffer.alloc(maxBytes + 1);
        const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0);
        if (bytesRead > maxBytes) {
            throw new Error(`${basename(fileURLToPathSafe(path))} exceeded its expected size`);
        }
        return buffer.subarray(0, bytesRead).toString('utf8');
    } finally {
        await handle.close();
    }
}

function fileURLToPathSafe(value) {
    return value instanceof URL ? fileURLToPath(value) : String(value);
}

function validateTarEntries(entries, expectedName) {
    let binaryEntry;
    for (const rawEntry of entries) {
        const entry = rawEntry.replace(/^\.\//, '');
        if (
            entry.startsWith('/') ||
            entry.split('/').includes('..') ||
            entry.includes('\\')
        ) {
            throw new Error('release archive contained an unsafe path');
        }
        if (entry === expectedName) {
            binaryEntry = rawEntry;
        }
    }
    if (!binaryEntry) {
        throw new Error(`release archive did not contain ${expectedName}`);
    }
    return binaryEntry;
}

function runTar(args) {
    const result = spawnSync('tar', args, {
        encoding: 'utf8',
        stdio: ['ignore', 'pipe', 'pipe'],
        timeout: TAR_TIMEOUT_MS,
        maxBuffer: TAR_MAX_OUTPUT_BYTES,
        killSignal: 'SIGKILL',
    });
    if (result.error?.code === 'ETIMEDOUT') {
        throw new Error('tar timed out while unpacking a required component');
    }
    if (result.error || result.status !== 0) {
        throw new Error('tar could not safely unpack a required component');
    }
    return result.stdout;
}

function normalizePlatform(platform) {
    if (platform === 'darwin' || platform === 'linux') return platform;
    throw new Error('OR3 Connect currently supports macOS and Linux');
}

function normalizeArchitecture(architecture) {
    if (architecture === 'x64') return 'amd64';
    if (architecture === 'arm64') return 'arm64';
    throw new Error(`OR3 Connect does not yet support ${architecture}`);
}

function getInstallDir() {
    return process.env.OR3_CONNECT_BIN_DIR?.trim() ||
        join(homedir(), '.or3', 'bin');
}

function spawnAndWait(spawnImpl, command, args, options) {
    return new Promise((resolvePromise, reject) => {
        const child = spawnImpl(command, args, options);
        child.once('error', reject);
        child.once('exit', (code, signal) => {
            if (signal) {
                reject(new Error(`or3-intern stopped after signal ${signal}`));
                return;
            }
            resolvePromise(code ?? 1);
        });
    });
}

function delay(milliseconds) {
    return new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds));
}

function safeMessage(error) {
    return error instanceof Error ? error.message : 'setup failed';
}

function canonicalExecutablePath(path) {
    if (!path) return '';
    try {
        return realpathSync(resolve(path));
    } catch {
        return resolve(path);
    }
}

const invokedPath = canonicalExecutablePath(process.argv[1]);
const modulePath = canonicalExecutablePath(fileURLToPath(import.meta.url));
if (invokedPath === modulePath) {
    runCLI().then((code) => {
        process.exitCode = code;
    }).catch((error) => {
        process.stderr.write(`or3: ${safeMessage(error)}\n`);
        process.exitCode = 1;
    });
}
