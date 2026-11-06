"""Installer regressions: TTY rendering, owned launchers and both uninstall modes."""
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import common as c
import console
import docker_console as dc
import install
import install_mode
import maintain

ROOT=Path(__file__).resolve().parents[1]


@unittest.skipUnless(os.name=='posix','Linux root lifecycle fixtures')
class InstallLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root=Path(self.tmp.name)

    def docker_fixture(self):
        project=self.root/'project';project.mkdir()
        for name in ('compose.yaml','install-docker.sh','scripts/docker_console.py','bin/miku-linux-amd64',
                     'docker-data/password','docker-data/native.json','docker-certs/etc/key',
                     'docker-backups/backup.tar.gz','.env','frontend/.runtime/state.json','frontend/public/index.html'):
            target=project/name;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(('fixture:'+name).encode())
        commands=self.root/'commands';commands.mkdir()
        return project,commands

    def test_default_reinstall_recognizes_owned_docker_shortcut(self):
        project,commands=self.docker_fixture()
        with patch.object(dc,'ROOT',project),patch.object(c,'root'),contextlib.redirect_stdout(io.StringIO()):
            dc.register(commands)
            self.assertEqual(install_mode.docker_project(commands/'miku'),project)
        (commands/'miku').write_text('# foreign shortcut\n')
        self.assertIsNone(install_mode.docker_project(commands/'miku'))

    def test_two_docker_instances_do_not_take_over_each_others_command(self):
        project,commands=self.docker_fixture()
        with patch.object(dc,'ROOT',project),patch.object(c,'root'),contextlib.redirect_stdout(io.StringIO()):dc.register(commands)
        before=(commands/'miku').read_bytes()
        with patch.object(dc,'ROOT',self.root/'other'),patch.object(c,'root'),contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaises(RuntimeError):dc.register(commands)
        self.assertEqual((commands/'miku').read_bytes(),before)

    def test_docker_uninstall_preserves_data_and_removes_programs_and_owned_commands(self):
        project,commands=self.docker_fixture()
        preserved={name:(project/name).read_bytes() for name in ('docker-data/password','docker-data/native.json','docker-certs/etc/key','docker-backups/backup.tar.gz','.env','frontend/.runtime/state.json')}
        with patch.object(dc,'ROOT',project),patch.object(c,'root'),patch.object(dc,'execute') as down,patch.object(dc.subprocess,'run'),contextlib.redirect_stdout(io.StringIO()):
            dc.register(commands)
            (commands/'other-panel').write_text('preserve')
            dc.uninstall(directory=commands)
        down.assert_called_once_with(['down'])
        self.assertEqual(preserved,{name:(project/name).read_bytes() for name in preserved})
        self.assertFalse((project/'bin').exists());self.assertFalse((project/'scripts').exists())
        self.assertFalse((commands/'miku').exists());self.assertTrue((commands/'other-panel').exists())

    def test_docker_purge_removes_only_current_project_and_preserves_foreign_shortcut(self):
        project,commands=self.docker_fixture()
        (commands/'miku').write_text('# foreign command\n')
        neighbor=self.root/'other-project';neighbor.mkdir();(neighbor/'data').write_bytes(b'keep')
        with patch.object(dc,'ROOT',project),patch.object(c,'root'),patch.object(dc,'execute'),patch.object(dc.subprocess,'run'),contextlib.redirect_stdout(io.StringIO()):
            dc.uninstall(True,commands)
        self.assertFalse(project.exists());self.assertEqual((neighbor/'data').read_bytes(),b'keep')
        self.assertEqual((commands/'miku').read_text(),'# foreign command\n')

    def test_failed_docker_shutdown_never_deletes_data_or_shortcuts(self):
        project,commands=self.docker_fixture()
        with patch.object(dc,'ROOT',project),patch.object(c,'root'),contextlib.redirect_stdout(io.StringIO()):
            dc.register(commands)
            with patch.object(dc,'execute',side_effect=RuntimeError('down failed')):
                with self.assertRaises(RuntimeError):dc.uninstall(True,commands)
        self.assertTrue((project/'docker-data/password').exists());self.assertTrue((commands/'miku').exists())

    def test_mountpoint_is_rejected_before_stopping_docker(self):
        project,commands=self.docker_fixture()
        with patch.object(dc,'ROOT',project),patch.object(c,'root'),patch('os.path.ismount',side_effect=lambda p:Path(p)==project/'docker-data'),patch.object(dc,'execute') as stop:
            with self.assertRaises(RuntimeError):dc.uninstall(True,commands)
        stop.assert_not_called();self.assertTrue((project/'docker-data/password').exists())

    def test_destructive_menu_cancellation_does_nothing_and_success_exits(self):
        with patch('builtins.input',return_value='no'),patch.object(dc,'uninstall') as uninstall,contextlib.redirect_stdout(io.StringIO()):dc.confirm_uninstall(True)
        uninstall.assert_not_called()
        with patch.object(sys.stdin,'isatty',return_value=True),patch('builtins.input',side_effect=['19','DELETE']),patch.object(dc,'uninstall') as uninstall,contextlib.redirect_stdout(io.StringIO()):
            console.menu('fixture',{'19':lambda:dc.confirm_uninstall(True)})
        uninstall.assert_called_once_with(True)

    def test_native_uninstall_retains_credentials_and_reinstall_restores_program(self):
        data=self.root/'data';data.mkdir();lib=self.root/'lib';lib.mkdir(mode=0o755)
        menu=self.root/'miku';unit=self.root/'miku.service';openrc=self.root/'openrc'
        with contextlib.ExitStack() as stack:
            for key,value in {'DATA':data,'LIB':lib,'MENU':menu,'UNIT':unit,'OPENRC':openrc}.items():stack.enter_context(patch.object(c,key,value))
            for name in ('root','dependencies','ensure_cores','service','wait_ready','info','run'):stack.enter_context(patch.object(c,name))
            stack.enter_context(patch.object(c,'systemd',return_value=True))
            stack.enter_context(patch.object(c,'active',return_value=False))
            stack.enter_context(patch.object(c,'old_maintenance',return_value=(None,None)))
            stack.enter_context(patch.object(c,'choose_port',return_value=19090))
            c.atomic(lib/'miku',b'old',0o755)
            c.atomic(menu,('exec python3 '+str(lib/'maintain.py')).encode(),0o755)
            c.atomic(unit,('ExecStart='+str(lib/'miku')).encode(),0o644)
            for name,value in {'password':b'fixture-password','basepath':b'fixture-base','native.json':b'{"inbounds":[]}'}.items():c.atomic(data/name,value)
            c.write_json(data/'settings.json',{'port':19090,'public_ip':'192.0.2.1'})
            c.write_json(data/'install.json',{'product':'miku-fanout','version':'0.2.7','binary_sha256':hashlib.sha256(b'old').hexdigest()})
            before={name:(data/name).read_bytes() for name in ('password','basepath','native.json','settings.json')}
            with contextlib.redirect_stdout(io.StringIO()):maintain.uninstall()
            self.assertFalse(lib.exists());self.assertFalse(menu.exists());self.assertTrue(data.exists())
            bundle=self.root/'bundle';bundle.mkdir()
            arch=c.arch();binary='bin/miku-linux-'+arch
            names=[binary,'bin/miku-export-linux-'+arch,'CORE_ASSETS.json','LICENSE','COPYING','NOTICE','FLAG_ICONS_LICENSE',
                   *('scripts/'+name for name in ('common.py','maintain.py','quick.py','install.py','console.py','inbound_wizard.py','miku.sh'))]
            for name in names:
                target=bundle/name;target.parent.mkdir(parents=True,exist_ok=True);c.atomic(target,b'fixture-new',0o755 if name.startswith('bin/') else 0o644)
            meta={'version':'0.2.7','files':{binary:hashlib.sha256(b'fixture-new').hexdigest()}}
            with patch.object(c,'verify_distribution',return_value=(bundle,meta)),contextlib.redirect_stdout(io.StringIO()):install._install(bundle)
            self.assertTrue(menu.exists());self.assertEqual((lib/'miku').read_bytes(),b'fixture-new')
            self.assertEqual(before,{name:(data/name).read_bytes() for name in before})

    def test_progress_is_left_aligned_with_terminal_onlcr_disabled(self):
        import pty
        import termios
        master,slave=pty.openpty()
        try:
            attrs=termios.tcgetattr(slave);attrs[1]&=~termios.ONLCR;termios.tcsetattr(slave,termios.TCSANOW,attrs)
            result=subprocess.run([sys.executable,'-B',str(ROOT/'scripts/progress.py'),'正在安装',sys.executable,'-c','print("hidden")'],stdin=subprocess.DEVNULL,stdout=slave,stderr=slave,timeout=20)
            os.close(slave);slave=None
            output=b''
            while True:
                try:part=os.read(master,65536)
                except OSError:break
                if not part:break
                output+=part
            self.assertEqual(result.returncode,0)
            self.assertNotIn(b'hidden',output)
            self.assertTrue(all(line.startswith(b'\r') for line in output.split(b'\n') if line))
        finally:
            os.close(master)
            if slave is not None:os.close(slave)

    def test_entry_and_local_installer_share_one_banner(self):
        # Exercise the real entry's banner, source local banner twice and call
        # the real native install function with only root mutations mocked.
        script=(ROOT/'mikugate.sh').read_text().split("url='https://",1)[0]
        script+='\nsource '+shlex.quote(str(ROOT/'scripts/banner.sh'))+'\n'
        code='import contextlib,os; from unittest.mock import patch; import common,install; from pathlib import Path\nwith patch.object(common,"root"),patch.object(common,"verify_distribution",return_value=(Path("/fixture"),{})),patch.object(common,"maintenance_lock",contextlib.nullcontext),patch.object(install,"_install"):\n install.install(Path("/fixture"))\n'
        script+=shlex.quote(sys.executable)+' -c '+shlex.quote(code)+'\n'
        result=subprocess.run(['bash','-c',script],cwd=ROOT/'scripts',env={**os.environ,'MIKU_INSTALL_BANNER_SHOWN':'0'},capture_output=True,text=True,timeout=10)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(result.stdout.count('你的连接，你的节奏'),1,result.stdout)


if __name__=='__main__':unittest.main()
