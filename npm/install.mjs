import * as fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { isDeepStrictEqual } from 'node:util';
import { extract } from 'tar';
import { unzipSync } from 'fflate';
import { parse } from 'smol-toml';

const REPOSITORY = 'https://github.com/originaleric/digcrawl';
const MAX_DOWNLOAD = 80 * 1024 * 1024;
const MAX_EXPANDED = 200 * 1024 * 1024;
const REQUIRED_SKILL_FILES = [
  'SKILL.md', 'agents/openai.yaml', 'claude-code/.mcp.json.example',
  'codex/config.toml.example', 'mcp/README.md',
  'references/error-handling.md', 'references/tool-routing.md',
];

async function exists(file) {
  try { await fs.stat(file); return true; }
  catch (error) { if (error.code === 'ENOENT') return false; throw error; }
}

export function assetName(version, platform = process.platform, arch = process.arch) {
  version = version.replace(/^v/, '');
  if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version)) throw new Error('Invalid release version');
  const system = { darwin: 'darwin', linux: 'linux', win32: 'windows' }[platform];
  const cpu = { x64: 'amd64', arm64: 'arm64' }[arch];
  if (!system || !cpu || (platform === 'win32' && arch !== 'x64')) {
    throw new Error(`Unsupported platform: ${platform}/${arch}. See ${REPOSITORY}/releases`);
  }
  return `digcrawl_${version}_${system}_${cpu}.${platform === 'win32' ? 'zip' : 'tar.gz'}`;
}

export function locations(env = process.env, platform = process.platform, home = os.homedir()) {
  const configRoot = env.XDG_CONFIG_HOME ? path.join(env.XDG_CONFIG_HOME, 'digcrawl')
    : platform === 'win32' ? path.join(env.APPDATA || path.join(home, 'AppData', 'Roaming'), 'DigCrawl')
      : path.join(home, '.config', 'digcrawl');
  const installDir = path.resolve(env.DIGCRAWL_INSTALL_DIR || path.join(home, '.local', 'bin'));
  return {
    installDir, binary: path.join(installDir, platform === 'win32' ? 'digcrawl.exe' : 'digcrawl'),
    profile: path.resolve(configRoot, 'digcrawl-skill.yaml'),
    skill: path.resolve(env.DIGCRAWL_SKILL_DIR || path.join(env.XDG_DATA_HOME || path.join(home, '.local', 'share'), 'digcrawl', 'skills', 'digcrawl')),
    codexHome: path.resolve(env.CODEX_HOME || path.join(home, '.codex')),
    claudeHome: path.resolve(env.CLAUDE_CONFIG_DIR || path.join(home, '.claude')),
  };
}

export async function download(url, limit = MAX_DOWNLOAD, fetchImpl = fetch) {
  const response = await fetchImpl(url, { signal: AbortSignal.timeout(120_000), headers: { 'User-Agent': 'digcrawl-skill' } });
  if (!response.ok) throw new Error(`Download returned HTTP ${response.status}: ${url}${response.status === 404 ? ' (release must exist and be publicly accessible)' : ''}`);
  if (Number(response.headers.get('content-length')) > limit) throw new Error('Download exceeds size limit');
  const chunks = [];
  let size = 0;
  for await (const chunk of response.body) {
    size += chunk.length;
    if (size > limit) throw new Error('Download exceeds size limit');
    chunks.push(chunk);
  }
  return Buffer.concat(chunks);
}

// Both archive formats use an explicit allowlist. Never extract links, devices,
// absolute paths, or parent traversal, including Windows drive/ADS names.
function entryName(name, binaryName) {
  name = name.replace(/^\.\//, '').replace(/\/$/, '');
  if (!name || name.includes('\\') || name.includes(':') || name.startsWith('/') || name.split('/').some(p => p === '..' || p === '.')) {
    throw new Error(`Unsafe release entry: ${name}`);
  }
  return name === binaryName || name === 'skills' || name === 'skills/digcrawl' || name.startsWith('skills/digcrawl/') ? name : null;
}

export async function unpack(archive, destination, binaryName) {
  let size = 0;
  const accept = (name, bytes) => {
    const allowed = entryName(name, binaryName);
    if (allowed) {
      size += bytes;
      if (size > MAX_EXPANDED) throw new Error('Expanded release exceeds size limit');
    }
    return allowed;
  };
  if (archive.endsWith('.zip')) {
    const files = unzipSync(await fs.readFile(archive), { filter: entry => Boolean(accept(entry.name, entry.originalSize)) });
    for (const [name, data] of Object.entries(files)) {
      const output = path.join(destination, entryName(name, binaryName));
      if (name.endsWith('/')) await fs.mkdir(output, { recursive: true });
      else {
        await fs.mkdir(path.dirname(output), { recursive: true });
        await fs.writeFile(output, data, { flag: 'wx', mode: 0o600 });
      }
    }
  } else {
    let rejected;
    await extract({ file: archive, cwd: destination, strict: true,
      filter: (name, entry) => {
        if (rejected) return false;
        try {
          const allowed = accept(name, entry.size);
          if (allowed && !['File', 'Directory'].includes(entry.type)) throw new Error(`Unsupported release entry: ${name}`);
          return Boolean(allowed);
        } catch (error) { rejected = error; return false; }
      },
    });
    if (rejected) throw rejected;
  }
  for (const file of [binaryName, ...REQUIRED_SKILL_FILES.map(p => `skills/digcrawl/${p}`)]) {
    if (!(await fs.stat(path.join(destination, file))).isFile()) throw new Error(`Incomplete release: ${file}`);
  }
  await fs.chmod(path.join(destination, binaryName), 0o755);
}

export async function selectCodexConfig(env, dirs, cwd) {
  if (env.DIGCRAWL_CODEX_CONFIG) return path.resolve(env.DIGCRAWL_CODEX_CONFIG);
  for (let current = path.resolve(cwd);;) {
    const candidate = path.join(current, '.codex', 'config.toml');
    if (await exists(candidate)) {
      const content = parse(await fs.readFile(candidate, 'utf8'));
      if (content.mcp_servers?.digcrawl) return candidate;
    }
    if (await exists(path.join(current, '.git'))) break;
    const parent = path.dirname(current);
    if (parent === current) break;
    current = parent;
  }
  return path.join(dirs.codexHome, 'config.toml');
}

export function codexConfig(source, binary, profile) {
  const before = parse(source);
  const header = /^\s*\[\[?/;
  const digcrawl = /^\s*\[\[?\s*mcp_servers\s*\.\s*"?digcrawl"?\s*[\].]/;
  let skip = false;
  const lines = source.split('\n').filter(line => {
    if (header.test(line)) skip = digcrawl.test(line);
    return !skip;
  });
  const replacement = { command: binary, args: ['mcp', '--idle-timeout', '15m', '--config', profile] };
  const output = `${lines.join('\n').trimEnd()}\n\n[mcp_servers.digcrawl]\ncommand = ${JSON.stringify(binary)}\nargs = ${JSON.stringify(replacement.args)}\n`;
  const expected = structuredClone(before);
  expected.mcp_servers ||= {};
  expected.mcp_servers.digcrawl = replacement;
  // Reject unusual TOML layouts rather than silently corrupting dotted keys,
  // multiline strings, or unrelated user settings during a text-preserving edit.
  if (!isDeepStrictEqual(structuredClone(parse(output)), expected)) throw new Error('Cannot safely update this Codex TOML layout; configure DigCrawl manually');
  return output;
}

async function prepareCodex(file, dirs) {
  try {
    const info = await fs.lstat(file);
    if (info.isSymbolicLink()) file = await fs.realpath(file); // preserve dotfiles links, reject dangling links
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
    // A dangling symlink must not be replaced by the atomic configuration write.
    try { if ((await fs.lstat(file)).isSymbolicLink()) throw new Error('Codex configuration symlink target is missing'); }
    catch (missing) { if (missing.code !== 'ENOENT') throw missing; }
  }
  const original = await exists(file) ? await fs.readFile(file, 'utf8') : null;
  const output = codexConfig(original || '', dirs.binary, dirs.profile);
  const mode = original === null ? 0o600 : (await fs.stat(file)).mode & 0o777;
  return async () => {
    const current = await exists(file) ? await fs.readFile(file, 'utf8') : null;
    if (current !== original) throw new Error('Codex configuration changed during installation; retry');
    await fs.mkdir(path.dirname(file), { recursive: true });
    const stage = `${file}.digcrawl-${randomUUID()}`;
    try {
      await fs.writeFile(stage, output, { flag: 'wx', mode });
      await fs.rename(stage, file);
    } finally { await fs.rm(stage, { force: true }); }
    return file;
  };
}

async function replaceDirectory(source, target) {
  await fs.mkdir(path.dirname(target), { recursive: true });
  const stage = `${target}.stage-${randomUUID()}`;
  const backup = `${target}.previous-${randomUUID()}`;
  let saved = false;
  try {
    await fs.cp(source, stage, { recursive: true, force: false, errorOnExist: true });
    try { await fs.rename(target, backup); saved = true; }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
    try { await fs.rename(stage, target); }
    catch (error) { if (saved) await fs.rename(backup, target); throw error; }
    if (saved) await fs.rm(backup, { recursive: true, force: true });
  } finally { await fs.rm(stage, { recursive: true, force: true }); }
}

function run(binary, args, env) {
  const result = spawnSync(binary, args, { env, stdio: 'inherit', timeout: 120_000, windowsHide: true });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${path.basename(binary)} ${args[0]} failed (${result.status ?? result.signal})`);
}

export async function install(options = {}, context = {}) {
  const env = context.env || process.env;
  const platform = context.platform || process.platform;
  const home = context.home || os.homedir();
  const log = context.log || console.log;
  const version = (options.version || '1.0.0').replace(/^v/, '');
  const asset = assetName(version, platform, context.arch || process.arch);
  const dirs = locations(env, platform, home);
  if (options.installDir) {
    dirs.installDir = path.resolve(options.installDir);
    dirs.binary = path.join(dirs.installDir, platform === 'win32' ? 'digcrawl.exe' : 'digcrawl');
  }
  const agents = !options.skipAgents && env.DIGCRAWL_SETUP_AGENTS !== '0';
  const codex = agents && !options.skipCodex && env.DIGCRAWL_SETUP_CODEX !== '0' && options.target !== 'claude-code';
  const claudeSetting = env.DIGCRAWL_SETUP_CLAUDE || 'auto';
  if (!['auto', '0', '1'].includes(claudeSetting)) throw new Error('DIGCRAWL_SETUP_CLAUDE must be auto, 0, or 1');
  const claude = agents && options.target !== 'codex' && (options.target === 'claude-code' || options.target === '--all' || claudeSetting === '1'
    || (claudeSetting === 'auto' && await exists(dirs.claudeHome)));
  const temp = await fs.mkdtemp(path.join(os.tmpdir(), 'digcrawl-npm-'));
  let lock;
  try {
    const base = `${REPOSITORY}/releases/download/v${version}`;
    log(`Downloading DigCrawl v${version} (${platform}/${context.arch || process.arch})…`);
    const archive = await download(`${base}/${asset}`, MAX_DOWNLOAD, context.fetchImpl);
    const checksums = (await download(`${base}/checksums.txt`, 1024 * 1024, context.fetchImpl)).toString('utf8');
    const matches = checksums.split(/\r?\n/).map(line => line.trim().split(/\s+/))
      .filter(parts => parts[1]?.replace(/^\*/, '') === asset);
    const digest = createHash('sha256').update(archive).digest('hex');
    if (matches.length !== 1 || !/^[a-fA-F0-9]{64}$/.test(matches[0][0]) || matches[0][0].toLowerCase() !== digest) {
      throw new Error('SHA-256 verification failed; nothing was installed');
    }
    const archivePath = path.join(temp, asset);
    await fs.writeFile(archivePath, archive);
    const unpacked = path.join(temp, 'release');
    await fs.mkdir(unpacked);
    const binaryName = path.basename(dirs.binary);
    await unpack(archivePath, unpacked, binaryName);
    const stagedBinary = path.join(unpacked, binaryName);
    await fs.mkdir(dirs.installDir, { recursive: true });
    try { lock = await fs.open(path.join(dirs.installDir, '.digcrawl-install.lock'), 'wx', 0o600); }
    catch (error) { if (error.code === 'EEXIST') throw new Error('Another installation is active (.digcrawl-install.lock)'); throw error; }
    const writeConfig = codex ? await prepareCodex(await selectCodexConfig(env, dirs, context.cwd || process.cwd()), dirs) : null;
    // The verified new program stops the old Bridge before executable replacement.
    // On Windows this also releases the executable file lock.
    if (await exists(dirs.profile)) run(stagedBinary, ['bridge', 'stop', '--config', dirs.profile], env);
    const stage = path.join(dirs.installDir, `.digcrawl-${randomUUID()}${platform === 'win32' ? '.exe' : ''}`);
    try {
      await fs.copyFile(stagedBinary, stage);
      await fs.chmod(stage, 0o755);
      await fs.rename(stage, dirs.binary);
    } finally { await fs.rm(stage, { force: true }); }
    run(dirs.binary, ['version'], env);
    if (agents) {
      if (!await exists(dirs.profile)) run(dirs.binary, ['init', '--profile', 'skill'], env);
      run(dirs.binary, ['config', 'validate', '--config', dirs.profile], env);
      const skill = path.join(unpacked, 'skills', 'digcrawl');
      await replaceDirectory(skill, dirs.skill);
      if (claude) await replaceDirectory(skill, path.join(dirs.claudeHome, 'skills', 'digcrawl'));
      if (codex) {
        await replaceDirectory(skill, path.join(dirs.codexHome, 'skills', 'digcrawl'));
        log(`Codex MCP configured: ${await writeConfig()}`);
      }
      log(`Skill installed: ${dirs.skill}`);
      log(`MCP stdio: ${JSON.stringify({ command: dirs.binary, args: ['mcp', '--config', dirs.profile] })}`);
      log('Restart your agent and open a new chat to load DigCrawl.');
    }
    log(`Installed: ${dirs.binary}`);
    log(`To run digcrawl directly, add ${dirs.installDir} to PATH if needed.`);
    return dirs;
  } finally {
    if (lock) { await lock.close(); await fs.rm(path.join(dirs.installDir, '.digcrawl-install.lock'), { force: true }); }
    await fs.rm(temp, { recursive: true, force: true });
  }
}

export async function status() {
  const dirs = locations();
  for (const [label, file] of Object.entries({ Program: dirs.binary, Profile: dirs.profile, Skill: dirs.skill,
    'Codex Skill': path.join(dirs.codexHome, 'skills', 'digcrawl'), 'Claude Code Skill': path.join(dirs.claudeHome, 'skills', 'digcrawl') })) {
    console.log(`${await exists(file) ? 'Installed' : 'Missing'} · ${label}: ${file}`);
  }
  if (await exists(dirs.binary)) run(dirs.binary, ['version'], process.env);
}
