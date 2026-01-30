package subscription

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"sing-box-manager/internal/config"
	"sing-box-manager/internal/utils"
)

// ParseSubscription 解析订阅内容
func ParseSubscription(content string, subID string) ([]config.Node, error) {
	content = strings.TrimSpace(content)

	// 尝试 Base64 解码
	if decoded, err := base64.StdEncoding.DecodeString(content); err == nil {
		content = string(decoded)
	} else if decoded, err := base64.RawStdEncoding.DecodeString(content); err == nil {
		content = string(decoded)
	} else if decoded, err := base64.URLEncoding.DecodeString(content); err == nil {
		content = string(decoded)
	} else if decoded, err := base64.RawURLEncoding.DecodeString(content); err == nil {
		content = string(decoded)
	}

	content = strings.TrimSpace(content)

	// 检测格式 - Clash YAML 格式（包含 proxies: 或以常见 Clash 配置开头）
	if strings.HasPrefix(content, "proxies:") || strings.Contains(content, "\nproxies:") ||
		strings.HasPrefix(content, "port:") || strings.HasPrefix(content, "mixed-port:") {
		return parseClashYAML(content, subID)
	}

	// URI 列表格式
	return parseURIList(content, subID)
}

// parseURIList 解析 URI 列表
func parseURIList(content string, subID string) ([]config.Node, error) {
	var nodes []config.Node
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		var node *config.Node
		var err error

		switch {
		case strings.HasPrefix(line, "ss://"):
			node, err = parseSSURI(line)
		case strings.HasPrefix(line, "vmess://"):
			node, err = parseVMessURI(line)
		case strings.HasPrefix(line, "vless://"):
			node, err = parseVLESSURI(line)
		case strings.HasPrefix(line, "trojan://"):
			node, err = parseTrojanURI(line)
		case strings.HasPrefix(line, "hysteria2://"), strings.HasPrefix(line, "hy2://"):
			node, err = parseHysteria2URI(line)
		default:
			continue
		}

		if err != nil {
			continue
		}

		if node != nil {
			node.Index = i + 1
			node.SubID = subID
			nodes = append(nodes, *node)
		}
	}

	return nodes, nil
}

// parseSSURI 解析 Shadowsocks URI
func parseSSURI(uri string) (*config.Node, error) {
	node := &config.Node{Type: "ss", Raw: uri, Latency: -1}

	// 提取名称
	if idx := strings.LastIndex(uri, "#"); idx != -1 {
		node.Name = utils.URLDecode(uri[idx+1:])
		uri = uri[:idx]
	}

	uri = strings.TrimPrefix(uri, "ss://")

	// SIP002 格式: base64(method:password)@server:port
	if atIdx := strings.LastIndex(uri, "@"); atIdx != -1 {
		serverPart := uri[atIdx+1:]
		if colonIdx := strings.LastIndex(serverPart, ":"); colonIdx != -1 {
			node.Server = serverPart[:colonIdx]
			node.Port, _ = strconv.Atoi(serverPart[colonIdx+1:])
		}
	} else {
		// 旧格式: base64(method:password@server:port)
		decoded, err := utils.Base64Decode(uri)
		if err != nil {
			return nil, err
		}
		if parts := strings.SplitN(decoded, "@", 2); len(parts) == 2 {
			if colonIdx := strings.LastIndex(parts[1], ":"); colonIdx != -1 {
				node.Server = parts[1][:colonIdx]
				node.Port, _ = strconv.Atoi(parts[1][colonIdx+1:])
			}
		}
	}

	if node.Name == "" {
		node.Name = node.Server
	}

	return node, nil
}

// parseVMessURI 解析 VMess URI
func parseVMessURI(uri string) (*config.Node, error) {
	node := &config.Node{Type: "vmess", Raw: uri, Latency: -1}

	uri = strings.TrimPrefix(uri, "vmess://")

	decoded, err := utils.Base64Decode(uri)
	if err != nil {
		return nil, err
	}

	var vmess map[string]interface{}
	if err := json.Unmarshal([]byte(decoded), &vmess); err != nil {
		return nil, err
	}

	node.Name = utils.GetString(vmess, "ps")
	node.Server = utils.GetString(vmess, "add")
	node.Port = utils.GetInt(vmess, "port")

	if node.Name == "" {
		node.Name = node.Server
	}

	return node, nil
}

// parseVLESSURI 解析 VLESS URI
func parseVLESSURI(uri string) (*config.Node, error) {
	node := &config.Node{Type: "vless", Raw: uri, Latency: -1}

	// 提取名称
	if idx := strings.LastIndex(uri, "#"); idx != -1 {
		node.Name = utils.URLDecode(uri[idx+1:])
		uri = uri[:idx]
	}

	uri = strings.TrimPrefix(uri, "vless://")

	// 格式: uuid@server:port?params
	if atIdx := strings.Index(uri, "@"); atIdx != -1 {
		serverPart := uri[atIdx+1:]
		if qIdx := strings.Index(serverPart, "?"); qIdx != -1 {
			serverPart = serverPart[:qIdx]
		}
		if colonIdx := strings.LastIndex(serverPart, ":"); colonIdx != -1 {
			node.Server = serverPart[:colonIdx]
			node.Port, _ = strconv.Atoi(serverPart[colonIdx+1:])
		}
	}

	if node.Name == "" {
		node.Name = node.Server
	}

	return node, nil
}

// parseTrojanURI 解析 Trojan URI
func parseTrojanURI(uri string) (*config.Node, error) {
	node := &config.Node{Type: "trojan", Raw: uri, Latency: -1}

	// 提取名称
	if idx := strings.LastIndex(uri, "#"); idx != -1 {
		node.Name = utils.URLDecode(uri[idx+1:])
		uri = uri[:idx]
	}

	uri = strings.TrimPrefix(uri, "trojan://")

	// 格式: password@server:port?params
	if atIdx := strings.Index(uri, "@"); atIdx != -1 {
		serverPart := uri[atIdx+1:]
		if qIdx := strings.Index(serverPart, "?"); qIdx != -1 {
			serverPart = serverPart[:qIdx]
		}
		if colonIdx := strings.LastIndex(serverPart, ":"); colonIdx != -1 {
			node.Server = serverPart[:colonIdx]
			node.Port, _ = strconv.Atoi(serverPart[colonIdx+1:])
		}
	}

	if node.Name == "" {
		node.Name = node.Server
	}

	return node, nil
}

// parseHysteria2URI 解析 Hysteria2 URI
func parseHysteria2URI(uri string) (*config.Node, error) {
	node := &config.Node{Type: "hysteria2", Raw: uri, Latency: -1}

	// 提取名称
	if idx := strings.LastIndex(uri, "#"); idx != -1 {
		node.Name = utils.URLDecode(uri[idx+1:])
		uri = uri[:idx]
	}

	uri = strings.TrimPrefix(uri, "hysteria2://")
	uri = strings.TrimPrefix(uri, "hy2://")

	// 格式: auth@server:port?params
	if atIdx := strings.Index(uri, "@"); atIdx != -1 {
		serverPart := uri[atIdx+1:]
		if qIdx := strings.Index(serverPart, "?"); qIdx != -1 {
			serverPart = serverPart[:qIdx]
		}
		if colonIdx := strings.LastIndex(serverPart, ":"); colonIdx != -1 {
			node.Server = serverPart[:colonIdx]
			node.Port, _ = strconv.Atoi(serverPart[colonIdx+1:])
		}
	}

	if node.Name == "" {
		node.Name = node.Server
	}

	return node, nil
}
