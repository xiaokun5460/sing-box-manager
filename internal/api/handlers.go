package api

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"sing-box-manager/internal/config"
	"sing-box-manager/internal/process"
	"sing-box-manager/internal/service"
	"sing-box-manager/internal/subscription"
)

// 全局 HTTP 客户端（连接池复用）
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	},
}

// Response API 响应
type Response struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// StatusData 状态数据
type StatusData struct {
	State       string `json:"state"`
	PID         int    `json:"pid,omitempty"`
	Version     string `json:"version"`
	ProxyMode   string `json:"proxy_mode"`
	CurrentNode string `json:"current_node"`
	NodeIndex   int    `json:"node_index"`
	NodeCount   int    `json:"node_count"`
}

// writeJSON 写入 JSON 响应（支持 Gzip 压缩）
func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// writeJSONGzip 写入 JSON 响应（强制 Gzip 压缩，用于大数据）
func writeJSONGzip(w http.ResponseWriter, r *http.Request, data interface{}) {
	// 检查客户端是否支持 gzip
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		json.NewEncoder(gz).Encode(data)
	} else {
		writeJSON(w, data)
	}
}

// writeJSONRaw 写入 JSON 响应（不压缩，用于小数据）
func writeJSONRaw(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// writeError 写入错误响应
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Response{Success: false, Message: message})
}

// writeSuccess 写入成功响应
func writeSuccess(w http.ResponseWriter, message string, data interface{}) {
	writeJSON(w, Response{Success: true, Message: message, Data: data})
}

// writeSuccessRaw 写入成功响应（不压缩）
func writeSuccessRaw(w http.ResponseWriter, message string, data interface{}) {
	writeJSONRaw(w, Response{Success: true, Message: message, Data: data})
}

// handleStatus 获取状态
func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	processMgr := process.GetManager()
	configMgr := config.GetManager()
	updater := subscription.GetUpdater()

	state := configMgr.GetState()

	data := StatusData{
		State:       processMgr.GetState().String(),
		PID:         processMgr.GetPID(),
		Version:     processMgr.GetBinaryVersion(),
		ProxyMode:   state.ProxyMode,
		CurrentNode: state.CurrentNode,
		NodeIndex:   state.NodeIndex,
		NodeCount:   updater.GetNodeCount(),
	}

	writeSuccess(w, "", data)
}

// handleStart 启动服务
func handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	processMgr := process.GetManager()

	if err := processMgr.Start(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "服务已启动", nil)
}

// handleStop 停止服务
func handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	processMgr := process.GetManager()

	if err := processMgr.Stop(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "服务已停止", nil)
}

// handleRestart 重启服务
func handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// 重新生成配置并重启
	if err := service.RegenerateAndRestart(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "服务已重启", nil)
}

// handleSubscriptions 订阅管理
func handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	configMgr := config.GetManager()

	switch r.Method {
	case "GET":
		subs := configMgr.GetSubscriptions()
		writeSuccess(w, "", subs)

	case "POST":
		var sub config.Subscription
		if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
			writeError(w, http.StatusBadRequest, "无效的请求数据")
			return
		}

		if sub.URL == "" {
			writeError(w, http.StatusBadRequest, "订阅 URL 不能为空")
			return
		}

		if err := configMgr.AddSubscription(sub); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		writeSuccess(w, "订阅已添加", nil)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleSubscription 单个订阅操作
func handleSubscription(w http.ResponseWriter, r *http.Request) {
	// 提取订阅 ID
	path := strings.TrimPrefix(r.URL.Path, "/api/subscriptions/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "无效的订阅 ID")
		return
	}

	subID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	configMgr := config.GetManager()

	switch {
	case action == "refresh" && r.Method == "POST":
		updater := subscription.GetUpdater()
		if err := updater.RefreshSubscription(subID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSuccess(w, "订阅已刷新", nil)

	case r.Method == "PUT":
		// 更新订阅信息
		var sub config.Subscription
		if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
			writeError(w, http.StatusBadRequest, "无效的请求数据")
			return
		}
		sub.ID = subID
		if err := configMgr.UpdateSubscription(sub); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeSuccess(w, "订阅已更新", nil)

	case r.Method == "DELETE":
		if err := configMgr.RemoveSubscription(subID); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeSuccess(w, "订阅已删除", nil)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleRefreshAll 刷新所有订阅
func handleRefreshAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	updater := subscription.GetUpdater()
	if err := updater.RefreshAll(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "所有订阅已刷新", nil)
}

// handleNodes 节点列表
func handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	updater := subscription.GetUpdater()
	nodes := updater.GetNodes()

	// 节点列表较大，使用 Gzip 压缩
	writeJSONGzip(w, r, Response{Success: true, Data: nodes})
}

// handleNode 单个节点操作
func handleNode(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "无效的节点 ID")
		return
	}

	nodeIndex, err := strconv.Atoi(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效的节点 ID")
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "select" && r.Method == "POST":
		handleSelectNode(w, r, nodeIndex)
	case action == "test" && r.Method == "POST":
		handleTestNode(w, r, nodeIndex)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleSelectNode 选择节点
func handleSelectNode(w http.ResponseWriter, r *http.Request, nodeIndex int) {
	updater := subscription.GetUpdater()
	node := updater.GetNode(nodeIndex)
	if node == nil {
		writeError(w, http.StatusNotFound, "节点不存在")
		return
	}

	if err := service.SwitchNode(nodeIndex); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, fmt.Sprintf("已切换到: %s", node.Name), nil)
}

// handleTestNode 测试节点
func handleTestNode(w http.ResponseWriter, r *http.Request, nodeIndex int) {
	updater := subscription.GetUpdater()
	node := updater.GetNode(nodeIndex)
	if node == nil {
		writeError(w, http.StatusNotFound, "节点不存在")
		return
	}

	// 使用 Clash API 测速
	latency := testNodeViaClashAPI(nodeIndex)
	writeSuccess(w, "测试完成", map[string]interface{}{
		"latency": latency,
	})
}

// testNodeLatency TCP 连接测试（备用）
func testNodeLatency(node *config.Node) int64 {
	start := time.Now()
	addr := fmt.Sprintf("%s:%d", node.Server, node.Port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return -1
	}
	conn.Close()
	return time.Since(start).Milliseconds()
}

// handleTestAllNodes 批量测速（使用 Clash API）
func handleTestAllNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	updater := subscription.GetUpdater()
	nodes := updater.GetNodes()

	// 高并发测速，限制并发数为 30（避免 Clash API 过载）
	maxConcurrency := 30
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	results := make([]map[string]interface{}, len(nodes))
	var mu sync.Mutex

	for i, node := range nodes {
		wg.Add(1)
		go func(idx int, n config.Node) {
			defer wg.Done()
			sem <- struct{}{}        // 获取信号量
			defer func() { <-sem }() // 释放信号量

			// 使用 Clash API 测速
			latency := testNodeViaClashAPI(idx + 1)

			mu.Lock()
			results[idx] = map[string]interface{}{
				"index":   idx + 1,
				"name":    n.Name,
				"latency": latency,
			}
			mu.Unlock()
		}(i, node)
	}

	wg.Wait()
	writeSuccess(w, "批量测速完成", results)
}

// 测速 URL 列表（多个备选，提高成功率）
var testURLs = []string{
	"http://www.gstatic.com/generate_204",      // Google
	"http://cp.cloudflare.com/generate_204",    // Cloudflare
	"http://www.msftconnecttest.com/connecttest.txt", // Microsoft
}

// testNodeViaClashAPI 通过 Clash API 测试节点延迟（多 URL 并行，取最快）
func testNodeViaClashAPI(nodeIndex int) int {
	nodeName := fmt.Sprintf("node-%d", nodeIndex)

	// 并行测试多个 URL
	results := make(chan int, len(testURLs))
	var wg sync.WaitGroup

	for _, testURL := range testURLs {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			apiURL := fmt.Sprintf("http://127.0.0.1:9090/proxies/%s/delay?timeout=5000&url=%s", nodeName, url)

			resp, err := httpClient.Get(apiURL)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return
			}

			if delay, ok := result["delay"].(float64); ok && delay > 0 {
				results <- int(delay)
			}
		}(testURL)
	}

	// 等待所有测试完成（或超时）
	go func() {
		wg.Wait()
		close(results)
	}()

	// 取最快的结果
	minDelay := -1
	for delay := range results {
		if minDelay == -1 || delay < minDelay {
			minDelay = delay
		}
	}

	return minDelay
}

// testNodeLatencyFast TCP 连接测试（备用）
func testNodeLatencyFast(node *config.Node) int {
	if node.Server == "" || node.Port == 0 {
		return -1
	}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", node.Server, node.Port), 3*time.Second)
	if err != nil {
		return -1
	}
	conn.Close()

	return int(time.Since(start).Milliseconds())
}

// handleMode 代理模式
func handleMode(w http.ResponseWriter, r *http.Request) {
	configMgr := config.GetManager()

	switch r.Method {
	case "GET":
		state := configMgr.GetState()
		writeSuccess(w, "", map[string]string{"mode": state.ProxyMode})

	case "PUT":
		var req struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "无效的请求数据")
			return
		}

		if err := configMgr.SetProxyMode(req.Mode); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// 重新生成配置并重启
		if err := service.RegenerateAndRestart(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, fmt.Sprintf("模式已切换为: %s", req.Mode), nil)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleRules 规则管理
func handleRules(w http.ResponseWriter, r *http.Request) {
	configMgr := config.GetManager()

	switch r.Method {
	case "GET":
		rules := configMgr.GetCustomRules()
		writeSuccess(w, "", rules)

	case "PUT":
		var rules []config.CustomRule
		if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
			writeError(w, http.StatusBadRequest, "无效的请求数据")
			return
		}

		if err := configMgr.SetCustomRules(rules); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if err := service.RegenerateAndRestart(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, "规则已更新", nil)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleLogs 获取日志 - 使用 tail 只读取最后几行（高性能）
func handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	// 使用 tail 命令只读取最后 limit 行
	logFile := "/tmp/sing-box.log"
	cmd := exec.Command("tail", "-n", strconv.Itoa(limit), logFile)
	data, err := cmd.Output()
	if err != nil {
		writeSuccess(w, "", []interface{}{})
		return
	}

	lines := strings.Split(string(data), "\n")

	// 预分配切片容量
	logs := make([]map[string]interface{}, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		logs = append(logs, parseLogLine(line))
	}

	writeSuccess(w, "", logs)
}

// 预编译正则表达式（性能优化）
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// handleLogStream SSE 日志流 - 使用 tail -f 实时读取（高性能）
func handleLogStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	logFile := "/tmp/sing-box.log"

	// 先发送最近 50 行日志
	initialCmd := exec.Command("tail", "-n", "50", logFile)
	initialOutput, _ := initialCmd.Output()
	if len(initialOutput) > 0 {
		lines := strings.Split(string(initialOutput), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			logEntry := parseLogLine(line)
			data, _ := json.Marshal(logEntry)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		flusher.Flush()
	}

	// 使用 tail -f 实时跟踪新日志
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	cmd := exec.CommandContext(ctx, "tail", "-f", "-n", "0", logFile)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}

	if err := cmd.Start(); err != nil {
		return
	}

	// 确保进程被清理
	defer func() {
		cancel()
		cmd.Process.Kill()
		cmd.Wait()
	}()

	reader := bufio.NewReader(stdout)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		logEntry := parseLogLine(line)
		data, _ := json.Marshal(logEntry)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// parseLogLine 解析日志行
func parseLogLine(line string) map[string]interface{} {
	// 解析日志级别
	level := "info"
	if strings.Contains(line, "ERROR") || strings.Contains(line, "FATAL") {
		level = "error"
	} else if strings.Contains(line, "WARN") {
		level = "warn"
	} else if strings.Contains(line, "DEBUG") {
		level = "debug"
	}

	// 清理 ANSI 颜色码
	line = ansiRegex.ReplaceAllString(line, "")

	// 去掉时区前缀 "+0000 "
	if strings.HasPrefix(line, "+") && len(line) > 6 {
		line = strings.TrimPrefix(line, line[:6])
	}

	return map[string]interface{}{
		"time":    time.Now().Format(time.RFC3339),
		"level":   level,
		"message": line,
	}
}

// handleClearLogs 清空日志
func handleClearLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	processMgr := process.GetManager()
	processMgr.ClearLogs()

	writeSuccess(w, "日志已清空", nil)
}

// handleLogLevel 日志级别
func handleLogLevel(w http.ResponseWriter, r *http.Request) {
	configMgr := config.GetManager()

	switch r.Method {
	case "GET":
		cfg := configMgr.GetConfig()
		writeSuccess(w, "", map[string]string{"level": cfg.SingBox.LogLevel})

	case "POST":
		var req struct {
			Level string `json:"level"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "无效的请求数据")
			return
		}

		validLevels := map[string]bool{"trace": true, "debug": true, "info": true, "warn": true, "error": true}
		if !validLevels[req.Level] {
			writeError(w, http.StatusBadRequest, "无效的日志级别")
			return
		}

		cfg := configMgr.GetConfig()
		cfg.SingBox.LogLevel = req.Level
		if err := configMgr.UpdateConfig(&cfg); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// 重启服务使日志级别生效
		if err := service.RegenerateAndRestart(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, "日志级别已更新", nil)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleConfig 配置管理
func handleConfig(w http.ResponseWriter, r *http.Request) {
	configMgr := config.GetManager()

	switch r.Method {
	case "GET":
		cfg := configMgr.GetConfig()
		writeSuccess(w, "", cfg)

	case "PUT":
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "无效的请求数据")
			return
		}

		if err := configMgr.UpdateConfig(&cfg); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, "配置已更新", nil)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleClearCache 清空缓存
func handleClearCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	updater := subscription.GetUpdater()
	if err := updater.ClearCache(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSuccess(w, "缓存已清空", nil)
}

// handleBypass 绕过配置
func handleBypass(w http.ResponseWriter, r *http.Request) {
	configMgr := config.GetManager()

	switch r.Method {
	case "GET":
		bypass := configMgr.GetBypass()
		writeSuccess(w, "", bypass)

	case "PUT":
		var bypass config.BypassConfig
		if err := json.NewDecoder(r.Body).Decode(&bypass); err != nil {
			writeError(w, http.StatusBadRequest, "无效的请求数据")
			return
		}

		if err := configMgr.SetBypass(bypass); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// 重新生成配置并重启
		if err := service.RegenerateAndRestart(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeSuccess(w, "绕过配置已更新", nil)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleConnections 连接管理（代理 Clash API，支持排序）
func handleConnections(w http.ResponseWriter, r *http.Request) {
	// 代理到 Clash API
	client := &http.Client{Timeout: 5 * time.Second}

	switch r.Method {
	case "GET":
		resp, err := client.Get("http://127.0.0.1:9090/connections")
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "无法连接到 Clash API")
			return
		}
		defer resp.Body.Close()

		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			writeError(w, http.StatusInternalServerError, "解析响应失败")
			return
		}

		// 获取排序参数
		sortBy := r.URL.Query().Get("sort")
		order := r.URL.Query().Get("order") // asc 或 desc

		// 对连接进行排序
		if connections, ok := data["connections"].([]interface{}); ok && sortBy != "" {
			sort.Slice(connections, func(i, j int) bool {
				ci, ok1 := connections[i].(map[string]interface{})
				cj, ok2 := connections[j].(map[string]interface{})
				if !ok1 || !ok2 {
					return false
				}

				var vi, vj float64
				switch sortBy {
				case "download":
					vi, _ = ci["download"].(float64)
					vj, _ = cj["download"].(float64)
				case "upload":
					vi, _ = ci["upload"].(float64)
					vj, _ = cj["upload"].(float64)
				case "time":
					// 按开始时间排序
					ti, _ := ci["start"].(string)
					tj, _ := cj["start"].(string)
					if order == "asc" {
						return ti < tj
					}
					return ti > tj
				case "speed":
					// 按下载速度排序（需要计算）
					di, _ := ci["download"].(float64)
					dj, _ := cj["download"].(float64)
					vi, vj = di, dj
				default:
					return false
				}

				if order == "asc" {
					return vi < vj
				}
				return vi > vj // 默认降序
			})
			data["connections"] = connections
		}

		writeJSONGzip(w, r, Response{Success: true, Data: data})

	case "DELETE":
		req, err := http.NewRequest("DELETE", "http://127.0.0.1:9090/connections", nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "创建请求失败")
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "无法连接到 Clash API")
			return
		}
		resp.Body.Close()
		writeSuccess(w, "已断开所有连接", nil)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleConnection 单个连接操作
func handleConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	connID := strings.TrimPrefix(r.URL.Path, "/api/connections/")
	if connID == "" {
		writeError(w, http.StatusBadRequest, "无效的连接 ID")
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("DELETE", "http://127.0.0.1:9090/connections/"+connID, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建请求失败")
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "无法连接到 Clash API")
		return
	}
	resp.Body.Close()

	writeSuccess(w, "连接已断开", nil)
}

// handleTraffic 获取流量统计
func handleTraffic(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// 获取连接信息计算流量
	resp, err := client.Get("http://127.0.0.1:9090/connections")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "无法连接到 Clash API")
		return
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&data)

	// 统计总流量
	var totalDownload, totalUpload int64
	var activeConnections int

	if connections, ok := data["connections"].([]interface{}); ok {
		activeConnections = len(connections)
		for _, conn := range connections {
			if c, ok := conn.(map[string]interface{}); ok {
				if d, ok := c["download"].(float64); ok {
					totalDownload += int64(d)
				}
				if u, ok := c["upload"].(float64); ok {
					totalUpload += int64(u)
				}
			}
		}
	}

	// 获取总流量（从 Clash API）
	if download, ok := data["downloadTotal"].(float64); ok {
		totalDownload = int64(download)
	}
	if upload, ok := data["uploadTotal"].(float64); ok {
		totalUpload = int64(upload)
	}

	result := map[string]interface{}{
		"download":          totalDownload,
		"upload":            totalUpload,
		"total":             totalDownload + totalUpload,
		"activeConnections": activeConnections,
		"downloadFormatted": formatBytes(totalDownload),
		"uploadFormatted":   formatBytes(totalUpload),
		"totalFormatted":    formatBytes(totalDownload + totalUpload),
	}

	writeSuccess(w, "", result)
}

// handleTrafficRealtime 实时流量统计（SSE）
func handleTrafficRealtime(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastDownload, lastUpload int64

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			resp, err := client.Get("http://127.0.0.1:9090/connections")
			if err != nil {
				continue
			}

			var data map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&data)
			resp.Body.Close()

			var currentDownload, currentUpload int64
			var activeConnections int

			if download, ok := data["downloadTotal"].(float64); ok {
				currentDownload = int64(download)
			}
			if upload, ok := data["uploadTotal"].(float64); ok {
				currentUpload = int64(upload)
			}
			if connections, ok := data["connections"].([]interface{}); ok {
				activeConnections = len(connections)
			}

			// 计算速度
			downloadSpeed := currentDownload - lastDownload
			uploadSpeed := currentUpload - lastUpload
			if downloadSpeed < 0 {
				downloadSpeed = 0
			}
			if uploadSpeed < 0 {
				uploadSpeed = 0
			}

			lastDownload = currentDownload
			lastUpload = currentUpload

			result := map[string]interface{}{
				"download":          currentDownload,
				"upload":            currentUpload,
				"downloadSpeed":     downloadSpeed,
				"uploadSpeed":       uploadSpeed,
				"activeConnections": activeConnections,
				"downloadFormatted": formatBytes(currentDownload),
				"uploadFormatted":   formatBytes(currentUpload),
				"downloadSpeedFormatted": formatBytes(downloadSpeed) + "/s",
				"uploadSpeedFormatted":   formatBytes(uploadSpeed) + "/s",
			}

			jsonData, _ := json.Marshal(result)
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()
		}
	}
}

// formatBytes 格式化字节数
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// handleConnectionsWS WebSocket 连接实时推送
func handleConnectionsWS(w http.ResponseWriter, r *http.Request) {
	// 使用 SSE 替代 WebSocket（无需额外依赖）
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(500 * time.Millisecond) // 500ms 更新一次
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			resp, err := httpClient.Get("http://127.0.0.1:9090/connections")
			if err != nil {
				continue
			}

			var data map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&data)
			resp.Body.Close()

			jsonData, _ := json.Marshal(data)
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()
		}
	}
}

// handleTrafficWS WebSocket 流量实时推送
func handleTrafficWS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(200 * time.Millisecond) // 200ms 更新一次，更流畅
	defer ticker.Stop()

	var lastDownload, lastUpload int64
	var lastTime time.Time

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			resp, err := httpClient.Get("http://127.0.0.1:9090/connections")
			if err != nil {
				continue
			}

			var data map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&data)
			resp.Body.Close()

			now := time.Now()
			var currentDownload, currentUpload int64
			var activeConnections int

			if download, ok := data["downloadTotal"].(float64); ok {
				currentDownload = int64(download)
			}
			if upload, ok := data["uploadTotal"].(float64); ok {
				currentUpload = int64(upload)
			}
			if connections, ok := data["connections"].([]interface{}); ok {
				activeConnections = len(connections)
			}

			// 计算实时速度
			var downloadSpeed, uploadSpeed int64
			if !lastTime.IsZero() {
				elapsed := now.Sub(lastTime).Seconds()
				if elapsed > 0 {
					downloadSpeed = int64(float64(currentDownload-lastDownload) / elapsed)
					uploadSpeed = int64(float64(currentUpload-lastUpload) / elapsed)
				}
			}
			if downloadSpeed < 0 {
				downloadSpeed = 0
			}
			if uploadSpeed < 0 {
				uploadSpeed = 0
			}

			lastDownload = currentDownload
			lastUpload = currentUpload
			lastTime = now

			result := map[string]interface{}{
				"download":               currentDownload,
				"upload":                 currentUpload,
				"downloadSpeed":          downloadSpeed,
				"uploadSpeed":            uploadSpeed,
				"activeConnections":      activeConnections,
				"downloadFormatted":      formatBytes(currentDownload),
				"uploadFormatted":        formatBytes(currentUpload),
				"downloadSpeedFormatted": formatBytes(downloadSpeed) + "/s",
				"uploadSpeedFormatted":   formatBytes(uploadSpeed) + "/s",
			}

			jsonData, _ := json.Marshal(result)
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()
		}
	}
}
