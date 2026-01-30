package generator

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"sing-box-manager/internal/config"
)

// SingBoxConfig sing-box 完整配置
type SingBoxConfig struct {
	Log          *LogConfig    `json:"log,omitempty"`
	DNS          *DNSConfig    `json:"dns,omitempty"`
	Inbounds     []Inbound     `json:"inbounds,omitempty"`
	Outbounds    []*Outbound   `json:"outbounds,omitempty"`
	Route        *RouteConfig  `json:"route,omitempty"`
	Experimental *Experimental `json:"experimental,omitempty"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level     string `json:"level,omitempty"`
	Timestamp bool   `json:"timestamp,omitempty"`
}

// DNSConfig DNS 配置
type DNSConfig struct {
	Servers       []DNSServer `json:"servers"`
	Rules         []DNSRule   `json:"rules"`
	Final         string      `json:"final"`
	Strategy      string      `json:"strategy,omitempty"`
	CacheCapacity int         `json:"cache_capacity,omitempty"`
}

// DNSServer DNS 服务器
type DNSServer struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Server     string `json:"server,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`
	Detour     string `json:"detour,omitempty"`
	Inet4Range string `json:"inet4_range,omitempty"`
	Inet6Range string `json:"inet6_range,omitempty"`
}

// DNSRule DNS 规则 (sing-box 1.12.0+ 格式)
type DNSRule struct {
	// 匹配条件
	Domain                   []string `json:"domain,omitempty"`
	DomainSuffix             []string `json:"domain_suffix,omitempty"`
	RuleSet                  []string `json:"rule_set,omitempty"`
	QueryType                []string `json:"query_type,omitempty"`
	RuleSetIPCIDRMatchSource bool     `json:"rule_set_ip_cidr_match_source,omitempty"`
	RuleSetIPCIDRAcceptEmpty bool     `json:"rule_set_ip_cidr_accept_empty,omitempty"`
	// 动作 (1.12.0+ 必需)
	Action   string `json:"action"`
	Server   string `json:"server,omitempty"`   // action=route 时使用
	Strategy string `json:"strategy,omitempty"` // 域名解析策略
}

// Inbound 入站配置
type Inbound struct {
	Type                   string   `json:"type"`
	Tag                    string   `json:"tag"`
	InterfaceName          string   `json:"interface_name,omitempty"`
	Address                []string `json:"address,omitempty"`
	MTU                    int      `json:"mtu,omitempty"`
	AutoRoute              bool     `json:"auto_route,omitempty"`
	AutoRedirect           bool     `json:"auto_redirect,omitempty"`
	StrictRoute            bool     `json:"strict_route,omitempty"`
	Stack                  string   `json:"stack,omitempty"`
	Sniff                  bool     `json:"sniff,omitempty"`
	SniffOverrideDestination bool   `json:"sniff_override_destination,omitempty"`
}

// RouteConfig 路由配置
type RouteConfig struct {
	Rules               []RouteRule `json:"rules,omitempty"`
	RuleSet             []RuleSet   `json:"rule_set,omitempty"`
	Final               string      `json:"final,omitempty"`
	AutoDetectInterface bool        `json:"auto_detect_interface,omitempty"`
}

// RouteRule 路由规则
type RouteRule struct {
	Action        string   `json:"action,omitempty"`
	Protocol      []string `json:"protocol,omitempty"`
	IPIsPrivate   bool     `json:"ip_is_private,omitempty"`
	Domain        []string `json:"domain,omitempty"`
	DomainSuffix  []string `json:"domain_suffix,omitempty"`
	DomainKeyword []string `json:"domain_keyword,omitempty"`
	IPCIDR        []string `json:"ip_cidr,omitempty"`
	RuleSet       []string `json:"rule_set,omitempty"`
	Outbound      string   `json:"outbound,omitempty"`
}

// RuleSet 规则集
type RuleSet struct {
	Type           string `json:"type"`
	Tag            string `json:"tag"`
	Format         string `json:"format"`
	URL            string `json:"url,omitempty"`
	DownloadDetour string `json:"download_detour,omitempty"`
	UpdateInterval string `json:"update_interval,omitempty"`
}

// Experimental 实验性配置
type Experimental struct {
	CacheFile *CacheFile `json:"cache_file,omitempty"`
	ClashAPI  *ClashAPI  `json:"clash_api,omitempty"`
}

// CacheFile 缓存文件配置
type CacheFile struct {
	Enabled     bool   `json:"enabled"`
	Path        string `json:"path,omitempty"`
	StoreFakeIP bool   `json:"store_fakeip,omitempty"`
	StoreRDRC   bool   `json:"store_rdrc,omitempty"`
}

// ClashAPI Clash API 配置
type ClashAPI struct {
	ExternalController string `json:"external_controller,omitempty"`
}

// Generator 配置生成器
type Generator struct {
	cfg     config.Config
	state   config.State
	nodes   []config.Node
	dataDir string
}

// NewGenerator 创建配置生成器
func NewGenerator(cfg config.Config, state config.State, nodes []config.Node, dataDir string) *Generator {
	return &Generator{
		cfg:     cfg,
		state:   state,
		nodes:   nodes,
		dataDir: dataDir,
	}
}

// Generate 生成完整配置
func (g *Generator) Generate() (*SingBoxConfig, error) {
	proxyDomains := g.collectProxyDomains()

	sbConfig := &SingBoxConfig{
		Log: &LogConfig{
			Level:     g.cfg.SingBox.LogLevel,
			Timestamp: true,
		},
		DNS:       g.generateDNS(proxyDomains),
		Inbounds:  g.generateInbounds(),
		Outbounds: g.generateOutbounds(),
		Route:     g.generateRoute(),
		Experimental: &Experimental{
			CacheFile: &CacheFile{
				Enabled:     true,
				Path:        filepath.Join(g.dataDir, "cache.db"),
				StoreFakeIP: true,
				StoreRDRC:   true, // 缓存 DNS 响应结果，用于 china-ip 规则匹配
			},
			ClashAPI: &ClashAPI{
				ExternalController: "127.0.0.1:9090",
			},
		},
	}

	return sbConfig, nil
}

// generateDNS 生成 DNS 配置
func (g *Generator) generateDNS(proxyDomains []string) *DNSConfig {
	dns := &DNSConfig{
		Strategy:      "prefer_ipv4",
		CacheCapacity: g.cfg.DNS.CacheCapacity,
	}

	// DNS 服务器
	dns.Servers = []DNSServer{
		{
			Type:   "udp",
			Tag:    "local-dns",
			Server: g.cfg.DNS.DomesticServers[0],
		},
	}

	// 代理 DNS
	if g.cfg.DNS.UseDoH {
		dns.Servers = append(dns.Servers, DNSServer{
			Type:       "https",
			Tag:        "proxy-dns",
			Server:     g.cfg.DNS.ProxyServers[0],
			ServerPort: 443,
			Detour:     "proxy",
		})
	} else {
		dns.Servers = append(dns.Servers, DNSServer{
			Type:   "udp",
			Tag:    "proxy-dns",
			Server: g.cfg.DNS.ProxyServers[0],
			Detour: "proxy",
		})
	}

	// FakeIP（仅用于国外域名）
	if g.cfg.DNS.UseFakeIP {
		dns.Servers = append(dns.Servers, DNSServer{
			Type:       "fakeip",
			Tag:        "fakeip",
			Inet4Range: g.cfg.DNS.FakeIPRange,
			Inet6Range: g.cfg.DNS.FakeIP6Range,
		})
	}

	// DNS 规则
	dns.Rules = []DNSRule{}

	// 1. 代理服务器域名使用本地 DNS（防止循环）
	if len(proxyDomains) > 0 {
		dns.Rules = append(dns.Rules, DNSRule{
			Domain: proxyDomains,
			Action: "route",
			Server: "local-dns",
		})
	}

	// 2. 私有域名使用本地 DNS
	dns.Rules = append(dns.Rules, DNSRule{
		DomainSuffix: []string{"local", "lan", "internal", "home", "localhost"},
		Action:       "route",
		Server:       "local-dns",
	})

	// 3. 中国域名使用本地 DNS（geosite-cn 是公开的域名列表，不算泄露）
	if g.cfg.Bypass.BypassChina {
		dns.Rules = append(dns.Rules, DNSRule{
			RuleSet: []string{"geosite-cn"},
			Action:  "route",
			Server:  "local-dns",
		})
	}

	// 4. 对于未知域名：用远程 DoH 解析，如果响应 IP 在 china-ip 中则返回真实 IP
	//    这样可以让不在 geosite-cn 中的国内小众网站也能正确直连
	//    使用 proxy-dns (DoH) 而不是 local-dns，避免 DNS 泄露
	//    sing-box 1.9.0+ 支持在 DNS 规则中基于响应 IP 匹配 rule_set
	if g.cfg.Bypass.BypassChina {
		dns.Rules = append(dns.Rules, DNSRule{
			QueryType:                []string{"A", "AAAA"},
			RuleSet:                  []string{"china-ip"},
			Action:                   "route",
			Server:                   "proxy-dns", // 使用远程 DoH，避免 DNS 泄露
			RuleSetIPCIDRAcceptEmpty: true,
		})
	}

	// 5. 其他域名使用 FakeIP（如果启用）
	if g.cfg.DNS.UseFakeIP {
		dns.Rules = append(dns.Rules, DNSRule{
			QueryType: []string{"A", "AAAA"},
			Action:    "route",
			Server:    "fakeip",
		})
	}

	// final 使用代理 DNS
	dns.Final = "proxy-dns"

	return dns
}

// generateInbounds 生成入站配置
func (g *Generator) generateInbounds() []Inbound {
	return []Inbound{
		{
			Type:                   "tun",
			Tag:                    "tun-in",
			InterfaceName:          g.cfg.TUN.Name,
			Address:                []string{g.cfg.TUN.Address, g.cfg.TUN.Address6},
			MTU:                    g.cfg.TUN.MTU,
			AutoRoute:              g.cfg.TUN.AutoRoute,
			AutoRedirect:           g.cfg.TUN.AutoRedirect,
			StrictRoute:            true,
			Stack:                  g.cfg.TUN.Stack,
			Sniff:                  true,
			SniffOverrideDestination: true,
		},
	}
}

// generateOutbounds 生成出站配置
func (g *Generator) generateOutbounds() []*Outbound {
	var outbounds []*Outbound
	var nodeOutbounds []string

	for i, node := range g.nodes {
		tag := fmt.Sprintf("node-%d", i+1)
		outbound := NodeToOutbound(node, tag)
		if outbound != nil {
			outbounds = append(outbounds, outbound)
			nodeOutbounds = append(nodeOutbounds, tag)
		}
	}

	// 如果没有节点，添加一个占位的 block outbound
	if len(nodeOutbounds) == 0 {
		nodeOutbounds = append(nodeOutbounds, "direct")
	}

	defaultNode := ""
	if g.state.NodeIndex > 0 && g.state.NodeIndex <= len(nodeOutbounds) {
		defaultNode = nodeOutbounds[g.state.NodeIndex-1]
	} else if len(nodeOutbounds) > 0 {
		defaultNode = nodeOutbounds[0]
	}

	selector := &Outbound{
		Type:                      "selector",
		Tag:                       "proxy",
		Outbounds:                 nodeOutbounds,
		Default:                   defaultNode,
		InterruptExistConnections: true,
	}

	direct := &Outbound{Type: "direct", Tag: "direct"}

	result := []*Outbound{selector, direct}
	result = append(result, outbounds...)

	return result
}

// generateRoute 生成路由配置
func (g *Generator) generateRoute() *RouteConfig {
	route := &RouteConfig{
		AutoDetectInterface: true,
	}

	switch g.state.ProxyMode {
	case "global":
		route.Rules = []RouteRule{
			{Action: "sniff"},
			{Protocol: []string{"dns"}, Action: "hijack-dns"},
			{IPIsPrivate: true, Action: "route", Outbound: "direct"},
		}
		route.Final = "proxy"

	case "direct":
		route.Rules = []RouteRule{
			{Action: "sniff"},
			{Protocol: []string{"dns"}, Action: "hijack-dns"},
		}
		route.Final = "direct"

	default: // rule
		route.Rules = g.generateRuleRoutes()
		route.Final = "proxy"
	}

	route.RuleSet = g.generateRuleSets()

	return route
}

// generateRuleRoutes 生成规则路由
func (g *Generator) generateRuleRoutes() []RouteRule {
	rules := []RouteRule{
		// 1. 嗅探（从 FakeIP 连接中提取真实域名）
		{Action: "sniff"},
		// 2. DNS 劫持
		{Protocol: []string{"dns"}, Action: "hijack-dns"},
	}

	// 3. 局域网绕过
	if g.cfg.Bypass.BypassLAN {
		rules = append(rules, RouteRule{
			IPIsPrivate: true,
			Action:      "route",
			Outbound:    "direct",
		})
	}

	// 4. 中国绕过
	if g.cfg.Bypass.BypassChina {
		// 中国 IP（使用每日更新的运营商 IP 段）
		rules = append(rules, RouteRule{
			RuleSet:  []string{"china-ip"},
			Action:   "route",
			Outbound: "direct",
		})
		// 中国域名
		rules = append(rules, RouteRule{
			RuleSet:  []string{"geosite-cn"},
			Action:   "route",
			Outbound: "direct",
		})
	}

	// 5. 广告拦截
	if g.cfg.Bypass.BlockAds {
		rules = append(rules, RouteRule{
			RuleSet: []string{"geosite-category-ads-all"},
			Action:  "reject",
		})
	}

	// 6. 自定义规则
	for _, rule := range g.state.CustomRules {
		outbound := rule.Outbound
		if outbound == "" {
			outbound = "proxy"
		}

		r := RouteRule{Outbound: outbound}
		if outbound == "block" {
			r.Action = "reject"
			r.Outbound = ""
		} else {
			r.Action = "route"
		}

		switch rule.Type {
		case "domain":
			r.Domain = []string{rule.Value}
		case "domain_suffix":
			r.DomainSuffix = []string{rule.Value}
		case "domain_keyword":
			r.DomainKeyword = []string{rule.Value}
		case "ip_cidr":
			r.IPCIDR = []string{rule.Value}
		case "geosite":
			r.RuleSet = []string{"geosite-" + rule.Value}
		case "geoip":
			r.RuleSet = []string{"geoip-" + rule.Value}
		}

		rules = append(rules, r)
	}

	return rules
}

// generateRuleSets 生成规则集
func (g *Generator) generateRuleSets() []RuleSet {
	var ruleSets []RuleSet

	if g.cfg.Bypass.BypassChina {
		// 中国 IP（fcshark-org 多源整合，每日更新，使用 CDN 加速）
		ruleSets = append(ruleSets, RuleSet{
			Type:           "remote",
			Tag:            "china-ip",
			Format:         "binary",
			URL:            "https://cdn.jsdelivr.net/gh/fcshark-org/route-list@release/china_ip.srs",
			DownloadDetour: "direct",
			UpdateInterval: "1d",
		})
		// 中国域名
		ruleSets = append(ruleSets, RuleSet{
			Type:           "remote",
			Tag:            "geosite-cn",
			Format:         "binary",
			URL:            "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cn.srs",
			DownloadDetour: "proxy",
			UpdateInterval: "7d",
		})
	}

	if g.cfg.Bypass.BlockAds {
		ruleSets = append(ruleSets, RuleSet{
			Type:           "remote",
			Tag:            "geosite-category-ads-all",
			Format:         "binary",
			URL:            "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-category-ads-all.srs",
			DownloadDetour: "proxy",
			UpdateInterval: "7d",
		})
	}

	return ruleSets
}

// collectProxyDomains 收集代理服务器域名
func (g *Generator) collectProxyDomains() []string {
	domainSet := make(map[string]bool)

	for _, node := range g.nodes {
		if node.Server != "" && !isIP(node.Server) {
			domainSet[node.Server] = true
		}
	}

	var domains []string
	for domain := range domainSet {
		domains = append(domains, domain)
	}

	return domains
}

// isIP 检查是否为 IP 地址
func isIP(s string) bool {
	return net.ParseIP(s) != nil
}

// SaveConfig 保存配置到文件
func (g *Generator) SaveConfig(sbConfig *SingBoxConfig, path string) error {
	data, err := json.MarshalIndent(sbConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}
