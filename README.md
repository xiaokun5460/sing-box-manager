# sing-box-manager

一个简洁高效的 [sing-box](https://github.com/SagerNet/sing-box) 代理管理工具，支持命令行和 Web 界面双模式管理。

## 功能特性

- **多协议支持** - Shadowsocks、VLESS、VMess
- **订阅管理** - 支持从订阅链接自动更新节点
- **节点测速** - TCP 延迟测试和 HTTP 连通性测试
- **智能切换** - 自动选择最快可用节点
- **故障检测** - 定时检测连接状态，失败自动切换
- **Web 界面** - 现代化深色主题管理面板
- **配置备份** - 切换节点时自动备份配置

## 安装

### 从源码编译

```bash
# 克隆仓库
git clone https://github.com/xiaokun5460/sing-box-manager.git
cd sing-box-manager

# 编译
go build -o sb main.go

# 安装到系统目录 (需要 root 权限)
sudo cp sb /usr/bin/sb
```

### 前置要求

- 已安装 [sing-box](https://sing-box.sagernet.org/installation/)
- 配置文件位于 `/etc/sing-box/config.json`

## 快速开始

### 1. 添加订阅

创建订阅文件 `/etc/sing-box/subscriptions.txt`，每行一个订阅链接：

```bash
sudo mkdir -p /etc/sing-box
sudo nano /etc/sing-box/subscriptions.txt
```

### 2. 更新订阅

```bash
sb update
```

### 3. 查看节点列表

```bash
sb list
```

### 4. 切换节点

```bash
sb switch 1  # 切换到第 1 个节点
```

### 5. 启动 Web 管理界面

```bash
sb web
```

然后访问 `http://你的IP:7788`

## 命令参考

| 命令 | 简写 | 说明 |
|------|------|------|
| `sb status` | `sb s` | 显示 sing-box 运行状态 |
| `sb list [关键词]` | `sb l` | 列出节点，可按关键词过滤 |
| `sb switch <编号>` | `sb sw` | 切换到指定编号的节点 |
| `sb update` | `sb u` | 更新订阅，获取最新节点 |
| `sb test` | `sb t` | 测试当前节点连通性 |
| `sb speed [数量]` | `sb sp` | 测速，默认测试前 10 个节点 |
| `sb auto` | - | 自动切换到最快可用节点 |
| `sb check` | - | 检测连接，失败时自动切换 |
| `sb restart` | `sb r` | 重启 sing-box |
| `sb start` | - | 启动 sing-box |
| `sb stop` | - | 停止 sing-box |
| `sb log` | - | 查看最近 100 行日志 |
| `sb cron [on N\|off]` | - | 定时检测开关，N 为间隔分钟 |
| `sb web` | - | 启动 Web 管理界面 (端口 7788) |
| `sb version` | `sb v` | 显示版本号 |

## 使用示例

### 查看状态

```bash
$ sb status
=== sing-box 状态 ===
状态: 运行中 (PID: 12345)
内存: 24560 kB
运行时间: 02:30:15
TUN接口: 已创建
当前节点: 香港01 [VLESS]
服务器: hk1.example.com:443
定时检测: 每 5 分钟
```

### 列出并过滤节点

```bash
$ sb list 香港
=== 节点列表 ===
   1) [VL] 香港01                    (hk1.example.com:443)
   2) [VL] 香港02                    (hk2.example.com:443)
   5) [SS] 香港-SS                   (hk-ss.example.com:8388)
共 3 个节点
```

### 测速并查看结果

```bash
$ sb speed 5
[INFO] 测速前 5 个节点...
[5/5] 测试: 日本02

=== 测速结果 ===
排名 编号 节点名称                       延迟
-------------------------------------------------------
1    2    香港02                        185 ms
2    1    香港01                        203 ms
3    5    日本02                        289 ms
4    3    新加坡01                      342 ms
5    4    日本01                        超时

可用: 4 / 5
[INFO] 最快: #2 香港02 (185 ms)
```

### 开启定时检测

```bash
# 每 5 分钟检测一次
sb cron on 5

# 关闭定时检测
sb cron off
```

## Web 管理界面

启动 Web 服务后，可以通过浏览器访问现代化的管理面板：

```bash
sb web
```

Web 界面功能：
- 实时查看运行状态
- 一键切换节点
- 节点搜索和类型过滤
- 批量测速
- 自动选择最快节点
- 订阅管理
- 日志查看
- 定时检测配置

## 文件说明

| 路径 | 说明 |
|------|------|
| `/etc/sing-box/config.json` | sing-box 主配置文件 |
| `/etc/sing-box/config.json.bak` | 配置备份 |
| `/etc/sing-box/nodes.txt` | 节点列表缓存 |
| `/etc/sing-box/subscriptions.txt` | 订阅链接 |
| `/etc/sing-box/current_node.txt` | 当前节点信息 |
| `/var/log/sing-box.log` | sing-box 日志 |
| `/var/log/sb-check.log` | 定时检测日志 |

## 注意事项

1. **权限要求** - 部分操作需要 root 权限（如启停服务、修改配置）
2. **配置兼容性** - 需要 sing-box 配置文件第一个 outbound 为代理节点
3. **网络环境** - 测速功能使用 Google 和 Cloudflare 进行连通性测试
4. **OpenWrt 适配** - 定时任务使用 `/etc/crontabs/root`

## 开源协议

MIT License

## 致谢

- [sing-box](https://github.com/SagerNet/sing-box) - 优秀的代理内核
