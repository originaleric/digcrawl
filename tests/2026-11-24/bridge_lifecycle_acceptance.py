"""Native macOS acceptance; explicit binaries, isolated HOME and owned launchd labels.
python3 bridge_lifecycle_acceptance.py --binary NEW --legacy OLD --browser-dir RUNTIME
"""
import argparse, hashlib, json, os, pathlib, selectors, shutil, subprocess, tempfile, time

parser = argparse.ArgumentParser()
parser.add_argument('--binary', required=True)
parser.add_argument('--legacy', required=True)
parser.add_argument('--browser-dir', required=True)
args = parser.parse_args()
root = pathlib.Path(tempfile.mkdtemp(prefix='dcl-', dir='/tmp'))
env = dict(os.environ, HOME=str(root), TZ='America/New_York', DIGCRAWL_BROWSER_DIR=args.browser_dir)
env['XDG_CONFIG_HOME'] = str(root / '.config')
env['XDG_DATA_HOME'] = str(root / '.local/share')
# launchd intentionally receives only HOME. Mirror a normal installed runtime
# at that HOME's default location, reusing immutable browser files without a
# download. A shell-only override would test a different launch environment.
runtime = root / '.local/share/digcrawl/browsers'
runtime.parent.mkdir(parents=True)
runtime.symlink_to(pathlib.Path(args.browser_dir).resolve(), target_is_directory=True)
installed = root / 'bin/digcrawl'
installed.parent.mkdir()
shutil.copy2(args.binary, installed)
other = root / 'other/digcrawl'
other.parent.mkdir()
shutil.copy2(args.legacy, other)
profiles, jobs, guard = [], [], None

def profile(name):
    p = root / (name + '.yaml')
    p.write_text('Gateway:\n  InstanceID: lifecycle-' + name + '\n  AllowedTools: [gateway.ping]\n  ArtifactEnabled: false\n  ArtifactDir: ' + str(root / (name+'-artifacts')) + '\nLocalBrowser:\n  Headless: true\n  CookieStoreDir: ' + str(root / (name+'-cookies')) + '\n  UserDataDir: ' + str(root / (name+'-browser')) + '\nSkill:\n  ASR:\n    Enabled: false\nOCR:\n  Enabled: false\nProviders:\n  PlatformEnabled: true\n')
    p.chmod(0o600)
    profiles.append(p)
    return p

def run(binary, p, *cmd, check=True, environment=env):
    result = subprocess.run([str(binary), *cmd, '--config', str(p)], env=environment, capture_output=True, text=True, timeout=55)
    if check and result.returncode:
        raise RuntimeError((cmd, result.returncode, result.stdout, result.stderr))
    return result

def key(p):
    return hashlib.sha256(str(p.resolve()).encode()).hexdigest()[:24]

def socket(p):
    return root / 'Library/Application Support/digcrawl/run' / (key(p)+'.sock')

def job(p):
    result = subprocess.run(['launchctl', 'print', 'gui/'+str(os.getuid())+'/com.dig.digcrawl.watch.'+key(p)], capture_output=True, text=True, timeout=5)
    if result.returncode:
        assert 'Could not find service' in result.stderr, result.stderr
        return None
    for line in result.stdout.splitlines():
        fields = line.split()
        if len(fields) == 3 and fields[:2] == ['pid', '=']:
            return int(fields[2])
    return 0

def running_job(p):
    end = time.monotonic()+10
    while time.monotonic() < end:
        pid = job(p)
        if pid:
            return pid
        time.sleep(.1)
    raise RuntimeError('watcher did not start: '+str(p))

def read_line(process, seconds=55):
    with selectors.DefaultSelector() as select:
        select.register(process.stdout, selectors.EVENT_READ)
        if not select.select(seconds):
            raise RuntimeError('installer acknowledgement timeout')
        line = process.stdout.readline().strip()
    return line


try:
    active, paused, unrelated = profile('active'), profile('paused'), profile('unrelated')
    for binary, p in [(installed, active), (installed, paused), (other, unrelated)]:
        run(binary, p, 'bridge', 'restart', environment=dict(env, TZ='UTC'))
        run(binary, p, 'bridge', 'watch', '--install')
        jobs.append((binary, p))
        running_job(p)
    run(installed, paused, 'bridge', 'stop')
    unrelated_pid = running_job(unrelated)
    # Cancellation before publication restores the old registered watcher and
    # removes only this attempt's maintenance intents.
    # Active jobs support task draining; unrelated historical jobs are untouched.
    def begin():
        global guard
        retired_pids = [running_job(p) for p in [active, paused]]
        guard = subprocess.Popen([args.binary, 'browser', 'install', '--replace-executable', str(installed), '--installer-lock', str(installed.parent), '--installer-lock', str(root/'config-lock')], env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=(root/'guard.log').open('a'), text=True)
        assert read_line(guard) == 'DIGCRAWL_INSTALL_READY_V2', (root/'guard.log').read_text()
        for pid in retired_pids:
            state = subprocess.run(['ps', '-p', str(pid), '-o', 'stat='], capture_output=True, text=True).stdout.strip()
            assert not state or state.startswith('Z'), (pid, state)
        for p in [active, paused]:
            assert job(p) is None
            assert list(socket(p).parent.glob(socket(p).name+'.stop-*'))
            state = json.loads(run(args.binary, p, 'bridge', 'status', check=False).stdout)
            assert not state['ready'] and not state.get('owners', []), state
        result = run(args.binary, active, 'bridge', 'watch', '--uninstall', check=False)
        assert result.returncode != 0 and 'maintenance operation' in result.stderr, result.stderr
        assert job(unrelated) == unrelated_pid
    begin()
    guard.stdin.close()
    assert guard.wait(timeout=55) != 0
    guard = None
    for p in [active, paused]:
        running_job(p)
        assert not list(socket(p).parent.glob(socket(p).name+'.stop-*'))
    print('parent_eof_restores_existing_jobs_and_releases_own_markers', True, flush=True)
    print('concurrent_watch_mutation_rejected_and_unrelated_job_retained', True, flush=True)
    old_pid = running_job(active)
    begin()
    stage = installed.with_suffix('.next')
    shutil.copy2(args.binary, stage)
    os.replace(stage, installed)
    guard.stdin.write('release\n')
    guard.stdin.flush()
    assert read_line(guard) == 'DIGCRAWL_INSTALL_RELEASED', (root/'guard.log').read_text()
    assert guard.wait(timeout=10) == 0
    guard = None
    assert running_job(active) != old_pid
    assert not list(socket(active).parent.glob(socket(active).name+'.stop-*'))
    assert json.loads(run(installed, paused, 'bridge', 'status', check=False).stdout)['paused']
    print('existing_watcher_replaced_and_operator_pause_retained', True, flush=True)
    # Reinstallation records explicit runtime deployment paths in the existing
    # plist. Remove the default-location fixture to prove that the relaunched
    # watcher/Bridge uses that configuration after socket recovery.
    run(installed, active, 'bridge', 'watch', '--install')
    running_job(active)
    runtime.unlink()
    run(installed, active, 'bridge', 'restart')
    run(installed, active, 'doctor')
    fields = pathlib.Path(str(socket(active))+'.owner').read_text().split()
    assert len(fields) >= 6 and fields[2].startswith('process-v2:') and fields[5].startswith('process-v2:'), fields
    socket(active).unlink()
    started = time.monotonic()
    while time.monotonic()-started < 90:
        state = json.loads(run(installed, active, 'bridge', 'status', check=False).stdout)
        if state['ready']:
            break
        time.sleep(1)
    else:
        details = subprocess.run(['launchctl', 'print', 'gui/'+str(os.getuid())+'/com.dig.digcrawl.watch.'+key(active)], capture_output=True, text=True, timeout=5)
        log = socket(active).parent / ('com.dig.digcrawl.watch.'+key(active)+'.log')
        raise RuntimeError(('new watcher failed recovery within 90 seconds', state, details.stdout, log.read_text() if log.exists() else 'missing log'))
    run(installed, active, 'doctor')
    assert job(unrelated) == unrelated_pid
    print('new_watcher_recovers_unlinked_socket_with_real_browser', True, 'seconds', round(time.monotonic()-started, 2), flush=True)
    # Exercise controlled downgrade through the same handoff. The restored old
    # job must see only old-format state after the new owner has been stopped.
    begin()
    stage = installed.with_suffix('.rollback')
    shutil.copy2(args.legacy, stage)
    os.replace(stage, installed)
    guard.stdin.write('release\n')
    guard.stdin.flush()
    assert read_line(guard) == 'DIGCRAWL_INSTALL_RELEASED'
    assert guard.wait(timeout=10) == 0
    guard = None
    running_job(active)
    run(installed, active, 'bridge', 'restart')
    assert json.loads(run(installed, active, 'bridge', 'status').stdout)['ready']
    print('controlled_downgrade_reloads_matching_watcher', True, flush=True)
finally:
    if guard:
        guard.stdin.close()
        try:
            guard.wait(timeout=55)
        except subprocess.TimeoutExpired:
            guard.terminate()
            guard.wait(timeout=55)
    failures = []
    # Stop each job with its installed controller, including an explicitly downgraded old version.
    for binary, p in reversed(jobs):
        for command in [('bridge', 'watch', '--uninstall'), ('bridge', 'stop')]:
            result = run(binary, p, *command, check=False)
            if result.returncode:
                failures.append((command, p, result.stderr))
        if job(p) is not None:
            failures.append(('job remained', p))
    print('evidence_root', root, 'cleanup_failures', failures, flush=True)
    assert not failures
