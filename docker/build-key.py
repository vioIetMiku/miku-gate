"""Stable local image build key; exclude credentials and mutable application data."""
import argparse
import hashlib
import json
from pathlib import Path

RUNTIME = ('Dockerfile', '.dockerignore', 'CORE_ASSETS.json', 'LICENSE', 'FLAG_ICONS_LICENSE',
           'scripts/common.py', 'scripts/quick.py', 'docker/container.py', 'docker/entrypoint.sh', 'docker/miku.sh')

def fingerprint(root, architecture, source):
    root = Path(root)
    if architecture not in ('amd64', 'arm64') or source not in ('prebuilt', 'source'):
        raise ValueError('Unsupported image architecture or source mode')
    digest = hashlib.sha256(('miku-build-v1\0' + architecture + '\0' + source + '\0').encode())
    names = list(RUNTIME)
    if source == 'source':
        names += ['go.mod', 'go.sum', 'VERSION']
        names += [path.relative_to(root).as_posix() for path in (root / 'src').rglob('*') if path.is_file()]
    else:
        name = 'bin/miku-linux-' + architecture
        raw = (root / name).read_bytes()
        meta = json.loads((root / 'BUILD.json').read_text(encoding='utf-8'))
        if meta.get('product') != 'miku-fanout' or meta.get('format') != 1 or hashlib.sha256(raw).hexdigest() != meta.get('files', {}).get(name):
            raise ValueError('Prebuilt application SHA256 mismatch')
        names += [name]
    for name in sorted(set(names)):
        raw = (root / name).read_bytes()
        digest.update(name.encode() + b'\0' + hashlib.sha256(raw).digest())
    return digest.hexdigest()

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('root', type=Path)
    parser.add_argument('architecture', choices=('amd64', 'arm64'))
    parser.add_argument('source', choices=('source', 'prebuilt'))
    args = parser.parse_args()
    print(fingerprint(args.root, args.architecture, args.source))
