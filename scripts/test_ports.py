"""Automatic port selection with real IPv4/IPv6 listeners and reserved ports."""
import socket
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import common as c


class PortSelectionTests(unittest.TestCase):
    def test_free_preference_and_reserved_following_port(self):
        with patch.object(c, 'port_available', return_value=True):
            self.assertEqual(c.select_port(18900), 18900)
            self.assertEqual(c.select_port(18900, {18900, 18901}), 18902)
            self.assertEqual(c.select_port(65535, {65535}), 1024)

    def test_running_owned_port_is_reused_but_saved_unowned_port_is_not(self):
        with patch.object(c, 'port_available', side_effect=lambda p: p != 18900):
            self.assertEqual(c.select_port(18900, owned=18900), 18900)
            self.assertEqual(c.select_port(18900), 18901)
            self.assertEqual(c.select_port(18900, {18900}, owned=18900), 18901)

    def test_real_ipv4_listener_is_avoided(self):
        with socket.socket() as listener:
            listener.bind(('0.0.0.0', 0))
            listener.listen()
            port = listener.getsockname()[1]
            selected = c.select_port(port)
            self.assertNotEqual(selected, port)
            self.assertTrue(c.port_available(selected))

    def test_real_ipv6_listener_is_avoided(self):
        try:
            listener = socket.socket(socket.AF_INET6, socket.SOCK_STREAM)
            listener.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
            listener.bind(('::', 0))
        except OSError:
            self.skipTest('IPv6 not available')
        with listener:
            listener.listen()
            self.assertNotEqual(c.select_port(listener.getsockname()[1]), listener.getsockname()[1])

    def test_null_saved_collections_are_empty_not_a_restart_failure(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            for name,field in (('state.json','tunnels'),('native.json','inbounds'),('xray.json','inbounds')):
                (root/name).write_text(json.dumps({field:None}),encoding='utf-8')
            with patch.object(c,'trusted',side_effect=lambda path,**kwargs:Path(path).read_bytes()):
                self.assertEqual(c.reserved_ports(root,include_common=False),set())
                (root/'native.json').write_text(json.dumps({'inbounds':[{'port':19443}]}),encoding='utf-8')
                self.assertEqual(c.reserved_ports(root,include_common=False),{19443})

    def test_invalid_preferences_and_exhaustion(self):
        for port in (0, 80, 65536, '8900', None):
            with self.subTest(port=port), self.assertRaises(RuntimeError):
                c.select_port(port)
        with patch.object(c, 'port_available', return_value=False) as probe:
            with self.assertRaisesRegex(RuntimeError, '未找到可用端口'):
                c.select_port(65535)
            self.assertEqual(probe.call_count, 64512)


if __name__ == '__main__':
    unittest.main()
