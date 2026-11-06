"""Create one ingress with an explicit core/protocol and export only its links."""
import re
import urllib.parse
import common as c
from console import clean
from quick import LocalPanel


PROTOCOLS = (
    ('VLESS + TCP + REALITY + Vision（默认 / 推荐）', 'vless', 'tcp', 'reality', True),
    ('VLESS + TCP + TLS + Vision', 'vless', 'tcp', 'tls', True),
    ('VLESS + WebSocket + TLS', 'vless', 'ws', 'tls', False),
    ('VMess + WebSocket + TLS', 'vmess', 'ws', 'tls', False),
    ('Trojan + TCP + TLS', 'trojan', 'tcp', 'tls', False),
    ('VLESS + gRPC + TLS', 'vless', 'grpc', 'tls', False),
    ('VLESS + HTTPUpgrade + TLS', 'vless', 'httpupgrade', 'tls', False),
)
XRAY_PROTOCOLS = (
    ('VLESS + XHTTP + REALITY（Xray）', 'vless', 'xhttp', 'reality', False),
    ('VLESS + XHTTP + TLS（Xray）', 'vless', 'xhttp', 'tls', False),
)
SINGBOX_PROTOCOLS = (
    ('VLESS + WebSocket + REALITY（sing-box）', 'vless', 'ws', 'reality', False),
    ('VMess + WebSocket + REALITY（sing-box）', 'vmess', 'ws', 'reality', False),
    ('Trojan + TCP + REALITY（sing-box）', 'trojan', 'tcp', 'reality', False),
)


def protocols_for(core):
    if core=='sing-box':return PROTOCOLS[:2]+SINGBOX_PROTOCOLS+PROTOCOLS[5:]+PROTOCOLS[2:5]
    return PROTOCOLS+XRAY_PROTOCOLS
REALITY_DOMAINS = (
    ('www.amd.com', '三网推荐 / 默认'),
    ('www.tesla.com', '香港 VPS 推荐'),
    ('www.microsoft.com', '备选'),
    ('www.apple.com', '备选'),
)


def pick(items, title, default=1):
    print('\n' + title)
    for i, (_, label) in enumerate(items, 1):print(f'  [{i}] {clean(label)}')
    answer=input(f'选择 [{default}]，0 返回：').strip() or str(default)
    if answer=='0':return None
    c.require(answer.isascii() and answer.isdigit() and 1<=int(answer)<=len(items),'请选择列表中的编号')
    return items[int(answer)-1][0]


def reality_domain():
    items=[(host,host+' · '+label) for host,label in REALITY_DOMAINS]+[('custom','自定义域名')]
    host=pick(items,'REALITY 伪装域名（实际连通性由后端 TLS 握手检查）')
    if host=='custom':host=input('域名（不带协议、端口或路径）：').strip().lower().rstrip('.')
    if host is not None:c.require(c.valid_domain(host),'请输入有效域名')
    return host


def tls_certificate(panel):
    catalog=panel.request('GET','api/domains')
    choices=[({'certificate_mode':'auto','certificate_id':cert['id'],'sni':domain},domain+' · '+cert.get('issuer',''))
             for cert in (catalog.get('certificates') or []) if not cert.get('self_signed')
             for domain in (cert.get('domains') or []) if c.valid_domain(domain)]
    if not choices:
        print('此协议需要有效 TLS 证书。请先在面板“域名 / 证书”添加证书，或返回选择默认 REALITY（无需证书）。')
        return None
    return pick(choices,'选择已有 TLS 域名证书')


def validate_links(links):
    for link in links:
        c.require(isinstance(link,str) and len(link)<=16384 and not re.search(r'[\x00-\x20\x7f-\x9f]',link)
                  and link.startswith(('vless://','vmess://','trojan://')),'后端返回的节点链接格式异常')


def node_links(inbound_id, panel=None):
    text=str(inbound_id)
    c.require(text.isascii() and text.isdigit() and int(text)>0,'用法：miku node-link 正整数ID')
    panel=panel or LocalPanel()
    packet=panel.request('GET','api/xui/links?'+urllib.parse.urlencode({'ids':text}))
    links=packet.get('links') or []
    c.require(links,'此入口尚无启用的客户端链接，请在面板查看该入口')
    validate_links(links)
    print('\n这个节点的单独链接（包含凭据，请自行保管）：')
    for link in links:print(link)
    return links


def choose_core(panel):
    state=panel.request('GET','api/panel/mode')
    core=pick([('sing-box','sing-box ★ 推荐'),('native','Xray')],'选择节点核心（自建节点共用）')
    if core is None:return None
    available={entry['mode']:entry for entry in (state.get('modes') or [])}
    c.require(core in available and available[core].get('available'),available.get(core,{}).get('reason') or '此核心不可用，请先运行 miku repair 准备核心')
    return state,core


def apply_core(panel,state,core):
    if state.get('kind')==core:return True
    existing=panel.request('GET','api/xui/inbounds') or []
    if existing:
        print('切换共用核心会重启现有自建节点；已有不兼容配置会由后端拒绝。')
        if input('输入 YES 切换并继续，其他输入返回：').strip()!='YES':return False
    panel.request('POST','api/panel/mode',{'mode':core})
    return True


def quick_menu(panel=None):
    from quick import terminal_quick
    return terminal_quick(panel=panel)


def create(panel=None):
    panel=panel or LocalPanel(timeout=60)
    choice=choose_core(panel)
    if choice is None:return
    state,core=choice
    protocols=protocols_for(core)
    selected=pick([(p,p[0]) for p in protocols],'选择节点协议')
    if selected is None:return
    label,protocol,network,security,vision=selected
    spec={'protocol':protocol,'network':network,'security':security,'vision':'1' if vision else '0'}
    if security=='reality':
        host=reality_domain()
        if host is None:return
        spec.update(dest=host+':443',server_names=host,fp='chrome')
    else:
        certificate=tls_certificate(panel)
        if certificate is None:return
        spec.update(certificate)
    port=input('入口监听端口 [随机空闲端口]：').strip()
    c.require(not port or port.isascii() and port.isdigit() and 1024<=int(port)<=65535,'端口需为 1024–65535')
    if port:spec['port']=port
    remark=input('节点备注 [Miku 新入口]：').strip() or 'Miku 新入口'
    c.require(len(remark)<=80 and not re.search(r'[\x00-\x1f\x7f-\x9f]',remark),'备注需不超过80字，且不含控制字符')
    spec['remark']=remark
    if not apply_core(panel,state,core):return
    print('正在创建 '+label+'，请稍候…',flush=True)
    created=panel.request('POST','api/panel/inbound/new?'+urllib.parse.urlencode(spec))
    inbound_id=created['id']
    print('入口已创建：ID '+str(inbound_id)+' / 端口 '+str(created['port']))
    try:return node_links(inbound_id,panel)
    except Exception as error:
        print('入口已保留，暂未导出链接：'+clean(error))
        print('重新导出：miku node-link '+str(inbound_id))
    finally:
        print('请去面板“出口与节点 → 新建出口”创建出口，并选择这个入口。当前入口使用本 VPS 直接出口。')
        print('请在安全组与防火墙放行入口端口 '+str(created['port'])+'。')
