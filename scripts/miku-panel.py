#!/usr/bin/env python3
"""Explicit local 3x-ui persistent-template adapter. Secrets use private stdin.

API credentials never saved. Apply backs up persistent template and rendered
config, compares both source hashes before write, and verifies generated route
and unchanged inbounds after the panel's own save/reconcile. No config.json edit.
"""
import argparse
import hashlib
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import re
import socket
import stat
import sys
import urllib.parse
import urllib.request

spec=importlib.util.spec_from_file_location('panel_maintenance',Path(__file__).with_name('miku-maintain.py'))
maintenance=importlib.util.module_from_spec(spec);spec.loader.exec_module(maintenance)
install=maintenance.install
class PanelError(RuntimeError):pass
def require(value,message):
    if not value:raise PanelError(message)
def encoded(value):return json.dumps(value,ensure_ascii=False,separators=(',',':'),sort_keys=True).encode()
def digest(raw):return hashlib.sha256(raw).hexdigest()
def object_json(value):
    for _ in range(3):
        if isinstance(value,dict):return value
        require(isinstance(value,str) and len(value)<=2<<20,'不支持面板响应包装')
        value=json.loads(value)
    raise PanelError('面板响应嵌套过深')
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args,**kwargs):raise PanelError('面板跳转拒绝；只允许明确本机接口，不把登录凭据发到其他来源')
class Panel:
    def __init__(self,port,base,api_mode="current"):
        require(api_mode in ("current","legacy"),"面板API模式无效，不遍历猜接口")
        self.xray_path="panel/api/xray/" if api_mode=="current" else "panel/xray/"
        self.csrf=""
        require(1024<=port<=65535 and re.fullmatch(r'/(?:[A-Za-z0-9_-]+/)*',base) is not None,'仅明确127.0.0.1非特权面板端口和规范基础路径')
        self.origin='http://127.0.0.1:'+str(port)
        self.base=self.origin+base
        self.opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect(),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    def request(self,path,form=None):
        require(path in ('csrf-token','login',self.xray_path,self.xray_path+'update','panel/api/server/getConfigJson','panel/api/server/restartXrayService'),'未知面板操作拒绝')
        data=urllib.parse.urlencode(form).encode() if form is not None else None
        request=urllib.request.Request(self.base+path,data=data,headers={'User-Agent':'MikuGate-explicit-panel-adapter','Origin':self.origin,'Referer':self.base})
        if data is not None and self.csrf:request.add_header('X-CSRF-Token',self.csrf)
        try:
            with self.opener.open(request,timeout=15) as response:raw=response.read((3<<20)+1)
            require(len(raw)<=3<<20,'面板响应超预算');value=json.loads(raw)
            require(isinstance(value,dict) and value.get('success') is True,'面板接口拒绝或版本不支持，原始可能含秘密的响应不回显')
            return value.get('obj')
        except (OSError,ValueError):raise PanelError('本机面板接口失败，未回显原始响应/凭据') from None
    def read(self):
        settings=object_json(self.request(self.xray_path,{}))
        template=object_json(settings.get('xraySetting'))
        rendered=object_json(self.request('panel/api/server/getConfigJson'))
        return template,rendered,str(settings.get('outboundTestUrl') or 'https://www.google.com/generate_204')
    def save(self,template,test_url):
        self.request(self.xray_path+'update',{'xraySetting':encoded(template).decode(),'outboundTestUrl':test_url})
        # Older supported panels only persist update; explicitly ask the panel
        # to regenerate/restart its own core rather than altering generated files.
        self.request('panel/api/server/restartXrayService',{})
def verify_route(before,after,plan):
    require(before.get('inbounds')==after.get('inbounds'),'面板保存后入口/客户端改变，拒绝报告复用成功')
    tag=plan['outbound_tag'];outbounds={row.get('tag'):row for row in after.get('outbounds',[]) if isinstance(row,dict)}
    require(outbounds.get(tag,{}).get('protocol')=='socks' and outbounds.get(tag+'-udp-block',{}).get('protocol')=='blackhole','持久模板未生成期望出口/阻断')
    rules=after.get('routing',{}).get('rules',[]);selected=[r for r in rules if isinstance(r,dict) and plan['inbound_tag'] in r.get('inboundTag',[])]
    require(len(selected)>=2 and selected[0].get('network')=='udp' and selected[0].get('outboundTag')==tag+'-udp-block' and selected[1].get('network')=='tcp' and selected[1].get('outboundTag')==tag,'持久生成路由优先级/绑定未验证')
    require(outbounds[tag].get('settings',{}).get('servers',[{}])[0].get('port')==plan['proxy_port'],'生成路由代理端口不一致')
def cli(binary,command,value):
    uid=install.pwd.getpwnam('miku-gate').pw_uid;gid=install.pwd.getpwnam('miku-gate').pw_gid
    return json.loads(install.run([install.trusted_tool('setpriv'),'--reuid='+str(uid),'--regid='+str(gid),'--clear-groups','--no-new-privs',str(binary),'ingress',command],input_bytes=encoded(value)))
def verify_socks(proxy):
    require(isinstance(proxy,dict) and set(proxy)=={'port','username','password'} and isinstance(proxy['port'],int) and 1024<=proxy['port']<=65535,'仅固定本机认证SOCKS目标')
    username=proxy['username'].encode();password=proxy['password'].encode()
    require(1<=len(username)<=255 and 1<=len(password)<=255,'代理认证长度无效')
    def exact(sock,size):
        value=b''
        while len(value)<size:
            chunk=sock.recv(size-len(value));require(bool(chunk),'本机SOCKS响应截断');value+=chunk
        return value
    with socket.create_connection(('127.0.0.1',proxy['port']),timeout=3) as sock:
        sock.sendall(b'\x05\x01\x02');require(exact(sock,2)==b'\x05\x02','Miku出口没有认证SOCKS监听，请先启动健康出口')
        sock.sendall(b'\x01'+bytes([len(username)])+username+bytes([len(password)])+password)
        require(exact(sock,2)==b'\x01\x00','本机出口凭据无效，未写面板')

def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command',choices=('inspect','apply','rollback'))
    parser.add_argument('--port',type=int,required=True);parser.add_argument('--base',default='/');parser.add_argument('--api-mode',choices=('current','legacy'),default='current')
    parser.add_argument('--exit-id')
    parser.add_argument('--inbound');parser.add_argument('--expected-rendered');parser.add_argument('--expected-template')
    parser.add_argument('--backup');parser.add_argument('--archive');parser.add_argument('--archive-sha256')
    parser.add_argument('--confirm-read',action='store_true');parser.add_argument('--confirm-apply',action='store_true');parser.add_argument('--confirm-rollback',action='store_true')
    args=parser.parse_args(argv)
    require(sys.platform=='linux' and os.geteuid()==0 and args.confirm_read,'仅授权Linux root本机协调，需--confirm-read')
    require(not maintenance.JOURNAL.exists() and not maintenance.STARTUP_BLOCK.exists(),'Miku已有维护事务，先恢复，不叠加面板更改')
    state=maintenance.installed_state();binary=maintenance.FILES['binary/miku-gate']
    raw=sys.stdin.buffer.read(16385);require(len(raw)<=16384,'私有stdin过大');private=json.loads(raw)
    require(isinstance(private,dict) and set(private)<= {'username','password','two_factor_code','proxy'} and private.get('username') and private.get('password'),'stdin只接受面板登录和所选Miku代理凭据，不输出秘密')
    panel=Panel(args.port,args.base,args.api_mode)
    if args.api_mode=='current':
        token=panel.request('csrf-token');require(isinstance(token,str) and 16<=len(token)<=256,'面板CSRF握手不可用，不禁用校验');panel.csrf=token
    panel.request('login',{'username':private['username'],'password':private['password'],'twoFactorCode':private.get('two_factor_code','')})
    if args.api_mode=='current':
        token=panel.request('csrf-token');require(isinstance(token,str) and 16<=len(token)<=256,'登录后的面板CSRF不可用');panel.csrf=token
    template,rendered,test_url=panel.read();tsha=digest(encoded(template));rsha=digest(encoded(rendered))
    if args.command=='inspect':
        report=cli(binary,'panel-inspect',rendered);report.update(template_sha256=tsha,rendered_sha256=rsha,persistent_api=True,applied=False)
        print(json.dumps(report,ensure_ascii=False));return
    require(args.backup and args.expected_rendered==rsha and args.expected_template==tsha,'变更需确切双摘要和全新私有备份，变化时重新inspect')
    target=Path(args.backup);maintenance.check_output(target)
    if args.command=='apply':
        require(not args.confirm_rollback,'apply不能混用回滚确认')
        require(args.confirm_apply and args.inbound and isinstance(private.get('proxy'),dict) and not args.archive and args.exit_id,'应用需--confirm-apply/inbound和私有proxy，不混用回滚')
        verify_socks(private['proxy'])
        generated=cli(binary,'panel-plan',{'rendered':rendered,'template':template,'expected_rendered':rsha,'expected_template':tsha,'inbound':args.inbound,'proxy':private['proxy'],'data':'/var/lib/miku-gate/data','instance':state['instance'],'exit_id':args.exit_id})
        candidate=generated['template'];plan=generated['plan']
    else:
        require(not args.confirm_apply and not args.inbound and not args.exit_id,'rollback不混用应用目标')
        require(args.confirm_rollback and args.archive and args.archive_sha256,'回滚需明确确认与私有归档SHA256')
        source,_=maintenance.safe_path(Path(args.archive),0,0o600,4<<20)
        require(digest(source)==args.archive_sha256,'回滚归档摘要不匹配');old=json.loads(source)
        require(old['instance']==state['instance'] and old['port']==args.port and old['base']==args.base and old['api_mode']==args.api_mode,'只能恢复同安装同面板来源')
        candidate=old['template'];plan=None
        require(rendered.get('inbounds')==old['rendered'].get('inbounds'),'回滚归档入口已变化，拒绝用旧路由覆盖当前面板')
    backup={'format':1,'instance':state['instance'],'port':args.port,'base':args.base,'api_mode':args.api_mode,'template':template,'rendered':rendered,'outbound_test_url':test_url,'template_sha256':tsha,'rendered_sha256':rsha}
    install.exclusive(target,encoded(backup),0o600)
    latest,live,_=panel.read();require(digest(encoded(latest))==tsha and digest(encoded(live))==rsha,'备份后面板数据变化，未写入')
    try:
        panel.save(candidate,test_url)
        saved,actual,_=panel.read();require(encoded(saved)==encoded(candidate),'面板持久模板读回不符')
        if plan:verify_route(rendered,actual,plan)
        else:require(actual.get('inbounds')==rendered.get('inbounds'),'回滚改变现有入口，拒绝')
    except BaseException:
        try:
            panel.save(template,test_url);restored,_,_=panel.read();require(encoded(restored)==encoded(template),'旧模板恢复读回失败')
        except BaseException:raise PanelError('PANEL_RECOVERY_REQUIRED：原私有备份保留，面板状态不确定，请停止所选入口并明确恢复。') from None
        raise PanelError('PANEL_CHANGE_FAILED_ROLLBACK_VERIFIED：已恢复旧持久模板，未报告应用成功。') from None
    print(json.dumps({'instance':state['instance'],'operation':args.command,'persistent_template_verified':True,'generated_route_verified':bool(plan),'existing_inbounds_preserved':True,'backup_sha256':digest(encoded(backup)),'business_path_verified':False,'panel_password_saved':False},sort_keys=True))
if __name__=='__main__':
    try:main()
    except (PanelError,maintenance.BackupError,install.InstallError,OSError,ValueError,KeyError) as exc:
        print('PANEL_STOPPED '+type(exc).__name__+': '+str(exc) if isinstance(exc,(PanelError,maintenance.BackupError,install.InstallError)) else 'PANEL_STOPPED external error; private diagnostics omitted',file=sys.stderr);sys.exit(1)
