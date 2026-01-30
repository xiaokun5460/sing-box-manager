package api

import (
	"net/http"
	"strings"
)

// NewRouter 创建路由器
func NewRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// API 路由
	mux.HandleFunc("/api/status", corsMiddleware(handleStatus))
	mux.HandleFunc("/api/start", corsMiddleware(handleStart))
	mux.HandleFunc("/api/stop", corsMiddleware(handleStop))
	mux.HandleFunc("/api/restart", corsMiddleware(handleRestart))

	mux.HandleFunc("/api/subscriptions", corsMiddleware(handleSubscriptions))
	mux.HandleFunc("/api/subscriptions/", corsMiddleware(handleSubscription))
	mux.HandleFunc("/api/subscriptions/refresh-all", corsMiddleware(handleRefreshAll))

	mux.HandleFunc("/api/nodes", corsMiddleware(handleNodes))
	mux.HandleFunc("/api/nodes/test-all", corsMiddleware(handleTestAllNodes))
	mux.HandleFunc("/api/nodes/", corsMiddleware(handleNode))

	mux.HandleFunc("/api/mode", corsMiddleware(handleMode))
	mux.HandleFunc("/api/rules", corsMiddleware(handleRules))
	mux.HandleFunc("/api/bypass", corsMiddleware(handleBypass))

	mux.HandleFunc("/api/connections", corsMiddleware(handleConnections))
	mux.HandleFunc("/api/connections/", corsMiddleware(handleConnection))

	mux.HandleFunc("/api/traffic", corsMiddleware(handleTraffic))
	mux.HandleFunc("/api/traffic/realtime", handleTrafficRealtime) // SSE 不需要 CORS

	mux.HandleFunc("/api/ws/connections", handleConnectionsWS) // WebSocket 连接实时推送
	mux.HandleFunc("/api/ws/traffic", handleTrafficWS)         // WebSocket 流量实时推送

	mux.HandleFunc("/api/logs", corsMiddleware(handleLogs))
	mux.HandleFunc("/api/logs/stream", handleLogStream) // SSE 不需要 CORS
	mux.HandleFunc("/api/logs/clear", corsMiddleware(handleClearLogs))
	mux.HandleFunc("/api/logs/level", corsMiddleware(handleLogLevel))

	mux.HandleFunc("/api/config", corsMiddleware(handleConfig))
	mux.HandleFunc("/api/cache/clear", corsMiddleware(handleClearCache))

	// 静态文件和 SPA
	mux.HandleFunc("/", handleStatic)

	return mux
}

// corsMiddleware CORS 中间件
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// 只允许本地访问
		if origin != "" {
			if strings.HasPrefix(origin, "http://localhost") ||
				strings.HasPrefix(origin, "http://127.0.0.1") ||
				strings.HasPrefix(origin, "http://192.168.") {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			}
		}

		// 处理预检请求
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// handleStatic 处理静态文件
func handleStatic(w http.ResponseWriter, r *http.Request) {
	// 防止路径遍历
	if strings.Contains(r.URL.Path, "..") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// 静态文件
	if r.URL.Path == "/app.js" {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Write([]byte(appJS))
		return
	}

	// SPA: 所有非 API 请求返回 index.html
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		serveIndex(w, r)
		return
	}
}

// serveIndex 返回 index.html
func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}
