package process

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ProcessState 进程状态
type ProcessState int

const (
	StateStopped ProcessState = iota
	StateStarting
	StateRunning
	StateStopping
)

func (s ProcessState) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	default:
		return "unknown"
	}
}

// LogEntry 日志条目
type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

// NetworkState 网络状态备份
type NetworkState struct {
	Routes     string // ip route 输出
	Rules      string // ip rule 输出
	Iptables   string // iptables-save 输出
	Interfaces string // ip link 输出
}

var (
	instance *Manager
	once     sync.Once
)

// Manager 进程管理器（单例）
type Manager struct {
	mu           sync.RWMutex
	state        ProcessState
	cmd          *exec.Cmd
	binaryPath   string
	configPath   string
	logs         []LogEntry
	maxLogs      int
	subscribers  map[string]chan LogEntry
	ctx          context.Context
	cancel       context.CancelFunc
	lastLog      string
	lastLogTime  time.Time
	networkState *NetworkState // 网络状态备份
}

// GetManager 获取进程管理器单例
func GetManager() *Manager {
	once.Do(func() {
		instance = &Manager{
			state:       StateStopped,
			logs:        make([]LogEntry, 0, 1000),
			maxLogs:     1000,
			subscribers: make(map[string]chan LogEntry),
		}
	})
	return instance
}

// Initialize 初始化进程管理器
func (m *Manager) Initialize(binaryPath, configPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.binaryPath = binaryPath
	m.configPath = configPath
}

// Start 启动进程
func (m *Manager) Start() error {
	m.mu.Lock()

	// 检查是否已有进程在运行
	if m.IsRunningInSystem() {
		m.mu.Unlock()
		return fmt.Errorf("进程已在运行")
	}

	m.state = StateStarting
	m.mu.Unlock()

	// 保存当前网络状态用于回退
	m.saveNetworkState()

	// 日志轮转：如果日志文件超过 10MB，备份并清空
	m.rotateLogFile()

	m.addLog(LogEntry{
		Time:    time.Now(),
		Level:   "info",
		Message: "已保存网络状态，准备启动 sing-box",
	})

	// 使用 nohup 启动 sing-box 作为独立守护进程，设置时区为 CST-8
	// 使用 >> 追加日志，避免覆盖
	cmd := exec.Command("sh", "-c",
		fmt.Sprintf("TZ='CST-8' nohup %s run -c %s >> /tmp/sing-box.log 2>&1 &",
			m.binaryPath, m.configPath))
	if err := cmd.Run(); err != nil {
		m.setState(StateStopped)
		return fmt.Errorf("启动进程失败: %w", err)
	}

	// 等待进程启动
	time.Sleep(1 * time.Second)

	// 检查进程是否成功启动
	if !m.IsRunningInSystem() {
		m.setState(StateStopped)
		// 读取日志查看错误
		if logData, err := os.ReadFile("/tmp/sing-box.log"); err == nil {
			lines := strings.Split(string(logData), "\n")
			// 取最后几行
			start := len(lines) - 10
			if start < 0 {
				start = 0
			}
			for _, line := range lines[start:] {
				if strings.Contains(line, "FATAL") || strings.Contains(line, "ERROR") {
					m.addLog(LogEntry{
						Time:    time.Now(),
						Level:   "error",
						Message: line,
					})
				}
			}
		}
		// 回退网络
		m.rollbackNetwork("进程启动失败")
		return fmt.Errorf("进程启动失败，请检查日志")
	}

	m.setState(StateRunning)
	m.addLog(LogEntry{
		Time:    time.Now(),
		Level:   "info",
		Message: "sing-box 进程已启动",
	})

	// 启动后进行连通性检测
	go m.checkConnectivityAndRollback()

	return nil
}

// Stop 停止进程
func (m *Manager) Stop() error {
	m.mu.Lock()

	// 检查内存中的状态
	if m.state == StateRunning && m.cmd != nil && m.cmd.Process != nil {
		// 内存中有进程，使用原有逻辑
		m.state = StateStopping
		m.mu.Unlock()

		// 取消上下文
		if m.cancel != nil {
			m.cancel()
		}

		// 发送 SIGTERM
		m.cmd.Process.Signal(syscall.SIGTERM)

		// 等待进程结束（最多 5 秒）
		done := make(chan struct{})
		go func() {
			m.cmd.Wait()
			close(done)
		}()

		select {
		case <-done:
			// 正常结束
		case <-time.After(5 * time.Second):
			// 强制杀死
			m.cmd.Process.Kill()
		}
	} else {
		m.mu.Unlock()

		// 检查系统中是否有 sing-box 进程
		pid := m.GetPID()
		if pid == 0 {
			return nil // 没有进程在运行
		}

		// 使用 kill 停止系统中的 sing-box 进程
		exec.Command("kill", "-TERM", fmt.Sprintf("%d", pid)).Run()

		// 等待进程结束
		for i := 0; i < 50; i++ { // 最多等待 5 秒
			time.Sleep(100 * time.Millisecond)
			if m.GetPID() == 0 {
				break
			}
		}

		// 如果还没停止，强制杀死
		if pid := m.GetPID(); pid > 0 {
			exec.Command("kill", "-KILL", fmt.Sprintf("%d", pid)).Run()
		}
	}

	m.setState(StateStopped)

	// 清理 TUN 设备
	cleanupTUN()

	m.addLog(LogEntry{
		Time:    time.Now(),
		Level:   "info",
		Message: "sing-box 已停止",
	})

	return nil
}

// Restart 重启进程
func (m *Manager) Restart() error {
	if err := m.Stop(); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return m.Start()
}

// GetState 获取进程状态
func (m *Manager) GetState() ProcessState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 如果内存中有状态，直接返回
	if m.state != StateStopped {
		return m.state
	}

	// 否则检测系统中是否有 sing-box 进程在运行
	if m.IsRunningInSystem() {
		return StateRunning
	}

	return m.state
}

// IsRunningInSystem 检测系统中是否有 sing-box 进程在运行
func (m *Manager) IsRunningInSystem() bool {
	cmd := exec.Command("pgrep", "-f", "sing-box run")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
}

// GetPID 获取进程 PID
func (m *Manager) GetPID() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 如果内存中有进程，返回其 PID
	if m.cmd != nil && m.cmd.Process != nil {
		return m.cmd.Process.Pid
	}

	// 否则从系统中查找 sing-box 进程的 PID
	cmd := exec.Command("pgrep", "-f", "sing-box run")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	pidStr := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	if pidStr == "" {
		return 0
	}
	var pid int
	fmt.Sscanf(pidStr, "%d", &pid)
	return pid
}

// GetLogs 获取日志
func (m *Manager) GetLogs(limit int) []LogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 || limit > len(m.logs) {
		limit = len(m.logs)
	}

	start := len(m.logs) - limit
	if start < 0 {
		start = 0
	}

	logs := make([]LogEntry, limit)
	copy(logs, m.logs[start:])
	return logs
}

// ClearLogs 清空日志
func (m *Manager) ClearLogs() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = make([]LogEntry, 0, m.maxLogs)
}

// Subscribe 订阅日志
func (m *Manager) Subscribe(id string) <-chan LogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan LogEntry, 100)
	m.subscribers[id] = ch
	return ch
}

// Unsubscribe 取消订阅
func (m *Manager) Unsubscribe(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ch, ok := m.subscribers[id]; ok {
		close(ch)
		delete(m.subscribers, id)
	}
}

// setState 设置状态
func (m *Manager) setState(state ProcessState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = state
}

// addLog 添加日志
func (m *Manager) addLog(entry LogEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addLogLocked(entry)
}

// addLogLocked 添加日志（需要持有锁）
func (m *Manager) addLogLocked(entry LogEntry) {
	// 日志去重
	if entry.Message == m.lastLog && time.Since(m.lastLogTime) < time.Second {
		return
	}
	m.lastLog = entry.Message
	m.lastLogTime = entry.Time

	// 添加日志
	m.logs = append(m.logs, entry)
	if len(m.logs) > m.maxLogs {
		m.logs = m.logs[1:]
	}

	// 通知订阅者
	for _, ch := range m.subscribers {
		select {
		case ch <- entry:
		default:
			// 通道满了，跳过
		}
	}
}

// readLogs 读取日志
func (m *Manager) readLogs(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	levelRe := regexp.MustCompile(`\[(DEBUG|INFO|WARN|ERROR|FATAL)\]`)
	ansiRe := regexp.MustCompile(`\x1b\[[0-9;]*m`)

	for scanner.Scan() {
		line := scanner.Text()

		// 清理 ANSI 颜色码
		line = ansiRe.ReplaceAllString(line, "")
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		// 解析日志级别
		level := "info"
		if matches := levelRe.FindStringSubmatch(line); len(matches) > 1 {
			level = strings.ToLower(matches[1])
		}

		m.addLog(LogEntry{
			Time:    time.Now(),
			Level:   level,
			Message: line,
		})
	}
}

// GetBinaryVersion 获取 sing-box 版本
func (m *Manager) GetBinaryVersion() string {
	m.mu.RLock()
	binaryPath := m.binaryPath
	m.mu.RUnlock()

	cmd := exec.Command(binaryPath, "version")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return "unknown"
}

// CheckConfig 检查配置文件
func (m *Manager) CheckConfig() error {
	m.mu.RLock()
	binaryPath := m.binaryPath
	configPath := m.configPath
	m.mu.RUnlock()

	cmd := exec.Command(binaryPath, "check", "-c", configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("配置检查失败: %s", string(output))
	}
	return nil
}

// cleanupTUN 清理 TUN 设备，恢复网络
func cleanupTUN() {
	// 删除 TUN 设备
	exec.Command("ip", "link", "del", "singtun0").Run()
	// 清理可能残留的路由规则
	exec.Command("ip", "rule", "del", "fwmark", "0x2023").Run()
	exec.Command("ip", "rule", "del", "fwmark", "0x2024").Run()
}

// saveNetworkState 保存当前网络状态
func (m *Manager) saveNetworkState() {
	state := &NetworkState{}

	// 保存路由表
	if output, err := exec.Command("ip", "route", "show").Output(); err == nil {
		state.Routes = string(output)
	}

	// 保存路由规则
	if output, err := exec.Command("ip", "rule", "show").Output(); err == nil {
		state.Rules = string(output)
	}

	// 保存 iptables 规则
	if output, err := exec.Command("iptables-save").Output(); err == nil {
		state.Iptables = string(output)
	}

	// 保存网络接口状态
	if output, err := exec.Command("ip", "link", "show").Output(); err == nil {
		state.Interfaces = string(output)
	}

	m.mu.Lock()
	m.networkState = state
	m.mu.Unlock()

	// 同时保存到文件，以便重启后也能恢复
	m.saveNetworkStateToFile(state)
}

// saveNetworkStateToFile 保存网络状态到文件
func (m *Manager) saveNetworkStateToFile(state *NetworkState) {
	stateDir := "/etc/sing-box"

	// 保存路由表
	if err := os.WriteFile(stateDir+"/network_routes.bak", []byte(state.Routes), 0644); err != nil {
		log.Printf("[网络] 保存路由表失败: %v", err)
	}

	// 保存路由规则
	if err := os.WriteFile(stateDir+"/network_rules.bak", []byte(state.Rules), 0644); err != nil {
		log.Printf("[网络] 保存路由规则失败: %v", err)
	}

	// 保存 iptables
	if err := os.WriteFile(stateDir+"/network_iptables.bak", []byte(state.Iptables), 0644); err != nil {
		log.Printf("[网络] 保存 iptables 失败: %v", err)
	}
}

// checkConnectivityAndRollback 检测连通性，失败时回退
func (m *Manager) checkConnectivityAndRollback() {
	// 等待 TUN 设备建立
	time.Sleep(2 * time.Second)

	// 检查进程是否还在运行
	if m.GetState() != StateRunning {
		return
	}

	// 进行连通性检测
	if !m.checkConnectivity() {
		m.addLog(LogEntry{
			Time:    time.Now(),
			Level:   "error",
			Message: "连通性检测失败，准备回退网络配置",
		})
		m.rollbackNetwork("连通性检测失败")
	} else {
		m.addLog(LogEntry{
			Time:    time.Now(),
			Level:   "info",
			Message: "连通性检测通过，sing-box 启动成功",
		})
	}
}

// checkConnectivity 检测网络连通性
func (m *Manager) checkConnectivity() bool {
	// 测试目标列表（国内外都测试）
	targets := []string{
		"223.5.5.5:53",      // 阿里 DNS
		"119.29.29.29:53",   // 腾讯 DNS
		"114.114.114.114:53", // 114 DNS
	}

	// 尝试 TCP 连接测试
	for _, target := range targets {
		conn, err := net.DialTimeout("tcp", target, 3*time.Second)
		if err == nil {
			conn.Close()
			return true
		}
	}

	// 尝试 HTTP 请求测试
	httpTargets := []string{
		"http://www.baidu.com",
		"http://www.qq.com",
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for _, url := range httpTargets {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return true
		}
	}

	return false
}

// rollbackNetwork 回退网络配置
func (m *Manager) rollbackNetwork(reason string) {
	m.addLog(LogEntry{
		Time:    time.Now(),
		Level:   "warn",
		Message: fmt.Sprintf("开始回退网络: %s", reason),
	})

	// 首先停止 sing-box 进程
	m.stopProcessOnly()

	// 清理 TUN 设备和路由规则
	cleanupTUN()

	// 删除所有 sing-box 相关的路由规则
	m.cleanupRoutingRules()

	// 尝试从备份恢复
	m.mu.RLock()
	state := m.networkState
	m.mu.RUnlock()

	if state != nil {
		m.restoreFromState(state)
	} else {
		// 尝试从文件恢复
		m.restoreFromFile()
	}

	// 最后验证网络是否恢复
	time.Sleep(1 * time.Second)
	if m.checkConnectivity() {
		m.addLog(LogEntry{
			Time:    time.Now(),
			Level:   "info",
			Message: "网络已成功恢复",
		})
	} else {
		m.addLog(LogEntry{
			Time:    time.Now(),
			Level:   "error",
			Message: "网络恢复可能不完整，请检查网络配置",
		})
	}
}

// stopProcessOnly 仅停止进程，不清理网络
func (m *Manager) stopProcessOnly() {
	m.mu.Lock()
	if m.cmd != nil && m.cmd.Process != nil {
		m.cmd.Process.Signal(syscall.SIGTERM)
		m.mu.Unlock()

		// 等待进程结束
		time.Sleep(2 * time.Second)

		// 强制杀死
		m.mu.Lock()
		if m.cmd != nil && m.cmd.Process != nil {
			m.cmd.Process.Kill()
		}
	}
	m.state = StateStopped
	m.mu.Unlock()

	// 也检查系统中的进程
	if pid := m.GetPID(); pid > 0 {
		exec.Command("kill", "-TERM", fmt.Sprintf("%d", pid)).Run()
		time.Sleep(1 * time.Second)
		if pid := m.GetPID(); pid > 0 {
			exec.Command("kill", "-KILL", fmt.Sprintf("%d", pid)).Run()
		}
	}
}

// cleanupRoutingRules 清理所有 sing-box 相关的路由规则
func (m *Manager) cleanupRoutingRules() {
	// 删除 TUN 设备
	exec.Command("ip", "link", "del", "singtun0").Run()
	exec.Command("ip", "link", "del", "tun0").Run()

	// 清理 fwmark 路由规则
	for i := 0; i < 10; i++ {
		exec.Command("ip", "rule", "del", "fwmark", "0x2023").Run()
		exec.Command("ip", "rule", "del", "fwmark", "0x2024").Run()
	}

	// 清理可能的路由表
	exec.Command("ip", "route", "flush", "table", "2023").Run()
	exec.Command("ip", "route", "flush", "table", "2024").Run()

	// 清理 iptables 中的 sing-box 相关规则
	exec.Command("iptables", "-t", "mangle", "-F").Run()
}

// restoreFromState 从内存状态恢复
func (m *Manager) restoreFromState(state *NetworkState) {
	// 恢复默认路由（最重要）
	lines := strings.Split(state.Routes, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "default") {
			// 解析默认路由
			parts := strings.Fields(line)
			if len(parts) >= 5 {
				// default via X.X.X.X dev ethX
				exec.Command("ip", "route", "add", "default", "via", parts[2], "dev", parts[4]).Run()
			}
		}
	}

	// 恢复 iptables
	if state.Iptables != "" {
		cmd := exec.Command("iptables-restore")
		cmd.Stdin = strings.NewReader(state.Iptables)
		cmd.Run()
	}
}

// restoreFromFile 从文件恢复网络状态
func (m *Manager) restoreFromFile() {
	stateDir := "/etc/sing-box"

	// 恢复路由
	if data, err := os.ReadFile(stateDir + "/network_routes.bak"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "default") {
				parts := strings.Fields(line)
				if len(parts) >= 5 {
					exec.Command("ip", "route", "add", "default", "via", parts[2], "dev", parts[4]).Run()
				}
			}
		}
	}

	// 恢复 iptables
	if data, err := os.ReadFile(stateDir + "/network_iptables.bak"); err == nil {
		cmd := exec.Command("iptables-restore")
		cmd.Stdin = strings.NewReader(string(data))
		cmd.Run()
	}
}

// rotateLogFile 日志轮转：保留最近的日志，超过大小限制时备份
func (m *Manager) rotateLogFile() {
	logFile := "/tmp/sing-box.log"
	maxSize := int64(10 * 1024 * 1024) // 10MB

	info, err := os.Stat(logFile)
	if err != nil {
		return // 文件不存在，无需轮转
	}

	if info.Size() > maxSize {
		// 备份旧日志
		backupFile := "/tmp/sing-box.log.1"
		if err := os.Remove(backupFile); err != nil && !os.IsNotExist(err) {
			log.Printf("[日志] 删除旧备份失败: %v", err)
		}
		if err := os.Rename(logFile, backupFile); err != nil {
			log.Printf("[日志] 重命名日志文件失败: %v", err)
			return
		}
		if err := os.WriteFile(logFile, []byte{}, 0644); err != nil {
			log.Printf("[日志] 创建新日志文件失败: %v", err)
		}
	}
}
