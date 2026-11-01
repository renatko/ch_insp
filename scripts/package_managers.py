#!/usr/bin/env python3
"""Generate a Homebrew formula and Scoop manifest for a published release."""

import argparse
import json
from pathlib import Path
import re
from urllib.parse import quote


def generate(tag, repo, sums, output):
    version = tag.removeprefix("v")
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        raise ValueError("Package managers are updated only for stable semantic versions")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo):
        raise ValueError("Invalid GitHub repository")
    hashes = {}
    for line in sums.read_text().splitlines():
        digest, name = line.split("  ", 1)
        if not re.fullmatch(r"[0-9a-f]{64}", digest) or name in hashes:
            raise ValueError(f"Invalid or duplicate checksum: {name}")
        hashes[name] = digest
    base = f"https://github.com/{repo}/releases/download/{quote(tag, safe='')}"
    homepage = f"https://github.com/{repo}"

    def asset(os_name, arch):
        name = f"ch_insp_{os_name}_{arch}.{'zip' if os_name == 'windows' else 'tar.gz'}"
        return f"{base}/{name}", hashes[name]

    mac_arm, mac_x86 = asset("darwin", "arm64"), asset("darwin", "amd64")
    linux_arm, linux_x86 = asset("linux", "arm64"), asset("linux", "amd64")
    formula = f'''class ChInsp < Formula
  desc "Read-only terminal UI for ClickHouse materialized views"
  homepage "{homepage}"
  version "{version}"
  license "MIT"

  on_macos do
    on_arm do
      url "{mac_arm[0]}"
      sha256 "{mac_arm[1]}"
    end
    on_intel do
      url "{mac_x86[0]}"
      sha256 "{mac_x86[1]}"
    end
  end
  on_linux do
    on_arm do
      url "{linux_arm[0]}"
      sha256 "{linux_arm[1]}"
    end
    on_intel do
      url "{linux_x86[0]}"
      sha256 "{linux_x86[1]}"
    end
  end

  def install
    bin.install "ch_insp"
  end

  test do
    assert_match "Usage: ch_insp", shell_output("#{{bin}}/ch_insp --help 2>&1")
  end
end
'''
    scoop = {
        "version": version,
        "description": "Read-only terminal UI for ClickHouse materialized views",
        "homepage": homepage,
        "license": "MIT",
        "architecture": {},
        "bin": "ch_insp.exe",
        "checkver": {"github": homepage},
        "autoupdate": {"architecture": {}, "hash": {"url": f"https://github.com/{repo}/releases/download/{'v' if tag.startswith('v') else ''}$version/SHA256SUMS"}},
    }
    for key, arch in (("64bit", "amd64"), ("arm64", "arm64")):
        url, digest = asset("windows", arch)
        scoop["architecture"][key] = {"url": url, "hash": digest}
        scoop["autoupdate"]["architecture"][key] = {"url": f"https://github.com/{repo}/releases/download/{'v' if tag.startswith('v') else ''}$version/ch_insp_windows_{arch}.zip"}
    (output / "Formula").mkdir(parents=True, exist_ok=True)
    (output / "bucket").mkdir(parents=True, exist_ok=True)
    (output / "Formula/ch-insp.rb").write_text(formula)
    (output / "bucket/ch-insp.json").write_text(json.dumps(scoop, indent=4) + "\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag")
    parser.add_argument("repo")
    parser.add_argument("checksums", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    generate(args.tag, args.repo, args.checksums, args.output)
