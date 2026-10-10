#!/usr/bin/env python3
"""Opt-in, bounded runner for private Linux kernel and full-runtime labs; not an installer.

Run only on an explicitly authorized test host, inside a dedicated systemd
scope. This runner never changes host networking or deletes leftover resources.
Raw host snapshots stay in the root-private artifact directory; stdout contains
only their hashes and the lab's assertion output.
"""

import argparse
import datetime as dt
import hashlib
import json
import os
import posixpath
from pathlib import Path
import re
import selectors
import shutil
import signal
import stat
import subprocess
import sys
import time

PATH = "/usr/sbin:/usr/bin:/sbin:/bin"
CLEAN_ENV = {"PATH": PATH, "LANG": "C", "LC_ALL": "C"}
MAX_OUTPUT = 2 << 20
MEMORY_MAX = 384 << 20
LAB_EXEC_ROOT = "/usr/local/lib/miku-gate-lab-exec"
DIRECTORY_RE = re.compile(r"/var/tmp/miku-gate-lab-[a-z0-9-]{6,64}")
LAB_ROOTS = (
    "/run/miku-gate-netexec-tests", "/run/miku-gate-ipc-tests",
    "/run/miku-gate-ipc-http-tests", "/run/miku-gate-openvpn-tests",
    "/run/miku-gate/netexec", "/run/miku-gate-ipc", "/run/miku-gate-vpnipc",
    "/run/miku-gate-lab-exec", LAB_EXEC_ROOT,
)
SUITES = {
    "vpn-runtime": ("runtime.test", "^TestLinuxVPNRuntime", {"TestLinuxVPNRuntimeControlPlane"}),
    "netexec": ("netexec.test", "^TestLinuxLab", {
        "TestLinuxLabQuarantineAndHostIsolation", "TestLinuxLabRefusesReplacedNamespace",
        "TestLinuxLabRollsBackFailedPreparation", "TestLinuxLabRefusesActiveProcess",
        "TestLinuxLabRestartAndLocks", "TestLinuxLabRecordObjectSafety",
        "TestLinuxLabResourceBudget",
    }),
    "ipc": ("executoripc.test", "^TestLinuxIPC", {
        "TestLinuxIPCHTTPControlPlane", "TestLinuxIPCPeerCredentialsLifecycle",
        "TestLinuxIPCStaleSocketOwnership", "TestLinuxIPCClientRejectsNonRootPeer",
    }),
    "openvpn": ("openvpn.test", "^TestLinuxOpenVPN", {
        "TestLinuxOpenVPNControlledTunnelAndFailClosed",
        "TestLinuxOpenVPNLegacyControlledTunnelAndFailClosed",
        "TestLinuxOpenVPNSessionWorkerLifecycle",
    }),
}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def normalize(value):
    """Ignore route expiry and nft counter values, not policies or addresses."""
    if isinstance(value, dict):
        result = {k: normalize(v) for k, v in value.items() if k != "expires"}
        if isinstance(result.get("counter"), dict):
            result["counter"].pop("packets", None)
            result["counter"].pop("bytes", None)
        return result
    if isinstance(value, list):
        return [normalize(item) for item in value]
    return value


def read_private(path, limit=MAX_OUTPUT, executable=False):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1,
                "artifact must be a singly linked, root-owned regular file")
        require(stat.S_IMODE(info.st_mode) == (0o700 if executable else 0o600),
                "artifact is not root-private with the expected mode")
        require(info.st_size <= limit, "artifact exceeds size limit")
        content = stream.read(limit + 1)
        require(len(content) <= limit, "artifact exceeds read limit")
        return content


def write_json(path, value):
    content = canonical(value) + b"\n"
    require(len(content) <= MAX_OUTPUT, "snapshot exceeds evidence limit")
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(content)


def tool(name):
    located = shutil.which(name, path=PATH)
    require(located is not None, "required tool missing: " + name)
    resolved = Path(located).resolve(strict=True)
    for candidate in (resolved, *resolved.parents):
        info = candidate.stat()
        require(info.st_uid == 0 and info.st_mode & 0o022 == 0,
                "untrusted tool path: " + name)
    require(stat.S_ISREG(resolved.stat().st_mode), "tool is not a regular file")
    return str(resolved)


def command(name, *args):
    result = subprocess.run([tool(name), *args], cwd="/", env=CLEAN_ENV,
                            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=10, check=False)
    require(result.returncode == 0, "host observation failed: " + name)
    require(len(result.stdout) <= MAX_OUTPUT and len(result.stderr) <= 65536,
            "host observation exceeds evidence limit")
    return result.stdout.decode("utf-8", errors="strict")


def process_identity(pid):
    # starttime disambiguates PID reuse; no command line or environment is read.
    fields = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()
    return {"pid": pid, "starttime": fields[19]}


def host_snapshot():
    snapshot = {}
    for key, name, args in (
        ("links", "ip", ["-j", "link", "show"]),
        ("routes4", "ip", ["-j", "route", "show", "table", "all"]),
        ("routes6", "ip", ["-j", "-6", "route", "show", "table", "all"]),
        ("rules4", "ip", ["-j", "rule", "show"]),
        ("rules6", "ip", ["-j", "-6", "rule", "show"]),
        ("nft", "nft", ["-j", "list", "ruleset"]),
    ):
        snapshot[key] = normalize(json.loads(command(name, *args)))
    # Socket receive/send queues vary with traffic; retain protocol, state,
    # endpoints and process/fd owners, excluding only the two queue columns.
    listeners = []
    pids = set()
    for line in command("ss", "-H", "-lntup").splitlines():
        parts = line.split()
        require(len(parts) >= 6, "unexpected ss listener format")
        listeners.append(" ".join(parts[:2] + parts[4:]))
        pids.update(int(pid) for pid in re.findall(r"pid=(\d+)", line))
    snapshot["listeners"] = sorted(listeners)
    snapshot["listener_processes"] = [process_identity(pid) for pid in sorted(pids)]
    snapshot["namespaces"] = sorted(command("ip", "netns", "list").splitlines())
    for name in ("/etc/resolv.conf", "/etc/hosts"):
        data = Path(name).read_bytes()
        require(len(data) <= 65536, "DNS file exceeds observation limit")
        snapshot[name] = {"target": os.path.realpath(name), "sha256": hashlib.sha256(data).hexdigest()}
    for name in ("net/ipv4/ip_forward", "net/ipv6/conf/all/forwarding",
                 "net/ipv6/conf/all/disable_ipv6", "net/ipv6/conf/default/disable_ipv6"):
        snapshot[name] = Path("/proc/sys", name).read_text().strip()
    snapshot["mounts"] = {name: json.loads(command("findmnt", "-J", "-T", name,
                                                "-o", "TARGET,FSTYPE,OPTIONS"))
                          for name in ("/run", "/usr/local/lib")}
    return snapshot


def empty_lab_roots():
    observed = {}
    for name in LAB_ROOTS:
        path = Path(name)
        try:
            info = path.lstat()
        except FileNotFoundError:
            observed[name] = "absent"
            continue
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0,
                "unsafe lab root; retained for review: " + name)
        require(not any(path.iterdir()), "nonempty lab root; retained for review: " + name)
        observed[name] = "empty"
    return observed


def validate_limits(values):
    memory = int(values["memory.max"])
    quota, period = map(int, values["cpu.max"].split())
    tasks = int(values["pids.max"])
    require(0 < memory <= MEMORY_MAX, "missing effective 384MiB memory cap")
    require(0 < quota <= period, "missing effective single-CPU quota")
    require(0 < tasks <= 64, "missing effective 64-task cap")
    require(values["memory.swap.max"] == "0", "scope swap is not disabled")


def cgroup_scope(suite):
    lines = Path("/proc/self/cgroup").read_text().splitlines()
    paths = [line[3:] for line in lines if line.startswith("0::/")]
    require(len(paths) == 1, "unified cgroup v2 is required")
    relative = paths[0]
    require(posixpath.normpath(relative) == relative and not relative.startswith("//") and ".." not in relative,
            "noncanonical cgroup path")
    require(re.fullmatch(r"miku-vps-" + suite + r"-[a-z0-9-]+\.scope", Path(relative).name),
            "dedicated suite-specific systemd scope required")
    root = Path("/sys/fs/cgroup", relative.lstrip("/"))
    values = {name: (root / name).read_text().strip()
              for name in ("memory.max", "cpu.max", "pids.max", "memory.swap.max")}
    validate_limits(values)
    return root, values


def key_values(path):
    return {key: int(value) for key, value in
            (line.split() for line in path.read_text().splitlines())}


def passed_tests(output):
    return set(re.findall(r"^--- PASS: (TestLinux\w+) \(", output, flags=re.MULTILINE))


def run_test(binary, pattern, logfile):
    env = {**CLEAN_ENV, "MIKU_GATE_NETNS_TEST": "1", "MIKU_GATE_LAB_BUDGET": "1",
           "MIKU_GATE_OPENVPN_TEST": "1", "MIKU_GATE_VPN_RUNTIME_TEST": "1", "GOMAXPROCS": "1", "GOMEMLIMIT": "192MiB",
           "MIKU_GATE_LAB_EXEC_ROOT": LAB_EXEC_ROOT}
    argv = [str(binary), "-test.v", "-test.parallel=1", "-test.run=" + pattern, "-test.timeout=3m"]
    fd = os.open(logfile, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    output = bytearray()
    with os.fdopen(fd, "wb") as log:
        child = subprocess.Popen(argv, cwd="/", env=env, stdin=subprocess.DEVNULL,
                                 stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                 start_new_session=True)
        started = time.monotonic()
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(child.stdout, selectors.EVENT_READ)
                while selector.get_map():
                    require(time.monotonic() - started < 200, "test exceeded wall-clock limit")
                    for key, _ in selector.select(timeout=0.25):
                        chunk = os.read(key.fd, 65536)
                        if not chunk:
                            selector.unregister(key.fd)
                            continue
                        require(len(output) + len(chunk) <= MAX_OUTPUT, "test output exceeded 2MiB")
                        output.extend(chunk)
                        log.write(chunk)
                        log.flush()
                        sys.stdout.buffer.write(chunk)
                        sys.stdout.buffer.flush()
            result = child.wait(timeout=max(1, 200 - (time.monotonic() - started)))
        except BaseException:
            # Only signal the session created above. Never kill host PIDs by name.
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGTERM)
                try:
                    child.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid, signal.SIGKILL)
                    child.wait(timeout=5)
            raise
        finally:
            child.stdout.close()
    return result, time.monotonic() - started, output.decode("utf-8", errors="replace")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--suite", required=True, choices=SUITES)
    parser.add_argument("--directory", required=True)
    parser.add_argument("--sha256", required=True)
    parser.add_argument("--source", required=True, help="full Git SHA of the uploaded binary source")
    args = parser.parse_args()
    require(sys.platform == "linux" and os.geteuid() == 0, "Linux root is required")
    import resource
    require(os.environ.get("MIKU_GATE_VPS_LAB") == "1", "explicit MIKU_GATE_VPS_LAB=1 opt-in required")
    require(DIRECTORY_RE.fullmatch(args.directory), "unexpected private artifact directory")
    require(re.fullmatch(r"[0-9a-f]{64}", args.sha256), "invalid expected binary SHA256")
    require(re.fullmatch(r"[0-9a-f]{40}", args.source), "invalid source commit SHA")
    directory = Path(args.directory)
    require(directory.resolve(strict=True) == directory, "symlinked artifact directory refused")
    info = directory.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o700,
            "artifact directory must be root-owned mode 0700")
    name, pattern, expected = SUITES[args.suite]
    binary = directory / name
    actual = hashlib.sha256(read_private(binary, 32 << 20, executable=True)).hexdigest()
    require(actual == args.sha256, "binary digest mismatch")
    group, limits = cgroup_scope(args.suite)
    meminfo = dict(line.split(":", 1) for line in Path("/proc/meminfo").read_text().splitlines())
    require(int(meminfo["MemAvailable"].split()[0]) >= 448 * 1024, "host available RAM below 448MiB")
    require(shutil.disk_usage("/run").free >= 48 << 20, "/run has less than 48MiB free")
    for dependency in ("ip", "nft", "ss", "findmnt", "nsenter", "ping", "openvpn"):
        tool(dependency)
    require(not Path("/usr/sbin/policy-rc.d").exists() and not Path("/usr/sbin/policy-rc.d").is_symlink(),
            "installation guard still present; inspect before testing")
    empty_lab_roots()
    before = host_snapshot()
    require(not before["namespaces"], "this VPS recipe requires an empty namespace registry")
    baseline = directory / "host-baseline.json"
    if baseline.exists() or baseline.is_symlink():
        require(json.loads(read_private(baseline)) == before,
                "host differs from original baseline; do not continue automatically")
    else:
        write_json(baseline, before)
    now = dt.datetime.now(dt.timezone.utc)
    run_id = args.suite + "-" + now.strftime("%Y%m%dT%H%M%SZ") + "-" + str(os.getpid())
    write_json(directory / (run_id + ".before.json"), before)
    events_before = key_values(group / "memory.events")
    cpu_before = key_values(group / "cpu.stat")
    pids_before = key_values(group / "pids.events")
    print("LAB_BEGIN " + json.dumps({"run_id": run_id, "source": args.source, "binary_sha256": actual,
          "limits": limits, "host_before_sha256": digest(before)}, sort_keys=True), flush=True)
    error = None
    result, duration, output = None, None, ""
    started = time.monotonic()
    try:
        result, duration, output = run_test(binary, pattern, directory / (run_id + ".log"))
    except Exception as exc:
        error = str(exc)
        duration = time.monotonic() - started
    summary = {"schema": 1, "run_id": run_id, "source": args.source, "suite": args.suite,
               "started_at": now.isoformat(), "binary_sha256": actual,
               "guard_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
               "limits": limits, "lab_exec_root": LAB_EXEC_ROOT, "exit_code": result, "elapsed_seconds": round(duration, 3),
               "passed_tests": sorted(passed_tests(output)), "expected_tests": sorted(expected),
               "host_before_sha256": digest(before), "error": error}
    try:
        after = host_snapshot()
        write_json(directory / (run_id + ".after.json"), after)
        summary["host_after_sha256"] = digest(after)
        summary["host_unchanged"] = before == after
        summary["changed_host_sections"] = sorted(key for key in before if before[key] != after[key])

        members = (group / "cgroup.procs").read_text().split()
        summary["remaining_scope_pids"] = sorted(int(pid) for pid in members if int(pid) != os.getpid())
        events_after = key_values(group / "memory.events")
        summary["memory_events_delta"] = {key: value - events_before[key] for key, value in events_after.items()}
        summary["memory_peak_bytes"] = int((group / "memory.peak").read_text())
        summary["child_maxrss_kib"] = resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss
        summary["scope_cpu_usage_usec"] = key_values(group / "cpu.stat")["usage_usec"] - cpu_before["usage_usec"]
        summary["pids_events_delta"] = {key: value - pids_before[key]
                                        for key, value in key_values(group / "pids.events").items()}
        summary["lab_roots"] = empty_lab_roots()
        require(result == 0 and error is None, "test process failed")
        require(passed_tests(output) == expected and "--- SKIP:" not in output, "missing or skipped lab tests")
        require(before == after, "host changed; preserve evidence and inspect")
        require(not summary["remaining_scope_pids"], "scope still contains child processes")
        require(all(value == 0 for value in summary["memory_events_delta"].values()),
                "scope memory pressure or OOM event occurred")
        require(all(value == 0 for value in summary["pids_events_delta"].values()),
                "scope task-limit denial occurred")
        summary["success"] = True
    except Exception as exc:
        summary["success"] = False
        summary["verification_error"] = str(exc)
    write_json(directory / (run_id + ".json"), summary)
    print("LAB_RESULT " + json.dumps(summary, sort_keys=True), flush=True)
    return 0 if summary["success"] else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (RuntimeError, ValueError, OSError, subprocess.SubprocessError) as exc:
        print("LAB_REFUSED: " + str(exc), file=sys.stderr, flush=True)
        sys.exit(1)
