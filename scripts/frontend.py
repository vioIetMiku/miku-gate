#!/usr/bin/env python3
"""Manage the static frontend independently from the Go backend and VPN nodes."""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from urllib.parse import urlsplit

import common as c

ROOT = Path(__file__).resolve().parents[1]


def require(condition, message):
    if not condition:
        raise ValueError(message)


def origin(raw, name):
    value = urlsplit(raw)
    require(value.scheme in ('http', 'https') and value.hostname and
            value.path in ('', '/') and not value.query and not value.fragment and
            not value.username and not value.password, name + ' 必须是 http(s)://主机[:端口]')
    require(re.fullmatch(r'[A-Za-z0-9.\[\]:-]+', value.netloc) is not None,
            name + ' 含不支持的字符')
    require(value.port is None or 1 <= value.port <= 65535, name + ' 端口无效')
    return value


def backend_settings(root):
    path = root / 'docker-data/settings.json'
    return json.loads(path.read_text()) if path.is_file() else {}


def port_number(value):
    text = str(value)
    require(text.isascii() and text.isdecimal() and 1024 <= int(text) <= 65535,
            '前端端口需为 1024–65535 的整数')
    return int(text)


def configuration(root, env):
    data = root / 'docker-data'
    settings = backend_settings(root)
    port = port_number(env.get('MIKU_FRONTEND_PORT') or '8900')
    listen = env.get('MIKU_FRONTEND_LISTEN') or '0.0.0.0'
    ipaddress.IPv4Address(listen)
    explicit_backend = env.get('MIKU_BACKEND_URL', '').strip()
    require(explicit_backend or settings, '请先部署后端；独立前端需设置 MIKU_BACKEND_URL 和 MIKU_BACKEND_BASE_PATH')
    backend_port = int(settings.get('port') or 8899)
    backend_url = explicit_backend or ('https' if settings.get('tls_cert') else 'http') + f'://127.0.0.1:{backend_port}'
    backend = origin(backend_url, 'MIKU_BACKEND_URL')
    # Even an explicitly configured loopback backend must not proxy to itself.
    require(port != (backend.port or (443 if backend.scheme == 'https' else 80)),
            '前端端口不能与后端端口相同')
    if explicit_backend:
        require(env.get('MIKU_BACKEND_BASE_PATH', '') != '',
                '独立后端请设置 MIKU_BACKEND_BASE_PATH，根路径填 /')
        base = env['MIKU_BACKEND_BASE_PATH'].strip('/')
    else:
        base = (data / 'basepath').read_text().strip().strip('/')
    require(re.fullmatch(r'[A-Za-z0-9_-]*', base) is not None, '后端访问路径无效')
    base = '/' + base if base else ''
    configured_host = env.get('MIKU_BACKEND_HOST', '').strip()
    if not configured_host and not explicit_backend and settings.get('public_host'):
        configured_host = settings['public_host'] + ':' + str(backend_port)
    backend_host = configured_host or backend.netloc
    host = origin(backend.scheme + '://' + backend_host, 'MIKU_BACKEND_HOST')
    public = env.get('MIKU_FRONTEND_ORIGIN', '').strip()
    if not public:
        require(not explicit_backend, '独立前端请设置 MIKU_FRONTEND_ORIGIN 为浏览器访问地址')
        public = f'http://{settings.get("public_ip") or "127.0.0.1"}:{port}'
    external = origin(public, 'MIKU_FRONTEND_ORIGIN')
    public = external.scheme + '://' + external.netloc
    replacements = {
        'PORT': str(port), 'LISTEN': listen, 'BASE': base,
        'BACKEND_URL': backend.scheme + '://' + backend.netloc,
        'BACKEND_HOST': backend_host,
        'BACKEND_ORIGIN': backend.scheme + '://' + backend_host,
        'BACKEND_SCHEME': backend.scheme,
        'TLS_NAME': host.hostname,
        'FRONTEND_ORIGIN': public,
        'COOKIE_FLAG': 'secure' if external.scheme == 'https' else 'nosecure',
        'BASE_REDIRECT': ('location = ' + base + ' { return 307 ' + base + '/; }') if base else '',
    }
    template = (root / 'frontend/nginx.conf.template').read_text()
    result = re.sub(r'@@([A-Z_]+)@@', lambda m: replacements[m[1]], template)
    require('@@' not in result, '前端配置模板未完整展开')
    return result, public + base + '/'


def verify_assets(root):
    public = root / 'frontend/public'
    files = json.loads((root / 'frontend/ASSETS.json').read_text())
    require('index.html' in files and 'login.html' in files, '前端资源不完整')
    actual = set()
    for path in public.rglob('*'):
        require(not path.is_symlink(), '前端资源不能包含符号链接')
        if path.is_file():
            actual.add(path.relative_to(public).as_posix())
    require(actual == set(files), '前端资源清单与文件不一致；请刷新 ASSETS.json')
    for name, digest in files.items():
        path = (public / name).resolve()
        require(path.is_relative_to(public.resolve()), '前端资源路径无效')
        require(hashlib.sha256(path.read_bytes()).hexdigest() == digest, '前端资源校验失败：' + name)


def atomic_write(path, raw, mode=0o644):
    require(not path.is_symlink(), '运行配置不能是符号链接：' + str(path))
    fd, temporary = tempfile.mkstemp(prefix='.miku-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as stream:
            os.fchmod(stream.fileno(), mode)
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def read_state(root):
    path = root / 'frontend/.runtime/state.json'
    if not path.exists():
        return {}
    value = json.loads(path.read_text())
    require(value.get('format') == 1 and value.get('address'), '前端运行记录无效，请检查 ' + str(path))
    port_number(value.get('port'))
    return value


def running_port(compose, state):
    """A saved port is reusable while busy only if Docker reports it healthy."""
    ids = subprocess.run(compose + ['ps', '--status', 'running', '--quiet', 'frontend'],
                         check=True, capture_output=True, text=True, timeout=30).stdout.split()
    if not ids:
        return None
    result = subprocess.run(['docker', 'inspect', *ids], check=True,
                            capture_output=True, text=True, timeout=30)
    for container in json.loads(result.stdout):
        status = container.get('State', {})
        env = dict(item.split('=', 1) for item in container.get('Config', {}).get('Env', []) if '=' in item)
        port = env.get('MIKU_FRONTEND_PORT', '')
        if status.get('Running') and status.get('Health', {}).get('Status') == 'healthy' and port.isdecimal():
            if int(port) == state.get('port'):
                return int(port)
    return None


def select_frontend_port(root, env, state, owned=None):
    preferred = port_number(env.get('MIKU_FRONTEND_PORT') or state.get('port') or 8900)
    reserved = c.reserved_ports(root / 'docker-data', include_common=False)
    settings = backend_settings(root)
    if settings.get('port'):
        reserved.add(int(settings['port']))
    explicit = env.get('MIKU_BACKEND_URL', '').strip()
    if explicit:
        backend = origin(explicit, 'MIKU_BACKEND_URL')
        reserved.add(backend.port or (443 if backend.scheme == 'https' else 80))
    chosen = c.select_port(preferred, reserved, owned)
    if chosen != preferred:
        print(f'前端端口 {preferred} 不可用或已预留，自动改用 {chosen}。', flush=True)
    return chosen


def show_info(state):
    require(state, '前端尚未启动，请先运行 frontend start')
    print('独立前端地址：' + state['address'])
    print('实际监听端口：' + str(state['port']) + '；访问口令与后端相同。')


def main(args=None, root=ROOT):
    import fcntl  # Deployment locking is Linux-only; asset validation remains portable.
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', nargs='?', default='start',
                        choices=('start', 'restart', 'stop', 'down', 'status', 'logs', 'info', 'check'))
    action = parser.parse_args(args).action
    compose = ['docker', 'compose', '--project-directory', str(root), '-f', str(root / 'compose.frontend.yaml')]
    if action in ('stop', 'down', 'status', 'logs'):
        command = {'stop': ['stop'], 'down': ['down'], 'status': ['ps'], 'logs': ['logs', '--tail=100', '-f']}[action]
        subprocess.run(compose + command, check=True)
        return
    state = read_state(root)
    if action == 'info':
        show_info(state)
        return
    config = subprocess.run(compose + ['config', '--format', 'json'], check=True,
                            capture_output=True, text=True, timeout=30)
    env = json.loads(config.stdout)['services']['frontend'].get('environment', {})
    # Invalid explicit ports fail before any runtime files are changed.
    port_number(env.get('MIKU_FRONTEND_PORT') or state.get('port') or 8900)
    verify_assets(root)
    runtime = root / 'frontend/.runtime'
    runtime.mkdir(exist_ok=True, mode=0o755)
    require(not runtime.is_symlink(), '运行目录不能是符号链接')
    os.chmod(runtime, 0o755)
    with (runtime / 'lock').open('a') as lock:
        fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        # Files are mounted read-only into an unprivileged Nginx process. tar's
        # normal mode preservation is insufficient when installed under umask 077.
        public=root/'frontend/public'
        public.chmod(0o755)
        for item in public.rglob('*'):
            item.chmod(0o755 if item.is_dir() else 0o644)
        state = read_state(root)
        owned = running_port(compose, state)
        port = select_frontend_port(root, env, state, owned)
        selected = {**env, 'MIKU_FRONTEND_PORT': str(port)}
        text, address = configuration(root, selected)
        process_env = {**os.environ, 'MIKU_FRONTEND_PORT': str(port)}
        fd, candidate = tempfile.mkstemp(prefix='candidate-', suffix='.conf', dir=runtime)
        os.close(fd)
        candidate = Path(candidate)
        try:
            atomic_write(candidate, text.encode())
            # Validate a separate config; the running frontend's file stays intact.
            subprocess.run(compose + ['run', '--rm', '--no-deps', 'frontend',
                                      '-c', '/etc/miku/' + candidate.name, '-t'],
                           check=True, env=process_env, timeout=90)
        finally:
            candidate.unlink(missing_ok=True)
        if action == 'check':
            print(f'前端配置与资源校验通过；试选端口 {port}，启动时会重新确认。')
            return
        current = runtime / 'nginx.conf'
        previous = current.read_bytes() if current.exists() else None
        changed = previous != text.encode() or owned != port
        if changed or action == 'restart':
            atomic_write(current, text.encode())
            try:
                subprocess.run(compose + ['up', '-d', '--force-recreate', '--wait', '--wait-timeout', '60'],
                               check=True, env=process_env, timeout=90)
            except (OSError, subprocess.SubprocessError):
                if previous is None:
                    current.unlink(missing_ok=True)
                else:
                    atomic_write(current, previous)
                if previous is not None and owned and state:
                    try:
                        subprocess.run(compose + ['up', '-d', '--force-recreate', '--wait', '--wait-timeout', '60'],
                                       check=True, env={**os.environ, 'MIKU_FRONTEND_PORT': str(state['port'])}, timeout=90)
                    except (OSError, subprocess.SubprocessError) as rollback:
                        print('已恢复旧配置，但前端恢复启动失败：' + str(rollback), file=sys.stderr)
                raise
        state = {'format': 1, 'port': port, 'address': address,
                 'config_sha256': hashlib.sha256(text.encode()).hexdigest()}
        atomic_write(runtime / 'state.json', (json.dumps(state, ensure_ascii=False, indent=2) + '\n').encode(), 0o600)
    show_info(state)
    print('前端配置已生效；后端和节点无需重启。')
    if port != 8900 and env.get('MIKU_FRONTEND_ORIGIN'):
        print('已配置固定公网 Origin；如使用外层反向代理，请确认其上游指向实际监听端口。')


if __name__ == '__main__':
    try:
        main()
    except (OSError, RuntimeError, ValueError, KeyError, subprocess.SubprocessError) as exc:
        print('前端操作未完成：' + str(exc), file=sys.stderr)
        sys.exit(1)
