"""Stage Docker project files outside an installer's temporary extraction tree."""
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import common as c

FILES = (
    'Dockerfile', 'compose.yaml', 'compose.frontend.yaml', '.dockerignore', '.env.example',
    'install-docker.sh', 'CORE_ASSETS.json', 'LICENSE', 'FLAG_ICONS_LICENSE',
    'VERSION', 'go.mod', 'go.sum', 'BUILD.json', 'README.md', 'CHANGELOG.md',
    'scripts/common.py', 'scripts/quick.py', 'scripts/frontend.py', 'scripts/export-frontend.py',
    'docker/container.py', 'docker/build-key.py', 'docker/entrypoint.sh', 'docker/miku.sh',
    'docs/DOCKER.md', 'docs/FRONTEND.md', 'docs/VALIDATION.md', 'bin/miku-linux-amd64', 'bin/miku-linux-arm64',
)


def stage(source, destination):
    source = Path(source).resolve(strict=True)
    destination = Path(destination)
    c.require(destination.is_absolute(), 'MIKU_DOCKER_INSTALL_DIR 必须为绝对路径')
    c.require(destination.resolve() == destination, 'Docker 安装路径不能包含符号链接或 ..')
    c.require(source != destination and source not in destination.parents and destination not in source.parents, 'Docker 安装目录必须与安装包目录分离')
    if (source / 'BUILD.json').exists():
        c.verify_distribution(source)
    c.private_dir(destination)
    names = [name for name in FILES if (source / name).is_file()]
    if (source / 'frontend').is_dir():
        names.extend(path.relative_to(source).as_posix() for path in sorted((source / 'frontend').rglob('*'))
                     if path.is_file() and '.runtime' not in path.parts)
    # Source checkouts can build in Docker; no source data or credentials are copied.
    if (source / 'src').is_dir():
        names.extend(path.relative_to(source).as_posix() for path in sorted((source / 'src').rglob('*')) if path.is_file() and '__pycache__' not in path.parts)
    for name in names:
        target = destination / name
        c.private_dir(target.parent)
        mode = 0o755 if name.startswith('bin/') or name.endswith('.sh') else 0o644
        c.atomic(target, c.trusted(source / name), mode)
    # Nginx is unprivileged and receives only these public static files.
    public = destination / 'frontend/public'
    if public.is_dir():
        public.chmod(0o755)
        for directory in public.rglob('*'):
            if directory.is_dir():
                directory.chmod(0o755)
    print('Docker 部署目录：' + str(destination), flush=True)
    return destination


if __name__ == '__main__':
    try:
        c.root()
        c.require(len(sys.argv) == 3, 'stage.py 需要安装包目录和目标目录')
        stage(*sys.argv[1:])
    except Exception as exc:
        print('Docker 安装未完成：' + str(exc), file=sys.stderr)
        sys.exit(1)
