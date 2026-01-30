package config

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 7788,
		},
		SingBox: SingBoxConfig{
			BinaryPath: "/usr/bin/sing-box",
			ConfigPath: "/etc/sing-box/config.json",
			LogLevel:   "info",
		},
		DNS: DNSConfig{
			DomesticServers: []string{"223.5.5.5", "119.29.29.29"},
			ProxyServers:    []string{"1.1.1.1", "8.8.8.8"},
			UseDoH:          false,
			UseFakeIP:       true,
			FakeIPRange:     "198.18.0.0/15",
			FakeIP6Range:    "fc00::/18",
			CacheCapacity:   50000,
		},
		TUN: TUNConfig{
			Name:         "singtun0",
			Address:      "172.19.0.1/30",
			Address6:     "fdfe:dcba:9876::1/126",
			MTU:          9000,
			AutoRoute:    true,
			AutoRedirect: true,
			Stack:        "mixed",
		},
		Subscription: SubscriptionConfig{
			AutoUpdate:     true,
			UpdateInterval: 60,
		},
		Bypass: BypassConfig{
			BypassLAN:   true,
			BypassChina: true,
			BlockAds:    false,
		},
	}
}

// DefaultState 返回默认状态
func DefaultState() *State {
	return &State{
		ProxyMode:     "rule",
		CurrentNode:   "",
		NodeIndex:     0,
		Subscriptions: []Subscription{},
		CustomRules:   []CustomRule{},
	}
}
