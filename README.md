# nftables 一键端口转发脚本 & 可视化管理面板

基于 **nftables** 的一键端口转发解决方案，支持 IPv4/IPv6 双栈，适用于 Debian 12+ 等使用 nftables 的 Linux 发行版。

## ✨ 功能特点

- 🚀 **内核级转发** — 使用 nftables NAT，零额外进程，性能极佳
- 🌐 **IPv4/IPv6 双栈** — 使用 `inet` 表同时支持 IPv4 和 IPv6 转发
- 🌍 **域名转发** — 支持以域名作为落地地址，面板自动解析为 IP 写入内核；后台每 60 秒动态刷新，DDNS / CDN IP 变化时自动热重载，DNS 故障时沿用缓存 IP 兜底
- 🖥️ **可视化面板** — 现代浅色主题 Web 管理界面，单二进制部署（go:embed）
- 📦 **批量操作** — 支持批量导入转发规则
- 📊 **流量监控** — 基于 nftables counter 的实时流量统计，支持配额限制与账期自动重置
- 🌏 **中英双语** — 面板 UI 一键切换中文 / English，语言偏好自动记忆
- 💬 **内核级注释** — DNAT 规则写入 `comment` 元数据，`nft list ruleset` 直接可见规则用途
- 🔒 **安全认证** — bcrypt 密码哈希 + 随机 Session Secret + 登录限流 + HTTPS 支持
- 🛡️ **安全隔离** — 仅操作 `inet nft_forward` 表，不影响其他防火墙规则
- 🧹 **安全卸载** — state 标记文件区分项目配置与系统原有配置，卸载绝不误改 Docker / WireGuard 等服务
- ✨ **GFW 封锁检测** — 混合模式自动检测服务器 IP 是否被墙，被墙时全局红色警告 + 规则状态显示「入口被墙」
- 🛡️ **Realm 转发** — 面板内置 Realm 规则管理，支持 TCP/UDP/TCP+UDP 协议选择，配置自动生成并重启服务
- 📨 **Telegram 推送** — 支持自定义时间的每日流量报告、配额告警、GFW 封锁告警，通过 Bot 实时推送到 Telegram
- 🔗 **多节点主控** — 主控端可监控汇总多个被控端节点的流量与转发规则

## 📋 流量走向

```
客户端 --> A服务器(中转) --> B/C 服务器(落地) --> 目标网站 --> 返回客户端
```

## 🔧 一键安装

```bash
curl -L https://raw.githubusercontent.com/wsuming97/nftnode/main/nft-forward.sh -o nft-forward.sh && chmod +x nft-forward.sh && ./nft-forward.sh
```

## 📖 使用说明

### 脚本界面

```
################################################
#    nftables & Realm 端口转发脚本 (v1.0.0)  #
################################################
 nftables 状态: 运行中
 Realm 状态:    未安装
 面板 状态:     未安装
------------------------------------------------
  1. 安装 / 重置 nftables 转发
  2. 安装 / 重置 Realm 转发
  3. 卸载转发配置
------------------------------------------------
  4. 安装 Caddy HTTPS 代理
  5. 安装 Xray Reality
  6. 安装 Shadowsocks Rust
  7. 查看当前转发配置
  8. 查看已部署节点
------------------------------------------------
  9. 启动 nftables
  10. 停止 nftables
  11. 重启 nftables
------------------------------------------------
  12. 更新脚本
  13. 面板管理
  0. 退出脚本
################################################
```

### 转发规则示例

| 中转端口 | 目标地址 | 目标端口 | 说明 |
|---|---|---|---|
| 2222 | 6.6.6.6 | 6666 | IPv4 转发 |
| 3333 | [2001:db8::1] | 7777 | IPv6 转发 |
| 4444 | example.com | 8888 | 域名转发（自动解析，支持 DDNS）|

> 💡 **关于目标域名的动态 DNS 解析机制**：
> - **优先 IPv4**：目标地址若配置为域名，系统解析时会优先选择其 IPv4 地址，当且仅当无 A 记录时才回退至 IPv6。
> - **排序去抖**：针对 CDN 等具有多条 A 记录的域名，解析结果会进行排序并锁定首个 IP，避免因 IP 轮询导致后台每分钟频繁重载 `nftables`。
> - **超时与兜底**：DNS 解析带有 3 秒超时控制。若因 DNS 临时故障解析失败，系统会自动沿用上一次成功解析的缓存 IP（已持久化至 `rules.json`），绝不影响整表其他转发规则的加载。

### Web 面板

面板默认运行在 `http://服务器IP:3456`，右上角可一键切换 **中文 / English**。

配置文件路径：`/root/nft-forward/web/config.toml`

```toml
[auth]
# 首次运行后，明文 password 会自动加密为 bcrypt 哈希并清空 password 字段
password = "admin123"
password_hash = ""

[server]
port = 3456

[https]
enabled = false
cert_file = "./certificate/cert.pem"
key_file = "./certificate/private.key"

[session]
# 首次运行会自动生成 64 位随机安全密钥
secret = ""
```

### 多节点主控

在 `config.toml` 中配置被控节点：

```toml
[[nodes]]
name = "东京中转"
url = "https://1.2.3.4:3456"
token = "被控端的 Metrics Token"
```

主控面板将自动轮询各节点的转发规则与流量数据，统一展示在「节点总览」大盘中。

在「节点管理」中还可远程修改被控端的 Xray Reality / Shadowsocks 配置（SNI、端口、UUID、密码、加密方式等），主控端会代理转发到对应被控端，被控端校验参数、备份原配置、重启服务并在启动失败时自动回滚。

> **传输安全**：主控与被控之间的调用（流量监控、远程改配置）复用被控端的 Metrics Token 鉴权，且**不接受自签名证书**。请确保被控端 `url` 使用有效证书的 HTTPS，或主控与被控处于可信内网直连环境。远程改配置属于写操作，切勿在不受信任的网络上以明文暴露该 Token。

## 🔐 安全建议

1. **修改默认密码** — 安装后立即修改面板密码
2. **启用 HTTPS** — 配置 SSL 证书后开启 HTTPS
3. **限制访问** — 建议通过防火墙限制面板端口的访问来源

## 🛡️ GFW 封锁自动检测

面板内置 GFW 封锁检测机制，当服务器 IP 被墙时自动告警。

### 检测原理

GFW 对 IP 的封锁是**双向的** — 被封锁 IP 不仅国内无法访问，从该 IP 向国内发包也会被边境路由器丢弃。因此服务器自身即可检测：

- 并发 TCP 连接国内公共 DNS（阿里 `223.5.5.5` / 腾讯 `119.29.29.29` / 百度 `180.76.76.76`）
- 同时测试国际 DNS（Google `8.8.8.8` / Cloudflare `1.1.1.1`）
- **国际至少一个通 + 国内全部超时 → 判定被墙**

### 混合触发策略

| 触发方式 | 频率 | 说明 |
|---------|------|------|
| 定时探测 | 每 5 分钟 | 兆底保障，无流量场景也能检测 |
| 流量异常触发 | 实时 | 上轮 ≥3 条规则有流量 → 本轮全部归零 + 目标仍可达 → 立即探测 |

### UI 表现

- **被墙时**：页面顶部出现红色脉冲警告横幅，所有非封停规则状态列显示「入口被墙」
- **恢复后**：横幅自动消失，状态列恢复正常显示
- 封停规则仍显示「已封停」以保留配额信息

## 📁 文件结构

```
nftables-forward/
├── nft-forward.sh              # 一键管理脚本（含安全卸载逻辑）
├── reality.sh                  # Xray Reality 一键安装
├── ss-rust.sh                  # Shadowsocks Rust 一键安装
├── https.sh                    # Caddy HTTPS 代理安装
├── diagnose.sh                 # 7 步转发诊断工具
├── README.md                   # 项目说明
├── LICENSE                     # MIT 许可证
└── web/                        # Web 可视化面板（仅含源码，二进制通过 Releases 分发）
    ├── config.toml.example     # 面板配置模板（首次运行自动生成 config.toml）
    ├── go.mod                  # Go 依赖
    ├── go.sum                  # Go 依赖锁定
    ├── main.go                 # Go 后端（Gin + 规则管理 + 流量统计 + GFW 检测 + 节点探针 + 域名解析）
    ├── templates/
    │   ├── index.html          # 管理页面（含 i18n 标记）
    │   └── login.html          # 登录页面（含 i18n 标记）
    └── static/
        └── app.js              # 前端逻辑（含 i18n 翻译系统）

> 💡 编译好的 Linux 二进制文件（`nft-panel-linux-amd64.tar.gz` / `nft-panel-linux-arm64.tar.gz`）通过 [GitHub Releases](https://github.com/wsuming97/nftnode/releases) 分发，不提交到代码仓库。
```

## 📝 生成的 nftables 配置

脚本会自动生成 `/etc/nftables.conf`（仅管理 `nft_forward` 表，不影响其他规则）：

```nft
#!/usr/sbin/nft -f

# 仅操作本项目创建的表
table inet nft_forward
delete table inet nft_forward

table inet nft_forward {
    chain prerouting {
        type nat hook prerouting priority -100; policy accept;
        # Rule abc12345 (东京节点)
        tcp dport 2222 dnat ip to 6.6.6.6:6666 comment "nat_2222_Tokyo"
        udp dport 2222 dnat ip to 6.6.6.6:6666 comment "nat_2222_Tokyo"
    }

    chain postrouting {
        type nat hook postrouting priority 100; policy accept;
        ct status dnat masquerade
    }
}
```

> 💡 `comment` 是 nftables 的内核级元数据，执行 `nft list ruleset` 时可直接看到每条规则的用途，即使面板未运行也能快速定位规则。

## 🧹 安全卸载机制

卸载时通过 state 标记文件（`$NFT_DIR/.state-*`）精确识别本项目修改过的配置：

| 标记文件 | 作用 |
|---|---|
| `.state-sysctl-ipv4` | 本项目修改了 sysctl.conf 中的 ip_forward |
| `.state-systemd-enabled` | 本项目执行了 systemctl enable nftables |
| `.state-iptables-dnat` | 本项目添加了 iptables DNAT 放行规则 |

只有存在对应标记文件时才还原对应配置，**绝不影响 Docker / WireGuard 等其他服务的网络设置**。

## ⚙️ 系统要求

- Linux (Debian 12+ / Ubuntu 22.04+)
- root 权限
- nftables (`apt install nftables`)

## 📜 许可证

MIT License
