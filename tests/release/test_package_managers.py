import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("package_managers", ROOT / "scripts/package_managers.py")
generator = importlib.util.module_from_spec(spec)
spec.loader.exec_module(generator)


class ManifestTests(unittest.TestCase):
    def test_stable_manifests_contain_the_correct_archives_and_hashes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            sums = root / "SHA256SUMS"
            assets = [(os_name, arch) for os_name in ("darwin", "linux", "windows") for arch in ("amd64", "arm64")]
            hashes = {f"ch_insp_{os_name}_{arch}.{'zip' if os_name == 'windows' else 'tar.gz'}": f"{i:064x}" for i, (os_name, arch) in enumerate(assets, 1)}
            sums.write_text("".join(f"{digest}  {name}\n" for name, digest in hashes.items()))
            for tag in ("1.2.3", "v1.2.3"):
                generator.generate(tag, "owner/project", sums, root)
                formula = (root / "Formula/ch-insp.rb").read_text()
                scoop = json.loads((root / "bucket/ch-insp.json").read_text())
                self.assertEqual(scoop["version"], "1.2.3")
                for name, digest in hashes.items():
                    if "windows" in name:
                        key = "arm64" if "arm64" in name else "64bit"
                        self.assertTrue(scoop["architecture"][key]["url"].endswith(f"/{tag}/{name}"))
                        self.assertEqual(scoop["architecture"][key]["hash"], digest)
                    else:
                        self.assertIn(f"/{tag}/{name}", formula)
                        self.assertIn(digest, formula)
                self.assertIn("/v$version/" if tag.startswith("v") else "/$version/", scoop["autoupdate"]["hash"]["url"])
            with self.assertRaises(ValueError):
                generator.generate("1.2.3-rc.1", "owner/project", sums, root)
            sums.write_text("")
            with self.assertRaises(KeyError):
                generator.generate("1.2.3", "owner/project", sums, root)


if __name__ == "__main__":
    unittest.main()
