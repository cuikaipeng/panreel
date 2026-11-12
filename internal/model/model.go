package model

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/cuikaipeng/panreel/internal/catalog"
)

type Item struct {
	ID          string    `json:"id"`
	Drive       string    `json:"drive"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	URL         string    `json:"url"`
	Password    string    `json:"password,omitempty"`
	Sources     []string  `json:"sources"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	Quality     string    `json:"quality,omitempty"`
	Year        string    `json:"year,omitempty"`
	Subtitle    string    `json:"subtitle,omitempty"`
	Size        string    `json:"size,omitempty"`
}
type History struct {
	Query string
	At    time.Time
}
type Check struct {
	State   string    `json:"state"`
	Summary string    `json:"summary"`
	At      time.Time `json:"-"`
}
type Settings struct {
	Drives         []string          `json:"drives"`
	Plugins        []string          `json:"plugins"`
	Channels       []string          `json:"channels"`
	CustomChannels []string          `json:"custom_channels,omitempty"`
	Source         string            `json:"source"`
	ProxyMode      string            `json:"proxy_mode,omitempty"`
	Proxy          string            `json:"proxy"`
	Timeout        int               `json:"timeout"`
	Concurrency    int               `json:"concurrency"`
	CacheMB        int               `json:"cache_mb"`
	PluginEnv      map[string]string `json:"plugin_env"`
	Extensions     json.RawMessage   `json:"extensions,omitempty"`
	Include        string            `json:"include"`
	Exclude        string            `json:"exclude"`
	Theme          string            `json:"theme"`
}

const (
	ProxySystem = "system"
	ProxyDirect = "direct"
	ProxyManual = "manual"
)

// An absent mode preserves old manual addresses while making new installs and
// old empty proxy settings follow the system. Do not fill it in Defaults:
// storage decodes old JSON on top of those defaults.
func (s Settings) EffectiveProxyMode() string {
	if s.ProxyMode != "" {
		return s.ProxyMode
	}
	if s.Proxy != "" {
		return ProxyManual
	}
	return ProxySystem
}

func Defaults() Settings {
	return Settings{Drives: catalog.DriveIDs(), Plugins: catalog.PluginIDs(), Channels: []string{}, Source: "all", Timeout: 45, Concurrency: 16, CacheMB: 100, PluginEnv: map[string]string{}, Theme: "system"}
}

// Missing/null is the legacy all-types setting; an explicit empty array means
// the user unchecked everything and must survive save/clone/relaunch as empty.
func (s Settings) EnabledDrives() []string {
	if s.Drives == nil {
		return catalog.DriveIDs()
	}
	return s.Drives
}

func (s Settings) AllowsDrive(id string) bool {
	return slices.Contains(s.EnabledDrives(), id)
}
func (s Settings) Clone() Settings {
	b, _ := json.Marshal(s)
	var out Settings
	_ = json.Unmarshal(b, &out)
	return out
}

var channelID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,63}$`)

func ParseChannels(text string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, v := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；' || unicode.IsSpace(r)
	}) {
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "t.me/") {
			v = "https://" + v
		}
		if strings.HasPrefix(v, "https://t.me/") || strings.HasPrefix(v, "http://t.me/") {
			u, e := url.Parse(v)
			if e != nil {
				return nil, e
			}
			v = strings.TrimPrefix(strings.Trim(u.Path, "/"), "s/")
		}
		v = strings.TrimPrefix(v, "@")
		if !channelID.MatchString(v) {
			return nil, fmt.Errorf("频道名无效：%s（请使用公开频道用户名）", v)
		}
		key := strings.ToLower(v)
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	if len(out) > 1000 {
		return nil, fmt.Errorf("最多配置 1000 个频道")
	}
	return out, nil
}
func (s Settings) Validate() error {
	return s.ValidateCatalog(catalog.Current())
}
func (s Settings) ValidateCatalog(c catalog.Catalog) error {
	if !slices.Contains([]string{"system", "light", "dark"}, s.Theme) {
		return fmt.Errorf("外观主题无效")
	}
	if s.Source != "all" && s.Source != "plugin" && s.Source != "tg" {
		return fmt.Errorf("搜索来源类型无效")
	}
	if s.Timeout < 5 || s.Timeout > 180 {
		return fmt.Errorf("搜索超时需为 5–180 秒")
	}
	if s.Concurrency < 1 || s.Concurrency > 64 {
		return fmt.Errorf("并发数需为 1–64")
	}
	if s.CacheMB < 20 || s.CacheMB > 2048 {
		return fmt.Errorf("缓存上限需为 20–2048 MB")
	}
	for _, id := range s.Drives {
		if !slices.Contains(catalog.DriveIDs(), id) {
			return fmt.Errorf("未知网盘类型：%s", id)
		}
	}
	for _, id := range s.Plugins {
		if !slices.Contains(c.PluginIDs(), id) {
			return fmt.Errorf("未知插件：%s", id)
		}
	}
	if _, err := ParseChannels(strings.Join(s.Channels, "\n")); err != nil {
		return err
	}
	if _, err := ParseChannels(strings.Join(s.CustomChannels, "\n")); err != nil {
		return err
	}
	if len(s.Extensions) > 64*1024 {
		return fmt.Errorf("插件扩展参数过长")
	}
	if len(s.Extensions) > 0 {
		var v map[string]any
		if json.Unmarshal(s.Extensions, &v) != nil || v == nil {
			return fmt.Errorf("插件扩展参数必须为 JSON 对象")
		}
	}
	allowed := c.AllowedEnv()
	for k, v := range s.PluginEnv {
		if !allowed[k] || ReservedEnv(k) {
			return fmt.Errorf("不支持的插件配置项：%s", k)
		}
		if strings.ContainsRune(v, 0) || len(v) > 16384 {
			return fmt.Errorf("插件配置 %s 无效或过长", k)
		}
	}
	if !slices.Contains([]string{ProxySystem, ProxyDirect, ProxyManual}, s.EffectiveProxyMode()) {
		return fmt.Errorf("网络代理模式无效")
	}
	if s.EffectiveProxyMode() == ProxyManual {
		u, e := url.Parse(s.Proxy)
		if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("代理需为有效的 HTTP、HTTPS 或 SOCKS5 地址")
		}
	}
	return nil
}
func ReservedEnv(k string) bool {
	return strings.HasPrefix(k, "PANREEL_") || strings.HasPrefix(k, "AUTH_") || slices.Contains([]string{"PORT", "CHANNELS", "ENABLED_PLUGINS", "CACHE_PATH", "HOME", "PATH", "GOTOOLCHAIN", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "PROXY", "INSECURE_SKIP_TLS_VERIFY"}, k)
}
func (s Settings) EffectiveSource() (string, error) {
	// Channel setup is required for every search mode, even though plugins
	// ship enabled. Keep this gate shared by the UI and the HTTP client.
	if len(s.Channels) == 0 {
		return "", fmt.Errorf("请先在「搜索来源 → TG 频道」添加并启用频道")
	}
	if len(s.EnabledDrives()) == 0 {
		return "", fmt.Errorf("请先在「搜索来源 → 网盘类型」勾选至少一种类型")
	}
	if s.Source == "plugin" && len(s.Plugins) == 0 {
		return "", fmt.Errorf("尚未选择搜索插件，请在「搜索来源」中启用插件")
	}
	if s.Source == "all" && len(s.Plugins) == 0 {
		return "tg", nil
	}
	return s.Source, nil
}
