package subscription

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"sing-box-manager/internal/config"
)

// ClashConfig Clash 配置结构
type ClashConfig struct {
	Proxies []ClashProxy `yaml:"proxies"`
}

// ClashProxy Clash 代理结构
type ClashProxy struct {
	Name           string `yaml:"name"`
	Type           string `yaml:"type"`
	Server         string `yaml:"server"`
	Port           int    `yaml:"port"`
	Cipher         string `yaml:"cipher"`
	Password       string `yaml:"password"`
	UUID           string `yaml:"uuid"`
	AlterId        int    `yaml:"alterId"`
	Network        string `yaml:"network"`
	TLS            bool   `yaml:"tls"`
	SkipCertVerify bool   `yaml:"skip-cert-verify"`
	ServerName     string `yaml:"servername"`
	SNI            string `yaml:"sni"`
	Flow           string `yaml:"flow"`
	// WebSocket 选项
	WSOpts *WSOptions `yaml:"ws-opts"`
	// gRPC 选项
	GRPCOpts *GRPCOptions `yaml:"grpc-opts"`
	// Reality 选项
	RealityOpts *RealityOptions `yaml:"reality-opts"`
	// 客户端指纹
	ClientFingerprint string `yaml:"client-fingerprint"`
	// Hysteria2 选项
	Obfs         string `yaml:"obfs"`
	ObfsPassword string `yaml:"obfs-password"`
	// UDP
	UDP bool `yaml:"udp"`
}

// WSOptions WebSocket 选项
type WSOptions struct {
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
}

// GRPCOptions gRPC 选项
type GRPCOptions struct {
	GRPCServiceName string `yaml:"grpc-service-name"`
}

// RealityOptions Reality 选项
type RealityOptions struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id"`
}

// parseClashYAML 解析 Clash YAML 格式
func parseClashYAML(content string, subID string) ([]config.Node, error) {
	var clashConfig ClashConfig
	if err := yaml.Unmarshal([]byte(content), &clashConfig); err != nil {
		return nil, fmt.Errorf("解析 Clash YAML 失败: %w", err)
	}

	var nodes []config.Node
	index := 0

	for _, proxy := range clashConfig.Proxies {
		node := clashProxyToNode(&proxy, index, subID)
		if node != nil {
			nodes = append(nodes, *node)
			index++
		}
	}

	// 重新编号
	for i := range nodes {
		nodes[i].Index = i + 1
	}

	return nodes, nil
}

// clashProxyToNode 将 Clash 代理转换为 Node
func clashProxyToNode(proxy *ClashProxy, index int, subID string) *config.Node {
	proxyType := strings.ToLower(proxy.Type)
	if proxyType == "" {
		return nil
	}

	node := &config.Node{
		Index:   index + 1,
		Name:    proxy.Name,
		Server:  proxy.Server,
		Port:    proxy.Port,
		Latency: -1,
		SubID:   subID,
	}

	// 转换类型并生成 Raw URI
	switch proxyType {
	case "ss", "shadowsocks":
		node.Type = "ss"
		node.Raw = clashSSToURI(proxy)
	case "vmess":
		node.Type = "vmess"
		node.Raw = clashVMessToURI(proxy)
	case "vless":
		node.Type = "vless"
		node.Raw = clashVLESSToURI(proxy)
	case "trojan":
		node.Type = "trojan"
		node.Raw = clashTrojanToURI(proxy)
	case "hysteria2", "hy2":
		node.Type = "hysteria2"
		node.Raw = clashHysteria2ToURI(proxy)
	default:
		return nil
	}

	if node.Name == "" {
		node.Name = node.Server
	}

	// 如果 Raw 为空，说明转换失败
	if node.Raw == "" {
		return nil
	}

	return node
}

// clashSSToURI 将 Clash SS 转换为 URI
func clashSSToURI(proxy *ClashProxy) string {
	method := proxy.Cipher
	password := proxy.Password
	server := proxy.Server
	port := proxy.Port
	name := proxy.Name

	if method == "" || password == "" || server == "" || port == 0 {
		return ""
	}

	// 去除密码两端的引号
	password = strings.Trim(password, "\"")

	auth := base64Encode(method + ":" + password)
	return "ss://" + auth + "@" + server + ":" + strconv.Itoa(port) + "#" + urlEncode(name)
}

// clashVMessToURI 将 Clash VMess 转换为 URI
func clashVMessToURI(proxy *ClashProxy) string {
	if proxy.UUID == "" || proxy.Server == "" || proxy.Port == 0 {
		return ""
	}

	network := proxy.Network
	if network == "" {
		network = "tcp"
	}

	tls := ""
	if proxy.TLS {
		tls = "tls"
	}

	host := ""
	path := ""
	if proxy.WSOpts != nil {
		path = proxy.WSOpts.Path
		if h, ok := proxy.WSOpts.Headers["Host"]; ok {
			host = h
		}
	}

	vmess := map[string]interface{}{
		"v":    "2",
		"ps":   proxy.Name,
		"add":  proxy.Server,
		"port": strconv.Itoa(proxy.Port),
		"id":   proxy.UUID,
		"aid":  strconv.Itoa(proxy.AlterId),
		"net":  network,
		"type": "none",
		"host": host,
		"path": path,
		"tls":  tls,
	}

	// 简单 JSON 序列化
	jsonStr := "{"
	first := true
	for k, v := range vmess {
		if !first {
			jsonStr += ","
		}
		first = false
		jsonStr += "\"" + k + "\":\"" + toString(v) + "\""
	}
	jsonStr += "}"

	return "vmess://" + base64Encode(jsonStr)
}

// clashVLESSToURI 将 Clash VLESS 转换为 URI
func clashVLESSToURI(proxy *ClashProxy) string {
	if proxy.UUID == "" || proxy.Server == "" || proxy.Port == 0 {
		return ""
	}

	uri := "vless://" + proxy.UUID + "@" + proxy.Server + ":" + strconv.Itoa(proxy.Port)

	// 构建查询参数
	var params []string

	if proxy.Network != "" {
		params = append(params, "type="+proxy.Network)
	}
	if proxy.TLS {
		params = append(params, "security=tls")
	}
	if proxy.Flow != "" {
		params = append(params, "flow="+proxy.Flow)
	}

	// SNI
	sni := proxy.SNI
	if sni == "" {
		sni = proxy.ServerName
	}
	if sni != "" {
		params = append(params, "sni="+sni)
	}

	// Reality
	if proxy.RealityOpts != nil && proxy.RealityOpts.PublicKey != "" {
		params = append(params, "pbk="+proxy.RealityOpts.PublicKey)
		params = append(params, "security=reality")
		if proxy.RealityOpts.ShortID != "" {
			params = append(params, "sid="+proxy.RealityOpts.ShortID)
		}
	}

	// 客户端指纹
	if proxy.ClientFingerprint != "" {
		params = append(params, "fp="+proxy.ClientFingerprint)
	}

	// WebSocket
	if proxy.WSOpts != nil {
		if proxy.WSOpts.Path != "" {
			params = append(params, "path="+urlEncode(proxy.WSOpts.Path))
		}
		if h, ok := proxy.WSOpts.Headers["Host"]; ok {
			params = append(params, "host="+h)
		}
	}

	// gRPC
	if proxy.GRPCOpts != nil && proxy.GRPCOpts.GRPCServiceName != "" {
		params = append(params, "serviceName="+proxy.GRPCOpts.GRPCServiceName)
	}

	if len(params) > 0 {
		uri += "?" + strings.Join(params, "&")
	}

	uri += "#" + urlEncode(proxy.Name)

	return uri
}

// clashTrojanToURI 将 Clash Trojan 转换为 URI
func clashTrojanToURI(proxy *ClashProxy) string {
	if proxy.Password == "" || proxy.Server == "" || proxy.Port == 0 {
		return ""
	}

	uri := "trojan://" + urlEncode(proxy.Password) + "@" + proxy.Server + ":" + strconv.Itoa(proxy.Port)

	var params []string

	// SNI
	sni := proxy.SNI
	if sni == "" {
		sni = proxy.ServerName
	}
	if sni != "" {
		params = append(params, "sni="+sni)
	}

	if proxy.Network != "" && proxy.Network != "tcp" {
		params = append(params, "type="+proxy.Network)
	}

	// WebSocket
	if proxy.WSOpts != nil {
		if proxy.WSOpts.Path != "" {
			params = append(params, "path="+urlEncode(proxy.WSOpts.Path))
		}
		if h, ok := proxy.WSOpts.Headers["Host"]; ok {
			params = append(params, "host="+h)
		}
	}

	// gRPC
	if proxy.GRPCOpts != nil && proxy.GRPCOpts.GRPCServiceName != "" {
		params = append(params, "serviceName="+proxy.GRPCOpts.GRPCServiceName)
	}

	if len(params) > 0 {
		uri += "?" + strings.Join(params, "&")
	}

	uri += "#" + urlEncode(proxy.Name)

	return uri
}

// clashHysteria2ToURI 将 Clash Hysteria2 转换为 URI
func clashHysteria2ToURI(proxy *ClashProxy) string {
	if proxy.Password == "" || proxy.Server == "" || proxy.Port == 0 {
		return ""
	}

	uri := "hysteria2://" + urlEncode(proxy.Password) + "@" + proxy.Server + ":" + strconv.Itoa(proxy.Port)

	var params []string

	// SNI
	sni := proxy.SNI
	if sni == "" {
		sni = proxy.ServerName
	}
	if sni != "" {
		params = append(params, "sni="+sni)
	}

	if proxy.Obfs != "" {
		params = append(params, "obfs="+proxy.Obfs)
	}
	if proxy.ObfsPassword != "" {
		params = append(params, "obfs-password="+proxy.ObfsPassword)
	}

	if len(params) > 0 {
		uri += "?" + strings.Join(params, "&")
	}

	uri += "#" + urlEncode(proxy.Name)

	return uri
}

// base64Encode Base64 编码
func base64Encode(s string) string {
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(s))
}

// urlEncode URL 编码
func urlEncode(s string) string {
	return strings.ReplaceAll(
		strings.ReplaceAll(
			strings.ReplaceAll(s, " ", "%20"),
			"#", "%23"),
		"&", "%26")
}

// toString 转换为字符串
func toString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return ""
	}
}
