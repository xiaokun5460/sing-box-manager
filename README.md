# sing-box-manager

一个简洁高效的 [sing-box](https://github.com/SagerNet/sing-box) 代理管理工具，支持**智能分流**、**DNS 防泄漏**、命令行和 Web 界面双模式管理。

专为 **OpenWrt 软路由** 设计，让全屋设备无感翻墙。

![Version](https://img.shields.io/badge/Version-3.0.0-blue?style=flat-square)
![Web 界面](https://img.shields.io/badge/Web_UI-Tailwind_CSS-38B2AC?style=flat-square)
![协议支持](https://img.shields.io/badge/协议-SS%20%7C%20VMess%20%7C%20VLESS-blue?style=flat-square)
![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)

## 为什么选择 sing-box-manager？

- **开箱即用** - 一条命令初始化，自动生成最优分流配置
- **无需手写配置** - 告别复杂的 JSON 配置文件
- **智能分流** - 国内直连、国外代理，访问速度和隐私兼得
- **DNS 防泄漏** - 未知域名通过代理查询，保护隐私
- **现代化界面** - 深色主题 Web 面板，手机电脑都能用

## v3.0.0 新特性

- **模块化架构** - 代码重构为清晰的模块结构 (api/config/generator/process/subscription)
- **专业 YAML 解析** - 使用 `gopkg.in/yaml.v3` 库解析 Clash 订阅，更稳定可靠
- **轮询替代 SSE** - Web 界面改用轮询机制，解决长连接假死问题
- **智能轮询** - 页面不可见时自动暂停轮询，节省资源
- **实时流量统计** - 仪表盘显示实时上传/下载速度

## 功能特性

### 核心功能
- **智能分流** - 国内直连、国外代理，基于 BGP IP 列表和 GeoSite 规则
- **DNS 防泄漏** - FakeIP + 代理 DNS，未知域名通过代理查询
- **三种代理模式** - 规则模式、全局代理、直连模式一键切换

### 节点管理
- **多协议支持** - Shadowsocks、VLESS、VMess
- **多订阅格式** - 支持标准 URI 和 Clash YAML 格式订阅（使用专业 YAML 库解析）
- **节点测速** - TCP 延迟测试和 HTTP 连通性测试
- **智能切换** - 自动选择最快可用节点
- **故障检测** - 定时检测连接状态，失败自动切换

### Web 管理界面
- **现代化设计** - Tailwind CSS + Alpine.js，深色主题
- **仪表盘** - 服务状态、内存占用、TUN 状态、运行时间、实时流量
- **节点管理** - 搜索过滤、类型筛选、延迟显示、一键切换
- **订阅管理** - 添加、编辑、更新订阅
- **规则管理** - 自定义分流规则（域名/IP/进程）
- **日志查看** - 搜索过滤、级别筛选、日志级别设置
- **连接管理** - 实时查看活跃连接、流量统计、断开连接

## 系统要求

| 要求 | 说明 |
|------|------|
| **sing-box** | >= 1.12.0 (支持新版 DNS 和路由规则格式) |
| **操作系统** | Linux (推荐 OpenWrt) |
| **权限** | Root 权限 |
| **架构** | x86_64 / ARM64 |

## 安装

### 方式一：下载预编译版本

```bash
# 下载最新版本 (根据你的架构选择)
# x86_64
wget https://github.com/xiaokun5460/sing-box-manager/releases/latest/download/sb-linux-amd64 -O sb

# ARM64
wget https://github.com/xiaokun5460/sing-box-manager/releases/latest/download/sb-linux-arm64 -O sb

# 安装
chmod +x sb
sudo mv sb /usr/bin/sb
```

### 方式二：从源码编译

```bash
# 克隆仓库
git clone https://github.com/xiaokun5460/sing-box-manager.git
cd sing-box-manager

# 编译 (本机)
go build -o sb .

# 交叉编译 (OpenWrt x86_64)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o sb .

# 交叉编译 (OpenWrt ARM64)
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o sb .

# 安装到系统目录
sudo cp sb /usr/bin/sb
```

## 快速开始

只需 3 步，即可完成配置：

### 第一步：添加订阅

通过 Web 界面或命令行添加订阅：

```bash
# 启动 Web 界面后，在"订阅"页面添加订阅链接
sb web

# 或使用命令行添加订阅文件
sudo mkdir -p /etc/sing-box-manager
sudo nano /etc/sing-box-manager/subscriptions.txt
```

**支持的订阅格式：**

| 格式 | 示例 |
|------|------|
| Shadowsocks URI | `ss://base64...` |
| VMess URI | `vmess://base64...` |
| VLESS URI | `vless://uuid@server:port?...` |
| Clash YAML | 直接粘贴 Clash 订阅链接 |

### 第二步：更新订阅并启动

```bash
sb update   # 从订阅链接获取节点
sb start    # 启动 sing-box
```

### 第三步：访问 Web 界面

```bash
# 启动 Web 管理界面
sb web
```

然后访问 `http://你的IP:7788` 即可使用 Web 界面管理。

## Web 管理界面

现代化的 Web 管理面板，功能一览：

### 仪表盘
- 服务运行状态（运行中/已停止）
- 内存占用实时显示
- TUN 接口状态
- 运行时间统计
- 当前节点和延迟
- 实时上传/下载速度
- 代理模式快速切换

### 节点管理
- 节点列表展示（名称、类型、服务器、延迟）
- 关键词搜索过滤
- 按协议类型筛选（SS/VMess/VLESS）
- 一键切换节点
- 批量测速

### 订阅管理
- 添加/编辑/删除订阅
- 一键更新所有订阅
- 支持 Clash YAML 格式自动转换

### 日志查看
- 实时日志滚动
- 关键词搜索过滤
- 级别筛选（debug/info/warn/error）
- 日志级别动态设置
- 清理 DNS 缓存

### 连接管理
- 实时活跃连接列表
- 连接详情（目标地址、规则、出站、流量）
- 断开指定连接
- 流量统计

## 代理模式

| 模式 | 说明 | 适用场景 |
|------|------|----------|
| `rule` | **规则模式** - 国内直连，国外代理 | 日常使用（推荐） |
| `global` | **全局模式** - 所有流量走代理 | 需要全程代理时 |
| `direct` | **直连模式** - 所有流量直连 | 临时关闭代理 |

**切换方式：**

```bash
# 命令行
sb mode rule    # 切换到规则模式
sb mode global  # 切换到全局模式
sb mode direct  # 切换到直连模式

# Web 界面
# 在仪表盘页面点击对应模式按钮即可
```

## 命令参考

### 常用命令

| 命令 | 简写 | 说明 |
|------|------|------|
| `sb status` | `sb s` | 显示 sing-box 运行状态 |
| `sb list [关键词]` | `sb l` | 列出节点，可按关键词过滤 |
| `sb switch <编号>` | `sb sw` | 切换到指定编号的节点 |
| `sb mode <模式>` | `sb m` | 切换代理模式 |
| `sb update` | `sb u` | 更新订阅，获取最新节点 |
| `sb test` | `sb t` | 测试当前节点连通性 |
| `sb web` | - | 启动 Web 管理界面 (端口 7788) |

### 进阶命令

| 命令 | 简写 | 说明 |
|------|------|------|
| `sb speed [数量]` | `sb sp` | 测速，默认测试前 10 个节点 |
| `sb auto` | - | 自动切换到最快可用节点 |
| `sb check` | - | 检测连接，失败时自动切换 |
| `sb init` | - | 初始化/重建完整配置 |
| `sb restart` | `sb r` | 重启 sing-box |
| `sb start` | - | 启动 sing-box |
| `sb stop` | - | 停止 sing-box |
| `sb log` | - | 查看最近 100 行日志 |
| `sb cron [on N\|off]` | - | 定时检测开关，N 为间隔分钟 |
| `sb version` | `sb v` | 显示版本号 |

### 使用示例

```bash
# 查看当前状态
sb status

# 列出所有香港节点
sb list 香港

# 切换到第 3 个节点
sb switch 3

# 测试前 5 个节点速度
sb speed 5

# 自动选择最快节点
sb auto

# 开启每 5 分钟自动检测
sb cron on 5
```

## 分流规则说明

### DNS 分流策略

sing-box-manager 采用智能 DNS 分流，确保国内域名使用国内 DNS，国外域名通过代理查询：

```
┌─────────────────────────────────────────────────────────────┐
│                      DNS 查询流程                            │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  HTTPS/SVCB 查询                                            │
│      └─→ 代理 DNS (1.1.1.1 via proxy)                       │
│          └─→ 防止 FakeIP 不支持错误                          │
│                                                             │
│  代理服务器域名                                              │
│      └─→ 国内 DNS (223.5.5.5)                               │
│          └─→ 真实 IP → 直连                                  │
│                                                             │
│  已知中国域名 (geosite-cn + china-domains)                   │
│      └─→ 国内 DNS (223.5.5.5)                               │
│          └─→ 真实 IP → 直连                                  │
│                                                             │
│  已知国外域名 (geosite-geolocation-!cn)                      │
│      └─→ FakeIP (198.18.x.x)                                │
│          └─→ 代理                                            │
│                                                             │
│  未知域名                                                    │
│      └─→ 代理 DNS (1.1.1.1 via proxy)                       │
│          └─→ 真实 IP                                         │
│          └─→ BGP IP 列表匹配                                 │
│              ├─→ 中国 IP → 直连                              │
│              └─→ 其他 IP → 代理                              │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 路由规则

流量处理顺序：

1. **DNS 劫持** - 接管系统 DNS 请求 (53 端口)
2. **协议嗅探** - 识别 HTTP/TLS/QUIC 等协议
3. **域名解析** - FakeIP 转真实 IP (resolve action)
4. **私有网络直连** - 10.x, 192.168.x, 172.16.x 等内网地址
5. **中国域名直连** - geosite-cn + dnsmasq-china-list
6. **中国 IP 直连** - BGP 路由表 (比 MaxMind GeoIP 更准确)
7. **其他流量代理** - 默认走代理出站

### 规则集来源

| 规则集 | 来源 | 用途 |
|--------|------|------|
| chnroutes-bgp | [chnroutes2](https://github.com/misakaio/chnroutes2) | BGP 中国 IP (更准确) |
| china-domains | [dnsmasq-china-list](https://github.com/felixonmars/dnsmasq-china-list) | 中国域名加速列表 |
| geosite-cn | [sing-geosite](https://github.com/SagerNet/sing-geosite) | 中国域名规则 |
| geosite-geolocation-!cn | sing-geosite | 国外域名规则 |

## OpenWrt 部署

### 开机自启配置

在 OpenWrt 上配置开机自启：

```bash
# 1. sing-box 服务
cat > /etc/init.d/sing-box << 'EOF'
#!/bin/sh /etc/rc.common
START=90
STOP=15
USE_PROCD=1

start_service() {
    procd_open_instance
    procd_set_param command /usr/bin/sing-box run -c /etc/sing-box/config.json -D /etc/sing-box
    procd_set_param respawn
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_set_param file /etc/sing-box/config.json
    procd_close_instance
}
EOF
chmod +x /etc/init.d/sing-box
/etc/init.d/sing-box enable

# 2. Web 管理界面服务
cat > /etc/init.d/sb-web << 'EOF'
#!/bin/sh /etc/rc.common
START=99
STOP=10
USE_PROCD=1

start_service() {
    procd_open_instance
    procd_set_param command /usr/bin/sb web
    procd_set_param respawn
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}
EOF
chmod +x /etc/init.d/sb-web
/etc/init.d/sb-web enable
```

### 服务管理

```bash
# sing-box 服务
/etc/init.d/sing-box start    # 启动
/etc/init.d/sing-box stop     # 停止
/etc/init.d/sing-box restart  # 重启
/etc/init.d/sing-box status   # 状态

# Web 管理界面
/etc/init.d/sb-web start      # 启动
/etc/init.d/sb-web stop       # 停止
```

## 文件说明

| 路径 | 说明 |
|------|------|
| `/etc/sing-box-manager/config.yaml` | 管理器配置文件 |
| `/etc/sing-box-manager/state.json` | 运行状态（代理模式、当前节点） |
| `/etc/sing-box-manager/cache/` | 订阅缓存目录 |
| `/etc/sing-box/config.json` | sing-box 主配置文件（自动生成） |
| `/etc/sing-box/cache.db` | sing-box DNS 和规则缓存 |
| `/var/log/sing-box.log` | sing-box 运行日志 |

## 项目结构

```
sing-box-manager/
├── main.go                 # 入口文件和 CLI 命令
├── internal/
│   ├── api/               # HTTP API 和 Web 界面
│   │   ├── handlers.go    # API 处理函数
│   │   ├── server.go      # HTTP 服务器
│   │   └── web/           # 前端资源 (HTML/JS/CSS)
│   ├── config/            # 配置管理
│   ├── generator/         # sing-box 配置生成
│   ├── openwrt/           # OpenWrt 集成
│   ├── process/           # 进程管理
│   ├── service/           # 业务逻辑层
│   ├── subscription/      # 订阅解析 (URI/Clash YAML)
│   └── utils/             # 工具函数
└── deploy.sh              # 部署脚本
```

## 常见问题

### Q: 首次启动很慢？
A: 首次启动需要下载规则集文件（约 10MB），请耐心等待。后续启动会使用缓存。

### Q: 如何查看是否生效？
A: 访问 [ip.sb](https://ip.sb) 或 [ipinfo.io](https://ipinfo.io)，如果显示代理服务器 IP 则说明生效。

### Q: 国内网站变慢了？
A: 检查是否使用了 `rule` 模式。如果仍然慢，可能是规则集未正确加载，尝试 `sb init` 重新初始化。

### Q: 订阅更新失败？
A: 检查订阅链接是否有效，以及网络是否能访问订阅地址。Clash 格式订阅会自动转换。

### Q: Web 界面无法访问？
A: 确认 `sb web` 已启动，检查防火墙是否放行 7788 端口。

### Q: 如何更换端口？
A: 目前 Web 端口固定为 7788，Clash API 端口为 9090。

## 注意事项

1. **sing-box 版本** - 需要 sing-box >= 1.12.0，支持新版 DNS 和路由规则格式
2. **首次启动** - 首次启动会下载规则集文件，可能需要等待几秒
3. **权限要求** - 部分操作需要 root 权限（如启停服务、修改配置）
4. **网络环境** - 测速功能使用 Google 和 Cloudflare 进行连通性测试
5. **OpenWrt 适配** - 使用 auto_redirect 自动管理 nftables 规则

## 开源协议

MIT License

## 致谢

- [sing-box](https://github.com/SagerNet/sing-box) - 优秀的代理内核
- [dnsmasq-china-list](https://github.com/felixonmars/dnsmasq-china-list) - 中国域名列表
- [chnroutes2](https://github.com/misakaio/chnroutes2) - BGP 中国 IP 列表
- [Tailwind CSS](https://tailwindcss.com/) - 现代化 CSS 框架
- [Alpine.js](https://alpinejs.dev/) - 轻量级 JavaScript 框架
