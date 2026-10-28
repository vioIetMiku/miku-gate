"""Offline Docker lifecycle checks; no daemon, network or host mutation."""
import contextlib
import fcntl
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import common as c

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('miku_container_fixture', ROOT / 'docker/container.py')
d = importlib.util.module_from_spec(spec)
spec.loader.exec_module(d)
stage_spec = importlib.util.spec_from_file_location('miku_docker_stage_fixture', ROOT / 'docker/stage.py')
stager = importlib.util.module_from_spec(stage_spec)
stage_spec.loader.exec_module(stager)


class DockerDataTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.data, self.lib, self.seed = (self.root / name for name in ('data', 'lib', 'seed'))
        self.lib.mkdir()
        (self.lib / 'miku').write_bytes(b'\x7fELFfixture-service')
        (self.lib / 'miku').chmod(0o755)
        (self.seed / 'bin').mkdir(parents=True)
        for name in ('xray', 'sing-box'):
            (self.seed / 'bin' / name).write_bytes(b'fixture-' + name.encode())
            (self.seed / 'bin' / name).chmod(0o755)
        patches = [patch.object(c, 'DATA', self.data), patch.object(c, 'LIB', self.lib),
                   patch.object(d, 'SEED', self.seed), patch.object(c, 'port_available', return_value=True),
                   patch.object(c, 'public_ip', return_value='8.8.8.8'),
                   patch.object(c, 'run', return_value=subprocess.CompletedProcess([], 0, b'Miku Gate 0.2.5\n', b'')),
                   patch.dict(os.environ, {'WEB_PORT': '', 'MIKU_MAX_SLOTS': '0', 'MIKU_PUBLIC_IP': ''})]
        for p in patches:
            p.start()
            self.addCleanup(p.stop)

    def test_restart_keeps_credentials_tunnels_and_ui_port(self):
        args = d.initialize()
        self.assertEqual(args, [str(self.lib / 'miku'), '-dir', str(self.data), '-max', '0'])
        names = ('password', 'basepath', 'bin/xray', 'bin/sing-box')
        initial = {name: (self.data / name).read_bytes() for name in names}
        settings = c.settings()
        settings.update(port=19090, public_host='panel.example.com', sub_token='keep-me')
        c.write_json(self.data / 'settings.json', settings)
        c.atomic(self.data / 'state.json', b'{"tunnels":["keep-me"]}')
        (self.data / 'bin/xray').write_bytes(b'custom-existing-core')
        initial['bin/xray'] = b'custom-existing-core'
        d.initialize()
        self.assertEqual(c.settings(), settings)
        self.assertEqual((self.data / 'state.json').read_bytes(), b'{"tunnels":["keep-me"]}')
        self.assertEqual(initial, {name: (self.data / name).read_bytes() for name in names})
        for name in ('password', 'basepath', 'settings.json', 'install.json'):
            self.assertEqual(stat.S_IMODE((self.data / name).stat().st_mode), 0o600)

    def test_explicit_environment_changes_only_requested_settings(self):
        d.initialize()
        value = c.settings()
        value['public_host'] = 'keep.example.com'
        c.write_json(self.data / 'settings.json', value)
        with patch.dict(os.environ, {'WEB_PORT': '9900', 'MIKU_PUBLIC_IP': '1.1.1.1', 'MIKU_MAX_SLOTS': '8'}):
            self.assertEqual(d.initialize()[-1], '8')
        self.assertEqual(c.settings()['port'], 9900)
        self.assertEqual(c.settings()['public_ip'], '1.1.1.1')
        self.assertEqual(c.settings()['public_host'], 'keep.example.com')

    def test_bad_environment_and_busy_port_do_not_replace_credentials(self):
        d.initialize()
        before = (self.data / 'password').read_bytes()
        settings = (self.data / 'settings.json').read_bytes()
        for environment in ({'WEB_PORT': '80'}, {'WEB_PORT': '65536'}, {'MIKU_MAX_SLOTS': '-1'}, {'MIKU_PUBLIC_IP': '127.0.0.1'}):
            with self.subTest(environment=environment), patch.dict(os.environ, environment), self.assertRaises((RuntimeError, ValueError)):
                d.initialize()
        with patch.object(c, 'port_available', return_value=False), self.assertRaisesRegex(RuntimeError, '已占用'):
            d.initialize()
        self.assertEqual((self.data / 'password').read_bytes(), before)
        self.assertEqual((self.data / 'settings.json').read_bytes(), settings)

    def test_native_install_is_rejected_without_modification(self):
        c.private_dir(self.data)
        c.write_json(self.data / 'install.json', {'product': 'miku-fanout', 'version': '0.2.5'})
        with self.assertRaisesRegex(RuntimeError, '原生安装'):
            d.initialize()
        self.assertFalse((self.data / 'password').exists())

    def test_binary_selection_validates_both_architectures_and_tampering(self):
        directory = self.root / 'binaries'
        directory.mkdir()
        meta = {'product': 'miku-fanout', 'format': 1, 'files': {}}
        for architecture in ('amd64', 'arm64'):
            name = 'miku-linux-' + architecture
            raw = b'\x7fELF' + architecture.encode()
            (directory / name).write_bytes(raw)
            meta['files']['bin/' + name] = hashlib.sha256(raw).hexdigest()
        manifest = self.root / 'BUILD.json'
        manifest.write_text(json.dumps(meta))
        for architecture in ('amd64', 'arm64'):
            target = self.root / ('selected-' + architecture)
            d.select_binary(directory, manifest, architecture, target)
            self.assertEqual(target.read_bytes(), b'\x7fELF' + architecture.encode())
            self.assertEqual(stat.S_IMODE(target.stat().st_mode), 0o755)
        (directory / 'miku-linux-amd64').write_bytes(b'\x7fELFtampered')
        with self.assertRaisesRegex(RuntimeError, 'SHA256'):
            d.select_binary(directory, manifest, 'amd64', self.root / 'rejected')
        self.assertFalse((self.root / 'rejected').exists())
        with self.assertRaises(RuntimeError):
            d.select_binary(directory, manifest, '386', self.root / 'rejected')

    def test_backup_has_distinct_certificate_roots_and_refuses_active_service(self):
        d.initialize()
        cert_etc, cert_lib, backups = (self.root / name for name in ('cert-etc', 'cert-lib', 'backups'))
        for root, raw in ((cert_etc, b'account'), (cert_lib, b'work')):
            root.mkdir()
            (root / 'same-name').write_bytes(raw)
        with patch.object(d, 'BACKUPS', backups), patch.object(d, 'CERT_ETC', cert_etc), patch.object(d, 'CERT_LIB', cert_lib):
            with contextlib.redirect_stdout(io.StringIO()):
                d.snapshot('miku-docker-fixture.tar.gz')
            target = backups / 'miku-docker-fixture.tar.gz'
            self.assertEqual(stat.S_IMODE(target.stat().st_mode), 0o600)
            with tarfile.open(target) as archive:
                self.assertEqual(archive.extractfile('letsencrypt-etc/same-name').read(), b'account')
                self.assertEqual(archive.extractfile('letsencrypt-lib/same-name').read(), b'work')
                self.assertEqual(archive.extractfile('data/password').read(), (self.data / 'password').read_bytes())
            with (self.data / 'lock').open('rb') as lock:
                fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                with self.assertRaises(BlockingIOError):
                    d.snapshot('miku-docker-active.tar.gz')
            self.assertFalse((backups / 'miku-docker-active.tar.gz').exists())

    def test_quick_never_invokes_a_host_init_system(self):
        import quick
        with patch.object(c, 'active', return_value=False), patch.object(c, 'service') as service, patch.object(quick, 'command') as command:
            d.main(['quick', '--no-domain', '--background'])
            self.assertTrue(c.active())
        service.assert_not_called()
        command.assert_called_once_with(['--no-domain', '--background'])


class DockerShellTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        shutil.copyfile(ROOT / 'install-docker.sh', self.root / 'install-docker.sh')
        shutil.copyfile(ROOT / 'compose.yaml', self.root / 'compose.yaml')
        self.fake_bin = self.root / 'fake-bin'
        self.fake_bin.mkdir()
        fake = self.fake_bin / 'docker'
        fake.write_text('''#!/usr/bin/env python3
import json, os, sys
args = sys.argv[1:]
with open(os.environ['DOCKER_FIXTURE_LOG'], 'a') as stream:
    stream.write(json.dumps(args) + '\\n')
if args[:1] == ['info']:
    print('linux' if args[-1] == '{{.OSType}}' else '["name=seccomp"]')
if 'ps' in args and '--status' in args and os.environ.get('DOCKER_FIXTURE_RUNNING') == '1':
    print('fixture-container')
if 'run' in args and os.environ.get('DOCKER_FIXTURE_BACKUP_FAIL') == '1':
    sys.exit(23)
''')
        fake.chmod(0o755)
        self.log = self.root / 'calls.jsonl'
        self.environment = {**os.environ, 'PATH': str(self.fake_bin) + os.pathsep + os.environ['PATH'], 'DOCKER_FIXTURE_LOG': str(self.log)}
        self.environment.pop('MIKU_BINARY_SOURCE', None)

    def run_script(self, *args, **environment):
        return subprocess.run(['bash', str(self.root / 'install-docker.sh'), *args], cwd=self.root, env={**self.environment, **environment}, text=True, capture_output=True)

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()]

    def test_help_and_invalid_arguments_do_not_touch_docker(self):
        self.assertEqual(self.run_script('--help').returncode, 0)
        self.assertEqual(self.run_script('down', '--volumes').returncode, 2)
        self.assertEqual(self.run_script('unknown').returncode, 2)
        self.assertFalse(self.log.exists())

    def test_build_validates_configuration_and_uses_persistent_project_directory(self):
        result = self.run_script('build')
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.calls()
        self.assertIn(['compose', '--project-directory', str(self.root), '-f', str(self.root / 'compose.yaml'), 'config', '--quiet'], calls)
        self.assertEqual(calls[-1][-2:], ['build', '--pull'])

    def test_failed_backup_restores_running_service_and_propagates_failure(self):
        result = self.run_script('backup', DOCKER_FIXTURE_RUNNING='1', DOCKER_FIXTURE_BACKUP_FAIL='1')
        self.assertEqual(result.returncode, 23, result.stderr)
        self.assertEqual(self.calls()[-1][-2:], ['start', 'miku'])
        self.assertNotIn('备份完成', result.stdout)

    def test_backup_keeps_previously_stopped_service_stopped(self):
        result = self.run_script('backup', DOCKER_FIXTURE_RUNNING='0')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any('start' in call for call in self.calls()))
        self.assertIn('备份完成', result.stdout)


class DockerStageTests(unittest.TestCase):
    def test_temporary_source_is_copied_without_secrets_and_existing_data_survives(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source, target = root / 'source', root / 'project'
            source.mkdir()
            target.mkdir()
            for name in ('Dockerfile', 'compose.yaml', 'install-docker.sh', '.env.example'):
                (source / name).write_bytes(b'fixture-project')
            (source / '.env').write_bytes(b'must-not-copy')
            (source / 'docker-data').mkdir()
            (source / 'docker-data/password').write_bytes(b'must-not-copy')
            (target / '.env').write_bytes(b'keep-settings')
            (target / 'docker-data').mkdir()
            (target / 'docker-data/password').write_bytes(b'keep-password')
            with contextlib.redirect_stdout(io.StringIO()):
                stager.stage(source, target)
            shutil.rmtree(source)
            self.assertEqual((target / 'Dockerfile').read_bytes(), b'fixture-project')
            self.assertEqual((target / '.env').read_bytes(), b'keep-settings')
            self.assertEqual((target / 'docker-data/password').read_bytes(), b'keep-password')

    def test_nested_and_symlink_destinations_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'source'
            source.mkdir()
            actual = root / 'actual'
            actual.mkdir()
            link = root / 'linked'
            link.symlink_to(actual, target_is_directory=True)
            for target in (source, source / 'nested', root, link):
                with self.subTest(target=target), self.assertRaises(RuntimeError):
                    stager.stage(source, target)

    def test_packaging_excludes_docker_credentials_and_keeps_deployment_files(self):
        spec = importlib.util.spec_from_file_location('miku_release_fixture', ROOT / 'scripts/build-release.py')
        builder = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(builder)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with patch.object(builder, 'ROOT', root):
                for name in ('.env', 'docker-data/password', 'docker-certs/etc/key', 'docker-backups/private.tar.gz', '.env.local'):
                    path = root / name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_bytes(b'secret')
                    self.assertFalse(builder.source_file(path), name)
                for name in ('Dockerfile', 'compose.yaml', '.env.example', 'docker/container.py'):
                    path = root / name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_bytes(b'project')
                    self.assertTrue(builder.source_file(path), name)


if __name__ == '__main__':
    unittest.main()
