package sing

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
)

// parsePort 严格解析端口号：只接受十进制整数，合法范围为 1-65535。
//
// 这里先按 64 位解析再做显式上下界检查，避免先解析成与架构相关的 int
// 再直接转换为 uint16 时发生的静默截断（CWE-190/CWE-681）。端口 0 没有
// 明确的业务含义（本项目未使用 0 表示随机端口），因此和负数、超过 65535
// 的值、空字符串一样被拒绝，而不是被截断成别的端口。
func parsePort(value string) (uint16, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse port %q: %w", value, err)
	}
	// 上下界检查与常量比较，保证转换前已经排除负数/溢出。
	if parsed >= 1 && parsed <= math.MaxUint16 {
		return uint16(parsed), nil
	}
	return 0, fmt.Errorf("port %q out of range [1, 65535]", value)
}

// parseEarlyData 严格解析 WebSocket early data 长度，语义与 sing-box 的
// option.V2RayWebsocketOptions.MaxEarlyData（uint32）保持一致：0 表示禁用
// early data，是合法值；负数以及超过 math.MaxUint32 的值返回错误。
// 注意这里不套用端口范围，early data 的取值上限只由 uint32 决定。
func parseEarlyData(value string) (uint32, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse early data %q: %w", value, err)
	}
	// 上下界检查与常量比较，保证转换前已经排除负数/溢出。
	if parsed >= 0 && parsed <= math.MaxUint32 {
		return uint32(parsed), nil
	}
	return 0, fmt.Errorf("early data %q out of range [0, 4294967295]", value)
}

// parseEarlyDataQuery 从 WebSocket path 的查询参数中解析 ed。
//
// 旧实现（ed, _ := strconv.Atoi(u.Query().Get("ed"))）在 ed 缺省或为空时
// 会得到 0，即不启用 early data；这里保留该兼容语义（0 表示禁用），
// 但只要显式给出了 ed 值，就必须是合法的 uint32，非法值会返回错误，
// 不再像旧实现那样静默按 0 处理。
func parseEarlyDataQuery(query url.Values) (uint32, error) {
	raw := query.Get("ed")
	if raw == "" {
		return 0, nil
	}
	return parseEarlyData(raw)
}
