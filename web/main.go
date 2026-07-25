package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// --- 内嵌前端文件 ---

//go:embed templates/*
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// --- 数据结构 ---

type ForwardRule struct {
	ID             string  `json:"id"`
	LocalPort      string  `json:"local_port"`
	RemoteAddr     string  `json:"remote_addr"`
	RemotePort     string  `json:"remote_port"`
	Note           string  `json:"note"`
	UsedBytes      uint64  `json:"used_bytes"`
	QuotaGB        float64 `json:"quota_gb"`
	Enabled        bool    `json:"enabled"`
	ResetDay       int     `json:"reset_day"`       // 每月几号重置 (1-31)，0 不自动重置
	LastResetTime  string  `json:"last_reset_time"` // 上次重置的月份 "2026-04"
	Reachable      *bool   `json:"reachable,omitempty"` // 运行时：落地机是否可达（不持久化）
	LastResolvedIP string  `json:"last_resolved_ip,omitempty"` // 运行时：上次解析的 IP 缓存
}

type NodeConf struct {
	Name  string `toml:"name" json:"name"`
	URL   string `toml:"url" json:"url"`
	Token string `toml:"token" json:"token"`
}

type RealmRule struct {
	ID         string `json:"id"`
	ListenPort string `json:"listen_port"`
	Network    string `json:"network"` // "tcp+udp", "tcp", "udp"
	RemoteAddr string `json:"remote_addr"`
	RemotePort string `json:"remote_port"`
	Note       string `json:"note"`
	Enabled    bool   `json:"enabled"`
}

type TelegramConfig struct {
	Enabled     bool   `toml:"enabled" json:"enabled"`
	BotToken    string `toml:"bot_token" json:"bot_token"`
	ChatID      string `toml:"chat_id" json:"chat_id"`
	DailyReport bool   `toml:"daily_report" json:"daily_report"`
	ReportTime  string `toml:"report_time" json:"report_time"` // 自定义推送时间，如 "08:00"
	AlertQuota  bool   `toml:"alert_quota" json:"alert_quota"`
	AlertGFW    bool   `toml:"alert_gfw" json:"alert_gfw"`
}

type PanelConfig struct {
	Auth struct {
		Password     string `toml:"password"`
		PasswordHash string `toml:"password_hash"`
	} `toml:"auth"`
	Server struct {
		Port int `toml:"port"`
	} `toml:"server"`
	HTTPS struct {
		Enabled  bool   `toml:"enabled"`
		CertFile string `toml:"cert_file"`
		KeyFile  string `toml:"key_file"`
	} `toml:"https"`
	Nftables struct {
		ConfigPath string `toml:"config_path"`
		RulesPath  string `toml:"rules_path"`
	} `toml:"nftables"`
	Realm struct {
		ConfigPath string `toml:"config_path"`
		RulesPath  string `toml:"rules_path"`
	} `toml:"realm"`
	Session struct {
		Secret string `toml:"secret"`
	} `toml:"session"`
	Metrics struct {
		Token string `toml:"token"`
	} `toml:"metrics"`
	Telegram TelegramConfig `toml:"telegram"`
	Nodes    []NodeConf     `toml:"nodes"`
}

// 主控端实体
type NodeSnapshot struct {
	Name     string        `json:"name"`
	URL      string        `json:"url"`
	Online   bool          `json:"online"`
	LastSeen time.Time     `json:"last_seen"`
	Hostname string        `json:"hostname"`
	Rules    []ForwardRule `json:"rules"`
}

// --- 全局变量 ---

var (
	mu          sync.Mutex
	rules       []ForwardRule
	realmRules  []RealmRule
	panelConfig PanelConfig
	configPath  = "./config.toml"

	// tgMu 保护 panelConfig.Telegram：告警由轮询协程触发，配置由 HTTP handler 写入
	tgMu sync.RWMutex

	nodesMu    sync.RWMutex
	nodesCache []NodeSnapshot

	// 流量统计：记录上次 nft counter 读数，用于计算增量
	lastCounterSnap = make(map[string]uint64)

	// 登录限流：防暴力破解
	loginMu        sync.Mutex
	loginAttempts  = make(map[string]int)
	loginLockUntil = make(map[string]time.Time)
	maxAttempts    = 5
	lockDuration   = 15 * time.Minute

	// GFW 封锁检测：混合模式（定时探测 + 流量异常触发）
	gfwMu               sync.RWMutex
	gfwBlocked           bool
	gfwTickCount         int // 距上次 GFW 探测的轮询次数
	gfwCheckInterval     = 5 // 默认每 5 轮（5 分钟）探测一次
	gfwPrevActiveRules   int // 上一轮有流量增量的规则数（用于异常检测）
)

// --- 输入校验 ---

// 合法地址正则：IPv4、方括号包裹的 IPv6、域名
var (
	reIPv4   = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
	reDomain = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
)

func validatePort(s string) bool {
	p, err := strconv.Atoi(s)
	return err == nil && p >= 1 && p <= 65535
}

func validateAddress(addr string) bool {
	if addr == "" {
		return false
	}
	// 方括号包裹的 IPv6
	if strings.HasPrefix(addr, "[") && strings.HasSuffix(addr, "]") {
		inner := addr[1 : len(addr)-1]
		return net.ParseIP(inner) != nil
	}
	// 纯 IPv4
	if reIPv4.MatchString(addr) {
		return net.ParseIP(addr) != nil
	}
	// 域名
	if reDomain.MatchString(addr) && len(addr) <= 253 {
		return true
	}
	return false
}

// 防止 nftables 配置注入：只允许安全字符
func sanitizeForNft(s string) string {
	safe := regexp.MustCompile(`[^a-zA-Z0-9\.\:\[\]\-\_]`)
	return safe.ReplaceAllString(s, "")
}

// tomlEscape 转义 TOML 字符串值中的特殊字符（双引号、反斜杠、换行），
// 防止节点名/URL/token 等用户输入包含 " 时写坏配置文件
func tomlEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

// resolveDomainIP 把域名解析成单个稳定 IP，供内核 nftables DNAT 使用。
//   - 带 3 秒超时，避免 DNS 故障时阻塞调用方（generateNftConfLocked 在持锁中调用）
//   - 优先返回 IPv4（中转转发场景 v4 路由最普遍），无 A 记录才回退 IPv6
//   - 同组记录排序后取首个，避免 CDN/轮询 DNS 多记录导致 IP 抖动、反复触发 nft 重载
func resolveDomainIP(host string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", err
	}
	var v4, v6 []string
	for _, ip := range ips {
		if ip.IP.To4() != nil {
			v4 = append(v4, ip.IP.String())
		} else {
			v6 = append(v6, ip.IP.String())
		}
	}
	sort.Strings(v4)
	sort.Strings(v6)
	if len(v4) > 0 {
		return v4[0], nil
	}
	if len(v6) > 0 {
		return v6[0], nil
	}
	return "", fmt.Errorf("域名 %s 无可用 A/AAAA 记录", host)
}

// --- 配置加载 ---

func LoadPanelConfig() error {
	// 如果 config.toml 不存在，尝试从 config.toml.example 自动生成
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		examplePath := configPath + ".example"
		if _, err := os.Stat(examplePath); err == nil {
			data, err := os.ReadFile(examplePath)
			if err != nil {
				return fmt.Errorf("读取 %s 失败: %v", examplePath, err)
			}
			if err := os.WriteFile(configPath, data, 0600); err != nil {
				return fmt.Errorf("生成 %s 失败: %v", configPath, err)
			}
			log.Printf("已从 %s 自动生成 %s，请及时修改默认密码！", examplePath, configPath)
		} else {
			return fmt.Errorf("配置文件 %s 不存在，也找不到 %s 模板", configPath, examplePath)
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if _, err := toml.Decode(string(data), &panelConfig); err != nil {
		return err
	}

	if panelConfig.Nftables.ConfigPath == "" {
		panelConfig.Nftables.ConfigPath = "/etc/nftables.conf"
	}
	if panelConfig.Nftables.RulesPath == "" {
		panelConfig.Nftables.RulesPath = "/root/.nft-forward/rules.json"
	}
	if panelConfig.Realm.ConfigPath == "" {
		panelConfig.Realm.ConfigPath = "/etc/realm/config.toml"
	}
	if panelConfig.Realm.RulesPath == "" {
		panelConfig.Realm.RulesPath = "/root/.nft-forward/realm-rules.json"
	}
	if panelConfig.Telegram.ReportTime == "" {
		panelConfig.Telegram.ReportTime = "08:00"
	}

	// 自动生成 Session Secret
	if panelConfig.Session.Secret == "" {
		secret, err := generateRandomSecret(32)
		if err != nil {
			return fmt.Errorf("生成 Session Secret 失败: %v", err)
		}
		panelConfig.Session.Secret = secret
		log.Println("已自动生成 Session Secret")
		if err := savePanelConfig(); err != nil {
			return fmt.Errorf("保存配置失败: %v", err)
		}
	}

	// 自动将明文密码迁移为 bcrypt 哈希
	if panelConfig.Auth.Password != "" && panelConfig.Auth.PasswordHash == "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(panelConfig.Auth.Password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("密码哈希化失败: %v", err)
		}
		panelConfig.Auth.PasswordHash = string(hash)
		panelConfig.Auth.Password = ""
		log.Println("已将明文密码迁移为 bcrypt 哈希")
		if err := savePanelConfig(); err != nil {
			return fmt.Errorf("保存配置失败: %v", err)
		}
	}

	// 自动生成 Metrics Token
	if panelConfig.Metrics.Token == "" {
		token, err := generateRandomSecret(32)
		if err != nil {
			return fmt.Errorf("生成 Metrics Token 失败: %v", err)
		}
		panelConfig.Metrics.Token = token
		log.Println("已自动生成 Metrics Token")
		if err := savePanelConfig(); err != nil {
			return fmt.Errorf("保存配置失败: %v", err)
		}
	}

	// 没有任何密码配置 → 提示
	if panelConfig.Auth.PasswordHash == "" && panelConfig.Auth.Password == "" {
		log.Println("警告: 未配置任何密码，请在 config.toml 中设置 [auth] password")
	}

	return nil
}

func generateRandomSecret(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func savePanelConfig() error {
	var buf bytes.Buffer
	buf.WriteString("# nftables 转发面板配置\n")
	buf.WriteString("# 警告: 请修改默认密码！首次运行后明文密码会自动迁移为 bcrypt 哈希\n\n")

	buf.WriteString("[auth]\n")
	if panelConfig.Auth.Password != "" {
		buf.WriteString(fmt.Sprintf("password = \"%s\"\n", tomlEscape(panelConfig.Auth.Password)))
	}
	if panelConfig.Auth.PasswordHash != "" {
		buf.WriteString(fmt.Sprintf("password_hash = \"%s\"\n", tomlEscape(panelConfig.Auth.PasswordHash)))
	}
	buf.WriteString("\n")

	buf.WriteString("[server]\n")
	buf.WriteString(fmt.Sprintf("port = %d\n\n", panelConfig.Server.Port))

	buf.WriteString("[https]\n")
	buf.WriteString(fmt.Sprintf("enabled = %t\n", panelConfig.HTTPS.Enabled))
	buf.WriteString(fmt.Sprintf("cert_file = \"%s\"\n", tomlEscape(panelConfig.HTTPS.CertFile)))
	buf.WriteString(fmt.Sprintf("key_file = \"%s\"\n\n", tomlEscape(panelConfig.HTTPS.KeyFile)))

	buf.WriteString("[nftables]\n")
	buf.WriteString(fmt.Sprintf("config_path = \"%s\"\n", tomlEscape(panelConfig.Nftables.ConfigPath)))
	buf.WriteString(fmt.Sprintf("rules_path = \"%s\"\n\n", tomlEscape(panelConfig.Nftables.RulesPath)))

	buf.WriteString("[realm]\n")
	buf.WriteString(fmt.Sprintf("config_path = \"%s\"\n", tomlEscape(panelConfig.Realm.ConfigPath)))
	buf.WriteString(fmt.Sprintf("rules_path = \"%s\"\n\n", tomlEscape(panelConfig.Realm.RulesPath)))

	tg := tgConfigSnapshot()
	buf.WriteString("[telegram]\n")
	buf.WriteString(fmt.Sprintf("enabled = %t\n", tg.Enabled))
	buf.WriteString(fmt.Sprintf("bot_token = \"%s\"\n", tomlEscape(tg.BotToken)))
	buf.WriteString(fmt.Sprintf("chat_id = \"%s\"\n", tomlEscape(tg.ChatID)))
	buf.WriteString(fmt.Sprintf("daily_report = %t\n", tg.DailyReport))
	buf.WriteString(fmt.Sprintf("report_time = \"%s\"\n", tomlEscape(tg.ReportTime)))
	buf.WriteString(fmt.Sprintf("alert_quota = %t\n", tg.AlertQuota))
	buf.WriteString(fmt.Sprintf("alert_gfw = %t\n\n", tg.AlertGFW))

	buf.WriteString("[session]\n")
	buf.WriteString(fmt.Sprintf("secret = \"%s\"\n\n", tomlEscape(panelConfig.Session.Secret)))

	buf.WriteString("[metrics]\n")
	buf.WriteString(fmt.Sprintf("token = \"%s\"\n\n", tomlEscape(panelConfig.Metrics.Token)))

	if len(panelConfig.Nodes) > 0 {
		for _, n := range panelConfig.Nodes {
			buf.WriteString("[[nodes]]\n")
			buf.WriteString(fmt.Sprintf("name = \"%s\"\n", tomlEscape(n.Name)))
			buf.WriteString(fmt.Sprintf("url = \"%s\"\n", tomlEscape(n.URL)))
			buf.WriteString(fmt.Sprintf("token = \"%s\"\n\n", tomlEscape(n.Token)))
		}
	}

	return os.WriteFile(configPath, buf.Bytes(), 0600)
}

// --- 规则持久化 ---

// 调用方必须持有 mu 锁
func LoadRules() error {
	// 确保父目录存在
	dir := filepath.Dir(panelConfig.Nftables.RulesPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("创建规则目录失败 %s: %v", dir, err)
	}

	data, err := os.ReadFile(panelConfig.Nftables.RulesPath)
	if err != nil {
		rules = []ForwardRule{}
		return saveRulesLocked()
	}
	if err := json.Unmarshal(data, &rules); err != nil {
		return err
	}
	
	// 旧数据向后兼容：将新加载配置中因零值而判定停用的项，如果在初始态，强制修正为开启
	changed := false
	for i := range rules {
		if !rules[i].Enabled && rules[i].UsedBytes == 0 && rules[i].QuotaGB == 0 {
			rules[i].Enabled = true
			changed = true
		}
	}
	if changed {
		_ = saveRulesLocked()
	}
	return nil
}

// 调用方必须持有 mu 锁
func saveRulesLocked() error {
	// 落盘前复制并清空运行时字段 Reachable，避免重启后残留旧拨测状态
	cp := make([]ForwardRule, len(rules))
	copy(cp, rules)
	for i := range cp {
		cp[i].Reachable = nil
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(panelConfig.Nftables.RulesPath, data, 0600)
}

// --- Realm 规则持久化 ---

// 调用方必须持有 mu 锁
func LoadRealmRules() error {
	dir := filepath.Dir(panelConfig.Realm.RulesPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("创建 Realm 规则目录失败 %s: %v", dir, err)
	}

	data, err := os.ReadFile(panelConfig.Realm.RulesPath)
	if err != nil {
		realmRules = []RealmRule{}
		return saveRealmRulesLocked()
	}
	if err := json.Unmarshal(data, &realmRules); err != nil {
		return err
	}
	return nil
}

// 调用方必须持有 mu 锁
func saveRealmRulesLocked() error {
	data, err := json.MarshalIndent(realmRules, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(panelConfig.Realm.RulesPath, data, 0600)
}

// --- nftables 配置生成 ---

func isIPv6(addr string) bool {
	clean := strings.Trim(addr, "[]")
	return strings.Contains(clean, ":")
}

// 调用方必须持有 mu 锁（读 rules）
func generateNftConfLocked() error {
	var buf bytes.Buffer

	buf.WriteString("#!/usr/sbin/nft -f\n\n")
	// 仅删除本项目创建的表，不影响其他防火墙规则
	buf.WriteString("table inet nft_forward\n")
	buf.WriteString("delete table inet nft_forward\n\n")
	buf.WriteString("table inet nft_forward {\n\n")

	// prerouting 链
	buf.WriteString("    chain prerouting {\n")
	buf.WriteString("        type nat hook prerouting priority -100; policy accept;\n\n")

	for i := range rules {
		rule := &rules[i]
		if !rule.Enabled {
			continue // 超额封停或被禁用的规则跳过其 NAT，拦截网络
		}

		targetIP := strings.Trim(rule.RemoteAddr, "[]")
		if net.ParseIP(targetIP) == nil {
			// 目标是域名：内核只认 IP，这里替换为解析后的 IP
			if rule.LastResolvedIP != "" && net.ParseIP(rule.LastResolvedIP) != nil {
				targetIP = rule.LastResolvedIP // 优先用缓存（含后台定时刷新的结果）
			} else if ip, err := resolveDomainIP(targetIP); err == nil {
				targetIP = ip
				rule.LastResolvedIP = ip // 首次解析成功，写入缓存（后续会持久化到 rules.json）
			} else {
				log.Printf("[警告] 规则 %s 的目标域名 %s 解析失败，跳过此转发规则: %v", rule.ID, rule.RemoteAddr, err)
				buf.WriteString(fmt.Sprintf("        # Rule %s (跳过: 域名 %s 解析失败)\n\n", rule.ID, rule.RemoteAddr))
				continue
			}
		}

		// 安全: 经过校验的值再额外做一次 sanitize
		lport := sanitizeForNft(rule.LocalPort)
		rport := sanitizeForNft(rule.RemotePort)
		addr := sanitizeForNft(targetIP)

		noteComment := ""
		if rule.Note != "" {
			// 注释里也做 sanitize 防止换行注入
			noteComment = fmt.Sprintf(" (%s)", sanitizeForNft(rule.Note))
		}

		// 内核级 comment：写入 nftables 规则元数据，nft list ruleset 时可直接看到规则用途
		// 格式: "nat_端口" 或 "nat_端口_备注"，截断到 80 字符防止超出 nftables 128 字符上限
		nftComment := fmt.Sprintf("nat_%s", lport)
		if rule.Note != "" {
			sanitizedNote := sanitizeForNft(rule.Note)
			if len(sanitizedNote) > 60 {
				sanitizedNote = sanitizedNote[:60]
			}
			nftComment = fmt.Sprintf("nat_%s_%s", lport, sanitizedNote)
		}

		if isIPv6(targetIP) {
			buf.WriteString(fmt.Sprintf("        # Rule %s%s\n", rule.ID, noteComment))
			buf.WriteString(fmt.Sprintf("        tcp dport %s dnat ip6 to [%s]:%s comment \"%s\"\n", lport, addr, rport, nftComment))
			buf.WriteString(fmt.Sprintf("        udp dport %s dnat ip6 to [%s]:%s comment \"%s\"\n", lport, addr, rport, nftComment))
		} else {
			buf.WriteString(fmt.Sprintf("        # Rule %s%s\n", rule.ID, noteComment))
			buf.WriteString(fmt.Sprintf("        tcp dport %s dnat ip to %s:%s comment \"%s\"\n", lport, addr, rport, nftComment))
			buf.WriteString(fmt.Sprintf("        udp dport %s dnat ip to %s:%s comment \"%s\"\n", lport, addr, rport, nftComment))
		}
		buf.WriteString("\n")
	}

	buf.WriteString("    }\n\n")

	// forward 链 — 流量统计（filter 类型可统计所有包，NAT 链只统计首包）
	// 用远端 IP+端口匹配（最大兼容性），comment 标记本机端口供解析器提取
	buf.WriteString("    chain forward {\n")
	buf.WriteString("        type filter hook forward priority 0; policy accept;\n\n")
	for i := range rules {
		rule := &rules[i]
		if !rule.Enabled {
			continue
		}

		targetIP := strings.Trim(rule.RemoteAddr, "[]")
		if net.ParseIP(targetIP) == nil {
			// 域名规则：仅在已有有效缓存 IP 时才生成统计规则；尚未解析成功的跳过，防止 nft 报错
			if rule.LastResolvedIP != "" && net.ParseIP(rule.LastResolvedIP) != nil {
				targetIP = rule.LastResolvedIP
			} else {
				continue
			}
		}

		lport := sanitizeForNft(rule.LocalPort)
		rport := sanitizeForNft(rule.RemotePort)
		addr := sanitizeForNft(targetIP)
		ipFamily := "ip"
		if isIPv6(targetIP) {
			ipFamily = "ip6"
		}
		// 去程：客户端 → 远端（匹配目标地址+端口）
		buf.WriteString(fmt.Sprintf("        %s daddr %s tcp dport %s counter comment \"fwd_%s\"\n", ipFamily, addr, rport, lport))
		buf.WriteString(fmt.Sprintf("        %s daddr %s udp dport %s counter comment \"fwd_%s\"\n", ipFamily, addr, rport, lport))
		// 回程：远端 → 客户端（匹配源地址+端口）
		buf.WriteString(fmt.Sprintf("        %s saddr %s tcp sport %s counter comment \"fwd_%s\"\n", ipFamily, addr, rport, lport))
		buf.WriteString(fmt.Sprintf("        %s saddr %s udp sport %s counter comment \"fwd_%s\"\n", ipFamily, addr, rport, lport))
	}
	buf.WriteString("    }\n\n")

	// postrouting 链 — 仅对 DNAT 过的包做 masquerade
	buf.WriteString("    chain postrouting {\n")
	buf.WriteString("        type nat hook postrouting priority 100; policy accept;\n")
	buf.WriteString("        ct status dnat masquerade\n")
	buf.WriteString("    }\n")
	buf.WriteString("}\n")

	return os.WriteFile(panelConfig.Nftables.ConfigPath, buf.Bytes(), 0644)
}

// 调用方必须持有 mu 锁
func applyNftRulesLocked() error {
	if err := generateNftConfLocked(); err != nil {
		return fmt.Errorf("生成配置失败: %v", err)
	}
	cmd := exec.Command("nft", "-f", panelConfig.Nftables.ConfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("应用规则失败: %s - %v", string(output), err)
	}
	return nil
}

// --- Telegram 告警与通知推送 ---

// noteOrDash 用于告警文案，备注为空时回退为占位符
func noteOrDash(note string) string {
	if strings.TrimSpace(note) == "" {
		return "无备注"
	}
	return note
}

// maskBotToken 生成用于展示的掩码 Token（GET 下发与 POST 还原必须共用同一实现，
// 否则前端把掩码原样提交回来时会覆盖掉真实 Token）
func maskBotToken(token string) string {
	if len(token) <= 6 {
		return token
	}
	return token[:6] + ":****"
}

// resolveBotToken 决定最终写入的 Token：
// 前端把 GET 下发的掩码原样提交回来时视为「未修改」，保留 current；
// 空值同样视为「未修改」——loadTgConfig 拉取失败会静默留空，按字面写入会误清空 Token。
// 只有提交了一个既非空、又不等于当前掩码的值，才认为用户真的换了 Token。
func resolveBotToken(incoming, current string) string {
	if incoming == "" || incoming == maskBotToken(current) {
		return current
	}
	return incoming
}

// tgConfigSnapshot 在 tgMu 保护下返回 Telegram 配置副本，供各协程无竞争读取
func tgConfigSnapshot() TelegramConfig {
	tgMu.RLock()
	defer tgMu.RUnlock()
	return panelConfig.Telegram
}

func sendTelegramNotification(text string) {
	tg := tgConfigSnapshot()
	if !tg.Enabled || tg.BotToken == "" || tg.ChatID == "" {
		return
	}
	go func() {
		apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tg.BotToken)
		payload := map[string]string{
			"chat_id":    tg.ChatID,
			"text":       text,
			"parse_mode": "Markdown",
		}
		body, _ := json.Marshal(payload)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Post(apiURL, "application/json", bytes.NewBuffer(body))
		if err != nil {
			log.Printf("[TG Bot] 推送失败: %v", err)
			return
		}
		defer resp.Body.Close()
	}()
}

// --- Realm 配置生成与操作 ---

func generateRealmConfLocked() error {
	if panelConfig.Realm.ConfigPath == "" {
		panelConfig.Realm.ConfigPath = "/etc/realm/config.toml"
	}
	var buf bytes.Buffer
	buf.WriteString("# Realm Configuration generated by nft-panel\n\n")
	buf.WriteString("[network]\nno_tcp = false\nuse_udp = true\n\n")

	for _, rule := range realmRules {
		if !rule.Enabled {
			continue
		}
		buf.WriteString("[[endpoints]]\n")
		buf.WriteString(fmt.Sprintf("listen = \"0.0.0.0:%s\"\n", rule.ListenPort))
		buf.WriteString(fmt.Sprintf("remote = \"%s:%s\"\n", rule.RemoteAddr, rule.RemotePort))

		// 将前端的 network 选项映射为 realm 的 no_tcp / use_udp 布尔值
		// realm 端点级别通过 network 内联表覆盖全局 [network] 配置
		switch rule.Network {
		case "tcp":
			// 只转 TCP：禁用 UDP
			buf.WriteString("network = { use_udp = false }\n")
		case "udp":
			// 只转 UDP：禁用 TCP
			buf.WriteString("network = { no_tcp = true }\n")
		// "tcp+udp" 或空值：使用全局默认（no_tcp=false, use_udp=true），无需额外配置
		}
		buf.WriteString("\n")
	}

	dir := filepath.Dir(panelConfig.Realm.ConfigPath)
	_ = os.MkdirAll(dir, 0755)
	return os.WriteFile(panelConfig.Realm.ConfigPath, buf.Bytes(), 0644)
}

// applyRealmRulesLocked 持久化 Realm 规则、重新生成 realm 配置并重启服务
// 调用方必须持有 mu 锁，且在调用前将 backup 设为修改前的 realmRules 快照
// 落盘或生成配置失败时回滚内存状态；服务重启失败不回滚（规则已持久化，
// 待 realm 安装或修复后生效），但会返回错误让前端提示用户
func applyRealmRulesLocked(backup []RealmRule) error {
	if err := saveRealmRulesLocked(); err != nil {
		realmRules = backup
		return fmt.Errorf("保存 Realm 规则失败: %v", err)
	}
	if err := generateRealmConfLocked(); err != nil {
		realmRules = backup
		_ = saveRealmRulesLocked()
		return fmt.Errorf("生成 Realm 配置失败: %v", err)
	}
	out, err := exec.Command("systemctl", "restart", "realm").CombinedOutput()
	if err != nil {
		log.Printf("[Realm] 重启服务失败: %v (%s)", err, strings.TrimSpace(string(out)))
		return fmt.Errorf("规则已保存，但 realm 服务重启失败（是否已安装 realm？）: %v", err)
	}
	return nil
}

// saveAndApplyLocked 保存规则并应用，失败时回滚内存状态
// 调用方必须持有 mu 锁，且在调用前将 backup 设为修改前的 rules 快照
func saveAndApplyLocked(backup []ForwardRule) error {
	if err := saveRulesLocked(); err != nil {
		rules = backup // 回滚内存
		return fmt.Errorf("保存规则失败: %v", err)
	}
	if err := applyNftRulesLocked(); err != nil {
		// 回滚: 恢复内存 + 磁盘 JSON + 磁盘 nftables.conf
		rules = backup
		_ = saveRulesLocked()
		_ = generateNftConfLocked()
		return err
	}
	return nil
}

// 深拷贝 rules 用于回滚
func snapshotRules() []ForwardRule {
	cp := make([]ForwardRule, len(rules))
	copy(cp, rules)
	return cp
}

// --- 密码校验 ---

func verifyPassword(inputPassword string) bool {
	if panelConfig.Auth.PasswordHash != "" {
		return bcrypt.CompareHashAndPassword([]byte(panelConfig.Auth.PasswordHash), []byte(inputPassword)) == nil
	}
	if panelConfig.Auth.Password != "" {
		log.Println("警告: 正在使用明文密码验证，请检查 bcrypt 哈希迁移是否成功")
		return panelConfig.Auth.Password == inputPassword
	}
	return false
}

// --- 流量提取与主控拉取 ---

func parseNftCounters(out []byte) map[string]uint64 {
	counterMap := make(map[string]uint64)
	var data map[string]interface{}
	if err := json.Unmarshal(out, &data); err != nil {
		return counterMap
	}
	nftables, ok := data["nftables"].([]interface{})
	if !ok {
		return counterMap
	}
	for _, item := range nftables {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		ruleObj, ok := obj["rule"].(map[string]interface{})
		if !ok {
			continue
		}
		// 只解析 forward 链的 counter（流量统计）
		chain, _ := ruleObj["chain"].(string)
		if chain != "forward" {
			continue
		}

		// 从 comment 字段提取本机端口（格式: "fwd_12142"）
		comment, _ := ruleObj["comment"].(string)
		if !strings.HasPrefix(comment, "fwd_") {
			continue
		}
		localPort := strings.TrimPrefix(comment, "fwd_")

		exprs, ok := ruleObj["expr"].([]interface{})
		if !ok {
			continue
		}

		var countBytes uint64
		for _, expr := range exprs {
			e, ok := expr.(map[string]interface{})
			if !ok {
				continue
			}
			if counter, ok := e["counter"].(map[string]interface{}); ok {
				if bytes, ok := counter["bytes"].(float64); ok {
					countBytes = uint64(bytes)
				}
			}
		}
		counterMap[localPort] += countBytes
	}
	return counterMap
}

// checkForwardBlocked 检测 iptables FORWARD 链是否为 DROP 且缺少 DNAT 放行规则
// 典型场景：Docker 将 FORWARD 策略设为 DROP，导致 nftables DNAT 转发流量被拦截
func checkForwardBlocked() bool {
	// 检查 FORWARD 默认策略
	out, err := exec.Command("iptables", "-L", "FORWARD", "-n").Output()
	if err != nil {
		return false // iptables 不可用，不阻断
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) == 0 {
		return false
	}
	// 首行格式: "Chain FORWARD (policy DROP)"
	if !strings.Contains(lines[0], "policy DROP") {
		return false // 策略不是 DROP，不阻断
	}
	// 策略是 DROP，检查是否有 DNAT 放行规则
	checkOut, err := exec.Command("iptables", "-C", "FORWARD", "-m", "conntrack", "--ctstate", "DNAT", "-j", "ACCEPT").CombinedOutput()
	_ = checkOut
	if err == nil {
		return false // 有放行规则，不阻断
	}
	return true // FORWARD=DROP 且无 DNAT 放行 → 转发被阻断
}

// checkGFWBlocked 检测服务器 IP 是否被 GFW 封锁（双向阻断检测）
// 原理：GFW 对 IP 的封锁是双向的 - 被封锁 IP 不仅国内无法访问，从该 IP 向国内发包也会被边境路由器丢弃
// 调用时机：由混合模式控制 - 默认每 5 分钟定时调用一次，流量异常时立即触发
// 方法：并发测试 TCP 连接国内公共 DNS（53端口）与国际 DNS 的成功率
//
//	国际至少有一个通 + 国内全部超时 -> 大概率被墙
func checkGFWBlocked() bool {
	type probeResult struct {
		china bool
		ok    bool
	}

	// 国内知名公共 DNS（TCP 53 端口）
	cnEndpoints := []string{
		"223.5.5.5:53",    // 阿里 DNS
		"119.29.29.29:53", // 腾讯 DNSPod
		"180.76.76.76:53", // 百度 DNS
	}
	// 国际端点：验证服务器自身出站网络正常
	intlEndpoints := []string{
		"8.8.8.8:53", // Google DNS
		"1.1.1.1:53", // Cloudflare DNS
	}

	ch := make(chan probeResult, len(cnEndpoints)+len(intlEndpoints))

	for _, ep := range cnEndpoints {
		go func(addr string) {
			conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				ch <- probeResult{china: true, ok: false}
			} else {
				conn.Close()
				ch <- probeResult{china: true, ok: true}
			}
		}(ep)
	}
	for _, ep := range intlEndpoints {
		go func(addr string) {
			conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				ch <- probeResult{china: false, ok: false}
			} else {
				conn.Close()
				ch <- probeResult{china: false, ok: true}
			}
		}(ep)
	}

	cnFail, intlFail := 0, 0
	for i := 0; i < len(cnEndpoints)+len(intlEndpoints); i++ {
		r := <-ch
		if r.china && !r.ok {
			cnFail++
		}
		if !r.china && !r.ok {
			intlFail++
		}
	}

	// 国际至少有一个通 + 国内全部不通 → 判定被墙
	return intlFail < len(intlEndpoints) && cnFail == len(cnEndpoints)
}

// remoteCfgMu 串行化远程配置修改（读-改-写-重启-回滚），避免并发交错破坏 .bak 与配置
var remoteCfgMu sync.Mutex

// Reality shortId：0-16 位十六进制、偶数长度（代表 0-8 字节）
var reShortID = regexp.MustCompile(`^[0-9a-fA-F]+$`)

func validateShortID(s string) bool {
	return reShortID.MatchString(s) && len(s) <= 16 && len(s)%2 == 0
}

// uTLS 指纹白名单（Xray 客户端 fingerprint 取值）
var validFingerprints = map[string]bool{
	"chrome": true, "firefox": true, "safari": true, "ios": true,
	"android": true, "edge": true, "360": true, "qq": true,
	"random": true, "randomized": true,
}

// shadowsocks-rust 支持的加密方式白名单
var validSSMethods = map[string]bool{
	"aes-128-gcm": true, "aes-256-gcm": true,
	"chacha20-ietf-poly1305": true,
	"2022-blake3-aes-128-gcm": true, "2022-blake3-aes-256-gcm": true,
	"2022-blake3-chacha20-poly1305": true,
	"plain":                         true, "none": true,
}

// restartAndVerify 重启服务并探活，服务未进入 active 视为失败
func restartAndVerify(service string) error {
	if out, err := exec.Command("systemctl", "restart", service).CombinedOutput(); err != nil {
		return fmt.Errorf("restart 失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	// 服务可能启动后立即崩溃，稍等再确认最终状态
	time.Sleep(800 * time.Millisecond)
	if err := exec.Command("systemctl", "is-active", "--quiet", service).Run(); err != nil {
		return fmt.Errorf("%s 重启后未处于 active 状态", service)
	}
	return nil
}

// applyServiceConfig 原子地应用新配置：先在临时文件上校验（validate 可为 nil），
// 通过后备份并写入 live 配置、重启服务并探活；任一步失败都保证 live 配置与运行服务
// 回到修改前的状态。返回 nil 表示新配置已生效。
func applyServiceConfig(path string, newData []byte, service string, validate func(tmpPath string) error) error {
	oldData, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取原配置失败: %v", err)
	}

	// 预校验在临时文件上进行，不触碰 live 配置
	if validate != nil {
		tmp := path + ".new"
		if err := os.WriteFile(tmp, newData, 0644); err != nil {
			return fmt.Errorf("写入临时配置失败: %v", err)
		}
		verr := validate(tmp)
		_ = os.Remove(tmp)
		if verr != nil {
			return fmt.Errorf("配置校验未通过: %v", verr)
		}
	}

	_ = os.WriteFile(path+".bak", oldData, 0644)
	if err := os.WriteFile(path, newData, 0644); err != nil {
		return fmt.Errorf("写入配置失败: %v", err)
	}

	if err := restartAndVerify(service); err != nil {
		// 回滚到旧配置并尝试恢复服务
		_ = os.WriteFile(path, oldData, 0644)
		if rbErr := restartAndVerify(service); rbErr != nil {
			return fmt.Errorf("%v；且回滚后服务仍异常: %v", err, rbErr)
		}
		return fmt.Errorf("%v，已回滚到原配置", err)
	}
	return nil
}

// updateXrayClientLink 根据服务端 config.json 重新生成客户端 reclient.json 中的连接链接
// 在远程修改 Xray 配置后调用，确保客户端链接与服务端配置一致
// fpOverride/spxOverride 为本次修改新指定的客户端指纹与 spiderX（客户端专属参数，
// 不写入服务端 config.json）；为空时沿用 reclient.json 中已有的值，再退回默认。
func updateXrayClientLink(xrayConfigPath, xrayClientPath, fpOverride, spxOverride string) {
	data, err := os.ReadFile(xrayConfigPath)
	if err != nil {
		log.Printf("[远程配置] 读取 Xray 配置失败: %v", err)
		return
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}

	inbounds, ok := cfg["inbounds"].([]interface{})
	if !ok || len(inbounds) == 0 {
		return
	}
	ib, ok := inbounds[0].(map[string]interface{})
	if !ok {
		return
	}

	// 提取关键参数
	port := 443
	if p, ok := ib["port"].(float64); ok {
		port = int(p)
	}
	uuid := ""
	flow := ""
	if settings, ok := ib["settings"].(map[string]interface{}); ok {
		if clients, ok := settings["clients"].([]interface{}); ok && len(clients) > 0 {
			if cl, ok := clients[0].(map[string]interface{}); ok {
				uuid, _ = cl["id"].(string)
				flow, _ = cl["flow"].(string)
			}
		}
	}
	sni := ""
	shortID := ""
	publicKey := ""
	// 指纹与 spiderX 是客户端参数，只从本次覆盖值 / reclient.json 取，不读服务端配置
	fingerprint := fpOverride
	spiderX := spxOverride
	if ss, ok := ib["streamSettings"].(map[string]interface{}); ok {
		if rs, ok := ss["realitySettings"].(map[string]interface{}); ok {
			if sn, ok := rs["serverNames"].([]interface{}); ok && len(sn) > 0 {
				sni, _ = sn[0].(string)
			}
			// shortIds 可能是 []string 或 []interface{}
			if sids, ok := rs["shortIds"].([]interface{}); ok && len(sids) > 0 {
				shortID, _ = sids[0].(string)
			} else if sids, ok := rs["shortIds"].([]string); ok && len(sids) > 0 {
				shortID = sids[0]
			}
			// 公钥存在 privateKey 同级的 publicKey 字段（reality 安装脚本写入）
			publicKey, _ = rs["publicKey"].(string)
		}
	}

	if uuid == "" {
		return
	}

	// 获取本机 IP
	serverIP := ""
	ipOut, err := exec.Command("curl", "-s", "-4", "--max-time", "3", "ip.sb").Output()
	if err != nil || len(strings.TrimSpace(string(ipOut))) == 0 {
		ipOut, _ = exec.Command("curl", "-s", "--max-time", "3", "ip.sb").Output()
	}
	serverIP = strings.TrimSpace(string(ipOut))
	if serverIP == "" {
		log.Println("[远程配置] 无法获取服务器 IP，跳过更新 reclient.json")
		return
	}

	// 读取现有 reclient.json（保留公钥等字段）
	var clientCfg map[string]interface{}
	if existingData, err := os.ReadFile(xrayClientPath); err == nil {
		_ = json.Unmarshal(existingData, &clientCfg)
	}
	if clientCfg == nil {
		clientCfg = make(map[string]interface{})
	}

	// 从已有 reclient.json 补齐未指定的字段（公钥/指纹/spiderX），保证多次编辑间不丢失
	if params, ok := clientCfg["配置参数"].(map[string]interface{}); ok {
		if publicKey == "" {
			if pk, ok := params["公钥"].(string); ok {
				publicKey = pk
			}
		}
		if fingerprint == "" {
			fingerprint, _ = params["指纹"].(string)
		}
		if spiderX == "" {
			spiderX, _ = params["spiderX"].(string)
		}
	}
	if fingerprint == "" {
		fingerprint = "chrome"
	}

	// 构建 VLESS 链接
	host := serverIP
	if strings.Contains(serverIP, ":") {
		host = "[" + serverIP + "]"
	}
	link := fmt.Sprintf("vless://%s@%s:%d?encryption=none&flow=%s&security=reality&sni=%s&fp=%s&pbk=%s&sid=%s&spx=%s&type=tcp#VLESS-%d",
		uuid, host, port, flow, sni, fingerprint, publicKey, shortID, spiderX, port)

	clientCfg["连接链接"] = link
	clientCfg["配置参数"] = map[string]interface{}{
		"地址":       serverIP,
		"端口":       port,
		"UUID":     uuid,
		"流控":       flow,
		"SNI":      sni,
		"公钥":       publicKey,
		"Short ID": shortID,
		"指纹":       fingerprint,
		"spiderX":  spiderX,
	}

	newClientData, err := json.MarshalIndent(clientCfg, "", "  ")
	if err != nil {
		log.Printf("[远程配置] 序列化 reclient.json 失败: %v", err)
		return
	}
	if err := os.WriteFile(xrayClientPath, newClientData, 0644); err != nil {
		log.Printf("[远程配置] 写入 reclient.json 失败: %v", err)
	}
}

func fetchNodeMetrics(n NodeConf) NodeSnapshot {
	snap := NodeSnapshot{
		Name:     n.Name,
		URL:      n.URL,
		Hostname: n.URL,
		Online:   false,
		LastSeen: time.Now(),
	}
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", n.URL+"/api/metrics", nil)
	if err != nil {
		return snap
	}
	req.Header.Set("Authorization", "Bearer "+n.Token)
	resp, err := client.Do(req)
	if err != nil {
		return snap
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		if err := json.NewDecoder(resp.Body).Decode(&snap); err == nil {
			snap.Online = true
			snap.Name = n.Name
			snap.URL = n.URL
			snap.LastSeen = time.Now()
		}
	}
	return snap
}

// --- 登录限流 ---

// 检查 IP 是否被锁定
func isLoginLocked(ip string) (bool, time.Duration) {
	loginMu.Lock()
	defer loginMu.Unlock()

	if until, ok := loginLockUntil[ip]; ok {
		if time.Now().Before(until) {
			return true, time.Until(until)
		}
		// 锁定已过期，清理
		delete(loginLockUntil, ip)
		delete(loginAttempts, ip)
	}
	return false, 0
}

// 记录登录失败
func recordLoginFailure(ip string) {
	loginMu.Lock()
	defer loginMu.Unlock()

	loginAttempts[ip]++
	if loginAttempts[ip] >= maxAttempts {
		loginLockUntil[ip] = time.Now().Add(lockDuration)
		log.Printf("IP %s 连续登录失败 %d 次，已锁定 %v", ip, maxAttempts, lockDuration)
	}
}

// 登录成功后清除记录
func clearLoginAttempts(ip string) {
	loginMu.Lock()
	defer loginMu.Unlock()

	delete(loginAttempts, ip)
	delete(loginLockUntil, ip)
}

// --- 中间件 ---

func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		user := session.Get("user")
		if user == nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		c.Next()
	}
}

// --- 主函数 ---

func main() {
	if err := LoadPanelConfig(); err != nil {
		log.Fatalf("无法加载面板配置: %v", err)
	}

	mu.Lock()
	if err := LoadRules(); err != nil {
		log.Fatalf("无法加载转发规则: %v", err)
	}
	if err := LoadRealmRules(); err != nil {
		log.Printf("加载 Realm 规则失败: %v", err)
	} else if len(realmRules) > 0 {
		// 重新生成 realm 配置，确保磁盘配置与面板状态一致
		if err := generateRealmConfLocked(); err != nil {
			log.Printf("启动时生成 Realm 配置失败: %v", err)
		} else {
			log.Printf("启动时已加载 %d 条 Realm 规则", len(realmRules))
		}
	}
	// 启动时自动重新生成并应用 nftables 配置（确保 forward 统计链等新功能生效）
	if err := applyNftRulesLocked(); err != nil {
		log.Printf("启动时应用规则失败（可能首次安装未配置）: %v", err)
	} else {
		log.Printf("启动时已重新应用 %d 条规则", len(rules))
	}
	mu.Unlock()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	store := cookie.NewStore([]byte(panelConfig.Session.Secret))
	store.Options(sessions.Options{
		HttpOnly: true,
		MaxAge:   3600 * 4,
		Path:     "/",
	})
	r.Use(sessions.Sessions("nft_session", store))

	// --- 并发轮询控制 ---
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		// 启动后立即执行第一次检测，不等 60 秒
		firstRun := make(chan struct{}, 1)
		firstRun <- struct{}{}
		for {
			select {
			case <-firstRun:
			case <-ticker.C:
			}
			out, err := exec.Command("nft", "-j", "list", "table", "inet", "nft_forward").Output()
			if err != nil {
				continue
			}

			// 检测 iptables FORWARD DROP（Docker 冲突）
			forwardBlocked := checkForwardBlocked()

			// 并发拨测所有落地机连通性（每条规则一个 goroutine，3 秒超时）
			// 先拷贝需要拨测的信息，避免长时间持锁
			// 用 rule.ID 作为索引键，避免拨测期间增删规则导致下标错位
			mu.Lock()
			type probeTarget struct {
				id   string
				addr string
				port string
			}
			targets := make([]probeTarget, len(rules))
			for i, rule := range rules {
				targets[i] = probeTarget{rule.ID, rule.RemoteAddr, rule.RemotePort}
			}
			mu.Unlock()

			type probeResult struct {
				id        string
				reachable bool
			}
			results := make(chan probeResult, len(targets))
			for _, t := range targets {
				go func(id, addr, port string) {
					// 移除 IPv6 方括号
					cleanAddr := strings.Trim(addr, "[]")
					target := net.JoinHostPort(cleanAddr, port)
					conn, err := net.DialTimeout("tcp", target, 3*time.Second)
					if err == nil {
						conn.Close()
						results <- probeResult{id, true}
					} else {
						results <- probeResult{id, false}
					}
				}(t.id, t.addr, t.port)
			}

			// 收集拨测结果（按规则 ID 索引）
			reachMap := make(map[string]bool)
			for j := 0; j < len(targets); j++ {
				r := <-results
				reachMap[r.id] = r.reachable
			}

			counterMap := parseNftCounters(out)
			mu.Lock()
			changed := false
			backup := snapshotRules()
			now := time.Now()
			currentMonth := now.Format("2006-01")
			currentDay := now.Day()
			// 计算当月最后一天：下月1号往前推1天
			lastDayOfMonth := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day()
			// GFW 流量异常追踪：统计本轮有流量增量的规则数 和 目标可达的规则数
			currActiveRules := 0
			currReachableRules := 0
			// 本轮因超额被封停的规则，待释放 mu 后再推送 TG 告警
			var quotaAlerts []string

			for i := range rules {
				// 1. 动态域名解析与变更检测（DDNS/CDN 目标 IP 变化时自动热重载）
				domain := strings.Trim(rules[i].RemoteAddr, "[]")
				if net.ParseIP(domain) == nil {
					// 这是一个域名，后台定时解析其最新 IP（resolveDomainIP 已做超时/优先v4/排序去抖）
					if resolved, err := resolveDomainIP(domain); err == nil {
						if rules[i].LastResolvedIP != resolved {
							log.Printf("[DNS] 域名 %s 目标 IP 发生变更: %s -> %s", rules[i].RemoteAddr, rules[i].LastResolvedIP, resolved)
							rules[i].LastResolvedIP = resolved
							changed = true
						}
					} else {
						// 解析失败：保留上次成功的缓存 IP 兜底，不清空、不触发重载
						log.Printf("[DNS] 后台定时解析域名 %s 失败（沿用缓存 %s）: %v", rules[i].RemoteAddr, rules[i].LastResolvedIP, err)
					}
				}

				// 流量统计：增量累加，不直接覆盖
				// nft counter 在 delete table 后会清零，所以用“本次读数 - 上次读数”算增量
				var thisDelta uint64
				if currentCounter, ok := counterMap[rules[i].LocalPort]; ok {
					prevCounter := lastCounterSnap[rules[i].LocalPort]
					if currentCounter >= prevCounter {
						thisDelta = currentCounter - prevCounter
					} else {
						// counter 被重置了（delete table 或重启 nftables），直接加上新值
						thisDelta = currentCounter
					}
					rules[i].UsedBytes += thisDelta
				}
				// 追踪本轮有流量增量的规则数（用于 GFW 异常检测）
				if thisDelta > 0 {
					currActiveRules++
				}

				// 更新连通性状态：落地可达 且 FORWARD 链未被阻断
				// 按规则 ID 匹配，避免拨测期间增删规则导致索引错位
				if reachable, ok := reachMap[rules[i].ID]; ok {
					r := reachable && !forwardBlocked
					rules[i].Reachable = &r
					if r {
						currReachableRules++
					}
				}

				// 账期自动重置：当月未重置过 且 今天已到达重置日（或月末兜底）
				// 例：ResetDay=31 但2月只有28天 → 在28号（月末最后一天）自动触发
				if rules[i].ResetDay > 0 && rules[i].LastResetTime != currentMonth {
					effectiveDay := rules[i].ResetDay
					if effectiveDay > lastDayOfMonth {
						effectiveDay = lastDayOfMonth // 短月兜底：月末最后一天触发
					}
					if currentDay >= effectiveDay {
						rules[i].UsedBytes = 0
						rules[i].Enabled = true
						rules[i].LastResetTime = currentMonth
						changed = true
						log.Printf("规则 %s (端口 %s) 账期重置，流量已清零（重置日:%d，实际触发日:%d）", rules[i].ID, rules[i].LocalPort, rules[i].ResetDay, currentDay)
					}
				}

				// 判断封停
				if rules[i].QuotaGB > 0 && rules[i].Enabled {
					if float64(rules[i].UsedBytes) > rules[i].QuotaGB*1024*1024*1024 {
						rules[i].Enabled = false
						changed = true
						log.Printf("规则 %s (端口 %s) 流量超额，已封停", rules[i].ID, rules[i].LocalPort)
						quotaAlerts = append(quotaAlerts, fmt.Sprintf(
							"端口 %s（%s）已用 %.2f GB / 配额 %.2f GB",
							rules[i].LocalPort, noteOrDash(rules[i].Note),
							float64(rules[i].UsedBytes)/(1024*1024*1024), rules[i].QuotaGB))
					}
				}
			}
			if changed {
				_ = saveAndApplyLocked(backup)
			} else {
				// 没封停也存一次总流量
				_ = saveRulesLocked()
			}
			// 更新 counter 快照，供下一轮增量计算
			lastCounterSnap = counterMap
			totalRulesThisCycle := len(rules)
			mu.Unlock()

			// 配额超额告警（在释放 mu 后推送，避免持锁做网络 IO）
			if len(quotaAlerts) > 0 && tgConfigSnapshot().AlertQuota {
				sendTelegramNotification(fmt.Sprintf(
					"*流量配额告警*\n\n以下 %d 条规则已达配额并自动封停：\n\n%s",
					len(quotaAlerts), strings.Join(quotaAlerts, "\n")))
			}

			// === GFW 混合检测 ===
			// 策略：默认每 5 分钟定时探测一次；流量异常时立即触发确认
			// 流量异常判定：上轮 ≥3 条规则有流量 → 本轮全部归零 + 目标仍可达
			gfwTickCount++
			shouldCheckGFW := gfwTickCount >= gfwCheckInterval
			if !shouldCheckGFW && gfwPrevActiveRules >= 3 && currActiveRules == 0 &&
				currReachableRules > 0 && totalRulesThisCycle > 0 {
				shouldCheckGFW = true
				log.Printf("[GFW] 流量异常触发探测：上轮 %d 条规则有流量，本轮全部归零（目标可达 %d 条）",
					gfwPrevActiveRules, currReachableRules)
			}
			gfwPrevActiveRules = currActiveRules

			if shouldCheckGFW {
				gfwTickCount = 0
				isGFWBlocked := checkGFWBlocked()
				gfwMu.Lock()
				gfwChanged := isGFWBlocked != gfwBlocked
				if gfwChanged {
					if isGFWBlocked {
						log.Println("[GFW] ⚠️ 检测到服务器 IP 可能被墙：国内 DNS 端点全部超时，国际端点正常")
					} else {
						log.Println("[GFW] ✓ 服务器 IP 国内连通性已恢复")
					}
					gfwBlocked = isGFWBlocked
				}
				gfwMu.Unlock()

				// 封锁状态发生翻转时推送 TG 告警（仅状态变化时推送，避免重复轰炸）
				if gfwChanged && tgConfigSnapshot().AlertGFW {
					if isGFWBlocked {
						sendTelegramNotification("*GFW 封锁告警*\n\n检测到服务器 IP 可能被墙：国内 DNS 端点全部超时，国际端点正常。\n所有中转链路可能已中断，请及时更换 IP。")
					} else {
						sendTelegramNotification("*GFW 封锁解除*\n\n服务器 IP 国内连通性已恢复，中转链路恢复正常。")
					}
				}
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for ; true; <-ticker.C {
			// 动态读取节点列表，运行时增删节点无需重启
			nodesMu.RLock()
			nodeList := make([]NodeConf, len(panelConfig.Nodes))
			copy(nodeList, panelConfig.Nodes)
			nodesMu.RUnlock()

			if len(nodeList) == 0 {
				continue
			}

			var wg sync.WaitGroup
			snapshots := make([]NodeSnapshot, len(nodeList))
			for i, node := range nodeList {
				wg.Add(1)
				go func(idx int, n NodeConf) {
					defer wg.Done()
					snapshots[idx] = fetchNodeMetrics(n)
				}(i, node)
			}
			wg.Wait()
			nodesMu.Lock()
			nodesCache = snapshots
			nodesMu.Unlock()
		}
	}()

	// --- 探针拉取 ---
	r.GET("/api/metrics", func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if panelConfig.Metrics.Token == "" || authHeader != "Bearer "+panelConfig.Metrics.Token {
			c.AbortWithStatusJSON(401, gin.H{"error": "Unauthorized"})
			return
		}
		hostname, _ := os.Hostname()
		mu.Lock()
		snapRules := snapshotRules()
		mu.Unlock()
		c.JSON(200, gin.H{
			"hostname":  hostname,
			"timestamp": time.Now().Unix(),
			"rules":     snapRules,
		})
	})

	// --- 被控端：远程修改节点配置 API（metrics token 鉴权） ---
	// 校验 metrics token 的辅助函数
	metricsAuth := func(c *gin.Context) bool {
		authHeader := c.GetHeader("Authorization")
		if panelConfig.Metrics.Token == "" || authHeader != "Bearer "+panelConfig.Metrics.Token {
			c.AbortWithStatusJSON(401, gin.H{"error": "Unauthorized"})
			return false
		}
		return true
	}

	// 远程修改 Xray Reality 配置
	// 支持字段：sni, dest, port, short_id, fingerprint, spider_x
	// 修改服务端 config.json + 客户端 reclient.json，然后重启 xray 服务
	r.POST("/api/node/update-xray", func(c *gin.Context) {
		if !metricsAuth(c) {
			return
		}
		xrayConfigPath := "/usr/local/etc/xray/config.json"
		xrayClientPath := "/usr/local/etc/xray/reclient.json"

		if _, err := os.Stat(xrayConfigPath); err != nil {
			c.JSON(404, gin.H{"error": "Xray 未安装"})
			return
		}

		var req struct {
			SNI         string `json:"sni"`
			Dest        string `json:"dest"`
			Port        int    `json:"port"`
			ShortID     string `json:"short_id"`
			Fingerprint string `json:"fingerprint"`
			SpiderX     string `json:"spider_x"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "参数错误: " + err.Error()})
			return
		}

		// --- 输入校验（接口可绕过前端直连，必须在被控侧校验） ---
		if req.Port != 0 && (req.Port < 1 || req.Port > 65535) {
			c.JSON(400, gin.H{"error": "端口无效 (1-65535)"})
			return
		}
		if req.SNI != "" && !(reDomain.MatchString(req.SNI) && len(req.SNI) <= 253) {
			c.JSON(400, gin.H{"error": "SNI 不是合法域名"})
			return
		}
		if req.Dest != "" && !validateAddress(strings.Split(req.Dest, ":")[0]) {
			c.JSON(400, gin.H{"error": "dest 地址无效"})
			return
		}
		if req.ShortID != "" && !validateShortID(req.ShortID) {
			c.JSON(400, gin.H{"error": "short_id 必须为偶数长度的十六进制（≤16 位）"})
			return
		}
		if req.Fingerprint != "" && !validFingerprints[req.Fingerprint] {
			c.JSON(400, gin.H{"error": "fingerprint 取值不在支持列表内"})
			return
		}

		remoteCfgMu.Lock()
		defer remoteCfgMu.Unlock()

		data, err := os.ReadFile(xrayConfigPath)
		if err != nil {
			c.JSON(500, gin.H{"error": "读取配置失败: " + err.Error()})
			return
		}
		var cfg map[string]interface{}
		if err := json.Unmarshal(data, &cfg); err != nil {
			c.JSON(500, gin.H{"error": "解析配置失败: " + err.Error()})
			return
		}

		// 修改 inbounds[0] 的相关字段
		inbounds, ok := cfg["inbounds"].([]interface{})
		if !ok || len(inbounds) == 0 {
			c.JSON(500, gin.H{"error": "配置中未找到 inbounds"})
			return
		}
		ib, ok := inbounds[0].(map[string]interface{})
		if !ok {
			c.JSON(500, gin.H{"error": "inbounds[0] 格式错误"})
			return
		}

		if req.Port > 0 {
			ib["port"] = req.Port
		}

		// 修改 Reality 服务端参数（fingerprint/spiderX 属于客户端参数，不写入服务端配置）
		if ss, ok := ib["streamSettings"].(map[string]interface{}); ok {
			if rs, ok := ss["realitySettings"].(map[string]interface{}); ok {
				if req.SNI != "" {
					rs["serverNames"] = []string{req.SNI}
					if req.Dest != "" {
						rs["dest"] = req.Dest
					} else {
						rs["dest"] = req.SNI + ":443"
					}
				} else if req.Dest != "" {
					rs["dest"] = req.Dest
				}
				if req.ShortID != "" {
					rs["shortIds"] = []string{req.ShortID}
				}
			}
		}

		newData, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			c.JSON(500, gin.H{"error": "序列化配置失败: " + err.Error()})
			return
		}

		// 原子应用：先 xray -test 校验临时配置，通过才写入并重启，失败自动回滚
		xrayTest := func(tmp string) error {
			out, err := exec.Command("/usr/local/bin/xray", "run", "-test", "-config", tmp).CombinedOutput()
			if err != nil {
				return fmt.Errorf("%s", strings.TrimSpace(string(out)))
			}
			return nil
		}
		if err := applyServiceConfig(xrayConfigPath, newData, "xray", xrayTest); err != nil {
			c.JSON(500, gin.H{"error": "Xray 配置应用失败: " + err.Error()})
			return
		}

		// 配置生效后再更新客户端 reclient.json 连接链接
		updateXrayClientLink(xrayConfigPath, xrayClientPath, req.Fingerprint, req.SpiderX)

		// 构建已修改字段摘要（不回显敏感值）
		changed := []string{}
		if req.SNI != "" { changed = append(changed, "sni") }
		if req.Dest != "" { changed = append(changed, "dest") }
		if req.Port != 0 { changed = append(changed, "port") }
		if req.ShortID != "" { changed = append(changed, "short_id") }
		if req.Fingerprint != "" { changed = append(changed, "fingerprint") }
		if req.SpiderX != "" { changed = append(changed, "spider_x") }
		log.Printf("[远程配置] Xray Reality 已更新 (来自 %s, 字段: %v)", c.ClientIP(), changed)
		c.JSON(200, gin.H{"message": "Xray Reality 配置已更新并重启", "changed_fields": changed})
	})

	// 远程修改 Shadowsocks 配置
	// 支持字段：port, password, method
	r.POST("/api/node/update-ss", func(c *gin.Context) {
		if !metricsAuth(c) {
			return
		}
		ssConfigPath := "/etc/shadowsocks/config.json"

		if _, err := os.Stat(ssConfigPath); err != nil {
			c.JSON(404, gin.H{"error": "Shadowsocks 未安装"})
			return
		}

		var req struct {
			Port     int    `json:"port"`
			Password string `json:"password"`
			Method   string `json:"method"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "参数错误: " + err.Error()})
			return
		}

		// --- 输入校验 ---
		if req.Port != 0 && (req.Port < 1 || req.Port > 65535) {
			c.JSON(400, gin.H{"error": "端口无效 (1-65535)"})
			return
		}
		if req.Method != "" && !validSSMethods[req.Method] {
			c.JSON(400, gin.H{"error": "加密方式不在支持列表内"})
			return
		}

		remoteCfgMu.Lock()
		defer remoteCfgMu.Unlock()

		data, err := os.ReadFile(ssConfigPath)
		if err != nil {
			c.JSON(500, gin.H{"error": "读取配置失败: " + err.Error()})
			return
		}
		var cfg map[string]interface{}
		if err := json.Unmarshal(data, &cfg); err != nil {
			c.JSON(500, gin.H{"error": "解析配置失败: " + err.Error()})
			return
		}

		if req.Port > 0 {
			cfg["server_port"] = req.Port
		}
		if req.Password != "" {
			cfg["password"] = req.Password
		}
		if req.Method != "" {
			cfg["method"] = req.Method
		}

		newData, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			c.JSON(500, gin.H{"error": "序列化配置失败: " + err.Error()})
			return
		}

		// SS 无独立的配置校验命令，靠重启后探活；失败自动回滚（含 method/password 不匹配等情况）
		if err := applyServiceConfig(ssConfigPath, newData, "shadowsocks", nil); err != nil {
			c.JSON(500, gin.H{"error": "Shadowsocks 配置应用失败: " + err.Error()})
			return
		}

		// 构建已修改字段摘要（不回显密码等敏感值）
		changed := []string{}
		if req.Port != 0 { changed = append(changed, "port") }
		if req.Password != "" { changed = append(changed, "password") }
		if req.Method != "" { changed = append(changed, "method") }
		log.Printf("[远程配置] Shadowsocks 已更新 (来自 %s, 字段: %v)", c.ClientIP(), changed)
		c.JSON(200, gin.H{"message": "Shadowsocks 配置已更新并重启", "changed_fields": changed})
	})

	staticSubFS, _ := fs.Sub(staticFS, "static")
	r.StaticFS("/static", http.FS(staticSubFS))

	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		log.Fatalf("无法加载模板: %v", err)
	}
	r.SetHTMLTemplate(tmpl)

	// --- 登录 ---
	r.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		if session.Get("user") != nil {
			c.Redirect(http.StatusFound, "/")
			return
		}
		c.HTML(http.StatusOK, "login.html", nil)
	})

	r.POST("/login", func(c *gin.Context) {
		clientIP := c.ClientIP()

		// 检查是否被锁定
		if locked, remaining := isLoginLocked(clientIP); locked {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": fmt.Sprintf("登录失败次数过多，请 %d 分钟后重试", int(remaining.Minutes())+1),
			})
			return
		}

		var loginData struct {
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&loginData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效请求"})
			return
		}
		if verifyPassword(loginData.Password) {
			clearLoginAttempts(clientIP)
			session := sessions.Default(c)
			session.Set("user", true)
			session.Options(sessions.Options{MaxAge: 3600 * 4, HttpOnly: true, Path: "/"})
			if err := session.Save(); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Session 保存失败"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "登录成功"})
		} else {
			recordLoginFailure(clientIP)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "密码错误"})
		}
	})

	// --- 认证路由组 ---
	api := r.Group("/")
	api.Use(AuthRequired())
	{
		api.GET("/", func(c *gin.Context) {
			c.HTML(http.StatusOK, "index.html", nil)
		})

		// 获取规则（分页）
		api.GET("/api/rules", func(c *gin.Context) {
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
			if page < 1 {
				page = 1
			}
			if size < 1 || size > 100 {
				size = 10
			}

			mu.Lock()
			defer mu.Unlock()

			total := len(rules)
			start := (page - 1) * size
			end := start + size
			if start >= total {
				start = total
			}
			if end > total {
				end = total
			}

			gfwMu.RLock()
			isGFW := gfwBlocked
			gfwMu.RUnlock()
			c.JSON(200, gin.H{
				"rules":       rules[start:end],
				"total":       total,
				"gfw_blocked": isGFW,
			})
		})

		// 添加单条规则
		api.POST("/api/rules", func(c *gin.Context) {
			var input struct {
				LocalPort  string  `json:"local_port"`
				RemoteAddr string  `json:"remote_addr"`
				RemotePort string  `json:"remote_port"`
				Note       string  `json:"note"`
				QuotaGB    float64 `json:"quota_gb"`
				ResetDay   int     `json:"reset_day"`
			}
			if err := c.ShouldBindJSON(&input); err != nil {
				c.JSON(400, gin.H{"error": "无效输入"})
				return
			}

			// 校验所有字段
			if !validatePort(input.LocalPort) {
				c.JSON(400, gin.H{"error": "本机端口无效 (1-65535)"})
				return
			}
			if !validatePort(input.RemotePort) {
				c.JSON(400, gin.H{"error": "目标端口无效 (1-65535)"})
				return
			}
			if !validateAddress(input.RemoteAddr) {
				c.JSON(400, gin.H{"error": "目标地址无效，请输入合法的 IPv4/IPv6/域名"})
				return
			}

			mu.Lock()
			defer mu.Unlock()

			// 检查端口重复
			for _, r := range rules {
				if r.LocalPort == input.LocalPort {
					c.JSON(409, gin.H{"error": fmt.Sprintf("端口 %s 已存在", input.LocalPort)})
					return
				}
			}

			// 先备份再修改
			backup := snapshotRules()

			newRule := ForwardRule{
				ID:         uuid.New().String()[:8],
				LocalPort:  input.LocalPort,
				RemoteAddr: input.RemoteAddr,
				RemotePort: input.RemotePort,
				Note:       input.Note,
				QuotaGB:    input.QuotaGB,
				ResetDay:   input.ResetDay,
				Enabled:    true,
			}
			rules = append(rules, newRule)

			if err := saveAndApplyLocked(backup); err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}

			c.JSON(201, newRule)
		})

		// 批量添加规则
		api.POST("/api/rules/batch", func(c *gin.Context) {
			var input struct {
				Rules []struct {
					LocalPort  string `json:"local_port"`
					RemoteAddr string `json:"remote_addr"`
					RemotePort string `json:"remote_port"`
					Note       string `json:"note"`
					// 此处批量未扩展quota_gb输入，可考虑支持，默认为0
				} `json:"rules"`
			}
			if err := c.ShouldBindJSON(&input); err != nil {
				c.JSON(400, gin.H{"error": "无效输入"})
				return
			}

			mu.Lock()
			defer mu.Unlock()

			backup := snapshotRules()
			added := 0
			failed := []string{}

			for _, item := range input.Rules {
				// 校验每条规则
				if !validatePort(item.LocalPort) {
					failed = append(failed, fmt.Sprintf("端口 %s 无效", item.LocalPort))
					continue
				}
				if !validatePort(item.RemotePort) {
					failed = append(failed, fmt.Sprintf("目标端口 %s 无效", item.RemotePort))
					continue
				}
				if !validateAddress(item.RemoteAddr) {
					failed = append(failed, fmt.Sprintf("地址 %s 无效", item.RemoteAddr))
					continue
				}

				exists := false
				for _, r := range rules {
					if r.LocalPort == item.LocalPort {
						exists = true
						break
					}
				}
				if exists {
					failed = append(failed, fmt.Sprintf("端口 %s 已存在", item.LocalPort))
					continue
				}

				rules = append(rules, ForwardRule{
					ID:         uuid.New().String()[:8],
					LocalPort:  item.LocalPort,
					RemoteAddr: item.RemoteAddr,
					RemotePort: item.RemotePort,
					Note:       item.Note,
					Enabled:    true,
				})
				added++
			}

			if added > 0 {
				if err := saveAndApplyLocked(backup); err != nil {
					c.JSON(500, gin.H{"error": err.Error()})
					return
				}
			}

			c.JSON(200, gin.H{
				"added":  added,
				"failed": failed,
			})
		})

		// 删除规则
		api.DELETE("/api/rules/:id", func(c *gin.Context) {
			id := c.Param("id")

			mu.Lock()
			defer mu.Unlock()

			backup := snapshotRules()
			found := false
			for i, r := range rules {
				if r.ID == id {
					rules = append(rules[:i], rules[i+1:]...)
					found = true
					break
				}
			}

			if !found {
				c.JSON(404, gin.H{"error": "规则不存在"})
				return
			}

			if err := saveAndApplyLocked(backup); err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}

			c.JSON(200, gin.H{"message": "规则已删除"})
		})

		// 重置用量
		api.POST("/api/rules/:id/reset", func(c *gin.Context) {
			id := c.Param("id")
			mu.Lock()
			defer mu.Unlock()
			backup := snapshotRules()
			found := false
			for i, r := range rules {
				if r.ID == id {
					rules[i].UsedBytes = 0
					rules[i].Enabled = true
					found = true
					break
				}
			}
			if !found {
				c.JSON(404, gin.H{"error": "规则不存在"})
				return
			}
			if err := saveAndApplyLocked(backup); err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, gin.H{"message": "流量已重置并恢复"})
		})

		// 设置配额
		api.PUT("/api/rules/:id/quota", func(c *gin.Context) {
			id := c.Param("id")
			var input struct {
				QuotaGB float64 `json:"quota_gb"`
			}
			if err := c.ShouldBindJSON(&input); err != nil {
				c.JSON(400, gin.H{"error": "无效输入"})
				return
			}
			mu.Lock()
			defer mu.Unlock()
			backup := snapshotRules()
			found := false
			for i, r := range rules {
				if r.ID == id {
					rules[i].QuotaGB = input.QuotaGB
					// 如果设置了配额，判断一下是不是可以解封
					if !rules[i].Enabled && (rules[i].QuotaGB == 0 || float64(rules[i].UsedBytes) <= rules[i].QuotaGB*1024*1024*1024) {
						rules[i].Enabled = true
					}
					found = true
					break
				}
			}
			if !found {
				c.JSON(404, gin.H{"error": "规则不存在"})
				return
			}
			if err := saveAndApplyLocked(backup); err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, gin.H{"message": "配额已更新"})
		})

		// 大盘探针汇总
		api.GET("/api/nodes/overview", func(c *gin.Context) {
			nodesMu.RLock()
			defer nodesMu.RUnlock()
			c.JSON(200, gin.H{"nodes": nodesCache})
		})

		// --- 节点管理 CRUD ---
		api.GET("/api/nodes/manage", func(c *gin.Context) {
			nodesMu.RLock()
			defer nodesMu.RUnlock()
			c.JSON(200, gin.H{"nodes": panelConfig.Nodes})
		})

		api.POST("/api/nodes/manage", func(c *gin.Context) {
			var input struct {
				Name  string `json:"name"`
				URL   string `json:"url"`
				Token string `json:"token"`
			}
			if err := c.ShouldBindJSON(&input); err != nil {
				c.JSON(400, gin.H{"error": "无效输入"})
				return
			}
			if input.Name == "" || input.URL == "" || input.Token == "" {
				c.JSON(400, gin.H{"error": "节点名称、地址和 Token 不能为空"})
				return
			}
			nodesMu.Lock()
			panelConfig.Nodes = append(panelConfig.Nodes, NodeConf{
				Name:  input.Name,
				URL:   input.URL,
				Token: input.Token,
			})
			nodesMu.Unlock()
			if err := savePanelConfig(); err != nil {
				c.JSON(500, gin.H{"error": "保存配置失败: " + err.Error()})
				return
			}
			log.Printf("新增监控节点: %s (%s)", input.Name, input.URL)
			c.JSON(200, gin.H{"message": "节点已添加"})
		})

		api.DELETE("/api/nodes/manage/:idx", func(c *gin.Context) {
			idxStr := c.Param("idx")
			idx, err := strconv.Atoi(idxStr)
			if err != nil {
				c.JSON(400, gin.H{"error": "节点索引无效"})
				return
			}
			nodesMu.Lock()
			if idx < 0 || idx >= len(panelConfig.Nodes) {
				nodesMu.Unlock()
				c.JSON(404, gin.H{"error": "节点不存在"})
				return
			}
			removed := panelConfig.Nodes[idx]
			panelConfig.Nodes = append(panelConfig.Nodes[:idx], panelConfig.Nodes[idx+1:]...)
			nodesMu.Unlock()
			if err := savePanelConfig(); err != nil {
				c.JSON(500, gin.H{"error": "保存配置失败: " + err.Error()})
				return
			}
			log.Printf("删除监控节点: %s (%s)", removed.Name, removed.URL)
			c.JSON(200, gin.H{"message": "节点已删除"})
		})

		// --- 主控端：代理转发远程节点配置修改 ---
		// 前端传入被控端索引 + 协议类型 + 配置参数，主控端转发请求到对应被控端
		api.POST("/api/remote-node/update", func(c *gin.Context) {
			var input struct {
				NodeIdx     int    `json:"node_idx"`     // 被控端在 nodes 数组中的索引
				NodeType    string `json:"node_type"`    // "xray" 或 "ss"
				SNI         string `json:"sni,omitempty"`
				Dest        string `json:"dest,omitempty"`
				Port        int    `json:"port,omitempty"`
				ShortID     string `json:"short_id,omitempty"`
				Fingerprint string `json:"fingerprint,omitempty"`
				SpiderX     string `json:"spider_x,omitempty"`
				Password    string `json:"password,omitempty"`
				Method      string `json:"method,omitempty"`
			}
			if err := c.ShouldBindJSON(&input); err != nil {
				c.JSON(400, gin.H{"error": "参数错误: " + err.Error()})
				return
			}

			// 查找对应的被控端节点
			nodesMu.RLock()
			if input.NodeIdx < 0 || input.NodeIdx >= len(panelConfig.Nodes) {
				nodesMu.RUnlock()
				c.JSON(404, gin.H{"error": "节点索引不存在"})
				return
			}
			target := panelConfig.Nodes[input.NodeIdx]
			nodesMu.RUnlock()

			// 构造转发请求
			var apiPath string
			var body []byte
			switch input.NodeType {
			case "xray":
				apiPath = "/api/node/update-xray"
				body, _ = json.Marshal(map[string]interface{}{
					"sni":         input.SNI,
					"dest":        input.Dest,
					"port":        input.Port,
					"short_id":    input.ShortID,
					"fingerprint": input.Fingerprint,
					"spider_x":    input.SpiderX,
				})
			case "ss":
				apiPath = "/api/node/update-ss"
				body, _ = json.Marshal(map[string]interface{}{
					"port":     input.Port,
					"password": input.Password,
					"method":   input.Method,
				})
			default:
				c.JSON(400, gin.H{"error": "不支持的节点类型，应为 xray 或 ss"})
				return
			}

			// 转发到被控端
			client := &http.Client{Timeout: 30 * time.Second}
			req, err := http.NewRequest("POST", target.URL+apiPath, bytes.NewReader(body))
			if err != nil {
				c.JSON(500, gin.H{"error": "构造请求失败: " + err.Error()})
				return
			}
			req.Header.Set("Authorization", "Bearer "+target.Token)
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				c.JSON(502, gin.H{"error": "连接被控端失败: " + err.Error()})
				return
			}
			defer resp.Body.Close()

			// 透传被控端的响应
			respBody, _ := io.ReadAll(resp.Body)
			var result gin.H
			if err := json.Unmarshal(respBody, &result); err != nil {
				result = gin.H{"raw_response": string(respBody)}
			}
			c.JSON(resp.StatusCode, result)
		})

		// 编辑规则（修改本机端口、目标地址、目标端口、备注、配额、重置日）
		api.PUT("/api/rules/:id", func(c *gin.Context) {
			id := c.Param("id")
			var input struct {
				LocalPort  string  `json:"local_port"`
				RemoteAddr string  `json:"remote_addr"`
				RemotePort string  `json:"remote_port"`
				Note       string  `json:"note"`
				QuotaGB    float64 `json:"quota_gb"`
				ResetDay   int     `json:"reset_day"`
			}
			if err := c.ShouldBindJSON(&input); err != nil {
				c.JSON(400, gin.H{"error": "无效输入"})
				return
			}

			// 校验字段（允许部分更新：只校验非空字段）
			if input.LocalPort != "" && !validatePort(input.LocalPort) {
				c.JSON(400, gin.H{"error": "本机端口无效 (1-65535)"})
				return
			}
			if input.RemotePort != "" && !validatePort(input.RemotePort) {
				c.JSON(400, gin.H{"error": "目标端口无效 (1-65535)"})
				return
			}
			if input.RemoteAddr != "" && !validateAddress(input.RemoteAddr) {
				c.JSON(400, gin.H{"error": "目标地址无效，请输入合法的 IPv4/IPv6/域名"})
				return
			}

			mu.Lock()
			defer mu.Unlock()

			// 如果要修改本机端口，需要检查是否与其他规则冲突
			if input.LocalPort != "" {
				for _, r := range rules {
					if r.ID != id && r.LocalPort == input.LocalPort {
						c.JSON(409, gin.H{"error": fmt.Sprintf("本机端口 %s 已被其他规则占用", input.LocalPort)})
						return
					}
				}
			}

			backup := snapshotRules()
			found := false
			for i, r := range rules {
				if r.ID == id {
					if input.LocalPort != "" {
						rules[i].LocalPort = input.LocalPort
					}
					if input.RemoteAddr != "" {
						rules[i].RemoteAddr = input.RemoteAddr
					}
					if input.RemotePort != "" {
						rules[i].RemotePort = input.RemotePort
					}
					// 备注允许清空，所以始终更新
					rules[i].Note = input.Note
					// 配额和重置日始终更新
					rules[i].QuotaGB = input.QuotaGB
					rules[i].ResetDay = input.ResetDay
					// 如果配额增大或取消限额，自动解封
					if !rules[i].Enabled && (rules[i].QuotaGB == 0 || float64(rules[i].UsedBytes) <= rules[i].QuotaGB*1024*1024*1024) {
						rules[i].Enabled = true
					}
					found = true
					break
				}
			}

			if !found {
				c.JSON(404, gin.H{"error": "规则不存在"})
				return
			}

			if err := saveAndApplyLocked(backup); err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}

			c.JSON(200, gin.H{"message": "规则已更新"})
		})

		// 服务控制
		api.POST("/api/service/start", func(c *gin.Context) {
			mu.Lock()
			err := applyNftRulesLocked()
			mu.Unlock()
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			cmd := exec.Command("systemctl", "start", "nftables")
			if err := cmd.Run(); err != nil {
				c.JSON(500, gin.H{"error": "启动失败"})
				return
			}
			c.JSON(200, gin.H{"message": "nftables 已启动"})
		})

		api.POST("/api/service/stop", func(c *gin.Context) {
			cmd := exec.Command("systemctl", "stop", "nftables")
			if err := cmd.Run(); err != nil {
				c.JSON(500, gin.H{"error": "停止失败"})
				return
			}
			c.JSON(200, gin.H{"message": "nftables 已停止"})
		})

		api.POST("/api/service/restart", func(c *gin.Context) {
			mu.Lock()
			err := applyNftRulesLocked()
			mu.Unlock()
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			cmd := exec.Command("systemctl", "restart", "nftables")
			if err := cmd.Run(); err != nil {
				c.JSON(500, gin.H{"error": "重启失败"})
				return
			}
			c.JSON(200, gin.H{"message": "nftables 已重启"})
		})

		api.GET("/api/service/status", func(c *gin.Context) {
			cmd := exec.Command("systemctl", "is-active", "--quiet", "nftables")
			err := cmd.Run()
			status := "已停止"
			if err == nil {
				status = "运行中"
			}
			gfwMu.RLock()
			isGFW := gfwBlocked
			gfwMu.RUnlock()
			c.JSON(200, gin.H{"status": status, "gfw_blocked": isGFW})
		})

		// 登出
		api.POST("/logout", func(c *gin.Context) {
			session := sessions.Default(c)
			session.Clear()
			session.Save()
			c.JSON(http.StatusOK, gin.H{"message": "登出成功"})
		})

		// 修改密码
		api.PUT("/api/password", func(c *gin.Context) {
			var input struct {
				OldPassword string `json:"old_password"`
				NewPassword string `json:"new_password"`
			}
			if err := c.ShouldBindJSON(&input); err != nil {
				c.JSON(400, gin.H{"error": "无效请求"})
				return
			}

			if input.OldPassword == "" || input.NewPassword == "" {
				c.JSON(400, gin.H{"error": "旧密码和新密码不能为空"})
				return
			}
			if len(input.NewPassword) < 4 {
				c.JSON(400, gin.H{"error": "新密码至少 4 个字符"})
				return
			}

			// 验证旧密码
			if !verifyPassword(input.OldPassword) {
				c.JSON(401, gin.H{"error": "当前密码错误"})
				return
			}

			// 生成新密码的 bcrypt 哈希
			hash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
			if err != nil {
				c.JSON(500, gin.H{"error": "密码加密失败"})
				return
			}

			// 更新配置并持久化
			panelConfig.Auth.PasswordHash = string(hash)
			panelConfig.Auth.Password = ""
			if err := savePanelConfig(); err != nil {
				c.JSON(500, gin.H{"error": "保存配置失败: " + err.Error()})
				return
			}

			log.Printf("密码已修改 (来自 IP: %s)", c.ClientIP())
			c.JSON(200, gin.H{"message": "密码修改成功"})
		})

		// 查看已部署节点
		api.GET("/api/nodes", func(c *gin.Context) {
			nodes := []gin.H{}

			// 检测 Xray Reality
			xrayConfigPath := "/usr/local/etc/xray/config.json"
			xrayClientPath := "/usr/local/etc/xray/reclient.json"
			if _, err := os.Stat(xrayConfigPath); err == nil {
				node := gin.H{
					"type":   "Xray Reality",
					"status": "未运行",
				}

				// 检查服务状态
				cmd := exec.Command("systemctl", "is-active", "--quiet", "xray")
				if cmd.Run() == nil {
					node["status"] = "运行中"
				}

				// 读取服务端配置，提取端口和 UUID
				if data, err := os.ReadFile(xrayConfigPath); err == nil {
					var cfg map[string]interface{}
					if json.Unmarshal(data, &cfg) == nil {
						if inbounds, ok := cfg["inbounds"].([]interface{}); ok && len(inbounds) > 0 {
							if ib, ok := inbounds[0].(map[string]interface{}); ok {
								if port, ok := ib["port"].(float64); ok {
									node["port"] = int(port)
								}
								// 提取 UUID
								if settings, ok := ib["settings"].(map[string]interface{}); ok {
									if clients, ok := settings["clients"].([]interface{}); ok && len(clients) > 0 {
										if client, ok := clients[0].(map[string]interface{}); ok {
											node["uuid"] = client["id"]
											node["flow"] = client["flow"]
										}
									}
								}
								// 提取 Reality 参数
								if ss, ok := ib["streamSettings"].(map[string]interface{}); ok {
									node["network"] = ss["network"]
									node["security"] = ss["security"]
									if rs, ok := ss["realitySettings"].(map[string]interface{}); ok {
										node["dest"] = rs["dest"]
										if sn, ok := rs["serverNames"].([]interface{}); ok && len(sn) > 0 {
											node["sni"] = sn[0]
										}
										if sids, ok := rs["shortIds"].([]interface{}); ok && len(sids) > 0 {
											node["short_id"] = sids[0]
										}
									}
								}
							}
						}
					}
				}

				// 读取客户端配置，提取连接链接和公钥
				if data, err := os.ReadFile(xrayClientPath); err == nil {
					var clientCfg map[string]interface{}
					if json.Unmarshal(data, &clientCfg) == nil {
						if link, ok := clientCfg["连接链接"].(string); ok {
							node["link"] = link
						}
						if params, ok := clientCfg["配置参数"].(map[string]interface{}); ok {
							node["public_key"] = params["公钥"]
							node["address"] = params["地址"]
						}
					}
				}

				nodes = append(nodes, node)
			}

			// 检测 Shadowsocks Rust
			ssConfigPath := "/etc/shadowsocks/config.json"
			if _, err := os.Stat(ssConfigPath); err == nil {
				node := gin.H{
					"type":   "Shadowsocks",
					"status": "未运行",
				}

				// 检查服务状态
				cmd := exec.Command("systemctl", "is-active", "--quiet", "shadowsocks")
				if cmd.Run() == nil {
					node["status"] = "运行中"
				}

				// 读取配置
				if data, err := os.ReadFile(ssConfigPath); err == nil {
					var cfg map[string]interface{}
					if json.Unmarshal(data, &cfg) == nil {
						if port, ok := cfg["server_port"].(float64); ok {
							node["port"] = int(port)
						}
						node["password"] = cfg["password"]
						node["method"] = cfg["method"]
					}
				}

				// 获取服务器 IP 用于生成 SS 链接
				// 优先获取 IPv4 地址，失败再回退 IPv6
				ipOut, err := exec.Command("curl", "-s", "-4", "--max-time", "3", "ip.sb").Output()
				if err != nil || len(strings.TrimSpace(string(ipOut))) == 0 {
					ipOut, err = exec.Command("curl", "-s", "--max-time", "3", "ip.sb").Output()
				}
				if err == nil {
					serverIP := strings.TrimSpace(string(ipOut))
					if serverIP != "" {
						node["address"] = serverIP
						// 生成 SS 链接（格式: ss://base64(method:password)@host:port#name）
						if pwd, ok := node["password"].(string); ok {
							if method, ok := node["method"].(string); ok {
								if port, ok := node["port"].(int); ok {
									userInfo := fmt.Sprintf("%s:%s", method, pwd)
									encoded := base64.StdEncoding.EncodeToString([]byte(userInfo))
									// IPv6 地址需要方括号包裹，否则客户端无法区分地址和端口
									host := serverIP
									if strings.Contains(serverIP, ":") {
										host = "[" + serverIP + "]"
									}
									node["link"] = fmt.Sprintf("ss://%s@%s:%d#SS-%d", encoded, host, port, port)
								}
							}
						}
					}
				}

				nodes = append(nodes, node)
			}

			c.JSON(200, gin.H{"nodes": nodes})
		})

		// --- Realm 转发规则 API ---
		api.GET("/api/realm/rules", func(c *gin.Context) {
			mu.Lock()
			defer mu.Unlock()
			c.JSON(200, gin.H{"rules": realmRules})
		})

		api.POST("/api/realm/rules", func(c *gin.Context) {
			var rule RealmRule
			if err := c.ShouldBindJSON(&rule); err != nil {
				c.JSON(400, gin.H{"error": "无效的规则数据"})
				return
			}

			// --- 输入校验（复用 nftables 的 validatePort/validateAddress） ---
			if !validatePort(rule.ListenPort) {
				c.JSON(400, gin.H{"error": "监听端口无效 (1-65535)"})
				return
			}
			if !validatePort(rule.RemotePort) {
				c.JSON(400, gin.H{"error": "目标端口无效 (1-65535)"})
				return
			}
			if !validateAddress(rule.RemoteAddr) {
				c.JSON(400, gin.H{"error": "目标地址无效，请输入合法的 IPv4/IPv6/域名"})
				return
			}
			// network 只允许三种值
			if rule.Network == "" {
				rule.Network = "tcp+udp"
			}
			if rule.Network != "tcp+udp" && rule.Network != "tcp" && rule.Network != "udp" {
				c.JSON(400, gin.H{"error": "协议类型无效，仅支持 tcp+udp / tcp / udp"})
				return
			}

			rule.ID = uuid.New().String()[:8]
			rule.Enabled = true

			mu.Lock()
			// --- 端口冲突检查：realm 内部 ---
			for _, r := range realmRules {
				if r.ListenPort == rule.ListenPort {
					mu.Unlock()
					c.JSON(409, gin.H{"error": fmt.Sprintf("监听端口 %s 已被其他 Realm 规则占用", rule.ListenPort)})
					return
				}
			}
			// --- 端口冲突检查：与 nftables 规则交叉 ---
			for _, r := range rules {
				if r.LocalPort == rule.ListenPort {
					mu.Unlock()
					c.JSON(409, gin.H{"error": fmt.Sprintf("监听端口 %s 已被 nftables 转发规则占用", rule.ListenPort)})
					return
				}
			}

			backup := make([]RealmRule, len(realmRules))
			copy(backup, realmRules)
			realmRules = append(realmRules, rule)
			err := applyRealmRulesLocked(backup)
			mu.Unlock()

			if err != nil {
				c.JSON(500, gin.H{"error": "配置应用失败: " + err.Error()})
				return
			}
			c.JSON(200, gin.H{"message": "Realm 规则保存成功", "rule": rule})
		})

		api.DELETE("/api/realm/rules/:id", func(c *gin.Context) {
			id := c.Param("id")
			mu.Lock()
			found := false
			newRules := make([]RealmRule, 0, len(realmRules))
			for _, r := range realmRules {
				if r.ID == id {
					found = true
				} else {
					newRules = append(newRules, r)
				}
			}
			if !found {
				mu.Unlock()
				c.JSON(404, gin.H{"error": "规则不存在"})
				return
			}
			backup := make([]RealmRule, len(realmRules))
			copy(backup, realmRules)
			realmRules = newRules
			err := applyRealmRulesLocked(backup)
			mu.Unlock()

			if err != nil {
				c.JSON(500, gin.H{"error": "配置应用失败: " + err.Error()})
				return
			}
			c.JSON(200, gin.H{"message": "Realm 规则已删除"})
		})

		// --- Telegram API ---
		api.GET("/api/tg/config", func(c *gin.Context) {
			cfg := tgConfigSnapshot()
			// 掩码 bot_token，避免明文泄露；提交时由 POST 侧还原
			cfg.BotToken = maskBotToken(cfg.BotToken)
			c.JSON(200, cfg)
		})

		api.POST("/api/tg/config", func(c *gin.Context) {
			var cfg TelegramConfig
			if err := c.ShouldBindJSON(&cfg); err != nil {
				c.JSON(400, gin.H{"error": "无效配置"})
				return
			}
			if cfg.ReportTime == "" {
				cfg.ReportTime = "08:00"
			}
			if _, err := time.Parse("15:04", cfg.ReportTime); err != nil {
				c.JSON(400, gin.H{"error": "推送时间格式无效，应为 HH:MM"})
				return
			}

			tgMu.Lock()
			// GET 下发的是掩码，前端原样提交回来时必须还原，否则真实 Token 会被星号覆盖
			cfg.BotToken = resolveBotToken(cfg.BotToken, panelConfig.Telegram.BotToken)
			panelConfig.Telegram = cfg
			tgMu.Unlock()

			if err := savePanelConfig(); err != nil {
				c.JSON(500, gin.H{"error": "保存配置失败: " + err.Error()})
				return
			}
			c.JSON(200, gin.H{"message": "TG 配置已保存"})
		})

		api.POST("/api/tg/test", func(c *gin.Context) {
			sendTelegramNotification("🔔 *Forward Pro 消息通知测试*\n\nTelegram 机器人连通正常！")
			c.JSON(200, gin.H{"message": "测试消息已发送"})
		})
	}

	// --- Telegram 定时流量报告守护协程 ---
	go func() {
		lastPushedDate := ""
		for {
			time.Sleep(30 * time.Second)

			tg := tgConfigSnapshot()
			if !tg.Enabled || !tg.DailyReport || tg.BotToken == "" || tg.ChatID == "" {
				continue
			}
			targetTime := tg.ReportTime
			if targetTime == "" {
				targetTime = "08:00"
			}
			target, err := time.Parse("15:04", targetTime)
			if err != nil {
				continue
			}

			now := time.Now()
			currentDate := now.Format("2006-01-02")
			if lastPushedDate == currentDate {
				continue
			}
			// 用「今天已过推送时间」而非精确匹配分钟，避免调度抖动导致整天漏推
			todayTarget := time.Date(now.Year(), now.Month(), now.Day(),
				target.Hour(), target.Minute(), 0, 0, now.Location())
			if now.Before(todayTarget) {
				continue
			}
			// 首次启动补推保护：距目标时间超过 6 小时则跳过，只标记，避免重启时补发过期报告
			if now.Sub(todayTarget) > 6*time.Hour {
				lastPushedDate = currentDate
				continue
			}
			lastPushedDate = currentDate

			mu.Lock()
			var totalUsed uint64
			activeCount := 0
			suspended := 0
			for _, r := range rules {
				totalUsed += r.UsedBytes
				if r.Enabled {
					activeCount++
				} else {
					suspended++
				}
			}
			totalRules := len(rules)
			realmCount := len(realmRules)
			mu.Unlock()

			gfwMu.RLock()
			blocked := gfwBlocked
			gfwMu.RUnlock()

			statusLine := "系统运行正常"
			if blocked {
				statusLine = "⚠️ 当前检测到 IP 可能被墙"
			}

			usedGB := float64(totalUsed) / (1024 * 1024 * 1024)
			msg := fmt.Sprintf(
				"📊 *Forward Pro 每日流量报告*\n\n"+
					"📅 日期: %s\n"+
					"⚡ nftables 规则: %d 条（活跃 %d / 封停 %d）\n"+
					"🔮 Realm 规则: %d 条\n"+
					"📈 总已用流量: %.2f GB\n\n%s",
				currentDate, totalRules, activeCount, suspended, realmCount, usedGB, statusLine)
			sendTelegramNotification(msg)
		}
	}()

	// --- 启动服务器 ---
	port := panelConfig.Server.Port
	if port == 0 {
		port = 3456
	}

	if panelConfig.HTTPS.Enabled && panelConfig.HTTPS.CertFile != "" && panelConfig.HTTPS.KeyFile != "" {
		log.Printf("面板运行在 HTTPS :%d\n", port)
		// HTTP → HTTPS 重定向 (监听 port+1，跳转到 port)
		go func() {
			httpPort := port + 1
			log.Printf("HTTP→HTTPS 重定向: :%d → :%d\n", httpPort, port)
			srv := &http.Server{
				Addr: fmt.Sprintf(":%d", httpPort),
				Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					host := req.Host
					// 去掉请求中的端口号
					if h, _, err := net.SplitHostPort(host); err == nil {
						host = h
					}
					target := fmt.Sprintf("https://%s:%d%s", host, port, req.URL.Path)
					if req.URL.RawQuery != "" {
						target += "?" + req.URL.RawQuery
					}
					http.Redirect(w, req, target, http.StatusMovedPermanently)
				}),
			}
			srv.ListenAndServe()
		}()
		if err := r.RunTLS(fmt.Sprintf(":%d", port), panelConfig.HTTPS.CertFile, panelConfig.HTTPS.KeyFile); err != nil {
			log.Fatalf("HTTPS 启动失败: %v", err)
		}
	} else {
		if panelConfig.HTTPS.Enabled {
			log.Println("警告: HTTPS 已启用但证书未配置，回退 HTTP")
		}
		log.Printf("面板运行在 HTTP :%d\n", port)
		r.Run(fmt.Sprintf(":%d", port))
	}
}
