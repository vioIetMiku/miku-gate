"""Persistent host shortcut for a Docker deployment; does not replace native miku."""
import os
from pathlib import Path
import shlex
import subprocess
import sys
import common as c

ROOT = Path(__file__).resolve().parents[1]
MARKER = '# Miku Gate Docker launcher'


def register(directory=Path('/usr/local/bin')):
    c.root()
    # Keep the actual project path, never the online installer's temporary tree.
    payload = ('#!/bin/sh\n' + MARKER + '\n'
               'if [ "$(id -u)" -ne 0 ]; then exec sudo -- "$0" "$@"; fi\n'
               'exec /usr/bin/env python3 -B ' + shlex.quote(str(ROOT/'scripts/docker_console.py')) + ' "$@"\n').encode()
    directory.mkdir(parents=True, exist_ok=True)
    for name in ('miku-docker', 'miku'):
        target = directory/name
        if target.exists() or target.is_symlink():
            if target.is_symlink() or MARKER.encode() not in c.trusted(target):
                print(name + ' 已被其他安装使用，保留原命令；Docker 菜单请用 miku-docker。')
                continue
        c.atomic(target, payload, 0o755)
    print('Docker 快捷菜单：miku（兼容入口：miku-docker）')


def execute(args):
    return subprocess.run(['bash', str(ROOT/'install-docker.sh'), *args], check=True).returncode


def menu():
    from console import menu as ui
    actions = {str(i): (lambda cmd=cmd: execute([cmd])) for i,cmd in enumerate(('list','countries','swap','quick','remove','jobs','sub','info','password'), 1)}
    actions.update({'10':lambda:execute(['status']), '11':lambda:execute(['start']),
                    '12':lambda:execute(['restart']), '13':lambda:execute(['stop']),
                    '14':lambda:execute(['logs']), '15':lambda:execute(['backup']),
                    '16':more})
    version=(ROOT/'VERSION').read_text().strip()
    ui('Docker / v'+version, actions, status)


def status():
    result=subprocess.run(['docker','compose','--project-directory',str(ROOT),'-f',str(ROOT/'compose.yaml'),
                           'ps','--status','running','-q','miku'],capture_output=True,timeout=8)
    return '运行中' if result.returncode==0 and result.stdout.strip() else '未运行'


def more():
    print('\n1 前端重启  2 前端地址  3 前端日志  4 后端信息  0 返回')
    choice=input('选择：').strip()
    commands={'1':['frontend','restart'],'2':['frontend','info'],'3':['frontend','logs'],'4':['backend','info']}
    if choice in commands:execute(commands[choice])


def main(args=None):
    args=sys.argv[1:] if args is None else args
    c.root()
    if args==['register']:return register()
    if not args or args==['menu']:return menu()
    aliases={'ip':'swap','country':'countries','links':'sub'}
    return execute([aliases.get(args[0],args[0]),*args[1:]])


if __name__=='__main__':
    try:main()
    except (Exception, KeyboardInterrupt) as error:
        print('操作未完成：'+str(error),file=sys.stderr);sys.exit(1)
