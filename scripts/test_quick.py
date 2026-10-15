"""Offline quick-command checks; no installation or VPN process is started."""
import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import urllib.parse

import common as c
import quick


class PanelFixture:
    value = {'public_ip': '203.0.113.10', 'port': 32001}
    path = '/private/'

    def __init__(self, jobs=None):
        self.calls = []
        self.jobs = iter(jobs or [])

    def request(self, method, path, body=None):
        self.calls.append((method, path, body))
        if path == 'api/quick-setup':
            return {'job': 'fixture-job'}
        if path == 'api/sub':
            return {'path': '/private/sub?token=fixture-token', 'url': 'http://127.0.0.1:32001/private/sub?token=fixture-token'}
        if path == 'api/sub/links':
            return {'links': ['vless://fixture-ready@203.0.113.10:30001#Japan']}
        if path == 'api/jobs':
            result = next(self.jobs)
            if isinstance(result, BaseException):
                raise result
            return [result]
        if path.startswith('api/quick-setup/stop?id='):
            return {'ok': 'stopping'}
        raise AssertionError(path)


def job(status='running', ready=1):
    return {'id': 'fixture-job', 'status': status, 'steps': [{'label': '日本', 'status': 'ok' if status == 'done' else 'running', 'detail': '后台连接中'}], 'quick_result': {'ready': ready, 'regions': [{'name': '日本', 'ready': min(ready, 5), 'target': 5}]}}


class QuickTests(unittest.TestCase):
    def test_all_subscription_formats_use_public_origin_and_keep_token(self):
        panel = PanelFixture()
        urls = quick.subscription_urls(panel)
        self.assertEqual(len(urls), 5)
        for address in urls.values():
            parsed = urllib.parse.urlsplit(address)
            self.assertEqual(parsed.netloc, '203.0.113.10:32001')
            self.assertEqual(parsed.path, '/private/sub')
            self.assertEqual(urllib.parse.parse_qs(parsed.query)['token'], ['fixture-token'])
        self.assertEqual(urllib.parse.parse_qs(urllib.parse.urlsplit(urls['Clash / Mihomo']).query)['target'], ['clash'])

    def test_quick_prints_only_one_final_result_without_progress(self):
        panel = PanelFixture([job(), job('done', 11)])
        output = io.StringIO()
        with patch.object(c, 'active', return_value=True), patch.object(c, 'wait_ready'), patch.object(quick.time, 'sleep'), contextlib.redirect_stdout(output):
            self.assertEqual(quick.quick('', panel=panel), 'fixture-job')
        text = output.getvalue()
        self.assertEqual(text.count('订阅地址（'), 1)
        self.assertEqual(text.count('当前可用节点链接：'), 1)
        self.assertEqual(text.count('vless://fixture-ready'), 1)
        self.assertNotIn('后台连接中', text)
        self.assertNotIn('已就绪，可直接导入', text)
        self.assertEqual(sum(path == 'api/sub/links' for _, path, _ in panel.calls), 1)
        self.assertIn('sing-box', text)
        self.assertEqual(panel.calls[0], ('POST', 'api/quick-setup', {'domain': ''}))

    def test_background_never_waits_for_missing_countries(self):
        panel = PanelFixture()
        with patch.object(c, 'active', return_value=True), patch.object(c, 'wait_ready'), contextlib.redirect_stdout(io.StringIO()):
            quick.quick('node.example.com', background=True, panel=panel)
        self.assertFalse(any(path == 'api/jobs' for _, path, _ in panel.calls))
        self.assertEqual(panel.calls[0][2], {'domain': 'node.example.com'})

    def test_ctrl_c_keeps_service_job_running(self):
        panel = PanelFixture([KeyboardInterrupt()])
        output = io.StringIO()
        with patch.object(c, 'active', return_value=True), patch.object(c, 'wait_ready'), contextlib.redirect_stdout(output):
            quick.quick('', panel=panel)
        self.assertFalse(any('stop' in path for _, path, _ in panel.calls))
        self.assertEqual(output.getvalue().count('当前可用节点链接：'), 1)
        self.assertIn('vless://fixture-ready', output.getvalue())

    def test_partial_failure_keeps_copyable_results_without_step_logs(self):
        failed = job('failed', 2)
        failed['steps'] = [{'label': '韩国', 'status': 'failed', 'detail': 'private-process-error'}]
        panel, output = PanelFixture([failed]), io.StringIO()
        with patch.object(c, 'active', return_value=True), patch.object(c, 'wait_ready'), contextlib.redirect_stdout(output):
            with self.assertRaisesRegex(RuntimeError, '已输出可用结果'):
                quick.quick('', panel=panel)
        self.assertIn('vless://fixture-ready', output.getvalue())
        self.assertNotIn('private-process-error', output.getvalue())
        self.assertEqual(output.getvalue().count('订阅地址（'), 1)

    def test_invalid_domain_does_not_start_service_or_submit_job(self):
        panel = PanelFixture()
        with patch.object(c, 'active') as active, self.assertRaises(RuntimeError):
            quick.quick('https://invalid.example.com', panel=panel)
        active.assert_not_called()
        self.assertEqual(panel.calls, [])

    def test_explicit_stop_targets_only_the_running_quick_job_and_exports_links(self):
        panel = PanelFixture([{**job(), 'kind': 'quick-setup'}])
        with contextlib.redirect_stdout(io.StringIO()):
            quick.stop(panel)
        self.assertIn(('POST', 'api/quick-setup/stop?id=fixture-job', None), panel.calls)
        self.assertTrue(any(path == 'api/sub/links' for _, path, _ in panel.calls))
        self.assertFalse(any(path == 'api/quick-setup' for _, path, _ in panel.calls))

    def test_local_api_logs_in_with_cookie_and_accepts_job_arrays(self):
        requests = []
        class Response:
            status = 200
            def __init__(self, path): self.path = path
            def getheader(self, name, default=None):
                return {'X-Miku-Gate': '1', 'Set-Cookie': 'miku_session=fixture-session; HttpOnly; Path=/'}.get(name, default)
            def read(self, _): return json.dumps([] if self.path.endswith('api/jobs') else {'ok': 'yes'}).encode()
        class Connection:
            def request(self, method, path, body, headers):
                self.path = path; requests.append((method, path, body, dict(headers)))
            def getresponse(self): return Response(self.path)
            def close(self): pass
        with tempfile.TemporaryDirectory() as temp, patch.object(c, 'DATA', Path(temp)), patch.object(c, 'settings', return_value=PanelFixture.value), patch.object(quick.http.client, 'HTTPConnection', return_value=Connection()) as connect:
            c.atomic(c.DATA/'basepath', b'private\n'); c.atomic(c.DATA/'password', b'fixture-password\n')
            panel = quick.LocalPanel()
            self.assertEqual(panel.request('GET', 'api/jobs'), [])
        self.assertEqual(connect.call_args.args[:2], ('127.0.0.1', 32001))
        self.assertEqual(requests[0][1], '/private/login')
        self.assertEqual(urllib.parse.parse_qs(requests[0][2].decode())['password'], ['fixture-password'])
        self.assertEqual(requests[1][3]['Cookie'], 'miku_session=fixture-session')
        self.assertEqual(requests[1][3]['Host'], '203.0.113.10:32001')


if __name__ == '__main__':
    unittest.main()
