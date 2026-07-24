// ============================================================
//  nftables 转发管理面板 - 前端逻辑（含流量监控 + 节点总览）
// ============================================================

document.addEventListener('DOMContentLoaded', () => {

    // ============================================================
    //  i18n 国际化系统 — 中英双语切换
    // ============================================================
    const I18N = {
        zh: {
            // Header
            'header.title': 'nftables 转发管理',
            'header.logout': '退出',
            // Service
            'svc.start': '启动', 'svc.stop': '停止', 'svc.restart': '重启',
            // Buttons
            'btn.refresh': '刷新', 'btn.cancel': '取消', 'btn.save': '保存',
            // Overview
            'overview.title': '节点总览',
            'overview.node': '节点名', 'overview.online': '在线', 'overview.rules': '规则数',
            'overview.used': '总已用', 'overview.quota': '总配额', 'overview.lastUpdate': '最后更新',
            // Nodes
            'nodes.title': '已部署节点',
            // Rules
            'rules.title': '转发规则列表',
            'rules.localPort': '本机端口', 'rules.ipType': 'IP类型',
            'rules.targetAddr': '目标地址', 'rules.targetPort': '目标端口',
            'rules.protocol': '协议', 'rules.traffic': '流量 / 配额',
            'rules.status': '状态', 'rules.note': '备注', 'rules.actions': '操作',
            // Pagination
            'page.prev': '上一页', 'page.next': '下一页',
            // Add rule
            'add.title': '添加转发规则',
            'add.localPort': '本机端口', 'add.targetPort': '目标端口',
            'add.note': '备注（可选）', 'add.quota': '月配额 GB（可选）',
            'add.resetDay': '重置日（可选）', 'add.submit': '添加规则',
            // Batch
            'batch.title': '批量添加转发规则', 'batch.submit': '批量添加',
            // Edit modal
            'edit.title': '编辑转发规则',
            // Password modal
            'pwd.title': '修改密码', 'pwd.confirm': '确认修改',
            // Status / dynamic
            'status.running': '运行中', 'status.stopped': '已停止', 'status.checking': '检测中...',
            'status.active': '正常', 'status.suspended': '已封停',
            'status.unreachable': '不通', 'status.checkingShort': '检测中',
            'status.exceeded': '已超额',
            // Toast messages
            'toast.addOk': '{proto} 规则已添加', 'toast.editOk': '规则已更新',
            'toast.deleteOk': '规则已删除', 'toast.resetOk': '流量已重置',
            'toast.batchOk': '成功添加 {n} 条规则', 'toast.batchPartial': '成功添加 {n} 条规则，{f} 条失败',
            'toast.startOk': '服务已启动', 'toast.stopOk': '服务已停止',
            'toast.restartOk': '服务已重启',
            'toast.pwdOk': '密码修改成功',
            'toast.pwdMismatch': '两次输入的新密码不一致',
            'toast.pwdShort': '新密码至少 4 个字符',
            'toast.pwdEmpty': '请填写所有密码字段',
            'toast.editEmpty': '本机端口、目标地址和端口不能为空',
            'toast.addEmpty': '请填写所有必填字段',
            'toast.batchEmpty': '请输入规则',
            'toast.batchFormat': '格式错误: {line}',
            'toast.batchNone': '未解析到有效规则',
            'toast.copied': '连接链接已复制',
            'toast.nodeAdded': '节点已添加',
            'toast.nodeDeleted': '节点已删除',
            'toast.nodeEmpty': '节点名称、地址和 Token 不能为空',
            'toast.confirmDelete': '确定删除此转发规则？',
            'toast.confirmReset': '确定清零该端口的已用流量并恢复转发？',
            'toast.confirmDeleteNode': '确定删除此监控节点？',
            // Table misc
            'rules.total': '共 {n} 条',
            'rules.empty': '暂无转发规则',
            'rules.fetchFail': '获取规则失败',
            'rules.edit': '编辑', 'rules.delete': '删除', 'rules.reset': '重置',
            'page.info': '第 {cur} / {total} 页',
            'traffic.unlimited': '不限', 'traffic.unlimitedQuota': '不限额',
            // Status text
            'status.unknown': '未知',
            // Nodes
            'nodes.empty': '暂无已部署节点（可通过脚本菜单安装 Xray Reality 或 Shadowsocks）',
            'nodes.fetchFail': '获取节点信息失败',
            'nodes.refreshing': '刷新中...',
            'nodes.copyHint': '点击复制',
            'overview.empty': '暂无监控节点',
            'overview.online': '在线', 'overview.offline': '离线',
            'overview.refreshing': '刷新中...',
            'pwd.changing': '修改中...',
            // IPv4/IPv6 切换
            'proto.v4Label': '目标 IPv4 地址', 'proto.v4Ph': '如: 6.6.6.6 或 1.2.3.4',
            'proto.v6Label': '目标 IPv6 地址', 'proto.v6Ph': '如: 2001:db8::1 (自动添加方括号)',
            // Node detail sub-table
            'detail.suspended': '封停', 'detail.active': '正常',
            // Node manage
            'nodeManage.title': '管理监控节点',
            'nodeManage.name': '节点名称', 'nodeManage.url': '节点地址',
            'nodeManage.token': 'Metrics Token', 'nodeManage.add': '添加节点',
            'nodeManage.empty': '未配置监控节点，在下方添加被控服务器',
            'nodeManage.fetchFail': '获取节点列表失败',
            // GFW 封锁检测
            'gfw.blocked': '⚠️ 检测到服务器 IP 可能被墙（无法连接国内网络），所有中转链路已中断',
            'gfw.statusBlocked': '入口被墙',
        },
        en: {
            'header.title': 'nftables Forward Manager',
            'header.logout': 'Logout',
            'svc.start': 'Start', 'svc.stop': 'Stop', 'svc.restart': 'Restart',
            'btn.refresh': 'Refresh', 'btn.cancel': 'Cancel', 'btn.save': 'Save',
            'overview.title': 'Node Overview',
            'overview.node': 'Node', 'overview.online': 'Online', 'overview.rules': 'Rules',
            'overview.used': 'Used', 'overview.quota': 'Quota', 'overview.lastUpdate': 'Last Update',
            'nodes.title': 'Deployed Nodes',
            'rules.title': 'Forward Rules',
            'rules.localPort': 'Local Port', 'rules.ipType': 'IP Type',
            'rules.targetAddr': 'Target Addr', 'rules.targetPort': 'Target Port',
            'rules.protocol': 'Protocol', 'rules.traffic': 'Traffic / Quota',
            'rules.status': 'Status', 'rules.note': 'Note', 'rules.actions': 'Actions',
            'page.prev': 'Prev', 'page.next': 'Next',
            'add.title': 'Add Forward Rule',
            'add.localPort': 'Local Port', 'add.targetPort': 'Target Port',
            'add.note': 'Note (optional)', 'add.quota': 'Monthly Quota GB (optional)',
            'add.resetDay': 'Reset Day (optional)', 'add.submit': 'Add Rule',
            'batch.title': 'Batch Add Rules', 'batch.submit': 'Batch Add',
            'edit.title': 'Edit Forward Rule',
            'pwd.title': 'Change Password', 'pwd.confirm': 'Confirm',
            'status.running': 'Running', 'status.stopped': 'Stopped', 'status.checking': 'Checking...',
            'status.active': 'Active', 'status.suspended': 'Suspended',
            'status.unreachable': 'Unreachable', 'status.checkingShort': 'Checking',
            'status.exceeded': 'Exceeded',
            'toast.addOk': '{proto} rule added', 'toast.editOk': 'Rule updated',
            'toast.deleteOk': 'Rule deleted', 'toast.resetOk': 'Traffic reset',
            'toast.batchOk': '{n} rules added', 'toast.batchPartial': '{n} added, {f} failed',
            'toast.startOk': 'Service started', 'toast.stopOk': 'Service stopped',
            'toast.restartOk': 'Service restarted',
            'toast.pwdOk': 'Password changed',
            'toast.pwdMismatch': 'New passwords do not match',
            'toast.pwdShort': 'New password must be at least 4 characters',
            'toast.pwdEmpty': 'Please fill in all password fields',
            'toast.editEmpty': 'Local port, target address and port are required',
            'toast.addEmpty': 'Please fill in all required fields',
            'toast.batchEmpty': 'Please enter rules',
            'toast.batchFormat': 'Format error: {line}',
            'toast.batchNone': 'No valid rules parsed',
            'toast.copied': 'Link copied',
            'toast.nodeAdded': 'Node added',
            'toast.nodeDeleted': 'Node deleted',
            'toast.nodeEmpty': 'Node name, URL and Token are required',
            'toast.confirmDelete': 'Delete this forward rule?',
            'toast.confirmReset': 'Reset traffic and resume forwarding?',
            'toast.confirmDeleteNode': 'Delete this monitor node?',
            'rules.total': '{n} rules',
            'rules.empty': 'No forward rules',
            'rules.fetchFail': 'Failed to load rules',
            'rules.edit': 'Edit', 'rules.delete': 'Delete', 'rules.reset': 'Reset',
            'page.info': 'Page {cur} / {total}',
            'traffic.unlimited': 'Unlimited', 'traffic.unlimitedQuota': 'Unlimited',
            'status.unknown': 'Unknown',
            'nodes.empty': 'No deployed nodes (install Xray Reality or Shadowsocks via script menu)',
            'nodes.fetchFail': 'Failed to load node info',
            'nodes.refreshing': 'Refreshing...',
            'nodes.copyHint': 'Click to copy',
            'overview.empty': 'No monitoring nodes',
            'overview.online': 'Online', 'overview.offline': 'Offline',
            'overview.refreshing': 'Refreshing...',
            'pwd.changing': 'Saving...',
            'nodeManage.title': 'Manage Monitor Nodes',
            'nodeManage.name': 'Node Name', 'nodeManage.url': 'Node URL',
            'nodeManage.token': 'Metrics Token', 'nodeManage.add': 'Add Node',
            'nodeManage.empty': 'No monitor nodes configured; add a managed server below',
            'nodeManage.fetchFail': 'Failed to load node list',
            'proto.v4Label': 'Target IPv4 Address', 'proto.v4Ph': 'e.g. 6.6.6.6 or 1.2.3.4',
            'proto.v6Label': 'Target IPv6 Address', 'proto.v6Ph': 'e.g. 2001:db8::1 (brackets added automatically)',
            'detail.suspended': 'Suspended', 'detail.active': 'Active',
            // GFW detection
            'gfw.blocked': '⚠️ Server IP may be blocked by GFW (cannot reach Chinese network), all relay chains broken',
            'gfw.statusBlocked': 'GFW Blocked',
        }
    };

    // 当前语言，从 localStorage 读取，默认中文
    let currentLang = localStorage.getItem('nft_lang') || 'zh';

    // 翻译函数：按 key 获取当前语言文字，支持 {var} 占位符
    function t(key, vars) {
        let text = (I18N[currentLang] && I18N[currentLang][key]) || (I18N.zh[key]) || key;
        if (vars) {
            Object.keys(vars).forEach(k => {
                text = text.replace('{' + k + '}', vars[k]);
            });
        }
        return text;
    }

    // 应用翻译到所有 data-i18n 元素
    function applyI18n() {
        document.querySelectorAll('[data-i18n]').forEach(el => {
            const key = el.getAttribute('data-i18n');
            const translated = t(key);
            if (translated !== key) {
                el.textContent = translated;
            }
        });
        // 更新语言切换按钮文字
        const langBtn = document.getElementById('langToggleBtn');
        if (langBtn) {
            langBtn.textContent = currentLang === 'zh' ? 'EN' : '中文';
        }
    }

    // 切换语言
    function toggleLang() {
        currentLang = currentLang === 'zh' ? 'en' : 'zh';
        localStorage.setItem('nft_lang', currentLang);
        applyI18n();
        // 刷新动态内容
        renderRules();
        updateStatus();
        ruleCount.textContent = t('rules.total', {n: totalRules});
        setProtocol(selectedProto);
        fetchNodes();
        fetchOverview();
        fetchNodeManage();
    }

    // --- DOM 引用 ---
    const rulesBody = document.getElementById('rulesBody');
    const ruleCount = document.getElementById('ruleCount');
    const statusBadge = document.getElementById('statusBadge');
    const statusText = document.getElementById('statusText');
    const pageInfo = document.getElementById('pageInfo');
    const pageSizeSelect = document.getElementById('pageSizeSelect');
    const toggleV4 = document.getElementById('toggleV4');
    const toggleV6 = document.getElementById('toggleV6');
    const ipv6Notice = document.getElementById('ipv6Notice');
    const addrLabel = document.getElementById('addrLabel');
    const remoteAddrInput = document.getElementById('remoteAddr');
    const editModal = document.getElementById('editModal');
    const passwordModal = document.getElementById('passwordModal');

    let currentPage = 1;
    let pageSize = 10;
    let totalRules = 0;
    let allRules = [];
    let selectedProto = 'ipv4'; // 当前选中的协议
    let gfwBlocked = false;     // GFW 封锁状态（从后端定时同步）

    // --- 工具函数 ---
    function formatBytes(bytes) {
        if (!bytes || bytes === 0) return '0 B';
        const units = ['B', 'KB', 'MB', 'GB', 'TB'];
        const k = 1024;
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return (bytes / Math.pow(k, i)).toFixed(2) + ' ' + units[i];
    }

    // --- IPv4/IPv6 协议切换 ---
    function setProtocol(proto) {
        selectedProto = proto;
        if (proto === 'ipv4') {
            toggleV4.className = 'toggle-btn active-v4';
            toggleV6.className = 'toggle-btn';
            ipv6Notice.classList.remove('show');
            addrLabel.textContent = t('proto.v4Label');
            remoteAddrInput.placeholder = t('proto.v4Ph');
        } else {
            toggleV4.className = 'toggle-btn';
            toggleV6.className = 'toggle-btn active-v6';
            ipv6Notice.classList.add('show');
            addrLabel.textContent = t('proto.v6Label');
            remoteAddrInput.placeholder = t('proto.v6Ph');
        }
    }
    toggleV4.addEventListener('click', () => setProtocol('ipv4'));
    toggleV6.addEventListener('click', () => setProtocol('ipv6'));

    // --- Toast 通知 ---
    function showToast(message, type = 'info') {
        const existing = document.querySelector('.toast');
        if (existing) existing.remove();

        const toast = document.createElement('div');
        toast.className = `toast ${type}`;
        toast.textContent = message;
        document.body.appendChild(toast);
        setTimeout(() => toast.remove(), 3500);
    }

    // --- 服务状态 ---
    async function updateStatus() {
        try {
            const res = await fetch('/api/service/status');
            if (!res.ok) throw new Error();
            const data = await res.json();

            if (data.status === '运行中') {
                statusBadge.className = 'status-badge running';
                statusText.textContent = t('status.running');
            } else {
                statusBadge.className = 'status-badge stopped';
                statusText.textContent = t('status.stopped');
            }

            // GFW 封锁检测：后端通过对比国内外 DNS 端点连通性判断是否被墙
            const wasBlocked = gfwBlocked;
            gfwBlocked = data.gfw_blocked === true;
            const gfwBanner = document.getElementById('gfwBanner');
            if (gfwBanner) {
                if (gfwBlocked) {
                    gfwBanner.hidden = false;
                    gfwBanner.textContent = t('gfw.blocked');
                } else {
                    gfwBanner.hidden = true;
                }
            }
            // GFW 状态变化时刷新规则列表以更新状态列显示
            if (wasBlocked !== gfwBlocked && allRules.length > 0) {
                renderRules();
            }
        } catch {
            statusBadge.className = 'status-badge stopped';
            statusText.textContent = t('status.unknown');
        }
    }

    // --- 规则列表 ---
    async function fetchRules() {
        try {
            const res = await fetch(`/api/rules?page=${currentPage}&size=${pageSize}`, {
                headers: { 'Cache-Control': 'no-cache' }
            });
            if (!res.ok) throw new Error('fetch failed');
            const data = await res.json();

            totalRules = data.total || 0;
            allRules = Array.isArray(data.rules) ? data.rules : [];
            // 同步 GFW 状态（规则接口也返回，避免首次加载时状态闪烁）
            if (data.gfw_blocked !== undefined) gfwBlocked = data.gfw_blocked === true;
            ruleCount.textContent = t('rules.total', {n: totalRules});
            renderRules();
        } catch (e) {
            rulesBody.innerHTML = `<tr class="empty-row"><td colspan="10">${t('rules.fetchFail')}</td></tr>`;
        }
    }

    function isIPv6(addr) {
        return addr && addr.includes(':') && !addr.match(/^\d+\.\d+\.\d+\.\d+$/);
    }

    function renderRules() {
        if (allRules.length === 0) {
            rulesBody.innerHTML = `<tr class="empty-row"><td colspan="10">${t('rules.empty')}</td></tr>`;
            updatePagination();
            return;
        }

        rulesBody.innerHTML = allRules.map((rule, idx) => {
            const globalIdx = (currentPage - 1) * pageSize + idx + 1;
            const addr = rule.remote_addr || '';
            const v6 = isIPv6(addr);
            const tag = v6
                ? '<span class="ip-tag v6">IPv6</span>'
                : '<span class="ip-tag v4">IPv4</span>';
            const note = rule.note || '-';
            const suspended = rule.enabled === false;
            const rowClass = suspended ? 'rule-suspended' : '';

            // 流量进度条
            let trafficHtml = '';
            const usedBytes = rule.used_bytes || 0;
            const quotaGB = rule.quota_gb || 0;
            const resetDay = rule.reset_day || 0;
            const resetInfo = resetDay > 0 ? (currentLang === 'zh' ? `每月${resetDay}号重置` : `Resets on day ${resetDay}`) : '';
            if (quotaGB > 0) {
                const quotaBytes = quotaGB * 1024 * 1024 * 1024;
                const pct = Math.min(100, (usedBytes / quotaBytes) * 100);
                const barColor = pct >= 100 ? 'red' : pct >= 80 ? 'yellow' : 'green';
                const textClass = pct >= 100 ? 'exceeded' : '';
                trafficHtml = `
                    <div class="traffic-cell">
                        <div class="traffic-text ${textClass}">${formatBytes(usedBytes)} / ${quotaGB} GB</div>
                        <div class="traffic-bar"><div class="traffic-bar-fill ${barColor}" style="width:${pct.toFixed(1)}%"></div></div>
                        ${resetInfo ? `<div style="font-size:11px;color:var(--text-muted);margin-top:2px">${resetInfo}</div>` : ''}
                    </div>`;
            } else {
                trafficHtml = `<div class="traffic-text">${formatBytes(usedBytes)} / ${t('traffic.unlimited')}</div>`;
            }

            // 状态标签（四态：入口被墙/已封停/不通/正常）
            let statusTag;
            if (gfwBlocked && !suspended) {
                // GFW 封锁时，非封停规则显示“入口被墙”（封停规则仍显示“已封停”以保留配额信息）
                statusTag = `<span class="status-tag gfw-blocked">${t('gfw.statusBlocked')}</span>`;
            } else if (suspended) {
                statusTag = `<span class="status-tag suspended">${t('status.suspended')}</span>`;
            } else if (rule.reachable === false) {
                statusTag = `<span class="status-tag unreachable">${t('status.unreachable')}</span>`;
            } else if (rule.reachable === true) {
                statusTag = `<span class="status-tag active">${t('status.active')}</span>`;
            } else {
                statusTag = `<span class="status-tag checking">${t('status.checkingShort')}</span>`;
            }

            // 操作按钮（增加重置按钮）
            let actionsHtml = `
                <button class="btn btn-outline btn-sm" data-action="edit" data-id="${rule.id}" data-localport="${rule.local_port}" data-addr="${addr}" data-port="${rule.remote_port}" data-note="${rule.note || ''}" data-quota="${quotaGB}" data-resetday="${resetDay}">${t('rules.edit')}</button>
                <button class="btn btn-danger btn-sm" data-action="delete" data-id="${rule.id}">${t('rules.delete')}</button>`;
            if (quotaGB > 0) {
                actionsHtml += `<button class="btn btn-warning btn-sm" data-action="reset" data-id="${rule.id}">${t('rules.reset')}</button>`;
            }

            return `<tr class="${rowClass}">
                <td>${globalIdx}</td>
                <td><strong>${rule.local_port}</strong></td>
                <td>${tag}</td>
                <td>${addr}</td>
                <td>${rule.remote_port}</td>
                <td>TCP + UDP</td>
                <td>${trafficHtml}</td>
                <td>${statusTag}</td>
                <td style="color:var(--text-secondary);font-size:13px;max-width:120px;overflow:hidden;text-overflow:ellipsis" title="${note}">${note}</td>
                <td>${actionsHtml}</td>
            </tr>`;
        }).join('');

        updatePagination();
    }

    function updatePagination() {
        const totalPages = Math.max(1, Math.ceil(totalRules / pageSize));
        pageInfo.textContent = t('page.info', {cur: currentPage, total: totalPages});
        document.getElementById('prevPage').disabled = currentPage <= 1;
        document.getElementById('nextPage').disabled = currentPage >= totalPages;
    }

    // --- 事件委托：规则表格操作按钮 ---
    rulesBody.addEventListener('click', async (e) => {
        const btn = e.target.closest('[data-action]');
        if (!btn) return;

        const action = btn.dataset.action;
        const id = btn.dataset.id;

        if (action === 'delete') {
            if (!confirm(t('toast.confirmDelete'))) return;
            try {
                const res = await fetch(`/api/rules/${id}`, { method: 'DELETE' });
                if (!res.ok) {
                    const d = await res.json();
                    throw new Error(d.error || 'Delete failed');
                }
                showToast(t('toast.deleteOk'), 'success');
                await fetchRules();
                await updateStatus();
            } catch (err) {
                showToast(err.message, 'error');
            }
        }

        if (action === 'edit') {
            // 打开编辑模态框，填充当前值
            document.getElementById('editRuleId').value = id;
            document.getElementById('editLocalPort').value = btn.dataset.localport || '';
            document.getElementById('editRemoteAddr').value = btn.dataset.addr || '';
            document.getElementById('editRemotePort').value = btn.dataset.port || '';
            document.getElementById('editRuleNote').value = btn.dataset.note || '';
            document.getElementById('editQuotaGB').value = btn.dataset.quota || '0';
            document.getElementById('editResetDay').value = btn.dataset.resetday || '0';
            editModal.classList.add('show');
        }

        if (action === 'reset') {
            if (!confirm(t('toast.confirmReset'))) return;
            try {
                const res = await fetch(`/api/rules/${id}/reset`, { method: 'POST' });
                if (!res.ok) {
                    const d = await res.json();
                    throw new Error(d.error || 'Reset failed');
                }
                showToast(t('toast.resetOk'), 'success');
                await fetchRules();
            } catch (err) {
                showToast(err.message, 'error');
            }
        }
    });

    // --- 编辑模态框：关闭 ---
    document.getElementById('editCancelBtn').addEventListener('click', () => {
        editModal.classList.remove('show');
    });
    editModal.addEventListener('click', (e) => {
        // 点击遮罩层关闭
        if (e.target === editModal) editModal.classList.remove('show');
    });

    // --- 编辑模态框：保存 ---
    document.getElementById('editSaveBtn').addEventListener('click', async () => {
        const id = document.getElementById('editRuleId').value;
        const localPort = document.getElementById('editLocalPort').value.trim();
        const remoteAddr = document.getElementById('editRemoteAddr').value.trim();
        const remotePort = document.getElementById('editRemotePort').value.trim();
        const note = document.getElementById('editRuleNote').value.trim();
        const quotaGB = parseFloat(document.getElementById('editQuotaGB').value) || 0;
        const resetDay = parseInt(document.getElementById('editResetDay').value) || 0;

        if (!localPort || !remoteAddr || !remotePort) {
            showToast(t('toast.editEmpty'), 'error');
            return;
        }

        try {
            const res = await fetch(`/api/rules/${id}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    local_port: localPort,
                    remote_addr: remoteAddr,
                    remote_port: remotePort,
                    note: note,
                    quota_gb: quotaGB,
                    reset_day: resetDay
                })
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Update failed');

            showToast(t('toast.editOk'), 'success');
            editModal.classList.remove('show');
            await fetchRules();
            await updateStatus();
        } catch (err) {
            showToast(err.message, 'error');
        }
    });

    // --- 添加单条规则 ---
    document.getElementById('addRuleBtn').addEventListener('click', async () => {
        const lp = document.getElementById('localPort').value.trim();
        let ra = document.getElementById('remoteAddr').value.trim();
        const rp = document.getElementById('remotePort').value.trim();
        const note = document.getElementById('ruleNote').value.trim();
        const quota = parseFloat(document.getElementById('ruleQuota').value) || 0;
        const resetDay = parseInt(document.getElementById('ruleResetDay').value) || 0;

        if (!lp || !ra || !rp) {
            showToast(t('toast.addEmpty'), 'error');
            return;
        }

        // IPv6 地址自动包裹方括号
        if (selectedProto === 'ipv6' && !ra.startsWith('[')) {
            ra = `[${ra}]`;
        }

        try {
            const res = await fetch('/api/rules', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    local_port: lp,
                    remote_addr: ra,
                    remote_port: rp,
                    note: note,
                    quota_gb: quota,
                    reset_day: resetDay
                })
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Add failed');

            showToast(t('toast.addOk', {proto: selectedProto.toUpperCase()}), 'success');
            document.getElementById('localPort').value = '';
            document.getElementById('remoteAddr').value = '';
            document.getElementById('remotePort').value = '';
            document.getElementById('ruleNote').value = '';
            document.getElementById('ruleQuota').value = '';
            document.getElementById('ruleResetDay').value = '';
            await fetchRules();
            await updateStatus();
        } catch (e) {
            showToast(e.message, 'error');
        }
    });

    // --- 批量添加 ---
    document.getElementById('batchAddBtn').addEventListener('click', async () => {
        const text = document.getElementById('batchRules').value.trim();
        if (!text) {
            showToast(t('toast.batchEmpty'), 'error');
            return;
        }

        const lines = text.split('\n').filter(Boolean);
        const parsedRules = [];

        for (const line of lines) {
            const trimmed = line.trim();
            if (!trimmed) continue;

            // 匹配: 端口:[IPv6]:端口 或 端口:IPv4:端口
            const ipv6Match = trimmed.match(/^(\d+):\[([^\]]+)\]:(\d+)$/);
            const ipv4Match = trimmed.match(/^(\d+):([^:\[\]]+):(\d+)$/);

            if (ipv6Match) {
                parsedRules.push({
                    local_port: ipv6Match[1],
                    remote_addr: `[${ipv6Match[2]}]`,
                    remote_port: ipv6Match[3]
                });
            } else if (ipv4Match) {
                parsedRules.push({
                    local_port: ipv4Match[1],
                    remote_addr: ipv4Match[2],
                    remote_port: ipv4Match[3]
                });
            } else {
                showToast(t('toast.batchFormat', {line: trimmed}), 'error');
                return;
            }
        }

        if (parsedRules.length === 0) {
            showToast(t('toast.batchNone'), 'error');
            return;
        }

        try {
            const res = await fetch('/api/rules/batch', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ rules: parsedRules })
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Batch add failed');

            const failedCount = (data.failed && data.failed.length) || 0;
            const msg = failedCount > 0
                ? t('toast.batchPartial', {n: data.added, f: failedCount})
                : t('toast.batchOk', {n: data.added});
            showToast(msg, data.added > 0 ? 'success' : 'error');
            document.getElementById('batchRules').value = '';
            await fetchRules();
            await updateStatus();
        } catch (e) {
            showToast(e.message, 'error');
        }
    });

    // --- 服务控制 ---
    async function serviceAction(action, okKey) {
        try {
            const res = await fetch(`/api/service/${action}`, { method: 'POST' });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || action + ' failed');
            showToast(data.message || t(okKey), 'success');
            await updateStatus();
        } catch (e) {
            showToast(e.message, 'error');
        }
    }

    document.getElementById('startBtn').addEventListener('click', () => serviceAction('start', 'toast.startOk'));
    document.getElementById('stopBtn').addEventListener('click', () => serviceAction('stop', 'toast.stopOk'));
    document.getElementById('restartBtn').addEventListener('click', () => serviceAction('restart', 'toast.restartOk'));

    // --- 登出 ---
    document.getElementById('logoutBtn').addEventListener('click', async () => {
        try {
            await fetch('/logout', { method: 'POST' });
            window.location.href = '/login';
        } catch {
            window.location.href = '/login';
        }
    });

    // --- 修改密码 ---
    const changePasswordBtn = document.getElementById('changePasswordBtn');
    const pwdCancelBtn = document.getElementById('pwdCancelBtn');
    const pwdSaveBtn = document.getElementById('pwdSaveBtn');

    // 打开修改密码弹窗
    changePasswordBtn.addEventListener('click', () => {
        document.getElementById('oldPassword').value = '';
        document.getElementById('newPassword').value = '';
        document.getElementById('confirmPassword').value = '';
        passwordModal.classList.add('show');
    });

    // 关闭修改密码弹窗
    pwdCancelBtn.addEventListener('click', () => {
        passwordModal.classList.remove('show');
    });
    passwordModal.addEventListener('click', (e) => {
        if (e.target === passwordModal) passwordModal.classList.remove('show');
    });

    // 提交修改密码
    pwdSaveBtn.addEventListener('click', async () => {
        const oldPwd = document.getElementById('oldPassword').value.trim();
        const newPwd = document.getElementById('newPassword').value.trim();
        const confirmPwd = document.getElementById('confirmPassword').value.trim();

        if (!oldPwd || !newPwd || !confirmPwd) {
            showToast(t('toast.pwdEmpty'), 'error');
            return;
        }
        if (newPwd !== confirmPwd) {
            showToast(t('toast.pwdMismatch'), 'error');
            return;
        }
        if (newPwd.length < 4) {
            showToast(t('toast.pwdShort'), 'error');
            return;
        }

        pwdSaveBtn.disabled = true;
        pwdSaveBtn.textContent = t('pwd.changing');

        try {
            const res = await fetch('/api/password', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    old_password: oldPwd,
                    new_password: newPwd
                })
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Change failed');

            showToast(t('toast.pwdOk'), 'success');
            passwordModal.classList.remove('show');
        } catch (err) {
            showToast(err.message, 'error');
        } finally {
            pwdSaveBtn.disabled = false;
            pwdSaveBtn.textContent = t('pwd.confirm');
        }
    });

    // --- 分页 ---
    document.getElementById('prevPage').addEventListener('click', () => {
        if (currentPage > 1) { currentPage--; fetchRules(); }
    });
    document.getElementById('nextPage').addEventListener('click', () => {
        const totalPages = Math.ceil(totalRules / pageSize);
        if (currentPage < totalPages) { currentPage++; fetchRules(); }
    });
    pageSizeSelect.addEventListener('change', () => {
        pageSize = parseInt(pageSizeSelect.value, 10);
        currentPage = 1;
        fetchRules();
    });

    // --- 节点查看（本地代理） ---
    const nodesContainer = document.getElementById('nodesContainer');

    async function fetchNodes() {
        try {
            const res = await fetch('/api/nodes');
            if (!res.ok) throw new Error('fail');
            const data = await res.json();
            renderNodes(data.nodes || []);
        } catch (e) {
            nodesContainer.innerHTML = `<div class="nodes-empty">${t('nodes.fetchFail')}</div>`;
        }
    }

    function renderNodes(nodes) {
        if (nodes.length === 0) {
            nodesContainer.innerHTML = `<div class="nodes-empty">${t('nodes.empty')}</div>`;
            return;
        }

        nodesContainer.innerHTML = nodes.map(node => {
            const statusClass = node.status === '运行中' ? 'running' : 'stopped';
            let infoRows = '';
            let icon = '';

            if (node.type === 'Xray Reality') {
                icon = '<svg class="icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path></svg>';
                infoRows = `
                    <span class="label">协议</span><span class="value">VLESS + Reality</span>
                    <span class="label">地址</span><span class="value">${node.address || '-'}</span>
                    <span class="label">端口</span><span class="value">${node.port || '-'}</span>
                    <span class="label">UUID</span><span class="value">${node.uuid || '-'}</span>
                    <span class="label">流控</span><span class="value">${node.flow || '-'}</span>
                    <span class="label">传输</span><span class="value">${node.network || '-'}</span>
                    <span class="label">安全</span><span class="value">${node.security || '-'}</span>
                    <span class="label">SNI</span><span class="value">${node.sni || '-'}</span>
                    <span class="label">公钥</span><span class="value">${node.public_key || '-'}</span>
                    <span class="label">Short ID</span><span class="value">${node.short_id || '-'}</span>
                `;
            } else if (node.type === 'Shadowsocks') {
                icon = '<svg class="icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"></circle><path d="M12 16v-4"></path><path d="M12 8h.01"></path></svg>';
                infoRows = `
                    <span class="label">协议</span><span class="value">Shadowsocks</span>
                    <span class="label">地址</span><span class="value">${node.address || '-'}</span>
                    <span class="label">端口</span><span class="value">${node.port || '-'}</span>
                    <span class="label">密码</span><span class="value">${node.password || '-'}</span>
                    <span class="label">加密方式</span><span class="value">${node.method || '-'}</span>
                `;
            }

            const linkHtml = node.link
                ? `<div class="node-link" data-link="${encodeURIComponent(node.link)}" title="${t('nodes.copyHint')}">
                       ${node.link}
                       <span class="copy-hint">${t('nodes.copyHint')}</span>
                   </div>`
                : '';

            return `<div class="node-card">
                <div class="node-header">
                    <div class="node-type">${icon} ${node.type}</div>
                    <span class="node-status ${statusClass}">${node.status}</span>
                </div>
                <div class="node-info">${infoRows}</div>
                ${linkHtml}
            </div>`;
        }).join('');

        // 点击复制链接
        nodesContainer.querySelectorAll('.node-link').forEach(el => {
            el.addEventListener('click', () => {
                const link = decodeURIComponent(el.dataset.link);
                navigator.clipboard.writeText(link).then(() => {
                    showToast(t('toast.copied'), 'success');
                }).catch(() => {
                    // fallback
                    const ta = document.createElement('textarea');
                    ta.value = link;
                    document.body.appendChild(ta);
                    ta.select();
                    document.execCommand('copy');
                    document.body.removeChild(ta);
                    showToast(t('toast.copied'), 'success');
                });
            });
        });
    }

    document.getElementById('refreshNodesBtn').addEventListener('click', () => {
        nodesContainer.innerHTML = `<div class="nodes-empty">${t('nodes.refreshing')}</div>`;
        fetchNodes();
    });

    // --- 节点总览（主控大盘） ---
    const overviewCard = document.getElementById('overviewCard');
    const overviewBody = document.getElementById('overviewBody');

    async function fetchOverview() {
        try {
            const res = await fetch('/api/nodes/overview');
            if (!res.ok) throw new Error('fetch failed');
            const data = await res.json();
            const nodes = data.nodes || [];
            if (nodes.length === 0) {
                // 没有配置远程被控节点，则隐藏总览板块
                overviewCard.hidden = true;
                return;
            }
            overviewCard.hidden = false;
            renderOverview(nodes);
        } catch (e) {
            // 如果接口报错，静默隐藏
            overviewCard.hidden = true;
        }
    }

    function renderOverview(nodes) {
        overviewBody.innerHTML = nodes.map((node, idx) => {
            const onlineClass = node.online ? 'on' : 'off';
            const onlineText = node.online ? t('overview.online') : t('overview.offline');
            const rulesCount = (node.rules || []).length;
            const totalUsed = (node.rules || []).reduce((s, r) => s + (r.used_bytes || 0), 0);
            const totalQuota = (node.rules || []).reduce((s, r) => s + (r.quota_gb || 0), 0);
            const quotaText = totalQuota > 0 ? `${totalQuota} GB` : t('traffic.unlimited');
            const lastSeen = node.last_seen ? new Date(node.last_seen).toLocaleTimeString() : '-';
            const rowStyle = node.online ? '' : 'style="color:var(--danger); opacity:0.7"';

            return `<tr data-node-idx="${idx}" ${rowStyle}>
                <td><strong>${node.name || node.hostname || node.url}</strong></td>
                <td><span class="online-dot ${onlineClass}"></span>${onlineText}</td>
                <td>${rulesCount}</td>
                <td>${formatBytes(totalUsed)}</td>
                <td>${quotaText}</td>
                <td>${lastSeen}</td>
            </tr>`;
        }).join('');

        // 点击展开节点明细
        overviewBody.querySelectorAll('tr[data-node-idx]').forEach(tr => {
            tr.addEventListener('click', () => {
                const idx = parseInt(tr.dataset.nodeIdx);
                const existing = tr.nextElementSibling;
                if (existing && existing.classList.contains('node-detail-row')) {
                    existing.remove();
                    return;
                }
                const node = nodes[idx];
                const rules = node.rules || [];
                if (rules.length === 0) return;

                const detailHtml = `<tr class="node-detail-row"><td colspan="6">
                    <div class="node-detail-content">
                        <table>
                            <tr><th>端口</th><th>目标</th><th>已用</th><th>配额</th><th>状态</th></tr>
                            ${rules.map(r => {
                                const statusText = r.enabled === false ? `<span class="status-tag suspended">${t('detail.suspended')}</span>` : `<span class="status-tag active">${t('detail.active')}</span>`;
                                const quota = r.quota_gb > 0 ? `${r.quota_gb} GB` : t('traffic.unlimited');
                                return `<tr>
                                    <td>${r.local_port}</td>
                                    <td>${r.remote_addr}:${r.remote_port}</td>
                                    <td>${formatBytes(r.used_bytes || 0)}</td>
                                    <td>${quota}</td>
                                    <td>${statusText}</td>
                                </tr>`;
                            }).join('')}
                        </table>
                    </div>
                </td></tr>`;
                tr.insertAdjacentHTML('afterend', detailHtml);
            });
        });
    }

    const refreshOverviewBtn = document.getElementById('refreshOverviewBtn');
    if (refreshOverviewBtn) {
        refreshOverviewBtn.addEventListener('click', () => {
            overviewBody.innerHTML = `<tr><td colspan="6" class="overview-empty">${t('overview.refreshing')}</td></tr>`;
            fetchOverview();
        });
    }

    // --- 节点管理 CRUD ---
    const nodeManageList = document.getElementById('nodeManageList');

    async function fetchNodeManage() {
        try {
            const res = await fetch('/api/nodes/manage');
            if (!res.ok) throw new Error('fail');
            const data = await res.json();
            renderNodeManage(data.nodes || []);
            // 有被控节点配置时，同步显示节点总览大盘
            if (data.nodes && data.nodes.length > 0) {
                overviewCard.hidden = false;
                fetchOverview();
            } else {
                overviewCard.hidden = true;
            }
        } catch (e) {
            nodeManageList.innerHTML = `<div style="color:var(--text-muted);font-size:13px">${t('nodeManage.fetchFail')}</div>`;
        }
    }

    function renderNodeManage(nodes) {
        if (nodes.length === 0) {
            nodeManageList.innerHTML = `<div style="color:var(--text-muted);font-size:13px">${t('nodeManage.empty')}</div>`;
            return;
        }
        nodeManageList.innerHTML = nodes.map((n, idx) => {
            return `<div style="display:flex;align-items:center;gap:10px;padding:6px 0;border-bottom:1px solid var(--border)">
                <strong style="flex:1;font-size:13px">${n.name}</strong>
                <span style="flex:2;font-size:12px;color:var(--text-secondary);font-family:monospace">${n.url}</span>
                <span style="flex:1;font-size:12px;color:var(--text-muted);font-family:monospace;overflow:hidden;text-overflow:ellipsis" title="${n.token}">${n.token.substring(0,12)}...</span>
                <button class="btn btn-danger btn-sm" data-del-node="${idx}">${t('rules.delete')}</button>
            </div>`;
        }).join('');

        nodeManageList.querySelectorAll('[data-del-node]').forEach(btn => {
            btn.addEventListener('click', async () => {
                const idx = btn.dataset.delNode;
                if (!confirm(t('toast.confirmDeleteNode'))) return;
                try {
                    const res = await fetch(`/api/nodes/manage/${idx}`, { method: 'DELETE' });
                    if (!res.ok) {
                        const d = await res.json();
                        throw new Error(d.error || 'Delete failed');
                    }
                    showToast(t('toast.nodeDeleted'), 'success');
                    fetchNodeManage();
                    fetchOverview();
                } catch (err) {
                    showToast(err.message, 'error');
                }
            });
        });
    }

    document.getElementById('addNodeBtn').addEventListener('click', async () => {
        const name = document.getElementById('nodeManageName').value.trim();
        const url = document.getElementById('nodeManageURL').value.trim();
        const token = document.getElementById('nodeManageToken').value.trim();
        if (!name || !url || !token) {
            showToast(t('toast.nodeEmpty'), 'error');
            return;
        }
        try {
            const res = await fetch('/api/nodes/manage', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name, url, token })
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Add failed');
            showToast(t('toast.nodeAdded'), 'success');
            document.getElementById('nodeManageName').value = '';
            document.getElementById('nodeManageURL').value = '';
            document.getElementById('nodeManageToken').value = '';
            fetchNodeManage();
            // 60秒后自动拉取总览
            setTimeout(fetchOverview, 2000);
        } catch (e) {
            showToast(e.message, 'error');
        }
    });

    // --- 初始化 ---
    // i18n: 绑定语言切换按钮并应用当前语言
    const langToggleBtn = document.getElementById('langToggleBtn');
    if (langToggleBtn) langToggleBtn.addEventListener('click', toggleLang);
    applyI18n();

    fetchRules();
    updateStatus();
    fetchNodes();
    fetchOverview();
    fetchNodeManage();

    // 定时刷新状态与流量
    setInterval(updateStatus, 15000);
    setInterval(fetchRules, 60000);    // 每60秒刷新规则（含流量数据）
    setInterval(fetchOverview, 60000); // 每60秒刷新节点总览

    // ============================================================
    //  侧边栏导航 — 视图切换
    // ============================================================
    const navItems = document.querySelectorAll('.sidebar-nav .nav-item[data-view]');
    const viewPanels = document.querySelectorAll('.view-panel');
    const pageTitleEl = document.getElementById('pageTitle');

    // 视图标题映射
    const viewTitles = {
        'nftables': 'nftables 转发管理',
        'realm': 'Realm 转发管理',
        'nodes': '节点管理',
        'telegram': 'Telegram 推送配置'
    };

    navItems.forEach(item => {
        item.addEventListener('click', () => {
            const viewId = item.dataset.view;
            // 更新导航激活状态
            navItems.forEach(n => n.classList.remove('active'));
            item.classList.add('active');
            // 切换视图面板
            viewPanels.forEach(p => p.classList.remove('active'));
            const target = document.getElementById('view-' + viewId);
            if (target) target.classList.add('active');
            // 更新页面标题
            if (pageTitleEl) pageTitleEl.textContent = viewTitles[viewId] || viewId;
            // 切换到 Realm 时自动加载数据
            if (viewId === 'realm') fetchRealmRules();
            // 切换到 Telegram 时自动加载配置
            if (viewId === 'telegram') loadTgConfig();
            // 切换到节点管理时加载节点数据
            if (viewId === 'nodes') {
                fetchNodes();
                fetchOverview();
                fetchNodeManage();
            }
        });
    });

    // ============================================================
    //  Realm 规则 CRUD — 调用真实后端 API
    // ============================================================
    const realmRulesBody = document.getElementById('realmRulesBody');

    async function fetchRealmRules() {
        try {
            const res = await fetch('/api/realm/rules');
            if (!res.ok) throw new Error('fetch failed');
            const data = await res.json();
            renderRealmRules(data.rules || []);
        } catch (e) {
            if (realmRulesBody) {
                realmRulesBody.innerHTML = '<tr class="empty-row"><td colspan="7">获取 Realm 规则失败</td></tr>';
            }
        }
    }

    function renderRealmRules(rules) {
        if (!realmRulesBody) return;
        if (rules.length === 0) {
            realmRulesBody.innerHTML = '<tr class="empty-row"><td colspan="7">暂无 Realm 规则</td></tr>';
            return;
        }
        realmRulesBody.innerHTML = rules.map((r, idx) => {
            return `<tr>
                <td>${idx + 1}</td>
                <td><strong>${r.listen_port}</strong></td>
                <td>${r.network || 'tcp+udp'}</td>
                <td>${r.remote_addr}</td>
                <td>${r.remote_port}</td>
                <td style="color:var(--text-secondary);font-size:13px">${r.note || '-'}</td>
                <td><button class="btn btn-danger btn-sm" data-realm-delete="${r.id}">删除</button></td>
            </tr>`;
        }).join('');

        // 绑定删除事件
        realmRulesBody.querySelectorAll('[data-realm-delete]').forEach(btn => {
            btn.addEventListener('click', async () => {
                const id = btn.dataset.realmDelete;
                if (!confirm('确定删除此 Realm 规则？')) return;
                try {
                    const res = await fetch(`/api/realm/rules/${id}`, { method: 'DELETE' });
                    if (!res.ok) {
                        const d = await res.json();
                        throw new Error(d.error || 'Delete failed');
                    }
                    showToast('Realm 规则已删除', 'success');
                    fetchRealmRules();
                } catch (err) {
                    showToast(err.message, 'error');
                }
            });
        });
    }

    // 添加 Realm 规则
    const addRealmRuleBtn = document.getElementById('addRealmRuleBtn');
    if (addRealmRuleBtn) {
        addRealmRuleBtn.addEventListener('click', async () => {
            const listenPort = document.getElementById('realmListenPort').value.trim();
            const network = document.getElementById('realmNetwork').value;
            const remoteAddr = document.getElementById('realmRemoteAddr').value.trim();
            const remotePort = document.getElementById('realmRemotePort').value.trim();
            const note = document.getElementById('realmNote').value.trim();

            if (!listenPort || !remoteAddr || !remotePort) {
                showToast('请填写所有必填字段', 'error');
                return;
            }

            try {
                const res = await fetch('/api/realm/rules', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        listen_port: listenPort,
                        network: network,
                        remote_addr: remoteAddr,
                        remote_port: remotePort,
                        note: note
                    })
                });
                const data = await res.json();
                if (!res.ok) throw new Error(data.error || 'Add failed');

                showToast('Realm 规则已添加', 'success');
                document.getElementById('realmListenPort').value = '';
                document.getElementById('realmRemoteAddr').value = '';
                document.getElementById('realmRemotePort').value = '';
                document.getElementById('realmNote').value = '';
                fetchRealmRules();
            } catch (e) {
                showToast(e.message, 'error');
            }
        });
    }

    // 刷新 Realm 规则按钮
    const refreshRealmBtn = document.getElementById('refreshRealmBtn');
    if (refreshRealmBtn) {
        refreshRealmBtn.addEventListener('click', () => {
            if (realmRulesBody) realmRulesBody.innerHTML = '<tr class="empty-row"><td colspan="7">加载中...</td></tr>';
            fetchRealmRules();
        });
    }

    // ============================================================
    //  Telegram 推送配置 — 调用真实后端 API
    // ============================================================

    async function loadTgConfig() {
        try {
            const res = await fetch('/api/tg/config');
            if (!res.ok) throw new Error('fetch failed');
            const cfg = await res.json();
            // 填充表单
            const el = (id) => document.getElementById(id);
            if (el('tgBotToken'))    el('tgBotToken').value = cfg.bot_token || '';
            if (el('tgChatID'))      el('tgChatID').value = cfg.chat_id || '';
            if (el('tgReportTime'))  el('tgReportTime').value = cfg.report_time || '08:00';
            if (el('tgEnabled'))     el('tgEnabled').checked = cfg.enabled || false;
            if (el('tgDailyReport')) el('tgDailyReport').checked = cfg.daily_report || false;
            if (el('tgAlertQuota'))  el('tgAlertQuota').checked = cfg.alert_quota || false;
            if (el('tgAlertGFW'))    el('tgAlertGFW').checked = cfg.alert_gfw || false;
        } catch (e) {
            // 静默处理，TG 配置可能不存在
        }
    }

    // 保存 TG 配置
    const saveTgConfigBtn = document.getElementById('saveTgConfigBtn');
    if (saveTgConfigBtn) {
        saveTgConfigBtn.addEventListener('click', async () => {
            const el = (id) => document.getElementById(id);
            const cfg = {
                enabled: el('tgEnabled') ? el('tgEnabled').checked : false,
                bot_token: el('tgBotToken') ? el('tgBotToken').value.trim() : '',
                chat_id: el('tgChatID') ? el('tgChatID').value.trim() : '',
                daily_report: el('tgDailyReport') ? el('tgDailyReport').checked : false,
                report_time: el('tgReportTime') ? el('tgReportTime').value : '08:00',
                alert_quota: el('tgAlertQuota') ? el('tgAlertQuota').checked : false,
                alert_gfw: el('tgAlertGFW') ? el('tgAlertGFW').checked : false
            };
            try {
                const res = await fetch('/api/tg/config', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(cfg)
                });
                const data = await res.json();
                if (!res.ok) throw new Error(data.error || 'Save failed');
                showToast('TG 配置已保存', 'success');
            } catch (e) {
                showToast(e.message, 'error');
            }
        });
    }

    // 测试 TG 发送
    const testTgBtn = document.getElementById('testTgBtn');
    if (testTgBtn) {
        testTgBtn.addEventListener('click', async () => {
            try {
                const res = await fetch('/api/tg/test', { method: 'POST' });
                const data = await res.json();
                if (!res.ok) throw new Error(data.error || 'Test failed');
                showToast('测试消息已发送', 'success');
            } catch (e) {
                showToast(e.message, 'error');
            }
        });
    }
});
