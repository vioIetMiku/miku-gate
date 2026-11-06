"""Supply the unmodified GPL v3 text to release packages and container images."""
import hashlib
from pathlib import Path
import sys
import urllib.request

SHA256='3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986'
KNOWN_SHA256={SHA256,'8ceb4b9ee5adedde47b31e975c1d90c73ad27b6b165a1dcd80c7c545eb65b903'}
URLS=('https://raw.githubusercontent.com/gcc-mirror/gcc/master/COPYING3',
      'https://www.gnu.org/licenses/gpl-3.0.txt')


def write_gpl(destination):
    destination=Path(destination)
    if destination.is_file() and hashlib.sha256(destination.read_bytes()).hexdigest() in KNOWN_SHA256:return destination
    for url in URLS:
        for _ in range(2):
            try:
                with urllib.request.urlopen(url,timeout=10) as response:raw=response.read(65537)
                if len(raw)>65536 or hashlib.sha256(raw).hexdigest() not in KNOWN_SHA256:
                    raise ValueError('GPL v3 official text checksum mismatch')
                destination.parent.mkdir(parents=True,exist_ok=True)
                destination.write_bytes(raw);destination.chmod(0o644)
                return destination
            except (OSError,ValueError):pass
    raise RuntimeError('无法取得已校验的 GPL v3 全文；停止构建，避免分发缺失协议的安装包')


if __name__=='__main__':
    if len(sys.argv)!=2:raise SystemExit('usage: license_text.py destination')
    write_gpl(sys.argv[1])
