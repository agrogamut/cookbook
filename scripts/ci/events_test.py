import unittest
from events import EXPECTED_SKIPS, PREFIX, REQUIRED, validate


class EventsTest(unittest.TestCase):
    def complete(self):
        events, packages = [], []
        for package, tests in REQUIRED.items():
            package = PREFIX+package
            packages.append(package)
            events.append(dict(Package=package, Action="start"))
            for test in tests:
                events.extend([dict(Package=package, Test=test, Action="run"), dict(Package=package, Test=test, Action="pass")])
            events.append(dict(Package=package, Action="pass"))
        return events, packages

    def test_complete(self):
        events, packages = self.complete()
        self.assertEqual(validate(events, 0, packages)["errors"], [])

    def test_missing_native_failure_and_interruption(self):
        events, packages = self.complete()
        for mutation in (events[:-1], events[2:], events+[dict(Package=packages[0], Test="Extra", Action="fail")]):
            self.assertTrue(validate(mutation, 0, packages)["errors"])
        self.assertTrue(validate(events, 1, packages)["errors"])

    def test_expected_skips_require_named_reason(self):
        events, packages = self.complete()
        for (package, test), reason in EXPECTED_SKIPS.items():
            skip = dict(Package=package, Test=test, Action="skip")
            self.assertTrue(validate(events+[skip], 0, packages)["errors"])
            evidence = dict(Package=package, Test=test, Action="output", Output=reason)
            start = dict(Package=package, Test=test, Action="run")
            self.assertEqual(validate(events+[start, evidence, skip], 0, packages)["errors"], [])
        self.assertTrue(validate(events+[dict(Package=packages[0], Test="MissingTool", Action="skip")], 0, packages)["errors"])

    def test_no_test_package_is_separate(self):
        events, packages = self.complete()
        package = PREFIX+"cmd/example"
        events.extend([dict(Package=package, Action="start"), dict(Package=package, Action="output", Output="[no test files]"), dict(Package=package, Action="skip")])
        self.assertEqual(validate(events, 0, packages+[package])["errors"], [])


if __name__ == "__main__":
    unittest.main()
