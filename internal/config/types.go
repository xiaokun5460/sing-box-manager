package config

import "time"

// Config 应用配置
type Config struct {
	Server       ServerConfig       `yaml:"server" json:"server"`
	SingBox      SingBoxConfig      `yaml:"singbox" json:"singbox"`
	DNS          DNSConfig          `yaml:"dns" json:"dns"`
	TUN          TUNConfig          `yaml:"tun" json:"tun"`
	Subscription SubscriptionConfig `yaml:"subscription" json:"subscription"`
	Bypass       BypassConfig       `yaml:"bypass" json:"bypass"`
}

// ServerConfig Web 服务器配置
type ServerConfig struct {
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
}

// SingBoxConfig sing-box 配置
type SingBoxConfig struct {
	BinaryPath string `yaml:"binary_path" json:"binary_path"`
	ConfigPath string `yaml:"config_path" json:"config_path"`
	LogLevel   string `yaml:"log_level" json:"log_level"`
}

// DNSConfig DNS 配置
type DNSConfig struct {
	DomesticServers []string `yaml:"domestic_servers" json:"domestic_servers"`
	ProxyServers    []string `yaml:"proxy_servers" json:"proxy_servers"`
	UseDoH          bool     `yaml:"use_doh" json:"use_doh"`
	UseFakeIP       bool     `yaml:"use_fakeip" json:"use_fakeip"`
	FakeIPRange     string   `yaml:"fakeip_range" json:"fakeip_range"`
	FakeIP6Range    string   `yaml:"fakeip6_range" json:"fakeip6_range"`
	CacheCapacity   int      `yaml:"cache_capacity" json:"cache_capacity"`
}

// TUNConfig TUN 设备配置
type TUNConfig struct {
	Name         string `yaml:"name" json:"name"`
	Address      string `yaml:"address" json:"address"`
	Address6     string `yaml:"address6" json:"address6"`
	MTU          int    `yaml:"mtu" json:"mtu"`
	AutoRoute    bool   `yaml:"auto_route" json:"auto_route"`
	AutoRedirect bool   `yaml:"auto_redirect" json:"auto_redirect"`
	Stack        string `yaml:"stack" json:"stack"`
}

// SubscriptionConfig 订阅配置
type SubscriptionConfig struct {
	AutoUpdate     bool `yaml:"auto_update" json:"auto_update"`
	UpdateInterval int  `yaml:"update_interval" json:"update_interval"` // 分钟
}

// BypassConfig 绕过配置
type BypassConfig struct {
	BypassLAN   bool `yaml:"bypass_lan" json:"bypass_lan"`
	BypassChina bool `yaml:"bypass_china" json:"bypass_china"`
	BlockAds    bool `yaml:"block_ads" json:"block_ads"`
}

// State 应用状态
type State struct {
	ProxyMode     string         `yaml:"proxy_mode" json:"proxy_mode"`
	CurrentNode   string         `yaml:"current_node" json:"current_node"`
	NodeIndex     int            `yaml:"node_index" json:"node_index"`
	Subscriptions []Subscription `yaml:"subscriptions" json:"subscriptions"`
	CustomRules   []CustomRule   `yaml:"custom_rules" json:"custom_rules"`
}

// Subscription 订阅信息
type Subscription struct {
	ID        string    `yaml:"id" json:"id"`
	Name      string    `yaml:"name" json:"name"`
	URL       string    `yaml:"url" json:"url"`
	UpdatedAt time.Time `yaml:"updated_at" json:"updated_at"`
	NodeCount int       `yaml:"node_count" json:"node_count"`
}

// CustomRule 自定义规则
type CustomRule struct {
	Type     string `yaml:"type" json:"type"`         // domain, domain_suffix, domain_keyword, ip_cidr, geosite, geoip
	Value    string `yaml:"value" json:"value"`
	Outbound string `yaml:"outbound" json:"outbound"` // proxy, direct, block
}

// Node 代理节点
type Node struct {
	Index   int    `json:"index"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Server  string `json:"server"`
	Port    int    `json:"port"`
	Latency int    `json:"latency"`
	Raw     string `json:"-"`
	SubID   string `json:"sub_id,omitempty"`
}
