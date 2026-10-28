"""Docker bootstrap and CLI. The service itself runs directly under tini."""
import fcntl
import getpass
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import secrets
import sys
import tarfile

# Also allow offline tests to import this module from the source tree.
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import common as c

SEED = Path('/opt/miku/seed')
BACKUPS = Path('/backups')
CERT_ETC = Path('/etc/letsencrypt')
CERT_LIB = Path('/var/lib/letsencrypt')


def number(name, value, low, high):
    c.require(str(value).isascii() and str(value).isdecimal(), name + ' 必须是整数')
    result = int(value)
    c.require(low <= result <= high, name + ' 超出范围')
    return result


def select_binary(directory, manifest, architecture, destination):
    c.require(architecture in ('amd64', 'arm64'), '仅支持 Linux amd64 / arm64')
    meta = json.loads(Path(manifest).read_text())
    c.require(meta.get('product') == 'miku-fanout' and meta.get('format') == 1, '安装包清单无效')
    name = 'miku-linux-' + architecture
    raw = (Path(directory) / name).read_bytes()
    expected = meta.get('files', {}).get('bin/' + name)
    c.require(raw.startswith(b'\x7fELF') and hashlib.sha256(raw).hexdigest() == expected, '主程序 SHA256 校验失败')
    target = Path(destination)
    target.parent.mkdir(parents=True, exist_ok=True)
    c.atomic(target, raw, 0o755)


def seed_cores():
    c.DATA = SEED
    c.private_dir(c.DATA)
    c.ensure_cores()


def initialize():
    """Initialize once; keep credentials, tunnel state and user settings on restart."""
    requested = os.environ.get('WEB_PORT', '').strip()
    if requested:
        requested = number('WEB_PORT', requested, 1024, 65535)
    slots = number('MIKU_MAX_SLOTS', os.environ.get('MIKU_MAX_SLOTS', '0'), 0, 2147483647)
    public_ip = os.environ.get('MIKU_PUBLIC_IP', '').strip()
    if public_ip:
        c.require(ipaddress.IPv4Address(public_ip).is_global, 'MIKU_PUBLIC_IP 需要公网 IPv4')
    c.private_dir(c.DATA)
    if (c.DATA / 'install.json').exists():
        previous = c.installed()
        c.require(previous.get('installation_mode') == 'docker', '此目录属于原生安装；请使用独立的 Docker 数据目录')
    exists = (c.DATA / 'settings.json').exists()
    value = c.settings() if exists else {'port': 8899, 'listen_addr': '', 'residential_only': False}
    preferred = requested or number('管理端口', value['port'], 1024, 65535)
    port = c.select_port(preferred, c.reserved_ports(include_common=False))
    if port != preferred:
        print('后端端口 ' + str(preferred) + ' 不可用或已预留，自动改用 ' + str(port) + '。', flush=True)
    value['port'] = port
    if public_ip:
        value['public_ip'] = public_ip
    elif not value.get('public_ip'):
        try:
            value['public_ip'] = c.public_ip()
        except (OSError, ValueError, RuntimeError):
            print('公网 IPv4 自动检测失败；可在 .env 设置 MIKU_PUBLIC_IP 后重建容器。', flush=True)
    c.private_dir(c.DATA / 'bin')
    for core in ('xray', 'sing-box'):
        target = c.DATA / 'bin' / core
        if target.exists():
            c.trusted(target, 0o755)
        else:
            c.atomic(target, c.trusted(SEED / 'bin' / core, 0o755), 0o755)
    for name, size in (('password', 16), ('basepath', 8)):
        path = c.DATA / name
        if path.exists():
            c.trusted(path, 0o600, 1024)
        else:
            c.atomic(path, (secrets.token_hex(size) + '\n').encode())
    if not (c.DATA / 'panel_mode').exists():
        c.atomic(c.DATA / 'panel_mode', b'native\n')
    if not exists or value != c.settings():
        c.write_json(c.DATA / 'settings.json', value)
    binary = c.trusted(c.LIB / 'miku', 0o755)
    version = c.run([c.LIB / 'miku', '-version']).stdout.decode().strip()
    c.write_json(c.DATA / 'install.json', {
        'product': 'miku-fanout', 'installation_mode': 'docker',
        'version': version.removeprefix('Miku Gate '),
        'binary_sha256': hashlib.sha256(binary).hexdigest(),
    })
    return [str(c.LIB / 'miku'), '-dir', str(c.DATA), '-max', str(slots)]


def serve():
    c.require(Path('/dev/net/tun').is_char_device(), '缺少 /dev/net/tun；请在宿主机启用 TUN 并使用提供的 compose.yaml')
    c.private_dir(c.DATA)
    # Refuse a second bootstrap against a running service before changing files.
    fd = os.open(c.DATA / 'lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        args = initialize()
        print('Miku Gate Docker 模式；查看地址与口令：docker compose exec miku miku info', flush=True)
        # CLOEXEC releases the same lock immediately before the service takes it.
        os.execv(args[0], args)
    finally:
        os.close(fd)


def info():
    value = c.settings()
    path = c.trusted(c.DATA / 'basepath', 0o600, 1024).decode().strip().strip('/')
    password = c.trusted(c.DATA / 'password', 0o600, 1024).decode().strip()
    print('管理地址：' + c.origin(value) + '/' + path + '/\n访问口令：' + password)
    print('一键节点：docker compose exec miku miku quick\n全部订阅：docker compose exec miku miku sub')


def password():
    from quick import LocalPanel
    new = getpass.getpass('新口令（至少 8 位）：')
    c.require(len(new) >= 8 and new == getpass.getpass('再次输入：'), '口令过短或两次不一致')
    LocalPanel().request('POST', 'api/settings', {'password': new})
    print('口令已更新。')


def snapshot(name):
    c.require(name.startswith('miku-docker-') and name.endswith('.tar.gz') and Path(name).name == name, '备份文件名无效')
    c.private_dir(BACKUPS)
    fd = os.open(c.DATA / 'lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    target = BACKUPS / name
    try:
        # A forgotten running instance must never produce an inconsistent backup.
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        with target.open('xb') as output:
            os.fchmod(output.fileno(), 0o600)
            try:
                with tarfile.open(fileobj=output, mode='w:gz') as archive:
                    for source, prefix in ((c.DATA, 'data'), (CERT_ETC, 'letsencrypt-etc'), (CERT_LIB, 'letsencrypt-lib')):
                        if source.exists():
                            archive.add(source, arcname=prefix)
            except BaseException:
                target.unlink(missing_ok=True)
                raise
        print('已生成备份：' + str(target))
    finally:
        os.close(fd)


def main(args=None):
    args = sys.argv[1:] if args is None else args
    if not args or args in (['help'], ['--help'], ['-h']):
        print('容器内：miku [info|quick [参数]|sub|password|status]\n宿主机：bash install-docker.sh [up|info|quick|sub|status|logs|start|stop|restart|backup|update|down]')
        return
    c.root()
    command = args[0]
    if command == 'select-binary':
        c.require(len(args) == 5, 'select-binary 参数无效')
        return select_binary(*args[1:])
    if command == 'seed-cores':
        c.require(len(args) == 1, 'seed-cores 无需参数')
        return seed_cores()
    if command == 'snapshot':
        c.require(len(args) == 2, 'snapshot 参数无效')
        return snapshot(args[1])
    if command == 'serve':
        c.require(len(args) == 1, 'serve 无需参数；使用 .env 配置')
        return serve()
    if command in ('healthcheck', 'status'):
        c.require(len(args) == 1, 'status 无需参数')
        c.wait_ready(seconds=3)
        if command == 'status':
            print('运行中')
        return
    if command == 'quick':
        # Compose owns the process lifecycle; quick must never invoke OpenRC.
        c.active = lambda: True
        from quick import command as quick_command
        return quick_command(args[1:])
    c.require(len(args) == 1, '命令参数无效')
    if command in ('sub', 'links'):
        from quick import subscriptions
        return subscriptions()
    if command == 'info':
        return info()
    if command in ('password', 'passwd'):
        return password()
    raise RuntimeError('此命令请使用宿主机 bash install-docker.sh；域名和证书请在网页管理')


if __name__ == '__main__':
    try:
        main()
    except (Exception, KeyboardInterrupt) as exc:
        print('操作未完成：' + str(exc), file=sys.stderr)
        sys.exit(1)
