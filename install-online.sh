#!/usr/bin/env bash
# Miku Gate: download a verified stable release, then run its local installer.
set -euo pipefail
if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  printf '%s\n' 'Miku Gate VPS installer (Linux amd64/arm64, root).' \
    'Usage: bash install-online.sh [--panel | --quick | --docker] [--domain HOST | --no-domain] [--background] [--download-only DIRECTORY]' \
    'Environment: MIKU_VERSION=v0.2.5 (default: latest stable), GITHUB_TOKEN (private repository), WEB_PORT (default: 8899), MIKU_DOCKER_INSTALL_DIR (default: /opt/miku-gate-docker).'
  exit 0
fi
[[ "$(uname -s)" == Linux ]] || { echo '仅支持 Linux VPS。' >&2; exit 1; }
[[ $EUID -eq 0 ]] || { echo '请以 root 运行；私有仓库请保留 GITHUB_TOKEN 环境变量。' >&2; exit 1; }
[[ "$(uname -m)" == x86_64 || "$(uname -m)" == aarch64 || "$(uname -m)" == arm64 ]] || { echo '仅支持 amd64 / arm64。' >&2; exit 1; }
if ! command -v python3 >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then apt-get update -qq; DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends python3 ca-certificates
  elif command -v dnf >/dev/null 2>&1; then dnf install -y python3 ca-certificates
  elif command -v yum >/dev/null 2>&1; then yum install -y python3 ca-certificates
  elif command -v apk >/dev/null 2>&1; then apk add --no-cache python3 ca-certificates
  elif command -v pacman >/dev/null 2>&1; then pacman -Sy --noconfirm --needed python ca-certificates
  elif command -v zypper >/dev/null 2>&1; then zypper --non-interactive install -y python3 ca-certificates
  else echo '请先安装 Python 3 和 CA 证书。' >&2; exit 1; fi
fi
python3 - "$@" <<'MIKU_PYTHON'
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.error
import urllib.parse
import urllib.request

API = 'https://api.github.com/repos/vioIetMiku/miku-gate'
MAX_ARCHIVE = 160 << 20
MAX_UNPACKED = 320 << 20

class SafeRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, url):
        if urllib.parse.urlsplit(url).scheme != 'https':
            raise RuntimeError('拒绝非 HTTPS 重定向')
        redirected = super().redirect_request(request, response, code, message, headers, url)
        if redirected is not None and urllib.parse.urlsplit(url).netloc != urllib.parse.urlsplit(request.full_url).netloc:
            redirected.remove_header('Authorization')
        return redirected

def fetch(url, limit, binary=False):
    if urllib.parse.urlsplit(url).scheme != 'https':
        raise RuntimeError('只允许 HTTPS 下载')
    headers = {'User-Agent': 'MikuGateInstaller/0.2.4', 'Accept': 'application/octet-stream' if binary else 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28'}
    token = os.environ.get('GITHUB_TOKEN') or os.environ.get('GH_TOKEN')
    if token and urllib.parse.urlsplit(url).hostname == 'api.github.com':
        headers['Authorization'] = 'Bearer ' + token
    opener = urllib.request.build_opener(SafeRedirect())
    with opener.open(urllib.request.Request(url, headers=headers), timeout=120) as response:
        raw = response.read(limit + 1)
    if len(raw) > limit:
        raise RuntimeError('下载文件超出大小限制')
    return raw

def stable_release(version):
    if version != 'latest' and not re.fullmatch(r'v\d+\.\d+\.\d+', version):
        raise RuntimeError('MIKU_VERSION 只能是 latest 或正式版本标签，例如 v0.2.5')
    endpoint = '/releases/latest' if version == 'latest' else '/releases/tags/' + version
    release = json.loads(fetch(API + endpoint, 4 << 20))
    if release.get('draft') or release.get('prerelease') or not re.fullmatch(r'v\d+\.\d+\.\d+', release.get('tag_name', '')):
        raise RuntimeError('拒绝草稿或预发布版本')
    if version != 'latest' and release['tag_name'] != version:
        raise RuntimeError('Release 标签不匹配')
    return release

def asset(release, name):
    matches = [entry for entry in release.get('assets', []) if entry.get('name') == name and entry.get('state') == 'uploaded']
    if len(matches) != 1:
        raise RuntimeError('Release 缺少完整附件：' + name)
    url = matches[0]['url']
    if not url.startswith(API + '/releases/assets/'):
        raise RuntimeError('Release 附件地址异常')
    return url

def expected_checksum(raw, name):
    matches = []
    for line in raw.decode('ascii').splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  ([A-Za-z0-9_.-]+)', line)
        if not match:
            raise RuntimeError('SHA256SUMS 格式异常')
        if match[2] == name:
            matches.append(match[1])
    if len(matches) != 1:
        raise RuntimeError('找不到唯一的安装包 SHA256')
    return matches[0]

def unpack(package, destination, version):
    with tarfile.open(package, 'r:gz') as archive:
        entries = archive.getmembers()
        if not 5 <= len(entries) <= 100:
            raise RuntimeError('安装包成员数量异常')
        bundle_root = entries[0].name.split('/')[0]
        if bundle_root not in ('miku-gate', 'miku-fanout'):
            raise RuntimeError('安装包根目录异常')
        prefix = bundle_root + '/'
        names = set()
        total = 0
        for entry in entries:
            parts = PurePosixPath(entry.name).parts
            if not entry.isfile() or entry.name in names or not parts or parts[0] != bundle_root or len(parts) < 2 or any(not re.fullmatch(r'[A-Za-z0-9_.-]+', part) or part in ('.', '..') for part in parts) or entry.name != '/'.join(parts):
                raise RuntimeError('安装包包含重复、链接或不安全路径')
            if entry.uid != 0 or entry.gid != 0 or entry.mode not in (0o644, 0o755) or not 0 <= entry.size <= MAX_ARCHIVE:
                raise RuntimeError('安装包权限或大小异常')
            names.add(entry.name)
            total += entry.size
        if total > MAX_UNPACKED or prefix + 'BUILD.json' not in names:
            raise RuntimeError('安装包超限或没有构建清单')
        manifest_entry = archive.getmember(prefix + 'BUILD.json')
        if manifest_entry.size > 64 << 10:
            raise RuntimeError('构建清单过大')
        meta = json.loads(archive.extractfile(manifest_entry).read())
        files = meta.get('files', {})
        if meta.get('product') != 'miku-fanout' or meta.get('format') != 1 or meta.get('version') != version.removeprefix('v') or not isinstance(files, dict):
            raise RuntimeError('构建清单版本或类型不匹配')
        if names != {prefix + name for name in files} | {prefix + 'BUILD.json'}:
            raise RuntimeError('安装包内容与构建清单不一致')
        if not {'install.sh', 'scripts/install.py', 'scripts/common.py', 'scripts/maintain.py', 'scripts/miku.sh', 'bin/miku-linux-amd64', 'bin/miku-linux-arm64', 'bin/miku-export-linux-amd64', 'bin/miku-export-linux-arm64', 'CORE_ASSETS.json'} <= files.keys():
            raise RuntimeError('安装包缺少必要程序')
        for entry in entries:
            name = entry.name.removeprefix(prefix)
            raw = archive.extractfile(entry).read()
            if name != 'BUILD.json' and hashlib.sha256(raw).hexdigest() != files[name]:
                raise RuntimeError('文件 SHA256 校验失败：' + name)
            target = destination.joinpath(*PurePosixPath(entry.name).parts)
            target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            with target.open('xb') as stream:
                stream.write(raw)
            target.chmod(entry.mode)
    return destination / bundle_root

def main():
    parser = argparse.ArgumentParser(description='Miku Gate 正式版安装器')
    parser.add_argument('--download-only', type=Path, help='只下载和验证，不修改 VPS 服务')
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument('--quick', action='store_true')
    modes.add_argument('--panel', action='store_true')
    modes.add_argument('--docker', action='store_true', help='部署到固定目录并使用 Docker Compose')
    domain = parser.add_mutually_exclusive_group()
    domain.add_argument('--domain')
    domain.add_argument('--no-domain', action='store_true')
    parser.add_argument('--background', action='store_true')
    args = parser.parse_args()
    if (args.panel or args.docker) and (args.domain is not None or args.no_domain or args.background):
        parser.error('--panel / --docker 不能与节点一键配置参数同时使用；Docker 安装后再运行 quick')
    install_args = []
    for option in ('quick', 'panel', 'docker', 'no_domain', 'background'):
        if getattr(args, option):
            install_args.append('--' + option.replace('_', '-'))
    if args.domain is not None:
        install_args.extend(['--domain', args.domain])
    release = stable_release(os.environ.get('MIKU_VERSION', 'latest'))
    version = release['tag_name']
    name = 'miku-gate-vps-' + version + '.tar.gz'
    expected = expected_checksum(fetch(asset(release, 'SHA256SUMS'), 64 << 10, True), name)
    print('正在下载 Miku Gate ' + version + ' …', flush=True)
    raw = fetch(asset(release, name), MAX_ARCHIVE, True)
    if hashlib.sha256(raw).hexdigest() != expected:
        raise RuntimeError('安装包 SHA256 不匹配，停止安装')
    with tempfile.TemporaryDirectory(prefix='miku-release-') as temporary:
        root = Path(temporary)
        package = root / name
        package.write_bytes(raw)
        package.chmod(0o600)
        directory = unpack(package, root / 'verified', version)
        if args.download_only:
            args.download_only.mkdir(mode=0o700, parents=True, exist_ok=True)
            output = args.download_only / name
            with output.open('xb') as stream:
                stream.write(raw)
            output.chmod(0o600)
            print('已校验并保存：' + str(output.resolve()))
            return
        environment = {key: value for key, value in os.environ.items() if key not in ('GITHUB_TOKEN', 'GH_TOKEN')}
        # Python source is supplied by a heredoc; reconnect installer input to the TTY.
        try:
            tty = open('/dev/tty', 'rb')
        except OSError:
            tty = None
        try:
            subprocess.run(['bash', str(directory / 'install.sh'), *install_args], env=environment, stdin=tty or subprocess.DEVNULL, check=True)
        finally:
            if tty:
                tty.close()

if __name__ == '__main__':
    try:
        main()
    except urllib.error.HTTPError as error:
        print('下载失败（HTTP %s）。私有仓库需要可读取仓库 Contents 的 GITHUB_TOKEN；同时检查版本、网络及 GitHub API 额度。' % error.code, file=sys.stderr)
        sys.exit(1)
    except (Exception, KeyboardInterrupt) as error:
        print('安装未完成：' + str(error), file=sys.stderr)
        sys.exit(1)
MIKU_PYTHON
