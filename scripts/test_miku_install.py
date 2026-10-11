import contextlib
import importlib.util
import io
from pathlib import Path, PurePosixPath
import unittest
from unittest import mock
spec=importlib.util.spec_from_file_location('installer',Path(__file__).with_name('miku-install.py'))
install=importlib.util.module_from_spec(spec);spec.loader.exec_module(install)
class InstallerPlanningSafety(unittest.TestCase):
    def test_dependency_refusal_never_invokes_package_manager(self):
        prompt=install.Prompt({'install_dependencies':'no'})
        with mock.patch.object(install,'run') as run,contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaises(install.InstallError):install.install_missing([{'tool':'openvpn','package':'openvpn'}],prompt)
            run.assert_not_called()
    def test_plan_only_has_no_apply_or_install_and_preserves_reuse_intent(self):
        answers={'enable_vpn':'no','vps_ip':'8.8.8.8','use_domain':'no','management_port':'8443','reuse_existing':'yes','confirm_deferred_reuse':'yes','managed_core':'both','confirm_ip_certificate':'yes'}
        inventory={'components':[{'id':'x-ui','name':'x-ui','state':'detected'}]}
        with mock.patch.object(install,'platform_gate',return_value='amd64'),mock.patch.object(install,'validate_binary',return_value=(Path('/verified/miku'),b'ELF')),mock.patch.object(install,'refusal_preflight'),mock.patch.object(install,'dependencies',return_value=[{'tool':'openvpn','package':'openvpn'}]),mock.patch.object(install,'public_ip_candidate',return_value='8.8.8.8'),mock.patch.object(install,'port_available',return_value=True),mock.patch.object(install,'run',return_value=__import__('json').dumps(inventory).encode()),mock.patch.object(install,'install_missing') as dep,mock.patch.object(install,'apply') as apply,contextlib.redirect_stdout(io.StringIO()):
            plan=install.main(['--binary','/verified/miku','--sha256','a'*64,'--plan-only'],install.Prompt(answers))
            self.assertEqual(plan['origin'],'https://8.8.8.8:8443');self.assertTrue(plan['reuse_requested']);self.assertEqual(plan['cores'],['xray','sing-box'])
            dep.assert_not_called();apply.assert_not_called();self.assertFalse(plan['existing_service_configs_changed'])
    def test_final_apply_refusal_never_creates_accounts_or_units(self):
        answers={'enable_vpn':'no','vps_ip':'8.8.8.8','use_domain':'no','management_port':'8443','managed_core':'none','confirm_ip_certificate':'yes','confirm_install':'no'}
        with mock.patch.object(install,'platform_gate',return_value='amd64'),mock.patch.object(install,'validate_binary',return_value=(Path('/verified/miku'),b'ELF')),mock.patch.object(install,'refusal_preflight'),mock.patch.object(install,'dependencies',return_value=[]),mock.patch.object(install,'public_ip_candidate',return_value='8.8.8.8'),mock.patch.object(install,'port_available',return_value=True),mock.patch.object(install,'run',return_value=b'{"components":[]}'),mock.patch.object(install,'apply') as apply,contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaises(install.InstallError):install.main(['--binary','/verified/miku','--sha256','a'*64],install.Prompt(answers))
            apply.assert_not_called()
    def test_plan_only_domain_issue_does_not_install_certbot(self):
        answers={'issue_certificate':'yes','certificate_email':'admin@example.com'}
        with mock.patch.object(install,'source_certificate',return_value=None),mock.patch.object(install,'port_available',return_value=True),mock.patch.object(install.shutil,'which',return_value=None),mock.patch.object(install.socket,'getaddrinfo',return_value=[(2,1,6,'',('8.8.8.8',80))]),mock.patch.object(install,'install_missing') as dep,contextlib.redirect_stdout(io.StringIO()):
            plan=install.certificate_plan(Path('/verified/miku'),install.Prompt(answers),'entry.example','8.8.8.8',plan_only=True)
            self.assertEqual(plan['mode'],'domain-issue');dep.assert_not_called()
    def test_domain_port_80_conflict_does_not_stop_existing_service(self):
        with mock.patch.object(install,'source_certificate',return_value=None),mock.patch.object(install,'port_available',return_value=False),mock.patch.object(install,'run') as run,contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaises(install.InstallError):install.certificate_plan(Path('/verified/miku'),install.Prompt({'issue_certificate':'yes'}),'entry.example','8.8.8.8')
            run.assert_not_called()
    def test_units_separate_control_and_root_vpn_no_shell_command(self):
        uid={name:(1000+i,1000+i) for i,name in enumerate(install.USERS)}
        units=install.unit_text(PurePosixPath('/usr/local/lib/miku-gate/miku-gate'),PurePosixPath('/var/lib/miku-gate/data'),'https://8.8.8.8:8443',8443,PurePosixPath('/var/lib/miku-gate/data/management/certificate.pem'),PurePosixPath('/var/lib/miku-gate/data/management/private.key'),True,True,'a'*22,uid,{'xray':PurePosixPath('/usr/local/lib/miku-gate/xray')})
        control=units['miku-gate.service'];root=units['miku-gate-vpn.service']
        self.assertIn('User=miku-gate',control);self.assertIn('NoNewPrivileges=yes',control);self.assertIn('MemoryMax=384M',control);self.assertIn('--allow-bootstrap-certificate',control)
        self.assertIn('User=root',root);self.assertIn('--allow-uid=1000',root);self.assertIn('--tunnel-uid=1001',root);self.assertIn('--worker-uid=1002',root)
        self.assertNotIn('sh -c',control);self.assertNotIn('iptables',control+root);self.assertNotIn('nginx',control+root)
    def test_selected_core_download_failure_precedes_all_installation_writes(self):
        plan={'port':8443,'arch':'amd64','cores':['xray','sing-box']}
        with mock.patch.object(install,'refusal_preflight'),mock.patch.object(install,'port_available',return_value=True),mock.patch.object(install,'core_bytes',side_effect=install.InstallError('pinned download failed')),mock.patch.object(install,'make_dir') as mkdir,mock.patch.object(install,'exclusive') as write,mock.patch.object(install,'run') as run:
            with self.assertRaises(install.InstallError):install.apply(b'ELF','a'*64,plan,install.Prompt({}))
            mkdir.assert_not_called();write.assert_not_called();run.assert_not_called()
    def test_vpn_health_uses_only_authorized_ordinary_control_uid(self):
        ids={name:(1000+i,1000+i) for i,name in enumerate(install.USERS)}
        response={'instance':'a'*22,'mode':'isolated-openvpn','version':1,'options':{'tunnel_uid':1001,'worker_uid':1002}}
        with mock.patch.object(install,'trusted_tool',return_value='/usr/bin/setpriv'),mock.patch.object(install,'run',return_value=__import__('json').dumps(response).encode()) as run:
            install.verify_vpn_executor(PurePosixPath('/usr/local/lib/miku-gate/miku-gate'),'a'*22,ids)
            args=run.call_args.args[0]
            self.assertIn('--reuid=1000',args);self.assertIn('--regid=1000',args);self.assertIn('--no-new-privs',args)
        response['options']['tunnel_uid']=0
        with mock.patch.object(install,'trusted_tool',return_value='/usr/bin/setpriv'),mock.patch.object(install,'run',return_value=__import__('json').dumps(response).encode()):
            with self.assertRaises(install.InstallError):install.verify_vpn_executor(PurePosixPath('/usr/local/lib/miku-gate/miku-gate'),'a'*22,ids)
    def test_hostname_rejects_shell_paths_and_ip(self):
        for bad in ('/etc/shadow','../domain.example','example.com:443','https://example.com','8.8.8.8','x;reboot.example','*.example'):
            with self.assertRaises(install.InstallError):install.hostname(bad)
    def test_fixed_official_architecture_assets_have_sha256_and_safe_members(self):
        self.assertEqual(len(install.CORE_ASSETS),4)
        for (core,arch),item in install.CORE_ASSETS.items():
            self.assertIn(arch,('amd64','arm64'));self.assertEqual(len(item[3]),64);self.assertNotIn('..',item[5]);self.assertLess(item[4],48<<20)
if __name__=='__main__':unittest.main()
