"""CLI selection, credential visibility, safe shortcut registration and quiet logs."""
import contextlib
import io
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
import zipfile
from unittest.mock import Mock, patch
import console
import docker_console
import progress
import common as c


class ConsoleTests(unittest.TestCase):
    def test_core_fallback_resolves_only_matching_official_asset(self):
        record={'repo':'XTLS/Xray-core','version':'v1.2.3','name':'core.zip','bytes':42}
        def response(url):
            raw=json.dumps({'tag_name':'v1.2.3','assets':[{'name':'core.zip','size':42,'state':'uploaded','url':url}]}).encode()
            stream=io.BytesIO(raw);stream.geturl=lambda:'https://api.github.com/repos/XTLS/Xray-core/releases/tags/v1.2.3';return stream
        official='https://api.github.com/repos/XTLS/Xray-core/releases/assets/123'
        with patch('urllib.request.urlopen',return_value=response(official)) as fetch:
            self.assertEqual(c.core_asset_api(record),official)
            self.assertEqual(fetch.call_args.args[0].get_header('Accept'),'application/vnd.github+json')
        for invalid in ('http://api.github.com/repos/XTLS/Xray-core/releases/assets/123','https://untrusted.example/core.zip','https://api.github.com/repos/other/repo/releases/assets/123'):
            with patch('urllib.request.urlopen',return_value=response(invalid)),self.assertRaisesRegex(RuntimeError,'API地址异常'):c.core_asset_api(record)

    def test_native_install_default_runs_fixed_quick_setup(self):
        import install
        import maintain
        with patch.object(sys,'argv',['install.py']),patch.object(install,'install',return_value=True),patch('os.isatty',return_value=True),patch('builtins.input',return_value=''),patch.object(maintain,'menu') as menu,patch('quick.terminal_quick') as quick,contextlib.redirect_stdout(io.StringIO()):
            install.main()
        quick.assert_called_once_with(background=False);menu.assert_not_called()

    def test_explicit_second_option_still_opens_menu(self):
        import install
        import maintain
        with patch.object(sys,'argv',['install.py']),patch.object(install,'install',return_value=True),patch('os.isatty',return_value=True),patch('builtins.input',return_value='2'),patch.object(maintain,'menu') as menu,patch('quick.terminal_quick') as quick,contextlib.redirect_stdout(io.StringIO()):install.main()
        menu.assert_called_once();quick.assert_not_called()

    def test_country_input_builds_real_api_request_and_no_shell_command(self):
        panel=Mock()
        panel.request.side_effect=[[{'code':'JP','name':'日本','available':3}],{'job':'job-1'}]
        with patch('builtins.input',side_effect=['1','2']),contextlib.redirect_stdout(io.StringIO()):console.provision(panel)
        panel.request.assert_called_with('POST','api/provision?region=JP&count=2&template=-1')

    def test_invalid_count_never_submits_job(self):
        panel=Mock();panel.request.return_value=[{'code':'US','available':2}]
        with patch('builtins.input',side_effect=['1','$(touch /tmp/invalid)']),contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaises(RuntimeError):console.provision(panel)
        self.assertEqual(panel.request.call_count,1)

    def test_swap_uses_existing_slot_and_does_not_recreate_credentials(self):
        panel=Mock();panel.request.side_effect=[[{'slot':7,'exit_ip':'192.0.2.4','node':{'country':'日本'}}],{}]
        with patch('builtins.input',return_value='1'),contextlib.redirect_stdout(io.StringIO()):console.swap(panel)
        panel.request.assert_called_with('POST','api/swap?slot=7')

    def test_menu_returns_after_action_and_tolerates_failure(self):
        action=Mock(side_effect=RuntimeError('service unavailable'))
        with patch.object(sys.stdin,'isatty',return_value=True),patch('builtins.input',side_effect=['8','','0']),contextlib.redirect_stdout(io.StringIO()) as output:
            console.menu('fixture',{'8':action})
        action.assert_called_once();self.assertIn('service unavailable',output.getvalue())

    @unittest.skipUnless(os.name=='posix','Linux permissions and installation ownership')
    def test_shortcut_preserves_foreign_native_command_and_uses_persistent_path(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);native=root/'miku';native.write_text('# existing native command\n')
            with patch.object(c,'root'),contextlib.redirect_stdout(io.StringIO()):docker_console.register(root)
            self.assertEqual(native.read_text(),'# existing native command\n')
            wrapper=(root/'miku-docker').read_text()
            self.assertIn(str(docker_console.ROOT/'scripts/docker_console.py'),wrapper)
            self.assertIn('"$@"',wrapper)

    @unittest.skipUnless(os.name=='posix','Private file modes on Linux')
    def test_progress_hides_success_noise_and_retains_failure_code_and_private_log(self):
        with tempfile.TemporaryDirectory() as temporary,patch('tempfile.gettempdir',return_value=temporary):
            with contextlib.redirect_stdout(io.StringIO()) as out:
                code=progress.run('正在安装',[sys.executable,'-c','print("BUILD-NOISE")'])
            self.assertEqual(code,0);self.assertNotIn('BUILD-NOISE',out.getvalue())
            with contextlib.redirect_stdout(io.StringIO()),contextlib.redirect_stderr(io.StringIO()) as err:
                code=progress.run('正在安装',[sys.executable,'-c','import sys;print("bad build");sys.exit(23)'])
            self.assertEqual(code,23);self.assertIn('bad build',err.getvalue())
            for log in Path(temporary).rglob('*.log'):
                self.assertEqual(log.stat().st_mode&0o777,0o600)

    @unittest.skipUnless(os.name=='posix','Private installation log on Linux')
    def test_noisy_install_failure_keeps_bounded_log_and_latest_error(self):
        command=[sys.executable,'-c','import sys;sys.stdout.buffer.write(b"x"*(5<<20)+b"\\nFINAL-ERROR\\n");sys.exit(7)']
        with tempfile.TemporaryDirectory() as temporary,patch('tempfile.gettempdir',return_value=temporary),contextlib.redirect_stdout(io.StringIO()),contextlib.redirect_stderr(io.StringIO()) as err:
            self.assertEqual(progress.run('正在安装',command),7)
            self.assertIn('FINAL-ERROR',err.getvalue())
            logs=list(Path(temporary).rglob('*.log'));self.assertEqual(len(logs),1)
            self.assertLessEqual(logs[0].stat().st_size,2<<20)
            self.assertIn(b'FINAL-ERROR',logs[0].read_bytes())

    @unittest.skipUnless(os.name=='posix','Linux ownership and atomic file modes')
    def test_core_cache_avoids_redownload_and_rejects_corruption(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);data=root/'data';lib=root/'lib';cache=root/'cache'
            (data/'bin').mkdir(parents=True);lib.mkdir()
            payload=io.BytesIO()
            with zipfile.ZipFile(payload,'w') as archive:archive.writestr('xray',b'\x7fELFfixture')
            raw=payload.getvalue();digest=hashlib.sha256(raw).hexdigest()
            record={'repo':'XTLS/Xray-core','version':'fixture','name':'fixture.zip','member':'xray','bytes':len(raw),'sha256':digest}
            c.atomic(lib/'CORE_ASSETS.json',json.dumps({'xray-amd64':record}).encode())
            def response(*args,**kwargs):
                stream=io.BytesIO(raw);stream.geturl=lambda:'https://github.com/fixture';return stream
            with patch.object(c,'DATA',data),patch.object(c,'LIB',lib),patch.dict(os.environ,{'MIKU_CORE_CACHE':str(cache)}),patch('urllib.request.urlopen',side_effect=response) as download:
                first=c.core_download('xray','amd64');second=c.core_download('xray','amd64')
                self.assertEqual(first,second);self.assertEqual(download.call_count,1)
                (cache/digest).write_bytes(b'x'*len(raw))
                c.core_download('xray','amd64');self.assertEqual(download.call_count,2)
                self.assertEqual((data/'bin/xray').read_bytes(),b'\x7fELFfixture')
            (cache/digest).write_bytes(b'x'*len(raw))
            api='https://api.github.com/repos/XTLS/Xray-core/releases/assets/123'
            metadata={'tag_name':'fixture','assets':[{'name':'fixture.zip','size':len(raw),'state':'uploaded','url':api}]}
            def fallback(request,**kwargs):
                if request.full_url.startswith('https://github.com/'):raise OSError('gateway unavailable')
                content=raw if request.full_url==api else json.dumps(metadata).encode()
                stream=io.BytesIO(content);stream.geturl=lambda:request.full_url;return stream
            with patch.object(c,'DATA',data),patch.object(c,'LIB',lib),patch.dict(os.environ,{'MIKU_CORE_CACHE':str(cache)}),patch('urllib.request.urlopen',side_effect=fallback) as download,patch('time.sleep'):
                self.assertEqual(c.core_download('xray','amd64'),first)
                self.assertEqual(download.call_count,4)
                self.assertEqual(download.call_args.args[0].full_url,api)
                self.assertEqual((data/'bin/xray').read_bytes(),b'\x7fELFfixture')


if __name__=='__main__':unittest.main()
