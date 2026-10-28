"""Actual frontend ports, idempotence, configuration validation and rollback."""
import contextlib
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import common as c
import frontend as f
from test_frontend import fixture

REAL_RUN = subprocess.run


class FrontendLifecycleTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        fixture(self.root)
        self.environment, self.calls = {}, []
        self.running, self.container_port = False, 18910
        self.fail_validation = self.fail_start = False
        self.available = lambda port: True
        for mocker in (patch.object(f.subprocess, 'run', side_effect=self.run_command),
                       patch.object(c, 'port_available', side_effect=lambda port: self.available(port))):
            mocker.start()
            self.addCleanup(mocker.stop)

    def run_command(self, args, **kwargs):
        self.calls.append((args, kwargs))
        output = ''
        if args[:2] == ['docker', 'inspect']:
            output = json.dumps([{'State': {'Running': True, 'Health': {'Status': 'healthy'}},
                                  'Config': {'Env': ['MIKU_FRONTEND_PORT=' + str(self.container_port)]}}])
        elif 'config' in args:
            output = json.dumps({'services': {'frontend': {'environment': self.environment}}})
        elif 'ps' in args:
            output = 'fixture-container\n' if self.running else ''
        elif '-t' in args:
            self.assertIn('/etc/miku/candidate-', args[-2])
            if self.fail_validation:
                raise subprocess.CalledProcessError(1, args)
        elif 'up' in args and self.fail_start:
            self.fail_start = False
            raise subprocess.CalledProcessError(1, args)
        return subprocess.CompletedProcess(args, 0, output, '')

    def invoke(self, action):
        with contextlib.redirect_stdout(io.StringIO()) as output:
            f.main([action], self.root)
        return output.getvalue()

    def save_running(self):
        runtime = self.root / 'frontend/.runtime'
        runtime.mkdir()
        config, address = f.configuration(self.root, {'MIKU_FRONTEND_PORT': str(self.container_port)})
        (runtime / 'nginx.conf').write_text(config)
        state = {'format': 1, 'port': self.container_port, 'address': address,
                 'config_sha256': hashlib.sha256(config.encode()).hexdigest()}
        (runtime / 'state.json').write_text(json.dumps(state))
        self.running = True
        return runtime

    def test_first_start_avoids_busy_ports_and_exports_actual_healthcheck_port(self):
        self.available = lambda port: port not in (8900, 8901)
        output = self.invoke('start')
        self.assertEqual(f.read_state(self.root)['port'], 8902)
        self.assertIn(':8902/private-path/', output)
        self.assertIn('listen 0.0.0.0:8902;', (self.root / 'frontend/.runtime/nginx.conf').read_text())
        for args, kwargs in self.calls:
            if 'up' in args or '-t' in args:
                self.assertEqual(kwargs['env']['MIKU_FRONTEND_PORT'], '8902')

    def test_repeated_start_reuses_healthy_owned_port_without_recreation(self):
        runtime = self.save_running()
        self.available = lambda port: port != self.container_port
        before = (runtime / 'nginx.conf').read_bytes()
        self.invoke('start')
        self.assertEqual(f.read_state(self.root)['port'], self.container_port)
        self.assertEqual((runtime / 'nginx.conf').read_bytes(), before)
        self.assertFalse(any('up' in args for args, _ in self.calls))

    def test_stopped_container_does_not_own_saved_busy_port(self):
        self.save_running()
        self.running = False
        self.available = lambda port: port != self.container_port
        self.invoke('start')
        self.assertEqual(f.read_state(self.root)['port'], self.container_port + 1)

    def test_explicit_port_collision_and_local_backend_are_skipped(self):
        self.environment = {'MIKU_FRONTEND_PORT': '18899'}
        self.available = lambda port: port != 18900
        self.invoke('start')
        self.assertEqual(f.read_state(self.root)['port'], 18901)

    def test_restart_refreshes_changed_backend_and_keeps_frontend_port(self):
        runtime = self.save_running()
        self.available = lambda port: port != self.container_port
        settings = self.root / 'docker-data/settings.json'
        value = json.loads(settings.read_text())
        value['port'] = 18898
        settings.write_text(json.dumps(value))
        self.invoke('restart')
        self.assertIn('proxy_pass http://127.0.0.1:18898', (runtime / 'nginx.conf').read_text())
        self.assertEqual(f.read_state(self.root)['port'], self.container_port)
        self.assertEqual(sum('up' in args for args, _ in self.calls), 1)

    def test_info_uses_saved_actual_address_without_docker_or_config_recalculation(self):
        self.save_running()
        self.environment = {'MIKU_FRONTEND_PORT': '19999'}
        self.assertIn(':18910/private-path/', self.invoke('info'))
        self.assertEqual(self.calls, [])

    def test_failed_validation_and_check_preserve_live_config_and_state(self):
        runtime = self.save_running()
        original = {name: (runtime / name).read_bytes() for name in ('nginx.conf', 'state.json')}
        self.environment = {'MIKU_FRONTEND_PORT': '19999'}
        self.fail_validation = True
        with self.assertRaises(subprocess.CalledProcessError):
            self.invoke('restart')
        self.fail_validation = False
        self.invoke('check')
        self.assertEqual(original, {name: (runtime / name).read_bytes() for name in original})
        self.assertFalse(list(runtime.glob('candidate-*')))
        self.assertFalse(any('up' in args for args, _ in self.calls))

    def test_failed_recreation_restores_config_state_and_container_port(self):
        runtime = self.save_running()
        original = {name: (runtime / name).read_bytes() for name in ('nginx.conf', 'state.json')}
        self.environment = {'MIKU_FRONTEND_PORT': '19999'}
        self.fail_start = True
        with self.assertRaises(subprocess.CalledProcessError):
            self.invoke('restart')
        self.assertEqual(original, {name: (runtime / name).read_bytes() for name in original})
        starts = [kwargs['env']['MIKU_FRONTEND_PORT'] for args, kwargs in self.calls if 'up' in args]
        self.assertEqual(starts, ['19999', '18910'])

    def test_invalid_port_does_not_write_runtime_files(self):
        self.environment = {'MIKU_FRONTEND_PORT': '80'}
        with self.assertRaisesRegex(ValueError, '1024'):
            self.invoke('start')
        self.assertFalse((self.root / 'frontend/.runtime').exists())

    def test_stop_and_logs_still_work_with_a_corrupt_runtime_record(self):
        runtime = self.save_running()
        (runtime / 'state.json').write_text('invalid json')
        self.invoke('stop')
        self.invoke('logs')
        self.assertEqual(self.calls[0][0][-1], 'stop')
        self.assertEqual(self.calls[1][0][-3:], ['logs', '--tail=100', '-f'])

    def test_release_asset_command_validates_without_overwriting_independent_ui(self):
        original = (self.root / 'frontend/public/index.html').read_bytes()
        script = Path(__file__).parent / 'export-frontend.py'
        completed = REAL_RUN(['python3', '-B', str(script), '--root', str(self.root)], capture_output=True, text=True)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual((self.root / 'frontend/public/index.html').read_bytes(), original)


if __name__ == '__main__':
    unittest.main()
