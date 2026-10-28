"""Offline tests for the online release installer; no service changes or network."""
import hashlib
import contextlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import types
import unittest
from unittest.mock import patch
import urllib.request

source = (Path(__file__).resolve().parents[1] / 'install-online.sh').read_text(encoding='utf-8')
source = source.split("<<'MIKU_PYTHON'\n", 1)[1].rsplit('\nMIKU_PYTHON', 1)[0]
b = types.ModuleType('miku_bootstrap_fixture')
exec(compile(source, 'install-online.sh:python', 'exec'), b.__dict__)

REQUIRED = ['install.sh', 'scripts/install.py', 'scripts/common.py', 'scripts/maintain.py', 'scripts/miku.sh', 'bin/miku-linux-amd64', 'bin/miku-linux-arm64', 'bin/miku-export-linux-amd64', 'bin/miku-export-linux-arm64', 'CORE_ASSETS.json']

class BootstrapTests(unittest.TestCase):
    def test_docker_option_is_forwarded_to_the_verified_installer(self):
        raw = b'fixture-release'
        checksum = hashlib.sha256(raw).hexdigest()
        with patch.object(b.sys, 'argv', ['install-online.sh', '--docker']), \
             patch.object(b, 'stable_release', return_value={'tag_name': 'v0.2.5'}), \
             patch.object(b, 'asset', side_effect=lambda release, name: name), \
             patch.object(b, 'fetch', side_effect=lambda name, *args: ((checksum + '  miku-gate-vps-v0.2.5.tar.gz\n').encode() if name == 'SHA256SUMS' else raw)), \
             patch.object(b, 'unpack', return_value=Path('/fixture/verified/miku-gate')), \
             patch.object(b.subprocess, 'run') as run, contextlib.redirect_stdout(io.StringIO()):
            b.main()
        self.assertEqual(run.call_args.args[0], ['bash', str(Path('/fixture/verified/miku-gate/install.sh')), '--docker'])
        self.assertNotIn('GITHUB_TOKEN', run.call_args.kwargs['env'])

    def test_docker_rejects_native_quick_flags_before_downloading(self):
        for flags in (['--quick'], ['--no-domain'], ['--background']):
            with self.subTest(flags=flags), patch.object(b.sys, 'argv', ['install-online.sh', '--docker', *flags]), \
                 patch.object(b, 'stable_release') as release, contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                b.main()
            release.assert_not_called()

    def package(self, root, extra=None, corrupt=False, version='0.2.5', bundle_root='miku-gate'):
        path = root / 'package.tar.gz'
        files = {name: ('fixture:' + name).encode() for name in REQUIRED}
        meta = {'product': 'miku-fanout', 'format': 1, 'version': version, 'files': {name: hashlib.sha256(raw).hexdigest() for name, raw in files.items()}}
        if corrupt:
            files['install.sh'] = b'changed after checksum'
        files['BUILD.json'] = json.dumps(meta).encode()
        with tarfile.open(path, 'w:gz') as archive:
            for name, raw in files.items():
                info = tarfile.TarInfo(bundle_root + '/' + name)
                info.mode = 0o755 if name.startswith('bin/') or name.endswith('.sh') else 0o644
                info.size = len(raw)
                archive.addfile(info, io.BytesIO(raw))
            if extra:
                archive.addfile(extra, io.BytesIO(b'x') if extra.isfile() else None)
        return path

    def test_unpack_valid_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result = b.unpack(self.package(root), root / 'verified', 'v0.2.5')
            self.assertEqual((result / 'install.sh').read_bytes(), b'fixture:install.sh')

    def test_pinned_previous_release_root_remains_supported(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result = b.unpack(self.package(root, version='0.2.0', bundle_root='miku-fanout'), root / 'verified', 'v0.2.0')
            self.assertEqual(result.name, 'miku-fanout')
            self.assertEqual((result / 'install.sh').read_bytes(), b'fixture:install.sh')

    def test_corrupt_file_and_wrong_version_rejected(self):
        for options in ({'corrupt': True}, {'version': '0.2.0-dev.41'}):
            with self.subTest(options=options), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                with self.assertRaises(RuntimeError):
                    b.unpack(self.package(root, **options), root / 'verified', 'v0.2.5')

    def test_traversal_symlink_duplicate_and_unsafe_permissions(self):
        for name, kind, mode in [('miku-gate/../../outside', tarfile.REGTYPE, 0o644), ('miku-gate/link', tarfile.SYMTYPE, 0o644), ('miku-gate/install.sh', tarfile.REGTYPE, 0o644), ('miku-gate/unsafe', tarfile.REGTYPE, 0o666)]:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                entry = tarfile.TarInfo(name)
                entry.type, entry.mode, entry.size = kind, mode, 1 if kind == tarfile.REGTYPE else 0
                entry.linkname = '/etc/passwd' if kind == tarfile.SYMTYPE else ''
                with self.assertRaises(RuntimeError):
                    b.unpack(self.package(root, extra=entry), root / 'verified', 'v0.2.5')
                self.assertFalse((root / 'outside').exists())

    def test_release_selection_rejects_drafts_prereleases_and_invalid_tags(self):
        for value in ({'tag_name': 'v0.2.5', 'draft': True}, {'tag_name': 'v0.2.5', 'prerelease': True}, {'tag_name': 'v0.2.0-dev.41'}):
            with patch.object(b, 'fetch', return_value=json.dumps(value).encode()), self.assertRaises(RuntimeError):
                b.stable_release('latest')
        with patch.object(b, 'fetch') as fetch, self.assertRaises(RuntimeError):
            b.stable_release('../unexpected')
        fetch.assert_not_called()
        with patch.object(b, 'fetch', return_value=b'{"tag_name":"v0.2.5"}'):
            self.assertEqual(b.stable_release('v0.2.5')['tag_name'], 'v0.2.5')

    def test_checksum_requires_unique_exact_asset(self):
        digest = 'a' * 64
        self.assertEqual(b.expected_checksum((digest + '  bundle.tar.gz\n').encode(), 'bundle.tar.gz'), digest)
        for raw in (digest + '  other.tar.gz\n', (digest + '  bundle.tar.gz\n') * 2, 'malformed'):
            with self.assertRaises(RuntimeError):
                b.expected_checksum(raw.encode(), 'bundle.tar.gz')

    def test_asset_must_be_unique_uploaded_and_in_same_repository(self):
        valid = {'name': 'bundle.tar.gz', 'state': 'uploaded', 'url': b.API + '/releases/assets/123'}
        self.assertEqual(b.asset({'assets': [valid]}, valid['name']), valid['url'])
        for records in ([], [valid, valid], [{**valid, 'url': 'https://example.com/token'}]):
            with self.assertRaises(RuntimeError):
                b.asset({'assets': records}, valid['name'])

    def test_cross_host_redirect_strips_token(self):
        request = urllib.request.Request(b.API, headers={'Authorization': 'Bearer fixture-only'})
        redirect = b.SafeRedirect().redirect_request(request, None, 302, 'Found', {}, 'https://release-assets.githubusercontent.com/asset')
        self.assertIsNone(redirect.get_header('Authorization'))
        with self.assertRaises(RuntimeError):
            b.SafeRedirect().redirect_request(request, None, 302, 'Found', {}, 'http://example.com/asset')

    def test_token_only_sent_to_api_host(self):
        for url, expected in [(b.API, 'Bearer fixture-only'), ('https://example.com/asset', None)]:
            with patch.dict(b.os.environ, {'GITHUB_TOKEN': 'fixture-only'}), patch.object(b.urllib.request, 'build_opener') as opener:
                opener.return_value.open.return_value.__enter__.return_value.read.return_value = b'fixture'
                self.assertEqual(b.fetch(url, 100), b'fixture')
                request = opener.return_value.open.call_args.args[0]
                self.assertEqual(request.get_header('Authorization'), expected)
        with self.assertRaises(RuntimeError):
            b.fetch('http://example.com', 100)

if __name__ == '__main__':
    unittest.main()
