"""Read-only local process/build verification; no browser or Cookie access."""
import hashlib
import http.client
import json
import socket
import subprocess
from datetime import datetime, timezone
from pathlib import Path

profile = str((Path.home() / ".config/digcrawl/digcrawl-skill.yaml").resolve())
binary = Path.home() / ".local/bin/digcrawl"
socket_path = Path.home() / "Library/Caches/digcrawl/bridge" / (hashlib.sha256(profile.encode()).hexdigest()[:24] + ".sock")
processes = subprocess.check_output(["ps", "-axo", "pid=,command="], text=True)
pids = []
for line in processes.splitlines():
    fields = line.strip().split(None, 1)
    if len(fields) == 2 and fields[1].startswith(str(binary) + " bridge ") and "--config " + profile in fields[1]:
        pids.append(int(fields[0]))

class UnixHTTP(http.client.HTTPConnection):
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX)
        self.sock.settimeout(self.timeout)
        self.sock.connect(str(socket_path))

c = UnixHTTP("localhost", timeout=5)
try:
    c.request("GET", "/manifest")
    response = c.getresponse()
    assert response.status == 200
    manifest = json.loads(response.read(65536))
finally:
    c.close()

build_id = "sha256:" + hashlib.sha256(binary.read_bytes()).hexdigest()
evidence = {"time": datetime.now(timezone.utc).isoformat(), "running_bridge_count": len(pids), "running_bridge_pids": pids, "installed_build_id": build_id, "running_build_id": manifest["build_id"], "build_matches": build_id == manifest["build_id"]}
assert evidence["running_bridge_count"] == 1 and evidence["build_matches"], evidence
Path(__file__).with_name("final-running-build-evidence.json").write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + "\n")
print(json.dumps(evidence, ensure_ascii=False))
