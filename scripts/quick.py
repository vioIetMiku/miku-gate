"""VPS quick setup using the running service's authenticated local API."""
import argparse
import http.client
from http.cookies import SimpleCookie
import json
import os
import ssl
import sys
import time
import urllib.parse
import common as c


class LocalPanel:
    def __init__(self, timeout=12):
        self.timeout = timeout
        self.value = c.settings()
        self.path = '/' + c.trusted(c.DATA/'basepath', 0o600, 1024).decode().strip().strip('/') + '/'
        self.host = self.value.get('public_host') or self.value.get('public_ip') or '127.0.0.1'
        self.cookie = ''
        password = c.trusted(c.DATA/'password', 0o600, 1024).decode().strip()
        self.request('POST', 'login', urllib.parse.urlencode({'password': password}).encode(), 'application/x-www-form-urlencoded')

    def connection(self):
        if not self.value.get('tls_cert'):
            return http.client.HTTPConnection('127.0.0.1', self.value['port'], timeout=self.timeout)
        context = ssl.create_default_context(cafile=self.value['tls_cert'])
        class LocalTLS(http.client.HTTPSConnection):
            def connect(conn):
                import socket
                conn.sock = socket.create_connection(('127.0.0.1', conn.port), timeout=self.timeout)
                conn.sock = conn._context.wrap_socket(conn.sock, server_hostname=conn.host)
        return LocalTLS(self.host, self.value['port'], context=context, timeout=self.timeout)

    def request(self, method, path, body=None, content_type='application/json'):
        if isinstance(body, dict):
            body = json.dumps(body).encode()
        host = '[' + self.host + ']' if ':' in self.host else self.host
        headers = {'Host': host + ':' + str(self.value['port']), 'Content-Type': content_type}
        if self.cookie:
            headers['Cookie'] = self.cookie
        connection = self.connection()
        try:
            connection.request(method, self.path + path.lstrip('/'), body, headers)
            response = connection.getresponse()
            raw = response.read((8 << 20) + 1)
            c.require(len(raw) <= 8 << 20, '面板响应过大')
            c.require(response.getheader('X-Miku-Gate') == '1', '当前端口不是 Miku 服务，请运行 miku repair')
            value = json.loads(raw)
            if not 200 <= response.status < 300:
                raise RuntimeError(value.get('error', '面板请求失败：' + str(response.status)) if isinstance(value, dict) else '面板请求失败')
            if path == 'login':
                cookies = SimpleCookie(response.getheader('Set-Cookie', ''))
                c.require('miku_session' in cookies, '未收到登录会话')
                self.cookie = 'miku_session=' + cookies['miku_session'].value
            return value
        finally:
            connection.close()


def subscription_urls(panel):
    packet = panel.request('GET', 'api/sub')
    # Use the configured public origin, never the loopback request's address.
    parsed = urllib.parse.urlsplit(packet['path'])
    c.require(parsed.path == panel.path + 'sub', '订阅路径异常')
    query = urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
    formats = [('通用 Base64', ''), ('V2Ray / v2rayN', 'v2ray'), ('Clash / Mihomo', 'clash'), ('sing-box', 'singbox'), ('明文节点链接', 'links')]
    return {name: c.origin(panel.value) + parsed.path + '?' + urllib.parse.urlencode(query + ([('target', target)] if target else [])) for name, target in formats}


def subscriptions(panel=None, include_links=True):
    panel = panel or LocalPanel()
    urls = subscription_urls(panel)
    print('\n订阅地址（新增节点就绪后自动纳入，客户端更新订阅即可）：', flush=True)
    for name, address in urls.items():
        print(name + '：\n' + address, flush=True)
    if include_links:
        packet = panel.request('GET', 'api/sub/links')
        links = packet.get('links', [])
        print('\n当前可用节点链接：' + str(len(links)) + ' 条', flush=True)
        for link in links:
            print(link, flush=True)
    return urls


def choose_domain():
    if not os.isatty(0):
        raise RuntimeError('非交互安装请指定 --no-domain 或 --domain node.example.com')
    print('是否为节点绑定域名？已有证书会自动复用；暂不绑定使用 Reality。')
    answer = input('节点域名（直接回车暂不绑定）：').strip().lower().rstrip('.')
    c.require(not answer or c.valid_domain(answer), '请只填域名，例如 node.example.com')
    return answer


def quick(domain=None, background=False, panel=None):
    if domain is None:
        domain = choose_domain()
    c.require(not domain or c.valid_domain(domain), '请只填域名，例如 node.example.com')
    if not c.active():
        c.service('start')
    c.wait_ready()
    panel = panel or LocalPanel()
    result = panel.request('POST', 'api/quick-setup', {'domain': domain})
    job_id = result['job']
    if background:
        subscriptions(panel)
        return job_id
    if sys.stderr.isatty():
        print('正在准备节点；按 Ctrl+C 可立即获取已有结果，后台继续补齐。', file=sys.stderr, flush=True)
    try:
        while True:
            jobs = panel.request('GET', 'api/jobs')
            job = next((job for job in jobs if job['id'] == job_id), None)
            if job is None:
                subscriptions(panel)
                raise RuntimeError('任务已结束或服务已重启，以上为当前可用结果')
            if job['status'] != 'running':
                subscriptions(panel)
                if job['status'] == 'failed':
                    raise RuntimeError('配置未全部完成，已输出可用结果；运行 miku quick 可继续补齐')
                return job_id
            time.sleep(1.5)
    except KeyboardInterrupt:
        subscriptions(panel)
        return job_id


def stop(panel=None):
    panel = panel or LocalPanel()
    jobs = panel.request('GET', 'api/jobs')
    running = next((j for j in reversed(jobs) if j.get('kind') == 'quick-setup' and j['status'] == 'running'), None)
    if running:
        panel.request('POST', 'api/quick-setup/stop?id=' + urllib.parse.quote(running['id'], safe=''))
        print('正在停止后台补齐，已连通节点和订阅继续保留。', flush=True)
    else:
        print('当前没有正在补齐的一键任务。', flush=True)
    subscriptions(panel)


def command(args):
    parser = argparse.ArgumentParser(prog='miku quick', description='一键配置日本 5 / 韩国 3 / 美国 3，固定 Reality 并输出节点链接')
    domain = parser.add_mutually_exclusive_group()
    domain.add_argument('--domain')
    domain.add_argument('--no-domain', action='store_true')
    parser.add_argument('--background', action='store_true', help='立即输出已有节点，后台继续补齐')
    parser.add_argument('--stop', action='store_true', help='停止后台补齐，保留已有节点和订阅')
    options = parser.parse_args(args)
    if options.stop:
        c.require(options.domain is None and not options.no_domain and not options.background, '--stop 请单独使用')
        return stop()
    if options.domain is None:return terminal_quick(background=options.background)
    return quick(options.domain, options.background)


def terminal_quick(panel=None, background=False):
    """Fixed Reality family; reuse persisted nodes, export no subscription URLs."""
    if panel is None:
        if not c.active():c.service('start')
        c.wait_ready()
        panel=LocalPanel(timeout=60)
    state=panel.request('POST','api/quick-setup/terminal')
    job_id=state.get('job')
    reused=state.get('complete',False)
    print('使用已搭建的一键节点。' if reused else '正在搭建日本 5、韩国 3、美国 3 个节点：VLESS + TCP + REALITY + Vision。',flush=True)
    if not reused:print('按 Ctrl+C 可返回，后台继续补齐；再次选择此项可查看已有节点。',flush=True)
    last=None
    try:
        while not state.get('complete'):
            state=panel.request('GET','api/quick-setup/terminal')
            if state.get('complete') or background or state.get('status')!='running':break
            current=(state.get('assigned',0),state.get('ready',0))
            if current!=last:
                print(f'已搭建 {current[0]}/11，当前连通 {current[1]} 个。',flush=True);last=current
            time.sleep(1.5)
    except KeyboardInterrupt:
        state=panel.request('GET','api/quick-setup/terminal')
    links=state.get('links') or []
    c.require(len(links)<=11,'一键节点返回数量异常')
    from inbound_wizard import validate_links
    validate_links(links)
    print(f'\n一键节点链接：{len(links)}/11（当前连通 {state.get("ready",0)} 个）：',flush=True)
    for link in links:print(link,flush=True)
    if not state.get('complete'):
        print('尚未补齐；后台运行中的任务会继续尝试，再次选择此项可查看已有结果。',flush=True)
        if state.get('status')=='failed' and state.get('detail'):
            from console import clean
            print('配置未完成：'+clean(state['detail']),flush=True)
    print('需要更多节点，请去面板“出口与节点 → 新建出口”添加。',flush=True)
    return job_id
