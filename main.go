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
	CurrentNodeFile   = "/etc/sing-box/current_node.txt"
	CronFile          = "/etc/crontabs/root"
	Version           = "3.0.0"
	WebPort           = 7788
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

// SingBoxConfig represents the sing-box configuration structure
type SingBoxConfig struct {
	Log          json.RawMessage   `json:"log,omitempty"`
	DNS          json.RawMessage   `json:"dns,omitempty"`
	Inbounds     json.RawMessage   `json:"inbounds,omitempty"`
	Outbounds    []json.RawMessage `json:"outbounds"`
	Route        json.RawMessage   `json:"route,omitempty"`
	Experimental json.RawMessage   `json:"experimental,omitempty"`
}

// Outbound types
type SSOutbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	Method     string `json:"method"`
	Password   string `json:"password"`
}

type VLESSOutbound struct {
	Type       string     `json:"type"`
	Tag        string     `json:"tag"`
	Server     string     `json:"server"`
	ServerPort int        `json:"server_port"`
	UUID       string     `json:"uuid"`
	Flow       string     `json:"flow,omitempty"`
	TLS        *TLSConfig `json:"tls,omitempty"`
	Transport  *Transport `json:"transport,omitempty"`
}

type TLSConfig struct {
	Enabled    bool        `json:"enabled"`
	ServerName string      `json:"server_name,omitempty"`
	UTLS       *UTLSConfig `json:"utls,omitempty"`
	Reality    *Reality    `json:"reality,omitempty"`
}

type UTLSConfig struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type Reality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key,omitempty"`
	ShortID   string `json:"short_id,omitempty"`
}

type Transport struct {
	Type string `json:"type"`
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"`
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
	case "web":
		startWebServer()
	case "version", "v":
		fmt.Printf("sing-box manager v%s\n", Version)
	default:
		printUsage()
	}
}

func printUsage() {
	fmt.Printf(`sing-box 管理工具 v%s

用法: sb <命令> [参数]

命令:
  status, s          显示运行状态
  list, l [filter]   列出节点
  switch, sw <n>     切换到指定节点
  update, u          更新订阅
  test, t            测试当前连接
  speed, sp [n]      测速显示最快n个节点
  restart, r         重启 sing-box
  start/stop         启动/停止
  log                查看日志
  cron [on N|off]    定时检测
  auto               自动切换到最快节点
  check              检测连接,失败自动切换
  web                启动Web管理界面
  version, v         显示版本
`, Version)
}

func info(msg string)   { fmt.Printf("\033[32m[INFO]\033[0m %s\n", msg) }
func warn(msg string)   { fmt.Printf("\033[33m[WARN]\033[0m %s\n", msg) }
func errMsg(msg string) { fmt.Printf("\033[31m[ERROR]\033[0m %s\n", msg) }

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

	status.TunCreated = exec.Command("ip", "link", "show", "tun0").Run() == nil

	// Read current node from config.json (authoritative source)
	if data, err := os.ReadFile(ConfigFile); err == nil {
		var config SingBoxConfig
		if json.Unmarshal(data, &config) == nil && len(config.Outbounds) > 0 {
			var outbound map[string]interface{}
			if json.Unmarshal(config.Outbounds[0], &outbound) == nil {
				if t, ok := outbound["type"].(string); ok {
					status.NodeType = strings.ToUpper(t)
					if status.NodeType == "SHADOWSOCKS" {
						status.NodeType = "SS"
					}
				}
				server, _ := outbound["server"].(string)
				port := 0
				if p, ok := outbound["server_port"].(float64); ok {
					port = int(p)
				}
				if server != "" {
					status.Server = fmt.Sprintf("%s:%d", server, port)
				}
			}
		}
	}

	// Read node name from current_node.txt, but verify server matches
	if data, err := os.ReadFile(CurrentNodeFile); err == nil {
		parts := strings.Split(strings.TrimSpace(string(data)), "|")
		if len(parts) >= 3 {
			savedServer := parts[2]
			// Only use saved name if server matches config
			if savedServer == status.Server {
				status.CurrentNode = parts[0]
			} else {
				// Server mismatch - find actual node name
				status.CurrentNode = findNodeNameByServer(status.Server)
			}
		}
	} else if status.Server != "" {
		status.CurrentNode = findNodeNameByServer(status.Server)
	}

	status.CronEnabled, status.CronInterval = getCronInfo()
	return status
}

func findNodeNameByServer(server string) string {
	nodes, err := loadNodes()
	if err != nil {
		return server // Fallback to server address
	}
	for _, n := range nodes {
		nodeServer := fmt.Sprintf("%s:%d", n.Server, n.Port)
		if nodeServer == server {
			return n.Name
		}
	}
	return server // Not found, use server address
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

// ============ Node Switching ============

func switchNode(nodeNum int) bool {
	nodes, err := loadNodes()
	if err != nil || nodeNum < 1 || nodeNum > len(nodes) {
		errMsg("无效节点")
		return false
	}

	node := nodes[nodeNum-1]
	info(fmt.Sprintf("切换到: %s [%s]", node.Name, strings.ToUpper(node.Type)))

	configData, err := os.ReadFile(ConfigFile)
	if err != nil {
		errMsg("无法读取配置")
		return false
	}
	os.WriteFile(BackupFile, configData, 0644)

	var config SingBoxConfig
	if json.Unmarshal(configData, &config) != nil {
		errMsg("解析配置失败")
		return false
	}

	newOutbound, err := generateOutbound(node)
	if err != nil {
		errMsg("生成配置失败")
		return false
	}

	if len(config.Outbounds) > 0 {
		config.Outbounds[0] = newOutbound
	}

	newData, _ := json.MarshalIndent(config, "", "  ")
	if os.WriteFile(ConfigFile, newData, 0644) != nil {
		errMsg("写入配置失败")
		return false
	}

	nodeInfo := fmt.Sprintf("%s|%s|%s:%d", node.Name, strings.ToUpper(node.Type), node.Server, node.Port)
	os.WriteFile(CurrentNodeFile, []byte(nodeInfo), 0644)

	restartSingbox()
	time.Sleep(2 * time.Second)

	if testConnectionQuiet() {
		info("切换成功!")
		return true
	}

	warn("连接测试失败，回滚...")
	os.WriteFile(ConfigFile, configData, 0644)
	restartSingbox()
	return false
}

func switchNodeQuiet(node Node) bool {
	configData, _ := os.ReadFile(ConfigFile)
	var config SingBoxConfig
	if json.Unmarshal(configData, &config) != nil {
		return false
	}

	newOutbound, err := generateOutbound(node)
	if err != nil {
		return false
	}

	if len(config.Outbounds) > 0 {
		config.Outbounds[0] = newOutbound
	}

	newData, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(ConfigFile, newData, 0644)

	exec.Command("killall", "sing-box").Run()
	time.Sleep(500 * time.Millisecond)
	exec.Command("sh", "-c", "sing-box run -c "+ConfigFile+" -D "+ConfigDir+" >> /var/log/sing-box.log 2>&1 &").Run()
	time.Sleep(2 * time.Second)

	return exec.Command("pgrep", "-x", "sing-box").Run() == nil
}

// ============ Outbound Generation ============

func generateOutbound(node Node) (json.RawMessage, error) {
	switch node.Type {
	case "ss":
		return generateSSOutbound(node)
	case "vless":
		return generateVLESSOutbound(node)
	case "vmess":
		return generateVMessOutbound(node)
	}
	return nil, fmt.Errorf("unsupported type: %s", node.Type)
}

func generateSSOutbound(node Node) (json.RawMessage, error) {
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

	return json.Marshal(SSOutbound{
		Type: "shadowsocks", Tag: "proxy",
		Server: node.Server, ServerPort: node.Port,
		Method: method, Password: password,
	})
}

func generateVLESSOutbound(node Node) (json.RawMessage, error) {
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

	out := VLESSOutbound{
		Type: "vless", Tag: "proxy",
		Server: node.Server, ServerPort: node.Port, UUID: uuid,
		Flow: params["flow"],
	}

	if security := params["security"]; security == "reality" || security == "tls" {
		out.TLS = &TLSConfig{Enabled: true, ServerName: params["sni"]}
		if fp := params["fp"]; fp != "" {
			out.TLS.UTLS = &UTLSConfig{Enabled: true, Fingerprint: fp}
		}
		if security == "reality" {
			out.TLS.Reality = &Reality{Enabled: true, PublicKey: params["pbk"], ShortID: params["sid"]}
		}
	}

	if t := params["type"]; t != "" && t != "tcp" {
		out.Transport = &Transport{Type: t, Path: params["path"], Host: params["host"]}
	}

	return json.Marshal(out)
}

func generateVMessOutbound(node Node) (json.RawMessage, error) {
	decoded, _ := base64Decode(strings.TrimPrefix(node.Raw, "vmess://"))
	var cfg map[string]interface{}
	if json.Unmarshal([]byte(decoded), &cfg) != nil {
		return nil, fmt.Errorf("invalid vmess config")
	}

	out := map[string]interface{}{
		"type": "vmess", "tag": "proxy",
		"server": cfg["add"], "server_port": cfg["port"],
		"uuid": cfg["id"], "security": "auto", "alter_id": 0,
	}

	if aid, ok := cfg["aid"].(float64); ok {
		out["alter_id"] = int(aid)
	}
	if tls, _ := cfg["tls"].(string); tls == "tls" {
		out["tls"] = map[string]interface{}{"enabled": true}
	}
	if net, _ := cfg["net"].(string); net != "" && net != "tcp" {
		out["transport"] = map[string]interface{}{"type": net, "path": cfg["path"], "host": cfg["host"]}
	}

	return json.Marshal(out)
}

// ============ Service Control ============

func stopSingbox() {
	exec.Command("killall", "sing-box").Run()
	time.Sleep(time.Second)
	info("sing-box 已停止")
}

func startSingbox() {
	exec.Command("sh", "-c", "sing-box run -c "+ConfigFile+" -D "+ConfigDir+" >> /var/log/sing-box.log 2>&1 &").Run()
	time.Sleep(2 * time.Second)
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
	currentNode, _ := os.ReadFile(CurrentNodeFile)

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
	os.WriteFile(CurrentNodeFile, currentNode, 0644)
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
		if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(content)); err == nil {
			content = string(decoded)
		}

		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "ss://") || strings.HasPrefix(line, "vmess://") || strings.HasPrefix(line, "vless://") {
				allNodes = append(allNodes, line)
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
	mux.HandleFunc("/api/subscriptions", apiSubscriptions)
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
	configData, _ := os.ReadFile(ConfigFile)
	var config SingBoxConfig
	json.Unmarshal(configData, &config)

	newOutbound, _ := generateOutbound(node)
	if len(config.Outbounds) > 0 {
		config.Outbounds[0] = newOutbound
	}

	newData, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(ConfigFile, newData, 0644)
	os.WriteFile(CurrentNodeFile, []byte(fmt.Sprintf("%s|%s|%s:%d", node.Name, strings.ToUpper(node.Type), node.Server, node.Port)), 0644)

	exec.Command("killall", "sing-box").Run()
	time.Sleep(time.Second)
	exec.Command("sh", "-c", "sing-box run -c "+ConfigFile+" -D "+ConfigDir+" >> /var/log/sing-box.log 2>&1 &").Run()
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
		currentNode, _ := os.ReadFile(CurrentNodeFile)

		for i := 0; i < testCount; i++ {
			if switchNodeQuiet(successful[i].Node) {
				time.Sleep(time.Second)
				if latency, ok := measureHTTPLatency(10 * time.Second); ok {
					successful[i].HTTPLatency = latency
				}
			}
		}

		os.WriteFile(ConfigFile, currentConfig, 0644)
		os.WriteFile(CurrentNodeFile, currentNode, 0644)
		exec.Command("killall", "sing-box").Run()
		time.Sleep(time.Second)
		exec.Command("sh", "-c", "sing-box run -c "+ConfigFile+" -D "+ConfigDir+" >> /var/log/sing-box.log 2>&1 &").Run()

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

func webUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(webHTML))
}

var webHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>sing-box</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        :root {
            --bg: #1a1a1a;
            --card: #242424;
            --border: #333;
            --text: #e5e5e5;
            --text2: #888;
            --blue: #3b82f6;
            --green: #22c55e;
            --red: #ef4444;
        }
        body {
            font-family: -apple-system, system-ui, sans-serif;
            background: var(--bg);
            color: var(--text);
            line-height: 1.5;
            min-height: 100vh;
        }
        .container { max-width: 900px; margin: 0 auto; padding: 16px; }

        /* Header */
        .header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 16px 0;
            border-bottom: 1px solid var(--border);
            margin-bottom: 16px;
        }
        .header h1 { font-size: 1.25rem; font-weight: 600; }

        /* Cards */
        .card {
            background: var(--card);
            border: 1px solid var(--border);
            border-radius: 8px;
            padding: 16px;
            margin-bottom: 16px;
        }
        .card-title {
            font-size: 0.875rem;
            font-weight: 600;
            margin-bottom: 12px;
            color: var(--text2);
            text-transform: uppercase;
            letter-spacing: 0.5px;
        }

        /* Grid */
        .grid { display: grid; gap: 16px; }
        .grid-2 { grid-template-columns: repeat(2, 1fr); }
        .grid-4 { grid-template-columns: repeat(4, 1fr); }
        @media (max-width: 600px) {
            .grid-2, .grid-4 { grid-template-columns: 1fr 1fr; }
        }

        /* Stats */
        .stat { text-align: center; padding: 12px; background: var(--bg); border-radius: 6px; }
        .stat-label { font-size: 0.75rem; color: var(--text2); margin-bottom: 4px; }
        .stat-value { font-size: 1rem; font-weight: 600; }
        .stat-value.ok { color: var(--green); }
        .stat-value.err { color: var(--red); }

        /* Current Node */
        .current {
            background: var(--bg);
            border-radius: 6px;
            padding: 12px;
            margin-top: 12px;
        }
        .current-label { font-size: 0.75rem; color: var(--text2); }
        .current-name { font-size: 1rem; font-weight: 600; color: var(--blue); margin: 4px 0; }
        .current-server { font-size: 0.875rem; color: var(--text2); }

        /* Buttons */
        .btn {
            display: inline-flex;
            align-items: center;
            gap: 6px;
            padding: 8px 16px;
            border: none;
            border-radius: 6px;
            cursor: pointer;
            font-size: 0.875rem;
            font-weight: 500;
            transition: opacity 0.2s;
        }
        .btn:hover { opacity: 0.85; }
        .btn:disabled { opacity: 0.5; cursor: not-allowed; }
        .btn-blue { background: var(--blue); color: white; }
        .btn-gray { background: var(--border); color: var(--text); }
        .btn-green { background: var(--green); color: white; }
        .btn-group { display: flex; gap: 8px; flex-wrap: wrap; }

        /* Tabs */
        .tabs {
            display: flex;
            gap: 4px;
            background: var(--bg);
            padding: 4px;
            border-radius: 6px;
            margin-bottom: 12px;
        }
        .tab {
            flex: 1;
            padding: 8px;
            border: none;
            background: transparent;
            color: var(--text2);
            cursor: pointer;
            border-radius: 4px;
            font-size: 0.875rem;
        }
        .tab.active { background: var(--blue); color: white; }

        /* Search */
        .search {
            width: 100%;
            padding: 10px 12px;
            border: 1px solid var(--border);
            border-radius: 6px;
            background: var(--bg);
            color: var(--text);
            font-size: 0.875rem;
            margin-bottom: 12px;
        }
        .search:focus { outline: none; border-color: var(--blue); }

        /* Node List */
        .nodes { max-height: 400px; overflow-y: auto; }
        .node {
            display: flex;
            align-items: center;
            padding: 10px 12px;
            border-radius: 6px;
            cursor: pointer;
            margin-bottom: 4px;
            background: var(--bg);
            transition: background 0.15s;
        }
        .node:hover { background: #2a2a2a; }
        .node.active { background: rgba(59, 130, 246, 0.2); border: 1px solid var(--blue); }
        .node-idx { width: 32px; color: var(--text2); font-size: 0.75rem; }
        .node-type {
            padding: 2px 8px;
            border-radius: 4px;
            font-size: 0.7rem;
            font-weight: 600;
            margin-right: 10px;
            background: var(--border);
        }
        .node-type.ss { color: var(--green); }
        .node-type.vless { color: #f59e0b; }
        .node-type.vmess { color: var(--red); }
        .node-name { flex: 1; font-size: 0.875rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
        .node-latency {
            font-size: 0.75rem;
            padding: 2px 8px;
            border-radius: 4px;
            background: var(--border);
        }
        .node-latency.fast { color: var(--green); }
        .node-latency.medium { color: #f59e0b; }
        .node-latency.slow { color: var(--red); }

        /* Toggle */
        .toggle-row {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 12px 0;
            border-top: 1px solid var(--border);
            margin-top: 12px;
        }
        .toggle-label { font-size: 0.875rem; }
        .toggle-desc { font-size: 0.75rem; color: var(--text2); }
        .toggle-ctrl { display: flex; align-items: center; gap: 8px; }
        .toggle {
            width: 40px;
            height: 22px;
            background: var(--border);
            border-radius: 11px;
            cursor: pointer;
            position: relative;
            transition: background 0.2s;
        }
        .toggle.on { background: var(--green); }
        .toggle::after {
            content: '';
            position: absolute;
            top: 2px;
            left: 2px;
            width: 18px;
            height: 18px;
            background: white;
            border-radius: 50%;
            transition: transform 0.2s;
        }
        .toggle.on::after { transform: translateX(18px); }
        .input-sm {
            width: 50px;
            padding: 4px 8px;
            border: 1px solid var(--border);
            border-radius: 4px;
            background: var(--bg);
            color: var(--text);
            font-size: 0.875rem;
            text-align: center;
        }

        /* Modal */
        .modal-bg {
            position: fixed;
            top: 0; left: 0; right: 0; bottom: 0;
            background: rgba(0,0,0,0.7);
            display: flex;
            align-items: center;
            justify-content: center;
            z-index: 100;
            opacity: 0;
            visibility: hidden;
            transition: all 0.2s;
        }
        .modal-bg.show { opacity: 1; visibility: visible; }
        .modal {
            background: var(--card);
            border-radius: 8px;
            padding: 20px;
            width: 90%;
            max-width: 500px;
            max-height: 80vh;
            overflow-y: auto;
        }
        .modal-title {
            font-size: 1rem;
            font-weight: 600;
            margin-bottom: 16px;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        .modal-close {
            background: none;
            border: none;
            color: var(--text2);
            font-size: 1.25rem;
            cursor: pointer;
        }
        .textarea {
            width: 100%;
            min-height: 120px;
            padding: 10px;
            border: 1px solid var(--border);
            border-radius: 6px;
            background: var(--bg);
            color: var(--text);
            font-size: 0.875rem;
            resize: vertical;
            margin-bottom: 12px;
        }
        .log-box {
            background: #000;
            border-radius: 6px;
            padding: 12px;
            font-family: monospace;
            font-size: 0.75rem;
            max-height: 300px;
            overflow-y: auto;
            color: var(--green);
        }

        /* Toast */
        .toast-box { position: fixed; bottom: 16px; right: 16px; z-index: 200; }
        .toast {
            padding: 10px 16px;
            border-radius: 6px;
            color: white;
            margin-top: 8px;
            font-size: 0.875rem;
            animation: slideIn 0.2s;
        }
        .toast.ok { background: var(--green); }
        .toast.err { background: var(--red); }
        .toast.info { background: var(--blue); }
        @keyframes slideIn { from { transform: translateX(100%); opacity: 0; } }

        /* Result */
        .result {
            margin-top: 12px;
            padding: 10px;
            border-radius: 6px;
            font-size: 0.875rem;
            display: none;
        }
        .result.show { display: block; }
        .result.ok { background: rgba(34, 197, 94, 0.1); color: var(--green); }
        .result.err { background: rgba(239, 68, 68, 0.1); color: var(--red); }

        /* Scrollbar */
        ::-webkit-scrollbar { width: 6px; }
        ::-webkit-scrollbar-track { background: transparent; }
        ::-webkit-scrollbar-thumb { background: var(--border); border-radius: 3px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>sing-box</h1>
            <div class="btn-group">
                <button class="btn btn-gray" onclick="showModal('settings')">设置</button>
                <button class="btn btn-gray" onclick="showModal('logs')">日志</button>
            </div>
        </div>

        <div class="grid grid-2">
            <div class="card">
                <div class="card-title">状态</div>
                <div class="grid grid-4">
                    <div class="stat">
                        <div class="stat-label">服务</div>
                        <div class="stat-value" id="svcStatus">--</div>
                    </div>
                    <div class="stat">
                        <div class="stat-label">内存</div>
                        <div class="stat-value" id="svcMem">--</div>
                    </div>
                    <div class="stat">
                        <div class="stat-label">TUN</div>
                        <div class="stat-value" id="svcTun">--</div>
                    </div>
                    <div class="stat">
                        <div class="stat-label">运行</div>
                        <div class="stat-value" id="svcUp">--</div>
                    </div>
                </div>
                <div class="current">
                    <div class="current-label">当前节点</div>
                    <div class="current-name" id="curNode">未连接</div>
                    <div class="current-server" id="curServer">-</div>
                </div>
                <div class="btn-group" style="margin-top:12px">
                    <button class="btn btn-blue" onclick="testConn()">测试</button>
                    <button class="btn btn-gray" onclick="restart()">重启</button>
                    <button class="btn btn-gray" onclick="updateSub()">更新订阅</button>
                </div>
                <div class="result" id="testRes"></div>
            </div>

            <div class="card">
                <div class="card-title">操作</div>
                <div class="btn-group">
                    <button class="btn btn-blue" id="speedBtn" onclick="runSpeed()">测速</button>
                    <button class="btn btn-green" id="autoBtn" onclick="runAuto()">自动选择</button>
                </div>
                <div class="toggle-row">
                    <div>
                        <div class="toggle-label">定时检测</div>
                        <div class="toggle-desc">连接失败自动切换</div>
                    </div>
                    <div class="toggle-ctrl">
                        <input type="number" class="input-sm" id="cronInt" value="5" min="1" max="60">
                        <span style="color:var(--text2);font-size:0.75rem">分钟</span>
                        <div class="toggle" id="cronToggle" onclick="toggleCron()"></div>
                    </div>
                </div>
            </div>
        </div>

        <div class="card">
            <div class="card-title">节点 (<span id="nodeCount">0</span>)</div>
            <div class="tabs">
                <button class="tab active" data-f="all">全部</button>
                <button class="tab" data-f="ss">SS</button>
                <button class="tab" data-f="vless">VLESS</button>
                <button class="tab" data-f="vmess">VMess</button>
            </div>
            <input type="text" class="search" placeholder="搜索..." id="searchBox" oninput="render()">
            <div class="nodes" id="nodeList"></div>
        </div>
    </div>

    <div class="modal-bg" id="settingsModal">
        <div class="modal">
            <div class="modal-title">订阅设置 <button class="modal-close" onclick="hideModal('settings')">&times;</button></div>
            <textarea class="textarea" id="subUrls" placeholder="每行一个订阅链接"></textarea>
            <div class="btn-group">
                <button class="btn btn-blue" onclick="saveSub()">保存</button>
                <button class="btn btn-gray" onclick="hideModal('settings')">取消</button>
            </div>
        </div>
    </div>

    <div class="modal-bg" id="logsModal">
        <div class="modal" style="max-width:700px">
            <div class="modal-title">日志 <button class="modal-close" onclick="hideModal('logs')">&times;</button></div>
            <div class="log-box" id="logBox">加载中...</div>
            <button class="btn btn-gray" style="margin-top:12px" onclick="fetchLogs()">刷新</button>
        </div>
    </div>

    <div class="toast-box" id="toastBox"></div>

    <script>
        let nodes = [], filter = 'all', curName = '';

        document.addEventListener('DOMContentLoaded', () => {
            fetchStatus();
            fetchNodes();
            fetchCron();
            document.querySelectorAll('.tab').forEach(t => {
                t.onclick = () => {
                    document.querySelectorAll('.tab').forEach(x => x.classList.remove('active'));
                    t.classList.add('active');
                    filter = t.dataset.f;
                    render();
                };
            });
            setInterval(fetchStatus, 10000);
        });

        async function fetchStatus() {
            try {
                const d = await (await fetch('/api/status')).json();
                document.getElementById('svcStatus').textContent = d.running ? '运行中' : '已停止';
                document.getElementById('svcStatus').className = 'stat-value ' + (d.running ? 'ok' : 'err');
                document.getElementById('svcMem').textContent = d.memory ? (parseInt(d.memory)/1024).toFixed(1)+'M' : '--';
                document.getElementById('svcTun').textContent = d.tun_created ? '正常' : '异常';
                document.getElementById('svcTun').className = 'stat-value ' + (d.tun_created ? 'ok' : 'err');
                document.getElementById('svcUp').textContent = d.uptime || '--';
                curName = d.current_node || '';
                document.getElementById('curNode').textContent = d.current_node ? d.current_node + ' [' + d.node_type + ']' : '未连接';
                document.getElementById('curServer').textContent = d.server || '-';
                if (d.cron_enabled) {
                    document.getElementById('cronToggle').classList.add('on');
                    document.getElementById('cronInt').value = d.cron_interval;
                }
                render();
            } catch(e) {}
        }

        async function fetchNodes() {
            try {
                nodes = await (await fetch('/api/nodes')).json();
                document.getElementById('nodeCount').textContent = nodes.length;
                render();
            } catch(e) {
                document.getElementById('nodeList').innerHTML = '<div style="text-align:center;padding:20px;color:var(--text2)">加载失败</div>';
            }
        }

        function render() {
            const kw = document.getElementById('searchBox').value.toLowerCase();
            let list = nodes;
            if (filter !== 'all') list = list.filter(n => n.type === filter);
            if (kw) list = list.filter(n => n.name.toLowerCase().includes(kw) || n.server.toLowerCase().includes(kw));

            document.getElementById('nodeList').innerHTML = list.map(n => {
                const active = n.name === curName ? ' active' : '';
                let lat = '';
                if (n.latency > 0) {
                    const cls = n.latency < 300 ? 'fast' : n.latency < 600 ? 'medium' : 'slow';
                    lat = '<span class="node-latency '+cls+'">'+n.latency+'ms</span>';
                }
                return '<div class="node'+active+'" onclick="switchTo('+n.index+')">'+
                    '<span class="node-idx">#'+n.index+'</span>'+
                    '<span class="node-type '+n.type+'">'+n.type.toUpperCase()+'</span>'+
                    '<span class="node-name">'+esc(n.name)+'</span>'+lat+'</div>';
            }).join('') || '<div style="text-align:center;padding:20px;color:var(--text2)">无节点</div>';
        }

        function esc(s) { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; }

        async function switchTo(idx) {
            toast('切换中...', 'info');
            try {
                const d = await (await fetch('/api/switch?node='+idx)).json();
                toast(d.success ? '切换成功' : '切换失败', d.success ? 'ok' : 'err');
                fetchStatus();
            } catch(e) { toast('请求失败', 'err'); }
        }

        async function testConn() {
            const res = document.getElementById('testRes');
            res.className = 'result show';
            res.textContent = '测试中...';
            try {
                const d = await (await fetch('/api/test')).json();
                res.className = 'result show ' + (d.success ? 'ok' : 'err');
                res.textContent = d.success ? '连接正常 '+d.latency+'ms' : '连接失败';
            } catch(e) { res.className = 'result show err'; res.textContent = '请求失败'; }
        }

        async function runSpeed() {
            const btn = document.getElementById('speedBtn');
            btn.disabled = true;
            btn.textContent = '测速中...';
            toast('开始测速...', 'info');
            try {
                const ctrl = new AbortController();
                setTimeout(() => ctrl.abort(), 300000);
                const results = await (await fetch('/api/speed', {signal: ctrl.signal})).json();
                nodes.forEach(n => {
                    const r = results.find(x => x.node.index === n.index);
                    if (r && r.http_latency > 0) n.latency = r.http_latency;
                    else if (r && r.tcp_latency > 0) n.latency = r.tcp_latency;
                });
                nodes.sort((a,b) => (a.latency<=0?9999:a.latency) - (b.latency<=0?9999:b.latency));
                render();
                toast('测速完成', 'ok');
            } catch(e) { toast('测速失败', 'err'); }
            btn.disabled = false;
            btn.textContent = '测速';
        }

        async function runAuto() {
            const btn = document.getElementById('autoBtn');
            btn.disabled = true;
            btn.textContent = '搜索中...';
            toast('寻找最佳节点...', 'info');
            try {
                await fetch('/api/auto');
                toast('已切换', 'ok');
                fetchStatus();
                fetchNodes();
            } catch(e) { toast('失败', 'err'); }
            btn.disabled = false;
            btn.textContent = '自动选择';
        }

        async function restart() {
            toast('重启中...', 'info');
            await fetch('/api/restart');
            setTimeout(() => { fetchStatus(); toast('已重启', 'ok'); }, 3000);
        }

        async function updateSub() {
            toast('更新中...', 'info');
            await fetch('/api/update');
            setTimeout(() => { fetchNodes(); toast('已更新', 'ok'); }, 5000);
        }

        async function fetchCron() {
            try {
                const d = await (await fetch('/api/cron')).json();
                if (d.enabled) {
                    document.getElementById('cronToggle').classList.add('on');
                    document.getElementById('cronInt').value = d.interval;
                }
            } catch(e) {}
        }

        async function toggleCron() {
            const tog = document.getElementById('cronToggle');
            const on = tog.classList.contains('on');
            const int = document.getElementById('cronInt').value;
            await fetch('/api/cron?action='+(on?'off':'on')+'&interval='+int);
            tog.classList.toggle('on');
            toast(on?'已关闭':'已开启', 'ok');
        }

        function showModal(name) {
            if (name === 'settings') {
                fetch('/api/subscriptions').then(r=>r.json()).then(d => {
                    document.getElementById('subUrls').value = (d.urls||[]).join('\n');
                });
            } else if (name === 'logs') {
                fetchLogs();
            }
            document.getElementById(name+'Modal').classList.add('show');
        }

        function hideModal(name) {
            document.getElementById(name+'Modal').classList.remove('show');
        }

        async function fetchLogs() {
            try {
                const d = await (await fetch('/api/logs')).json();
                document.getElementById('logBox').innerHTML = (d.logs||[]).map(l => esc(l)).join('<br>');
            } catch(e) { document.getElementById('logBox').textContent = '加载失败'; }
        }

        async function saveSub() {
            const urls = document.getElementById('subUrls').value.split('\n').filter(u => u.trim());
            await fetch('/api/subscriptions', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({urls})
            });
            toast('已保存', 'ok');
            hideModal('settings');
        }

        function toast(msg, type) {
            const box = document.getElementById('toastBox');
            const t = document.createElement('div');
            t.className = 'toast ' + type;
            t.textContent = msg;
            box.appendChild(t);
            setTimeout(() => t.remove(), 3000);
        }

        document.querySelectorAll('.modal-bg').forEach(m => {
            m.onclick = e => { if (e.target === m) m.classList.remove('show'); };
        });
    </script>
</body>
</html>`
