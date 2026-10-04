#!/usr/bin/env python3
"""Linux filesystem sandbox for the retained upstream management menu.

systemd, networking, editor, binary and all downloads are mocked. No kernel,
firewall, real node configuration or production service is changed.
"""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile

SOURCE = Path(__file__).resolve().parent.parent
MANAGER = (SOURCE / "scripts/v2bx-manager.sh").read_text()
PREFIX = MANAGER.rsplit("if [[ $# > 0 ]]; then", 1)[0]
spec = importlib.util.spec_from_file_location("installer_fixture", SOURCE / "scripts/test-installer.py")
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)
write = fixture.write


class Sandbox:
    def __enter__(self):
        self.directory = tempfile.TemporaryDirectory(prefix="v2bx-full-manager-test-")
        self.root = Path(self.directory.name)
        self.fake = self.root / "fakebin"
        self.fake.mkdir()
        self.temp = self.root / "tmp"
        self.temp.mkdir()
        self.config = self.root / "etc/V2bX/config.json"
        self.binary = self.root / "usr/local/V2bX/V2bX"
        self.manager = self.root / "usr/bin/V2bX"
        self.events = self.root / "events.txt"
        self.events.write_text("")
        self.old_config = b'{"Nodes":[{"ApiKey":"fixture-only"}]}\n'
        write(self.config, self.old_config)
        write(self.root / "etc/V2bX/custom_outbound.json", "[]\n")
        write(self.root / "etc/V2bX/cert.key", "fixture-certificate\n")
        write(self.manager, "original-manager\n", 0o755)
        write(self.binary, "#!/bin/bash\nprintf '%s\\n' \"binary:$1\"\n", 0o755)
        self.payload = self.root / "payload.sh"
        write(self.payload, "#!/bin/bash\necho payload-executed\nexit 0\n")
        write(self.fake / "systemctl", '#!/bin/bash\nprintf "systemctl:%s\\n" "$*" >> "$FIXTURE_ROOT/events.txt"\ncase "$1" in status) echo "Active: active (running)";; is-enabled) echo enabled;; esac\n', 0o755)
        write(self.fake / "curl", '#!/bin/bash\nprintf "curl:%s\\n" "$*" >> "$FIXTURE_ROOT/events.txt"\n[[ "${FAIL_DOWNLOAD:-0}" == 1 ]] && exit 28\nwhile (($#)); do if [[ "$1" == -o ]]; then cp "$FIXTURE_ROOT/payload.sh" "$2"; exit; fi; shift; done\nexit 2\n', 0o755)
        write(self.fake / "ip", "#!/bin/bash\nexit 0\n", 0o755)
        write(self.fake / "sleep", "#!/bin/bash\nexit 0\n", 0o755)
        write(self.fake / "journalctl", '#!/bin/bash\necho journal-fixture\n', 0o755)
        write(self.fake / "vi", '#!/bin/bash\nprintf "\\n" >> "$1"\n', 0o755)
        for name in ("iptables", "ufw", "setenforce", "netfilter-persistent"):
            write(self.fake / name, '#!/bin/bash\nprintf "network-mock:%s\\n" "$*" >> "$FIXTURE_ROOT/events.txt"\n', 0o755)
        self.env = dict(os.environ, PATH=str(self.fake)+":"+os.environ["PATH"],
                        FIXTURE_ROOT=str(self.root), TMPDIR=str(self.temp), PYTHONDONTWRITEBYTECODE="1")
        return self

    def __exit__(self, *args):
        self.directory.cleanup()

    def translate(self, script):
        for path in ("/usr/local/V2bX", "/etc/V2bX", "/usr/bin/V2bX", "/var/backups/V2bX-limitfix"):
            script = script.replace(path, str(self.root)+path)
        return script

    def run(self, *, function=None, command=None, inputs="", overrides="", expected=0):
        script = PREFIX+"\n"+overrides+"\n"+function+"\n" if function else MANAGER
        target = self.root / "runner.sh"
        write(target, self.translate(script))
        result = subprocess.run(["bash", str(target)]+(command or []), input=inputs, env=self.env,
                                text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=15)
        assert result.returncode == expected, (function, command, result.stdout)
        assert not list(self.temp.iterdir()), "download temporary directory remains"
        return result.stdout

    def backups(self):
        return list((self.root / "var/backups/V2bX-limitfix").glob("*"))


def menus():
    targets = {0: "config", 1: "install", 2: "update", 3: "uninstall", 4: "start", 5: "stop",
               6: "restart", 7: "status", 8: "show_log", 9: "enable", 10: "disable",
               11: "install_bbr", 12: "show_V2bX_version", 13: "generate_x25519_key",
               14: "update_shell", 15: "generate_config_file", 16: "open_ports"}
    for number, function in targets.items():
        with Sandbox() as box:
            overrides = "check_install() { return 0; }\ncheck_uninstall() { return 0; }\nshow_status() { :; }\n"
            for name in targets.values():
                overrides += name+"() { echo SELECTED:"+name+"; exit 0; }\n"
            output = box.run(function="show_menu", inputs=str(number)+"\n", overrides=overrides)
            assert "SELECTED:"+function in output, (number, output)
    with Sandbox() as box:
        output = box.run(command=[], inputs="17\n")
        for label in ("修改配置", "安装 V2bX", "更新 V2bX", "卸载 V2bX", "启动 V2bX", "停止 V2bX",
                      "重启 V2bX", "查看 V2bX 状态", "查看 V2bX 日志", "设置 V2bX 开机自启", "取消 V2bX 开机自启",
                      "一键安装 bbr", "查看 V2bX 版本", "生成 X25519 密钥", "升级 V2bX 维护脚本",
                      "生成 V2bX 配置文件", "放行 VPS 的所有网络端口", "退出脚本"):
            assert label in output, label
    print("PASS all 18 original menu entries and routing", flush=True)


def config_wizard():
    # fixed panel credentials; Xray and sing VLESS without TLS, or native HY2.
    nodes = {"xray": ["1", "33", "2", "n", "n"], "sing": ["2", "33", "2", "n", "n"],
             "hysteria2": ["3", "33", "3", "node.example"]}
    for core, choices in nodes.items():
        with Sandbox() as box:
            answers = ["y", "https://xboard.example", "fixture-api-key", "y"]+choices+["n"]
            box.run(command=["generate"], inputs="\n".join(answers)+"\n")
            data = json.loads(box.config.read_text())
            assert data["Cores"][0]["Type"] == core
            assert data["Nodes"][0]["Core"] == core
            assert data["Nodes"][0]["NodeType"] == ("hysteria2" if core == "hysteria2" else "vless")
            assert data["Nodes"][0]["LimitConfig"] == {"SpeedLimit": 0, "EnableDynamicSpeedLimit": False}
            assert (box.config.parent / "config.json.bak").read_bytes() == box.old_config
            assert (box.backups()[0] / "config/config.json").read_bytes() == box.old_config
            assert (box.backups()[0] / "config/cert.key").read_text() == "fixture-certificate\n"
            for name in ("custom_outbound.json", "route.json", "sing_origin.json"):
                json.loads((box.config.parent / name).read_text())
        print("PASS original config wizard "+core+", XBoard limits and backup", flush=True)
    with Sandbox() as box:
        answers = ["y", "https://xboard.example", "fixture-api-key", "y"]+nodes["xray"]+[""]+nodes["hysteria2"]+["n"]
        box.run(command=["generate"], inputs="\n".join(answers)+"\n")
        data = json.loads(box.config.read_text())
        assert [node["NodeType"] for node in data["Nodes"]] == ["vless", "hysteria2"]
    print("PASS mixed Xray/HY2 nodes do not inherit another node's protocol", flush=True)
    with Sandbox() as box:
        box.run(command=["generate"], inputs="n\n")
        assert box.config.read_bytes() == box.old_config and not box.backups()
    print("PASS canceled generator leaves configuration unchanged", flush=True)


def management():
    for command in ("start", "stop", "restart", "status", "enable", "disable", "log", "version", "x25519"):
        with Sandbox() as box:
            box.run(command=[command])
    print("PASS service, log, version and X25519 CLI with mocked systemd/binary", flush=True)
    with Sandbox() as box:
        box.run(command=["config"])
        assert (box.backups()[0] / "config/config.json").read_bytes() == box.old_config
    print("PASS config editing creates backup", flush=True)
    with Sandbox() as box:
        box.run(command=["uninstall"], inputs="n\n")
        assert box.binary.exists() and box.config.read_bytes() == box.old_config
    print("PASS canceled uninstall preserves node", flush=True)
    with Sandbox() as box:
        box.run(command=["update", "unknown-version"], expected=2)
        assert "curl:" not in box.events.read_text()
    print("PASS unknown version cannot overwrite repair with upstream", flush=True)
    with Sandbox() as box:
        box.binary.unlink()
        box.run(command=["install"])
        assert "AronWang001/V2bX/v0.4.0-hy2-vless-limitfix-menu1/install.sh" in box.events.read_text()
    print("PASS install downloads our fixed installer", flush=True)
    for condition in ("success", "download-failure", "invalid-shell"):
        with Sandbox() as box:
            old = box.manager.read_bytes()
            if condition == "download-failure": box.env["FAIL_DOWNLOAD"] = "1"
            if condition == "invalid-shell": write(box.payload, "#!/bin/bash\nif\n")
            if condition == "success": write(box.payload, MANAGER)
            box.run(command=["update_shell"], expected=0 if condition == "success" else 1)
            assert box.manager.read_bytes() == (MANAGER.encode() if condition == "success" else old)
            if condition == "success":
                assert (box.backups()[0] / "manager").read_bytes() == old
                assert "AronWang001/V2bX/v0.4.0-hy2-vless-limitfix-menu1/scripts/v2bx-manager.sh" in box.events.read_text()
        print("PASS maintenance script upgrade "+condition, flush=True)
    with Sandbox() as box:
        box.run(function="install_bbr")
        assert "ylx2016/Linux-NetSpeed" in box.events.read_text()
    with Sandbox() as box:
        box.run(function="open_ports")
        assert "network-mock:" in box.events.read_text()
    print("PASS BBR and open-ports entry points with downloads/network fully mocked", flush=True)


if __name__ == "__main__":
    if os.geteuid() != 0: raise SystemExit("Run sandbox on Linux as root.")
    menus()
    config_wizard()
    management()
    print("PASS full upstream management sandbox; no production changes", flush=True)
