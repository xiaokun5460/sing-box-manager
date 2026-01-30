package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"sing-box-manager/internal/api"
	"sing-box-manager/internal/config"
	"sing-box-manager/internal/openwrt"
	"sing-box-manager/internal/process"
	"sing-box-manager/internal/service"
	"sing-box-manager/internal/subscription"
)

const Version = "3.0.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// 初始化配置管理器
	configMgr := config.GetManager()
	if err := configMgr.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化进程管理器
	cfg := configMgr.GetConfig()
	processMgr := process.GetManager()
	processMgr.Initialize(cfg.SingBox.BinaryPath, cfg.SingBox.ConfigPath)

	// 初始化订阅更新器
	updater := subscription.GetUpdater()
	updater.Initialize(configMgr.GetCacheDir())

	cmd := os.Args[1]
	switch cmd {
	case "start":
		cmdStart()
	case "stop":
		cmdStop()
	case "restart":
		cmdRestart()
	case "status":
		cmdStatus()
	case "update":
		cmdUpdate()
	case "node":
		if len(os.Args) < 3 {
			cmdNodeList()
		} else {
			cmdNodeSelect(os.Args[2])
		}
	case "mode":
		if len(os.Args) < 3 {
			cmdModeShow()
		} else {
			cmdModeSet(os.Args[2])
		}
	case "web":
		cmdWeb()
	case "version":
		cmdVersion()
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`Sing-Box Manager v%s

用法:
  sing-box-manager <命令> [参数]

命令:
  start           启动 sing-box 服务
  stop            停止 sing-box 服务
  restart         重启 sing-box 服务
  status          查看服务状态
  update          更新订阅
  node            列出所有节点
  node <index>    切换到指定节点
  mode            查看当前代理模式
  mode <mode>     设置代理模式 (rule/global/direct)
  web             启动 Web 管理界面
  version         显示版本信息
`, Version)
}

func cmdStart() {
	if err := service.RegenerateConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "生成配置失败: %v\n", err)
		os.Exit(1)
	}

	processMgr := process.GetManager()
	if err := processMgr.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("服务已启动")
}

func cmdStop() {
	processMgr := process.GetManager()
	if err := processMgr.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "停止失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("服务已停止")
}

func cmdRestart() {
	if err := service.RegenerateConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "生成配置失败: %v\n", err)
		os.Exit(1)
	}

	processMgr := process.GetManager()
	if err := processMgr.Restart(); err != nil {
		fmt.Fprintf(os.Stderr, "重启失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("服务已重启")
}

func cmdStatus() {
	processMgr := process.GetManager()
	configMgr := config.GetManager()
	updater := subscription.GetUpdater()
	state := configMgr.GetState()

	fmt.Printf("状态: %s\n", processMgr.GetState().String())
	if pid := processMgr.GetPID(); pid > 0 {
		fmt.Printf("PID: %d\n", pid)
	}
	fmt.Printf("版本: %s\n", processMgr.GetBinaryVersion())
	fmt.Printf("代理模式: %s\n", state.ProxyMode)
	fmt.Printf("当前节点: %s\n", state.CurrentNode)
	fmt.Printf("节点数量: %d\n", updater.GetNodeCount())
}

func cmdUpdate() {
	updater := subscription.GetUpdater()
	fmt.Println("正在更新订阅...")
	if err := updater.RefreshAll(); err != nil {
		fmt.Fprintf(os.Stderr, "更新失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("更新完成，共 %d 个节点\n", updater.GetNodeCount())

	// 重新生成配置
	if err := service.RegenerateConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "生成配置失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("配置已更新")
}

func cmdNodeList() {
	updater := subscription.GetUpdater()
	configMgr := config.GetManager()
	state := configMgr.GetState()
	nodes := updater.GetNodes()

	if len(nodes) == 0 {
		fmt.Println("暂无节点，请先添加订阅")
		return
	}

	for i, node := range nodes {
		marker := "  "
		if i+1 == state.NodeIndex {
			marker = "* "
		}
		fmt.Printf("%s%d. [%s] %s\n", marker, i+1, node.Type, node.Name)
	}
}

func cmdNodeSelect(indexStr string) {
	var index int
	if _, err := fmt.Sscanf(indexStr, "%d", &index); err != nil {
		fmt.Fprintf(os.Stderr, "无效的节点索引\n")
		os.Exit(1)
	}

	updater := subscription.GetUpdater()
	node := updater.GetNode(index)
	if node == nil {
		fmt.Fprintf(os.Stderr, "节点不存在\n")
		os.Exit(1)
	}

	if err := service.SwitchNode(index); err != nil {
		fmt.Fprintf(os.Stderr, "切换节点失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("已切换到: %s\n", node.Name)
}

func cmdModeShow() {
	configMgr := config.GetManager()
	state := configMgr.GetState()
	fmt.Printf("当前模式: %s\n", state.ProxyMode)
}

func cmdModeSet(mode string) {
	configMgr := config.GetManager()
	if err := configMgr.SetProxyMode(mode); err != nil {
		fmt.Fprintf(os.Stderr, "设置模式失败: %v\n", err)
		os.Exit(1)
	}

	if err := service.RegenerateAndRestart(); err != nil {
		fmt.Fprintf(os.Stderr, "重启失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("模式已切换为: %s\n", mode)
}

func cmdWeb() {
	configMgr := config.GetManager()
	cfg := configMgr.GetConfig()

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	fmt.Printf("Web 管理界面启动于 http://%s\n", addr)

	if openwrt.IsOpenWrt() {
		openwrt.SetupSubscriptionCron(cfg.Subscription.AutoUpdate, cfg.Subscription.UpdateInterval)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n正在关闭...")
		process.GetManager().Stop()
		os.Exit(0)
	}()

	router := api.NewRouter()
	if err := http.ListenAndServe(addr, router); err != nil {
		fmt.Fprintf(os.Stderr, "启动 Web 服务失败: %v\n", err)
		os.Exit(1)
	}
}

func cmdVersion() {
	fmt.Printf("Sing-Box Manager v%s\n", Version)
	fmt.Printf("Sing-Box: %s\n", process.GetManager().GetBinaryVersion())
}
