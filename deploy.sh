#!/bin/bash
# 编译并部署到 OpenWrt 路由器

set -e

# 配置
ROUTER_IP="192.168.1.1"
ROUTER_USER="root"
ROUTER_PASS="password"
REMOTE_PATH="/usr/bin/sb"
SERVICE_NAME="sb-web"

# 目标架构 (OpenWrt x86_64 musl)
export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0

echo "==> 编译 sb (linux/amd64 静态链接)..."
go build -ldflags="-s -w" -o sb .

echo "==> 停止服务并等待进程退出..."
sshpass -p "${ROUTER_PASS}" ssh -o StrictHostKeyChecking=no ${ROUTER_USER}@${ROUTER_IP} "
    /etc/init.d/${SERVICE_NAME} stop 2>/dev/null || true
    killall sb 2>/dev/null || true
    sleep 2
    killall -9 sb 2>/dev/null || true
    sleep 1
"

echo "==> 上传到 ${ROUTER_IP}..."
sshpass -p "${ROUTER_PASS}" scp -O -o StrictHostKeyChecking=no sb ${ROUTER_USER}@${ROUTER_IP}:${REMOTE_PATH}

echo "==> 启动服务..."
sshpass -p "${ROUTER_PASS}" ssh -o StrictHostKeyChecking=no ${ROUTER_USER}@${ROUTER_IP} "
    chmod +x ${REMOTE_PATH}
    # 清理旧的 TUN 设备（缓存文件保留，避免 fakeip record 丢失）
    ip link delete singtun0 2>/dev/null || true
    # 启动 Web 管理界面
    /etc/init.d/${SERVICE_NAME} start
    sleep 1
    # 检查节点缓存，如果为空则更新订阅
    if [ -z \"\$(ls -A /etc/sing-box-manager/cache/ 2>/dev/null)\" ]; then
        echo '节点缓存为空，正在更新订阅...'
        ${REMOTE_PATH} update
    fi
    # 使用 sb restart 重新生成配置并启动 sing-box
    ${REMOTE_PATH} restart
"

echo "==> 验证服务状态..."
sleep 2
sshpass -p "${ROUTER_PASS}" ssh -o StrictHostKeyChecking=no ${ROUTER_USER}@${ROUTER_IP} "
    echo '--- sb-web ---'
    if ps | grep -v grep | grep 'sb web' > /dev/null; then
        echo '管理界面: 运行中'
    else
        echo '管理界面: 未运行'
    fi

    echo '--- sing-box ---'
    if ps | grep -v grep | grep 'sing-box run' > /dev/null; then
        echo 'sing-box: 运行中'
    else
        echo 'sing-box: 未运行'
    fi

    echo '--- 端口 ---'
    netstat -tlnp 2>/dev/null | grep -E '7788|9090' || true
"

echo ""
echo "==> 完成! 访问: http://${ROUTER_IP}:7788"
