"""Native macOS regression: python3 watcher_directory_acceptance.py /absolute/digcrawl /absolute/browser-runtime"""
import pathlib,tempfile,subprocess,os,json,time,hashlib,sys,selectors
binary=sys.argv[1]
root=pathlib.Path(tempfile.mkdtemp(prefix='dcp5cwd-',dir='/tmp'))
working=root/'project';working.mkdir()
env=dict(os.environ,HOME=str(root))
if len(sys.argv)>2: env['DIGCRAWL_BROWSER_DIR']=str(pathlib.Path(sys.argv[2]).resolve())
if not env.get('DIGCRAWL_BROWSER_DIR'): raise SystemExit('pass the installed browser runtime directory as the second argument')
p=root/'profile.yaml'
p.write_text('Gateway:\n  InstanceID: cwd-review\n  AllowedTools: [gateway.ping]\n  ArtifactEnabled: false\n  ArtifactDir: '+str(root/'artifacts')+'\nLocalBrowser:\n  Headless: true\n  CookieStoreDir: '+str(root/'cookies')+'\n  UserDataDir: storage/browser\nSkill:\n  ASR:\n    Enabled: false\nOCR:\n  Enabled: false\nProviders:\n  PlatformEnabled: true\n')
p.chmod(0o600)
original_profile=p.read_bytes()
key=hashlib.sha256(str(p.resolve()).encode()).hexdigest()[:24]
socket=root/'Library/Application Support/digcrawl/run'/(key+'.sock')
installed=False
guard=None
def run(*args,check=True):
 r=subprocess.run([binary,*args,'--config',str(p)],cwd=working,env=env,text=True,capture_output=True,timeout=45)
 if check and r.returncode:raise RuntimeError((args,r.returncode,r.stdout,r.stderr))
 return r
def browser_dir():
 fields=pathlib.Path(str(socket)+'.owner').read_text().split()
 pid=fields[3]
 args=subprocess.check_output(['/bin/ps','-p',pid,'-o','command='],text=True)
 return args.split('--user-data-dir=',1)[1].split(' --',1)[0].strip()
try:
 run('bridge','restart');run('doctor')
 before=browser_dir()
 run('bridge','watch','--install');installed=True
 plist=root/'Library/LaunchAgents'/('com.dig.digcrawl.watch.'+key+'.plist')
 definition=plist.read_bytes()
 job=json.loads(subprocess.check_output(['plutil','-convert','json','-o','-',str(plist)]))
 assert pathlib.Path(job['WorkingDirectory']).resolve()==working.resolve(),job
 # The same installed definition must retain its resource base across an
 # installer handoff launched from a different working directory.
 elsewhere=root/'installer-cwd';elsewhere.mkdir()
 guard=subprocess.Popen([binary,'browser','install','--replace-executable',binary,'--installer-lock',str(elsewhere),'--installer-lock',str(root/'another-resource')],cwd=elsewhere,env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=(root/'guard.log').open('w'),text=True)
 def acknowledgement():
  with selectors.DefaultSelector() as events:
   events.register(guard.stdout,selectors.EVENT_READ)
   assert events.select(55),'installer acknowledgement timeout'
   return guard.stdout.readline().strip()
 assert acknowledgement()=='DIGCRAWL_INSTALL_READY_V2',(root/'guard.log').read_text()
 state=json.loads(run('bridge','status',check=False).stdout)
 assert not state.get('owners',[]) and not state['ready'],state
 guard.stdin.write('release\n');guard.stdin.flush()
 assert acknowledgement()=='DIGCRAWL_INSTALL_RELEASED',(root/'guard.log').read_text()
 assert guard.wait(timeout=10)==0
 guard=None
 assert plist.read_bytes()==definition,'installation changed the recorded launch context'
 run('bridge','restart');run('doctor')
 assert browser_dir()==before,'cross-directory handoff changed browser identity'
 pathlib.Path(before,'lifecycle-marker').write_text('retained profile data')
 socket.unlink();started=time.monotonic()
 while time.monotonic()-started<90:
  status=json.loads(run('bridge','status',check=False).stdout)
  if status['ready']:break
  time.sleep(.5)
 else:raise RuntimeError('watcher failed to recover')
 run('doctor')
 after=browser_dir()
 assert p.read_bytes()==original_profile,'profile changed during recovery'
 assert before==after,(before,after)
 assert pathlib.Path(after,'lifecycle-marker').read_text()=='retained profile data'
 print(json.dumps({'browser_dir_before':before,'browser_dir_after':after,'same_profile_storage':before==after,'recovery_seconds':round(time.monotonic()-started,2)}),flush=True)
finally:
 if guard:
  guard.stdin.close();guard.wait(timeout=55)
 if installed:run('bridge','watch','--uninstall',check=False)
 cleanup=run('bridge','stop',check=False)
 print('cleanup_exit',cleanup.returncode,'evidence_root',root,flush=True)
