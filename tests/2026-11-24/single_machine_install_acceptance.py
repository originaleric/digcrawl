"""macOS: python3 single_machine_install_acceptance.py NEW_CLI BROWSER_RUNTIME [OLD_CLI]"""
import pathlib,sys,tempfile,subprocess,os,hashlib,fcntl,tarfile,json,time
binary=pathlib.Path(sys.argv[1]).resolve()
browsers=pathlib.Path(sys.argv[2]).resolve()
repo=pathlib.Path(__file__).resolve().parents[2]
root=pathlib.Path(tempfile.mkdtemp(prefix='dcsm-',dir='/tmp'))
project=root/'project';project.mkdir()
installer=root/'installer';installer.mkdir()
installed=root/'bin/digcrawl';installed.parent.mkdir()
previous=pathlib.Path(sys.argv[3]).resolve() if len(sys.argv)>3 else binary
installed.write_bytes(previous.read_bytes());installed.chmod(0o755)
old_digest=hashlib.sha256(installed.read_bytes()).hexdigest()
new_digest=hashlib.sha256(binary.read_bytes()).hexdigest()
if len(sys.argv)>3: assert old_digest!=new_digest,'upgrade fixture must use a different program'
env=dict(os.environ,HOME=str(root),XDG_CONFIG_HOME=str(root/'config'),XDG_DATA_HOME=str(root/'data'),DIGCRAWL_INSTALL_DIR=str(installed.parent),DIGCRAWL_BROWSER_DIR=str(browsers))
p=root/'config/digcrawl/digcrawl-skill.yaml';p.parent.mkdir(parents=True)
p.write_text('Gateway:\n  InstanceID: cold-install\n  AllowedTools: [gateway.ping]\n  ArtifactEnabled: false\n  ArtifactDir: '+str(root/'artifacts')+'\nLocalBrowser:\n  Headless: true\n  CookieStoreDir: '+str(root/'cookies')+'\n  UserDataDir: storage/browser\nSkill:\n  ASR:\n    Enabled: false\nOCR:\n  Enabled: false\nProviders:\n  PlatformEnabled: true\n');p.chmod(0o600)
archive=root/'release.tar.gz'
with tarfile.open(archive,'w:gz') as bundle:
 bundle.add(binary,arcname='digcrawl')
 bundle.add(repo/'.agents/skills/digcrawl',arcname='skills/digcrawl')
runner=root/'install.mjs'
runner.write_text('''import fs from 'node:fs/promises';
import {createHash} from 'node:crypto';
const {install,assetName}=await import(process.env.INSTALLER_MODULE);
const archive=await fs.readFile(process.env.FIXTURE_ARCHIVE);
const hash=createHash('sha256').update(archive).digest('hex');
await install({version:'1.0.0',skipAgents:true,skipCodex:true},{env:process.env,home:process.env.HOME,cwd:process.cwd(),fetchImpl:async url=>new Response(url.endsWith('/checksums.txt')?hash+'  '+assetName('1.0.0')+String.fromCharCode(10):archive)});
''')
env.update(INSTALLER_MODULE=(repo/'npm/install.mjs').as_uri(),FIXTURE_ARCHIVE=str(archive))
def run(*cmd,check=True):
 r=subprocess.run([str(installed),*cmd,'--config',str(p)],env=env,cwd=project,capture_output=True,text=True,timeout=40)
 if check and r.returncode:raise RuntimeError((cmd,r.returncode,r.stdout,r.stderr))
 return r
watch=False;fd=None
try:
 run('bridge','restart');run('doctor')
 run('bridge','watch','--install');watch=True
 foreign=installer/'storage/browser';foreign.mkdir(parents=True)
 digest=hashlib.sha256(str(foreign.resolve()).encode()).hexdigest()[:24]
 runtime=root/'Library/Application Support/digcrawl/run'
 fd=os.open(runtime/('browser-'+digest+'.lock'),os.O_CREAT|os.O_RDWR,0o600)
 fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
 old=installed.stat().st_ino
 result=subprocess.run(['node',str(runner)],cwd=installer,env=env,capture_output=True,text=True,timeout=60)
 assert result.returncode==0,(result.stdout,result.stderr)
 assert installed.stat().st_ino!=old,'program was not published'
 assert hashlib.sha256(installed.read_bytes()).hexdigest()==new_digest,'published program differs from verified release'
 started=time.monotonic()
 while time.monotonic()-started<12:
  state=json.loads(run('bridge','status',check=False).stdout)
  if state['ready']:break
  time.sleep(.1)
 else:raise RuntimeError('installed watcher did not cold-start promptly')
 run('doctor')
 key=hashlib.sha256(str(p.resolve()).encode()).hexdigest()[:24]
 fields=(runtime/(key+'.sock.owner')).read_text().split()
 args=subprocess.check_output(['ps','-p',fields[3],'-o','command='],text=True)
 actual=args.split('--user-data-dir=',1)[1].split(' --',1)[0].strip()
 expected=str((project/'storage/browser/digcrawl-isolated').resolve())
 assert actual==expected,(actual,expected)
 assert 'UserDataDir: storage/browser' in p.read_text()
 print(json.dumps({'actual_npm_install_exit':result.returncode,'different_program_update':old_digest!=new_digest,'foreign_resource_lock_ignored':True,'recorded_browser_directory_retained':True,'cold_start_seconds_after_install':round(time.monotonic()-started,2)}),flush=True)
finally:
 if fd is not None:fcntl.flock(fd,fcntl.LOCK_UN);os.close(fd)
 if watch:
  result=run('bridge','watch','--uninstall',check=False)
  assert result.returncode==0,result.stderr
 result=run('bridge','stop',check=False)
 assert result.returncode==0,result.stderr
 print('cleanup_exit',result.returncode,'evidence_root',root,flush=True)
