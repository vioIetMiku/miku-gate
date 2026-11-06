"""Ingress wizard transaction boundaries and exact per-inbound sharing links."""
import contextlib
import io
import unittest
import urllib.parse
from unittest.mock import patch
import inbound_wizard as w


class Panel:
    def __init__(self,kind='sing-box',tunnels=(),existing=(),fail_bind=False):
        self.kind=kind;self.tunnels=list(tunnels);self.existing=list(existing);self.fail_bind=fail_bind;self.calls=[]
    def request(self,method,path,body=None):
        self.calls.append((method,path,body))
        route=path.split('?',1)[0]
        if route=='api/panel/mode':
            if method=='POST':self.kind=body['mode'];return {}
            return {'kind':self.kind,'mode':self.kind,'modes':[{'mode':'sing-box','available':True},{'mode':'native','available':True}]}
        if route=='api/tunnels':return self.tunnels
        if route=='api/domains':return {'certificates':[]}
        if route=='api/xui/inbounds':return self.existing+[{'id':41,'tag':'inbound-41'}]
        if route=='api/panel/inbound/new':return {'id':41,'port':45001}
        if route=='api/xui/bind':
            if self.fail_bind:raise RuntimeError('fixture binding failure')
            return {}
        if route=='api/xui/delete':return {}
        if route=='api/xui/links':return {'links':['vless://fixture@192.0.2.1:45001?security=reality#new-only']}
        raise AssertionError((method,path,body))


class WizardTests(unittest.TestCase):
    def run_wizard(self,panel,answers):
        with patch('builtins.input',side_effect=answers),contextlib.redirect_stdout(io.StringIO()) as out:
            result=w.create(panel)
        return result,out.getvalue()

    def test_defaults_use_singbox_vision_amd_and_export_only_created_id(self):
        p=Panel()
        links,out=self.run_wizard(p,['','','','',''])
        create=next(path for method,path,_ in p.calls if path.startswith('api/panel/inbound/new?'))
        q=urllib.parse.parse_qs(urllib.parse.urlsplit(create).query)
        self.assertEqual({name:q[name][0] for name in ('protocol','network','security','vision','dest','server_names')},
                         {'protocol':'vless','network':'tcp','security':'reality','vision':'1','dest':'www.amd.com:443','server_names':'www.amd.com'})
        self.assertEqual(p.calls[-1][1],'api/xui/links?ids=41')
        self.assertEqual(len(links),1);self.assertEqual(out.count(links[0]),1)
        self.assertFalse(any('api/sub' in path for _,path,_ in p.calls))
        self.assertNotIn('XHTTP',out)

    def test_cancel_protocol_does_not_switch_core_or_create_inbound(self):
        p=Panel(kind='native')
        self.run_wizard(p,['','0'])
        self.assertTrue(all(method=='GET' for method,_,_ in p.calls))

    def test_xray_xhttp_can_choose_hong_kong_reality_preset(self):
        p=Panel(kind='native')
        self.run_wizard(p,['2','8','2','45001',''])
        path=next(path for _,path,_ in p.calls if path.startswith('api/panel/inbound/new?'))
        q=urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)
        self.assertEqual(q['network'],['xhttp']);self.assertEqual(q['vision'],['0'])
        self.assertEqual(q['dest'],['www.tesla.com:443'])

    def test_tls_without_certificate_does_not_mutate_runtime(self):
        p=Panel()
        _,out=self.run_wizard(p,['','2'])
        self.assertIn('需要有效 TLS 证书',out)
        self.assertTrue(all(method=='GET' for method,_,_ in p.calls))

    def test_refused_shared_core_switch_preserves_existing_nodes(self):
        p=Panel(kind='native',existing=[{'id':8}])
        self.run_wizard(p,['','','','','','no'])
        self.assertTrue(all(method=='GET' for method,_,_ in p.calls))

    def test_new_ingress_has_no_egress_picker_and_exports_single_link(self):
        p=Panel(tunnels=[{'status':'up','slot':9,'exit_ip':'192.0.2.9','node':{'hostname':'vpn-node','country':'日本'}}],fail_bind=True)
        links,out=self.run_wizard(p,['','','','',''])
        self.assertEqual(len(links),1)
        self.assertIn('新建出口',out)
        self.assertNotIn('选择这个入口的流量出口',out)
        self.assertFalse(any(path.startswith(('api/xui/bind','api/tunnels','api/sub')) for _,path,_ in p.calls))

    def test_singbox_reality_variants_are_hidden_for_xray(self):
        for index,(protocol,network) in enumerate((('vless','ws'),('vmess','ws'),('trojan','tcp')),3):
            p=Panel()
            self.run_wizard(p,['',str(index),'','',''])
            path=next(path for _,path,_ in p.calls if path.startswith('api/panel/inbound/new?'))
            query=urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)
            self.assertEqual([query[name][0] for name in ('protocol','network','security','vision')],[protocol,network,'reality','0'])
            self.assertEqual(w.protocols_for('native')[index-1][3],'tls')

    def test_new_arguments_and_invalid_link_ids_cannot_call_api(self):
        from console import node_command
        with self.assertRaises(RuntimeError):node_command('new',['unexpected'])
        for inbound_id in ('0','-1','41,8','41&ids=8'):
            p=Panel()
            with self.assertRaises(RuntimeError):w.node_links(inbound_id,p)
            self.assertFalse(p.calls)

    def test_docker_first_two_menu_entries_dispatch_to_correct_commands(self):
        import docker_console
        def inspect(mode,actions,status):
            actions['1']();actions['2']()
        with patch('console.menu',side_effect=inspect),patch.object(docker_console,'execute') as execute:
            docker_console.menu()
        self.assertEqual([call.args[0] for call in execute.call_args_list],[['quick-menu'],['new']])

    def test_empty_null_certificate_catalog_is_handled_without_side_effects(self):
        with patch.object(Panel,'request',return_value={'certificates':None}),contextlib.redirect_stdout(io.StringIO()):
            self.assertIsNone(w.tls_certificate(Panel()))


if __name__=='__main__':unittest.main()
