import importlib.util
from pathlib import Path
import unittest
from unittest import mock
import io
SPEC=importlib.util.spec_from_file_location("public_vpn",Path(__file__).with_name("vps-vpngate-live.py"))
public=importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(public)
class PublicVPNBudget(unittest.TestCase):
    def test_candidate_selection_has_fixed_small_budget_and_source(self):
        nodes=[{"id":str(i),"hostname":"vpn"+str(i),"source":"https://www.vpngate.net/api/iphone/"} for i in range(100)]
        self.assertEqual(len(public.select_candidates(nodes)),2)
        for host in ("vpn123.evil.example","localhost","127.0.0.1","VPN123"):
            self.assertFalse(public.select_candidates([{"hostname":host,"source":nodes[0]["source"]}]))
        self.assertFalse(public.select_candidates([{"hostname":"vpn123","source":"http://untrusted.example"}]))
    def test_tcp_preference_uses_at_most_twenty_local_cache_reads_and_two_peers(self):
        nodes = [{"id":str(i),"hostname":"vpn"+str(i),"source":"https://www.vpngate.net/api/iphone/"} for i in range(100)]
        calls=[]
        def read(id):
            calls.append(id)
            return {"mode":"legacy-candidate","transport":"tcp4-client" if int(id) in (3,6) else "udp4"}
        chosen=public.select_candidates(nodes,read)
        self.assertEqual([n["id"] for n in chosen],["3","6"])
        self.assertEqual(len(calls),7)
        calls.clear()
        chosen=public.select_candidates(nodes,lambda id: calls.append(id) or {"mode":"unsupported"})
        self.assertEqual(chosen,[])
        self.assertEqual(len(calls),20)
    def test_public_path_always_starts_nonroot_control_and_explicit_executor(self):
        control=public.UserControl(Path("/verified/miku-gate"),Path("/owned/data"))
        for command in ("init","serve","instance"):
            args=control.argv(command)
            self.assertEqual(args[0],"/usr/bin/setpriv")
            self.assertIn("--reuid=65534",args)
            self.assertIn("--no-new-privs",args)
        self.assertIn("--vpn-executor",control.argv("serve"))
    def test_daemon_constructor_failure_reaps_and_closes_output(self):
        process=mock.Mock()
        process.stdout=io.BytesIO(b"failed preflight\n")
        process.poll.return_value=1
        with mock.patch.object(public.subprocess,"Popen",return_value=process):
            with self.assertRaisesRegex(RuntimeError,"failed its preflight"):
                public.Daemon(Path("/verified/miku-gate"),"test")
        process.wait.assert_called_once()
        self.assertTrue(process.stdout.closed)

if __name__=="__main__":unittest.main()
