import importlib.util
from pathlib import Path
import unittest
spec=importlib.util.spec_from_file_location("encrypted",Path(__file__).with_name("vps-ingress-vpn-live.py"))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
class FullEncryptedAcceptanceSafety(unittest.TestCase):
    def test_explicit_core_paths_only_added_to_serve(self):
        module.EncryptedControl.cores={"xray":Path("/root-owned/xray"),"sing-box":Path("/root-owned/sing-box")}
        control=module.EncryptedControl(Path("/root-owned/miku-gate"),Path("/private/data"))
        args=control.argv("serve")
        self.assertIn("--no-new-privs",args);self.assertIn("--reuid=65534",args);self.assertIn("--vpn-executor",args);self.assertIn("--xray-core",args);self.assertIn("--sing-box-core",args)
        self.assertNotIn("--xray-core",control.argv("instance"))
    def test_client_does_not_disable_server_authentication(self):
        text=Path(__file__).with_name("vps-ingress-vpn-live.py").read_text()
        self.assertNotIn('"insecure":True',text)
        self.assertIn('runtime["phase"]=="stopped"',text)
        self.assertIn('raw_ip==running["proof"]["exit_ip"]',text)
if __name__=="__main__":unittest.main()
