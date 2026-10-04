#!/usr/bin/env python3
"""Exercise the installer on Linux in temporary directories with fake systemd.

No production service is touched. Paths and pinned hashes are substituted only
in a private copy; the published installer has no test override switches.
"""
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile


SOURCE = Path(__file__).resolve().parent.parent
INSTALLER = (SOURCE / "install.sh").read_text()
MANAGER = (SOURCE / "scripts/v2bx-manager.sh").read_text()
VERSION = "v0.4.0-hy2-vless-limitfix"
CANDIDATE = f"#!/bin/bash\nprintf '%s\\n' '{VERSION}'\n".encode()
ORIGINAL = b"#!/bin/bash\nprintf 'original\\n'\n"

SYSTEMCTL = r'''#!/usr/bin/env python3
import json, os, pathlib, sys
root = pathlib.Path(os.environ['FIXTURE_ROOT'])
file = root / 'state.json'
s = json.loads(file.read_text())
args = sys.argv[1:]
cmd = args[0]
if cmd == 'show':
    prop = args[args.index('-p')+1]
    values = {'LoadState': s['load'], 'ExecStart': '{ path='+s['exec']+' ; argv[]='+s['exec']+' server ; }',
              'MainPID': '123' if s['active'] else '0', 'NRestarts': '0'}
    print(values[prop])
elif cmd == 'is-active':
    sys.exit(0 if s['active'] else 3)
elif cmd in ('restart', 'start'):
    new = (root / 'usr/local/V2bX/V2bX').read_bytes()
    candidate = b'limitfix' in new
    if s.get('fail') and candidate:
        s['active'] = False
        file.write_text(json.dumps(s))
        sys.exit(1)
    s['active'] = True
    proc = root / 'proc/123/exe'
    proc.parent.mkdir(parents=True, exist_ok=True)
    proc.write_bytes(b'wrong binary' if s.get('wrong_pid') and candidate else new)
elif cmd == 'stop': s['active'] = False
elif cmd == 'daemon-reload':
    s['load'] = 'loaded' if (root / 'etc/systemd/system/V2bX.service').exists() else 'not-found'
elif cmd == 'cat': print('Synthetic service with preserved override')
elif cmd == 'status':
    print('Active: active (running)' if s['active'] else 'Active: inactive (dead)')
    sys.exit(0 if s['active'] else 3)
elif cmd == 'is-enabled': print('enabled')
elif cmd not in ('enable', 'disable'): sys.exit(2)
file.write_text(json.dumps(s))
'''


def write(path, data, mode=0o600):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data if isinstance(data, bytes) else data.encode())
    path.chmod(mode)


def scenario(name, *, fresh=False, active=True, fail=False, wrong_pid=False,
             corrupt=False, arch="x86_64", check=False, custom=False, missing=False):
    with tempfile.TemporaryDirectory(prefix="v2bx-installer-test-") as directory:
        root = Path(directory)
        binary = root / "usr/local/V2bX/V2bX"
        config = root / "etc/V2bX/config.json"
        service = root / "etc/systemd/system/V2bX.service"
        state = root / "state.json"
        fake = root / "fakebin"
        fake.mkdir()
        for path in ("run/systemd/system", "run/lock", "tmp", "usr/bin", "etc/systemd/system"):
            (root / path).mkdir(parents=True)
        original_config = b'{"Nodes":[{"LimitConfig":{"SpeedLimit":0},"ApiKey":"fixture-only"}]}\n'
        if not fresh:
            write(binary, ORIGINAL, 0o755)
            write(config, original_config)
            write(root / "etc/V2bX/cert.key", b"fixture certificate")
            write(root / "usr/bin/V2bX", b"#!/bin/bash\necho old-manager\n", 0o755)
            write(service, "Synthetic original service and overrides\n")
            write(root / "proc/123/exe", ORIGINAL, 0o755)
            if missing:
                config.unlink()
        state.write_text(json.dumps({"load": "not-found" if fresh else "loaded", "active": active and not fresh,
                                     "exec": "/custom/V2bX" if custom else str(binary),
                                     "fail": fail, "wrong_pid": wrong_pid}))
        archive = root / "fixture.tar.gz"
        with tarfile.open(archive, "w:gz") as tar:
            files = {"V2bX": CANDIDATE, "v2bx-manager.sh": MANAGER.encode(),
                     "config.json": (SOURCE / "scripts/config.limitfix.json").read_bytes()}
            files.update({"examples/"+name: b"fixture-geo" for name in ("geoip.dat", "geosite.dat", "geoip.db", "geosite.db")})
            for path, data in files.items():
                info = tarfile.TarInfo(path)
                info.size = len(data)
                info.mode = 0o644
                tar.addfile(info, io.BytesIO(data))
        package_hash = hashlib.sha256(archive.read_bytes()).hexdigest()
        binary_hash = hashlib.sha256(CANDIDATE).hexdigest()
        if corrupt: archive.write_bytes(b"corrupt-download")
        script = INSTALLER
        for path in ("/usr/local/V2bX", "/etc/V2bX", "/usr/bin", "/etc/systemd/system", "/run/systemd/system",
                     "/run/lock", "/tmp/v2bx-limitfix", "/var/backups/V2bX-limitfix", "/proc/"):
            script = script.replace(path, str(root)+path)
        for line in script.splitlines():
            if line.startswith("readonly PACKAGE_SHA256="): script = script.replace(line, f"readonly PACKAGE_SHA256='{package_hash}'")
            if line.startswith("readonly BINARY_SHA256="): script = script.replace(line, f"readonly BINARY_SHA256='{binary_hash}'")
        installer = root / "install.sh"
        write(installer, script)
        write(fake / "systemctl", SYSTEMCTL, 0o755)
        write(fake / "curl", '#!/bin/bash\nwhile (($#)); do if [[ "$1" == -o ]]; then cp "$FIXTURE_ROOT/fixture.tar.gz" "$2"; exit; fi; shift; done\nexit 2\n', 0o755)
        write(fake / "uname", f'#!/bin/bash\nif [[ "$1" == -s ]]; then echo Linux; else echo {arch}; fi\n', 0o755)
        write(fake / "sleep", "#!/bin/bash\nexit 0\n", 0o755)
        write(fake / "readlink", f'#!/bin/bash\nif [[ "$1" == "$FIXTURE_ROOT/proc/123/exe" ]]; then echo "$FIXTURE_ROOT/usr/local/V2bX/V2bX"; else exec {shutil.which("readlink")} "$@"; fi\n', 0o755)
        env = dict(os.environ, PATH=str(fake)+":"+os.environ["PATH"], FIXTURE_ROOT=str(root))
        result = subprocess.run(["bash", str(installer)]+(["--check"] if check else []), env=env,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=30)
        success = not (fail or wrong_pid or corrupt or arch != "x86_64" or custom or missing)
        assert (result.returncode == 0) == success, (name, result.stdout)
        s = json.loads(state.read_text())
        if fresh:
            assert binary.read_bytes() == CANDIDATE
            assert json.loads(config.read_text())["Nodes"] == []
            assert not s["active"]
            assert "--config" in service.read_text()
        else:
            if missing:
                assert not config.exists(), (name, "missing config replaced")
            else:
                assert config.read_bytes() == original_config, (name, "config changed")
            assert (root / "etc/V2bX/cert.key").read_bytes() == b"fixture certificate"
            assert service.read_text() == "Synthetic original service and overrides\n"
            assert binary.read_bytes() == (CANDIDATE if success and not check else ORIGINAL)
            expected_manager = MANAGER.encode() if success and not check else b"#!/bin/bash\necho old-manager\n"
            assert (root / "usr/bin/V2bX").read_bytes() == expected_manager
            assert s["active"] == active, (name, "service state changed")
        backups = list((root / "var/backups/V2bX-limitfix").glob("*"))
        if check or corrupt or custom or missing or arch != "x86_64": assert not backups
        if backups and not fresh:
            assert (backups[0] / "V2bX").read_bytes() == ORIGINAL
            assert (backups[0] / "config/config.json").read_bytes() == original_config
            assert backups[0].stat().st_mode & 0o777 == 0o700
        assert not list((root / "tmp").iterdir()), (name, "temporary download remains")
        assert not list(root.rglob("*.limitfix.*")), (name, "temporary replacement remains")
        print(f"PASS {name}", flush=True)


def manager_update():
    for fail in (False, True):
        with tempfile.TemporaryDirectory(prefix="v2bx-manager-test-") as directory:
            root = Path(directory)
            fake = root / "fakebin"
            temporary = root / "tmp"
            fake.mkdir()
            temporary.mkdir()
            binary = root / 'usr/local/V2bX/V2bX'
            write(binary, ORIGINAL, 0o755)
            (root / 'state.json').write_text(json.dumps({'load':'loaded', 'active':True, 'exec':str(binary)}))
            write(fake / 'systemctl', SYSTEMCTL, 0o755)
            write(root / "update-fixture.sh", "#!/bin/bash\nprintf 'fixture updater executed\\n'\nexit "+("17" if fail else "0")+"\n")
            write(fake / "curl", '#!/bin/bash\nwhile (($#)); do if [[ "$1" == -o ]]; then cp "$FIXTURE_ROOT/update-fixture.sh" "$2"; exit; fi; shift; done\nexit 2\n', 0o755)
            manager = MANAGER.replace('/usr/local/V2bX', str(root)+'/usr/local/V2bX')
            write(root / "manager.sh", manager, 0o755)
            env = dict(os.environ, PATH=str(fake)+":"+os.environ["PATH"], FIXTURE_ROOT=str(root), TMPDIR=str(temporary))
            result = subprocess.run(["bash", str(root / "manager.sh"), "update"], env=env, capture_output=True, text=True, timeout=10)
            assert result.returncode == (17 if fail else 0), (result.stdout, result.stderr)
            assert not list(temporary.iterdir()), "manager updater temporary files remain"
            print("PASS manager update cleanup "+("on failure" if fail else "on success"), flush=True)


def main():
    if os.geteuid() != 0:
        raise SystemExit("Run this filesystem sandbox test as root on Linux.")
    subprocess.run(["bash", "-n", str(SOURCE / "install.sh")], check=True)
    subprocess.run(["bash", "-n", str(SOURCE / "scripts/v2bx-manager.sh")], check=True)
    scenario("upgrade preserves configuration, certificates, service and creates backup")
    scenario("failed restart restores original binary, manager and active state", fail=True)
    scenario("wrong running binary triggers rollback", wrong_pid=True)
    scenario("corrupt download is rejected before stop", corrupt=True)
    scenario("unsupported architecture is rejected before download", arch="aarch64")
    scenario("custom systemd binary path is rejected", custom=True)
    scenario("download-only check preserves original service", check=True)
    scenario("stopped service remains stopped", active=False)
    scenario("fresh installation uses empty configuration and does not start", fresh=True)
    scenario("missing existing configuration is rejected", missing=True)
    manager_update()
    print("PASS Bash syntax, 10 installer and 2 manager sandbox scenarios", flush=True)


if __name__ == "__main__":
    main()
