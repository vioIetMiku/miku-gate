"""Quiet long-running operations; private full log, periodic progress, real exit code."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time


def line(message, stream=None):
    # Some package tools change the controlling terminal's ONLCR flag. Explicit
    # carriage returns keep subsequent messages at column zero on such terminals.
    stream = stream or sys.stdout
    stream.write('\r' + message + '\r\n' if stream.isatty() else message + '\n')
    stream.flush()


def run(label, command):
    if os.environ.get('MIKU_VERBOSE') == '1':
        return subprocess.call(command)
    directory = Path(tempfile.gettempdir()) / ('miku-gate-install-' + str(os.geteuid()))
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    if directory.is_symlink() or directory.stat().st_uid != os.geteuid():
        raise RuntimeError('安装日志目录归属异常')
    directory.chmod(0o700)
    fd, name = tempfile.mkstemp(prefix='install-', suffix='.log', dir=directory)
    started = time.monotonic()
    line(label + '，请稍候…')
    with os.fdopen(fd, 'wb') as log:
        process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                                   start_new_session=True)
        try:
            while process.poll() is None:
                try: process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    line(f'正在进行，已用 {int(time.monotonic()-started)} 秒…')
        except KeyboardInterrupt:
            process.terminate(); process.wait()
            line('已中止；日志：' + name, sys.stderr)
            return 130
    if process.returncode:
        line(label + '失败；完整日志：' + name, sys.stderr)
        # Build/setup logs only; info/password commands are never passed here.
        print(Path(name).read_bytes()[-4096:].decode(errors='replace'), file=sys.stderr)
    else:
        line(label + '完成。')
    return process.returncode


if __name__ == '__main__':
    raise SystemExit(run(sys.argv[1], sys.argv[2:]))
