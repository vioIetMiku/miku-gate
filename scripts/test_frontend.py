"""Frontend config, release compatibility and real Nginx proxy regression tests."""
import contextlib
import hashlib
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import shutil
import socket
import subprocess
import tempfile
import threading
import time
import unittest
from urllib.parse import parse_qs
from unittest.mock import patch

import frontend as f

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('export_frontend_fixture', ROOT / 'scripts/export-frontend.py')
exporter = importlib.util.module_from_spec(spec)
spec.loader.exec_module(exporter)


def fixture(root):
    (root / 'docker-data').mkdir()
    (root / 'docker-data/settings.json').write_text(json.dumps({'port': 18899, 'public_ip': '203.0.113.10'}))
    (root / 'docker-data/basepath').write_text('private-path\n')
    shutil.copytree(ROOT / 'frontend', root / 'frontend', ignore=shutil.ignore_patterns('.runtime'))


class FrontendConfigTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        fixture(self.root)

    def test_auto_config_uses_saved_backend_and_validates_browser_origin(self):
        config, address = f.configuration(self.root, {})
        self.assertEqual(address, 'http://203.0.113.10:8900/private-path/')
        self.assertIn('proxy_pass http://127.0.0.1:18899/private-path/api/ping;', config)
        self.assertIn('auth_request /_miku_session;', config)
        self.assertIn('proxy_cookie_flags miku_session nosecure;', config)
        self.assertIn('location / { return 404; }', config)
        self.assertNotIn('@@', config)

    def test_https_remote_uses_verified_tls_secure_cookie_and_correct_backend_host(self):
        env = {'MIKU_FRONTEND_ORIGIN': 'https://panel.example.com',
               'MIKU_BACKEND_URL': 'https://10.0.0.10:9443',
               'MIKU_BACKEND_BASE_PATH': '/random-path',
               'MIKU_BACKEND_HOST': 'api.example.com:9443'}
        config, address = f.configuration(self.root, env)
        self.assertEqual(address, 'https://panel.example.com/random-path/')
        self.assertIn('proxy_ssl_verify on;', config)
        self.assertIn('proxy_ssl_name "api.example.com";', config)
        self.assertIn('proxy_set_header Host "api.example.com:9443";', config)
        self.assertIn('proxy_cookie_flags miku_session secure;', config)
        self.assertIn('"https://panel.example.com" "https://api.example.com:9443";', config)
        env['MIKU_BACKEND_BASE_PATH'] = '/'
        config, address = f.configuration(self.root, env)
        self.assertEqual(address, 'https://panel.example.com/')
        self.assertNotIn('location =  {', config)

    def test_missing_remote_configuration_and_config_injection_are_rejected(self):
        cases = [
            {'MIKU_FRONTEND_PORT': '80'}, {'MIKU_FRONTEND_PORT': '18899'},
            {'MIKU_FRONTEND_LISTEN': '0.0.0.0;include evil'},
            {'MIKU_FRONTEND_ORIGIN': 'https://panel.example.com/path'},
            {'MIKU_FRONTEND_ORIGIN': 'https://panel.example.com;'},
            {'MIKU_BACKEND_URL': 'https://api.example.com'},
            {'MIKU_BACKEND_URL': 'https://user:password@api.example.com'},
            {'MIKU_BACKEND_URL': 'https://api.example.com', 'MIKU_BACKEND_BASE_PATH': '../private'},
            {'MIKU_BACKEND_URL': 'https://api.example.com', 'MIKU_BACKEND_BASE_PATH': '/ok', 'MIKU_BACKEND_HOST': 'api.example.com;include'},
        ]
        for env in cases:
            with self.subTest(env=env), self.assertRaises(ValueError):
                f.configuration(self.root, env)

    def test_assets_are_complete_and_tampering_is_detected(self):
        f.verify_assets(self.root)
        index = self.root / 'frontend/public/index.html'
        html = index.read_text()
        self.assertNotIn('<script>', html)
        self.assertNotIn('<style>', html)
        for name in re.findall(r'(?:src|href)="((?:static|assets)/[^"]+)"', html):
            self.assertTrue((index.parent / name).is_file(), name)
        index.write_text('tampered')
        with self.assertRaisesRegex(ValueError, '校验失败'):
            f.verify_assets(self.root)

    def test_export_matches_current_embedded_ui_and_is_reproducible(self):
        shutil.copytree(ROOT / 'src', self.root / 'src')
        with contextlib.redirect_stdout(io.StringIO()):
            exporter.export(self.root)
        values = exporter.constants(self.root / 'src')
        for page, constant in [('index', 'indexHTML'), ('login', 'loginHTML')]:
            path = self.root / 'frontend/public' / (page + '.html')
            html = path.read_text()
            html = re.sub(r'<script src="([^"]+)"></script>',
                          lambda m: '<script>' + (path.parent / m[1]).read_text().strip() + '</script>', html)
            html = re.sub(r'<link rel="stylesheet" href="([^"]+)">',
                          lambda m: '<style>' + (path.parent / m[1]).read_text().strip() + '</style>', html)
            embedded = exporter.render_constant(values, constant)
            normalize = lambda x: re.sub(r'>\s+|\s+<', lambda m: '>' if m[0].startswith('>') else '<', re.sub(r'\s+', ' ', x)).strip()
            self.assertEqual(normalize(html), normalize(embedded))
        before = (self.root / 'frontend/ASSETS.json').read_bytes()
        with contextlib.redirect_stdout(io.StringIO()):
            exporter.export(self.root)
        self.assertEqual((self.root / 'frontend/ASSETS.json').read_bytes(), before)

    @unittest.skipUnless(os.name == "posix", "Linux ownership / permissions test; run in CI")
    def test_frontend_stage_does_not_copy_runtime_or_backend_secrets(self):
        import test_docker
        source, target = self.root, self.root.parent / (self.root.name + '-staged')
        self.addCleanup(lambda: shutil.rmtree(target, ignore_errors=True))
        runtime = source / 'frontend/.runtime'
        runtime.mkdir()
        (runtime / 'nginx.conf').write_text('must-not-copy')
        (source / 'docker-data/password').write_text('must-not-copy')
        with contextlib.redirect_stdout(io.StringIO()):
            test_docker.stager.stage(source, target)
        self.assertFalse((target / 'frontend/.runtime').exists())
        self.assertFalse((target / 'docker-data/password').exists())
        self.assertEqual((target / 'frontend/public').stat().st_mode & 0o777, 0o755)
        self.assertEqual((target / 'frontend/public/static').stat().st_mode & 0o777, 0o755)
        f.verify_assets(target)


@unittest.skipUnless(os.environ.get('MIKU_NGINX'), 'set MIKU_NGINX to run real Nginx integration')
class FrontendProxyTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.root = Path(cls.temp.name)
        fixture(cls.root)
        cls.requests = []

        class Backend(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                self.route()

            def do_POST(self):
                self.route()

            def route(self):
                raw = self.rfile.read(int(self.headers.get('Content-Length', 0)))
                cls.requests.append((self.command, self.path, dict(self.headers), raw))
                cookie = self.headers.get('Cookie', '')
                if self.path == '/private-path/login':
                    valid = parse_qs(raw.decode()).get('password') == ['fixture-password']
                    self.send_response(200 if valid else 401)
                    self.send_header('Content-Type', 'application/json')
                    if valid:
                        self.send_header('Set-Cookie', 'miku_session=fixture-session; Path=/; HttpOnly; SameSite=Strict')
                    self.end_headers()
                    self.wfile.write(json.dumps({'ok': '已登录'} if valid else {'error': '口令不对'}).encode())
                    return
                if self.path == '/private-path/sub?token=fixture-token':
                    self.send_response(200)
                    self.end_headers()
                    self.wfile.write(b'fixture-subscription')
                    return
                if 'miku_session=fixture-session' not in cookie:
                    self.send_response(401)
                    self.send_header('Content-Type', 'application/json')
                    self.end_headers()
                    self.wfile.write(b'{"error":"unauthorized"}')
                    return
                if self.path == '/private-path/api/ping':
                    self.send_response(204)
                    self.end_headers()
                    return
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.end_headers()
                self.wfile.write(json.dumps({'job': 'fixture-job', 'domain': '', 'query': self.path}).encode())

        cls.backend = ThreadingHTTPServer(('127.0.0.1', 0), Backend)
        cls.backend_thread = threading.Thread(target=cls.backend.serve_forever, daemon=True)
        cls.backend_thread.start()
        with socket.socket() as probe:
            probe.bind(('127.0.0.1', 0))
            cls.port = probe.getsockname()[1]
        settings = {'port': cls.backend.server_port, 'public_ip': '127.0.0.1'}
        (cls.root / 'docker-data/settings.json').write_text(json.dumps(settings))
        config, cls.address = f.configuration(cls.root, {'MIKU_FRONTEND_PORT': str(cls.port)})
        mime = cls.root / 'mime.types'
        mime.write_text('types { text/html html; text/css css; application/javascript js; image/png png; image/svg+xml svg; }')
        config = config.replace('/usr/share/nginx/html', str(cls.root / 'frontend/public'))
        config = config.replace('/etc/nginx/mime.types', str(mime)).replace('worker_processes auto;', 'worker_processes 1;')
        config = config.replace('/tmp/', str(cls.root) + '/')
        config = config.replace('/dev/stderr', str(cls.root / 'runtime-error.log'))
        if os.geteuid() == 0:
            # The test fixture directory is private to this root-owned test;
            # production runs the entire container as unprivileged UID 101.
            config = 'user root;\n' + config
        (cls.root / 'nginx.conf').write_text(config)
        (cls.root / 'logs').mkdir()
        cls.log = (cls.root / 'nginx.log').open('w+')
        cls.process = subprocess.Popen([os.environ['MIKU_NGINX'], '-p', str(cls.root), '-c', str(cls.root / 'nginx.conf'), '-e', str(cls.root / 'startup-error.log'), '-g', 'daemon off;'], stdout=cls.log, stderr=cls.log)
        for _ in range(100):
            if cls.process.poll() is not None:
                cls.log.seek(0)
                raise RuntimeError(cls.log.read())
            try:
                status, _, _ = cls.request('/_miku_health')
                if status == 200:
                    break
            except OSError:
                time.sleep(.03)
        else:
            raise RuntimeError('Nginx did not start')

    @classmethod
    def tearDownClass(cls):
        cls.process.terminate()
        cls.process.wait(timeout=10)
        cls.backend.shutdown()
        cls.backend.server_close()
        cls.log.close()
        cls.temp.cleanup()

    @classmethod
    def request(cls, path, method='GET', headers=None, body=None):
        connection = http.client.HTTPConnection('127.0.0.1', cls.port, timeout=5)
        try:
            connection.request(method, path, body=body, headers=headers or {})
            response = connection.getresponse()
            return response.status, dict(response.getheaders()), response.read()
        finally:
            connection.close()

    def test_login_then_authenticated_page_and_background_quick_api(self):
        status, headers, body = self.request('/private-path/')
        self.assertEqual(status, 200)
        self.assertIn('欢迎回来', body.decode())
        self.assertIn('static/login-2.js', body.decode())
        self.assertEqual(headers['Cache-Control'], 'no-store')
        status, _, body = self.request('/private-path/api/jobs')
        self.assertEqual(status, 401)
        self.assertEqual(json.loads(body)['error'], 'unauthorized')
        status, _, _ = self.request('/private-path/login', 'POST',
                                   {'Origin': self.address.split('/private-path')[0], 'Content-Type': 'application/x-www-form-urlencoded'}, 'password=wrong')
        self.assertEqual(status, 401)
        status, headers, _ = self.request('/private-path/login', 'POST',
                                        {'Origin': self.address.split('/private-path')[0], 'Content-Type': 'application/x-www-form-urlencoded'}, 'password=fixture-password')
        self.assertEqual(status, 200)
        self.assertIn('HttpOnly', headers['Set-Cookie'])
        self.assertIn('SameSite=Strict', headers['Set-Cookie'])
        cookie = headers['Set-Cookie'].split(';')[0]
        status, _, body = self.request('/private-path/', headers={'Cookie': cookie})
        self.assertEqual(status, 200)
        self.assertIn('static/index-2.js', body.decode())
        self.assertNotIn('欢迎回来', body.decode())
        status, _, body = self.request('/private-path/api/quick-setup?fixture=1', 'POST',
                                      {'Cookie': cookie, 'Origin': self.address.split('/private-path')[0],
                                       'Content-Type': 'application/json', 'X-Forwarded-Proto': 'https'}, '{"domain":""}')
        self.assertEqual(status, 200)
        self.assertEqual(json.loads(body)['job'], 'fixture-job')
        method, path, forwarded, raw = self.requests[-1]
        self.assertEqual(path, '/private-path/api/quick-setup?fixture=1')
        self.assertEqual(forwarded['Origin'], 'http://127.0.0.1:' + str(self.backend.server_port))
        self.assertEqual(forwarded['X-Forwarded-Proto'], 'http')
        self.assertEqual(raw, b'{"domain":""}')

    def test_foreign_origin_is_rejected_before_backend_even_for_login(self):
        before = len(self.requests)
        status, _, _ = self.request('/private-path/login', 'POST',
                                    {'Origin': 'https://evil.example'}, 'password=fixture-password')
        self.assertEqual(status, 403)
        self.assertEqual(len(self.requests), before)

    def test_docker_binary_update_is_blocked_without_reaching_backend(self):
        before = len(self.requests)
        status, _, body = self.request('/private-path/api/update/apply', 'POST')
        self.assertEqual(status, 409)
        self.assertIn('install-docker.sh update', json.loads(body)['error'])
        self.assertEqual(len(self.requests), before)

    def test_static_assets_are_independent_and_internal_paths_are_hidden(self):
        for path, expected in [('/private-path/static/index-2.js', 200),
                               ('/private-path/assets/flags/jp.svg', 200),
                               ('/wrong-path/', 404), ('/index.html', 404),
                               ('/_miku_session', 404), ('/_miku_login', 404),
                               ('/private-path/static/../../docker-data/password', 404)]:
            with self.subTest(path=path):
                before = len(self.requests)
                status, _, _ = self.request(path)
                self.assertEqual(status, expected)
                self.assertEqual(len(self.requests), before)

    def test_subscription_query_and_prefix_redirect_are_preserved(self):
        status, headers, _ = self.request('/private-path')
        self.assertEqual(status, 307)
        self.assertTrue(headers['Location'].endswith('/private-path/'))
        status, _, body = self.request('/private-path/sub?token=fixture-token')
        self.assertEqual(status, 200)
        self.assertEqual(body, b'fixture-subscription')


if __name__ == '__main__':
    unittest.main()
