import importlib.util
from pathlib import Path
import unittest
spec=importlib.util.spec_from_file_location('panel',Path(__file__).with_name('miku-panel.py'))
panel=importlib.util.module_from_spec(spec);spec.loader.exec_module(panel)
class PersistentPanelSafety(unittest.TestCase):
    def test_local_only_explicit_routes_and_wrapper_bounds(self):
        for port,base in [(443,'/'),(2000,'https://evil.example/'),(2000,'/../'),(2000,'//other/')]:
            with self.assertRaises(panel.PanelError):panel.Panel(port,base)
        p=panel.Panel(2053,'/secret/')
        self.assertEqual(p.base,'http://127.0.0.1:2053/secret/');self.assertEqual(p.xray_path,'panel/api/xray/')
        with self.assertRaises(panel.PanelError):p.request('http://elsewhere/')
        self.assertEqual(panel.object_json('{"x":1}'),{'x':1})
    def test_generated_routes_keep_clients_and_udp_first(self):
        before={'inbounds':[{'tag':'entry','clients':['private']} ]}
        after={**before,'outbounds':[{'tag':'miku','protocol':'socks','settings':{'servers':[{'port':2000}]}},{'tag':'miku-udp-block','protocol':'blackhole'}],'routing':{'rules':[{'inboundTag':['entry'],'network':'udp','outboundTag':'miku-udp-block'},{'inboundTag':['entry'],'network':'tcp','outboundTag':'miku'}]}}
        plan={'outbound_tag':'miku','inbound_tag':'entry','proxy_port':2000}
        panel.verify_route(before,after,plan)
        bad={**after,'inbounds':[{'tag':'entry','clients':['changed']}]}
        with self.assertRaises(panel.PanelError):panel.verify_route(before,bad,plan)
        after['routing']['rules'].reverse()
        with self.assertRaises(panel.PanelError):panel.verify_route(before,after,plan)
if __name__=='__main__':unittest.main()
