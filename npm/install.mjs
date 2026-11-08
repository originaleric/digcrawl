import * as fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { spawn, spawnSync } from 'node:child_process';
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
  const replacement = { command: binary, args: ['mcp', '--idle-timeout', '0', '--config', profile] };
  const output = `${lines.join('\n').trimEnd()}\n\n[mcp_servers.digcrawl]\ncommand = ${JSON.stringify(binary)}\nargs = ${JSON.stringify(replacement.args)}\n`;
  const expected = structuredClone(before);
  expected.mcp_servers ||= {};
  expected.mcp_servers.digcrawl = replacement;
  // Reject unusual TOML layouts rather than silently corrupting dotted keys,
  // multiline strings, or unrelated user settings during a text-preserving edit.
  if (!isDeepStrictEqual(structuredClone(parse(output)), expected)) throw new Error('Cannot safely update this Codex TOML layout; configure DigCrawl manually');
  return output;
}

async function prepareCodex(file, dirs, resources) {
  try {
    const info = await fs.lstat(file);
    if (info.isSymbolicLink()) file = await fs.realpath(file); // preserve dotfiles links, reject dangling links
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
    // A dangling symlink must not be replaced by the atomic configuration write.
    try { if ((await fs.lstat(file)).isSymbolicLink()) throw new Error('Codex configuration symlink target is missing'); }
    catch (missing) { if (missing.code !== 'ENOENT') throw missing; }
  }
  resources.push(path.dirname(file));
  const original = await exists(file) ? await fs.readFile(file, 'utf8') : null;
  const output = codexConfig(original || '', dirs.binary, dirs.profile);
  const mode = original === null ? 0o600 : (await fs.stat(file)).mode & 0o777;
  return async guard => {
    guard.check();
    const current = await exists(file) ? await fs.readFile(file, 'utf8') : null;
    if (current !== original) throw new Error('Codex configuration changed during installation; retry');
    await fs.mkdir(path.dirname(file), { recursive: true });
    const stage = `${file}.digcrawl-${randomUUID()}`;
    try {
      await fs.writeFile(stage, output, { flag: 'wx', mode });
      guard.check();
      await fs.rename(stage, file);
      guard.check();
    } finally { await fs.rm(stage, { force: true }); }
    return file;
  };
}

async function replaceDirectory(source, target, guard) {
  guard.check();
  await fs.mkdir(path.dirname(target), { recursive: true });
  const stage = `${target}.stage-${randomUUID()}`;
  const backup = `${target}.previous-${randomUUID()}`;
  let saved = false, published = false;
  try {
    await fs.cp(source, stage, { recursive: true, force: false, errorOnExist: true });
    guard.check();
    try { await fs.rename(target, backup); saved = true; }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
    guard.check();
    await fs.rename(stage, target); published = true;
    guard.check();
    if (saved) await fs.rm(backup, { recursive: true, force: true });
  } catch (error) {
    // Never overwrite a replacement owner's data while trying to roll back.
    if (saved) {
      try { guard.check(); } catch { throw new Error(`Installation lock lost; previous Skill retained at ${backup}`, { cause: error }); }
      if (!published) await fs.rename(backup, target);
    }
    throw error;
  } finally { await fs.rm(stage, { recursive: true, force: true }); }
}

function run(binary, args, env, timeout = 120_000) {
  const result = spawnSync(binary, args, { env, stdio: 'inherit', timeout, windowsHide: true });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${path.basename(binary)} ${args.slice(0, 2).join(' ')} failed (${result.status ?? result.signal})`);
}

// The existing Go lock protects the full installation. Its stdin belongs to
// this process, so parent termination cancels preparation and releases the OS
// lock. Persistent lock files are never interpreted as live owners.
async function prepareBrowserWithInstallLock(binary, args, env, resources) {
  const child = spawn(binary, [...args, ...resources.flatMap(directory => ['--installer-lock', directory])], { env, stdio: ['pipe', 'pipe', 'inherit'], windowsHide: true });
  const interrupted = new AbortController();
  let exited = false, releasing = false, acknowledged = false;
  const check = () => {
    if (exited || child.exitCode !== null || child.signalCode !== null || interrupted.signal.aborted) throw new Error('Installation lock owner exited');
  };
  child.once('exit', () => { exited = true; if (!releasing) interrupted.abort(); });
  const done = new Promise(resolve => child.once('close', (code, signal) => resolve({ code, signal })));
  child.stdin.on('error', () => {});
  let pending = '';
  child.stdout.on('data', chunk => {
    pending += chunk.toString();
    acknowledged ||= pending.includes('DIGCRAWL_INSTALL_RELEASED\n');
    if (pending.length > 8192) pending = pending.slice(-8192);
  });
  try {
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => { child.kill(); reject(new Error('Browser preparation timed out')); }, 35 * 60_000);
      const finish = fn => value => { clearTimeout(timer); fn(value); };
      child.once('error', finish(reject));
      child.once('close', finish(() => reject(new Error('Browser preparation / installation lock unavailable'))));
      child.stdout.on('data', () => {
        const newline = pending.indexOf('\n');
        if (newline >= 0) {
          if (pending.slice(0,newline).replace(/\r$/, '') === 'DIGCRAWL_INSTALL_READY_V2') finish(resolve)();
          else finish(reject)(new Error('Unsupported installer lock protocol; use the matching updated CLI release'));
        }
      });
    });
    check();
  } catch (error) { child.stdin.end(); await done; throw error; }
  return {
    check,
    async run(executable, argv) {
      check();
      await new Promise((resolve, reject) => {
        const operation = spawn(executable, argv, { env, stdio: 'inherit', signal: interrupted.signal, timeout: 120_000, windowsHide: true });
        let failure;
        operation.once('error', error => { failure = error; });
        operation.once('close', (code, signal) => failure ? reject(failure) : code === 0 ? resolve() : reject(new Error(`${path.basename(executable)} ${argv.slice(0,2).join(' ')} failed (${code ?? signal})`)));
      });
      check();
    },
    async close(completed = false) {
      if (completed) { check(); releasing = true; child.stdin.end('release\n'); }
      else child.stdin.end();
      const timer = setTimeout(() => child.kill('SIGKILL'), 10_000);
      try {
        const result = await done;
        if (completed && (result.code !== 0 || !acknowledged)) throw new Error('Installation lock release was not acknowledged');
      } finally { clearTimeout(timer); }
    },
  };
}

export async function install(options = {}, context = {}) {
  const env = context.env || process.env;
  const platform = context.platform || process.platform;
  const home = context.home || os.homedir();
  const log = context.log || console.log;
  const version = (options.version || '1.0.3').replace(/^v/, '');
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
    const resources=[dirs.installDir,path.dirname(dirs.profile)];
    if (agents) resources.push(path.dirname(dirs.skill));
    if (codex) resources.push(path.join(dirs.codexHome,'skills'));
    if (claude) resources.push(path.join(dirs.claudeHome,'skills'));
    const writeConfig = codex ? await prepareCodex(await selectCodexConfig(env, dirs, context.cwd || process.cwd()), dirs, resources) : null;
    const browserArgs = ['browser', 'install'];
    if (env.DIGCRAWL_BROWSER_ARCHIVE_DIR) browserArgs.push('--archive-dir', env.DIGCRAWL_BROWSER_ARCHIVE_DIR);
    lock = await prepareBrowserWithInstallLock(stagedBinary, browserArgs, env, resources);
    // The verified new program stops the old Bridge before executable replacement.
    // On Windows this also releases the executable file lock.
    lock.check();
    try { if ((await fs.lstat(dirs.binary)).isSymbolicLink()) throw new Error('Refusing to replace a managed binary symlink; use its package manager or another install directory'); }
    catch(error) { if(error.code !== 'ENOENT') throw error; }
    if (await exists(dirs.profile)) await lock.run(stagedBinary, ['browser', 'migrate', '--config', dirs.profile, '--apply']);
    const stage = path.join(dirs.installDir, `.digcrawl-${randomUUID()}${platform === 'win32' ? '.exe' : ''}`);
    try {
      await fs.copyFile(stagedBinary, stage);
      await fs.chmod(stage, 0o755);
      lock.check();
      await fs.rename(stage, dirs.binary);
    } finally { await fs.rm(stage, { force: true }); }
    // Retire a legacy worker restarted by an old MCP client during replacement.
    // Subsequent starts now resolve this path to the isolated-runtime CLI.
    if (await exists(dirs.profile)) await lock.run(dirs.binary, ['browser', 'migrate', '--config', dirs.profile, '--apply']);
    await lock.run(dirs.binary, ['version']);
    if (agents) {
      if (!await exists(dirs.profile)) await lock.run(dirs.binary, ['init', '--profile', 'skill']);
      await lock.run(dirs.binary, ['config', 'validate', '--config', dirs.profile]);
      const skill = path.join(unpacked, 'skills', 'digcrawl');
      await replaceDirectory(skill, dirs.skill, lock);
      if (claude) await replaceDirectory(skill, path.join(dirs.claudeHome, 'skills', 'digcrawl'), lock);
      if (codex) {
        await replaceDirectory(skill, path.join(dirs.codexHome, 'skills', 'digcrawl'), lock);
        log(`Codex MCP configured: ${await writeConfig(lock)}`);
      }
      log(`Skill installed: ${dirs.skill}`);
      log(`MCP stdio: ${JSON.stringify({ command: dirs.binary, args: ['mcp', '--config', dirs.profile] })}`);
      log('Restart your agent and open a new chat to load DigCrawl.');
    }
    await lock.close(true);
    lock = null;
    log(`Installed: ${dirs.binary}`);
    log(`To run digcrawl directly, add ${dirs.installDir} to PATH if needed.`);
    return dirs;
  } finally {
    try { if (lock) await lock.close(); } finally { await fs.rm(temp, { recursive: true, force: true }); }
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
