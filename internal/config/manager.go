package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	instance *Manager
	once     sync.Once
)

// Manager 配置管理器（单例）
type Manager struct {
	mu         sync.RWMutex
	config     *Config
	state      *State
	dataDir    string
	configFile string
	stateFile  string
	onChange   []func()
}

// GetManager 获取配置管理器单例
func GetManager() *Manager {
	once.Do(func() {
		instance = &Manager{
			onChange: make([]func(), 0),
		}
	})
	return instance
}

// Initialize 初始化配置管理器
func (m *Manager) Initialize(dataDir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.dataDir = dataDir
	m.configFile = filepath.Join(dataDir, "config.yaml")
	m.stateFile = filepath.Join(dataDir, "state.yaml")

	// 确保目录存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	// 加载或创建配置
	if err := m.loadConfig(); err != nil {
		return err
	}

	// 加载或创建状态
	if err := m.loadState(); err != nil {
		return err
	}

	return nil
}

// loadConfig 加载配置文件
func (m *Manager) loadConfig() error {
	m.config = DefaultConfig()

	data, err := os.ReadFile(m.configFile)
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在，使用默认配置并保存
			return m.saveConfigLocked()
		}
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := yaml.Unmarshal(data, m.config); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	return nil
}

// loadState 加载状态文件
func (m *Manager) loadState() error {
	m.state = DefaultState()

	data, err := os.ReadFile(m.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			// 状态文件不存在，使用默认状态并保存
			return m.saveStateLocked()
		}
		return fmt.Errorf("读取状态文件失败: %w", err)
	}

	if err := yaml.Unmarshal(data, m.state); err != nil {
		return fmt.Errorf("解析状态文件失败: %w", err)
	}

	return nil
}

// saveConfigLocked 保存配置（需要持有锁）
func (m *Manager) saveConfigLocked() error {
	data, err := yaml.Marshal(m.config)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.WriteFile(m.configFile, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

// saveStateLocked 保存状态（需要持有锁）
func (m *Manager) saveStateLocked() error {
	data, err := yaml.Marshal(m.state)
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}

	if err := os.WriteFile(m.stateFile, data, 0600); err != nil {
		return fmt.Errorf("写入状态文件失败: %w", err)
	}

	return nil
}

// GetConfig 获取配置（只读）
func (m *Manager) GetConfig() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return *m.config
}

// GetState 获取状态（只读）
func (m *Manager) GetState() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return *m.state
}

// UpdateConfig 更新配置
func (m *Manager) UpdateConfig(cfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = cfg
	if err := m.saveConfigLocked(); err != nil {
		return err
	}

	m.notifyChange()
	return nil
}

// SetProxyMode 设置代理模式
func (m *Manager) SetProxyMode(mode string) error {
	if mode != "rule" && mode != "global" && mode != "direct" {
		return fmt.Errorf("无效的代理模式: %s", mode)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.ProxyMode = mode
	if err := m.saveStateLocked(); err != nil {
		return err
	}

	m.notifyChange()
	return nil
}

// SetCurrentNode 设置当前节点
func (m *Manager) SetCurrentNode(name string, index int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.CurrentNode = name
	m.state.NodeIndex = index
	return m.saveStateLocked()
}

// AddSubscription 添加订阅
func (m *Manager) AddSubscription(sub Subscription) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查是否已存在
	for _, s := range m.state.Subscriptions {
		if s.URL == sub.URL {
			return fmt.Errorf("订阅已存在")
		}
	}

	if sub.ID == "" {
		sub.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	sub.UpdatedAt = time.Now()

	m.state.Subscriptions = append(m.state.Subscriptions, sub)
	return m.saveStateLocked()
}

// RemoveSubscription 删除订阅
func (m *Manager) RemoveSubscription(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, s := range m.state.Subscriptions {
		if s.ID == id {
			m.state.Subscriptions = append(m.state.Subscriptions[:i], m.state.Subscriptions[i+1:]...)
			return m.saveStateLocked()
		}
	}

	return fmt.Errorf("订阅不存在: %s", id)
}

// UpdateSubscription 更新订阅信息
func (m *Manager) UpdateSubscription(sub Subscription) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, s := range m.state.Subscriptions {
		if s.ID == sub.ID {
			m.state.Subscriptions[i] = sub
			return m.saveStateLocked()
		}
	}

	return fmt.Errorf("订阅不存在: %s", sub.ID)
}

// GetSubscriptions 获取所有订阅
func (m *Manager) GetSubscriptions() []Subscription {
	m.mu.RLock()
	defer m.mu.RUnlock()

	subs := make([]Subscription, len(m.state.Subscriptions))
	copy(subs, m.state.Subscriptions)
	return subs
}

// SetCustomRules 设置自定义规则
func (m *Manager) SetCustomRules(rules []CustomRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.CustomRules = rules
	if err := m.saveStateLocked(); err != nil {
		return err
	}

	m.notifyChange()
	return nil
}

// GetCustomRules 获取自定义规则
func (m *Manager) GetCustomRules() []CustomRule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rules := make([]CustomRule, len(m.state.CustomRules))
	copy(rules, m.state.CustomRules)
	return rules
}

// OnChange 注册配置变更回调
func (m *Manager) OnChange(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = append(m.onChange, fn)
}

// notifyChange 通知配置变更
func (m *Manager) notifyChange() {
	for _, fn := range m.onChange {
		go fn()
	}
}

// GetDataDir 获取数据目录
func (m *Manager) GetDataDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dataDir
}

// GetCacheDir 获取缓存目录
func (m *Manager) GetCacheDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return filepath.Join(m.dataDir, "cache")
}

// Load 加载配置（使用默认数据目录）
func (m *Manager) Load() error {
	dataDir := "/etc/sing-box-manager"
	return m.Initialize(dataDir)
}

// GetBypass 获取绕过配置
func (m *Manager) GetBypass() BypassConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Bypass
}

// SetBypass 设置绕过配置
func (m *Manager) SetBypass(bypass BypassConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config.Bypass = bypass
	if err := m.saveConfigLocked(); err != nil {
		return err
	}

	m.notifyChange()
	return nil
}
