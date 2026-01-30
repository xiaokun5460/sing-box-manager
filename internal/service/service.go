package service

import (
	"fmt"

	"sing-box-manager/internal/config"
	"sing-box-manager/internal/generator"
	"sing-box-manager/internal/process"
	"sing-box-manager/internal/subscription"
)

// RegenerateConfig 重新生成 sing-box 配置
func RegenerateConfig() error {
	configMgr := config.GetManager()
	updater := subscription.GetUpdater()

	cfg := configMgr.GetConfig()
	state := configMgr.GetState()
	nodes := updater.GetNodes()

	gen := generator.NewGenerator(cfg, state, nodes, configMgr.GetDataDir())
	sbConfig, err := gen.Generate()
	if err != nil {
		return err
	}

	return gen.SaveConfig(sbConfig, cfg.SingBox.ConfigPath)
}

// RegenerateAndRestart 重新生成配置并重启服务
func RegenerateAndRestart() error {
	if err := RegenerateConfig(); err != nil {
		return err
	}

	processMgr := process.GetManager()
	if processMgr.GetState() == process.StateRunning {
		return processMgr.Restart()
	}

	return nil
}

// SwitchNode 切换节点
func SwitchNode(nodeIndex int) error {
	updater := subscription.GetUpdater()
	node := updater.GetNode(nodeIndex)
	if node == nil {
		return fmt.Errorf("节点不存在: %d", nodeIndex)
	}

	configMgr := config.GetManager()
	if err := configMgr.SetCurrentNode(node.Name, nodeIndex); err != nil {
		return err
	}

	return RegenerateAndRestart()
}
