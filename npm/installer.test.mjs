import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { create } from 'tar';
import { zipSync, strToU8 } from 'fflate';
import { parse } from 'smol-toml';
import { assetName, codexConfig, download, install, locations, selectCodexConfig, unpack } from './install.mjs';

let root, binary, archive;
const executable = process.platform === 'win32' ? 'digcrawl.exe' : 'digcrawl';
const skillFiles = ['SKILL.md', 'agents/openai.yaml', 'claude-code/.mcp.json.example',
  'codex/config.toml.example', 'mcp/README.md', 'references/error-handling.md', 'references/tool-routing.md'];

before(async () => {
  root = await fs.mkdtemp(path.join(os.tmpdir(), 'digcrawl-npm-test-'));
  const source = path.join(root, 'source');
  await fs.mkdir(source);
  // Native executable on every runner: no Unix shell fixtures on Windows.
  const fixture = path.join(root, 'fixture.go');
  await fs.writeFile(fixture, `package main
import("os";"path/filepath";"strings")
func main(){
 args:=os.Args[1:]
 f,e:=os.OpenFile(os.Getenv("FIXTURE_LOG"),os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600);if e!=nil{panic(e)}
 f.WriteString(strings.Join(args,"|")+"\\n");f.Close()
 if len(args)>0 && args[0]=="init" {p:=filepath.Join(os.Getenv("XDG_CONFIG_HOME"),"digcrawl","digcrawl-skill.yaml");os.MkdirAll(filepath.Dir(p),0700);os.WriteFile(p,[]byte("preserve-profile"),0600)}
 if len(args)>0 && args[0]=="bridge" && os.Getenv("FAIL_BRIDGE")=="1" {os.Exit(3)}
}`);
  binary = path.join(source, executable);
  const build = spawnSync('go', ['build', '-o', binary, fixture], { encoding: 'utf8' });
  assert.equal(build.status, 0, build.stderr || build.error?.message);
  for (const file of skillFiles) {
    const target = path.join(source, 'skills', 'digcrawl', file);
    await fs.mkdir(path.dirname(target), { recursive: true });
    await fs.writeFile(target, `fixture ${file}`);
  }
  const packed = path.join(root, 'fixture.tar.gz');
  await create({ file: packed, cwd: source, gzip: true }, [executable, 'skills']);
  archive = await fs.readFile(packed);
  if (process.platform === 'win32') {
    const entries = { [executable]: await fs.readFile(binary) };
    for (const file of skillFiles) entries[`skills/digcrawl/${file}`] = strToU8(`fixture ${file}`);
    archive = Buffer.from(zipSync(entries));
  }
});
after(async () => { if (root) await fs.rm(root, { recursive: true, force: true }); });

async function sandbox() {
  const home = await fs.mkdtemp(path.join(root, 'home with spaces-'));
  const env = { ...process.env, HOME: home, USERPROFILE: home,
    XDG_CONFIG_HOME: path.join(home, 'config'), XDG_DATA_HOME: path.join(home, 'data'),
    DIGCRAWL_INSTALL_DIR: path.join(home, 'bin'), CODEX_HOME: path.join(home, '.codex'),
    CLAUDE_CONFIG_DIR: path.join(home, '.claude'), FIXTURE_LOG: path.join(home, 'calls.log'),
    DIGCRAWL_SETUP_AGENTS: '1', DIGCRAWL_SETUP_CODEX: '1', DIGCRAWL_SETUP_CLAUDE: 'auto' };
  delete env.DIGCRAWL_CODEX_CONFIG;
  delete env.DIGCRAWL_SKILL_DIR;
  delete env.FAIL_BRIDGE;
  const name = assetName('1.0.0');
  const hash = createHash('sha256').update(archive).digest('hex');
  const requests = [];
  const fetchImpl = async url => {
    requests.push(url);
    return new Response(url.endsWith('/checksums.txt') ? `${hash}  ${name}\n` : archive);
  };
  return { env, home, cwd: home, log: () => {}, fetchImpl, requests };
}

test('release names cover exactly the five published targets', () => {
  for (const [system, cpu, expected] of [
    ['darwin', 'arm64', 'darwin_arm64.tar.gz'], ['darwin', 'x64', 'darwin_amd64.tar.gz'],
    ['linux', 'arm64', 'linux_arm64.tar.gz'], ['linux', 'x64', 'linux_amd64.tar.gz'], ['win32', 'x64', 'windows_amd64.zip'],
  ]) assert.equal(assetName('v1.0.0', system, cpu), `digcrawl_1.0.0_${expected}`);
  assert.throws(() => assetName('1.0.0', 'win32', 'arm64'), /Unsupported platform/);
  assert.throws(() => assetName('../latest'), /Invalid release/);
  assert.equal(locations({ APPDATA: 'C:/profile' }, 'win32', 'C:/home').profile, path.resolve('C:/profile/DigCrawl/digcrawl-skill.yaml'));
});

test('fresh install and update use stable paths, preserve profile and other MCP settings', async () => {
  const context = await sandbox();
  const dirs = locations(context.env, process.platform, context.home);
  const config = path.join(dirs.codexHome, 'config.toml');
  await fs.mkdir(dirs.codexHome);
  await fs.mkdir(dirs.claudeHome);
  await fs.writeFile(config, '# Keep this comment\nmodel = "test"\n[mcp_servers.other]\ncommand = "other"\n');
  await install({ version: '1.0.0' }, context);
  const first = await fs.readFile(config, 'utf8');
  assert.ok(first.includes('# Keep this comment'));
  assert.deepEqual(structuredClone(parse(first).mcp_servers.digcrawl), { command: dirs.binary, args: ['mcp', '--idle-timeout', '15m', '--config', dirs.profile] });
  assert.equal(parse(first).mcp_servers.other.command, 'other');
  for (const dir of [dirs.skill, path.join(dirs.codexHome, 'skills', 'digcrawl'), path.join(dirs.claudeHome, 'skills', 'digcrawl')]) {
    for (const file of skillFiles) assert.equal(await fs.readFile(path.join(dir, file), 'utf8'), `fixture ${file}`);
  }
  await fs.writeFile(dirs.profile, 'custom-profile-and-sessions');
  await install({ version: '1.0.0' }, context);
  assert.equal(await fs.readFile(dirs.profile, 'utf8'), 'custom-profile-and-sessions');
  assert.equal(await fs.readFile(config, 'utf8'), first);
  const calls = (await fs.readFile(context.env.FIXTURE_LOG, 'utf8')).trim().split('\n');
  assert.equal(calls.filter(c => c === 'init|--profile|skill').length, 1);
  assert.ok(calls.includes(`bridge|stop|--config|${dirs.profile}`));
  assert.ok(context.requests.every(url => url.includes('/v1.0.0/')));
  await assert.rejects(fs.stat(path.join(dirs.installDir, '.digcrawl-install.lock')), { code: 'ENOENT' });
});

test('checksum failure makes no installation or configuration changes', async () => {
  const context = await sandbox();
  context.fetchImpl = async url => new Response(url.endsWith('/checksums.txt') ? `${'0'.repeat(64)}  ${assetName('1.0.0')}` : archive);
  await assert.rejects(install({ version: '1.0.0' }, context), /SHA-256/);
  await assert.rejects(fs.stat(context.env.DIGCRAWL_INSTALL_DIR), { code: 'ENOENT' });
  await assert.rejects(fs.stat(context.env.CODEX_HOME), { code: 'ENOENT' });
});

test('failed Bridge stop preserves the installed executable and user settings', async () => {
  const context = await sandbox();
  const dirs = locations(context.env, process.platform, context.home);
  await fs.mkdir(path.dirname(dirs.profile), { recursive: true });
  await fs.writeFile(dirs.profile, 'existing');
  await fs.mkdir(dirs.installDir);
  await fs.writeFile(dirs.binary, 'old-program');
  context.env.FAIL_BRIDGE = '1';
  await assert.rejects(install({}, context), /bridge failed/);
  assert.equal(await fs.readFile(dirs.binary, 'utf8'), 'old-program');
  await assert.rejects(fs.stat(path.join(dirs.codexHome, 'config.toml')), { code: 'ENOENT' });
});

test('program-only and explicit Claude installations respect host scope', async () => {
  const only = await sandbox();
  const dirs = await install({ skipAgents: true }, only);
  await assert.rejects(fs.stat(dirs.profile), { code: 'ENOENT' });
  await assert.rejects(fs.stat(dirs.codexHome), { code: 'ENOENT' });
  const claude = await sandbox();
  const installed = await install({ target: 'claude-code' }, claude);
  await fs.stat(path.join(installed.claudeHome, 'skills', 'digcrawl', 'SKILL.md'));
  await assert.rejects(fs.stat(installed.codexHome), { code: 'ENOENT' });
});

test('project Codex overrides and user configuration symlinks are preserved', async () => {
  const context = await sandbox();
  const dirs = locations(context.env, process.platform, context.home);
  const project = path.join(context.home, 'project');
  await fs.mkdir(path.join(project, '.codex'), { recursive: true });
  await fs.mkdir(path.join(project, '.git'));
  const config = path.join(project, '.codex', 'config.toml');
  await fs.writeFile(config, '[mcp_servers.digcrawl]\ncommand="old"\n');
  assert.equal(await selectCodexConfig(context.env, dirs, project), config);
  await install({}, { ...context, cwd: project });
  assert.equal(parse(await fs.readFile(config, 'utf8')).mcp_servers.digcrawl.command, dirs.binary);
  await assert.rejects(fs.stat(path.join(dirs.codexHome, 'config.toml')), { code: 'ENOENT' });
  // Windows symlinks require privileges unavailable on some developer machines.
  if (process.platform !== 'win32') {
    const linked = path.join(dirs.codexHome, 'config.toml');
    await fs.symlink(config, linked);
    await install({}, context);
    assert.equal((await fs.lstat(linked)).isSymbolicLink(), true);
  }
});

test('TOML update preserves quoted paths, nested tables, and rejects ambiguous layouts', () => {
  const output = codexConfig('model="test"\n[mcp_servers."digcrawl"]\ncommand="old"\n[mcp_servers.digcrawl.env]\nX="1"\n[mcp_servers.other]\ncommand="keep"\n', 'C:\\Users\\test"quote\\digcrawl.exe', 'C:\\profile.yaml');
  assert.equal(parse(output).mcp_servers.digcrawl.command, 'C:\\Users\\test"quote\\digcrawl.exe');
  assert.equal(parse(output).mcp_servers.other.command, 'keep');
  assert.throws(() => codexConfig('mcp_servers.digcrawl.command="old"', '/new', '/profile'));
});

test('HTTP failures and excessive streaming downloads are rejected', async () => {
  await assert.rejects(download('https://example.invalid', 100, async () => new Response('', { status: 404 })), /HTTP 404/);
  await assert.rejects(download('https://example.invalid', 2, async () => new Response('large')), /size limit/);
});

test('ZIP parent traversal and TAR symlinks are rejected before installation', async () => {
  const zip = path.join(root, 'unsafe.zip');
  await fs.writeFile(zip, zipSync({ '../outside': strToU8('bad') }));
  await assert.rejects(unpack(zip, root, executable), /Unsafe release entry/);
  if (process.platform !== 'win32') {
    const directory = path.join(root, 'links');
    await fs.mkdir(directory);
    await fs.symlink('/outside', path.join(directory, executable));
    const tar = path.join(root, 'unsafe.tar.gz');
    await create({ file: tar, gzip: true, cwd: directory }, [executable]);
    await assert.rejects(unpack(tar, root, executable), /Unsupported release entry/);
  }
});

test('valid Windows ZIP extraction installs the full Skill on every test runner', async () => {
  const entries = { [executable]: await fs.readFile(binary) };
  for (const file of skillFiles) entries[`skills/digcrawl/${file}`] = strToU8(`fixture ${file}`);
  const zip = path.join(root, 'valid.zip');
  await fs.writeFile(zip, zipSync(entries));
  const destination = path.join(root, 'valid-zip');
  await fs.mkdir(destination);
  await unpack(zip, destination, executable);
  for (const file of skillFiles) assert.equal(await fs.readFile(path.join(destination, 'skills', 'digcrawl', file), 'utf8'), `fixture ${file}`);
});

test('npm tarball contains only the public installer and documents; npx entry resolves', async () => {
  const npmCli = process.env.npm_execpath;
  assert.ok(npmCli, 'Run this test through npm test');
  const result = spawnSync(process.execPath, [npmCli, 'pack', '--json', '--ignore-scripts', '--pack-destination', root], { encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  const packed = JSON.parse(result.stdout)[0];
  assert.deepEqual(packed.files.map(f => f.path).sort(), ['LICENSE', 'README.md', 'README.zh-CN.md', 'npm/cli.mjs', 'npm/install.mjs', 'package.json'].sort());
  const pkg = JSON.parse(await fs.readFile(new URL('../package.json', import.meta.url), 'utf8'));
  assert.equal(pkg.digcrawlRelease, '1.0.0');
  assert.equal(pkg.scripts.postinstall, undefined);
  const extracted = path.join(root, 'packed');
  await fs.mkdir(extracted);
  await unpackPackage(path.join(root, packed.filename), extracted);
  // Execute the actual packed entry, resolving its locked runtime dependencies.
  await fs.symlink(path.resolve('node_modules'), path.join(extracted, 'package', 'node_modules'), process.platform === 'win32' ? 'junction' : 'dir');
  const cli = spawnSync(process.execPath, [path.join(extracted, 'package', 'npm', 'cli.mjs'), '--help'], { encoding: 'utf8' });
  assert.equal(cli.status, 0, cli.stderr);
  assert.match(cli.stdout, /npx digcrawl-skill install/);
  const invalid = spawnSync(process.execPath, [path.join(extracted, 'package', 'npm', 'cli.mjs'), 'install', '--bad-flag'], { encoding: 'utf8' });
  assert.equal(invalid.status, 1);
  assert.match(invalid.stderr, /Unknown option/);
});

async function unpackPackage(file, cwd) {
  const { extract } = await import('tar');
  await extract({ file, cwd, strict: true });
}
