#!/usr/bin/env python3
"""Explicit, bounded acceptance against at most two official VPN Gate nodes.

Runs the actual CLI/default factory. Requires Linux root orchestration, ordinary
control UID and two different VPN worker UIDs inside a one-CPU/384MiB scope.
No insecure TLS switches, public proxy listeners or host route changes.
"""
import argparse
import contextlib
import hashlib
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import threading
import time

spec = importlib.util.spec_from_file_location("miku_live", Path(__file__).with_name("vps-proxy-live.py"))
live = importlib.util.module_from_spec(spec)
spec.loader.exec_module(live)

class UserControl(live.Control):
    api_timeout = 40
    def argv(self, command):
        args = super().argv(command)
        if command == "serve":
            args += ["--vpn-executor"]
        return ["/usr/bin/setpriv", "--reuid=65534", "--regid=65534", "--clear-groups", "--no-new-privs", "--", *args]

    def call(self, method, path, body=None):
        connection = live.http.client.HTTPConnection(*self.address, timeout=self.api_timeout)
        headers = {"Content-Type": "application/json", "Cookie": self.cookie}
        if method != "GET":
            headers["X-CSRF-Token"] = self.csrf
        try:
            connection.request(method, path, None if body is None else json.dumps(body), headers)
            response = connection.getresponse()
            raw = response.read(live.MAX_BODY + 1)
            live.require(len(raw) <= live.MAX_BODY, "API response bound")
            return response.status, json.loads(raw) if raw else None
        finally:
            connection.close()

class Daemon:
    def __init__(self, binary, instance):
        self.log = bytearray()
        self.lock = threading.Lock()
        self.process = subprocess.Popen([str(binary), "vpn-executor", "serve", "--instance", instance,
             "--allow-uid", "65534", "--tunnel-uid", "65533", "--worker-uid", "65532", "--apply"],
             cwd="/", env=live.ENV, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
             stderr=subprocess.STDOUT, start_new_session=True)
        self.reader = threading.Thread(target=self.read, daemon=True)
        self.reader.start()
        try:
            deadline = time.monotonic() + 8
            while time.monotonic() < deadline:
                if "隔离VPN服务已监听" in self.text():
                    return
                live.require(self.process.poll() is None, "root VPN daemon failed its preflight")
                time.sleep(0.05)
            raise RuntimeError("root daemon readiness timeout")
        except BaseException:
            with contextlib.suppress(Exception):
                self.close()
            raise

    def read(self):
        for line in self.process.stdout:
            with self.lock:
                self.log.extend(line)
                self.log = self.log[-65536:]

    def text(self):
        with self.lock:
            return bytes(self.log).decode("utf-8", errors="replace")

    def categories(self):
        return re.findall(r"VPN_START_FAILURE category=([a-z_]+)", self.text())

    def close(self):
        if self.process.poll() is None:
            self.process.terminate()
        try:
            self.process.wait(timeout=8)
        except subprocess.TimeoutExpired:
            self.process.kill()
            self.process.wait(timeout=3)
            raise RuntimeError("root VPN daemon did not exit cleanly") from None
        finally:
            self.reader.join(2)
            self.process.stdout.close()


def select_candidates(nodes):
    # This is a connection attempt budget, not a broad scanner or benchmark.
    return [node for node in nodes if re.fullmatch(r"vpn[0-9]{1,20}", node.get("hostname", ""))
            and node.get("source") == "https://www.vpngate.net/api/iphone/"][:2]


def exercise(binary, data, control_factory=UserControl, on_connected=None):
    control = control_factory(binary, data)
    daemon = None
    attempts = []
    exit_id = None
    summary = {"success": False, "public_nodes_attempted": attempts, "factory_substituted": False,
               "tls_verification_disabled": False, "public_proxy_listener": False,
               "scope": "at most two official public VPN Gate candidates; real CLI and isolated worker"}
    try:
        control.initialize()
        identity = subprocess.run(control.argv("instance"), cwd="/", env=live.ENV,
                   stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)
        live.require(identity.returncode == 0 and re.fullmatch(rb"[A-Za-z0-9_-]{22}\s*", identity.stdout), "instance identity unavailable")
        instance = identity.stdout.decode().strip()
        daemon = Daemon(binary, instance)
        control.start()
        control.request("POST", "/api/nodes/refresh", expect=200)
        # Official node lists may exceed the generic 64KiB API cap. Filter the
        # server-bounded list via a dedicated read with the known 2000-node cap.
        connection = live.http.client.HTTPConnection(*control.address, timeout=15)
        connection.request("GET", "/api/nodes", headers={"Cookie": control.cookie})
        response = connection.getresponse()
        raw = response.read((2 << 20) + 1)
        live.require(response.status == 200 and len(raw) <= 2 << 20, "node cache read failed")
        nodes = json.loads(raw)
        connection.close()
        candidates = select_candidates(nodes)
        live.require(candidates, "official cache contains no compatible naming candidates")
        for index, node in enumerate(candidates):
            if index:
                time.sleep(3)
            attempt = {"node_hostname": node["hostname"], "node_ip_sha256": hashlib.sha256(node["ip"].encode()).hexdigest()}
            attempts.append(attempt)
            prefix = "/api/nodes/" + node["id"]
            status, checked = control.call("POST", prefix + "/identity")
            attempt["identity_http"] = status
            if status != 200:
                attempt["error_code"] = (checked or {}).get("code", "identity_unavailable")
                continue
            live.require(checked["verified"] and not checked["vpn_connected"], "identity check result is not TLS-only")
            attempt.update(tls_hostname=checked["tls_host"], peer_name=checked["peer_name"], peer_fingerprint=checked["peer_fingerprint"])
            with live.socket.socket() as reservation:
                reservation.bind(("127.0.0.1", 0))
                port = reservation.getsockname()[1]
            created = control.request("POST", "/api/exits", {"name": "bounded public VPN acceptance", "country": node["country"], "policy": "any", "port": port}, expect=201)
            exit_id = created["exit"]["id"]
            exit_prefix = "/api/exits/" + exit_id
            control.request("PUT", exit_prefix + "/vpn", {"node_id": node["id"], "peer_name": "", "allow_legacy": True,
                            "use_verified_identity": True, "confirm": True}, expect=204)
            status, state = control.call("POST", exit_prefix + "/start")
            attempt["start_http"] = status
            if status != 200:
                attempt["error_code"] = (state or {}).get("code", "start_failed")
                attempt["root_failure_categories"] = daemon.categories()
                control.call("POST", exit_prefix + "/stop")
                control.request("DELETE", exit_prefix, expect=204)
                exit_id = None
                if "certificate_algorithm_rejected" in daemon.categories():
                    break
                continue
            proof = state["proof"]
            live.require(state["proxy_listening"] and state["healthy"] and proof["kind"] == "openvpn"
                         and proof["kernel_verified"] and proof["dns_verified"], "VPN start returned incomplete runtime proof")
            ip = ipaddress.ip_address(proof["exit_ip"])
            live.require(ip.version == 4 and ip.is_global, "no verified public exit IPv4")
            if on_connected is not None:
                summary["encrypted_ingress"] = on_connected(control, created, state)
            body = live.fetch_via(("127.0.0.1", port), "example.com", "/", created["exit"]["username"], created["password"])
            live.require(b"Example Domain" in body, "actual authenticated VPN business request failed")
            control.request("POST", exit_prefix + "/probe", expect=200)
            control.request("POST", exit_prefix + "/stop", expect=204)
            live.require(live.connection_refused(("127.0.0.1", port)), "public VPN entrance survived explicit stop")
            control.request("DELETE", exit_prefix, expect=204)
            exit_id = None
            attempt.update(runtime_verified=True, business_https_verified=True, stopped=True,
                           exit_ipv4_sha256=hashlib.sha256(str(ip).encode()).hexdigest())
            summary["success"] = True
            break
        summary["failure_categories"] = daemon.categories()
        return summary
    finally:
        if control.process is not None and exit_id is not None:
            with contextlib.suppress(Exception):
                control.call("POST", "/api/exits/" + exit_id + "/stop")
        try:
            control.stop()
        finally:
            if daemon is not None:
                daemon.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--data", required=True)
    parser.add_argument("--sha256", required=True)
    parser.add_argument("--allow-public-vpngate", action="store_true")
    parser.add_argument("--allow-legacy-data", action="store_true")
    args = parser.parse_args()
    live.require(sys.platform == "linux" and os.geteuid() == 0, "root coordinator on authorized Linux required")
    live.require(args.allow_public_vpngate and args.allow_legacy_data, "explicit public VPN and legacy data consent required")
    binary, data = Path(args.binary), Path(args.data)
    live.require(binary.resolve(strict=True) == binary and binary.stat().st_uid == 0 and binary.stat().st_nlink == 1
                 and stat.S_IMODE(binary.stat().st_mode) == 0o555, "unexpected staged executable")
    live.require(re.fullmatch(r"[a-f0-9]{64}", args.sha256) and hashlib.sha256(binary.read_bytes()).hexdigest() == args.sha256, "binary hash mismatch")
    live.require(data.resolve(strict=True) == data and data.stat().st_uid == 65534
                 and stat.S_IMODE(data.stat().st_mode) == 0o700 and not any(data.iterdir()), "fresh private ordinary-user data required")
    group, limits = live.effective_limits()
    memory_before, pids_before = live.counters(group / "memory.events"), live.counters(group / "pids.events")
    started = time.monotonic()
    result = exercise(binary, data)
    result.update(binary_sha256=args.sha256, elapsed_seconds=round(time.monotonic() - started, 3), limits=limits,
                  memory_peak_bytes=int((group / "memory.peak").read_text()),
                  memory_events_delta={k: v - memory_before[k] for k, v in live.counters(group / "memory.events").items()},
                  pids_events_delta={k: v - pids_before[k] for k, v in live.counters(group / "pids.events").items()})
    result["remaining_scope_pids"] = [int(pid) for pid in (group / "cgroup.procs").read_text().split() if int(pid) != os.getpid()]
    live.require(not result["remaining_scope_pids"], "owned child processes remain after test")
    live.require(not any(result["memory_events_delta"].values()) and not any(result["pids_events_delta"].values()), "resource limit pressure during test")
    print("PUBLIC_VPN_RESULT " + json.dumps(result, sort_keys=True), flush=True)
    return 0 if result["success"] else 1

if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as exc:
        print("PUBLIC_VPN_FAILED " + type(exc).__name__ + ": " + str(exc), file=sys.stderr, flush=True)
        sys.exit(1)
