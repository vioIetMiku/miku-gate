"""Keep the default online reinstall in the existing Docker deployment mode."""
import os
from pathlib import Path
import shlex
import sys
import common as c


def docker_project(menu=c.MENU):
    if not menu.exists() and not menu.is_symlink():return None
    raw=c.trusted(menu).decode()
    if '# Miku Gate Docker launcher' not in raw:return None
    lines=[shlex.split(line) for line in raw.splitlines() if line.startswith('exec /usr/bin/env python3 -B ')]
    c.require(len(lines)==1 and len(lines[0])==6 and lines[0][-1]=='$@','Docker 快捷命令格式异常')
    script=Path(lines[0][4]);project=script.parent.parent
    c.require(script.is_absolute() and script.resolve()==script and script.name=='docker_console.py' and script.parent.name=='scripts','Docker 快捷命令路径异常')
    c.trusted(script);c.trusted(project/'compose.yaml')
    return project


def main():
    project=docker_project()
    if project is None:return 0
    c.require(not os.environ.get('MIKU_DOCKER_INSTALL_DIR') or Path(os.environ['MIKU_DOCKER_INSTALL_DIR'])==project,'已指定的 Docker 目录与现有安装不一致')
    # stdout is consumed by the shell, so emit only the verified project path.
    print(project)
    return 0


if __name__=='__main__':
    try:sys.exit(main())
    except Exception as error:print('安装模式检查失败：'+str(error),file=sys.stderr);sys.exit(1)
