function app() {
    return {
        page: 'dashboard',
        menu: [
            { id: 'dashboard', name: '仪表盘', icon: '◉' },
            { id: 'nodes', name: '节点', icon: '◎' },
            { id: 'subs', name: '订阅', icon: '☰' },
            { id: 'rules', name: '规则', icon: '⚙' },
            { id: 'conns', name: '连接', icon: '⇄' },
            { id: 'logs', name: '日志', icon: '☷' },
            { id: 'settings', name: '设置', icon: '⚡' }
        ],
        status: {},
        nodes: [],
        subs: [],
        bypass: { bypass_lan: true, bypass_china: true, block_ads: false },
        rules: [],
        conns: [],
        connStats: {},
        logs: [],
        config: { dns: { domestic_servers: ['223.5.5.5'], proxy_servers: ['8.8.8.8'], use_fakeip: true }, singbox: { log_level: 'info' } },
        toasts: [],
        showSubModal: false,
        showRuleModal: false,
        editingSubId: null,
        subForm: { name: '', url: '' },
        ruleForm: { type: 'domain', value: '', outbound: 'proxy' },
        // 搜索筛选
        nodeSearch: '',
        connSearch: '',
        connSort: 'download',
        logLevel: 'all',
        logSearch: '',
        // 测速状态
        testing: false,
        nodeLatencies: {},
        // 实时流量
        traffic: { download: 0, upload: 0, downloadSpeed: 0, uploadSpeed: 0 },
        // 操作状态（防止重复点击）
        loading: {
            start: false,
            stop: false,
            restart: false,
            switchNode: false,
            refreshSubs: false,
            refreshSub: {},
            saveConfig: false,
            saveBypass: false,
            clearCache: false,
            clearLogs: false,
            closeConns: false,
            saveSub: false,
            deleteSub: {},
            addRule: false,
            deleteRule: {},
            setLogLevel: false,
        },
        switchingNodeIndex: null,  // 正在切换的节点索引

        async init() {
            await this.loadAll();
            this.startPolling();
            // 监听页面可见性变化
            document.addEventListener('visibilitychange', () => {
                this.pageVisible = !document.hidden;
            });
        },

        pageVisible: true,

        // 统一使用轮询，页面不可见时暂停
        startPolling() {
            // 每2秒更新状态和流量
            setInterval(async () => {
                if (!this.pageVisible) return;
                if (this.page === 'dashboard') {
                    await this.loadStatus();
                    await this.loadTraffic();
                }
            }, 2000);

            // 每秒更新连接
            setInterval(async () => {
                if (!this.pageVisible) return;
                if (this.page === 'conns') {
                    await this.loadConns();
                }
            }, 1000);

            // 每2秒更新日志
            setInterval(async () => {
                if (!this.pageVisible) return;
                if (this.page === 'logs') {
                    await this.loadLogs();
                }
            }, 2000);
        },

        async loadTraffic() {
            const res = await this.api('/traffic');
            if (res.success && res.data) {
                this.traffic = res.data;
            }
        },

        async loadLogs() {
            const res = await this.api('/logs?limit=100');
            if (res.success && res.data) {
                this.logs = res.data;
            }
        },

        async loadAll() {
            await Promise.all([
                this.loadStatus(),
                this.loadNodes(),
                this.loadSubs(),
                this.loadBypass(),
                this.loadRules(),
                this.loadConfig(),
                this.loadLogLevel()
            ]);
        },

        async api(path, method = 'GET', body = null) {
            try {
                const opts = { method, headers: { 'Content-Type': 'application/json' } };
                if (body) opts.body = JSON.stringify(body);
                const res = await fetch('/api' + path, opts);
                const data = await res.json();
                if (!data.success && data.message) {
                    this.toast(data.message, 'error');
                } else if (data.message) {
                    this.toast(data.message);
                }
                if (path.includes('/start') || path.includes('/stop') || path.includes('/restart')) {
                    setTimeout(() => this.loadStatus(), 500);
                }
                return data;
            } catch (e) {
                this.toast('请求失败: ' + e.message, 'error');
                return { success: false };
            }
        },

        async loadStatus() {
            const res = await this.api('/status');
            if (res.success) this.status = res.data;
        },

        async loadNodes() {
            const res = await this.api('/nodes');
            if (res.success) {
                this.nodes = (res.data || []).map((n, i) => ({ ...n, index: i + 1 }));
            }
        },

        async loadSubs() {
            const res = await this.api('/subscriptions');
            if (res.success) this.subs = res.data || [];
        },

        async loadBypass() {
            const res = await this.api('/bypass');
            if (res.success && res.data) this.bypass = res.data;
        },

        async loadRules() {
            const res = await this.api('/rules');
            if (res.success) this.rules = res.data || [];
        },

        async loadConfig() {
            const res = await this.api('/config');
            if (res.success && res.data) {
                this.config = res.data;
                if (!this.config.dns) {
                    this.config.dns = { domestic_servers: ['223.5.5.5'], proxy_servers: ['8.8.8.8'], use_fakeip: true };
                }
                if (!this.config.dns.domestic_servers) this.config.dns.domestic_servers = ['223.5.5.5'];
                if (!this.config.dns.proxy_servers) this.config.dns.proxy_servers = ['8.8.8.8'];
                if (!this.config.singbox) this.config.singbox = { log_level: 'info' };
            }
        },

        async loadLogLevel() {
            const res = await this.api('/logs/level');
            if (res.success && res.data) {
                this.config.singbox = this.config.singbox || {};
                this.config.singbox.log_level = res.data.level;
            }
        },

        async startService() {
            if (this.loading.start || this.status.state === 'running') return;
            this.loading.start = true;
            try {
                await this.api('/start', 'POST');
                await this.loadStatus();
            } finally {
                this.loading.start = false;
            }
        },

        async stopService() {
            if (this.loading.stop || this.status.state !== 'running') return;
            this.loading.stop = true;
            try {
                await this.api('/stop', 'POST');
                await this.loadStatus();
            } finally {
                this.loading.stop = false;
            }
        },

        async restartService() {
            if (this.loading.restart) return;
            this.loading.restart = true;
            try {
                await this.api('/restart', 'POST');
                await this.loadStatus();
            } finally {
                this.loading.restart = false;
            }
        },

        async loadConns() {
            const sort = this.connSort ? `?sort=${this.connSort}&order=desc` : '';
            const res = await this.api('/connections' + sort);
            if (res.success && res.data) {
                this.conns = res.data.connections || [];
                this.connStats = {
                    downloadTotal: res.data.downloadTotal || 0,
                    uploadTotal: res.data.uploadTotal || 0
                };
            }
        },

        async selectNode(index) {
            if (this.loading.switchNode) return;
            this.loading.switchNode = true;
            this.switchingNodeIndex = index;
            try {
                const res = await this.api(`/nodes/${index}/select`, 'POST');
                if (res.success) {
                    await this.loadStatus();
                }
            } finally {
                this.loading.switchNode = false;
                this.switchingNodeIndex = null;
            }
        },

        async testNode(index) {
            this.nodeLatencies[index] = 'testing';
            const res = await this.api(`/nodes/${index}/test`, 'POST');
            if (res.success && res.data) {
                this.nodeLatencies[index] = res.data.latency;
            } else {
                this.nodeLatencies[index] = -1;
            }
        },

        async testAllNodes() {
            if (this.testing) return;
            this.testing = true;
            this.nodeLatencies = {};

            const res = await this.api('/nodes/test-all', 'POST');
            if (res.success && res.data) {
                res.data.forEach(r => {
                    this.nodeLatencies[r.index] = r.latency;
                });
            }
            this.testing = false;
        },

        async setMode(mode) {
            if (this.loading.restart) return;
            this.loading.restart = true;
            try {
                await this.api('/mode', 'PUT', { mode });
                await this.loadStatus();
            } finally {
                this.loading.restart = false;
            }
        },

        async refreshSubs() {
            if (this.loading.refreshSubs) return;
            this.loading.refreshSubs = true;
            try {
                await this.api('/subscriptions/refresh-all', 'POST');
                await Promise.all([this.loadSubs(), this.loadNodes(), this.loadStatus()]);
            } finally {
                this.loading.refreshSubs = false;
            }
        },

        async refreshOneSub(id) {
            if (this.loading.refreshSub[id]) return;
            this.loading.refreshSub[id] = true;
            try {
                await this.api(`/subscriptions/${id}/refresh`, 'POST');
                await Promise.all([this.loadSubs(), this.loadNodes()]);
            } finally {
                this.loading.refreshSub[id] = false;
            }
        },

        editSub(sub) {
            this.editingSubId = sub.id;
            this.subForm = { name: sub.name || '', url: sub.url || '' };
            this.showSubModal = true;
        },

        async saveSub() {
            if (!this.subForm.url) {
                this.toast('URL 不能为空', 'error');
                return;
            }
            if (this.loading.saveSub) return;
            this.loading.saveSub = true;
            try {
                if (this.editingSubId) {
                    await this.api(`/subscriptions/${this.editingSubId}`, 'PUT', this.subForm);
                } else {
                    await this.api('/subscriptions', 'POST', this.subForm);
                }
                this.showSubModal = false;
                this.editingSubId = null;
                this.subForm = { name: '', url: '' };
                await this.loadSubs();
            } finally {
                this.loading.saveSub = false;
            }
        },

        async deleteSub(id) {
            if (!confirm('确定删除此订阅?')) return;
            if (this.loading.deleteSub[id]) return;
            this.loading.deleteSub[id] = true;
            try {
                await this.api(`/subscriptions/${id}`, 'DELETE');
                await Promise.all([this.loadSubs(), this.loadNodes()]);
            } finally {
                this.loading.deleteSub[id] = false;
            }
        },

        async saveBypass() {
            if (this.loading.saveBypass) return;
            this.loading.saveBypass = true;
            try {
                await this.api('/bypass', 'PUT', this.bypass);
            } finally {
                this.loading.saveBypass = false;
            }
        },

        async addRule() {
            if (!this.ruleForm.value) {
                this.toast('规则值不能为空', 'error');
                return;
            }
            if (this.loading.addRule) return;
            this.loading.addRule = true;
            try {
                this.rules.push({ ...this.ruleForm });
                await this.api('/rules', 'PUT', this.rules);
                this.showRuleModal = false;
                this.ruleForm = { type: 'domain', value: '', outbound: 'proxy' };
            } finally {
                this.loading.addRule = false;
            }
        },

        async deleteRule(index) {
            if (this.loading.deleteRule[index]) return;
            this.loading.deleteRule[index] = true;
            try {
                this.rules.splice(index, 1);
                await this.api('/rules', 'PUT', this.rules);
            } finally {
                this.loading.deleteRule[index] = false;
            }
        },

        async saveConfig() {
            if (this.loading.saveConfig) return;
            this.loading.saveConfig = true;
            try {
                await this.api('/config', 'PUT', this.config);
            } finally {
                this.loading.saveConfig = false;
            }
        },

        async setLogLevel(level) {
            if (this.loading.setLogLevel) return;
            this.loading.setLogLevel = true;
            try {
                await this.api('/logs/level', 'POST', { level });
                this.config.singbox.log_level = level;
            } finally {
                this.loading.setLogLevel = false;
            }
        },

        async clearCache() {
            if (this.loading.clearCache) return;
            this.loading.clearCache = true;
            try {
                await this.api('/cache/clear', 'POST');
            } finally {
                this.loading.clearCache = false;
            }
        },

        async clearLogs() {
            if (this.loading.clearLogs) return;
            this.loading.clearLogs = true;
            try {
                await this.api('/logs/clear', 'POST');
                this.logs = [];
            } finally {
                this.loading.clearLogs = false;
            }
        },

        async closeAllConns() {
            if (this.loading.closeConns) return;
            this.loading.closeConns = true;
            try {
                await this.api('/connections', 'DELETE');
                this.conns = [];
            } finally {
                this.loading.closeConns = false;
            }
        },

        // 切换排序时重新加载
        async changeConnSort(sort) {
            this.connSort = sort;
            await this.loadConns();
        },

        // 计算属性：筛选后的节点
        get filteredNodes() {
            if (!this.nodeSearch) return this.nodes;
            const s = this.nodeSearch.toLowerCase();
            return this.nodes.filter(n =>
                n.name.toLowerCase().includes(s) ||
                n.type.toLowerCase().includes(s) ||
                (n.server && n.server.toLowerCase().includes(s))
            );
        },

        // 计算属性：筛选后的连接
        get filteredConns() {
            if (!this.connSearch) return this.conns;
            const s = this.connSearch.toLowerCase();
            return this.conns.filter(c => {
                const host = c.metadata?.host || c.metadata?.destinationIP || '';
                const chains = (c.chains || []).join(' ');
                return host.toLowerCase().includes(s) || chains.toLowerCase().includes(s);
            });
        },

        // 计算属性：筛选后的日志
        get filteredLogs() {
            let logs = this.logs;
            if (this.logLevel !== 'all') {
                logs = logs.filter(l => l.level === this.logLevel);
            }
            if (this.logSearch) {
                const s = this.logSearch.toLowerCase();
                logs = logs.filter(l => l.message.toLowerCase().includes(s));
            }
            return logs;
        },

        formatBytes(bytes) {
            if (bytes === 0) return '0 B';
            const k = 1024;
            const sizes = ['B', 'KB', 'MB', 'GB'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return (bytes / Math.pow(k, i)).toFixed(1) + ' ' + sizes[i];
        },

        formatTime(ts) {
            if (!ts) return '';
            const d = new Date(ts);
            return d.toLocaleTimeString('zh-CN', { hour12: false });
        },

        formatLatency(index) {
            const l = this.nodeLatencies[index];
            if (l === undefined) return '';
            if (l === 'testing') return '...';
            if (l === -1) return '超时';
            return l + 'ms';
        },

        latencyColor(index) {
            const l = this.nodeLatencies[index];
            if (l === undefined || l === 'testing') return 'text-gray-500';
            if (l === -1) return 'text-red-400';
            if (l < 100) return 'text-green-400';
            if (l < 300) return 'text-yellow-400';
            return 'text-orange-400';
        },

        toast(msg, type = 'success') {
            this.toasts.push({ msg, type });
            setTimeout(() => this.toasts.shift(), 3000);
        }
    };
}
