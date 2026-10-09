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

if __name__ == "__main__":
    unittest.main()
