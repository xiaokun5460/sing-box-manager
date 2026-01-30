package openwrt

import (
	"os"
	"path/filepath"
)

// OpenWrt 路径常量
const (
	DefaultDataDir       = "/etc/sing-box"
	DefaultConfigFile    = "/etc/sing-box/config.json"
	DefaultStateFile     = "/etc/sing-box/state.yaml"
	DefaultAppConfigFile = "/etc/sing-box/app.yaml"
	DefaultCacheDir      = "/etc/sing-box/cache"
	DefaultNodesFile     = "/etc/sing-box/nodes.txt"
	DefaultCronFile      = "/etc/crontabs/root"
	DefaultInitScript    = "/etc/init.d/sing-box"
	DefaultBinaryPath    = "/usr/bin/sing-box"
	DefaultSBPath        = "/usr/bin/sb"
)

// Paths OpenWrt 路径配置
type Paths struct {
	DataDir    string
	ConfigFile string
	StateFile  string
	CacheDir   string
	NodesFile  string
	CronFile   string
	BinaryPath string
}

// DefaultPaths 返回默认路径配置
func DefaultPaths() *Paths {
	return &Paths{
		DataDir:    DefaultDataDir,
		ConfigFile: DefaultConfigFile,
		StateFile:  DefaultStateFile,
		CacheDir:   DefaultCacheDir,
		NodesFile:  DefaultNodesFile,
		CronFile:   DefaultCronFile,
		BinaryPath: DefaultBinaryPath,
	}
}

// EnsureDirectories 确保所有必要目录存在
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		p.DataDir,
		p.CacheDir,
		filepath.Dir(p.ConfigFile),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}

// IsOpenWrt 检测是否运行在 OpenWrt 上
func IsOpenWrt() bool {
	_, err := os.Stat("/etc/openwrt_release")
	if err == nil {
		return true
	}
	_, err = os.Stat("/etc/openwrt_version")
	return err == nil
}
