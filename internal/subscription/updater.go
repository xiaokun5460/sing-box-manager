package subscription

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"sing-box-manager/internal/config"
)

var (
	updaterInstance *Updater
	updaterOnce     sync.Once
)

// Updater 订阅更新器（单例）
type Updater struct {
	mu           sync.RWMutex
	nodes        []config.Node
	cacheDir     string
	httpClient   *http.Client
	updateTicker *time.Ticker
	stopChan     chan struct{}
	isRunning    bool
}

// GetUpdater 获取订阅更新器单例
func GetUpdater() *Updater {
	updaterOnce.Do(func() {
		updaterInstance = &Updater{
			nodes: make([]config.Node, 0),
			httpClient: &http.Client{
				Timeout: 30 * time.Second,
			},
		}
	})
	return updaterInstance
}

// Initialize 初始化更新器
func (u *Updater) Initialize(cacheDir string) error {
	u.mu.Lock()
	u.cacheDir = cacheDir
	u.mu.Unlock()

	// 确保缓存目录存在
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("创建缓存目录失败: %w", err)
	}

	// 同步加载节点数据（确保在返回前完成）
	u.rebuildNodes()

	return nil
}

// RefreshAll 刷新所有订阅（并发优化）
func (u *Updater) RefreshAll() error {
	configMgr := config.GetManager()
	subs := configMgr.GetSubscriptions()

	if len(subs) == 0 {
		return nil
	}

	// 并发更新所有订阅
	var wg sync.WaitGroup
	var mu sync.Mutex
	var lastErr error
	allNodes := make([][]config.Node, len(subs))

	for i, sub := range subs {
		wg.Add(1)
		go func(idx int, s config.Subscription) {
			defer wg.Done()

			nodes, err := u.fetchSubscription(s)
			if err != nil {
				log.Printf("[订阅] 更新失败 %s: %v", s.Name, err)
				mu.Lock()
				lastErr = err
				mu.Unlock()
				return
			}

			// 更新订阅信息
			s.UpdatedAt = time.Now()
			s.NodeCount = len(nodes)
			configMgr.UpdateSubscription(s)

			mu.Lock()
			allNodes[idx] = nodes
			mu.Unlock()
		}(i, sub)
	}

	wg.Wait()

	// 合并所有节点
	var mergedNodes []config.Node
	for _, nodes := range allNodes {
		mergedNodes = append(mergedNodes, nodes...)
	}

	// 重新编号
	for i := range mergedNodes {
		mergedNodes[i].Index = i + 1
	}

	u.mu.Lock()
	u.nodes = mergedNodes
	u.mu.Unlock()

	return lastErr
}

// RefreshSubscription 刷新单个订阅
func (u *Updater) RefreshSubscription(subID string) error {
	configMgr := config.GetManager()
	subs := configMgr.GetSubscriptions()

	var targetSub *config.Subscription
	for i := range subs {
		if subs[i].ID == subID {
			targetSub = &subs[i]
			break
		}
	}

	if targetSub == nil {
		return fmt.Errorf("订阅不存在: %s", subID)
	}

	nodes, err := u.fetchSubscription(*targetSub)
	if err != nil {
		return err
	}

	// 更新订阅信息
	targetSub.UpdatedAt = time.Now()
	targetSub.NodeCount = len(nodes)
	configMgr.UpdateSubscription(*targetSub)

	// 重建节点列表
	return u.rebuildNodes()
}

// fetchSubscription 获取订阅内容
func (u *Updater) fetchSubscription(sub config.Subscription) ([]config.Node, error) {
	u.mu.RLock()
	cacheFile := u.getCacheFileLocked(sub.URL)
	u.mu.RUnlock()

	// 获取新内容
	resp, err := u.httpClient.Get(sub.URL)
	if err != nil {
		// 网络错误，尝试使用缓存
		if content, readErr := os.ReadFile(cacheFile); readErr == nil {
			log.Printf("[订阅] 网络错误，使用缓存: %s", sub.Name)
			return ParseSubscription(string(content), sub.ID)
		}
		return nil, fmt.Errorf("获取订阅失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// HTTP 错误，尝试使用缓存
		if content, readErr := os.ReadFile(cacheFile); readErr == nil {
			log.Printf("[订阅] HTTP %d，使用缓存: %s", resp.StatusCode, sub.Name)
			return ParseSubscription(string(content), sub.ID)
		}
		return nil, fmt.Errorf("订阅返回错误状态: %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取订阅内容失败: %w", err)
	}

	// 保存到缓存（记录错误但不影响返回）
	if err := os.WriteFile(cacheFile, content, 0644); err != nil {
		log.Printf("[订阅] 缓存写入失败: %v", err)
	}

	return ParseSubscription(string(content), sub.ID)
}

// rebuildNodes 重建节点列表（加锁版本）
func (u *Updater) rebuildNodes() error {
	configMgr := config.GetManager()
	subs := configMgr.GetSubscriptions()

	var allNodes []config.Node

	u.mu.RLock()
	cacheDir := u.cacheDir
	u.mu.RUnlock()

	for _, sub := range subs {
		hash := sha256.Sum256([]byte(sub.URL))
		filename := hex.EncodeToString(hash[:8]) + ".txt"
		cacheFile := filepath.Join(cacheDir, filename)

		content, err := os.ReadFile(cacheFile)
		if err != nil {
			log.Printf("[订阅] 读取缓存失败 %s: %v", sub.Name, err)
			continue
		}

		nodes, err := ParseSubscription(string(content), sub.ID)
		if err != nil {
			log.Printf("[订阅] 解析失败 %s: %v", sub.Name, err)
			continue
		}

		allNodes = append(allNodes, nodes...)
	}

	// 重新编号
	for i := range allNodes {
		allNodes[i].Index = i + 1
	}

	u.mu.Lock()
	u.nodes = allNodes
	u.mu.Unlock()

	return nil
}

// getCacheFileLocked 获取缓存文件路径（需要持有读锁）
func (u *Updater) getCacheFileLocked(url string) string {
	hash := sha256.Sum256([]byte(url))
	filename := hex.EncodeToString(hash[:8]) + ".txt"
	return filepath.Join(u.cacheDir, filename)
}

// GetNodes 获取所有节点
func (u *Updater) GetNodes() []config.Node {
	u.mu.RLock()
	defer u.mu.RUnlock()

	nodes := make([]config.Node, len(u.nodes))
	copy(nodes, u.nodes)
	return nodes
}

// GetNode 获取指定节点
func (u *Updater) GetNode(index int) *config.Node {
	u.mu.RLock()
	defer u.mu.RUnlock()

	if index < 1 || index > len(u.nodes) {
		return nil
	}

	node := u.nodes[index-1]
	return &node
}

// GetNodeCount 获取节点数量
func (u *Updater) GetNodeCount() int {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return len(u.nodes)
}

// StartAutoUpdate 启动自动更新
func (u *Updater) StartAutoUpdate(interval time.Duration) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.isRunning {
		return
	}

	u.stopChan = make(chan struct{})
	u.updateTicker = time.NewTicker(interval)
	u.isRunning = true

	go func() {
		for {
			select {
			case <-u.updateTicker.C:
				if err := u.RefreshAll(); err != nil {
					log.Printf("[订阅] 自动更新失败: %v", err)
				}
			case <-u.stopChan:
				return
			}
		}
	}()
}

// StopAutoUpdate 停止自动更新
func (u *Updater) StopAutoUpdate() {
	u.mu.Lock()
	defer u.mu.Unlock()

	if !u.isRunning {
		return
	}

	if u.updateTicker != nil {
		u.updateTicker.Stop()
	}

	close(u.stopChan)
	u.isRunning = false
}

// IsAutoUpdateRunning 检查自动更新是否运行中
func (u *Updater) IsAutoUpdateRunning() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.isRunning
}

// ClearCache 清空缓存
func (u *Updater) ClearCache() error {
	u.mu.Lock()
	defer u.mu.Unlock()

	files, err := filepath.Glob(filepath.Join(u.cacheDir, "*.txt"))
	if err != nil {
		return err
	}

	var lastErr error
	for _, f := range files {
		if err := os.Remove(f); err != nil {
			log.Printf("[订阅] 删除缓存文件失败 %s: %v", f, err)
			lastErr = err
		}
	}

	return lastErr
}
