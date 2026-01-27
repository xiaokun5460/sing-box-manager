package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ConfigDir         = "/etc/sing-box"
	ConfigFile        = "/etc/sing-box/config.json"
	BackupFile        = "/etc/sing-box/config.json.bak"
	NodesFile         = "/etc/sing-box/nodes.txt"
	SubscriptionsFile = "/etc/sing-box/subscriptions.txt"
	StateFile         = "/etc/sing-box/state.json"
	CronFile          = "/etc/crontabs/root"
	Version           = "4.2.0"
	WebPort           = 7788

	// TUN configuration
	TunName  = "singtun0"
	TunAddr4 = "172.19.0.1/30"
	TunAddr6 = "fdfe:dcba:9876::1/126"
	TunMTU   = 9000
	DNSPort  = 5333
)

// Node represents a proxy node
type Node struct {
	Index   int    `json:"index"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Server  string `json:"server"`
	Port    int    `json:"port"`
	Raw     string `json:"-"`
	Latency int    `json:"latency"`
}

// AppState stores runtime state
type AppState struct {
	ProxyMode   string `json:"proxy_mode"`   // rule, global, direct
	CurrentNode string `json:"current_node"` // node name
	NodeIndex   int    `json:"node_index"`   // node index (1-based)
}

// ============ Full sing-box Config (1.12+) ============

type SingBoxConfig struct {
	Log          *LogConfig    `json:"log,omitempty"`
	DNS          *DNSConfig    `json:"dns,omitempty"`
	Inbounds     []Inbound     `json:"inbounds,omitempty"`
	Outbounds    []Outbound    `json:"outbounds,omitempty"`
	Route        *RouteConfig  `json:"route,omitempty"`
	Experimental *Experimental `json:"experimental,omitempty"`
}

type LogConfig struct {
	Level     string `json:"level,omitempty"`
	Timestamp bool   `json:"timestamp,omitempty"`
}

type DNSConfig struct {
	Servers        []DNSServer `json:"servers,omitempty"`
	Rules          []DNSRule   `json:"rules,omitempty"`
	Final          string      `json:"final,omitempty"`
	Strategy       string      `json:"strategy,omitempty"`
	ReverseMapping bool        `json:"reverse_mapping,omitempty"`
	CacheCapacity  int         `json:"cache_capacity,omitempty"`
}

type DNSServer struct {
	Type           string `json:"type"`
	Tag            string `json:"tag"`
	Server         string `json:"server,omitempty"`
	Detour         string `json:"detour,omitempty"`
	DomainResolver string `json:"domain_resolver,omitempty"`
	Inet4Range     string `json:"inet4_range,omitempty"`
	Inet6Range     string `json:"inet6_range,omitempty"`
}

type DNSRule struct {
	QueryType []string `json:"query_type,omitempty"`
	Domain    []string `json:"domain,omitempty"`
	RuleSet   []string `json:"rule_set,omitempty"`
	Server    string   `json:"server,omitempty"`
}

type Inbound struct {
	Type                     string   `json:"type"`
	Tag                      string   `json:"tag,omitempty"`
	InterfaceName            string   `json:"interface_name,omitempty"`
	Address                  []string `json:"address,omitempty"`
	MTU                      int      `json:"mtu,omitempty"`
	AutoRoute                bool     `json:"auto_route,omitempty"`
	AutoRedirect             bool     `json:"auto_redirect,omitempty"`
	Stack                    string   `json:"stack,omitempty"`
	Sniff                    bool     `json:"sniff,omitempty"`
	SniffOverrideDestination bool     `json:"sniff_override_destination,omitempty"`
	Listen                   string   `json:"listen,omitempty"`
	ListenPort               int      `json:"listen_port,omitempty"`
}

type Outbound struct {
	Type                      string           `json:"type"`
	Tag                       string           `json:"tag"`
	Outbounds                 []string         `json:"outbounds,omitempty"`
	Default                   string           `json:"default,omitempty"`
	InterruptExistConnections bool             `json:"interrupt_exist_connections,omitempty"`
	Server                    string           `json:"server,omitempty"`
	ServerPort                int              `json:"server_port,omitempty"`
	Method                    string           `json:"method,omitempty"`
	Password                  string           `json:"password,omitempty"`
	UUID                      string           `json:"uuid,omitempty"`
	Security                  string           `json:"security,omitempty"`
	AlterId                   int              `json:"alter_id,omitempty"`
	Flow                      string           `json:"flow,omitempty"`
	TLS                       *TLSConfig       `json:"tls,omitempty"`
	Transport                 *TransportConfig `json:"transport,omitempty"`
}

type RouteConfig struct {
	Rules                 []RouteRule `json:"rules,omitempty"`
	RuleSet               []RuleSet   `json:"rule_set,omitempty"`
	Final                 string      `json:"final,omitempty"`
	AutoDetectInterface   bool        `json:"auto_detect_interface,omitempty"`
	DefaultDomainResolver string      `json:"default_domain_resolver,omitempty"`
}

type RouteRule struct {
	Inbound     string   `json:"inbound,omitempty"`
	RuleSet     []string `json:"rule_set,omitempty"`
	Protocol    []string `json:"protocol,omitempty"`
	Port        []int    `json:"port,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	IPIsPrivate bool     `json:"ip_is_private,omitempty"`
	Action      string   `json:"action,omitempty"`
	Outbound    string   `json:"outbound,omitempty"`
}

type RuleSet struct {
	Tag            string `json:"tag"`
	Type           string `json:"type"`
	Format         string `json:"format"`
	URL            string `json:"url,omitempty"`
	Path           string `json:"path,omitempty"`
	DownloadDetour string `json:"download_detour,omitempty"`
	UpdateInterval string `json:"update_interval,omitempty"`
}

type Experimental struct {
	CacheFile *CacheFile `json:"cache_file,omitempty"`
	ClashAPI  *ClashAPI  `json:"clash_api,omitempty"`
}

type ClashAPI struct {
	ExternalController string `json:"external_controller,omitempty"`
}

type CacheFile struct {
	Enabled     bool   `json:"enabled,omitempty"`
	Path        string `json:"path,omitempty"`
	StoreFakeIP bool   `json:"store_fakeip,omitempty"`
	StoreRDRC   bool   `json:"store_rdrc,omitempty"`
}

type TLSConfig struct {
	Enabled    bool           `json:"enabled,omitempty"`
	ServerName string         `json:"server_name,omitempty"`
	UTLS       *UTLSConfig    `json:"utls,omitempty"`
	Reality    *RealityConfig `json:"reality,omitempty"`
}

type UTLSConfig struct {
	Enabled     bool   `json:"enabled,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type RealityConfig struct {
	Enabled   bool   `json:"enabled,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
	ShortID   string `json:"short_id,omitempty"`
}

type TransportConfig struct {
	Type        string              `json:"type,omitempty"`
	Path        string              `json:"path,omitempty"`
	Headers     map[string][]string `json:"headers,omitempty"`
	Host        []string            `json:"host,omitempty"` // for http transport
	ServiceName string              `json:"service_name,omitempty"`
}

// SpeedTestResult holds speed test results
type SpeedTestResult struct {
	Node        Node `json:"node"`
	TCPLatency  int  `json:"tcp_latency"`
	HTTPLatency int  `json:"http_latency"`
	Success     bool `json:"success"`
}

// StatusInfo for API response
type StatusInfo struct {
	Running      bool   `json:"running"`
	PID          string `json:"pid"`
	Memory       string `json:"memory"`
	TunCreated   bool   `json:"tun_created"`
	CurrentNode  string `json:"current_node"`
	NodeType     string `json:"node_type"`
	Server       string `json:"server"`
	Uptime       string `json:"uptime"`
	CronEnabled  bool   `json:"cron_enabled"`
	CronInterval int    `json:"cron_interval"`
	ProxyMode    string `json:"proxy_mode"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	switch os.Args[1] {
	case "status", "s":
		showStatus()
	case "list", "l":
		filter := ""
		if len(os.Args) > 2 {
			filter = os.Args[2]
		}
		listNodes(filter)
	case "switch", "sw":
		if len(os.Args) < 3 {
			errMsg("请指定节点编号")
			return
		}
		if n, err := strconv.Atoi(os.Args[2]); err == nil {
			switchNode(n)
		} else {
			errMsg("无效的节点编号")
		}
	case "mode", "m":
		handleModeCmd()
	case "update", "u":
		updateSubscription()
	case "test", "t":
		testConnection()
	case "speed", "sp":
		topN := 10
		if len(os.Args) > 2 {
			topN, _ = strconv.Atoi(os.Args[2])
		}
		speedTest(topN)
	case "restart", "r":
		restartSingbox()
	case "stop":
		stopSingbox()
	case "start":
		startSingbox()
	case "log":
		showLog()
	case "cron":
		handleCronCmd()
	case "auto":
		autoSwitch()
	case "check":
		checkAndSwitch()
	case "init":
		initConfig()
	case "web":
		startWebServer()
	case "version", "v":
		fmt.Printf("sing-box manager v%s\n", Version)
	default:
		printUsage()
	}
}

func printUsage() {
	fmt.Printf(`sing-box 管理工具 v%s (智能分流版)

用法: sb <命令> [参数]

命令:
  status, s          显示运行状态
  list, l [filter]   列出节点
  switch, sw <n>     切换到指定节点
  mode, m [模式]     切换代理模式 (rule/global/direct)
  update, u          更新订阅
  test, t            测试当前连接
  speed, sp [n]      测速显示最快n个节点
  restart, r         重启 sing-box
  start/stop         启动/停止
  log                查看日志
  cron [on N|off]    定时检测
  auto               自动切换到最快节点
  check              检测连接,失败自动切换
  init               初始化/重建配置
  web                启动Web管理界面
  version, v         显示版本

代理模式:
  rule    规则模式 (国内直连,国外代理) [默认]
  global  全局代理 (所有流量走代理)
  direct  直连模式 (所有流量直连)
`, Version)
}

func info(msg string)   { fmt.Printf("\033[32m[INFO]\033[0m %s\n", msg) }
func warn(msg string)   { fmt.Printf("\033[33m[WARN]\033[0m %s\n", msg) }
func errMsg(msg string) { fmt.Printf("\033[31m[ERROR]\033[0m %s\n", msg) }

// ============ Mode & Init Commands ============

func handleModeCmd() {
	state := loadState()

	if len(os.Args) < 3 {
		modeNames := map[string]string{"rule": "规则模式", "global": "全局代理", "direct": "直连模式"}
		info(fmt.Sprintf("当前模式: %s (%s)", state.ProxyMode, modeNames[state.ProxyMode]))
		return
	}

	newMode := strings.ToLower(os.Args[2])
	validModes := map[string]bool{"rule": true, "global": true, "direct": true}
	if !validModes[newMode] {
		errMsg("无效模式，可选: rule, global, direct")
		return
	}

	if newMode == state.ProxyMode {
		info("模式未改变")
		return
	}

	state.ProxyMode = newMode

	nodes, err := loadNodes()
	if err != nil {
		nodes = []Node{}
	}

	config := generateFullConfig(nodes, state)
	if err := saveAndRestart(config); err != nil {
		errMsg("切换失败: " + err.Error())
		return
	}
	saveState(state)

	modeNames := map[string]string{"rule": "规则模式", "global": "全局代理", "direct": "直连模式"}
	info(fmt.Sprintf("已切换到: %s", modeNames[newMode]))
}

func initConfig() {
	info("初始化配置...")

	nodes, err := loadNodes()
	if err != nil {
		warn("无节点，请先运行: sb update")
		nodes = []Node{}
	}

	state := loadState()
	if state.NodeIndex == 0 && len(nodes) > 0 {
		state.NodeIndex = 1
		state.CurrentNode = nodes[0].Name
	}

	config := generateFullConfig(nodes, state)
	if err := saveAndRestart(config); err != nil {
		errMsg("初始化失败: " + err.Error())
		return
	}
	saveState(state)

	info("配置初始化完成!")
	info(fmt.Sprintf("模式: %s", state.ProxyMode))
	if state.CurrentNode != "" {
		info(fmt.Sprintf("节点: %s", state.CurrentNode))
	}
}

// ============ Node Management ============

func loadNodes() ([]Node, error) {
	file, err := os.Open(NodesFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var nodes []Node
	scanner := bufio.NewScanner(file)
	idx := 1
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if node := parseNodeLine(line); node != nil {
			node.Index = idx
			nodes = append(nodes, *node)
			idx++
		}
	}
	return nodes, scanner.Err()
}

func parseNodeLine(line string) *Node {
	switch {
	case strings.HasPrefix(line, "ss://"):
		return parseSSNode(line)
	case strings.HasPrefix(line, "vless://"):
		return parseVLESSNode(line)
	case strings.HasPrefix(line, "vmess://"):
		return parseVMessNode(line)
	}
	return nil
}

func parseSSNode(link string) *Node {
	node := &Node{Type: "ss", Raw: link, Latency: -1}
	if idx := strings.LastIndex(link, "#"); idx != -1 {
		node.Name = urlDecode(link[idx+1:])
		link = link[:idx]
	}
	link = strings.TrimPrefix(link, "ss://")

	if atIdx := strings.LastIndex(link, "@"); atIdx != -1 {
		serverPart := link[atIdx+1:]
		if colonIdx := strings.LastIndex(serverPart, ":"); colonIdx != -1 {
			node.Server = serverPart[:colonIdx]
			node.Port, _ = strconv.Atoi(serverPart[colonIdx+1:])
		}
	} else {
		decoded, _ := base64Decode(link)
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
	return node
}

func parseVLESSNode(link string) *Node {
	node := &Node{Type: "vless", Raw: link, Latency: -1}
	if idx := strings.LastIndex(link, "#"); idx != -1 {
		node.Name = urlDecode(link[idx+1:])
		link = link[:idx]
	}
	link = strings.TrimPrefix(link, "vless://")

	if atIdx := strings.Index(link, "@"); atIdx != -1 {
		serverPart := link[atIdx+1:]
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
	return node
}

func parseVMessNode(link string) *Node {
	node := &Node{Type: "vmess", Raw: link, Latency: -1}
	link = strings.TrimPrefix(link, "vmess://")
	decoded, _ := base64Decode(link)

	var cfg map[string]interface{}
	if json.Unmarshal([]byte(decoded), &cfg) == nil {
		if ps, ok := cfg["ps"].(string); ok {
			node.Name = ps
		}
		if add, ok := cfg["add"].(string); ok {
			node.Server = add
		}
		if port, ok := cfg["port"].(float64); ok {
			node.Port = int(port)
		} else if port, ok := cfg["port"].(string); ok {
			node.Port, _ = strconv.Atoi(port)
		}
	}
	if node.Name == "" {
		node.Name = node.Server
	}
	return node
}

func base64Decode(s string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(s)
	}
	return string(decoded), err
}

func urlDecode(s string) string {
	if decoded, err := url.QueryUnescape(s); err == nil {
		return decoded
	}
	return s
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

// ============ Status & Info ============

func getStatusInfo() StatusInfo {
	status := StatusInfo{}

	out, err := exec.Command("pgrep", "-x", "sing-box").Output()
	if err == nil {
		status.Running = true
		status.PID = strings.TrimSpace(string(out))

		if mem, err := exec.Command("sh", "-c", fmt.Sprintf("cat /proc/%s/status 2>/dev/null | grep VmRSS | awk '{print $2, $3}'", status.PID)).Output(); err == nil {
			status.Memory = strings.TrimSpace(string(mem))
		}
		if uptime, err := exec.Command("sh", "-c", fmt.Sprintf("ps -o etime= -p %s 2>/dev/null", status.PID)).Output(); err == nil {
			status.Uptime = strings.TrimSpace(string(uptime))
		}
	}

	status.TunCreated = exec.Command("ip", "link", "show", TunName).Run() == nil

	// Load state
	state := loadState()
	status.ProxyMode = state.ProxyMode
	status.CurrentNode = state.CurrentNode

	// Get node type and server from config or nodes list
	if state.NodeIndex > 0 {
		if nodes, err := loadNodes(); err == nil && state.NodeIndex <= len(nodes) {
			node := nodes[state.NodeIndex-1]
			status.NodeType = strings.ToUpper(node.Type)
			status.Server = fmt.Sprintf("%s:%d", node.Server, node.Port)
		}
	}

	status.CronEnabled, status.CronInterval = getCronInfo()
	return status
}

func getCronInfo() (bool, int) {
	data, err := os.ReadFile(CronFile)
	if err != nil {
		return false, 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "sb check") && !strings.HasPrefix(line, "#") {
			if matches := regexp.MustCompile(`\*/(\d+)`).FindStringSubmatch(line); len(matches) > 1 {
				interval, _ := strconv.Atoi(matches[1])
				return true, interval
			}
		}
	}
	return false, 0
}

func showStatus() {
	s := getStatusInfo()
	fmt.Println("=== sing-box 状态 ===")
	if s.Running {
		fmt.Printf("状态: \033[32m运行中\033[0m (PID: %s)\n", s.PID)
		fmt.Printf("内存: %s\n", s.Memory)
		if s.Uptime != "" {
			fmt.Printf("运行时间: %s\n", s.Uptime)
		}
	} else {
		fmt.Println("状态: \033[31m未运行\033[0m")
	}
	fmt.Printf("TUN接口: %s\n", map[bool]string{true: "已创建", false: "未创建"}[s.TunCreated])

	modeNames := map[string]string{"rule": "规则模式", "global": "全局代理", "direct": "直连模式"}
	fmt.Printf("代理模式: %s\n", modeNames[s.ProxyMode])

	if s.CurrentNode != "" {
		fmt.Printf("当前节点: %s [%s]\n", s.CurrentNode, s.NodeType)
		fmt.Printf("服务器: %s\n", s.Server)
	}
	if s.CronEnabled {
		fmt.Printf("定时检测: 每 %d 分钟\n", s.CronInterval)
	}
}

func listNodes(filter string) {
	nodes, err := loadNodes()
	if err != nil {
		errMsg("无法加载节点，请先运行: sb update")
		return
	}
	if len(nodes) == 0 {
		warn("没有可用节点")
		return
	}

	filter = strings.ToLower(filter)
	count := 0
	fmt.Println("=== 节点列表 ===")
	for _, n := range nodes {
		if filter != "" && !strings.Contains(strings.ToLower(n.Name), filter) && !strings.Contains(strings.ToLower(n.Server), filter) {
			continue
		}
		typeTag := map[string]string{"ss": "SS", "vless": "VL", "vmess": "VM"}[n.Type]
		fmt.Printf("%4d) [%s] %-28s (%s:%d)\n", n.Index, typeTag, truncate(n.Name, 28), n.Server, n.Port)
		count++
	}
	fmt.Printf("共 %d 个节点\n", count)
}

// ============ State Management ============

func loadState() AppState {
	state := AppState{ProxyMode: "rule"}
	data, err := os.ReadFile(StateFile)
	if err == nil {
		json.Unmarshal(data, &state)
	}
	if state.ProxyMode == "" {
		state.ProxyMode = "rule"
	}
	return state
}

func saveState(state AppState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(StateFile, data, 0644)
}

// ============ Full Config Generation ============

func generateFullConfig(nodes []Node, state AppState) *SingBoxConfig {
	config := &SingBoxConfig{
		Log: &LogConfig{
			Level:     "debug",
			Timestamp: true,
		},
	}

	// Collect proxy server domains (exclude IPs)
	var proxyDomains []string
	for _, node := range nodes {
		if node.Server != "" && net.ParseIP(node.Server) == nil {
			proxyDomains = append(proxyDomains, node.Server)
		}
	}

	// Generate DNS with proxy domains excluded from FakeIP
	config.DNS = generateDNS(proxyDomains)

	// Generate inbounds
	config.Inbounds = generateInbounds()

	// Generate outbounds
	config.Outbounds = generateOutboundsWithSelector(nodes, state.NodeIndex)

	// Generate route
	config.Route = generateRoute(state.ProxyMode)

	// Cache file and Clash API
	config.Experimental = &Experimental{
		CacheFile: &CacheFile{
			Enabled:     true,
			Path:        ConfigDir + "/cache.db",
			StoreFakeIP: true,
			StoreRDRC:   true,
		},
		ClashAPI: &ClashAPI{
			ExternalController: "127.0.0.1:9090",
		},
	}

	return config
}

func generateDNS(proxyDomains []string) *DNSConfig {
	// Deduplicate proxy domains
	domainSet := make(map[string]bool)
	var uniqueDomains []string
	for _, d := range proxyDomains {
		if !domainSet[d] {
			domainSet[d] = true
			uniqueDomains = append(uniqueDomains, d)
		}
	}

	rules := []DNSRule{}

	// Rule 0: HTTPS/SVCB queries not supported by fakeip, use proxy-dns
	rules = append(rules, DNSRule{
		QueryType: []string{"HTTPS", "SVCB"},
		Server:    "proxy-dns",
	})

	// Rule 1: Proxy server domains must use local DNS (not FakeIP, not proxy)
	if len(uniqueDomains) > 0 {
		rules = append(rules, DNSRule{
			Domain: uniqueDomains,
			Server: "local-dns",
		})
	}

	// Rule 2: Local/private domains use local DNS
	rules = append(rules, DNSRule{
		Domain: []string{"localhost", "local", "lan", "internal", "home", "corp"},
		Server: "local-dns",
	})

	// Rule 3: China domains use local DNS
	rules = append(rules, DNSRule{
		RuleSet: []string{"geosite-cn", "china-domains"},
		Server:  "local-dns",
	})

	// Rule 4: Known foreign domains use FakeIP
	rules = append(rules, DNSRule{
		RuleSet: []string{"geosite-geolocation-!cn"},
		Server:  "fakeip",
	})

	return &DNSConfig{
		Servers: []DNSServer{
			// 国内 DNS (直连)
			{Type: "udp", Tag: "local-dns", Server: "223.5.5.5"},
			// 代理 DNS (通过代理查询，用于未知域名)
			{Type: "udp", Tag: "proxy-dns", Server: "1.1.1.1", Detour: "proxy"},
			// FakeIP (用于已知国外域名)
			{Type: "fakeip", Tag: "fakeip", Inet4Range: "198.18.0.0/15", Inet6Range: "fc00::/18"},
		},
		Rules:          rules,
		Final:          "proxy-dns",
		Strategy:       "prefer_ipv4", // 优先 IPv4，兼容性更好
		ReverseMapping: true,          // 日志显示域名而非 IP
		CacheCapacity:  50000,         // 缓存 5 万条记录
	}
}

func generateInbounds() []Inbound {
	return []Inbound{
		// DNS 入站 - 监听端口，供 dnsmasq 转发
		{
			Type:       "direct",
			Tag:        "dns-in",
			Listen:     "127.0.0.1",
			ListenPort: DNSPort,
		},
		// TUN 入站 - 使用 auto_route + auto_redirect (sing-box 1.10+)
		// auto_redirect 会自动插入 nftables 规则到 OpenWrt fw4 表
		{
			Type:                     "tun",
			Tag:                      "tun-in",
			InterfaceName:            TunName,
			Address:                  []string{TunAddr4, TunAddr6},
			MTU:                      TunMTU,
			AutoRoute:                true,
			AutoRedirect:             true,
			Stack:                    "mixed",
			Sniff:                    true,
			SniffOverrideDestination: true,
		},
	}
}

func generateOutboundsWithSelector(nodes []Node, selectedIndex int) []Outbound {
	outbounds := []Outbound{}

	// Collect node tags
	nodeTags := make([]string, 0, len(nodes))
	for _, node := range nodes {
		nodeTags = append(nodeTags, fmt.Sprintf("node-%d", node.Index))
	}

	// Default selection
	defaultNode := ""
	if selectedIndex > 0 && selectedIndex <= len(nodes) {
		defaultNode = fmt.Sprintf("node-%d", selectedIndex)
	} else if len(nodeTags) > 0 {
		defaultNode = nodeTags[0]
	}

	// 1. Selector outbound (不使用 urltest，避免启动时大量并发测速)
	if len(nodeTags) > 0 {
		selector := Outbound{
			Type:                      "selector",
			Tag:                       "proxy",
			Outbounds:                 nodeTags,
			Default:                   defaultNode,
			InterruptExistConnections: true,
		}
		outbounds = append(outbounds, selector)
	} else {
		// No nodes, use direct as proxy
		outbounds = append(outbounds, Outbound{
			Type: "direct",
			Tag:  "proxy",
		})
	}

	// 2. Direct outbound
	outbounds = append(outbounds, Outbound{
		Type: "direct",
		Tag:  "direct",
	})

	// 3. Node outbounds
	for _, node := range nodes {
		if ob := nodeToOutbound(node); ob != nil {
			outbounds = append(outbounds, *ob)
		}
	}

	return outbounds
}

func nodeToOutbound(node Node) *Outbound {
	tag := fmt.Sprintf("node-%d", node.Index)

	switch node.Type {
	case "ss":
		return parseSSToOutbound(node, tag)
	case "vless":
		return parseVLESSToOutbound(node, tag)
	case "vmess":
		return parseVMessToOutbound(node, tag)
	}
	return nil
}

func parseSSToOutbound(node Node, tag string) *Outbound {
	link := node.Raw
	if idx := strings.LastIndex(link, "#"); idx != -1 {
		link = link[:idx]
	}
	link = strings.TrimPrefix(link, "ss://")

	var method, password string
	if atIdx := strings.LastIndex(link, "@"); atIdx != -1 {
		if decoded, err := base64Decode(link[:atIdx]); err == nil {
			if parts := strings.SplitN(decoded, ":", 2); len(parts) == 2 {
				method, password = parts[0], parts[1]
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

func parseVLESSToOutbound(node Node, tag string) *Outbound {
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
		for _, pair := range strings.Split(link[qIdx+1:], "&") {
			if kv := strings.SplitN(pair, "=", 2); len(kv) == 2 {
				params[kv[0]] = urlDecode(kv[1])
			}
		}
	}

	out := &Outbound{
		Type:       "vless",
		Tag:        tag,
		Server:     node.Server,
		ServerPort: node.Port,
		UUID:       uuid,
		Flow:       params["flow"],
	}

	if security := params["security"]; security == "reality" || security == "tls" {
		out.TLS = &TLSConfig{Enabled: true, ServerName: params["sni"]}
		if fp := params["fp"]; fp != "" {
			out.TLS.UTLS = &UTLSConfig{Enabled: true, Fingerprint: fp}
		}
		if security == "reality" {
			out.TLS.Reality = &RealityConfig{Enabled: true, PublicKey: params["pbk"], ShortID: params["sid"]}
		}
	}

	if t := params["type"]; t != "" && t != "tcp" {
		out.Transport = buildTransport(t, params["path"], params["host"])
	}

	return out
}

func parseVMessToOutbound(node Node, tag string) *Outbound {
	decoded, _ := base64Decode(strings.TrimPrefix(node.Raw, "vmess://"))
	var cfg map[string]interface{}
	if json.Unmarshal([]byte(decoded), &cfg) != nil {
		return nil
	}

	out := &Outbound{
		Type:     "vmess",
		Tag:      tag,
		Security: "auto",
		AlterId:  0,
	}

	if add, ok := cfg["add"].(string); ok {
		out.Server = add
	}
	if port, ok := cfg["port"].(float64); ok {
		out.ServerPort = int(port)
	} else if port, ok := cfg["port"].(string); ok {
		out.ServerPort, _ = strconv.Atoi(port)
	}
	if id, ok := cfg["id"].(string); ok {
		out.UUID = id
	}
	if aid, ok := cfg["aid"].(float64); ok {
		out.AlterId = int(aid)
	}
	if tls, _ := cfg["tls"].(string); tls == "tls" {
		out.TLS = &TLSConfig{Enabled: true}
	}
	if net, _ := cfg["net"].(string); net != "" && net != "tcp" {
		path, _ := cfg["path"].(string)
		host, _ := cfg["host"].(string)
		out.Transport = buildTransport(net, path, host)
	}

	return out
}

func buildTransport(transportType, path, host string) *TransportConfig {
	tc := &TransportConfig{Type: transportType}

	if path != "" {
		tc.Path = path
	}

	if host != "" {
		switch transportType {
		case "ws", "websocket":
			// WebSocket: host goes in headers
			tc.Headers = map[string][]string{"Host": {host}}
		case "http", "h2":
			// HTTP/H2: host is an array
			tc.Host = []string{host}
		case "grpc":
			// gRPC: use service_name, not host
			if tc.ServiceName == "" && path != "" {
				tc.ServiceName = path
				tc.Path = ""
			}
		}
	}

	return tc
}

func generateRoute(mode string) *RouteConfig {
	route := &RouteConfig{
		AutoDetectInterface:   true,
		DefaultDomainResolver: "local-dns",
	}

	// Set final based on mode
	switch mode {
	case "direct":
		route.Final = "direct"
	case "global":
		route.Final = "proxy"
	default: // rule
		route.Final = "proxy"
	}

	// Route rules (order matters!)
	route.Rules = []RouteRule{
		// 1. DNS inbound hijacking
		{Inbound: "dns-in", Action: "hijack-dns"},
		// 2. Sniff protocol
		{Action: "sniff"},
		// 3. Resolve FakeIP to real IP for accurate geoip matching
		// This is CRITICAL for BGP IP rules to work with FakeIP
		{Action: "resolve"},
		// 4. DNS hijacking (from TUN)
		{Protocol: []string{"dns"}, Action: "hijack-dns"},
		// 5. Private networks direct
		{IPIsPrivate: true, Action: "route", Outbound: "direct"},
		// 6. China domains direct
		{RuleSet: []string{"geosite-cn", "china-domains"}, Action: "route", Outbound: "direct"},
		// 7. China IPs direct (BGP-based, more accurate)
		{RuleSet: []string{"chnroutes-bgp"}, Action: "route", Outbound: "direct"},
	}

	// Remote rule sets
	route.RuleSet = []RuleSet{
		{
			Tag: "chnroutes-bgp", Type: "remote", Format: "binary",
			URL:            "https://testingcf.jsdelivr.net/gh/Dreista/sing-box-rule-set-cn@rule-set/chnroutes.txt.srs",
			DownloadDetour: "direct", UpdateInterval: "1d",
		},
		{
			Tag: "china-domains", Type: "remote", Format: "binary",
			URL:            "https://testingcf.jsdelivr.net/gh/Dreista/sing-box-rule-set-cn@rule-set/accelerated-domains.china.conf.srs",
			DownloadDetour: "direct", UpdateInterval: "1d",
		},
		{
			Tag: "geosite-cn", Type: "remote", Format: "binary",
			URL:            "https://testingcf.jsdelivr.net/gh/SagerNet/sing-geosite@rule-set/geosite-cn.srs",
			DownloadDetour: "direct", UpdateInterval: "1d",
		},
		{
			Tag: "geosite-geolocation-!cn", Type: "remote", Format: "binary",
			URL:            "https://testingcf.jsdelivr.net/gh/SagerNet/sing-geosite@rule-set/geosite-geolocation-!cn.srs",
			DownloadDetour: "direct", UpdateInterval: "1d",
		},
	}

	return route
}

func saveAndRestart(config *SingBoxConfig) error {
	// Backup old config
	if oldData, err := os.ReadFile(ConfigFile); err == nil {
		os.WriteFile(BackupFile, oldData, 0644)
	}

	// Save new config
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(ConfigFile, data, 0644); err != nil {
		return err
	}

	// Restart sing-box
	restartSingbox()

	return nil
}

// ============ Node Switching ============

func switchNode(nodeNum int) bool {
	nodes, err := loadNodes()
	if err != nil || nodeNum < 1 || nodeNum > len(nodes) {
		errMsg("无效节点")
		return false
	}

	node := nodes[nodeNum-1]
	info(fmt.Sprintf("切换到: %s [%s]", node.Name, strings.ToUpper(node.Type)))

	// Load and update state
	state := loadState()
	state.NodeIndex = nodeNum
	state.CurrentNode = node.Name

	// Generate full config
	config := generateFullConfig(nodes, state)

	// Save config and restart
	if err := saveAndRestart(config); err != nil {
		errMsg("保存配置失败: " + err.Error())
		return false
	}

	// Save state
	saveState(state)

	time.Sleep(2 * time.Second)

	if testConnectionQuiet() {
		info("切换成功!")
		return true
	}

	warn("连接测试失败，回滚...")
	if backupData, err := os.ReadFile(BackupFile); err == nil {
		os.WriteFile(ConfigFile, backupData, 0644)
		restartSingbox()
	}
	return false
}

func switchNodeQuiet(node Node) bool {
	nodes, _ := loadNodes()
	state := loadState()
	state.NodeIndex = node.Index
	state.CurrentNode = node.Name

	config := generateFullConfig(nodes, state)

	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(ConfigFile, data, 0644)
	saveState(state)

	exec.Command("killall", "sing-box").Run()
	time.Sleep(500 * time.Millisecond)

	exec.Command("sh", "-c", "sing-box run -c "+ConfigFile+" -D "+ConfigDir+" >> /var/log/sing-box.log 2>&1 &").Run()

	// Wait for TUN
	for i := 0; i < 10; i++ {
		time.Sleep(300 * time.Millisecond)
		if exec.Command("ip", "link", "show", TunName).Run() == nil {
			break
		}
	}

	return exec.Command("pgrep", "-x", "sing-box").Run() == nil
}

// ============ Service Control ============

func stopSingbox() {
	exec.Command("killall", "sing-box").Run()
	time.Sleep(time.Second)
	info("sing-box 已停止")
}

func startSingbox() {
	exec.Command("sh", "-c", "sing-box run -c "+ConfigFile+" -D "+ConfigDir+" >> /var/log/sing-box.log 2>&1 &").Run()

	// Wait for TUN interface to be created
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		if exec.Command("ip", "link", "show", TunName).Run() == nil {
			break
		}
	}

	if exec.Command("pgrep", "-x", "sing-box").Run() == nil {
		info("sing-box 启动成功")
	} else {
		errMsg("sing-box 启动失败")
	}
}

func restartSingbox() {
	exec.Command("killall", "sing-box").Run()
	time.Sleep(time.Second)
	startSingbox()
}

// ============ Connection Testing ============

func testConnection() {
	info("测试连接...")
	start := time.Now()
	if testConnectionQuiet() {
		info(fmt.Sprintf("连接正常! 延迟: %d ms", time.Since(start).Milliseconds()))
	} else {
		errMsg("连接失败")
	}
}

func testConnectionQuiet() bool {
	client := &http.Client{Timeout: 10 * time.Second}
	for _, u := range []string{"https://www.google.com/generate_204", "https://cp.cloudflare.com/"} {
		if resp, err := client.Get(u); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 204 || resp.StatusCode == 200 {
				return true
			}
		}
	}
	return false
}

func measureHTTPLatency(timeout time.Duration) (int, bool) {
	client := &http.Client{Timeout: timeout}
	start := time.Now()
	resp, err := client.Get("https://www.google.com/generate_204")
	if err != nil {
		start = time.Now()
		resp, err = client.Get("https://cp.cloudflare.com/")
	}
	if err != nil {
		return -1, false
	}
	resp.Body.Close()
	return int(time.Since(start).Milliseconds()), true
}

func measureTCPLatency(server string, port int, timeout time.Duration) (int, bool) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", server, port), timeout)
	if err != nil {
		return -1, false
	}
	conn.Close()
	return int(time.Since(start).Milliseconds()), true
}

// ============ Speed Test ============

func speedTest(topN int) {
	nodes, err := loadNodes()
	if err != nil {
		errMsg("无法加载节点")
		return
	}

	testCount := topN
	if testCount > len(nodes) {
		testCount = len(nodes)
	}

	info(fmt.Sprintf("测速前 %d 个节点...", testCount))

	currentConfig, _ := os.ReadFile(ConfigFile)
	currentState, _ := os.ReadFile(StateFile)

	var results []SpeedTestResult
	for i := 0; i < testCount; i++ {
		node := nodes[i]
		fmt.Printf("\r[%d/%d] 测试: %-30s", i+1, testCount, truncate(node.Name, 30))

		result := SpeedTestResult{Node: node}
		if switchNodeQuiet(node) {
			time.Sleep(time.Second)
			if latency, ok := measureHTTPLatency(8 * time.Second); ok {
				result.HTTPLatency = latency
				result.Success = true
			}
		}
		results = append(results, result)
	}
	fmt.Println()

	// Restore
	os.WriteFile(ConfigFile, currentConfig, 0644)
	os.WriteFile(StateFile, currentState, 0644)
	restartSingbox()

	// Filter and sort
	var successful []SpeedTestResult
	for _, r := range results {
		if r.Success && r.HTTPLatency > 0 {
			successful = append(successful, r)
		}
	}
	sort.Slice(successful, func(i, j int) bool {
		return successful[i].HTTPLatency < successful[j].HTTPLatency
	})

	// Display
	fmt.Printf("\n=== 测速结果 ===\n")
	fmt.Printf("%-4s %-4s %-30s %-10s\n", "排名", "编号", "节点名称", "延迟")
	fmt.Println(strings.Repeat("-", 55))

	for i, r := range successful {
		color := "\033[32m" // green
		if r.HTTPLatency >= 600 {
			color = "\033[31m" // red
		} else if r.HTTPLatency >= 300 {
			color = "\033[33m" // yellow
		}
		fmt.Printf("%-4d %-4d %-30s %s%d ms\033[0m\n", i+1, r.Node.Index, truncate(r.Node.Name, 30), color, r.HTTPLatency)
	}

	fmt.Printf("\n可用: %d / %d\n", len(successful), testCount)
	if len(successful) > 0 {
		info(fmt.Sprintf("最快: #%d %s (%d ms)", successful[0].Node.Index, successful[0].Node.Name, successful[0].HTTPLatency))
	}
}

func autoSwitch() {
	nodes, err := loadNodes()
	if err != nil {
		errMsg("无法加载节点")
		return
	}

	info("测速并自动切换...")

	// TCP test all nodes concurrently
	results := make([]SpeedTestResult, len(nodes))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 30)

	for i, node := range nodes {
		wg.Add(1)
		go func(idx int, n Node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if latency, ok := measureTCPLatency(n.Server, n.Port, 3*time.Second); ok {
				results[idx] = SpeedTestResult{Node: n, TCPLatency: latency, Success: true}
			} else {
				results[idx] = SpeedTestResult{Node: n}
			}
		}(i, node)
	}
	wg.Wait()

	// Sort by TCP latency
	var successful []SpeedTestResult
	for _, r := range results {
		if r.Success {
			successful = append(successful, r)
		}
	}
	sort.Slice(successful, func(i, j int) bool {
		return successful[i].TCPLatency < successful[j].TCPLatency
	})

	// Try top 5
	for i := 0; i < 5 && i < len(successful); i++ {
		node := successful[i].Node
		info(fmt.Sprintf("尝试: #%d %s", node.Index, node.Name))
		if switchNode(node.Index) {
			return
		}
	}
	errMsg("所有快速节点均不可用")
}

func checkAndSwitch() {
	info("检测连接...")
	for i := 0; i < 3; i++ {
		if testConnectionQuiet() {
			info("连接正常")
			return
		}
		if i < 2 {
			warn(fmt.Sprintf("重试 %d/3...", i+2))
			time.Sleep(3 * time.Second)
		}
	}
	warn("连接失败，自动切换...")
	autoSwitch()
}

// ============ Subscription ============

func updateSubscription() {
	info("更新订阅...")

	data, err := os.ReadFile(SubscriptionsFile)
	if err != nil {
		errMsg("无法读取订阅文件")
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var allNodes []string

	for _, subURL := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		subURL = strings.TrimSpace(subURL)
		if subURL == "" || strings.HasPrefix(subURL, "#") {
			continue
		}

		info(fmt.Sprintf("获取: %s", truncate(subURL, 50)))
		resp, err := client.Get(subURL)
		if err != nil {
			warn("获取失败")
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		content := string(body)

		// Try base64 decode first
		if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(content)); err == nil {
			content = string(decoded)
		}

		// Check if it's Clash YAML format
		if strings.Contains(content, "proxies:") {
			nodes := parseClashYAML(content)
			allNodes = append(allNodes, nodes...)
		} else {
			// Standard URI format
			for _, line := range strings.Split(content, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "ss://") || strings.HasPrefix(line, "vmess://") || strings.HasPrefix(line, "vless://") {
					allNodes = append(allNodes, line)
				}
			}
		}
	}

	if len(allNodes) == 0 {
		errMsg("未找到节点")
		return
	}

	os.WriteFile(NodesFile, []byte(strings.Join(allNodes, "\n")), 0644)
	info(fmt.Sprintf("更新完成! 共 %d 个节点", len(allNodes)))
}

func parseClashYAML(content string) []string {
	var nodes []string
	lines := strings.Split(content, "\n")
	inProxies := false
	currentProxy := make(map[string]string)
	indent := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if trimmed == "proxies:" {
			inProxies = true
			continue
		}

		if inProxies {
			// Check if we've left proxies section
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && strings.Contains(line, ":") && !strings.HasPrefix(trimmed, "-") {
				inProxies = false
				continue
			}

			// New proxy entry
			if strings.HasPrefix(trimmed, "- ") {
				// Save previous proxy
				if len(currentProxy) > 0 {
					if node := clashProxyToURI(currentProxy); node != "" {
						nodes = append(nodes, node)
					}
				}
				currentProxy = make(map[string]string)
				// Parse inline format: - {name: xxx, type: ss, ...}
				if strings.HasPrefix(trimmed, "- {") {
					parseClashInline(trimmed[2:], currentProxy)
				} else {
					// Parse: - name: xxx
					kv := strings.TrimPrefix(trimmed, "- ")
					if idx := strings.Index(kv, ":"); idx > 0 {
						k := strings.TrimSpace(kv[:idx])
						v := strings.TrimSpace(kv[idx+1:])
						currentProxy[k] = v
					}
				}
				indent = len(line) - len(strings.TrimLeft(line, " \t"))
			} else if len(currentProxy) > 0 {
				// Continue parsing current proxy
				currentIndent := len(line) - len(strings.TrimLeft(line, " \t"))
				if currentIndent > indent {
					if idx := strings.Index(trimmed, ":"); idx > 0 {
						k := strings.TrimSpace(trimmed[:idx])
						v := strings.TrimSpace(trimmed[idx+1:])
						currentProxy[k] = v
					}
				}
			}
		}
	}

	// Don't forget last proxy
	if len(currentProxy) > 0 {
		if node := clashProxyToURI(currentProxy); node != "" {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

func parseClashInline(s string, m map[string]string) {
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if idx := strings.Index(part, ":"); idx > 0 {
			k := strings.TrimSpace(part[:idx])
			v := strings.TrimSpace(part[idx+1:])
			m[k] = v
		}
	}
}

func clashProxyToURI(p map[string]string) string {
	proxyType := p["type"]
	name := p["name"]
	server := p["server"]
	port := p["port"]

	if server == "" || port == "" {
		return ""
	}

	switch proxyType {
	case "ss", "shadowsocks":
		method := p["cipher"]
		password := p["password"]
		if method == "" || password == "" {
			return ""
		}
		auth := base64.StdEncoding.EncodeToString([]byte(method + ":" + password))
		return fmt.Sprintf("ss://%s@%s:%s#%s", auth, server, port, url.QueryEscape(name))

	case "vmess":
		cfg := map[string]interface{}{
			"v":    "2",
			"ps":   name,
			"add":  server,
			"port": port,
			"id":   p["uuid"],
			"aid":  p["alterId"],
			"net":  p["network"],
			"type": "none",
			"host": p["ws-opts-headers-Host"],
			"path": p["ws-opts-path"],
			"tls":  p["tls"],
		}
		if cfg["aid"] == "" {
			cfg["aid"] = "0"
		}
		if cfg["net"] == "" {
			cfg["net"] = "tcp"
		}
		data, _ := json.Marshal(cfg)
		return "vmess://" + base64.StdEncoding.EncodeToString(data)

	case "vless":
		uuid := p["uuid"]
		if uuid == "" {
			return ""
		}
		params := url.Values{}
		if v := p["network"]; v != "" {
			params.Set("type", v)
		}
		if v := p["tls"]; v == "true" {
			params.Set("security", "tls")
		}
		if v := p["servername"]; v != "" {
			params.Set("sni", v)
		}
		if v := p["flow"]; v != "" {
			params.Set("flow", v)
		}
		uri := fmt.Sprintf("vless://%s@%s:%s", uuid, server, port)
		if len(params) > 0 {
			uri += "?" + params.Encode()
		}
		return uri + "#" + url.QueryEscape(name)
	}

	return ""
}

// ============ Cron ============

func handleCronCmd() {
	if len(os.Args) < 3 {
		enabled, interval := getCronInfo()
		if enabled {
			info(fmt.Sprintf("定时任务: 每 %d 分钟", interval))
		} else {
			info("定时任务: 未启用")
		}
		return
	}

	switch os.Args[2] {
	case "on":
		interval := 5
		if len(os.Args) > 3 {
			interval, _ = strconv.Atoi(os.Args[3])
		}
		if interval < 1 {
			interval = 5
		}
		setCron(true, interval)
		info(fmt.Sprintf("定时检测已开启: 每 %d 分钟", interval))
	case "off":
		setCron(false, 0)
		info("定时检测已关闭")
	}
}

func setCron(enable bool, interval int) {
	data, _ := os.ReadFile(CronFile)
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "sb check") {
			lines = append(lines, line)
		}
	}
	if enable {
		lines = append(lines, fmt.Sprintf("*/%d * * * * /usr/bin/sb check >> /var/log/sb-check.log 2>&1", interval))
	}
	os.WriteFile(CronFile, []byte(strings.Join(lines, "\n")), 0644)
	exec.Command("/etc/init.d/cron", "restart").Run()
}

func showLog() {
	cmd := exec.Command("tail", "-100", "/var/log/sing-box.log")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

// ============ Web Server ============

func startWebServer() {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", apiStatus)
	mux.HandleFunc("/api/nodes", apiNodes)
	mux.HandleFunc("/api/switch", apiSwitch)
	mux.HandleFunc("/api/test", apiTest)
	mux.HandleFunc("/api/speed", apiSpeed)
	mux.HandleFunc("/api/update", apiUpdate)
	mux.HandleFunc("/api/restart", apiRestart)
	mux.HandleFunc("/api/auto", apiAuto)
	mux.HandleFunc("/api/cron", apiCron)
	mux.HandleFunc("/api/logs", apiLogs)
	mux.HandleFunc("/api/logs/level", apiLogLevel)
	mux.HandleFunc("/api/cache/clear", apiCacheClear)
	mux.HandleFunc("/api/connections", apiConnections)
	mux.HandleFunc("/api/subscriptions", apiSubscriptions)
	mux.HandleFunc("/api/mode", apiMode)
	mux.HandleFunc("/", webUI)

	addr := fmt.Sprintf(":%d", WebPort)
	info(fmt.Sprintf("Web界面: http://0.0.0.0%s", addr))

	http.ListenAndServe(addr, mux)
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(data)
}

func apiStatus(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, getStatusInfo())
}

func apiNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := loadNodes()
	if err != nil {
		jsonResponse(w, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, nodes)
}

func apiSwitch(w http.ResponseWriter, r *http.Request) {
	nodeNum, _ := strconv.Atoi(r.URL.Query().Get("node"))
	nodes, err := loadNodes()
	if err != nil || nodeNum < 1 || nodeNum > len(nodes) {
		jsonResponse(w, map[string]bool{"success": false})
		return
	}

	node := nodes[nodeNum-1]
	state := loadState()
	state.NodeIndex = nodeNum
	state.CurrentNode = node.Name

	config := generateFullConfig(nodes, state)

	if err := saveAndRestart(config); err != nil {
		jsonResponse(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	saveState(state)

	time.Sleep(2 * time.Second)
	jsonResponse(w, map[string]bool{"success": testConnectionQuiet()})
}

func apiTest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	success := testConnectionQuiet()
	latency := -1
	if success {
		latency = int(time.Since(start).Milliseconds())
	}
	jsonResponse(w, map[string]interface{}{"success": success, "latency": latency})
}

func apiSpeed(w http.ResponseWriter, r *http.Request) {
	nodes, err := loadNodes()
	if err != nil {
		jsonResponse(w, []SpeedTestResult{})
		return
	}

	// TCP test all
	results := make([]SpeedTestResult, len(nodes))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 30)

	for i, node := range nodes {
		wg.Add(1)
		go func(idx int, n Node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if latency, ok := measureTCPLatency(n.Server, n.Port, 5*time.Second); ok {
				results[idx] = SpeedTestResult{Node: n, TCPLatency: latency, Success: true}
			} else {
				results[idx] = SpeedTestResult{Node: n}
			}
		}(i, node)
	}
	wg.Wait()

	// Sort and get top 5 for HTTP test
	var successful []SpeedTestResult
	for _, r := range results {
		if r.Success {
			successful = append(successful, r)
		}
	}
	sort.Slice(successful, func(i, j int) bool {
		return successful[i].TCPLatency < successful[j].TCPLatency
	})

	testCount := 5
	if testCount > len(successful) {
		testCount = len(successful)
	}

	if testCount > 0 {
		currentConfig, _ := os.ReadFile(ConfigFile)
		currentState, _ := os.ReadFile(StateFile)

		for i := 0; i < testCount; i++ {
			if switchNodeQuiet(successful[i].Node) {
				time.Sleep(time.Second)
				if latency, ok := measureHTTPLatency(10 * time.Second); ok {
					successful[i].HTTPLatency = latency
				}
			}
		}

		os.WriteFile(ConfigFile, currentConfig, 0644)
		os.WriteFile(StateFile, currentState, 0644)
		restartSingbox()

		sort.Slice(successful[:testCount], func(i, j int) bool {
			if successful[i].HTTPLatency <= 0 {
				return false
			}
			if successful[j].HTTPLatency <= 0 {
				return true
			}
			return successful[i].HTTPLatency < successful[j].HTTPLatency
		})
	}

	jsonResponse(w, successful)
}

func apiUpdate(w http.ResponseWriter, r *http.Request) {
	go updateSubscription()
	jsonResponse(w, map[string]bool{"success": true})
}

func apiRestart(w http.ResponseWriter, r *http.Request) {
	go restartSingbox()
	jsonResponse(w, map[string]bool{"success": true})
}

func apiAuto(w http.ResponseWriter, r *http.Request) {
	go autoSwitch()
	jsonResponse(w, map[string]bool{"success": true})
}

func apiCron(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("action")
	switch action {
	case "on":
		interval, _ := strconv.Atoi(r.URL.Query().Get("interval"))
		if interval < 1 {
			interval = 5
		}
		setCron(true, interval)
		jsonResponse(w, map[string]interface{}{"success": true, "interval": interval})
	case "off":
		setCron(false, 0)
		jsonResponse(w, map[string]bool{"success": true})
	default:
		enabled, interval := getCronInfo()
		jsonResponse(w, map[string]interface{}{"enabled": enabled, "interval": interval})
	}
}

func apiLogs(w http.ResponseWriter, r *http.Request) {
	data, _ := os.ReadFile("/var/log/sing-box.log")
	// Strip ANSI color codes
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	cleaned := ansiRegex.ReplaceAllString(string(data), "")
	// Convert UTC to local time (UTC+8)
	timeRegex := regexp.MustCompile(`\+0000 (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`)
	cleaned = timeRegex.ReplaceAllStringFunc(cleaned, func(match string) string {
		parts := timeRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		t, err := time.Parse("2006-01-02 15:04:05", parts[1])
		if err != nil {
			return match
		}
		local := t.Add(8 * time.Hour)
		return local.Format("2006-01-02 15:04:05")
	})
	lines := strings.Split(cleaned, "\n")
	if len(lines) > 100 {
		lines = lines[len(lines)-100:]
	}
	jsonResponse(w, map[string][]string{"logs": lines})
}

func apiLogLevel(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			Level string `json:"level"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		validLevels := map[string]bool{"trace": true, "debug": true, "info": true, "warn": true, "error": true}
		if !validLevels[req.Level] {
			jsonResponse(w, map[string]interface{}{"success": false, "error": "invalid level"})
			return
		}

		// Read current config
		data, err := os.ReadFile(ConfigFile)
		if err != nil {
			jsonResponse(w, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}

		var config SingBoxConfig
		if err := json.Unmarshal(data, &config); err != nil {
			jsonResponse(w, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}

		// Update log level
		if config.Log == nil {
			config.Log = &LogConfig{}
		}
		config.Log.Level = req.Level

		// Save and restart
		if err := saveAndRestart(&config); err != nil {
			jsonResponse(w, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}

		jsonResponse(w, map[string]interface{}{"success": true, "level": req.Level})
		return
	}

	// GET: return current level
	data, _ := os.ReadFile(ConfigFile)
	var config SingBoxConfig
	json.Unmarshal(data, &config)
	level := "info"
	if config.Log != nil && config.Log.Level != "" {
		level = config.Log.Level
	}
	jsonResponse(w, map[string]interface{}{"level": level})
}

func apiCacheClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonResponse(w, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}

	// Delete cache file and restart
	cacheFile := ConfigDir + "/cache.db"
	os.Remove(cacheFile)

	// Restart sing-box to apply
	restartSingbox()

	jsonResponse(w, map[string]interface{}{"success": true})
}

func apiConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method == "DELETE" {
		// Close connection(s)
		connID := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		url := "http://127.0.0.1:9090/connections"
		if connID != "" && connID != "/api/connections" {
			url += "/" + connID
		}
		req, _ := http.NewRequest("DELETE", url, nil)
		http.DefaultClient.Do(req)
		jsonResponse(w, map[string]bool{"success": true})
		return
	}

	// GET connections
	resp, err := http.Get("http://127.0.0.1:9090/connections")
	if err != nil {
		jsonResponse(w, map[string]interface{}{"connections": []interface{}{}, "downloadTotal": 0, "uploadTotal": 0})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(body)
}

func apiSubscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			URLs []string `json:"urls"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		os.WriteFile(SubscriptionsFile, []byte(strings.Join(req.URLs, "\n")), 0644)
		jsonResponse(w, map[string]bool{"success": true})
		return
	}

	data, _ := os.ReadFile(SubscriptionsFile)
	var urls []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}
	jsonResponse(w, map[string][]string{"urls": urls})
}

func apiMode(w http.ResponseWriter, r *http.Request) {
	state := loadState()

	if r.Method == "POST" || r.URL.Query().Get("mode") != "" {
		newMode := r.URL.Query().Get("mode")
		if newMode == "" {
			var req struct {
				Mode string `json:"mode"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			newMode = req.Mode
		}

		validModes := map[string]bool{"rule": true, "global": true, "direct": true}
		if !validModes[newMode] {
			jsonResponse(w, map[string]interface{}{"success": false, "error": "invalid mode"})
			return
		}

		state.ProxyMode = newMode
		nodes, _ := loadNodes()
		config := generateFullConfig(nodes, state)

		if err := saveAndRestart(config); err != nil {
			jsonResponse(w, map[string]interface{}{"success": false, "error": err.Error()})
			return
		}
		saveState(state)

		jsonResponse(w, map[string]interface{}{"success": true, "mode": newMode})
		return
	}

	jsonResponse(w, map[string]string{"mode": state.ProxyMode})
}

func webUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(webHTML))
}

var webHTML = `<!DOCTYPE html>
<html lang="zh-CN" class="dark">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>sing-box Manager</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
    <script>
        tailwind.config = {
            darkMode: 'class',
            theme: {
                extend: {
                    colors: {
                        dark: { bg: '#0f172a', card: '#1e293b', border: '#334155', text: '#e2e8f0', muted: '#94a3b8' }
                    }
                }
            }
        }
    </script>
    <style>
        [x-cloak] { display: none !important; }
        .spinner { border: 2px solid #334155; border-top-color: #3b82f6; border-radius: 50%; width: 16px; height: 16px; animation: spin 1s linear infinite; }
        @keyframes spin { to { transform: rotate(360deg); } }
        .log-line { font-family: ui-monospace, monospace; font-size: 12px; padding: 2px 0; }
        .log-error { color: #f87171; }
        .log-warn { color: #fbbf24; }
        .log-info { color: #e2e8f0; }
        .log-debug { color: #60a5fa; }
    </style>
</head>
<body class="bg-dark-bg text-dark-text min-h-screen" x-data="app()" x-init="init()">
    <div class="flex h-screen">
        <!-- Sidebar -->
        <aside class="w-56 bg-dark-card border-r border-dark-border flex flex-col">
            <div class="p-4 border-b border-dark-border">
                <h1 class="text-lg font-bold text-blue-400">sing-box</h1>
                <p class="text-xs text-dark-muted">v4.2.0</p>
            </div>
            <nav class="flex-1 p-3 space-y-1">
                <button @click="page='dashboard'" :class="page==='dashboard' ? 'bg-blue-600 text-white' : 'text-dark-muted hover:bg-dark-border'" class="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-sm">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z"/></svg>
                    仪表盘
                </button>
                <button @click="page='nodes'" :class="page==='nodes' ? 'bg-blue-600 text-white' : 'text-dark-muted hover:bg-dark-border'" class="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-sm">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2"/></svg>
                    节点管理
                </button>
                <button @click="page='subscriptions'" :class="page==='subscriptions' ? 'bg-blue-600 text-white' : 'text-dark-muted hover:bg-dark-border'" class="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-sm">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/></svg>
                    订阅管理
                </button>
                <button @click="page='logs'" :class="page==='logs' ? 'bg-blue-600 text-white' : 'text-dark-muted hover:bg-dark-border'" class="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-sm">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/></svg>
                    日志
                </button>
                <button @click="page='connections'" :class="page==='connections' ? 'bg-blue-600 text-white' : 'text-dark-muted hover:bg-dark-border'" class="w-full flex items-center gap-2 px-3 py-2 rounded-lg text-sm">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"/></svg>
                    连接
                </button>
            </nav>
            <div class="p-3 border-t border-dark-border">
                <div class="flex items-center gap-2">
                    <div :class="status.running ? 'bg-green-500' : 'bg-gray-500'" class="w-2 h-2 rounded-full"></div>
                    <span class="text-xs" x-text="status.running ? '运行中' : '已停止'"></span>
                </div>
            </div>
        </aside>

        <!-- Main -->
        <main class="flex-1 overflow-auto p-6">
            <!-- Dashboard -->
            <div x-show="page==='dashboard'" x-cloak>
                <h2 class="text-xl font-bold mb-4">仪表盘</h2>
                <div class="grid grid-cols-4 gap-4 mb-6">
                    <div class="bg-dark-card rounded-lg p-4 border border-dark-border">
                        <p class="text-dark-muted text-xs mb-1">服务状态</p>
                        <p class="text-lg font-bold" :class="status.running ? 'text-green-400' : 'text-gray-400'" x-text="status.running ? '运行中' : '已停止'"></p>
                    </div>
                    <div class="bg-dark-card rounded-lg p-4 border border-dark-border">
                        <p class="text-dark-muted text-xs mb-1">内存</p>
                        <p class="text-lg font-bold" x-text="status.memory || '--'"></p>
                    </div>
                    <div class="bg-dark-card rounded-lg p-4 border border-dark-border">
                        <p class="text-dark-muted text-xs mb-1">TUN</p>
                        <p class="text-lg font-bold" :class="status.tun_created ? 'text-green-400' : 'text-red-400'" x-text="status.tun_created ? '正常' : '异常'"></p>
                    </div>
                    <div class="bg-dark-card rounded-lg p-4 border border-dark-border">
                        <p class="text-dark-muted text-xs mb-1">运行时间</p>
                        <p class="text-lg font-bold" x-text="status.uptime || '--'"></p>
                    </div>
                </div>

                <div class="grid grid-cols-2 gap-6">
                    <div class="bg-dark-card rounded-lg p-4 border border-dark-border">
                        <h3 class="font-semibold mb-3">当前节点</h3>
                        <p class="text-blue-400 font-medium" x-text="status.current_node || '未选择'"></p>
                        <p class="text-dark-muted text-sm" x-text="status.server || '-'"></p>
                        <div class="flex gap-2 mt-4">
                            <button @click="testConn()" :disabled="testing" class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 rounded text-sm">
                                <span x-show="!testing">测试连接</span>
                                <span x-show="testing" class="flex items-center gap-1"><span class="spinner"></span>测试中</span>
                            </button>
                            <button @click="restart()" class="px-3 py-1.5 bg-dark-border hover:bg-dark-muted/30 rounded text-sm">重启</button>
                        </div>
                        <p x-show="testResult" class="mt-2 text-sm" :class="testResult?.success ? 'text-green-400' : 'text-red-400'" x-text="testResult?.success ? '连接正常 ' + testResult.latency + 'ms' : '连接失败'"></p>
                    </div>

                    <div class="bg-dark-card rounded-lg p-4 border border-dark-border">
                        <h3 class="font-semibold mb-3">代理模式</h3>
                        <div class="flex gap-2">
                            <button @click="setMode('rule')" :class="status.proxy_mode==='rule' ? 'bg-blue-600' : 'bg-dark-border hover:bg-dark-muted/30'" class="flex-1 py-2 rounded text-sm">规则</button>
                            <button @click="setMode('global')" :class="status.proxy_mode==='global' ? 'bg-blue-600' : 'bg-dark-border hover:bg-dark-muted/30'" class="flex-1 py-2 rounded text-sm">全局</button>
                            <button @click="setMode('direct')" :class="status.proxy_mode==='direct' ? 'bg-blue-600' : 'bg-dark-border hover:bg-dark-muted/30'" class="flex-1 py-2 rounded text-sm">直连</button>
                        </div>
                        <p class="text-dark-muted text-xs mt-2" x-text="modeDesc[status.proxy_mode] || ''"></p>
                    </div>
                </div>

                <div class="bg-dark-card rounded-lg p-4 border border-dark-border mt-6">
                    <h3 class="font-semibold mb-3">快捷操作</h3>
                    <div class="flex gap-2">
                        <button @click="updateSub()" :disabled="updating" class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 rounded text-sm">
                            <span x-show="!updating">更新订阅</span>
                            <span x-show="updating">更新中...</span>
                        </button>
                        <button @click="runSpeed()" :disabled="speeding" class="px-4 py-2 bg-dark-border hover:bg-dark-muted/30 disabled:opacity-50 rounded text-sm">
                            <span x-show="!speeding">测速</span>
                            <span x-show="speeding">测速中...</span>
                        </button>
                        <button @click="runAuto()" class="px-4 py-2 bg-green-600 hover:bg-green-700 rounded text-sm">自动选择</button>
                    </div>
                </div>
            </div>

            <!-- Nodes -->
            <div x-show="page==='nodes'" x-cloak>
                <div class="flex justify-between items-center mb-4">
                    <h2 class="text-xl font-bold">节点管理 (<span x-text="nodes.length"></span>)</h2>
                    <div class="flex gap-2">
                        <input type="text" x-model="search" placeholder="搜索..." class="px-3 py-1.5 bg-dark-card border border-dark-border rounded text-sm w-48">
                        <button @click="updateSub()" class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 rounded text-sm">刷新</button>
                    </div>
                </div>
                <div class="flex gap-2 mb-4">
                    <button @click="filter='all'" :class="filter==='all' ? 'bg-blue-600' : 'bg-dark-card'" class="px-3 py-1 rounded text-sm">全部</button>
                    <button @click="filter='ss'" :class="filter==='ss' ? 'bg-blue-600' : 'bg-dark-card'" class="px-3 py-1 rounded text-sm">SS</button>
                    <button @click="filter='vless'" :class="filter==='vless' ? 'bg-blue-600' : 'bg-dark-card'" class="px-3 py-1 rounded text-sm">VLESS</button>
                    <button @click="filter='vmess'" :class="filter==='vmess' ? 'bg-blue-600' : 'bg-dark-card'" class="px-3 py-1 rounded text-sm">VMess</button>
                </div>
                <div class="bg-dark-card rounded-lg border border-dark-border overflow-hidden">
                    <div class="max-h-[calc(100vh-220px)] overflow-y-auto">
                        <template x-for="node in filteredNodes" :key="node.index">
                            <div @click="switchNode(node.index)" :class="node.name === status.current_node ? 'bg-blue-600/20 border-l-2 border-blue-500' : 'hover:bg-dark-border/50'" class="flex items-center px-4 py-3 cursor-pointer border-b border-dark-border last:border-0">
                                <span class="w-10 text-dark-muted text-xs" x-text="'#'+node.index"></span>
                                <span class="px-2 py-0.5 rounded text-xs mr-3" :class="{'text-green-400 bg-green-900/30': node.type==='ss', 'text-yellow-400 bg-yellow-900/30': node.type==='vless', 'text-red-400 bg-red-900/30': node.type==='vmess'}" x-text="node.type.toUpperCase()"></span>
                                <span class="flex-1 truncate text-sm" x-text="node.name"></span>
                                <span x-show="node.latency > 0" class="text-xs px-2 py-0.5 rounded" :class="node.latency < 300 ? 'text-green-400' : node.latency < 600 ? 'text-yellow-400' : 'text-red-400'" x-text="node.latency + 'ms'"></span>
                            </div>
                        </template>
                        <div x-show="filteredNodes.length === 0" class="p-8 text-center text-dark-muted">暂无节点</div>
                    </div>
                </div>
            </div>

            <!-- Subscriptions -->
            <div x-show="page==='subscriptions'" x-cloak>
                <h2 class="text-xl font-bold mb-4">订阅管理</h2>
                <div class="bg-dark-card rounded-lg p-4 border border-dark-border">
                    <p class="text-dark-muted text-sm mb-2">每行一个订阅链接</p>
                    <textarea x-model="subUrls" rows="6" class="w-full px-3 py-2 bg-dark-bg border border-dark-border rounded text-sm font-mono" placeholder="https://..."></textarea>
                    <div class="flex gap-2 mt-3">
                        <button @click="saveSub()" class="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded text-sm">保存</button>
                        <button @click="updateSub()" :disabled="updating" class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 rounded text-sm">
                            <span x-show="!updating">更新订阅</span>
                            <span x-show="updating">更新中...</span>
                        </button>
                    </div>
                </div>
            </div>

            <!-- Logs -->
            <div x-show="page==='logs'" x-cloak class="h-full flex flex-col">
                <div class="flex justify-between items-center mb-4">
                    <h2 class="text-xl font-bold">日志</h2>
                    <div class="flex items-center gap-2">
                        <input type="text" x-model="logSearch" placeholder="搜索..." class="px-2 py-1.5 bg-dark-card border border-dark-border rounded text-sm w-40">
                        <select x-model="logFilter" class="px-2 py-1.5 bg-dark-card border border-dark-border rounded text-sm">
                            <option value="all">全部</option>
                            <option value="dns">DNS</option>
                            <option value="outbound">出站</option>
                            <option value="important">重要</option>
                        </select>
                        <select x-model="logLevel" @change="setLogLevel()" class="px-2 py-1.5 bg-dark-card border border-dark-border rounded text-sm">
                            <option value="trace">Trace</option>
                            <option value="debug">Debug</option>
                            <option value="info">Info</option>
                            <option value="warn">Warn</option>
                            <option value="error">Error</option>
                        </select>
                        <button @click="clearCache()" :disabled="clearing" class="px-3 py-1.5 bg-orange-600 hover:bg-orange-700 disabled:opacity-50 rounded text-sm">
                            <span x-show="!clearing">清DNS缓存</span>
                            <span x-show="clearing">清理中...</span>
                        </button>
                        <button @click="fetchLogs()" class="px-3 py-1.5 bg-dark-border hover:bg-dark-muted/30 rounded text-sm">刷新</button>
                    </div>
                </div>
                <div class="flex-1 bg-black rounded-lg p-4 overflow-auto font-mono text-xs">
                    <template x-for="(log, i) in filteredLogs" :key="i">
                        <div class="log-line" :class="getLogClass(log)" x-html="highlightLog(log)"></div>
                    </template>
                    <div x-show="filteredLogs.length === 0" class="text-dark-muted">暂无匹配日志</div>
                </div>
            </div>

            <!-- Connections -->
            <div x-show="page==='connections'" x-cloak class="h-full flex flex-col">
                <div class="flex justify-between items-center mb-4">
                    <h2 class="text-xl font-bold">连接 (<span x-text="connections.length"></span>)</h2>
                    <div class="flex items-center gap-2 text-sm">
                        <span class="text-green-400">↓ <span x-text="formatBytes(connStats.download)"></span></span>
                        <span class="text-blue-400">↑ <span x-text="formatBytes(connStats.upload)"></span></span>
                        <button @click="closeAllConns()" class="px-3 py-1.5 bg-red-600 hover:bg-red-700 rounded text-sm ml-2">断开全部</button>
                    </div>
                </div>
                <div class="flex-1 bg-dark-card rounded-lg border border-dark-border overflow-hidden">
                    <div class="overflow-auto h-full">
                        <table class="w-full text-sm">
                            <thead class="bg-dark-border sticky top-0">
                                <tr>
                                    <th class="px-3 py-2 text-left">主机</th>
                                    <th class="px-3 py-2 text-left">类型</th>
                                    <th class="px-3 py-2 text-left">出口</th>
                                    <th class="px-3 py-2 text-left">下载</th>
                                    <th class="px-3 py-2 text-left">上传</th>
                                    <th class="px-3 py-2 text-left">操作</th>
                                </tr>
                            </thead>
                            <tbody class="divide-y divide-dark-border">
                                <template x-for="conn in connections" :key="conn.id">
                                    <tr class="hover:bg-dark-border/30">
                                        <td class="px-3 py-2 truncate max-w-xs" x-text="conn.metadata?.host || conn.metadata?.destinationIP || '-'"></td>
                                        <td class="px-3 py-2"><span class="px-1.5 py-0.5 bg-dark-border rounded text-xs" x-text="conn.metadata?.network?.toUpperCase()"></span></td>
                                        <td class="px-3 py-2 text-blue-400" x-text="conn.chains?.[0] || '-'"></td>
                                        <td class="px-3 py-2 text-green-400" x-text="formatBytes(conn.download)"></td>
                                        <td class="px-3 py-2 text-blue-400" x-text="formatBytes(conn.upload)"></td>
                                        <td class="px-3 py-2"><button @click="closeConn(conn.id)" class="px-2 py-0.5 bg-red-600/30 hover:bg-red-600/50 text-red-400 rounded text-xs">断开</button></td>
                                    </tr>
                                </template>
                                <tr x-show="connections.length === 0"><td colspan="6" class="px-3 py-8 text-center text-dark-muted">暂无连接</td></tr>
                            </tbody>
                        </table>
                    </div>
                </div>
            </div>
        </main>
    </div>

    <!-- Toast -->
    <div class="fixed bottom-4 right-4 z-50 space-y-2">
        <template x-for="(t, i) in toasts" :key="i">
            <div :class="t.type==='error' ? 'bg-red-600' : t.type==='success' ? 'bg-green-600' : 'bg-blue-600'" class="px-4 py-2 rounded-lg text-sm shadow-lg" x-text="t.msg"></div>
        </template>
    </div>

    <script>
        function app() {
            return {
                page: 'dashboard',
                status: {},
                nodes: [],
                logs: [],
                subUrls: '',
                search: '',
                filter: 'all',
                testing: false,
                testResult: null,
                updating: false,
                speeding: false,
                logLevel: 'info',
                logFilter: 'all',
                logSearch: '',
                clearing: false,
                connections: [],
                connStats: { download: 0, upload: 0 },
                connInterval: null,
                logInterval: null,
                toasts: [],
                modeDesc: {
                    rule: '规则模式: 国内直连，国外代理',
                    global: '全局模式: 所有流量走代理',
                    direct: '直连模式: 所有流量直连'
                },

                async init() {
                    await this.fetchStatus();
                    await this.fetchNodes();
                    await this.fetchSubs();
                    await this.fetchLogLevel();
                    setInterval(() => this.fetchStatus(), 5000);
                    this.$watch('page', (p, old) => {
                        if (p === 'logs') { this.fetchLogs(); this.startLogPolling(); }
                        else if (old === 'logs') this.stopLogPolling();
                        if (p === 'connections') this.startConnPolling();
                        else if (old === 'connections') this.stopConnPolling();
                    });
                },

                async fetchStatus() {
                    try {
                        this.status = await (await fetch('/api/status')).json();
                    } catch(e) {}
                },

                async fetchNodes() {
                    try {
                        const data = await (await fetch('/api/nodes')).json();
                        if (Array.isArray(data)) this.nodes = data;
                    } catch(e) {}
                },

                async fetchSubs() {
                    try {
                        const data = await (await fetch('/api/subscriptions')).json();
                        this.subUrls = (data.urls || []).join('\n');
                    } catch(e) {}
                },

                async fetchLogs() {
                    try {
                        const data = await (await fetch('/api/logs')).json();
                        this.logs = data.logs || [];
                    } catch(e) {}
                },

                get filteredNodes() {
                    let list = this.nodes;
                    if (this.filter !== 'all') list = list.filter(n => n.type === this.filter);
                    if (this.search) {
                        const s = this.search.toLowerCase();
                        list = list.filter(n => n.name.toLowerCase().includes(s) || n.server.toLowerCase().includes(s));
                    }
                    return list;
                },

                async switchNode(idx) {
                    this.toast('切换中...', 'info');
                    try {
                        const d = await (await fetch('/api/switch?node=' + idx)).json();
                        this.toast(d.success ? '切换成功' : '切换失败', d.success ? 'success' : 'error');
                        await this.fetchStatus();
                    } catch(e) { this.toast('请求失败', 'error'); }
                },

                async setMode(mode) {
                    if (mode === this.status.proxy_mode) return;
                    this.toast('切换模式...', 'info');
                    try {
                        const d = await (await fetch('/api/mode?mode=' + mode)).json();
                        if (d.success) {
                            this.status.proxy_mode = mode;
                            this.toast('已切换', 'success');
                        }
                    } catch(e) { this.toast('请求失败', 'error'); }
                },

                async testConn() {
                    this.testing = true;
                    this.testResult = null;
                    try {
                        this.testResult = await (await fetch('/api/test')).json();
                    } catch(e) { this.testResult = { success: false }; }
                    this.testing = false;
                },

                async restart() {
                    this.toast('重启中...', 'info');
                    await fetch('/api/restart');
                    setTimeout(() => { this.fetchStatus(); this.toast('已重启', 'success'); }, 3000);
                },

                async updateSub() {
                    this.updating = true;
                    this.toast('更新中...', 'info');
                    await fetch('/api/update');
                    setTimeout(async () => {
                        await this.fetchNodes();
                        this.updating = false;
                        this.toast('更新完成', 'success');
                    }, 5000);
                },

                async saveSub() {
                    const urls = this.subUrls.split('\n').filter(u => u.trim());
                    await fetch('/api/subscriptions', {
                        method: 'POST',
                        headers: {'Content-Type': 'application/json'},
                        body: JSON.stringify({urls})
                    });
                    this.toast('已保存', 'success');
                },

                async runSpeed() {
                    this.speeding = true;
                    this.toast('测速中...', 'info');
                    try {
                        const results = await (await fetch('/api/speed')).json();
                        this.nodes.forEach(n => {
                            const r = results.find(x => x.node?.index === n.index);
                            if (r?.http_latency > 0) n.latency = r.http_latency;
                            else if (r?.tcp_latency > 0) n.latency = r.tcp_latency;
                        });
                        this.nodes.sort((a,b) => (a.latency<=0?9999:a.latency) - (b.latency<=0?9999:b.latency));
                        this.toast('测速完成', 'success');
                    } catch(e) { this.toast('测速失败', 'error'); }
                    this.speeding = false;
                },

                async runAuto() {
                    this.toast('自动选择中...', 'info');
                    await fetch('/api/auto');
                    setTimeout(() => { this.fetchStatus(); this.toast('已切换', 'success'); }, 3000);
                },

                async fetchLogLevel() {
                    try {
                        const data = await (await fetch('/api/logs/level')).json();
                        this.logLevel = data.level || 'info';
                    } catch(e) {}
                },

                async setLogLevel() {
                    try {
                        const d = await (await fetch('/api/logs/level', {
                            method: 'POST',
                            headers: {'Content-Type': 'application/json'},
                            body: JSON.stringify({level: this.logLevel})
                        })).json();
                        if (d.success) this.toast('日志级别已设置: ' + this.logLevel, 'success');
                        else this.toast('设置失败', 'error');
                    } catch(e) { this.toast('请求失败', 'error'); }
                },

                async clearCache() {
                    this.clearing = true;
                    try {
                        const d = await (await fetch('/api/cache/clear', { method: 'POST' })).json();
                        if (d.success) {
                            this.toast('DNS缓存已清理', 'success');
                            await this.fetchStatus();
                        } else this.toast('清理失败', 'error');
                    } catch(e) { this.toast('请求失败', 'error'); }
                    this.clearing = false;
                },

                get filteredLogs() {
                    let list = this.logs;
                    if (this.logFilter === 'dns') list = list.filter(l => /dns|exchanged|cached/i.test(l));
                    else if (this.logFilter === 'outbound') list = list.filter(l => /outbound|connection/i.test(l));
                    else if (this.logFilter === 'important') list = list.filter(l => /ERROR|WARN|selected|started|stopped/i.test(l));
                    if (this.logSearch.trim()) {
                        const s = this.logSearch.toLowerCase();
                        list = list.filter(l => l.toLowerCase().includes(s));
                    }
                    return list;
                },

                getLogClass(log) {
                    if (/ERROR|FATAL/i.test(log)) return 'log-error';
                    if (/WARN/i.test(log)) return 'log-warn';
                    if (/DEBUG/i.test(log)) return 'log-debug';
                    return 'log-info';
                },

                highlightLog(log) {
                    if (!this.logSearch.trim()) return this.escapeHtml(log);
                    const s = this.logSearch.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
                    return this.escapeHtml(log).replace(new RegExp('(' + s + ')', 'gi'), '<span class="bg-yellow-500/40 text-yellow-200">$1</span>');
                },

                escapeHtml(str) {
                    return str.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
                },

                formatBytes(bytes) {
                    if (!bytes) return '0 B';
                    const k = 1024, sizes = ['B', 'KB', 'MB', 'GB'];
                    const i = Math.floor(Math.log(bytes) / Math.log(k));
                    return (bytes / Math.pow(k, i)).toFixed(1) + ' ' + sizes[i];
                },

                async fetchConns() {
                    try {
                        const d = await (await fetch('/api/connections')).json();
                        this.connections = d.connections || [];
                        this.connStats = { download: d.downloadTotal || 0, upload: d.uploadTotal || 0 };
                    } catch(e) {}
                },

                startConnPolling() {
                    this.fetchConns();
                    this.connInterval = setInterval(() => this.fetchConns(), 2000);
                },

                stopConnPolling() {
                    if (this.connInterval) { clearInterval(this.connInterval); this.connInterval = null; }
                },

                async closeConn(id) {
                    try { await fetch('/api/connections/' + id, { method: 'DELETE' }); } catch(e) {}
                },

                async closeAllConns() {
                    try { await fetch('/api/connections', { method: 'DELETE' }); this.toast('已断开全部', 'success'); } catch(e) {}
                },

                startLogPolling() {
                    this.logInterval = setInterval(() => this.fetchLogs(), 3000);
                },

                stopLogPolling() {
                    if (this.logInterval) { clearInterval(this.logInterval); this.logInterval = null; }
                },

                toast(msg, type = 'info') {
                    this.toasts.push({ msg, type });
                    setTimeout(() => this.toasts.shift(), 3000);
                }
            };
        }
    </script>
</body>
</html>`
