# XBoard 用户限速修复版

本版基于 `wyx2685/V2bX` 的 v0.4.0（`3deccaae00d168fd049f8a8659656664f25ee301`），保留原有 UniProxy 对接方式。当前安装包为 `v0.4.0-hy2-vless-limitfix-menu1`，binary 版本仍为 `v0.4.0-hy2-vless-limitfix`，binary 对应源码提交 `8f319764bbd2045fdc860ef522b296eba81a0ae6`；Release tag 还包含安装器和文档提交，Go 源码一致。

## 一键安装 / 升级

```bash
wget -N https://raw.githubusercontent.com/AronWang001/V2bX/v0.4.0-hy2-vless-limitfix-menu1/install.sh && bash install.sh
```

要求：root、Linux x86_64、正在运行的 systemd、Bash，以及 `tar`、`coreutils`、`util-linux`、`curl` 或 `wget`、CA 证书。Debian/Ubuntu 缺少依赖时可先运行 `apt-get update && apt-get install -y ca-certificates curl tar coreutils util-linux`。其他架构和 OpenRC/普通 Docker 安装暂不提供此安装包。菜单中的配置编辑需 `vi`；IPv6 检测使用 `iproute2`。

脚本固定下载本 Fork 的指定 Release，不使用上游 `latest`。下载包与 binary 各自校验 SHA256，并先运行 `version` 校验版本。`bash install.sh --check` 仅执行下载校验。

| 项目 | 行为 |
| --- | --- |
| binary | `/usr/local/V2bX/V2bX`，通过同目录临时文件原子替换 |
| 主配置 | `/etc/V2bX/config.json`，已有内容不变 |
| systemd | 保留已有服务及 override；非标准 binary 路径停止安装 |
| 管理入口 | `/usr/bin/V2bX`、`/usr/bin/v2bx`，更新固定指向修复版 |
| 备份 | `/var/backups/V2bX-limitfix/<UTC时间>_<PID>/`，目录仅 root 可访问 |
| 升级重启 | 仅重启原本运行的服务，验证 PID、binary 校验和和连续 10 秒存活 |
| 失败回滚 | 恢复旧 binary、管理入口及安装前运行状态；配置本身未被覆盖 |
| 首次安装 | 安装 geo 数据和含三个 Core 的空节点模板，设置开机自启；可用 `V2bX generate` 生成节点配置并启动 |

备份含旧 binary、管理入口、整个 `/etc/V2bX` 和现有 systemd 输出，可能包含私钥或 API 凭据，应留在服务器并保持 root-only 权限。自定义配置路径和外部证书路径不会被更改，需自行保留其备份。脚本不更改 firewall、数据库、Core 选择、用户、证书或面板 speed_limit。

服务存活检查不等于限速测速，也不等于面板显示在线。升级后请检查 `V2bX status`、`V2bX log`、XBoard 节点状态，再用套餐用户重新连接测速。如果现有主配置意外缺失，安装器会拒绝升级，不会用空模板替换正在运行的配置。

## 按 XBoard 套餐限速

节点端应使用：

```json
"LimitConfig": {
  "SpeedLimit": 0,
  "EnableDynamicSpeedLimit": false
}
```

用户限速来自 `/api/v1/server/UniProxy/user` 的 `speed_limit`，单位 Mbps。节点限速为 0 表示不另外设置节点上限；套餐 60 Mbps 等于 7.5 MB/s。限速桶按同用户共享，上下行与并发连接一起消耗额度，短时突发和协议开销会影响瞬时测速。节点配置若仍为 5 Mbps 会继续生效，安装器不自动覆盖。

首次安装的 `/etc/V2bX/config.json` 中 `Nodes` 为空。按自己的面板填入节点，例如原生 Hysteria2：

```json
{
  "Log": { "Level": "info", "Output": "" },
  "Cores": [{ "Type": "hysteria2" }],
  "Nodes": [{
    "Core": "hysteria2",
    "ApiHost": "https://your-xboard.example",
    "ApiKey": "YOUR_NODE_API_KEY",
    "NodeID": 2,
    "NodeType": "hysteria2",
    "Timeout": 30,
    "ListenIP": "0.0.0.0",
    "SendIP": "0.0.0.0",
    "LimitConfig": { "SpeedLimit": 0, "EnableDynamicSpeedLimit": false },
    "CertConfig": {
      "CertMode": "file",
      "CertDomain": "your-node.example",
      "CertFile": "/etc/V2bX/fullchain.cer",
      "KeyFile": "/etc/V2bX/cert.key"
    }
  }]
}
```

VLESS 可将 `Cores[0].Type` 和节点 `Core` 设为 `xray` 或 `sing`，`NodeType` 设为 `vless`，并使用自己面板对应的 NodeID、传输、TLS/Reality 与证书设置。以上仅是结构示例，不能代替实际节点配置。

```bash
V2bX start
V2bX status
V2bX log
V2bX version
```

`V2bX` 无参数打开原版完整 0～17 管理菜单；`V2bX update` 更新固定修复版，`V2bX update_shell` 升级完整修复版管理脚本，`V2bX x25519` 生成密钥，`V2bX generate` 打开原版配置生成向导。安装、更新和维护脚本升级均指向本 Fork，避免覆盖限速修复。其他 binary 命令仍可直接调用 `/usr/local/V2bX/V2bX`。

### 原版管理功能完整保留

管理脚本基于 [wyx2685/V2bX-script](https://github.com/wyx2685/V2bX-script/tree/c532ec57a67d7544c700f3f438c09dffcd0b1313) 的 `V2bX.sh`，按 MPL-2.0 保留来源，原有 30 个函数和全部菜单入口均保留。

| 菜单 | 功能 |
| --- | --- |
| 0 | 修改配置 |
| 1 / 2 / 3 | 安装 / 更新 / 卸载 V2bX |
| 4 / 5 / 6 | 启动 / 停止 / 重启 |
| 7 / 8 | 状态 / 日志 |
| 9 / 10 | 设置 / 取消开机自启 |
| 11 | 原版 BBR 安装入口 |
| 12 / 13 | 版本 / X25519 密钥 |
| 14 | 升级完整修复版维护脚本 |
| 15 | 原版配置生成向导 |
| 16 | 原版放行所有端口入口 |
| 17 | 退出 |

保留原版配置向导的节点添加、证书模式、路由和审计模板；新生成节点显式使用 `SpeedLimit=0`、动态限速关闭。编辑配置、生成配置和确认卸载前备份整个配置目录；维护脚本升级先备份旧脚本，下载和 Bash 语法验证通过后原子替换。菜单 15 会按原版行为重新生成主配置、路由和 Core 模板，需要主动选择。

菜单 2 的版本输入只接受当前已验证的安装包版本或 binary 版本，不安装未知版本。旧的简化菜单版本需重新执行上方新安装命令，不能通过它固定指向旧版的 `update` 自动得到完整菜单。

Linux 隔离测试已验证全部 18 项菜单路由、三个 Core 的配置生成、混合节点、配置备份和管理脚本升级成功/下载失败/语法失败，以及安装器回滚回归。BBR、放行端口、systemd、编辑器和下载全部使用替身测试；没有在生产服务器执行 BBR、修改 firewall 或卸载。

## 修复路径与证据

| 路径 | 原缺陷 | 修复 |
| --- | --- | --- |
| `core/hy2/hook.go` | 原生 HY2 只统计流量，创建的 bucket 未被转发数据使用 | `LogTraffic()` 双向流量消耗共享用户 bucket，用户更新后可重新取得桶 |
| `core/xray/app/dispatcher/countreader.go`、`default.go` | VLESS `DispatchLink()` 上传 CounterReader 只计数 | 上传读取也等待共享 bucket，与下行使用同一额度；保留 EOF 尾数据和读超时 |
| `core/sing/hook.go` | UDP bucket 有返回但包装被注释 | 启用 UDP 包装，原子创建统计器 |
| `common/rate/conn.go` | TCP Read 按缓冲区容量扣额度，UDP 无速率包装 | TCP 按实际字节数等待；UDP 两个方向消耗共享 bucket |

真实 VLESS TCP/UDP 集成测试位于 `core/vless_limit_integration_test.go`，HY2 测试位于 `core/hy2/`，安装器隔离测试位于 `scripts/test-installer.py`。

已完成 HY2/sing/Xray 相关回归、`go vet` 和 Linux amd64 多 Core 编译。真实本地 VLESS 测试用实际 UniProxy 解析、节点上限 0：

| 用户套餐 Mbps | Xray 实测 Mbps | sing 实测 Mbps |
| --- | --- | --- |
| 30 | 29.474 | 29.456 |
| 40 | 39.347 | 39.363 |
| 50 | 49.145 | 49.168 |
| 60 | 58.487 | 58.802 |

另外验证了 60 Mbps 上传、同用户两条并发流合计、TLS 1.3 Vision 上传/下载，约 59 Mbps。原版 Xray 的面板 60 Mbps 上传曾测得约 4671 Mbps，修复后约 58.9 Mbps。本地 TCP 测试环境为 Windows amd64 / Go 1.25.0；Linux splice 快路径未在该环境执行。真实 VLESS UDP echo 证明双向消耗桶，采用测试时钟，不等于持续 UDP Mbps 测速。

原生 HY2 首个修复提交已在一台实际 Linux 节点验证：30/40/50/60 Mbps 用户分别约 29.50/38.93/48.82/58.37 Mbps，60 Mbps 同用户双流合计约 58.70 Mbps。该节点尚未因本次发布替换为含 VLESS 修复的 binary。

未验证实际 VLESS 生产服务器、全部 Reality/WS/gRPC/XHTTP/mux 组合、其他协议和其他架构；没有运行 race detector。不会据此声称全部协议或其他服务器的所有下行不限速根因均已解决。

## 构建与校验

本包使用 Go 1.25.0，`CGO_ENABLED=0`、`GOEXPERIMENT=jsonv2`，启用 `sing xray hysteria2 with_quic with_grpc with_utls with_wireguard with_acme with_gvisor`。

binary SHA256：

```text
cec15e3e799392b99ca3903bcc6c13361f69c897f677a0e3d8807426f55517ef
```

Release 附带 `SHA256SUMS`；安装器内另固定下载包 SHA256。发布使用已验证 binary，Fork 的继承工作流在 Release 事件中跳过多架构和 Docker 自动发布，避免把未验证构建标成修复版。
