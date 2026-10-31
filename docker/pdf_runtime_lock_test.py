#!/usr/bin/env python3
"""Offline checks for the Dockerfile's native renderer inputs."""

import argparse
import datetime
import hashlib
import pathlib
import re
import subprocess
import sys
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
KEYS = {
    "LOCK_FORMAT", "PLATFORM", "BUILD_IMAGE", "RUNTIME_IMAGE", "GO_VERSION",
    "CHROMIUM_VERSION", "APT_SOURCES", "APT_RELEASE_SHA256", "APT_PACKAGES",
    "DEBIAN_PACKAGE_SET", "DEBIAN_PACKAGE_SET_SHA256", "SYSTEM_FONT_SHA256",
    "WRITER_URL", "WRITER_SHA256", "WRITER_VERSION", "CI_FISH_PACKAGES",
}
VERSION = r"[0-9][A-Za-z0-9.+:~_-]*"
PACKAGE = r"[a-z0-9][a-z0-9+.-]*(?::amd64)?"
SHA256 = r"[0-9a-f]{64}"


def require(condition, message):
    if not condition:
        raise ValueError(message)


def parse_lock(text):
    text = "\n".join(line for line in text.splitlines()
                     if not line.startswith("#"))
    values = {}
    offset = 0
    for match in re.finditer(r"([A-Z][A-Z_0-9]*)='([^']*)'", text):
        require(not text[offset:match.start()].strip(), "nonliteral lock syntax")
        key, value = match.groups()
        require(key not in values, "duplicate lock key: " + key)
        require(not any(c in value for c in "$`\\"), "executable lock value")
        values[key] = value
        offset = match.end()
    require(not text[offset:].strip(), "nonliteral trailing lock syntax")
    require(set(values) == KEYS, "missing or unknown lock keys")
    return values


def validate(values, dockerfile):
    require(values["LOCK_FORMAT"] == "1", "unsupported lock format")
    require(values["PLATFORM"] == "linux/amd64", "unsupported native platform")
    for key, repository in [("BUILD_IMAGE", "golang"), ("RUNTIME_IMAGE", "debian")]:
        require(re.fullmatch(repository + r":[A-Za-z0-9.-]+@sha256:" + SHA256,
                             values[key]), "base needs an official exact digest")
    require(re.fullmatch(r"\d+\.\d+\.\d+", values["GO_VERSION"]), "Go patch version missing")
    require(values["BUILD_IMAGE"].startswith("golang:" + values["GO_VERSION"] + "-bookworm@"),
            "Go toolchain and base disagree")
    expected_from = [
        "FROM --platform=linux/amd64 " + values["BUILD_IMAGE"] + " AS build",
        "FROM --platform=linux/amd64 " + values["RUNTIME_IMAGE"],
    ]
    require(re.findall(r"^FROM .+$", dockerfile, re.M) == expected_from,
            "Dockerfile bases differ from lock")

    release_files = set()
    suites = set()
    for source in values["APT_SOURCES"].splitlines():
        match = re.fullmatch(
            r"deb \[check-valid-until=no signed-by=/usr/share/keyrings/debian-archive-keyring.gpg\] "
            r"https://snapshot.debian.org/archive/(debian|debian-security)/(\d{8}T\d{6}Z)/ "
            r"(bookworm|bookworm-updates|bookworm-security) main", source)
        require(match, "apt source must be dated, signed and HTTPS")
        archive, timestamp, suite = match.groups()
        datetime.datetime.strptime(timestamp, "%Y%m%dT%H%M%SZ")
        require((archive == "debian-security") == (suite == "bookworm-security"),
                "wrong security archive")
        require(suite not in suites, "duplicate apt suite")
        suites.add(suite)
        release_files.add("snapshot.debian.org_archive_" + archive + "_" + timestamp
                          + "_dists_" + suite + "_InRelease")
    require(suites == {"bookworm", "bookworm-updates", "bookworm-security"},
            "incomplete apt source set")
    release_hashes = {}
    for line in values["APT_RELEASE_SHA256"].splitlines():
        match = re.fullmatch("(" + SHA256 + r")  (\S+)", line)
        require(match, "bad InRelease checksum record")
        digest, filename = match.groups()
        require(filename not in release_hashes, "duplicate release checksum")
        release_hashes[filename] = digest
    require(set(release_hashes) == release_files, "snapshot/checksum paths disagree")

    package_set = {}
    lines = values["DEBIAN_PACKAGE_SET"].splitlines()
    require(lines == sorted(lines), "package set must be canonical")
    for line in lines:
        match = re.fullmatch("(" + PACKAGE + ")\t(" + VERSION + ")", line)
        require(match, "invalid package/version record")
        name, version = match.groups()
        require(name not in package_set, "duplicate installed package")
        package_set[name] = version
    digest = hashlib.sha256((values["DEBIAN_PACKAGE_SET"] + "\n").encode()).hexdigest()
    require(digest == values["DEBIAN_PACKAGE_SET_SHA256"], "package-set checksum mismatch")
    direct = {}
    for item in values["APT_PACKAGES"].splitlines():
        match = re.fullmatch("(" + PACKAGE + ")=(" + VERSION + ")", item)
        require(match, "apt package requires an exact version")
        name, version = match.groups()
        require(name not in direct, "duplicate direct package")
        require(package_set.get(name) == version, "direct package disagrees with closure")
        direct[name] = version
    required = {"chromium", "chromium-common", "fonts-noto-core", "fonts-dejavu-core",
                "fonts-opensymbol", "fontconfig", "libfontconfig1:amd64",
                "libfreetype6:amd64", "libharfbuzz0b:amd64", "curl", "ca-certificates",
                "libreoffice-writer", "poppler-utils", "qpdf", "util-linux"}
    require(set(direct) == required, "native direct dependency set changed")
    browser = re.fullmatch(r"Chromium (\d+\.\d+\.\d+\.\d+) built on Debian GNU/Linux 12 \(bookworm\)",
                           values["CHROMIUM_VERSION"])
    require(browser and direct["chromium"].startswith(browser[1] + "-"),
            "browser assertion disagrees with package")
    require(direct["chromium"] == direct["chromium-common"], "Chromium packages disagree")
    ci_fish = {}
    for item in values["CI_FISH_PACKAGES"].splitlines():
        match = re.fullmatch(r"(fish|fish-common)=(" + VERSION + ")", item)
        require(match and match[1] not in ci_fish, "invalid CI-only fish pin")
        ci_fish[match[1]] = match[2]
    require(set(ci_fish) == {"fish", "fish-common"}
            and len(set(ci_fish.values())) == 1, "CI fish packages disagree")
    require("CI_FISH_PACKAGES" not in dockerfile, "test shell leaked into runtime")

    fonts = set()
    for line in values["SYSTEM_FONT_SHA256"].splitlines():
        match = re.fullmatch(SHA256 + r"  (/usr/share/fonts/[A-Za-z0-9/_.-]+)", line)
        require(match, "invalid font checksum/path")
        path = pathlib.PurePosixPath(match[1])
        require(".." not in path.parts and "bookdocx" not in path.parts,
                "system-font assertion crosses its boundary")
        require(path.name not in fonts, "duplicate font name")
        fonts.add(path.name)
    require({"NotoSerifBengali-Regular.ttf", "NotoSerifBengali-Bold.ttf",
             "NotoSerifDevanagari-Regular.ttf", "NotoSerifDevanagari-Bold.ttf",
             "DejaVuSans.ttf", "DejaVuSansMono.ttf", "opens___.ttf"} <= fonts,
            "missing shaping or generic fallback font")
    require(re.fullmatch(SHA256, values["WRITER_SHA256"]), "invalid Writer checksum")
    writer = re.fullmatch(r"LibreOffice (\d+\.\d+\.\d+\.\d+) [a-f0-9]{40}", values["WRITER_VERSION"])
    require(writer, "exact Writer build missing")
    version = writer[1]
    require(values["WRITER_URL"] == "https://downloadarchive.documentfoundation.org/libreoffice/old/"
            + version + "/deb/x86_64/LibreOffice_" + version + "_Linux_x86-64_deb.tar.gz",
            "Writer version/archive disagree")
    require("${tdf_package}_" + version + "-2_amd64.deb" in dockerfile,
            "Writer package selection differs from archive")

    mandatory = [
        "ENV GOTOOLCHAIN=local", "COPY docker/pdf-runtime.lock /usr/local/share/pdf-runtime.lock",
        ". /usr/local/share/pdf-runtime.lock", 'printf \'%s\\n\' "$APT_SOURCES"',
        '/etc/apt/sources.list.d/debian.sources',
        'printf \'%s\\n\' "$APT_RELEASE_SHA256" | sha256sum -c -',
        'apt-get install -y --no-install-recommends $APT_PACKAGES',
        "dpkg-query -W -f='${binary:Package}\\t${Version}\\n'",
        'diff -u /tmp/pdf-runtime.expected /tmp/pdf-runtime.sorted',
        '"$DEBIAN_PACKAGE_SET_SHA256" /tmp/pdf-runtime.sorted | sha256sum -c -',
        'printf \'%s\\n\' "$SYSTEM_FONT_SHA256" | sha256sum -c -',
        'test "$(chromium --version)" = "$CHROMIUM_VERSION"',
        '"$WRITER_URL"', '"$WRITER_SHA256" /tmp/libreoffice.tar.gz | sha256sum -c -',
        'test "$(libreoffice --headless --version)" = "$WRITER_VERSION"',
        'COPY internal/bookdocx/fonts/ /usr/share/fonts/bookdocx/',
        'sha256sum -c SHA256SUMS', '/opt/libreoffice26.2/share/fonts',
    ]
    for fragment in mandatory:
        require(fragment in dockerfile, "missing build assertion: " + fragment)
    for unsafe in ["--allow-unauthenticated", "trusted=yes", "Verify-Peer=false",
                   "apt-get upgrade", "apt-get dist-upgrade", "ARG "]:
        require(unsafe not in dockerfile, "runtime pin bypass: " + unsafe)
    require(dockerfile.count("apt-get update") == 1 and dockerfile.count("apt-get install") == 1,
            "additional mutable apt invocation")


class RuntimeLockTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.text = (ROOT / "docker/pdf-runtime.lock").read_text()
        cls.values = parse_lock(cls.text)
        cls.dockerfile = (ROOT / "Dockerfile").read_text()

    def test_current_lock_and_dockerfile(self):
        validate(self.values, self.dockerfile)

    def test_literal_parser_rejects_shell_code_and_duplicate_keys(self):
        for suffix in ["\nexit 0\n", "\nGO_VERSION='1.26.10'\n"]:
            with self.subTest(suffix=suffix), self.assertRaises(ValueError):
                parse_lock(self.text + suffix)
        with self.assertRaises(ValueError):
            parse_lock(self.text.replace("GO_VERSION='", "GO_VERSION='$(id)", 1))

    def test_lock_drift_is_rejected(self):
        mutations = {
            "mutable base": ("BUILD_IMAGE", "golang:1.26-bookworm"),
            "moving repository": ("APT_SOURCES", self.values["APT_SOURCES"].replace("snapshot.debian.org", "deb.debian.org")),
            "signature bypass": ("APT_SOURCES", self.values["APT_SOURCES"].replace("signed-by=", "trusted=yes ignored=")),
            "wrong release": ("APT_RELEASE_SHA256", self.values["APT_RELEASE_SHA256"].replace("_InRelease", "_Release", 1)),
            "wildcard version": ("APT_PACKAGES", self.values["APT_PACKAGES"].replace("chromium=", "chromium=156.*", 1)),
            "missing version": ("APT_PACKAGES", re.sub(r"chromium=[^\n]+", "chromium", self.values["APT_PACKAGES"])),
            "package closure": ("DEBIAN_PACKAGE_SET", self.values["DEBIAN_PACKAGE_SET"] + "\nunknown\t1.0"),
            "font escape": ("SYSTEM_FONT_SHA256", self.values["SYSTEM_FONT_SHA256"].replace("/fonts/truetype/", "/fonts/../", 1)),
            "Writer mismatch": ("WRITER_URL", self.values["WRITER_URL"].replace("26.2.1.2", "26.2.1.3")),
            "CI fish wildcard": ("CI_FISH_PACKAGES", "fish=3.*\nfish-common=3.*"),
            "CI fish mismatch": ("CI_FISH_PACKAGES", "fish=3.6.0-3.1+deb12u1\nfish-common=3.6.0-3.1"),
        }
        for name, (key, value) in mutations.items():
            with self.subTest(name=name), self.assertRaises(ValueError):
                validate(dict(self.values, **{key: value}), self.dockerfile)

    def test_dockerfile_bypass_is_rejected(self):
        for fragment in ["$APT_RELEASE_SHA256", "$SYSTEM_FONT_SHA256", "$CHROMIUM_VERSION",
                         "$DEBIAN_PACKAGE_SET_SHA256", "GOTOOLCHAIN=local"]:
            with self.subTest(fragment=fragment), self.assertRaises(ValueError):
                validate(self.values, self.dockerfile.replace(fragment, "REMOVED"))
        with self.assertRaises(ValueError):
            validate(self.values, self.dockerfile.replace("@sha256:", "@sha256:0", 1))

    def test_run_shell_syntax_without_executing_build(self):
        joined = self.dockerfile.replace("\\\n", " ")
        commands = re.findall(r"^RUN (.+)$", joined, re.M)
        self.assertTrue(commands)
        for command in commands:
            result = subprocess.run(["/bin/sh", "-n", "-c", command], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--get", choices=["GO_VERSION", "BUILD_IMAGE", "RUNTIME_IMAGE",
                                          "PLATFORM", "CI_FISH_PACKAGES"])
    args = parser.parse_args()
    if args.get:
        values = parse_lock((ROOT / "docker/pdf-runtime.lock").read_text())
        validate(values, (ROOT / "Dockerfile").read_text())
        print(values[args.get])
    else:
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(RuntimeLockTests)
        result = unittest.TextTestRunner(verbosity=2).run(suite)
        if result.wasSuccessful():
            print("PDF_RUNTIME_LOCK_PASS")
        sys.exit(not result.wasSuccessful())
