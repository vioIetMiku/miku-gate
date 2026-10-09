"""Offline tests for the Linux VPS guard's fail-closed validation helpers."""

import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("vps_guard", Path(__file__).with_name("vps-lab-guard.py"))
guard = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(guard)


class GuardTests(unittest.TestCase):
    def limits(self, **overrides):
        return {"memory.max": str(384 << 20), "cpu.max": "100000 100000",
                "memory.swap.max": "0", "pids.max": "64", **overrides}

    def test_accepts_actual_384m_single_cpu_budget(self):
        guard.validate_limits(self.limits())
        guard.validate_limits(self.limits(**{"cpu.max": "50000 100000", "memory.max": str(256 << 20)}))

    def test_refuses_unbounded_or_larger_limits(self):
        for key, values in {
            "memory.max": ["max", "0", "-1", str((384 << 20) + 1)],
            "cpu.max": ["max 100000", "200000 100000", "0 100000", "1 0", "100000", "-1 100000"],
            "memory.swap.max": ["max", "1", "-1"],
            "pids.max": ["max", "0", "-1", "65"],
        }.items():
            for value in values:
                with self.subTest(key=key, value=value), self.assertRaises((ValueError, RuntimeError)):
                    guard.validate_limits(self.limits(**{key: value}))

    def test_normalization_only_ignores_expected_volatility(self):
        before = {"route": [{"expires": 99, "dst": "default", "gateway": "192.0.2.1"}],
                  "rule": {"counter": {"packets": 100, "bytes": 8000, "name": "drop"}, "drop": None}}
        after = {"route": [{"expires": 20, "dst": "default", "gateway": "192.0.2.1"}],
                 "rule": {"counter": {"packets": 105, "bytes": 8500, "name": "drop"}, "drop": None}}
        self.assertEqual(guard.normalize(before), guard.normalize(after))
        self.assertEqual(before["route"][0]["expires"], 99)
        self.assertEqual(before["rule"]["counter"]["packets"], 100)
        after["rule"]["accept"] = None
        self.assertNotEqual(guard.normalize(before), guard.normalize(after))
        after["route"][0]["gateway"] = "192.0.2.2"
        self.assertNotEqual(guard.normalize(before), guard.normalize(after))

    def test_digest_is_order_independent_not_value_independent(self):
        self.assertEqual(guard.digest({"a": 1, "b": 2}), guard.digest({"b": 2, "a": 1}))
        self.assertNotEqual(guard.digest({"a": 1}), guard.digest({"a": 2}))

    def test_pass_parser_does_not_count_subtests_or_missing_tests(self):
        output = "--- PASS: TestLinuxLabResourceBudget (0.00s)\n"
        output += "    --- PASS: TestLinuxLabRecordObjectSafety/fifo (0.00s)\n"
        output += "--- SKIP: TestLinuxLabQuarantineAndHostIsolation (0.00s)\n"
        self.assertEqual(guard.passed_tests(output), {"TestLinuxLabResourceBudget"})
        self.assertNotEqual(guard.passed_tests(output), guard.SUITES["netexec"][2])
        self.assertEqual(guard.passed_tests("PASS\n"), set())

    def test_directory_names_cannot_escape_private_workspace(self):
        self.assertIsNotNone(guard.DIRECTORY_RE.fullmatch("/var/tmp/miku-gate-lab-c09d49b-20261009"))
        for path in ("/", "/var/tmp/other", "/var/tmp/miku-gate-lab-abcdef/..",
                     "/var/tmp/miku-gate-lab-abcdef/child", "/var/tmp/miku-gate-lab-ABCDEF",
                     "/var/tmp/miku-gate-lab-abcdef\n", "/var/tmp/miku-gate-lab-$(id)"):
            with self.subTest(path=path):
                self.assertIsNone(guard.DIRECTORY_RE.fullmatch(path))

    def test_cgroup_requires_matching_dedicated_scope(self):
        def read(path):
            if str(path).replace("\\", "/") == "/proc/self/cgroup":
                return "0::/system.slice/miku-vps-ipc-test01.scope\n"
            return self.limits()[path.name]
        with patch.object(Path, "read_text", read):
            _, values = guard.cgroup_scope("ipc")
            self.assertEqual(values, self.limits())
            with self.assertRaises(RuntimeError):
                guard.cgroup_scope("openvpn")

    def test_cgroup_rejects_host_root_traversal_or_v1(self):
        for value in ("0::/\n", "0::/system.slice/ssh.service\n", "1:memory:/somewhere\n",
                      "0::/system.slice/../miku-vps-ipc-test.scope\n", "0:://miku-vps-ipc-test.scope\n"):
            with self.subTest(value=value), patch.object(Path, "read_text", return_value=value):
                with self.assertRaises(RuntimeError):
                    guard.cgroup_scope("ipc")

    def test_fixed_suite_inventory(self):
        self.assertEqual({key: len(value[2]) for key, value in guard.SUITES.items()},
                         {"netexec": 7, "ipc": 4, "openvpn": 1})
        for name, pattern, tests in guard.SUITES.values():
            self.assertNotIn("/", name)
            for test in tests:
                self.assertRegex(test, pattern)


if __name__ == "__main__":
    unittest.main()
