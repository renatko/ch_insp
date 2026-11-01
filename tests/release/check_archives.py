#!/usr/bin/env python3
"""Validate release archive contents, executable formats and checksums."""

import hashlib
from pathlib import Path
import platform
import struct
import subprocess
import sys
import tarfile
import tempfile
import zipfile


TARGETS = [(os_name, arch) for os_name in ("linux", "darwin", "windows") for arch in ("amd64", "arm64")]
ROOT = Path(__file__).resolve().parents[2]


def verify_binary(data, os_name, arch):
    if os_name == "linux":
        assert data[:5] == b"\x7fELF\x02", "not a 64-bit ELF binary"
        assert struct.unpack_from("<H", data, 18)[0] == {"amd64": 62, "arm64": 183}[arch], "wrong ELF architecture"
    elif os_name == "darwin":
        assert data[:4] == b"\xcf\xfa\xed\xfe", "not a 64-bit Mach-O binary"
        assert struct.unpack_from("<I", data, 4)[0] == {"amd64": 0x1000007, "arm64": 0x100000C}[arch], "wrong Mach-O architecture"
    else:
        assert data[:2] == b"MZ", "not a PE binary"
        offset = struct.unpack_from("<I", data, 0x3C)[0]
        assert data[offset:offset+4] == b"PE\0\0", "invalid PE signature"
        assert struct.unpack_from("<H", data, offset+4)[0] == {"amd64": 0x8664, "arm64": 0xAA64}[arch], "wrong PE architecture"


def main():
    directory = Path(sys.argv[1])
    version = (sys.argv[2] if len(sys.argv) > 2 else "0.0.0").removeprefix("v")
    checksums = {}
    for line in (directory / "SHA256SUMS").read_text().splitlines():
        digest, name = line.split("  ", 1)
        checksums[name] = digest
    assert len(checksums) == len(TARGETS) + 4, "unexpected number of checksums"
    for os_name, arch in TARGETS:
        binary = "ch_insp.exe" if os_name == "windows" else "ch_insp"
        name = f"ch_insp_{os_name}_{arch}.{'zip' if os_name == 'windows' else 'tar.gz'}"
        path = directory / name
        assert hashlib.sha256(path.read_bytes()).hexdigest() == checksums[name], f"checksum mismatch: {name}"
        if os_name == "windows":
            with zipfile.ZipFile(path) as archive:
                names = archive.namelist()
                data, license_text = archive.read(binary), archive.read("LICENSE")
        else:
            with tarfile.open(path, "r:gz") as archive:
                names = archive.getnames()
                data = archive.extractfile(binary).read()
                license_text = archive.extractfile("LICENSE").read()
        assert license_text == (ROOT / "LICENSE").read_bytes(), f"project license mismatch: {name}"
        assert "README.md" in names and "THIRD_PARTY_LICENSES/README.txt" in names, f"documentation missing: {name}"
        for document in ("docs/usage.md", "docs/metrics.md", "docs/lineage.md", "docs/demo.md", "docs/development.md", "docs/assets/demo.gif"):
            assert document in names, f"missing {document}: {name}"
        assert "THIRD_PARTY_LICENSES/go/LICENSE" in names, f"Go license missing: {name}"
        assert any(n.startswith("THIRD_PARTY_LICENSES/modules/") and "/LICENSE" in n for n in names), f"dependency licenses missing: {name}"
        if os_name == "windows":
            upstream = "THIRD_PARTY_LICENSES/modules/github.com/mattn/go-localereader@v0.0.1/"
            assert upstream + "README.md" in names and upstream + "LICENSE-MIT.txt" in names, f"upstream MIT notice missing: {name}"
        verify_binary(data, os_name, arch)
        current_os = {"Linux": "linux", "Darwin": "darwin", "Windows": "windows"}.get(platform.system())
        current_arch = {"x86_64": "amd64", "AMD64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine())
        if (os_name, arch) == (current_os, current_arch):
            with tempfile.TemporaryDirectory(prefix="chinspector-release-test-") as temporary:
                executable = Path(temporary) / binary
                executable.write_bytes(data)
                executable.chmod(0o755)
                subprocess.run([str(executable), "--help"], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        print(f"Verified {name}")

    for arch in ("amd64", "arm64"):
        for fmt in ("deb", "rpm"):
            name = f"ch_insp_{version}_{arch}.{fmt}"
            path = directory / name
            assert hashlib.sha256(path.read_bytes()).hexdigest() == checksums[name], f"checksum mismatch: {name}"
            if platform.system() == "Linux":
                with tempfile.TemporaryDirectory(prefix="chinspector-package-test-") as temporary:
                    if fmt == "deb":
                        metadata = subprocess.check_output(["dpkg-deb", "-f", str(path), "Package", "Version", "Architecture"], text=True)
                        assert f"Architecture: {arch}" in metadata, metadata
                        subprocess.run(["dpkg-deb", "-x", str(path), temporary], check=True)
                    else:
                        metadata = subprocess.check_output(["rpm", "-qp", "--queryformat", "%{NAME} %{VERSION} %{ARCH}", str(path)], text=True)
                        assert metadata.split()[-1] == {"amd64": "x86_64", "arm64": "aarch64"}[arch], metadata
                        cpio = subprocess.check_output(["rpm2cpio", str(path)])
                        subprocess.run(["cpio", "-id", "--quiet"], input=cpio, cwd=temporary, check=True)
                    verify_binary((Path(temporary) / "usr/bin/ch_insp").read_bytes(), "linux", arch)
                    assert (Path(temporary) / "usr/share/doc/ch-insp/LICENSE").read_bytes() == (ROOT / "LICENSE").read_bytes()
            print(f"Verified {name}")


if __name__ == "__main__":
    main()
