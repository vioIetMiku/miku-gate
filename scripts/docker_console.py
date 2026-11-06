"""Persistent host shortcut for a Docker deployment; does not replace native miku."""
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import common as c

ROOT = Path(__file__).resolve().parents[1]
MARKER = '# Miku Gate Docker launcher'


def owns_shortcut(path):
    if path.is_symlink():return False
    if not path.exists() and not path.is_symlink():return False
    raw=c.trusted(path)
    return MARKER.encode() in raw and shlex.quote(str(ROOT/'scripts/docker_console.py')).encode() in raw


def register(directory=Path('/usr/local/bin')):
    c.root()
    # Keep the actual project path, never the online installer's temporary tree.
    payload = ('#!/bin/sh\n' + MARKER + '\n'
               'if [ "$(id -u)" -ne 0 ]; then exec sudo -- "$0" "$@"; fi\n'
               'exec /usr/bin/env python3 -B ' + shlex.quote(str(ROOT/'scripts/docker_console.py')) + ' "$@"\n').encode()
    directory.mkdir(parents=True, exist_ok=True)
    installed=[]
    for name in ('miku-docker', 'miku'):
        target = directory/name
        if target.exists() or target.is_symlink():
            if target.is_symlink() or not owns_shortcut(target):
                print(name + ' 已被其他安装使用，保留原命令；Docker 菜单请用 miku-docker。')
                continue
        c.atomic(target, payload, 0o755)
        installed.append(name)
    c.require('miku-docker' in installed,'miku-docker 已被其他程序使用；未注册 Docker 快捷菜单')
    print('Docker 快捷菜单：'+('miku（兼容入口：miku-docker）' if 'miku' in installed else 'miku-docker（miku 保留给原安装）'))


def uninstall(purge=False, directory=Path('/usr/local/bin')):
    """Remove only this Compose project; never prune global Docker resources."""
    c.root()
    root=ROOT
    c.require(root.is_absolute() and root.resolve()==root and len(root.parts)>=3 and not root.is_symlink(), 'Docker 安装目录异常')
    c.require(root not in (Path('/usr/local'),Path('/var/lib'),Path('/opt')), '拒绝卸载系统目录')
    c.trusted(root/'compose.yaml');c.trusted(root/'scripts/docker_console.py')
    # Validate all deletion destinations BEFORE stopping either service.
    keep={'docker-data','docker-certs','docker-backups','.env'}
    targets=[p for p in root.iterdir() if purge or p.name not in keep]
    if not purge:
        targets=[p for p in targets if p.name!='frontend']
        if (root/'frontend').exists():targets.extend(p for p in (root/'frontend').iterdir() if p.name!='.runtime')
    for path in [root,*targets]:
        c.require(not path.is_symlink() and path.resolve()==path and path.lstat().st_uid==0 and not os.path.ismount(path),'拒绝卸载归属或路径异常的文件：'+str(path))
        if path.is_dir():
            for child in path.rglob('*'):
                c.require(child.lstat().st_uid==0 and not os.path.ismount(child),'目录中包含其他归属或挂载点：'+str(child))
    shortcuts=[]
    for name in ('miku','miku-docker'):
        target=directory/name
        if target.exists() or target.is_symlink():
            if owns_shortcut(target):shortcuts.append(target)
    execute(['down'])
    # A non-forced image removal safely leaves an image used by other instances.
    subprocess.run(['docker','image','rm','miku-gate:local'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,check=False)
    for target in shortcuts:target.unlink()
    if purge:
        shutil.rmtree(root)
        print('Docker MikuGate 已彻底卸载；本实例的数据、口令、证书和备份已删除。')
    else:
        for target in targets:
            if target.is_dir():shutil.rmtree(target)
            else:target.unlink()
        print('Docker MikuGate 程序、容器和快捷命令已卸载；数据保留在 '+str(root)+'。')
        print('重新运行 Docker 一键安装命令即可恢复原节点、口令和地址。')


def confirm_uninstall(purge=False):
    print('当前实例：Docker / '+str(ROOT))
    print('将删除全部程序、容器、节点、口令、证书和本实例备份。' if purge else '将卸载程序、容器和快捷命令；保留节点、口令、证书和备份。')
    word='DELETE' if purge else 'YES'
    if input('输入 '+word+' 确认：').strip()!=word:return
    from console import EXIT_MENU
    uninstall(purge)
    return EXIT_MENU


def execute(args):
    return subprocess.run(['bash', str(ROOT/'install-docker.sh'), *args], check=True).returncode


def menu():
    from console import menu as ui
    actions = {str(i): (lambda cmd=cmd: execute([cmd])) for i,cmd in enumerate(('quick-menu','new','swap','countries','list','remove','sub','info','password'), 1)}
    actions.update({'10':lambda:execute(['status']), '11':lambda:execute(['start']),
                    '12':lambda:execute(['restart']), '13':lambda:execute(['stop']),
                    '14':lambda:execute(['logs']), '15':lambda:execute(['backup']),
                    '16':more,'17':lambda:execute(['jobs']),
                    '18':confirm_uninstall,'19':lambda:confirm_uninstall(True)})
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
    if args==['uninstall']:return confirm_uninstall()
    if args in (['purge'],['uninstall','--purge']):return confirm_uninstall(True)
    if not args or args==['menu']:return menu()
    aliases={'ip':'swap','country':'countries','links':'sub'}
    return execute([aliases.get(args[0],args[0]),*args[1:]])


if __name__=='__main__':
    try:main()
    except (Exception, KeyboardInterrupt) as error:
        print('操作未完成：'+str(error),file=sys.stderr);sys.exit(1)
