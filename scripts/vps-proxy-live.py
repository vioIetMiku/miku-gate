#!/usr/bin/env python3
"""Explicit live acceptance of the real CLI/default proxy driver on Linux.

This is NOT an installer. An authorized root coordinator stages a root-owned
binary on an executable filesystem and runs this script as an ordinary UID in a
bounded cgroup. The relay is loopback-only, authenticated, and allows only three
fixed public DNS/TLS endpoints on port 443. No public VPN nodes are contacted.
"""

import argparse
import base64
import contextlib
import hashlib
import hmac
import http.client
import ipaddress
import json
import os
from pathlib import Path
import queue
import re
import secrets
import select
import socket
import ssl
import stat
import struct
import subprocess
import sys
import threading
import time

ALLOWED = {("1.1.1.1", 443), ("8.8.8.8", 443), ("8.8.4.4", 443)}
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8",
       "GOMAXPROCS": "1", "GOMEMLIMIT": "160MiB", "GODEBUG": "netdns=go"}
MAX_BODY = 65536


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def exact(sock, count):
    chunks = bytearray()
    while len(chunks) < count:
        part = sock.recv(count - len(chunks))
        if not part:
            raise EOFError("truncated protocol frame")
        chunks.extend(part)
    return bytes(chunks)


def head(sock, maximum=16384):
    data = bytearray()
    while len(data) < maximum:
        data += exact(sock, 1)
        if data.endswith(b"\r\n\r\n"):
            return bytes(data)
    raise ValueError("header limit exceeded")


def authorized_target(host, port):
    return (host, port) in ALLOWED


def effective_limits():
    import posixpath
    entries = [line[3:] for line in Path("/proc/self/cgroup").read_text().splitlines()
               if line.startswith("0::/")]
    require(len(entries) == 1, "cgroup v2 required")
    name = entries[0]
    require(name == posixpath.normpath(name) and ".." not in name and not name.startswith("//"),
            "invalid cgroup path")
    require(re.fullmatch(r"miku-proxy-live-[a-z0-9-]+\.scope", Path(name).name),
            "dedicated live-test scope required")
    root = Path("/sys/fs/cgroup", name.lstrip("/"))
    values = {key: (root / key).read_text().strip()
              for key in ("cpu.max", "memory.max", "memory.swap.max", "pids.max")}
    validate_limits(values)
    return root, values


def validate_limits(values):
    quota, period = map(int, values["cpu.max"].split())
    require(0 < quota <= period, "single-CPU limit missing")
    require(0 < int(values["memory.max"]) <= 384 << 20, "384MiB memory limit missing")
    require(values["memory.swap.max"] == "0", "scope swap must be disabled")
    require(0 < int(values["pids.max"]) <= 64, "task limit missing")


def counters(path):
    return {key: int(value) for key, value in (line.split() for line in path.read_text().splitlines())}


class Relay:
    """Short-lived controlled proxy, never an unrestricted or public relay."""
    def __init__(self):
        self.username = "live-" + secrets.token_hex(6)
        self.password = secrets.token_urlsafe(24)
        self.lock = threading.Lock()
        self.stopped = threading.Event()
        self.sockets = set()
        self.workers = []
        self.count = 0
        self.rejected = 0
        self.targets = set()
        self.listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.listener.bind(("127.0.0.1", 0))
        self.port = self.listener.getsockname()[1]
        self.listener.listen(12)
        self.listener.settimeout(0.25)
        self.thread = threading.Thread(target=self.accept, daemon=True)
        self.thread.start()

    def keep(self, sock):
        with self.lock:
            if self.stopped.is_set():
                sock.close()
                raise EOFError("relay stopped")
            self.sockets.add(sock)

    def drop(self, sock):
        with self.lock:
            self.sockets.discard(sock)
        with contextlib.suppress(OSError):
            sock.shutdown(socket.SHUT_RDWR)
        sock.close()

    def accept(self):
        while not self.stopped.is_set():
            try:
                client, _ = self.listener.accept()
            except socket.timeout:
                continue
            except OSError:
                break
            with self.lock:
                active = len(self.sockets)
            if active >= 24:
                client.close()
                continue
            try:
                self.keep(client)
            except EOFError:
                break
            worker = threading.Thread(target=self.handle, args=(client,), daemon=True)
            self.workers.append(worker)
            worker.start()

    def handle(self, client):
        upstream = None
        try:
            client.settimeout(8)
            kind = client.recv(1, socket.MSG_PEEK)
            if kind == b"\x05":
                greeting = exact(client, 2)
                methods = exact(client, greeting[1])
                require(2 in methods, "relay requires credentials")
                client.sendall(b"\x05\x02")
                require(exact(client, 1) == b"\x01", "invalid auth version")
                user = exact(client, exact(client, 1)[0])
                password = exact(client, exact(client, 1)[0])
                valid = hmac.compare_digest(user, self.username.encode()) and hmac.compare_digest(password, self.password.encode())
                client.sendall(b"\x01\x00" if valid else b"\x01\x01")
                require(valid, "relay authentication denied")
                request = exact(client, 4)
                # Business names must have been resolved by Miku through DoH.
                require(request == b"\x05\x01\x00\x01", "non-numeric or unsupported SOCKS request")
                target = (socket.inet_ntoa(exact(client, 4)), struct.unpack("!H", exact(client, 2))[0])
            else:
                lines = head(client).decode("ascii").split("\r\n")
                method, authority, version = lines[0].split(" ")
                require(method == "CONNECT" and version == "HTTP/1.1", "CONNECT required")
                values = [line.split(":", 1)[1].strip() for line in lines[1:]
                          if line.lower().startswith("proxy-authorization:")]
                expected = "Basic " + base64.b64encode((self.username + ":" + self.password).encode()).decode()
                require(len(values) == 1 and hmac.compare_digest(values[0], expected), "relay authentication denied")
                address, port = authority.rsplit(":", 1)
                target = (address, int(port))
            require(authorized_target(*target), "relay target denied")
            upstream = socket.create_connection(target, timeout=6)
            self.keep(upstream)
            with self.lock:
                self.count += 1
                self.targets.add(target[0])
            if kind == b"\x05":
                client.sendall(b"\x05\x00\x00\x01\x00\x00\x00\x00\x00\x00")
            else:
                client.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
            peers = [client, upstream]
            total = 0
            started = time.monotonic()
            while peers and not self.stopped.is_set() and time.monotonic() - started < 60:
                ready, _, _ = select.select(peers, [], [], 0.25)
                for source in ready:
                    dest = upstream if source is client else client
                    data = source.recv(16384)
                    if not data:
                        peers.remove(source)
                        with contextlib.suppress(OSError):
                            dest.shutdown(socket.SHUT_WR)
                        continue
                    total += len(data)
                    require(total <= 2 << 20, "relay traffic cap exceeded")
                    dest.sendall(data)
        except (OSError, EOFError, ValueError, RuntimeError):
            with self.lock:
                self.rejected += 1
        finally:
            if upstream is not None:
                self.drop(upstream)
            self.drop(client)

    def close(self):
        self.stopped.set()
        self.listener.close()
        self.thread.join(2)
        with self.lock:
            active = list(self.sockets)
        for sock in active:
            self.drop(sock)
        for worker in self.workers:
            worker.join(2)
        require(not self.thread.is_alive() and not any(t.is_alive() for t in self.workers), "relay threads did not stop")


class Control:
    def __init__(self, binary, data):
        self.binary, self.data = binary, data
        self.process = None
        self.reader = None
        self.address = None
        self.cookie, self.csrf = "", ""

    def argv(self, command):
        return [str(self.binary), command, "--data", str(self.data)]

    def initialize(self):
        result = subprocess.run(self.argv("init"), env=ENV, cwd="/", stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=12)
        require(result.returncode == 0 and len(result.stdout) < 8192, "CLI init failed")
        match = re.search(r"密码：([^\r\n]+)", result.stdout.decode("utf-8"))
        require(match is not None, "missing one-time initialization credential")
        self.admin_password = match.group(1)

    def start(self):
        require(self.process is None, "prior service must be stopped before restart")
        self.address = None
        messages = queue.Queue(maxsize=32)
        self.process = subprocess.Popen(self.argv("serve") + ["--listen", "127.0.0.1:0", "--max-active", "1"],
                                        env=ENV, cwd="/", stdin=subprocess.DEVNULL,
                                        stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
                                        start_new_session=True)
        def observe():
            for line in self.process.stderr:
                if len(line) > 8192:
                    continue
                match = re.search(rb"http://(127\.0\.0\.1):(\d+)", line)
                if match:
                    with contextlib.suppress(queue.Full):
                        messages.put_nowait((match.group(1).decode(), int(match.group(2))))
        self.reader = threading.Thread(target=observe, daemon=True)
        self.reader.start()
        try:
            self.address = messages.get(timeout=12)
        except queue.Empty:
            raise RuntimeError("CLI serve did not report its actual loopback address") from None
        self.cookie, self.csrf = "", ""
        self.request("POST", "/api/auth/login", {"username": "admin", "password": self.admin_password}, expect=200)

    def request(self, method, path, body=None, expect=200):
        connection = http.client.HTTPConnection(*self.address, timeout=getattr(self, "api_timeout", 25))
        headers = {"Content-Type": "application/json"}
        if self.cookie:
            headers["Cookie"] = self.cookie
        if method != "GET" and self.csrf:
            headers["X-CSRF-Token"] = self.csrf
        try:
            connection.request(method, path, None if body is None else json.dumps(body), headers)
            response = connection.getresponse()
            raw = response.read(MAX_BODY + 1)
            require(len(raw) <= MAX_BODY, "API response exceeded limit")
            # Never dump API response text: login and creation can contain secrets.
            require(response.status == expect, "API " + method + " " + path.rsplit("/", 1)[-1] +
                    " returned HTTP " + str(response.status) + ", expected " + str(expect))
            data = json.loads(raw) if raw else None
            if path == "/api/auth/login" and response.status == 200:
                self.cookie = response.getheader("Set-Cookie").split(";", 1)[0]
                self.csrf = data["csrf"]
            return data
        finally:
            connection.close()

    def stop(self, crash=False):
        if self.process is None:
            return
        process = self.process
        if process.poll() is None:
            if crash:
                process.kill()
            else:
                process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=3)
            raise RuntimeError("control service did not shut down in time") from None
        finally:
            self.reader.join(2)
            process.stderr.close()
            self.process = None
        require(crash or process.returncode == 0, "control service did not exit cleanly")


def socks_reply(sock):
    # The BND address is core-specific metadata, not necessarily zero IPv4.
    # Consume all valid RFC1928 reply address forms without trusting it as a
    # business destination or introducing local DNS resolution.
    reply = exact(sock, 4)
    require(reply[:3] == b"\x05\x00\x00", "frontend SOCKS connect rejected")
    if reply[3] == 1:
        count = 6
    elif reply[3] == 4:
        count = 18
    elif reply[3] == 3:
        length = exact(sock, 1)[0]
        require(length > 0, "empty SOCKS bound domain")
        count = length + 2
    else:
        raise RuntimeError("invalid SOCKS bound address type")
    exact(sock, count)


def tunnel(address, host, port, username, password, socks=False):
    sock = socket.create_connection(address, timeout=12)
    try:
        if socks:
            sock.sendall(b"\x05\x01\x02")
            require(exact(sock, 2) == b"\x05\x02", "frontend did not require SOCKS authentication")
            user, secret = username.encode(), password.encode()
            sock.sendall(bytes([1, len(user)]) + user + bytes([len(secret)]) + secret)
            require(exact(sock, 2) == b"\x01\x00", "frontend rejected the generated credential")
            name = host.encode()
            sock.sendall(b"\x05\x01\x00\x03" + bytes([len(name)]) + name + struct.pack("!H", port))
            socks_reply(sock)
        else:
            auth = base64.b64encode((username + ":" + password).encode()).decode()
            authority = host + ":" + str(port)
            sock.sendall(("CONNECT " + authority + " HTTP/1.1\r\nHost: " + authority +
                          "\r\nProxy-Authorization: Basic " + auth + "\r\n\r\n").encode())
            require(head(sock).split(b"\r\n", 1)[0] == b"HTTP/1.1 200 Connection Established", "frontend HTTP CONNECT failed")
        return sock
    except BaseException:
        sock.close()
        raise


def fetch_via(address, host, path, user, password, socks=False):
    sock = tunnel(address, host, 443, user, password, socks=socks)
    try:
        with ssl.create_default_context().wrap_socket(sock, server_hostname=host) as secured:
            secured.settimeout(12)
            secured.sendall(("GET " + path + " HTTP/1.1\r\nHost: " + host + "\r\nConnection: close\r\n\r\n").encode())
            response = http.client.HTTPResponse(secured)
            response.begin()
            data = response.read(MAX_BODY + 1)
            require(response.status == 200 and len(data) <= MAX_BODY, "TLS business request failed or exceeded bound")
            return data
    finally:
        sock.close()


def connection_refused(address):
    try:
        with socket.create_connection(address, timeout=1):
            return False
    except OSError:
        return True


def exercise(binary, data):
    control = Control(binary, data)
    relay = Relay()
    assertions = []
    try:
        control.initialize()
        control.start()
        competing = subprocess.run(control.argv("serve") + ["--listen", "127.0.0.1:0"], env=ENV, cwd="/",
                                   stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)
        require(competing.returncode != 0 and "已有控制服务" in competing.stderr.decode("utf-8"), "control-directory lock not enforced")
        identity = subprocess.run(control.argv("instance"), env=ENV, cwd="/", stdin=subprocess.DEVNULL,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)
        require(identity.returncode == 0 and re.fullmatch(rb"[A-Za-z0-9_-]{22}\s*", identity.stdout), "read-only instance failed while running")
        assertions.append("single control owner; concurrent instance is read-only")
        # Avoid collision with an unrelated existing listener; Start must still
        # recheck the port atomically rather than trust this reservation probe.
        with socket.socket() as reservation:
            reservation.bind(("127.0.0.1", 0))
            port = reservation.getsockname()[1]
        created = control.request("POST", "/api/exits", {"name": "VPS controlled proxy acceptance", "country": "JP", "policy": "any", "port": port}, expect=201)
        exit_id, user, secret = created["exit"]["id"], created["exit"]["username"], created["password"]
        prefix, address = "/api/exits/" + exit_id, ("127.0.0.1", port)
        config = {"kind": "socks5", "host": "127.0.0.1", "port": relay.port, "username": relay.username,
                  "password": relay.password, "allow_loopback": True, "dns": "cloudflare"}
        control.request("PUT", prefix + "/upstream", config, expect=204)
        view = control.request("GET", prefix + "/upstream")
        require(view["has_password"] and "password" not in view, "upstream secret leaked")
        key_hash = hashlib.sha256((data / "miku-gate.db.key").read_bytes()).hexdigest()
        state = control.request("POST", prefix + "/start")
        require(state["proxy_listening"] and state["healthy"] and state["proof"]["dns_verified"]
                and not state["proof"]["kernel_verified"], "start lacks truthful proxy-only evidence")
        trace = fetch_via(address, "1.1.1.1", "/cdn-cgi/trace", user, secret).decode("ascii")
        trace_fields = dict(line.split("=", 1) for line in trace.splitlines() if "=" in line)
        observed = ipaddress.ip_address(trace_fields.get("ip", ""))
        require(observed.version == 4 and observed.is_global, "no actual public egress IPv4 response")
        google = json.loads(fetch_via(address, "dns.google", "/resolve?name=example.com&type=A", user, secret, socks=True))
        require(google["Status"] == 0 and google.get("Answer"), "SOCKS domain business flow did not complete")
        assertions.append("default factory: real authenticated SOCKS upstream, remote DoH, HTTP CONNECT and SOCKS domain frontend, verified business TLS")
        for method, suffix, body in (("PUT", "/upstream", config), ("POST", "/credentials", None), ("DELETE", "", None)):
            control.request(method, prefix + suffix, body, expect=409)
        assertions.append("running upstream/credential/delete mutations refused")
        held = tunnel(address, "1.1.1.1", 443, user, secret)
        relay_count = relay.count
        relay.close()
        held.settimeout(3)
        with contextlib.suppress(OSError):
            require(held.recv(1) == b"", "connection survived upstream termination")
        held.close()
        with socket.create_connection(address, timeout=3) as rejected:
            auth = base64.b64encode((user + ":" + secret).encode()).decode()
            rejected.sendall(("CONNECT 1.1.1.1:443 HTTP/1.1\r\nHost: 1.1.1.1:443\r\nProxy-Authorization: Basic " + auth + "\r\n\r\n").encode())
            require(b" 502 " in head(rejected).split(b"\r\n", 1)[0], "new flow bypassed the stopped upstream")
        control.request("POST", prefix + "/probe", expect=502)
        state = control.request("GET", prefix + "/runtime")
        require(state["phase"] == "degraded" and not state["healthy"] and state["proxy_listening"], "health did not reflect upstream loss")
        control.request("POST", prefix + "/stop", expect=204)
        require(connection_refused(address), "listener survived stop")
        assertions.append("upstream loss revokes old flow and new flow; degraded state observed; stop closes listener")
        relay = Relay()
        config.update(kind="http", port=relay.port, username=relay.username, password=relay.password)
        control.request("PUT", prefix + "/upstream", config, expect=204)
        state = control.request("POST", prefix + "/start")
        require(state["healthy"], "HTTP upstream did not start")
        require(b"ip=" in fetch_via(address, "1.1.1.1", "/cdn-cgi/trace", user, secret), "HTTP upstream business transfer failed")
        assertions.append("same logical exit and credentials work via authenticated HTTP CONNECT upstream")
        control.stop(crash=True)
        require(connection_refused(address), "listener survived killed control process")
        control.start()
        overview = control.request("GET", "/api/overview")
        require(overview["active_count"] == 0, "restart invented an active listener")
        require(hashlib.sha256((data / "miku-gate.db.key").read_bytes()).hexdigest() == key_hash, "restart changed encryption key")
        view = control.request("GET", prefix + "/upstream")
        require(view["has_password"] and "password" not in view, "encrypted config not recovered privately")
        control.request("POST", prefix + "/start")
        require(b"ip=" in fetch_via(address, "1.1.1.1", "/cdn-cgi/trace", user, secret), "recovered encrypted config could not connect")
        control.request("POST", prefix + "/stop", expect=204)
        require(connection_refused(address), "restarted listener survived stop")
        control.stop()
        assertions.append("process crash closes listener; restart retains key/config, reports zero active, and explicit restart transfers business traffic")
        return {"assertions": assertions, "public_ipv4_observed": True,
                "egress_ipv4_sha256": hashlib.sha256(str(observed).encode()).hexdigest(),
                "first_relay_connections": relay_count, "second_relay_connections": relay.count,
                "allowed_destinations": sorted(ip + ":443" for ip, _ in ALLOWED),
                "public_proxy_listener": False, "ordinary_uid": os.geteuid(),
                "factory_substituted": False, "tls_verification_disabled": False,
                "scope": "controlled loopback upstream uses VPS egress, not a VPN or a second remote exit"}
    finally:
        try:
            control.stop()
        finally:
            relay.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--data", required=True)
    parser.add_argument("--sha256", required=True)
    parser.add_argument("--allow-network", action="store_true")
    args = parser.parse_args()
    require(sys.platform == "linux" and os.geteuid() not in (0,), "must run as an ordinary Linux user")
    require(args.allow_network, "explicit outbound-network consent flag required")
    binary, data = Path(args.binary), Path(args.data)
    require(binary.is_absolute() and binary.resolve(strict=True) == binary, "binary path must be canonical")
    info = binary.stat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1 and stat.S_IMODE(info.st_mode) == 0o555, "binary must be root-owned read-only executable")
    require(re.fullmatch(r"[a-f0-9]{64}", args.sha256) and hashlib.sha256(binary.read_bytes()).hexdigest() == args.sha256, "binary SHA256 mismatch")
    require(data.is_absolute() and data.resolve(strict=True) == data and data.stat().st_uid == os.geteuid()
            and stat.S_IMODE(data.stat().st_mode) == 0o700 and not any(data.iterdir()), "data must be a fresh private user-owned directory")
    group, limits = effective_limits()
    before_memory, before_tasks = counters(group / "memory.events"), counters(group / "pids.events")
    started = time.monotonic()
    result = exercise(binary, data)
    memory, tasks = counters(group / "memory.events"), counters(group / "pids.events")
    result.update(memory_events_delta={k: v - before_memory[k] for k, v in memory.items()},
                  pids_events_delta={k: v - before_tasks[k] for k, v in tasks.items()},
                  limits=limits, memory_peak_bytes=int((group / "memory.peak").read_text()),
                  elapsed_seconds=round(time.monotonic() - started, 3), binary_sha256=args.sha256)
    require(not any(result["memory_events_delta"].values()) and not any(result["pids_events_delta"].values()), "resource limit pressure or OOM during acceptance")
    require((group / "cgroup.procs").read_text().split() == [str(os.getpid())], "test scope still contains child processes")
    result["success"] = True
    print("LIVE_PROXY_RESULT " + json.dumps(result, sort_keys=True), flush=True)


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        # Never print captured CLI output, HTTP response bodies or passwords.
        print("LIVE_PROXY_FAILED " + type(exc).__name__ + ": " + str(exc), file=sys.stderr, flush=True)
        sys.exit(1)
