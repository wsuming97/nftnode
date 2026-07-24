package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// 验证 savePanelConfig 写出的 TOML 能被完整读回，重点覆盖新增的 [realm] / [telegram] 段
func TestPanelConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	orig := configPath
	configPath = filepath.Join(dir, "config.toml")
	defer func() { configPath = orig }()

	panelConfig = PanelConfig{}
	panelConfig.Auth.PasswordHash = "$2a$10$abcdefg"
	panelConfig.Server.Port = 3456
	panelConfig.Nftables.ConfigPath = "/etc/nftables.conf"
	panelConfig.Nftables.RulesPath = "/root/.nft-forward/rules.json"
	panelConfig.Realm.ConfigPath = "/etc/realm/config.toml"
	panelConfig.Realm.RulesPath = "/root/.nft-forward/realm-rules.json"
	panelConfig.Session.Secret = "deadbeef"
	panelConfig.Metrics.Token = "tok123"
	panelConfig.Telegram = TelegramConfig{
		Enabled:     true,
		BotToken:    "123456:AAsomeTokenValue",
		ChatID:      "-1001234567890",
		DailyReport: true,
		ReportTime:  "09:30",
		AlertQuota:  true,
		AlertGFW:    true,
	}
	panelConfig.Nodes = []NodeConf{{Name: "tokyo", URL: "https://1.2.3.4:3456", Token: "nodetok"}}

	if err := savePanelConfig(); err != nil {
		t.Fatalf("savePanelConfig: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	t.Logf("generated config.toml:\n%s", data)

	var got PanelConfig
	if _, err := toml.Decode(string(data), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Telegram != panelConfig.Telegram {
		t.Errorf("telegram round-trip mismatch:\n got  %+v\n want %+v", got.Telegram, panelConfig.Telegram)
	}
	if got.Realm.ConfigPath != panelConfig.Realm.ConfigPath || got.Realm.RulesPath != panelConfig.Realm.RulesPath {
		t.Errorf("realm round-trip mismatch: got %+v want %+v", got.Realm, panelConfig.Realm)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].Name != "tokyo" {
		t.Errorf("nodes lost: %+v", got.Nodes)
	}
	if got.Auth.PasswordHash != panelConfig.Auth.PasswordHash || got.Metrics.Token != "tok123" {
		t.Errorf("existing sections regressed: %+v", got)
	}
}

// 回归测试：GET 下发掩码 → 前端原样提交 → 真实 Token 必须保留
// 这是「掩码把 Token 写坏」那个 bug 的守卫，删掉前请三思
func TestBotTokenMaskRoundTrip(t *testing.T) {
	const real = "7123456789:AAHk9xYzExampleRealTokenValue"

	masked := maskBotToken(real)
	if masked == real {
		t.Fatalf("token 未被掩码: %q", masked)
	}
	if strings.Contains(masked, "AAHk9x") {
		t.Errorf("掩码泄露了 Token 主体: %q", masked)
	}

	cases := []struct {
		name     string
		incoming string
		want     string
	}{
		// 用户只改了开关，输入框里还是 GET 回填的掩码 → 必须保留原 Token
		{"提交掩码视为未修改", masked, real},
		// loadTgConfig 静默失败导致输入框为空 → 不能把 Token 清空
		{"空值视为未修改", "", real},
		// 用户真的换了一个新 Token → 必须写入
		{"新 Token 正常写入", "9876543210:BBNewTokenValue", "9876543210:BBNewTokenValue"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveBotToken(tc.incoming, real); got != tc.want {
				t.Errorf("resolveBotToken(%q, real) = %q, want %q", tc.incoming, got, tc.want)
			}
		})
	}

	// 首次配置：当前无 Token，提交新值应正常写入
	if got := resolveBotToken("111111:FirstToken", ""); got != "111111:FirstToken" {
		t.Errorf("首次配置写入失败: %q", got)
	}
}

// 验证 Realm 规则能落盘并读回
func TestRealmRulesPersistence(t *testing.T) {
	dir := t.TempDir()
	panelConfig.Realm.RulesPath = filepath.Join(dir, "realm-rules.json")

	mu.Lock()
	defer mu.Unlock()

	realmRules = []RealmRule{
		{ID: "abc12345", ListenPort: "8080", Network: "tcp", RemoteAddr: "1.2.3.4", RemotePort: "443", Note: "test", Enabled: true},
	}
	if err := saveRealmRulesLocked(); err != nil {
		t.Fatalf("save: %v", err)
	}

	realmRules = nil
	if err := LoadRealmRules(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(realmRules) != 1 || realmRules[0].ListenPort != "8080" || !realmRules[0].Enabled {
		t.Fatalf("realm rules did not survive round-trip: %+v", realmRules)
	}
}
