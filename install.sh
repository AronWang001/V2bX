#!/usr/bin/env bash
# V2bX XBoard user-speed-limit repair; installation layout follows V2bX-script.
# Existing configuration, certificates, service overrides and enablement survive.
set -euo pipefail
umask 077

readonly REPOSITORY='AronWang001/V2bX'
readonly VERSION='v0.4.0-hy2-vless-limitfix'
readonly RELEASE='v0.4.0-hy2-vless-limitfix-menu1'
readonly ASSET='V2bX-linux-amd64.tar.gz'
readonly PACKAGE_SHA256='ee7dc37782a16297f8078b980d6ff026e5328338cfa672aa6045f74b7cd58af4'
readonly BINARY_SHA256='cec15e3e799392b99ca3903bcc6c13361f69c897f677a0e3d8807426f55517ef'
readonly RELEASE_URL="https://github.com/$REPOSITORY/releases/download/$RELEASE/$ASSET"
readonly INSTALL_DIR='/usr/local/V2bX'
readonly CONFIG_DIR='/etc/V2bX'
readonly BINARY="$INSTALL_DIR/V2bX"
readonly MANAGER='/usr/bin/V2bX'
readonly SERVICE_FILE='/etc/systemd/system/V2bX.service'

stage=''
backup=''
transaction=0
committed=0
was_active=0
had_binary=0
had_manager=0
created_service=0
created_link=0
check_only=0

log() { printf '%s\n' "$*"; }
die() { printf '错误：%s\n' "$*" >&2; exit 1; }

atomic_copy() {
    local source=$1 target=$2 mode=$3
    local temporary="${target}.limitfix.$$"
    install -m "$mode" -- "$source" "$temporary"
    mv -fT -- "$temporary" "$target"
}

rollback() {
    log '安装失败，正在恢复安装前的 binary、管理入口和服务状态。' >&2
    timeout 60 systemctl stop V2bX.service || true
    if ((had_binary)); then
        cp -a -- "$backup/V2bX" "$BINARY.limitfix.$$"
        mv -fT -- "$BINARY.limitfix.$$" "$BINARY"
    else
        rm -f -- "$BINARY"
    fi
    if ((had_manager)); then
        cp -a -- "$backup/manager" "$MANAGER.limitfix.$$"
        mv -fT -- "$MANAGER.limitfix.$$" "$MANAGER"
    else
        rm -f -- "$MANAGER"
    fi
    if ((created_link)); then rm -f -- /usr/bin/v2bx; fi
    if ((created_service)); then
        timeout 30 systemctl disable V2bX.service || true
        rm -f -- "$SERVICE_FILE"
        timeout 30 systemctl daemon-reload || true
    fi
    if ((was_active)); then
        if timeout 60 systemctl restart V2bX.service && systemctl is-active --quiet V2bX.service; then
            log "旧服务已恢复。备份：$backup" >&2
        else
            log "旧服务恢复失败，请检查 systemctl status V2bX。备份：$backup" >&2
        fi
    fi
}

cleanup() {
    local status=$?
    trap - EXIT
    set +e
    if ((status != 0 && transaction && !committed)); then rollback; fi
    rm -f -- "$BINARY.limitfix.$$" "$MANAGER.limitfix.$$"
    if [[ -n "$stage" ]]; then rm -rf -- "$stage"; fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

case "${1:-}" in
    '') ;;
    --check) check_only=1 ;;
    --help|-h)
        log '用法：bash install.sh [--check]'
        log '--check：仅下载并验证固定修复版，不替换文件或重启服务。'
        exit 0 ;;
    *) die '此安装器固定使用已验证的修复版本，不接受其他版本参数。' ;;
esac
(($# <= 1)) || die '参数过多。'
((EUID == 0)) || die '请使用 root 用户运行。'
[[ $(uname -s) == Linux ]] || die '仅支持 Linux。'
case "$(uname -m)" in
    x86_64|amd64) ;;
    *) die '当前修复包仅验证 Linux amd64（x86_64），不提供其他架构的默认回退。' ;;
esac

for tool in tar sha256sum timeout flock install mktemp readlink; do
    command -v "$tool" >/dev/null 2>&1 || die "缺少工具：$tool，请先安装 coreutils、tar 和 util-linux。"
done
if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
    die '请先安装 curl 或 wget，以及 ca-certificates。'
fi
if ((!check_only)); then
    command -v systemctl >/dev/null 2>&1 || die '当前版本需要 systemd。'
    [[ -d /run/systemd/system ]] || die 'systemd 没有运行；不支持在普通 Docker/OpenRC 环境中自动安装。'
fi

exec 9>/run/lock/v2bx-limitfix.lock
flock -n 9 || die '另一个限速修复版安装任务正在运行。'
stage=$(mktemp -d /tmp/v2bx-limitfix.XXXXXXXX)

log "下载固定修复版 $RELEASE（binary $VERSION，Linux amd64）..."
if command -v curl >/dev/null 2>&1; then
    curl --fail --location --proto '=https' --proto-redir '=https' \
        --connect-timeout 10 --max-time 300 "$RELEASE_URL" -o "$stage/$ASSET"
else
    timeout 310 wget --https-only --timeout=30 --tries=2 "$RELEASE_URL" -O "$stage/$ASSET"
fi
printf '%s  %s\n' "$PACKAGE_SHA256" "$stage/$ASSET" | sha256sum --check --status \
    || die '下载包 SHA256 校验失败，未进行安装。'
mkdir "$stage/unpacked"
tar -xzf "$stage/$ASSET" -C "$stage/unpacked" --no-same-owner
printf '%s  %s\n' "$BINARY_SHA256" "$stage/unpacked/V2bX" | sha256sum --check --status \
    || die 'binary SHA256 校验失败，未进行安装。'
chmod 755 "$stage/unpacked/V2bX"
version_output=$(timeout 10 "$stage/unpacked/V2bX" version)
[[ "$version_output" == *"$VERSION"* ]] || die '下载的 binary 版本与固定修复版不符。'
bash -n "$stage/unpacked/v2bx-manager.sh"
log '下载包、binary、版本和管理脚本校验通过。'
if ((check_only)); then
    log '检查完成：未修改安装文件，未重启服务。'
    exit 0
fi

[[ ! -L "$INSTALL_DIR" && ! -L "$BINARY" ]] || die '安装目录或 binary 为符号链接，请先检查实际安装路径。'
[[ ! -L "$CONFIG_DIR" ]] || die '配置目录为符号链接，请先检查并备份实际配置路径。'
[[ ! -f "$BINARY" || -f "$CONFIG_DIR/config.json" ]] \
    || die '现有安装的主配置缺失；未生成空模板，请先确认或恢复实际配置。'
[[ ! -d "$MANAGER" ]] || die '管理入口路径被目录占用。'
if [[ -e /usr/bin/v2bx || -L /usr/bin/v2bx ]]; then
    [[ -L /usr/bin/v2bx && $(readlink /usr/bin/v2bx) == "$MANAGER" ]] \
        || die '/usr/bin/v2bx 已被其他文件或链接占用。'
fi
load_state=$(systemctl show V2bX.service -p LoadState --value)
if [[ "$load_state" != 'not-found' ]]; then
    [[ "$load_state" == 'loaded' ]] || die '现有 V2bX service 无法正确加载。'
    service_exec=$(systemctl show V2bX.service -p ExecStart --value)
    [[ "$service_exec" == *"path=$BINARY ;"* ]] \
        || die '现有 systemd 使用其他 binary 路径；未修改，请先确认实际运行实例。'
    [[ -f "$BINARY" ]] || die 'service 存在但标准路径 binary 缺失。'
elif [[ -e "$SERVICE_FILE" || -L "$SERVICE_FILE" ]]; then
    die '存在未被 systemd 加载的服务文件，请先检查。'
fi
if systemctl is-active --quiet V2bX.service; then
    was_active=1
    pid=$(systemctl show V2bX.service -p MainPID --value)
    [[ "$pid" =~ ^[1-9][0-9]*$ ]] || die '无法确认正在运行的 V2bX PID。'
    running_binary=$(readlink "/proc/$pid/exe")
    [[ "$running_binary" == "$BINARY" || "$running_binary" == "$BINARY (deleted)" ]] \
        || die '运行 PID 的 binary 与待替换路径不一致。'
fi

backup="/var/backups/V2bX-limitfix/$(date -u +%Y%m%d_%H%M%S)_$$"
install -d -m 700 /var/backups/V2bX-limitfix "$backup"
if [[ -f "$BINARY" ]]; then had_binary=1; cp -a -- "$BINARY" "$backup/V2bX"; fi
if [[ -e "$MANAGER" || -L "$MANAGER" ]]; then had_manager=1; cp -a -- "$MANAGER" "$backup/manager"; fi
if [[ -d "$CONFIG_DIR" ]]; then cp -a -- "$CONFIG_DIR" "$backup/config"; fi
if [[ "$load_state" == loaded ]]; then
    # May contain environment secrets: the backup is root-only and never printed.
    systemctl cat V2bX.service > "$backup/service.txt"
fi
printf 'version=%s\nwas_active=%s\nbinary_sha256=%s\n' \
    "$VERSION" "$was_active" "$BINARY_SHA256" > "$backup/install.txt"
log "备份完成：$backup"

install -d -m 755 "$INSTALL_DIR"
if [[ ! -d "$CONFIG_DIR" ]]; then install -d -m 700 "$CONFIG_DIR"; fi
transaction=1
if ((was_active)); then timeout 60 systemctl stop V2bX.service; fi
atomic_copy "$stage/unpacked/V2bX" "$BINARY" 755
atomic_copy "$stage/unpacked/v2bx-manager.sh" "$MANAGER" 755
if [[ ! -L /usr/bin/v2bx ]]; then
    ln -s "$MANAGER" /usr/bin/v2bx
    created_link=1
fi

if [[ "$load_state" == 'not-found' ]]; then
    created_service=1
    cat > "$SERVICE_FILE" <<'UNIT'
[Unit]
Description=V2bX Service (XBoard user speed limit repair)
After=network-online.target nss-lookup.target
Wants=network-online.target

[Service]
User=root
Group=root
Type=simple
WorkingDirectory=/usr/local/V2bX
ExecStart=/usr/local/V2bX/V2bX server --config /etc/V2bX/config.json
LimitNOFILE=999999
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
UNIT
    chmod 644 "$SERVICE_FILE"
    timeout 30 systemctl daemon-reload
    timeout 30 systemctl enable V2bX.service
fi

if ((!had_binary)); then
    # Fresh installation only. Never overwrite existing node configuration/assets.
    for asset in geoip.dat geosite.dat geoip.db geosite.db; do
        if [[ ! -e "$CONFIG_DIR/$asset" ]]; then
            install -m 644 "$stage/unpacked/examples/$asset" "$CONFIG_DIR/$asset"
        fi
    done
fi
if [[ ! -e "$CONFIG_DIR/config.json" ]]; then
    install -m 600 "$stage/unpacked/config.json" "$CONFIG_DIR/config.json"
    log '已创建空节点配置模板；请先填写 XBoard 节点信息，再执行 V2bX start。'
    log '模板不会创建本地强制限速，也不会使用虚构的 API 凭据连接面板。'
fi

if ((was_active)); then
    log '重启现有服务，并验证运行 PID、binary 校验和及连续 10 秒的存活状态...'
    timeout 60 systemctl restart V2bX.service
    restarts=$(systemctl show V2bX.service -p NRestarts --value)
    healthy_pid=''
    for ((attempt=0; attempt<5; attempt++)); do
        sleep 2
        systemctl is-active --quiet V2bX.service || die '修复版启动失败。'
        pid=$(systemctl show V2bX.service -p MainPID --value)
        [[ "$pid" =~ ^[1-9][0-9]*$ ]] || die '修复版 PID 无效。'
        [[ -z "$healthy_pid" || "$pid" == "$healthy_pid" ]] || die '验收期间发生了进程重启。'
        healthy_pid=$pid
        printf '%s  %s\n' "$BINARY_SHA256" "/proc/$pid/exe" | sha256sum --check --status \
            || die '运行中的 binary 不是本次修复版。'
        [[ $(systemctl show V2bX.service -p NRestarts --value) == "$restarts" ]] \
            || die '验收期间发生了自动重启。'
    done
    log "运行校验通过，PID=$healthy_pid。"
else
    log '安装前服务未运行，已保留停止状态；配置完成后执行 V2bX start。'
fi
committed=1
log "安装完成：$RELEASE（binary $VERSION）"
log '现有配置未更改；用户限速继续取自 XBoard 的 speed_limit。'
log '若希望只使用面板限速，请在自己的节点配置中确认 SpeedLimit=0、EnableDynamicSpeedLimit=false。'
log '完整菜单：运行 V2bX；配置向导：V2bX generate；更新管理脚本：V2bX update_shell。'
log '管理命令：V2bX status / version / log / restart / update'
log '服务存活检查不代表客户端测速验收；请用套餐用户重新连接后测速。'
