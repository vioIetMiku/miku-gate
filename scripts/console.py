"""Shared native/container terminal UI using the authenticated local API."""
import os
import re
import sys
import urllib.parse
import common as c
from quick import LocalPanel, quick, subscriptions

EXIT_MENU = object()


def clean(value):
    return re.sub(r'[\x00-\x1f\x7f-\x9f]', '', str(value))[:120]


def header(mode, status=''):
    color = sys.stdout.isatty() and not os.environ.get('NO_COLOR') and os.environ.get('TERM') != 'dumb'
    cyan, end = ('\033[38;5;43m', '\033[0m') if color else ('', '')
    if os.environ.pop('MIKU_INSTALL_BANNER_SHOWN','0')!='1':c.banner(animate=False)
    print(cyan + '╭' + '─' * 50 + '╮\n  MikuGate  ·  Terminal Manager\n╰' + '─' * 50 + '╯' + end)
    print('  模式：' + mode + ('  |  状态：' + status if status else ''))


def choose(items, title):
    if not items:
        print('暂无可选项目。'); return None
    print('\n' + title)
    for i, (_, label) in enumerate(items, 1):
        print(f'  [{i:2}] {clean(label)}')
    value = input('输入编号，0 返回：').strip()
    if value in ('', '0'): return None
    c.require(value.isascii() and value.isdigit() and 1 <= int(value) <= len(items), '请选择列表中的编号')
    return items[int(value)-1][0]


def tunnels(panel=None):
    panel = panel or LocalPanel()
    items = panel.request('GET', 'api/tunnels') or []
    print('\n槽位  国家 / 出口 IP / 状态')
    for t in items:
        node = t.get('node') or {}
        print(f"{t['slot']:4}  {clean(node.get('country') or node.get('country_code', '?'))}  {clean(t.get('exit_ip') or '等待连接')}  {clean(t.get('status', ''))}")
    if not items: print('还没有出口，请先选择国家创建，或运行一键配置。')
    return items


def provision(panel=None):
    panel = panel or LocalPanel()
    regions = panel.request('GET', 'api/regions') or []
    options = [(r, f"{r.get('name') or r.get('country') or r.get('code')} ({r.get('code')}) 可用 {r.get('available', r.get('free', r.get('total', 0)))}") for r in regions]
    region = choose(options, '选择出口国家')
    if region is None: return
    count = input('创建数量 [1]（最多 20）：').strip() or '1'
    c.require(count.isascii() and count.isdigit() and 1 <= int(count) <= 20, '数量需为 1–20')
    query = urllib.parse.urlencode({'region': region['code'], 'count': count, 'template': -1})
    job = panel.request('POST', 'api/provision?' + query)
    print('已提交后台创建任务：' + clean(job['job']) + '；菜单查看出口或任务，连接就绪后获取订阅。')


def swap(panel=None):
    panel = panel or LocalPanel()
    items = tunnels(panel)
    slot = choose([(t['slot'], '槽位 ' + str(t['slot']) + ' / ' + clean(t.get('exit_ip') or '等待连接')) for t in items], '选择要切换 IP 的出口')
    if slot is None: return
    panel.request('POST', 'api/swap?' + urllib.parse.urlencode({'slot': slot}))
    print('已提交切换请求，将尝试同国家的其他节点；连接就绪后查看新 IP。')


def remove(panel=None):
    panel = panel or LocalPanel()
    slot = choose([(t['slot'], '槽位 ' + str(t['slot']) + ' / ' + clean(t.get('exit_ip') or '等待连接')) for t in tunnels(panel)], '选择停止并删除的出口')
    if slot is not None and input('输入 YES 确认停止该出口：').strip() == 'YES':
        panel.request('POST', 'api/stop?' + urllib.parse.urlencode({'slot': slot}))
        print('该出口已停止。')


def jobs(panel=None):
    panel = panel or LocalPanel()
    for job in panel.request('GET', 'api/jobs') or []:
        print(clean(job.get('id', '')), clean(job.get('title', '')), clean(job.get('status', '')))


def node_command(command, args=()):
    if command=='new':
        c.require(not args,'用法：miku new')
        from inbound_wizard import create
        return create()
    if command=='quick-menu':
        c.require(not args,'一键配置菜单不接受额外参数')
        from inbound_wizard import quick_menu
        return quick_menu()
    if command=='node-link':
        c.require(len(args)==1,'用法：miku node-link 入口ID')
        from inbound_wizard import node_links
        return node_links(args[0])
    c.require(not args,'该命令不接受额外参数')
    return {'countries': provision, 'swap': swap, 'list': tunnels, 'jobs': jobs, 'remove': remove}[command]()


def menu(mode, actions, status=lambda: ''):
    """Return after actions until explicitly exited; tolerate EOF and failed services."""
    if not sys.stdin.isatty():
        header(mode)
        print('交互菜单请在终端运行 miku；常用命令：miku info / miku new / miku sub / miku list / miku swap / miku countries')
        return
    while True:
        try:
            try: state = status()
            except Exception: state = '待检查（可用菜单启动 / 查看日志）'
            header(mode, state)
            print('''
【出口与 IP】
  [1] 一键配置常用国家 ★  [2] 搭建新的入口节点
  [3] 切换出口 IP        [4] 选择国家创建出口
  [5] 查看出口 / IP      [6] 停止指定出口
【订阅与面板】
  [7] 查看全部订阅       [8] 面板地址 / 登录口令 ★
  [9] 修改登录口令
【服务管理】
 [10] 查看运行状态      [11] 启动服务
 [12] 重启服务          [13] 停止服务
 [14] 查看日志          [15] 数据备份
 [16] 更多维护          [17] 查看后台任务
【卸载】
 [18] 卸载程序，保留数据
 [19] 彻底卸载，删除全部数据
  [0] 退出
''')
            choice = input('请选择 [0–19]：').strip()
            if choice in ('0', 'q', 'exit'): return
            shared = {'1': lambda: node_command('quick-menu'), '2': lambda: node_command('new'),
                      '3': swap, '4': provision, '5': tunnels, '6': remove, '7': subscriptions, '17': jobs}
            action = actions.get(choice) or shared.get(choice)
            if action:
                if action() is EXIT_MENU:return
            else: print('请输入菜单中的编号。')
        except (EOFError, KeyboardInterrupt):
            print('\n已退出菜单；后台服务继续运行。'); return
        except Exception as error:
            print('操作未完成：' + clean(error))
        try: input('\n按回车返回菜单…')
        except (EOFError, KeyboardInterrupt): return
