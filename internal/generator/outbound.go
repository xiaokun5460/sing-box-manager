package generator

import (
	"encoding/json"
	"net/url"
	"strings"

	"sing-box-manager/internal/config"
	"sing-box-manager/internal/utils"
)

// Outbound sing-box 出站配置
type Outbound struct {
	Type                       string                 `json:"type"`
	Tag                        string                 `json:"tag"`
	Server                     string                 `json:"server,omitempty"`
	ServerPort                 int                    `json:"server_port,omitempty"`
	Method                     string                 `json:"method,omitempty"`
	Password                   string                 `json:"password,omitempty"`
	UUID                       string                 `json:"uuid,omitempty"`
	Flow                       string                 `json:"flow,omitempty"`
	Security                   string                 `json:"security,omitempty"`
	AlterId                    int                    `json:"alter_id,omitempty"`
	TLS                        *TLSConfig             `json:"tls,omitempty"`
	Transport                  *TransportConfig       `json:"transport,omitempty"`
	Multiplex                  *MultiplexConfig       `json:"multiplex,omitempty"`
	Outbounds                  []string               `json:"outbounds,omitempty"`
	Default                    string                 `json:"default,omitempty"`
	InterruptExistConnections  bool                   `json:"interrupt_exist_connections,omitempty"`
	Up                         string                 `json:"up,omitempty"`
	Down                       string                 `json:"down,omitempty"`
	Obfs                       *ObfsConfig            `json:"obfs,omitempty"`
}

// TLSConfig TLS 配置
type TLSConfig struct {
	Enabled         bool          `json:"enabled"`
	ServerName      string        `json:"server_name,omitempty"`
	Insecure        bool          `json:"insecure,omitempty"`
	ALPN            []string      `json:"alpn,omitempty"`
	UTLS            *UTLSConfig   `json:"utls,omitempty"`
	Reality         *RealityConfig `json:"reality,omitempty"`
}

// UTLSConfig UTLS 配置
type UTLSConfig struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// RealityConfig Reality 配置
type RealityConfig struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key,omitempty"`
	ShortID   string `json:"short_id,omitempty"`
}

// TransportConfig 传输层配置
type TransportConfig struct {
	Type        string            `json:"type"`
	Path        string            `json:"path,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	ServiceName string            `json:"service_name,omitempty"`
	Host        string            `json:"host,omitempty"`
}

// MultiplexConfig 多路复用配置
type MultiplexConfig struct {
	Enabled  bool   `json:"enabled"`
	Protocol string `json:"protocol,omitempty"`
}

// ObfsConfig 混淆配置
type ObfsConfig struct {
	Type     string `json:"type"`
	Password string `json:"password,omitempty"`
}

// NodeToOutbound 将节点转换为出站配置
func NodeToOutbound(node config.Node, tag string) *Outbound {
	switch node.Type {
	case "ss":
		return parseSSToOutbound(node, tag)
	case "vmess":
		return parseVMessToOutbound(node, tag)
	case "vless":
		return parseVLESSToOutbound(node, tag)
	case "trojan":
		return parseTrojanToOutbound(node, tag)
	case "hysteria2":
		return parseHysteria2ToOutbound(node, tag)
	default:
		return nil
	}
}

// parseSSToOutbound 解析 SS 节点
func parseSSToOutbound(node config.Node, tag string) *Outbound {
	link := node.Raw
	if idx := strings.LastIndex(link, "#"); idx != -1 {
		link = link[:idx]
	}
	link = strings.TrimPrefix(link, "ss://")

	var method, password string
	if atIdx := strings.LastIndex(link, "@"); atIdx != -1 {
		if decoded, err := utils.Base64Decode(link[:atIdx]); err == nil {
			if parts := strings.SplitN(decoded, ":", 2); len(parts) == 2 {
				method, password = parts[0], parts[1]
				// 去除密码两端的引号
				password = strings.Trim(password, "\"")
			}
		}
	}

	return &Outbound{
		Type:       "shadowsocks",
		Tag:        tag,
		Server:     node.Server,
		ServerPort: node.Port,
		Method:     method,
		Password:   password,
	}
}

// parseVMessToOutbound 解析 VMess 节点
func parseVMessToOutbound(node config.Node, tag string) *Outbound {
	link := strings.TrimPrefix(node.Raw, "vmess://")

	decoded, err := utils.Base64Decode(link)
	if err != nil {
		return nil
	}

	var vmess map[string]interface{}
	if err := json.Unmarshal([]byte(decoded), &vmess); err != nil {
		return nil
	}

	outbound := &Outbound{
		Type:       "vmess",
		Tag:        tag,
		Server:     utils.GetString(vmess, "add"),
		ServerPort: utils.GetInt(vmess, "port"),
		UUID:       utils.GetString(vmess, "id"),
		AlterId:    utils.GetInt(vmess, "aid"),
		Security:   utils.GetString(vmess, "scy"),
	}

	if outbound.Security == "" {
		outbound.Security = "auto"
	}

	// TLS
	if utils.GetString(vmess, "tls") == "tls" {
		outbound.TLS = &TLSConfig{
			Enabled:    true,
			ServerName: utils.GetString(vmess, "sni"),
			Insecure:   true,
		}
		if host := utils.GetString(vmess, "host"); host != "" && outbound.TLS.ServerName == "" {
			outbound.TLS.ServerName = host
		}
	}

	// 传输层
	network := utils.GetString(vmess, "net")
	switch network {
	case "ws":
		outbound.Transport = &TransportConfig{
			Type: "ws",
			Path: utils.GetString(vmess, "path"),
		}
		if host := utils.GetString(vmess, "host"); host != "" {
			outbound.Transport.Headers = map[string]string{"Host": host}
		}
	case "grpc":
		outbound.Transport = &TransportConfig{
			Type:        "grpc",
			ServiceName: utils.GetString(vmess, "path"),
		}
	case "h2":
		outbound.Transport = &TransportConfig{
			Type: "http",
			Path: utils.GetString(vmess, "path"),
			Host: utils.GetString(vmess, "host"),
		}
	}

	return outbound
}

// parseVLESSToOutbound 解析 VLESS 节点
func parseVLESSToOutbound(node config.Node, tag string) *Outbound {
	link := node.Raw
	if idx := strings.LastIndex(link, "#"); idx != -1 {
		link = link[:idx]
	}
	link = strings.TrimPrefix(link, "vless://")

	var uuid string
	if atIdx := strings.Index(link, "@"); atIdx != -1 {
		uuid = link[:atIdx]
		link = link[atIdx+1:]
	}

	params := make(map[string]string)
	if qIdx := strings.Index(link, "?"); qIdx != -1 {
		queryStr := link[qIdx+1:]
		for _, pair := range strings.Split(queryStr, "&") {
			if kv := strings.SplitN(pair, "=", 2); len(kv) == 2 {
				params[kv[0]], _ = url.QueryUnescape(kv[1])
			}
		}
	}

	outbound := &Outbound{
		Type:       "vless",
		Tag:        tag,
		Server:     node.Server,
		ServerPort: node.Port,
		UUID:       uuid,
		Flow:       params["flow"],
	}

	// TLS / Reality
	security := params["security"]
	if security == "tls" || security == "reality" || params["pbk"] != "" {
		outbound.TLS = &TLSConfig{
			Enabled:    true,
			ServerName: params["sni"],
			Insecure:   params["allowInsecure"] == "1",
		}

		if fp := params["fp"]; fp != "" {
			outbound.TLS.UTLS = &UTLSConfig{
				Enabled:     true,
				Fingerprint: fp,
			}
		}

		if params["pbk"] != "" {
			outbound.TLS.Reality = &RealityConfig{
				Enabled:   true,
				PublicKey: params["pbk"],
				ShortID:   params["sid"],
			}
		}
	}

	// 传输层
	transportType := params["type"]
	switch transportType {
	case "ws":
		outbound.Transport = &TransportConfig{
			Type: "ws",
			Path: params["path"],
		}
		if host := params["host"]; host != "" {
			outbound.Transport.Headers = map[string]string{"Host": host}
		}
	case "grpc":
		outbound.Transport = &TransportConfig{
			Type:        "grpc",
			ServiceName: params["serviceName"],
		}
	case "h2":
		outbound.Transport = &TransportConfig{
			Type: "http",
			Path: params["path"],
			Host: params["host"],
		}
	}

	return outbound
}

// parseTrojanToOutbound 解析 Trojan 节点
func parseTrojanToOutbound(node config.Node, tag string) *Outbound {
	link := node.Raw
	if idx := strings.LastIndex(link, "#"); idx != -1 {
		link = link[:idx]
	}
	link = strings.TrimPrefix(link, "trojan://")

	var password string
	if atIdx := strings.Index(link, "@"); atIdx != -1 {
		password, _ = url.QueryUnescape(link[:atIdx])
		link = link[atIdx+1:]
	}

	params := make(map[string]string)
	if qIdx := strings.Index(link, "?"); qIdx != -1 {
		queryStr := link[qIdx+1:]
		for _, pair := range strings.Split(queryStr, "&") {
			if kv := strings.SplitN(pair, "=", 2); len(kv) == 2 {
				params[kv[0]], _ = url.QueryUnescape(kv[1])
			}
		}
	}

	outbound := &Outbound{
		Type:       "trojan",
		Tag:        tag,
		Server:     node.Server,
		ServerPort: node.Port,
		Password:   password,
		TLS: &TLSConfig{
			Enabled:    true,
			ServerName: params["sni"],
			Insecure:   params["allowInsecure"] == "1",
		},
	}

	// 传输层
	transportType := params["type"]
	switch transportType {
	case "ws":
		outbound.Transport = &TransportConfig{
			Type: "ws",
			Path: params["path"],
		}
		if host := params["host"]; host != "" {
			outbound.Transport.Headers = map[string]string{"Host": host}
		}
	case "grpc":
		outbound.Transport = &TransportConfig{
			Type:        "grpc",
			ServiceName: params["serviceName"],
		}
	}

	return outbound
}

// parseHysteria2ToOutbound 解析 Hysteria2 节点
func parseHysteria2ToOutbound(node config.Node, tag string) *Outbound {
	link := node.Raw
	if idx := strings.LastIndex(link, "#"); idx != -1 {
		link = link[:idx]
	}
	link = strings.TrimPrefix(link, "hysteria2://")
	link = strings.TrimPrefix(link, "hy2://")

	var password string
	if atIdx := strings.Index(link, "@"); atIdx != -1 {
		password, _ = url.QueryUnescape(link[:atIdx])
		link = link[atIdx+1:]
	}

	params := make(map[string]string)
	if qIdx := strings.Index(link, "?"); qIdx != -1 {
		queryStr := link[qIdx+1:]
		for _, pair := range strings.Split(queryStr, "&") {
			if kv := strings.SplitN(pair, "=", 2); len(kv) == 2 {
				params[kv[0]], _ = url.QueryUnescape(kv[1])
			}
		}
	}

	outbound := &Outbound{
		Type:       "hysteria2",
		Tag:        tag,
		Server:     node.Server,
		ServerPort: node.Port,
		Password:   password,
		TLS: &TLSConfig{
			Enabled:    true,
			ServerName: params["sni"],
			Insecure:   params["insecure"] == "1",
		},
	}

	// 混淆
	if obfs := params["obfs"]; obfs != "" {
		outbound.Obfs = &ObfsConfig{
			Type:     obfs,
			Password: params["obfs-password"],
		}
	}

	return outbound
}
