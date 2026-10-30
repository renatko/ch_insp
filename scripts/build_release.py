#!/usr/bin/env python3
"""Build release archives and include notices for the linked Go dependencies."""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import zipfile


ROOT = Path(__file__).resolve().parents[1]
TARGETS = [(os_name, arch) for os_name in ("linux", "darwin", "windows") for arch in ("amd64", "arm64")]


def objects(text):
    decoder = json.JSONDecoder()
    position = 0
    while position < len(text):
        while position < len(text) and text[position].isspace():
            position += 1
        if position == len(text):
            break
        obj, position = decoder.raw_decode(text, position)
        yield obj


def notices(directory):
    prefixes = ("LICENSE", "COPYING", "NOTICE", "PATENTS")
    return sorted(p for p in directory.iterdir() if p.is_file() and p.name.upper().startswith(prefixes))


def copy_notices(source, destination):
    destination.mkdir(parents=True, exist_ok=True)
    for item in notices(source):
        shutil.copyfile(item, destination / item.name)


def collect_licenses(stage, env):
    result = subprocess.run(["go", "list", "-mod=readonly", "-deps", "-json", "."], cwd=ROOT, env=env, check=True, capture_output=True, text=True)
    packages = list(objects(result.stdout))
    modules = {}
    for package in packages:
        module = package.get("Module")
        if module and not module.get("Main"):
            modules[module["Path"]] = module

    output = stage / "THIRD_PARTY_LICENSES"
    manifest = ["Licenses and notices for Go and the modules linked into this build.", ""]
    for name, module in sorted(modules.items()):
        source = Path(module["Dir"])
        version = module.get("Version", "unknown")
        destination = output / "modules" / (name + "@" + version)
        files = notices(source)
        if not any(f.name.upper().startswith(("LICENSE", "COPYING")) for f in files):
            # This specific upstream version declares MIT and its author in its
            # README, but does not ship a separate license file.
            readme = source / "README.md"
            if (name, version) != ("github.com/mattn/go-localereader", "v0.0.1") or not readme.is_file() or "## License\n\nMIT" not in readme.read_text():
                raise RuntimeError(f"No license file found for {name}@{version}")
            destination.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(readme, destination / "README.md")
            mit = (ROOT / "LICENSE").read_text().split("Permission is hereby granted", 1)[1]
            (destination / "LICENSE-MIT.txt").write_text("MIT license text; upstream declares MIT and its author in README.md.\n\nPermission is hereby granted" + mit)
            manifest.append(f"{name}@{version}: upstream license declaration in README.md")
        else:
            copy_notices(source, destination)
            manifest.append(f"{name}@{version}")

    goroot = Path(subprocess.check_output(["go", "env", "GOROOT"], cwd=ROOT, env=env, text=True).strip())
    copy_notices(goroot, output / "go")
    vendor_root = goroot / "src" / "vendor"
    vendor_notices = set()
    for package in packages:
        if not package.get("Standard") or not package.get("ImportPath", "").startswith("vendor/"):
            continue
        directory = Path(package["Dir"])
        while directory != vendor_root and vendor_root in directory.parents:
            if any(p.name.upper().startswith(("LICENSE", "COPYING")) for p in notices(directory)):
                vendor_notices.add(directory)
                break
            directory = directory.parent
        else:
            raise RuntimeError(f"No vendor license found for {package['ImportPath']}")
    for source in sorted(vendor_notices):
        copy_notices(source, output / "go-vendor" / source.relative_to(vendor_root))
    (output / "README.txt").write_text("\n".join(manifest) + "\n")


def main():
    output = Path(sys.argv[1] if len(sys.argv) > 1 else "dist").resolve()
    output.mkdir(parents=True, exist_ok=True)
    archives = [f"ch_insp_{os_name}_{arch}.{'zip' if os_name == 'windows' else 'tar.gz'}" for os_name, arch in TARGETS]
    for name in archives + ["SHA256SUMS"]:
        if (output / name).exists():
            raise FileExistsError(f"Refusing to overwrite {output / name}")

    with tempfile.TemporaryDirectory(prefix="chinspector-release-") as temporary:
        for (os_name, arch), archive in zip(TARGETS, archives):
            print(f"Building {os_name}/{arch}", flush=True)
            env = dict(os.environ, GOOS=os_name, GOARCH=arch, CGO_ENABLED="0")
            stage = Path(temporary) / f"{os_name}-{arch}"
            stage.mkdir()
            binary = "ch_insp.exe" if os_name == "windows" else "ch_insp"
            subprocess.run(["go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", str(stage / binary), "."], cwd=ROOT, env=env, check=True)
            shutil.copyfile(ROOT / "LICENSE", stage / "LICENSE")
            shutil.copyfile(ROOT / "README.md", stage / "README.md")
            shutil.copytree(ROOT / "docs", stage / "docs")
            collect_licenses(stage, env)
            if os_name == "windows":
                with zipfile.ZipFile(output / archive, "w", zipfile.ZIP_DEFLATED) as zipped:
                    for path in sorted(stage.rglob("*")):
                        if path.is_file():
                            zipped.write(path, path.relative_to(stage))
            else:
                with tarfile.open(output / archive, "w:gz") as tar:
                    for path in sorted(stage.iterdir()):
                        tar.add(path, arcname=path.name)

    with (output / "SHA256SUMS").open("w") as checksums:
        for name in archives:
            digest = hashlib.sha256((output / name).read_bytes()).hexdigest()
            checksums.write(f"{digest}  {name}\n")
    print(f"Release archives created in {output}")


if __name__ == "__main__":
    main()
