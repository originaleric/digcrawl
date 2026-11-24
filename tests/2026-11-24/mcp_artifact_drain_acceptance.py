"""macOS native acceptance: python3 mcp_artifact_drain_acceptance.py CLI BROWSER_RUNTIME [OLD_ARTIFACT_AWARE_CLI] [OPERATION].
Isolated HOME, real Chromium screenshot, stdio MCP, and all three maintenance paths.
"""
import base64, hashlib, http.server, json, os, pathlib, queue, shutil, tarfile
import subprocess, sys, tempfile, threading, time

binary = pathlib.Path(sys.argv[1]).resolve()
browsers = pathlib.Path(sys.argv[2]).resolve()
previous = pathlib.Path(sys.argv[3]).resolve() if len(sys.argv)>3 else binary
operations = (sys.argv[4],) if len(sys.argv)>4 else ('stop', 'restart', 'install', 'cancel_stop', 'update')
for operation in operations:
    root = pathlib.Path(tempfile.mkdtemp(prefix='dcmd-', dir='/tmp'))
    env = dict(os.environ, HOME=str(root), XDG_CONFIG_HOME=str(root/'config'),
               DIGCRAWL_BROWSER_DIR=str(browsers), XDG_DATA_HOME=str(root/'data'),
               DIGCRAWL_INSTALL_DIR=str(root/'bin'))
    installed = root/'bin/digcrawl'
    installed.parent.mkdir()
    shutil.copy2(previous if operation == 'update' else binary, installed)
    original_inode = installed.stat().st_ino
    original_digest = hashlib.sha256(installed.read_bytes()).hexdigest()
    release_digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    profile = root/'config/digcrawl/digcrawl-skill.yaml'
    profile.parent.mkdir(parents=True)
    profile.write_text('Gateway:\n  InstanceID: mcp-delivery\n  AllowedTools: [browser.browse]\n'
                       '  ArtifactEnabled: true\n  ArtifactDir: '+str(root/'artifacts')+'\n'
                       'LocalBrowser:\n  Headless: true\n  CookieStoreDir: '+str(root/'cookies')+'\n'
                       '  UserDataDir: '+str(root/'browser')+'\n'
                       'Skill:\n  ASR:\n    Enabled: false\nOCR:\n  Enabled: false\n')
    profile.chmod(0o600)
    entered, release = threading.Event(), threading.Event()
    class Page(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            entered.set()
            if not release.wait(60):
                return
            body = b'<html><body><h1>Accepted screenshot completes during maintenance</h1></body></html>'
            self.send_response(200)
            self.send_header('Content-Type', 'text/html')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        def log_message(self, *_):
            pass
    page = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Page)
    threading.Thread(target=page.serve_forever, daemon=True).start()
    mcp = control = None
    watching = False
    def run(*command, check=True):
        r = subprocess.run([str(installed), *command, '--config', str(profile)],
                           cwd=root, env=env, capture_output=True, text=True, timeout=40)
        if check and r.returncode:
            raise RuntimeError((command, r.returncode, r.stdout, r.stderr))
        return r
    try:
        if operation == 'update':
            run('bridge', 'watch', '--install')
            watching = True
        with (root/'mcp.log').open('w') as log:
            mcp = subprocess.Popen([str(installed), 'mcp', '--idle-timeout', '0', '--config', str(profile)],
                                   cwd=root, env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=log, text=True)
        replies = queue.Queue()
        def read_replies():
            for line in mcp.stdout:
                replies.put(json.loads(line))
        threading.Thread(target=read_replies, daemon=True).start()
        def send(message):
            mcp.stdin.write(json.dumps(dict(jsonrpc='2.0', **message))+'\n')
            mcp.stdin.flush()
        def reply(identifier):
            until = time.monotonic()+45
            while time.monotonic() < until:
                value = replies.get(timeout=max(.01, until-time.monotonic()))
                if value.get('id') == identifier:
                    return value
            raise RuntimeError('missing MCP response')
        send(dict(id=1, method='initialize', params=dict(protocolVersion='2025-03-26',
             capabilities={}, clientInfo=dict(name='native-delivery-test', version='1'))))
        assert 'result' in reply(1)
        send(dict(method='notifications/initialized', params={}))
        send(dict(id=2, method='tools/call', params=dict(name='browser.browse', arguments=dict(
             url='http://127.0.0.1:'+str(page.server_port)+'/page', include_screenshot=True))))
        assert entered.wait(20), (root/'mcp.log').read_text()
        if operation == 'update':
            archive = root/'release.tar.gz'
            with tarfile.open(archive, 'w:gz') as bundle:
                bundle.add(binary, arcname='digcrawl')
                bundle.add(pathlib.Path(__file__).resolve().parents[2]/'.agents/skills/digcrawl', arcname='skills/digcrawl')
            runner = root/'install.mjs'
            runner.write_text("import fs from 'node:fs/promises';\nimport {createHash} from 'node:crypto';\n"
                "const {install,assetName}=await import(process.env.INSTALLER_MODULE);\n"
                "const data=await fs.readFile(process.env.FIXTURE_ARCHIVE);\n"
                "const hash=createHash('sha256').update(data).digest('hex');\n"
                "await install({version:'1.0.0',skipAgents:true,skipCodex:true},{env:process.env,home:process.env.HOME,cwd:process.cwd(),fetchImpl:async url=>new Response(url.endsWith('/checksums.txt')?hash+'  '+assetName('1.0.0')+String.fromCharCode(10):data)});\n")
            env.update(INSTALLER_MODULE=(pathlib.Path(__file__).resolve().parents[2]/'npm/install.mjs').as_uri(), FIXTURE_ARCHIVE=str(archive))
            argv = ['node', str(runner)]
        elif operation == 'install':
            argv = ['browser', 'install', '--replace-executable', str(installed),
                    '--installer-lock', str(installed.parent), '--installer-lock', str(root/'asset-lock')]
        else:
            argv = ['bridge', 'stop' if operation == 'cancel_stop' else operation, '--config', str(profile)]
        with (root/'control.log').open('w') as log:
            control = subprocess.Popen(argv if operation == 'update' else [str(installed), *argv], cwd=root, env=env,
                                       stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=log, text=True)
        key = hashlib.sha256(str(profile.resolve()).encode()).hexdigest()[:24]
        socket = root/'Library/Application Support/digcrawl/run'/(key+'.sock')
        until = time.monotonic()+15
        while time.monotonic() < until:
            if pathlib.Path(str(socket)+'.paused').exists() or list(socket.parent.glob(socket.name+'.stop-*')):
                break
            if control.poll() is not None:
                raise RuntimeError((operation, control.returncode, (root/'control.log').read_text()))
            time.sleep(.02)
        else:
            raise RuntimeError('maintenance did not begin')
        time.sleep(.15)
        assert control.poll() is None, 'maintenance finished before accepted screenshot'
        if operation == 'update':
            assert installed.stat().st_ino == original_inode, 'program published before accepted screenshot finished'
        if operation == 'cancel_stop':
            control.terminate()
            assert control.wait(timeout=10) != 0, 'canceled stop reported completion'
            control = None
        release.set()
        response = reply(2)
        assert 'result' in response and not response['result'].get('isError'), response
        images = [item for item in response['result']['content'] if item['type'] == 'image']
        assert images and base64.b64decode(images[0]['data']).startswith(b'\x89PNG'), response
        metadata = response['result'].get('structuredContent', {})
        assert 'inline_data' not in json.dumps(metadata), 'binary data duplicated in metadata'
        if operation == 'install':
            assert control.stdout.readline().strip() == 'DIGCRAWL_INSTALL_READY_V2', (root/'control.log').read_text()
            control.stdin.write('release\n'); control.stdin.flush()
            assert control.stdout.readline().strip() == 'DIGCRAWL_INSTALL_RELEASED'
        if control:
            assert control.wait(timeout=30) == 0, (root/'control.log').read_text()
        control = None
        if operation in ('restart', 'update'):
            until = time.monotonic()+15
            while time.monotonic()<until:
                if json.loads(run('bridge', 'status', check=False).stdout)['ready']:
                    break
                time.sleep(.1)
            else:
                raise AssertionError('updated service did not become ready')
        if operation == 'update':
            assert installed.stat().st_ino != original_inode
            assert hashlib.sha256(installed.read_bytes()).hexdigest() == release_digest
            run('doctor')
        print(json.dumps(dict(operation=operation, mcp_success=True, screenshot_bytes=len(base64.b64decode(images[0]['data'])), different_build_update=operation == 'update' and original_digest != release_digest, evidence_root=str(root))), flush=True)
    finally:
        release.set()
        if control and control.poll() is None:
            control.stdin.close()
            control.wait(timeout=45)
        if mcp and mcp.poll() is None:
            mcp.stdin.close()
            mcp.wait(timeout=15)
        if watching:
            removed = run('bridge', 'watch', '--uninstall', check=False)
            assert removed.returncode == 0, removed.stderr
        cleanup = run('bridge', 'stop', check=False)
        page.shutdown()
        page.server_close()
        assert cleanup.returncode == 0, cleanup.stderr
        print('cleanup_exit', cleanup.returncode, flush=True)
