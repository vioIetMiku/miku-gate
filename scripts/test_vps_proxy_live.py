import importlib.util
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location("live", Path(__file__).with_name("vps-proxy-live.py"))
live = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(live)

class LiveAcceptanceSafety(unittest.TestCase):
    def test_fixed_relay_targets(self):
        for ip in ("1.1.1.1", "8.8.8.8", "8.8.4.4"):
            self.assertTrue(live.authorized_target(ip, 443))
        for host, port in (("127.0.0.1", 443), ("169.254.169.254", 80), ("8.8.8.8", 53),
                           ("example.com", 443), ("8.8.8.8 ", 443), ("::ffff:8.8.8.8", 443)):
            self.assertFalse(live.authorized_target(host, port))

    def test_limits_fail_closed(self):
        values = {"cpu.max": "100000 100000", "memory.max": str(384 << 20), "memory.swap.max": "0", "pids.max": "64"}
        live.validate_limits(values)
        for key, value in (("cpu.max", "max 100000"), ("cpu.max", "200000 100000"),
                           ("memory.max", "max"), ("memory.max", str((384 << 20) + 1)),
                           ("memory.swap.max", "1"), ("pids.max", "65")):
            with self.subTest(key=key, value=value), self.assertRaises((ValueError, RuntimeError)):
                live.validate_limits({**values, key: value})

    def test_frames_are_bounded_and_truncation_is_not_success(self):
        class Fake:
            def __init__(self, raw):
                self.raw = raw
            def recv(self, count):
                result, self.raw = self.raw[:count], self.raw[count:]
                return result
        self.assertEqual(live.head(Fake(b"HTTP/1.1 200 OK\r\n\r\nbody")), b"HTTP/1.1 200 OK\r\n\r\n")
        with self.assertRaises(ValueError):
            live.head(Fake(b"x" * 50), maximum=20)
        with self.assertRaises(EOFError):
            live.exact(Fake(b"x"), 2)

class SOCKSCoreReplyCompatibility(unittest.TestCase):
    class Fake:
        def __init__(self, data): self.data = data
        def recv(self, n):
            value, self.data = self.data[:n], self.data[n:]
            return value
    def test_core_bound_addresses_are_not_required_to_be_zero(self):
        replies = (b"\x05\x00\x00\x01\x7f\x00\x00\x01\x12\x34",
                   b"\x05\x00\x00\x04" + b"\x00" * 15 + b"\x01\x12\x34",
                   b"\x05\x00\x00\x03\x03abc\x12\x34")
        for reply in replies:
            sock = self.Fake(reply + b"payload")
            live.socks_reply(sock)
            self.assertEqual(sock.data, b"payload")
    def test_rejection_truncation_and_unknown_types_fail_closed(self):
        for reply in (b"\x05\x01\x00\x01" + b"\x00" * 6, b"\x05\x00\x00\x09",
                      b"\x05\x00\x00\x03\x00", b"\x05\x00\x00\x01\x7f"):
            with self.assertRaises((RuntimeError, EOFError)):
                live.socks_reply(self.Fake(reply))

if __name__ == "__main__":
    unittest.main()
