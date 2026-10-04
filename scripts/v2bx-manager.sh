#!/usr/bin/env bash
set -euo pipefail

# This entry point deliberately keeps updates on the verified limitfix release.
readonly INSTALL_URL='https://raw.githubusercontent.com/AronWang001/V2bX/v0.4.0-hy2-vless-limitfix/install.sh'
readonly BINARY='/usr/local/V2bX/V2bX'

update() {
    update_stage=$(mktemp -d)
    trap 'rm -rf -- "$update_stage"' EXIT
    if command -v curl >/dev/null 2>&1; then
        curl --fail --location --proto '=https' --proto-redir '=https' \
            --connect-timeout 10 --max-time 120 "$INSTALL_URL" -o "$update_stage/install.sh"
    else
        wget --https-only --timeout=30 --tries=2 "$INSTALL_URL" -O "$update_stage/install.sh"
    fi
    bash "$update_stage/install.sh"
}

run() {
    case "${1:-help}" in
        start|stop|restart|status|enable|disable)
            systemctl "$1" V2bX.service ;;
        log) journalctl -u V2bX.service -n 100 --no-pager ;;
        version) "$BINARY" version ;;
        update|install) update ;;
        config)
            printf '配置文件：/etc/V2bX/config.json\n编辑后执行 V2bX restart。\n'
            ;;
        help|--help|-h)
            printf '%s\n' \
                'V2bX 限速修复版管理命令：' \
                '  V2bX start / stop / restart / status' \
                '  V2bX enable / disable / log / version / config' \
                '  V2bX update     重新安装固定的、已验证的修复版' \
                '  /usr/local/V2bX/V2bX <命令>  直接使用 binary 的其他命令'
            ;;
        *) printf '不支持的管理命令：%s\n执行 V2bX help 查看用法。\n' "$1" >&2; return 2 ;;
    esac
}

if (($#)); then
    run "$@"
else
    printf '%s\n' 'V2bX 限速修复版' \
        '1) 状态  2) 启动  3) 停止  4) 重启  5) 日志  6) 版本  7) 更新  0) 退出'
    read -r -p '请选择：' choice
    case "$choice" in
        1) run status ;; 2) run start ;; 3) run stop ;; 4) run restart ;;
        5) run log ;; 6) run version ;; 7) run update ;; 0) exit 0 ;;
        *) exit 2 ;;
    esac
fi
