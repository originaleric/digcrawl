"""MCP/Bridge acceptance; uses a temporary profile unless explicitly configured.

A configured existing profile requires authorization for the target URL because
Chrome may send existing domain cookies. Evidence never includes cookies.
"""
import hashlib
import http.client
import json
import os
import queue
import socket
import subprocess
import threading
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

BIN = os.environ.get("DIGCRAWL_ACCEPTANCE_BIN", "/private/tmp/digcrawl-browser-20261010/digcrawl")
PROFILE = str(Path(os.environ.get("DIGCRAWL_ACCEPTANCE_PROFILE", "/private/tmp/digcrawl-browser-20261010/skill.yaml")).resolve())
SOCKET = str(Path.home() / "Library/Caches/digcrawl/bridge" / (hashlib.sha256(PROFILE.encode()).hexdigest()[:24] + ".sock"))
REPORT = Path(os.environ.get("DIGCRAWL_ACCEPTANCE_REPORT", str(Path(__file__).parent / "mcp-browser-evidence.json")))

class LocalPage(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/html")
        self.end_headers()
        self.wfile.write(b"<html><head><title>MCP acceptance</title></head><body><article>Confirmed MCP browser body</article></body></html>")
    def log_message(self, *_):
        pass

class UnixHTTP(http.client.HTTPConnection):
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX)
        self.sock.settimeout(self.timeout)
        self.sock.connect(SOCKET)

def manifest():
    c = UnixHTTP("localhost", timeout=5)
    try:
        c.request("GET", "/manifest")
        return json.loads(c.getresponse().read())
    finally:
        c.close()

class MCP:
    def __init__(self):
        self.process = subprocess.Popen([BIN, "mcp", "--config", PROFILE, "--idle-timeout", "0"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
        self.messages = queue.Queue()
        self.notifications = []
        self.counter = 0
        def reader():
            for line in self.process.stdout:
                self.messages.put(json.loads(line))
        threading.Thread(target=reader, daemon=True).start()
        self.call("initialize", {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "acceptance", "version": "1"}})
        self.process.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
        self.process.stdin.flush()

    def call(self, method, params):
        self.counter += 1
        request_id = self.counter
        self.process.stdin.write(json.dumps({"jsonrpc": "2.0", "id": request_id, "method": method, "params": params}) + "\n")
        self.process.stdin.flush()
        while True:
            message = self.messages.get(timeout=130)
            if message.get("id") == request_id:
                assert "error" not in message, message
                return message["result"]
            self.notifications.append(message.get("method"))

    def browse(self, url):
        result = self.call("tools/call", {"name": "browser.browse", "arguments": {"url": url, "mode": "full"}, "_meta": {"progressToken": "browser-check"}})
        output = result.get("structuredContent")
        if output is None:
            output = json.loads(next(c["text"] for c in result["content"] if c["type"] == "text"))
        assert not result.get("isError"), output
        return output

    def close(self):
        self.process.stdin.close()
        self.process.wait(timeout=10)

def run():
    records = []
    server = ThreadingHTTPServer(("127.0.0.1", 0), LocalPage)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    local_url = f"http://127.0.0.1:{server.server_port}/"
    subprocess.run([BIN, "bridge", "restart", "--config", PROFILE], check=True, timeout=45)
    clients = []
    try:
        m = manifest()
        expected = "sha256:" + hashlib.sha256(Path(BIN).read_bytes()).hexdigest()
        assert m["build_id"] == expected
        records.append({"check": "running_build", "pass": True, "build_id": expected})
        clients = [MCP(), MCP()]
        for client in clients:
            output = client.browse(local_url)
            assert output["title"] == "MCP acceptance"
            assert "Confirmed MCP browser body" in output["text"]
            d = output["browser_diagnostics"]
            assert d["attempts"] == 1 and d["retry_count"] == 0
            records.append({"check": "local_mcp_read", "pass": True, "diagnostics": d})
        output = clients[0].browse("https://github.com/WyattBlue/auto-editor")
        assert "auto-editor" in output["title"].lower()
        assert len(output.get("text", "")) > 1000
        records.append({"check": "original_github_url", "pass": True, "title": output["title"], "text_characters": len(output["text"]), "diagnostics": output["browser_diagnostics"]})
        doctor = subprocess.run([BIN, "doctor", "--config", PROFILE], capture_output=True, text=True, timeout=35)
        assert doctor.returncode == 0, doctor.stdout + doctor.stderr
        assert "OK   browser operation" in doctor.stdout
        records.append({"check": "doctor_same_owner", "pass": True, "summary": doctor.stdout.splitlines()})
        diagnostic = output["browser_diagnostics"]["diagnostic_id"]
        summary = subprocess.run([BIN, "doctor", "--diagnostic-id", diagnostic], capture_output=True, text=True, check=True, timeout=10)
        entries = json.loads(summary.stdout)
        assert entries and all(entry["diagnostic_id"] == diagnostic for entry in entries)
        records.append({"check": "safe_diagnostic_summary", "pass": True, "records": len(entries)})
        assert "notifications/progress" in clients[0].notifications
        records.append({"check": "mcp_progress", "pass": True})
        for client in clients:
            client.close()
        clients = []
        subprocess.run([BIN, "bridge", "restart", "--config", PROFILE], check=True, timeout=45)
        clients = [MCP()]
        output = clients[0].browse(local_url)
        assert output["title"] == "MCP acceptance"
        records.append({"check": "explicit_restart_and_reconnect", "pass": True})
    finally:
        for client in clients:
            client.close()
        if os.environ.get("DIGCRAWL_ACCEPTANCE_KEEP_BRIDGE") != "1":
            subprocess.run([BIN, "bridge", "stop", "--config", PROFILE], check=True, timeout=40)
        server.shutdown()
        REPORT.write_text(json.dumps(records, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps({"pass": True, "checks": len(records), "evidence": str(REPORT)}, ensure_ascii=False), flush=True)

if __name__ == "__main__":
    run()
