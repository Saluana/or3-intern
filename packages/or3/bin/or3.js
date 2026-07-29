#!/usr/bin/env node
import { createHash } from 'node:crypto';
import {
    chmod,
    copyFile,
    mkdir,
    mkdtemp,
    readFile,
    rm,
    writeFile,
} from 'node:fs/promises';
import { homedir, tmpdir } from 'node:os';
import { basename, delimiter, join } from 'node:path';
import { spawn, spawnSync } from 'node:child_process';

const INTERN_REPOSITORY = 'Saluana/or3-intern';
const CLOUDFLARED_REPOSITORY = 'cloudflare/cloudflared';
const CLOUDFLARED_VERSION = '2026.5.2';
const installDir = process.env.OR3_CONNECT_BIN_DIR?.trim() ||
    join(homedir(), '.or3', 'bin');

main().catch((error) => {
    process.stderr.write(`or3: ${safeMessage(error)}\n`);
    process.exitCode = 1;
});

async function main() {
    const args = process.argv.slice(2);
    const command = args[0] || 'connect';
    if (command !== 'connect') {
        process.stderr.write('Usage: npx or3 connect [options]\n');
        process.exitCode = 2;
        return;
    }
    process.stdout.write('OR3 Connect\n\n');
    await mkdir(installDir, { recursive: true, mode: 0o700 });
    const cloudflared = await ensureCloudflared();
    const intern = await ensureIntern();
    const child = spawn(intern, args, {
        stdio: 'inherit',
        env: {
            ...process.env,
            PATH: `${installDir}${delimiter}${process.env.PATH || ''}`,
            OR3_CONNECT_CLOUDFLARED_BIN: cloudflared,
        },
    });
    child.on('error', (error) => {
        process.stderr.write(`or3: could not start or3-intern: ${safeMessage(error)}\n`);
        process.exitCode = 1;
    });
    child.on('exit', (code, signal) => {
        if (signal) {
            process.kill(process.pid, signal);
            return;
        }
        process.exitCode = code ?? 1;
    });
}

async function ensureIntern() {
    const override = process.env.OR3_INTERN_BIN?.trim();
    if (override) return override;
    const packageMetadata = JSON.parse(
        await readFile(new URL('../package.json', import.meta.url), 'utf8')
    );
    const requestedVersion =
        process.env.OR3_INTERN_VERSION?.trim() ||
        `v${packageMetadata.version}`;
    const release = await githubRelease(INTERN_REPOSITORY, requestedVersion);
    const target = join(installDir, 'or3-intern');
    const versionFile = `${target}.version`;
    if (await cachedVersionMatches(versionFile, release.tag_name, target)) {
        return target;
    }
    const platform = normalizePlatform(process.platform);
    const architecture = normalizeArchitecture(process.arch);
    const asset = findAsset(
        release.assets,
        `or3-intern-${platform}-${architecture}.tar.gz`
    );
    process.stdout.write(`Installing or3-intern ${release.tag_name}…\n`);
    const archive = await downloadVerified(asset);
    const work = await mkdtemp(join(tmpdir(), 'or3-intern-'));
    try {
        await writeFile(join(work, asset.name), archive, { mode: 0o600 });
        run('tar', ['-xzf', join(work, asset.name), '-C', work]);
        const extracted = join(work, 'or3-intern');
        await copyFile(extracted, target);
        await chmod(target, 0o755);
        await writeFile(versionFile, `${release.tag_name}\n`, { mode: 0o600 });
    } finally {
        await rm(work, { recursive: true, force: true });
    }
    return target;
}

async function ensureCloudflared() {
    const override = process.env.OR3_CONNECT_CLOUDFLARED_BIN?.trim();
    if (override) return override;
    const existing = join(installDir, 'cloudflared');
    const versionFile = `${existing}.version`;
    if (await cachedVersionMatches(versionFile, CLOUDFLARED_VERSION, existing)) {
        return existing;
    }
    const release = await githubRelease(CLOUDFLARED_REPOSITORY, CLOUDFLARED_VERSION);
    const platform = normalizePlatform(process.platform);
    const architecture = normalizeArchitecture(process.arch);
    const assetName = platform === 'darwin'
        ? `cloudflared-darwin-${architecture}.tgz`
        : `cloudflared-linux-${architecture}`;
    const asset = findAsset(release.assets, assetName);
    process.stdout.write(`Installing cloudflared ${CLOUDFLARED_VERSION}…\n`);
    const body = await downloadVerified(asset);
    if (platform === 'darwin') {
        const work = await mkdtemp(join(tmpdir(), 'or3-cloudflared-'));
        try {
            const archivePath = join(work, basename(asset.name));
            await writeFile(archivePath, body, { mode: 0o600 });
            run('tar', ['-xzf', archivePath, '-C', work]);
            await copyFile(join(work, 'cloudflared'), existing);
        } finally {
            await rm(work, { recursive: true, force: true });
        }
    } else {
        await writeFile(existing, body, { mode: 0o755 });
    }
    await chmod(existing, 0o755);
    await writeFile(versionFile, `${CLOUDFLARED_VERSION}\n`, { mode: 0o600 });
    return existing;
}

async function githubRelease(repository, version) {
    const endpoint = version
        ? `https://api.github.com/repos/${repository}/releases/tags/${encodeURIComponent(version)}`
        : `https://api.github.com/repos/${repository}/releases/latest`;
    const response = await fetch(endpoint, {
        headers: {
            Accept: 'application/vnd.github+json',
            'User-Agent': 'or3-connect',
            'X-GitHub-Api-Version': '2022-11-28',
        },
    });
    if (!response.ok) {
        throw new Error(`could not find a compatible ${repository} release`);
    }
    return await response.json();
}

function findAsset(assets, name) {
    const asset = assets.find((candidate) => candidate.name === name);
    if (!asset?.browser_download_url || !asset?.digest?.startsWith('sha256:')) {
        throw new Error(`release is missing the verified ${name} asset`);
    }
    return asset;
}

async function downloadVerified(asset) {
    const response = await fetch(asset.browser_download_url, {
        redirect: 'follow',
        headers: { 'User-Agent': 'or3-connect' },
    });
    if (!response.ok) {
        throw new Error(`could not download ${asset.name}`);
    }
    const body = Buffer.from(await response.arrayBuffer());
    const actual = createHash('sha256').update(body).digest('hex');
    const expected = asset.digest.slice('sha256:'.length).toLowerCase();
    if (actual !== expected) {
        throw new Error(`${asset.name} failed its integrity check`);
    }
    return body;
}

async function cachedVersionMatches(versionFile, version, binary) {
    try {
        const [saved] = await Promise.all([
            readFile(versionFile, 'utf8'),
            readFile(binary),
        ]);
        return saved.trim() === version;
    } catch {
        return false;
    }
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

function run(command, args) {
    const result = spawnSync(command, args, { stdio: 'pipe' });
    if (result.status !== 0) {
        throw new Error(`${command} could not unpack a required component`);
    }
}

function safeMessage(error) {
    return error instanceof Error ? error.message : 'setup failed';
}
