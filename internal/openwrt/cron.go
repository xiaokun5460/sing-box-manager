package openwrt

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// CronManager Cron 任务管理器
type CronManager struct {
	cronFile string
	marker   string
}

// NewCronManager 创建 Cron 管理器
func NewCronManager(cronFile string) *CronManager {
	if cronFile == "" {
		cronFile = DefaultCronFile
	}
	return &CronManager{
		cronFile: cronFile,
		marker:   "# sing-box-manager",
	}
}

// Enable 启用定时任务
func (c *CronManager) Enable(updateInterval, checkInterval int) error {
	// 先移除旧的任务
	if err := c.Disable(); err != nil {
		return err
	}

	// 读取现有 cron 文件
	content, err := os.ReadFile(c.cronFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("读取 cron 文件失败: %w", err)
	}

	// 构建新任务
	var tasks []string
	tasks = append(tasks, c.marker)

	if updateInterval > 0 {
		tasks = append(tasks, fmt.Sprintf("*/%d * * * * /usr/bin/sb update >/dev/null 2>&1", updateInterval))
	}

	if checkInterval > 0 {
		tasks = append(tasks, fmt.Sprintf("*/%d * * * * /usr/bin/sb check >/dev/null 2>&1", checkInterval))
	}

	tasks = append(tasks, c.marker+" end")

	// 追加到文件
	newContent := string(content)
	if !strings.HasSuffix(newContent, "\n") && len(newContent) > 0 {
		newContent += "\n"
	}
	newContent += strings.Join(tasks, "\n") + "\n"

	if err := os.WriteFile(c.cronFile, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("写入 cron 文件失败: %w", err)
	}

	// 重启 cron 服务
	return c.restartCron()
}

// Disable 禁用定时任务
func (c *CronManager) Disable() error {
	content, err := os.ReadFile(c.cronFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 cron 文件失败: %w", err)
	}

	// 移除标记之间的内容
	lines := strings.Split(string(content), "\n")
	var newLines []string
	inBlock := false

	for _, line := range lines {
		if strings.HasPrefix(line, c.marker) && !strings.HasSuffix(line, "end") {
			inBlock = true
			continue
		}
		if strings.HasPrefix(line, c.marker) && strings.HasSuffix(line, "end") {
			inBlock = false
			continue
		}
		if !inBlock {
			newLines = append(newLines, line)
		}
	}

	// 移除末尾空行
	for len(newLines) > 0 && newLines[len(newLines)-1] == "" {
		newLines = newLines[:len(newLines)-1]
	}

	newContent := strings.Join(newLines, "\n")
	if len(newContent) > 0 {
		newContent += "\n"
	}

	if err := os.WriteFile(c.cronFile, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("写入 cron 文件失败: %w", err)
	}

	return c.restartCron()
}

// GetStatus 获取 cron 状态
func (c *CronManager) GetStatus() (enabled bool, updateInterval, checkInterval int, err error) {
	content, err := os.ReadFile(c.cronFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, 0, nil
		}
		return false, 0, 0, err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	inBlock := false
	updateRe := regexp.MustCompile(`\*/(\d+) .* /usr/bin/sb update`)
	checkRe := regexp.MustCompile(`\*/(\d+) .* /usr/bin/sb check`)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, c.marker) && !strings.HasSuffix(line, "end") {
			inBlock = true
			enabled = true
			continue
		}
		if strings.HasPrefix(line, c.marker) && strings.HasSuffix(line, "end") {
			inBlock = false
			continue
		}

		if inBlock {
			if matches := updateRe.FindStringSubmatch(line); len(matches) > 1 {
				updateInterval, _ = strconv.Atoi(matches[1])
			}
			if matches := checkRe.FindStringSubmatch(line); len(matches) > 1 {
				checkInterval, _ = strconv.Atoi(matches[1])
			}
		}
	}

	return enabled, updateInterval, checkInterval, nil
}

// restartCron 重启 cron 服务
func (c *CronManager) restartCron() error {
	cmd := exec.Command("/etc/init.d/cron", "restart")
	return cmd.Run()
}

// SetupSubscriptionCron 设置订阅更新 cron 任务
func SetupSubscriptionCron(enabled bool, intervalMinutes int) error {
	cron := NewCronManager("")
	if enabled && intervalMinutes > 0 {
		return cron.Enable(intervalMinutes, 0)
	}
	return cron.Disable()
}
