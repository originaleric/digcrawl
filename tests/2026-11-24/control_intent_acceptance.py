"""macOS native regression: python3 control_intent_acceptance.py /absolute/digcrawl.
Every competing controller is rejected before mutation; retry after completion.
"""
import fcntl, hashlib, json, os, pathlib, signal, subprocess, sys, tempfile, time

binary = str(pathlib.Path(sys.argv[1]).resolve())
root = pathlib.Path(tempfile.mkdtemp(prefix='dcserial-', dir='/tmp'))
env = dict(os.environ, HOME=str(root), XDG_CONFIG_HOME=str(root/'config'))
profile = root/'profile.yaml'
base = ('Gateway:\n  InstanceID: serial-maintenance\n  AllowedTools: [gateway.ping]\n'
        '  ArtifactEnabled: false\n  ArtifactDir: '+str(root/'artifacts')+'\n'
        'LocalBrowser:\n  Headless: true\n  CookieStoreDir: '+str(root/'cookies')+'\n'
        '  UserDataDir: '+str(root/'browser')+'\nSkill:\n  ASR:\n    Enabled: false\n'
        'OCR:\n  Enabled: false\nProviders:\n  PlatformEnabled: true\n')
profile.write_text(base)
profile.chmod(0o600)
key = hashlib.sha256(str(profile.resolve()).encode()).hexdigest()[:24]
socket = root/'Library/Application Support/digcrawl/run'/(key+'.sock')
controller = None

def run(*command, check=True):
    result = subprocess.run([binary, *command, '--config', str(profile)], env=env,
                            text=True, capture_output=True, timeout=45)
    if check and result.returncode:
        raise RuntimeError((command, result.returncode, result.stdout, result.stderr))
    return result

def busy(command):
    result = run('bridge', command, check=False)
    assert result.returncode != 0 and 'maintenance operation is in progress' in result.stderr, result
    return result.returncode

try:
    # The original finding: A has published pause but is still parsing config,
    # before the first .stop marker. B must not complete inside this gap.
    for command in ('stop', 'restart'):
        profile.write_text(base+'# valid profile padding\n'*200000)
        run('bridge', 'restart')
        controller = subprocess.Popen([binary, 'bridge', command, '--config', str(profile)],
                                      env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        until = time.monotonic()+5
        while time.monotonic() < until:
            if pathlib.Path(str(socket)+'.paused').exists():
                os.kill(controller.pid, signal.SIGSTOP)
                break
            time.sleep(.0005)
        else:
            raise RuntimeError('preparation boundary not observed')
        assert not list(socket.parent.glob(socket.name+'.stop-*')), 'missed preparation boundary'
        fd = os.open(socket.parent/'.digcrawl-install.lock', os.O_RDWR)
        try:
            try:
                fcntl.flock(fd, fcntl.LOCK_EX|fcntl.LOCK_NB)
            except BlockingIOError:
                pass
            else:
                raise AssertionError('A released maintenance lock before config preparation')
        finally:
            os.close(fd)
        rejected = busy('restart')
        assert pathlib.Path(str(socket)+'.paused').exists()
        assert not list(socket.parent.glob(socket.name+'.stop-*'))
        os.kill(controller.pid, signal.SIGCONT)
        out, err = controller.communicate(timeout=40)
        assert controller.returncode == 0, err
        controller = None
        run('bridge', 'restart')
        assert json.loads(run('bridge', 'status').stdout)['ready']
        print(json.dumps({'preparation_command': command, 'competing_restart_exit': rejected,
                          'retry_ready': True}), flush=True)
    profile.write_text(base)
    # A second boundary: the first controller is already observing resources.
    commands = root/'commands'
    commands.mkdir()
    wrapper = commands/'ps'
    wrapper.write_text('#!/bin/sh\nif [ ! -f "$REVIEW_SEEN" ]; then\n touch "$REVIEW_SEEN"\n while [ ! -f "$REVIEW_RELEASE" ]; do /bin/sleep 0.02; done\nfi\nexec /bin/ps "$@"\n')
    wrapper.chmod(0o700)
    delayed = dict(env, PATH=str(commands)+':'+env['PATH'], REVIEW_SEEN=str(root/'seen'),
                   REVIEW_RELEASE=str(root/'release'))
    controller = subprocess.Popen([binary, 'bridge', 'restart', '--config', str(profile)],
                                  env=delayed, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    until = time.monotonic()+8
    while not (root/'seen').exists():
        if time.monotonic() > until:
            raise RuntimeError('resource observation boundary not reached')
        time.sleep(.01)
    rejected = busy('stop')
    (root/'release').touch()
    out, err = controller.communicate(timeout=40)
    assert controller.returncode == 0, err
    controller = None
    run('bridge', 'stop')
    state = json.loads(run('bridge', 'status', check=False).stdout)
    assert state['paused'] and not state['ready'] and not state.get('owners'), state
    print(json.dumps({'resource_boundary_stop_exit': rejected, 'retry_stop_paused': True}), flush=True)
finally:
    (root/'release').touch()
    if controller and controller.poll() is None:
        os.kill(controller.pid, signal.SIGCONT)
        controller.terminate()
        controller.wait(timeout=10)
    cleanup = run('bridge', 'stop', check=False)
    print('cleanup_exit', cleanup.returncode, 'evidence_root', root, flush=True)
    assert cleanup.returncode == 0, cleanup.stderr
