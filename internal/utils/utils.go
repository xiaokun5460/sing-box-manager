package utils

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Base64Decode Base64 解码（支持多种格式）
func Base64Decode(s string) (string, error) {
	// 补齐 padding
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}

	// 尝试标准 Base64
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
		return string(decoded), nil
	}

	// 尝试 URL 安全 Base64
	if decoded, err := base64.URLEncoding.DecodeString(s); err == nil {
		return string(decoded), nil
	}

	// 尝试无 padding 的 Base64
	s = strings.TrimRight(s, "=")
	if decoded, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return string(decoded), nil
	}

	if decoded, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return string(decoded), nil
	}

	return "", fmt.Errorf("base64 解码失败")
}

// URLDecode URL 解码
func URLDecode(s string) string {
	decoded, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	return decoded
}

// GetString 从 map 获取字符串
func GetString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case string:
			return val
		case float64:
			return strconv.FormatFloat(val, 'f', -1, 64)
		case int:
			return strconv.Itoa(val)
		}
	}
	return ""
}

// GetInt 从 map 获取整数
func GetInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return int(val)
		case int:
			return val
		case string:
			i, _ := strconv.Atoi(val)
			return i
		}
	}
	return 0
}
