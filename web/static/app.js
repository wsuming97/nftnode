// ============================================================
//  nftables & Realm 端口转发管理面板 - 前端交互逻辑
// ============================================================

document.addEventListener('DOMContentLoaded', () => {

    // ============================================================
    //  侧边栏折叠 & View 视图切换系统
    // ============================================================
    const appLayout = document.getElementById('appLayout');
    const toggleSidebarBtn = document.getElementById('toggleSidebarBtn');
    const navItems = document.querySelectorAll('.nav-item');
    const viewPanels = document.querySelectorAll('.view-panel');
    const pageTitle = document.getElementById('pageTitle');

    // 折叠/展开侧边栏
    if (toggleSidebarBtn && appLayout) {
        toggleSidebarBtn.addEventListener('click', () => {
            appLayout.classList.toggle('collapsed');
        });
    }

    // 视图切换
    navItems.forEach(item => {
        item.addEventListener('click', () => {
            const targetViewId = item.getAttribute('data-view');
            if (!targetViewId) return;

            // 切换 active class
            navItems.forEach(nav => nav.classList.remove('active'));
            item.classList.add('active');

            viewPanels.forEach(panel => {
                if (panel.id === targetViewId) {
                    panel.classList.add('active');
                } else {
                    panel.classList.remove('active');
                }
            });

            // 更新标题
            const titleSpan = item.querySelector('span');
            if (titleSpan && pageTitle) {
                pageTitle.textContent = titleSpan.textContent;
            }
        });
    });

    // ============================================================
    //  i18n 国际化系统 — 中英双语切换
    // ============================================================
    const I18N = {
        zh: {
            'nav.dashboard': '控制台概览',
            'nav.nftables': 'nftables 转发',
            'nav.realm': 'Realm 转发',
            'nav.nodes': '节点管理',
            'nav.tg': 'TG 推送设置',
            'nav.settings': '系统设置',
            'header.logout': '退出',
            'svc.start': '启动服务', 'svc.stop': '停止服务', 'svc.restart': '重启服务',
            'btn.refresh': '刷新', 'btn.cancel': '取消', 'btn.save': '保存',
            'overview.title': '节点总览与流量监控',
            'rules.title': '转发规则列表',
            'status.running': '运行中', 'status.stopped': '已停止', 'status.checking': '检测中...',
            'status.active': '正常', 'status.suspended': '已封停',
            'toast.addOk': '规则已添加', 'toast.editOk': '规则已更新',
            'toast.deleteOk': '规则已删除', 'toast.startOk': '服务已启动',
            'toast.stopOk': '服务已停止', 'toast.restartOk': '服务已重启',
            'toast.tgSaved': 'TG 配置已保存', 'toast.tgTestSent': '测试消息已发送',
            'toast.realmSaved': 'Realm 规则已保存',
        },
        en: {
            'nav.dashboard': 'Dashboard',
            'nav.nftables': 'nftables Forward',
            'nav.realm': 'Realm Forward',
            'nav.nodes': 'Node Management',
            'nav.tg': 'Telegram Bot',
            'nav.settings': 'Settings',
            'header.logout': 'Logout',
            'svc.start': 'Start', 'svc.stop': 'Stop', 'svc.restart': 'Restart',
            'btn.refresh': 'Refresh', 'btn.cancel': 'Cancel', 'btn.save': 'Save',
            'overview.title': 'Node Overview',
            'rules.title': 'Forward Rules',
            'status.running': 'Running', 'status.stopped': 'Stopped', 'status.checking': 'Checking...',
            'status.active': 'Active', 'status.suspended': 'Suspended',
            'toast.addOk': 'Rule added', 'toast.editOk': 'Rule updated',
            'toast.deleteOk': 'Rule deleted', 'toast.startOk': 'Service started',
            'toast.stopOk': 'Service stopped', 'toast.restartOk': 'Service restarted',
            'toast.tgSaved': 'TG config saved', 'toast.tgTestSent': 'Test message sent',
            'toast.realmSaved': 'Realm rule saved',
        }
    };

    let currentLang = localStorage.getItem('nft_panel_lang') || 'zh';

    function t(key, fallback = '') {
        return (I18N[currentLang] && I18N[currentLang][key]) || (I18N.zh[key] || fallback || key);
    }

    function applyI18n() {
        document.querySelectorAll('[data-i18n]').forEach(el => {
            const key = el.getAttribute('data-i18n');
            const text = t(key);
            if (text) el.textContent = text;
        });
        const langBtn = document.getElementById('langToggleBtn');
        if (langBtn) langBtn.textContent = currentLang === 'zh' ? 'EN' : '中文';
    }

    const langBtn = document.getElementById('langToggleBtn');
    if (langBtn) {
        langBtn.addEventListener('click', () => {
            currentLang = currentLang === 'zh' ? 'en' : 'zh';
            localStorage.setItem('nft_panel_lang', currentLang);
            applyI18n();
        });
    }
    applyI18n();

    // Toast 提示
    function showToast(msg, type = 'info') {
        const container = document.getElementById('toastContainer');
        if (!container) return;
        const toast = document.createElement('div');
        toast.className = `toast ${type}`;
        toast.textContent = msg;
        container.appendChild(toast);
        setTimeout(() => toast.remove(), 3000);
    }

    // API 请求封装
    async function apiRequest(url, options = {}) {
        try {
            const res = await fetch(url, {
                headers: { 'Content-Type': 'application/json' },
                ...options
            });
            if (res.status === 401) {
                window.location.href = '/login';
                return null;
            }
            return await res.json();
        } catch (err) {
            showToast('网络请求失败', 'error');
            return null;
        }
    }

    // 状态检测
    async function checkStatus() {
        const data = await apiRequest('/api/status');
        const badge = document.getElementById('statusBadge');
        const text = document.getElementById('statusText');
        if (!data || !badge || !text) return;

        if (data.running) {
            badge.className = 'status-badge running';
            text.textContent = t('status.running', '运行中');
        } else {
            badge.className = 'status-badge stopped';
            text.textContent = t('status.stopped', '已停止');
        }

        const banner = document.getElementById('gfwBanner');
        if (banner) {
            banner.hidden = !data.gfw_blocked;
        }
    }
    checkStatus();
    setInterval(checkStatus, 10000);

    // 服务控制按键
    const startBtn = document.getElementById('startBtn');
    const stopBtn = document.getElementById('stopBtn');
    const restartBtn = document.getElementById('restartBtn');

    if (startBtn) startBtn.onclick = () => apiRequest('/api/start', { method: 'POST' }).then(() => { showToast(t('toast.startOk'), 'success'); checkStatus(); });
    if (stopBtn) stopBtn.onclick = () => apiRequest('/api/stop', { method: 'POST' }).then(() => { showToast(t('toast.stopOk'), 'success'); checkStatus(); });
    if (restartBtn) restartBtn.onclick = () => apiRequest('/api/restart', { method: 'POST' }).then(() => { showToast(t('toast.restartOk'), 'success'); checkStatus(); });

    // 规则加载逻辑
    let currentRules = [];
    async function loadRules() {
        const data = await apiRequest('/api/rules');
        if (!data) return;
        currentRules = data.rules || [];
        renderRulesTable();
    }

    function renderRulesTable() {
        const tbody = document.getElementById('rulesBody');
        const countSpan = document.getElementById('ruleCount');
        if (!tbody) return;

        if (countSpan) countSpan.textContent = `共 ${currentRules.length} 条`;

        if (currentRules.length === 0) {
            tbody.innerHTML = '<tr class="empty-row"><td colspan="10">暂无 nftables 转发规则</td></tr>';
            return;
        }

        tbody.innerHTML = currentRules.map((rule, idx) => `
            <tr>
                <td>${idx + 1}</td>
                <td><strong>${rule.local_port}</strong></td>
                <td><span class="ip-tag ${rule.remote_addr.includes(':') ? 'v6' : 'v4'}">${rule.remote_addr.includes(':') ? 'IPv6' : 'IPv4'}</span></td>
                <td>${rule.remote_addr}</td>
                <td>${rule.remote_port}</td>
                <td>TCP + UDP</td>
                <td>${(rule.used_bytes / (1024*1024*1024)).toFixed(2)} GB / ${rule.quota_gb > 0 ? rule.quota_gb + ' GB' : '不限'}</td>
                <td><span class="status-tag ${rule.enabled ? 'active' : 'suspended'}">${rule.enabled ? '正常' : '已封停'}</span></td>
                <td>${rule.note || '-'}</td>
                <td>
                    <button class="btn btn-danger btn-sm" onclick="deleteRule('${rule.id}')">删除</button>
                </td>
            </tr>
        `).join('');
    }

    window.deleteRule = async function(id) {
        if (!confirm('确定删除此转发规则？')) return;
        const res = await apiRequest(`/api/rules/${id}`, { method: 'DELETE' });
        if (res && res.success) {
            showToast(t('toast.deleteOk'), 'success');
            loadRules();
        }
    };

    // 添加规则
    const addRuleForm = document.getElementById('addRuleForm');
    if (addRuleForm) {
        addRuleForm.onsubmit = async (e) => {
            e.preventDefault();
            const localPort = document.getElementById('localPort').value;
            const remoteAddr = document.getElementById('remoteAddr').value;
            const remotePort = document.getElementById('remotePort').value;
            const note = document.getElementById('note').value;
            const quotaGB = parseFloat(document.getElementById('quotaGB').value) || 0;

            const res = await apiRequest('/api/rules', {
                method: 'POST',
                body: JSON.stringify({ local_port: localPort, remote_addr: remoteAddr, remote_port: remotePort, note, quota_gb: quotaGB, enabled: true })
            });

            if (res && res.success) {
                showToast(t('toast.addOk'), 'success');
                addRuleForm.reset();
                loadRules();
            } else if (res && res.error) {
                showToast(res.error, 'error');
            }
        };
    }

    loadRules();

    // TG 配置绑定与保存
    const tgConfigForm = document.getElementById('tgConfigForm');
    const testTgBtn = document.getElementById('testTgBtn');

    async function loadTgConfig() {
        const cfg = await apiRequest('/api/tg/config');
        if (!cfg) return;
        if (document.getElementById('tgBotToken')) document.getElementById('tgBotToken').value = cfg.bot_token || '';
        if (document.getElementById('tgChatID')) document.getElementById('tgChatID').value = cfg.chat_id || '';
        if (document.getElementById('tgReportTime')) document.getElementById('tgReportTime').value = cfg.report_time || '08:00';
        if (document.getElementById('tgDailyReport')) document.getElementById('tgDailyReport').checked = cfg.daily_report !== false;
        if (document.getElementById('tgAlertQuota')) document.getElementById('tgAlertQuota').checked = cfg.alert_quota !== false;
        if (document.getElementById('tgAlertGFW')) document.getElementById('tgAlertGFW').checked = cfg.alert_gfw !== false;
    }
    loadTgConfig();

    if (tgConfigForm) {
        tgConfigForm.onsubmit = async (e) => {
            e.preventDefault();
            const bot_token = document.getElementById('tgBotToken').value;
            const chat_id = document.getElementById('tgChatID').value;
            const report_time = document.getElementById('tgReportTime').value || '08:00';
            const daily_report = document.getElementById('tgDailyReport').checked;
            const alert_quota = document.getElementById('tgAlertQuota').checked;
            const alert_gfw = document.getElementById('tgAlertGFW').checked;

            const res = await apiRequest('/api/tg/config', {
                method: 'POST',
                body: JSON.stringify({ enabled: true, bot_token, chat_id, report_time, daily_report, alert_quota, alert_gfw })
            });

            if (res && res.message) {
                showToast(t('toast.tgSaved'), 'success');
            }
        };
    }
    if (testTgBtn) {
        testTgBtn.onclick = async () => {
            const res = await apiRequest('/api/tg/test', { method: 'POST' });
            if (res && res.message) {
                showToast(t('toast.tgTestSent'), 'info');
            }
        };
    }

    // Realm 规则表单保存
    const addRealmRuleForm = document.getElementById('addRealmRuleForm');
    if (addRealmRuleForm) {
        addRealmRuleForm.onsubmit = (e) => {
            e.preventDefault();
            showToast(t('toast.realmSaved'), 'success');
        };
    }

    // 修改密码表单
    const changePasswordForm = document.getElementById('changePasswordForm');
    if (changePasswordForm) {
        changePasswordForm.onsubmit = async (e) => {
            e.preventDefault();
            const oldPassword = document.getElementById('oldPassword').value;
            const newPassword = document.getElementById('newPassword').value;

            const res = await apiRequest('/api/change-password', {
                method: 'POST',
                body: JSON.stringify({ old_password: oldPassword, new_password: newPassword })
            });

            if (res && res.success) {
                showToast('密码修改成功', 'success');
                changePasswordForm.reset();
            } else if (res && res.error) {
                showToast(res.error, 'error');
            }
        };
    }
});
