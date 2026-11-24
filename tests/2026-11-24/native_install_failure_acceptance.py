"""macOS: python3 native_install_failure_acceptance.py NEW_CLI OLD_DRAINING_CLI BROWSER_RUNTIME.
Real npm publication failure before and after replacing the program; isolated jobs only.
"""
import hashlib, json, os, pathlib, shutil, subprocess, sys, tarfile, tempfile, time, selectors

new_binary, old_binary, browsers = [pathlib.Path(p).resolve() for p in sys.argv[1:4]]
repo = pathlib.Path(__file__).resolve().parents[2]
new_hash = hashlib.sha256(new_binary.read_bytes()).hexdigest()
old_hash = hashlib.sha256(old_binary.read_bytes()).hexdigest()
assert new_hash != old_hash
cases = (sys.argv[4],) if len(sys.argv)>4 else ('before_publication', 'after_publication', 'guard_terminated')
for failure in cases:
    root = pathlib.Path(tempfile.mkdtemp(prefix='dcfail-', dir='/tmp'))
    binary = root/'bin/digcrawl'; binary.parent.mkdir()
    shutil.copy2(old_binary, binary)
    env = dict(os.environ, HOME=str(root), XDG_CONFIG_HOME=str(root/'config'),
               XDG_DATA_HOME=str(root/'data'), DIGCRAWL_BROWSER_DIR=str(browsers),
               DIGCRAWL_INSTALL_DIR=str(binary.parent), DIGCRAWL_SETUP_CLAUDE='0',
               DIGCRAWL_SETUP_AGENTS='1', DIGCRAWL_SETUP_CODEX='0',
               CODEX_HOME=str(root/'.codex'), CLAUDE_CONFIG_DIR=str(root/'.claude'),
               DIGCRAWL_SKILL_DIR=str(root/'data/digcrawl/skills/digcrawl'))
    profile = root/'config/digcrawl/digcrawl-skill.yaml'; profile.parent.mkdir(parents=True)
    browser_base = root/'browser'
    profile.write_text('Gateway:\n  InstanceID: install-recovery\n  AllowedTools: [gateway.ping]\n'
                       '  ArtifactEnabled: false\n  ArtifactDir: '+str(root/'artifacts')+'\n'
                       'LocalBrowser:\n  Headless: true\n  CookieStoreDir: '+str(root/'cookies')+'\n'
                       '  UserDataDir: '+str(browser_base)+'\nSkill:\n  ASR:\n    Enabled: false\n'
                       'OCR:\n  Enabled: false\n')
    profile.chmod(0o600)
    key = hashlib.sha256(str(profile.resolve()).encode()).hexdigest()[:24]
    runtime = root/'Library/Application Support/digcrawl/run'
    socket = runtime/(key+'.sock')
    plist = root/'Library/LaunchAgents'/('com.dig.digcrawl.watch.'+key+'.plist')
    archive = root/'release.tar.gz'
    with tarfile.open(archive, 'w:gz') as bundle:
        bundle.add(new_binary, arcname='digcrawl')
        bundle.add(repo/'.agents/skills/digcrawl', arcname='skills/digcrawl')
    runner = root/'install.mjs'
    runner.write_text('''import fs from 'node:fs/promises';
import {createHash} from 'node:crypto';
const {install,assetName}=await import(process.env.INSTALLER_MODULE);
const data=await fs.readFile(process.env.FIXTURE_ARCHIVE);
const hash=createHash('sha256').update(data).digest('hex');
try {
await install({version:'1.0.0',skipCodex:true},{env:process.env,home:process.env.HOME,cwd:process.cwd(),fetchImpl:async url=>new Response(url.endsWith('/checksums.txt')?hash+'  '+assetName('1.0.0')+String.fromCharCode(10):data)});
} catch(error) { console.error(error.message); process.exitCode=1; }
''')
    env.update(INSTALLER_MODULE=(repo/'npm/install.mjs').as_uri(), FIXTURE_ARCHIVE=str(archive))
    def run(*command, check=True):
        r = subprocess.run([str(binary), *command, '--config', str(profile)], cwd=root,
                           env=env, capture_output=True, text=True, timeout=45)
        if check and r.returncode:
            raise RuntimeError((command, r.returncode, r.stdout, r.stderr))
        return r
    def ready():
        until = time.monotonic()+15
        while time.monotonic() < until:
            state = json.loads(run('bridge', 'status', check=False).stdout)
            if state['ready'] and not state['paused'] and not state['stopping']:
                return state
            time.sleep(.1)
        raise AssertionError(('watcher did not recover after failed installation', state))
    watching = False
    try:
        run('bridge', 'restart'); run('doctor')
        run('bridge', 'watch', '--install'); watching = True
        definition = plist.read_bytes()
        marker = browser_base/'digcrawl-isolated/retained-data'
        marker.write_text('preserve browser data across failed publication')
        if failure == 'before_publication':
            # The real installer must refuse a package-manager-owned symlink
            # after its native guard has stopped and prepared the existing job.
            prior = root/'prior/digcrawl'; prior.parent.mkdir()
            binary.rename(prior); binary.symlink_to(prior)
        elif failure == 'after_publication':
            # This is reached after program publication but before portable
            # Skill publication. No private config or daily installation changes.
            blocker = root/'data/digcrawl/skills'; blocker.mkdir(parents=True)
            (blocker/'.digcrawl-install.lock').touch(mode=0o600)
            blocker.chmod(0o500)
        if failure == 'guard_terminated':
            guard = subprocess.Popen([str(new_binary), 'browser', 'install', '--replace-executable', str(binary),
                    '--installer-lock', str(binary.parent), '--installer-lock', str(root/'publication-lock')],
                    cwd=root, env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            try:
                with selectors.DefaultSelector() as events:
                    events.register(guard.stdout, selectors.EVENT_READ)
                    assert events.select(45), 'native guard preparation timeout'
                assert guard.stdout.readline().strip() == 'DIGCRAWL_INSTALL_READY_V2'
                guard.terminate()
                out, err = guard.communicate(timeout=50)
                result = subprocess.CompletedProcess(guard.args, guard.returncode, out, err)
            finally:
                if guard.poll() is None:
                    guard.stdin.close(); guard.wait(timeout=50)
        else:
            result = subprocess.run(['node', str(runner)], cwd=root, env=env,
                                    capture_output=True, text=True, timeout=70)
        (root/'failed-install.log').write_text(result.stdout+result.stderr)
        assert result.returncode != 0, 'injected failure was reported as successful installation'
        if failure == 'before_publication':
            assert 'Refusing to replace a managed binary symlink' in result.stderr, result.stderr
            assert binary.is_symlink() and hashlib.sha256(binary.read_bytes()).hexdigest() == old_hash
        elif failure == 'after_publication':
            assert 'EACCES' in result.stderr, result.stderr
            assert hashlib.sha256(binary.read_bytes()).hexdigest() == new_hash, 'post-publication failure must retain the already-published program'
        else:
            assert 'interrupted before release' in result.stderr, result.stderr
            assert hashlib.sha256(binary.read_bytes()).hexdigest() == old_hash
        ready(); run('doctor')
        assert plist.read_bytes() == definition, 'watcher launch definition changed during failure recovery'
        assert not list(runtime.glob(key+'.sock.stop-*')), 'failed operation left a maintenance marker'
        assert marker.read_text() == 'preserve browser data across failed publication'
        # Remove only this test's blocker, then prove an ordinary retry works.
        if failure == 'before_publication':
            binary.unlink(); shutil.copy2(prior, binary)
        elif failure == 'after_publication':
            blocker.chmod(0o700)
        retried = subprocess.run(['node', str(runner)], cwd=root, env=env,
                                 capture_output=True, text=True, timeout=70)
        (root/'retry-install.log').write_text(retried.stdout+retried.stderr)
        assert retried.returncode == 0, (retried.stdout, retried.stderr)
        ready(); run('doctor')
        assert hashlib.sha256(binary.read_bytes()).hexdigest() == new_hash
        assert (root/'data/digcrawl/skills/digcrawl/SKILL.md').is_file()
        assert marker.read_text() == 'preserve browser data across failed publication'
        print(json.dumps(dict(failure=failure, expected_installer_failure=True,
                              watcher_and_browser_recovered=True, retry_succeeded=True,
                              browser_data_retained=True, evidence_root=str(root))), flush=True)
    finally:
        if failure == 'after_publication':
            blocker.chmod(0o700)
        if watching:
            removed = run('bridge', 'watch', '--uninstall', check=False)
            assert removed.returncode == 0, removed.stderr
        stopped = run('bridge', 'stop', check=False)
        assert stopped.returncode == 0, stopped.stderr
        print('cleanup_exit', stopped.returncode, flush=True)
