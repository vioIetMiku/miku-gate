"""Quiet operations; bounded private log, periodic progress and real exit code."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
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
    with os.fdopen(fd, 'wb', buffering=0) as log:
        process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                   start_new_session=True)
        tail=[b''];log_error=[]
        def drain():
            with process.stdout as source:
                while chunk:=source.read(65536):
                    tail[0]=(tail[0]+chunk)[-4096:]
                    if log_error:continue
                    try:
                        if log.tell()+len(chunk)>2<<20:log.seek(0);log.truncate()
                        log.write(chunk)
                    except OSError as error:log_error.append(error)
        reader=threading.Thread(target=drain,daemon=True);reader.start()
        try:
            while process.poll() is None:
                try: process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    line(f'正在进行，已用 {int(time.monotonic()-started)} 秒…')
        except KeyboardInterrupt:
            process.terminate()
            try:process.wait(timeout=10)
            except subprocess.TimeoutExpired:process.kill();process.wait()
            reader.join(timeout=10)
            line('已中止；日志：' + name, sys.stderr)
            return 130
        reader.join()
    if process.returncode:
        line(label + '失败；最近日志（最多 2 MiB）：' + name, sys.stderr)
        # Build/setup logs only; info/password commands are never passed here.
        print(tail[0].decode(errors='replace'), file=sys.stderr)
        if b'No space left on device' in tail[0] or log_error:line('请检查 df -h 和 df -i，磁盘空间或 inode 可能已满。',sys.stderr)
    else:
        Path(name).unlink(missing_ok=True)
        line(label + '完成。')
    return process.returncode


if __name__ == '__main__':
    raise SystemExit(run(sys.argv[1], sys.argv[2:]))
