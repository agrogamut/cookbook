"""Require complete Go results and explicit native execution."""
import argparse
import json
from pathlib import Path

PREFIX = "github.com/madamgy/recipie/"
REQUIRED = {
    "internal/api": ["TestNativeReleaseJourney"],
    "internal/book": ["TestNativeSourceSnapshot", "TestFinalSourceMethodFieldFidelity",
                      "TestFinalSourceNativeBengaliPreference", "TestReaderCopyFooterNativeFonts",
                      "TestReaderFixedTableNativeContinuation", "TestReaderRecipeHalfPageBoundary",
                      "TestTheUnionRemovesTheOrdinaryOutOfBandFill"],
    "internal/bookdocx": ["TestNative", "TestNativeImages", "TestNativeTallRows"],
    "internal/casesource": ["TestFinalSourceActivation"],
    "internal/engine": ["TestFinalSourceSuspectedAllergens"],
    "scripts/release": ["TestReleaseDatabaseInspection"],
}
EXPECTED_SKIPS = {
    (PREFIX+"internal/portal", "TestPortalBrowserServer"): "browser fixture disabled",
    (PREFIX+"internal/api/handlers", "TestEveryOfferedAllergenScreensSomething"): "GAP-017 still open:",
}


def validate(events, command_exit, packages):
    errors, skipped = [], []
    terminal, output, started = {}, {}, set()
    for event in events:
        package, test, action = event.get("Package"), event.get("Test", ""), event.get("Action")
        key = package, test
        if action in ("start", "run"):
            started.add(key)
        if action == "output":
            output[key] = output.get(key, "") + event.get("Output", "")
        if action in ("pass", "fail", "skip"):
            if key in terminal:
                errors.append(f"duplicate terminal event: {key}")
            terminal[key] = action
            if action == "fail":
                errors.append(f"failed: {key}")
    if command_exit != 0:
        errors.append(f"whole command exit: {command_exit}")
    for key in terminal.keys()-started:
        errors.append(f"terminal event without a start: {key}")
    for key in started-terminal.keys():
        errors.append(f"unfinished: {key}")
    for package in packages:
        key = package, ""
        if terminal.get(key) == "skip" and "[no test files]" in output.get(key, ""):
            continue
        if terminal.get(key) != "pass":
            errors.append(f"package did not complete: {package}")
    for package, names in REQUIRED.items():
        for name in names:
            if terminal.get((PREFIX+package, name)) != "pass":
                errors.append(f"required native/source test did not pass: {package}/{name}")
    for key, result in terminal.items():
        if result != "skip" or not key[1]:
            continue
        reason = EXPECTED_SKIPS.get(key)
        if reason is None or reason not in output.get(key, ""):
            errors.append(f"unexpected skip: {key}")
        else:
            skipped.append({"package": key[0], "test": key[1], "reason": reason})
    return {"command_exit": command_exit, "test_pass_events": sum(v == "pass" and bool(k[1]) for k, v in terminal.items()),
            "expected_skips": skipped, "errors": errors}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("events", type=Path)
    parser.add_argument("packages", type=Path)
    parser.add_argument("--exit-code", type=int, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    events = [json.loads(line) for line in args.events.read_text().splitlines() if line.strip()]
    result = validate(events, args.exit_code, args.packages.read_text().splitlines())
    args.output.write_text(json.dumps(result, indent=2)+"\n")
    if result["errors"]:
        raise SystemExit("Go result validation failed; see result artifact")


if __name__ == "__main__":
    main()
