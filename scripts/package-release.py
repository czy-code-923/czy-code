"""Build release assets on a clean checkout; never reads an installed control plane.

Official engines are downloaded by exact upstream tags and verified digests.
Control binaries and frontend are rebuilt from this checkout. Publishing is handled
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
VERSION = "0.17.0"
PREVIOUS = "0.16.0"
REPOSITORY = "czy-code-923/czy-code"
BASE = f"https://github.com/{REPOSITORY}/releases/download/v{VERSION}-distribution/"
OLD_BASE = f"https://github.com/{REPOSITORY}/releases/download/v{PREVIOUS}-distribution/"
PINNED = {'starhub-v016-update.zip': '2898a911ca2a80087956b991dbbeef2efe31d758cb67c0361814e02449985f20', 'install-starhub.sh': 'eeed37c5b3b1f2339abf8f0d459e78fe1d64947fd31025ad525111d91a0ca8de', 'install-starhub.ps1': 'a5682253cb66c98933549c5ba8dcc29985fa0ae8ff02ef29d679d18fcf3e3f53', 'harbor-0.16.0-linux-amd64.zip': 'ea12c4eded69bee9cab40a46b7d67cfa8fe637566858f6bfe07c33ff44515a20', 'harbor-0.16.0-linux-arm64.zip': '7464a7512818482ba550139f104f6dc6ba63edb67189db54e04a561045c72849', 'harbor-0.16.0-windows-amd64.zip': '1f05330b4fa504d6a1e924f890e6329050f4447b38bbd55b0a515866a6c6a121', 'harbor-0.16.0-windows-arm64.zip': 'a06115a5b765abfe4af0763b8b64574a369886d63f4a60da8ae5e4b2d6a19675'}
ENGINE_VERSIONS = {"sing-box": "v1.14.4", "xray": "v26.10.21", "frp": "v0.71.1"}


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

    engines = {}
    for target in targets:
        goos, arch = target.split("-")
        directory = cache / ("official-" + target)
        arguments = ["python3", "scripts/fetch-engines.py", "--os", goos, "--arch", arch, "--output", str(directory)]
        for name, version in ENGINE_VERSIONS.items():
            arguments += ["--" + name + "-version", version]
        run(*arguments)
        engines[target] = directory

    agents = cache / "agents"
    agents.mkdir()
    manifest = {}
    for target in targets[:2]:
        path = agents / (target + ".zip")
        with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as archive:
            for name in ("harbor-agent", "harbor-agent-cleanup"):
                archive.write(binaries[target] / name, name)
            for name in ("sing-box", "xray", "frps", "frpc", "LICENSE.sing-box", "sing-box-source.zip"):
                archive.write(engines[target] / name, name)
        manifest[target] = {"size": path.stat().st_size, "sha256": digest(path)}
    (agents / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    bundle = out / f"starhub-{VERSION}-agent-releases.zip"
    with zipfile.ZipFile(bundle, "w", zipfile.ZIP_STORED) as archive:
        for file in sorted(agents.iterdir()):
            archive.write(file, file.name)
    (ROOT / "scripts/quickstart-distribution.json").write_text(json.dumps({"agent_url": BASE + bundle.name, "agent_size": bundle.stat().st_size, "agent_sha256": digest(bundle)}, indent=2) + "\n", encoding="utf-8")

    api_tests = cache / "api-tests"
    run("go", "test", "-c", "-o", str(api_tests), "./internal/api", env={**os.environ, "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0"})
    update = out / "starhub-v017-update.zip"
    with zipfile.ZipFile(previous("starhub-v016-update.zip")) as old, zipfile.ZipFile(update, "w", zipfile.ZIP_DEFLATED) as archive:
        apply = old.read("apply.py").decode("utf-8").replace("v016", "v017").replace("V016", "V017").replace(PREVIOUS, VERSION)
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
        with zipfile.ZipFile(package, "w", zipfile.ZIP_DEFLATED) as archive:
            for file in sorted(engines[target].iterdir()):
                archive.write(file, "official-engines/" + file.name)
            for file in sorted(binaries[target].iterdir()):
                archive.write(file, "bin/" + file.name)
            for directory in ("scripts", "docs", "web/dist"):
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
    shutil.copyfile(engines["linux-amd64"] / "sing-box-source.zip", out / "sing-box-1.14.4-source.zip")
    assets = sorted(out.iterdir())
    (out / "SHA256SUMS.txt").write_text("".join(digest(file) + "  " + file.name + "\n" for file in assets), encoding="ascii")
    assets.append(out / "SHA256SUMS.txt")
    (cache / "manifest.json").write_text(json.dumps([{"name": file.name, "size": file.stat().st_size, "sha256": digest(file)} for file in assets], indent=2) + "\n", encoding="utf-8")
    print("Verified release assets: " + str(len(assets)), flush=True)


if __name__ == "__main__":
    main()
