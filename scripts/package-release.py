"""Build release assets on a clean checkout; never reads an installed control plane.

The patched engines are reused from hash-pinned, previously published packages.
Harbor binaries and frontend are rebuilt from this checkout. Publishing is handled
by the workflow, with the repository token kept out of archives and logs.
"""
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]
VERSION = "0.16.0"
PREVIOUS = "0.15.0"
REPOSITORY = "czy-code-923/czy-code"
BASE = f"https://github.com/{REPOSITORY}/releases/download/v{VERSION}-distribution/"
OLD_BASE = f"https://github.com/{REPOSITORY}/releases/download/v{PREVIOUS}-distribution/"
PINNED = {
    "starhub-0.15.0-agent-releases.zip": "e3d1ccda2ef0986cb6402d6dbd3279a0c2e54d33384361e80393c4c033aeb788",
    "starhub-v015-update.zip": "3dad3e485ae7a5ae2048029d80619f834ceee08b145fdaf5871b5a2f06ddd8a5",
    "harbor-0.15.0-linux-amd64.zip": "99b4b96e91a5b282e6592e67af80ab7d7a60daab5bff4c1440a61cabfa85ead7",
    "harbor-0.15.0-linux-arm64.zip": "fc06e6d21ccd24ef785abc72f84b3b4c65d54d8ef2d9915e6094a39cc402f320",
    "harbor-0.15.0-windows-amd64.zip": "f41e4290d12120ecf14240e9bf57f46256bac5b81482c2bbefbeb60c02d74f99",
    "harbor-0.15.0-windows-arm64.zip": "a4e5d9de626cadb13847edc8d7f60ee1239bc81cc2df266c886f52058a248de1",
    "sing-box-1.14.4-harbor.1-source.zip": "6fccff19fb6ee1625bfa6c2fe21fe1ff4a668ca172ff27bc381afde85fd18f2c",
    "install-starhub.sh": "785b51660b24d5eb264b973b1a10fb9deec58cfde04894cdd1afd075d55e2ca2",
    "install-starhub.ps1": "4f97ee29138481aa8711f809930de51f3076afb414e52e19663a7dc8d0ee5ee8",
}


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def run(*args, **kwargs):
    return subprocess.run(args, cwd=ROOT, check=True, **kwargs)


def main():
    if os.environ.get("GITHUB_ACTIONS") != "true":
        raise RuntimeError("Run in GitHub Actions from a reviewed checkout")
    out = ROOT / ".release-output"
    cache = ROOT / ".release-cache"
    out.mkdir(exist_ok=False)
    cache.mkdir(exist_ok=False)

    def previous(name):
        path = cache / name
        if not path.exists():
            request = urllib.request.Request(OLD_BASE + name, headers={"User-Agent": "Starhub-release"})
            with urllib.request.urlopen(request, timeout=180) as response, path.open("wb") as target:
                shutil.copyfileobj(response, target)
        if digest(path) != PINNED[name]:
            raise RuntimeError("Previous release checksum mismatch: " + name)
        return path

    targets = ("linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64")
    binaries = {}
    for target in targets:
        goos, arch = target.split("-")
        directory = cache / target
        directory.mkdir()
        extension = ".exe" if goos == "windows" else ""
        env = {**os.environ, "GOOS": goos, "GOARCH": arch, "CGO_ENABLED": "0"}
        for command in ("control", "agent", "agent-cleanup", "gateway", "firewall"):
            run("go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(directory / ("harbor-" + command + extension)), "./cmd/" + command, env=env)
        binaries[target] = directory
        print("Built " + target, flush=True)

    agents = cache / "agents"
    agents.mkdir()
    manifest = {}
    with zipfile.ZipFile(previous("starhub-0.15.0-agent-releases.zip")) as old_agents:
        for target in targets[:2]:
            path = agents / (target + ".zip")
            with zipfile.ZipFile(io.BytesIO(old_agents.read(target + ".zip"))) as old, zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as archive:
                for item in old.infolist():
                    body = (binaries[target] / item.filename).read_bytes() if item.filename in ("harbor-agent", "harbor-agent-cleanup") else old.read(item)
                    archive.writestr(item.filename, body)
            manifest[target] = {"size": path.stat().st_size, "sha256": digest(path)}
    (agents / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    bundle = out / f"starhub-{VERSION}-agent-releases.zip"
    with zipfile.ZipFile(bundle, "w", zipfile.ZIP_STORED) as archive:
        for file in sorted(agents.iterdir()):
            archive.write(file, file.name)
    (ROOT / "scripts/quickstart-distribution.json").write_text(json.dumps({"agent_url": BASE + bundle.name, "agent_size": bundle.stat().st_size, "agent_sha256": digest(bundle)}, indent=2) + "\n", encoding="utf-8")

    api_tests = cache / "api-tests"
    run("go", "test", "-c", "-o", str(api_tests), "./internal/api", env={**os.environ, "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0"})
    update = out / "starhub-v016-update.zip"
    with zipfile.ZipFile(previous("starhub-v015-update.zip")) as old, zipfile.ZipFile(update, "w", zipfile.ZIP_DEFLATED) as archive:
        apply = old.read("apply.py").decode("utf-8").replace("v015", "v016").replace("V015", "V016").replace(PREVIOUS, VERSION)
        archive.writestr("apply.py", apply)
        archive.write(api_tests, "api-tests")
        for name in ("harbor-control", "harbor-agent", "harbor-firewall", "harbor-agent-cleanup"):
            archive.write(binaries["linux-amd64"] / name, name)
        for file in sorted(agents.iterdir()):
            archive.write(file, "agent-releases/" + file.name)
        for file in sorted((ROOT / "web/dist").rglob("*")):
            if file.is_file():
                archive.write(file, file.relative_to(ROOT).as_posix())

    package_checksums = {}
    for target in targets:
        package = out / f"harbor-{VERSION}-{target}.zip"
        with zipfile.ZipFile(previous(f"harbor-{PREVIOUS}-{target}.zip")) as old, zipfile.ZipFile(package, "w", zipfile.ZIP_DEFLATED) as archive:
            for name in old.namelist():
                if name.startswith("patched-engines/"):
                    archive.writestr(name, old.read(name))
            for file in sorted(binaries[target].iterdir()):
                archive.write(file, "bin/" + file.name)
            for directory in ("scripts", "docs", "web/dist", "third_party"):
                for file in sorted((ROOT / directory).rglob("*")):
                    if file.is_file() and "__pycache__" not in file.parts:
                        archive.write(file, file.relative_to(ROOT).as_posix())
            for name in ("README.md", ".env.example"):
                archive.write(ROOT / name, name)
        package_checksums[target] = digest(package)
        print("Packaged " + package.name, flush=True)
    (ROOT / "scripts/distribution-checksums.json").write_text(json.dumps({"version": VERSION, "sha256": package_checksums}, indent=2) + "\n", encoding="utf-8")

    for name in ("install-starhub.sh", "install-starhub.ps1"):
        text = previous(name).read_text(encoding="utf-8-sig")
        for target in targets:
            text = text.replace(PINNED[f"harbor-{PREVIOUS}-{target}.zip"], package_checksums[target])
        text = text.replace(PREVIOUS, VERSION)
        (out / name).write_text(text, encoding="utf-8-sig" if name.endswith(".ps1") else "utf-8", newline="\n")

    # Only tracked project sources. Runtime state, credentials and build output
    # cannot enter the source archive through an untracked local file.
    tracked = run("git", "ls-files", "-z", capture_output=True).stdout.decode().split("\0")
    source = out / f"harbor-{VERSION}-source.zip"
    with zipfile.ZipFile(source, "w", zipfile.ZIP_DEFLATED) as archive:
        for name in sorted(filter(None, tracked)):
            path = Path(name)
            if path.is_absolute() or ".." in path.parts or any(p in {".git", "node_modules", "dist", "state", "bin", ".release-output", ".release-cache", "__pycache__"} for p in path.parts):
                raise ValueError("Unsafe tracked release path: " + name)
            if path.name.startswith(".env") and path.name != ".env.example":
                raise ValueError("Private environment file tracked")
            archive.write(ROOT / name, f"harbor-{VERSION}/" + name)
    engine_source = previous("sing-box-1.14.4-harbor.1-source.zip")
    shutil.copyfile(engine_source, out / engine_source.name)
    assets = sorted(out.iterdir())
    (out / "SHA256SUMS.txt").write_text("".join(digest(file) + "  " + file.name + "\n" for file in assets), encoding="ascii")
    assets.append(out / "SHA256SUMS.txt")
    (cache / "manifest.json").write_text(json.dumps([{"name": file.name, "size": file.stat().st_size, "sha256": digest(file)} for file in assets], indent=2) + "\n", encoding="utf-8")
    print("Verified release assets: " + str(len(assets)), flush=True)


if __name__ == "__main__":
    main()
