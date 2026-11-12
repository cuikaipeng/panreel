package catalog

import (
	_ "embed"
	"encoding/json"
	"sync/atomic"
)

//go:embed catalog.json
var raw []byte

type Plugin struct {
	ID  string   `json:"id"`
	Env []string `json:"env,omitempty"`
}
type Catalog struct {
	Revision string   `json:"revision"`
	Plugins  []Plugin `json:"plugins"`
}

var Bundled = func() Catalog {
	var c Catalog
	if err := json.Unmarshal(raw, &c); err != nil {
		panic(err)
	}
	return c
}()

var active atomic.Pointer[Catalog]

func Current() Catalog {
	if c := active.Load(); c != nil {
		return *c
	}
	return Bundled
}

// Catalogs are immutable once published, including their nested slices.
func SetCurrent(c Catalog) {
	b, _ := json.Marshal(c)
	var copy Catalog
	_ = json.Unmarshal(b, &copy)
	active.Store(&copy)
}

func PluginIDs() []string {
	return Current().PluginIDs()
}
func (c Catalog) PluginIDs() []string {
	out := make([]string, 0, len(c.Plugins))
	for _, p := range c.Plugins {
		out = append(out, p.ID)
	}
	return out
}
func (c Catalog) AllowedEnv() map[string]bool {
	out := map[string]bool{}
	for _, p := range c.Plugins {
		for _, v := range p.Env {
			out[v] = true
		}
	}
	return out
}

type Drive struct{ ID, Name string }

var Drives = []Drive{{"baidu", "百度网盘"}, {"aliyun", "阿里云盘"}, {"quark", "夸克网盘"}, {"guangya", "光鸭云盘"}, {"tianyi", "天翼云盘"}, {"uc", "UC 网盘"}, {"mobile", "移动云盘"}, {"115", "115 网盘"}, {"pikpak", "PikPak"}, {"xunlei", "迅雷网盘"}, {"123", "123 云盘"}, {"magnet", "磁力链接"}, {"ed2k", "电驴链接"}, {"others", "其他链接"}}

func DriveIDs() []string {
	ids := make([]string, len(Drives))
	for i, d := range Drives {
		ids[i] = d.ID
	}
	return ids
}

func DriveName(id string) string {
	for _, d := range Drives {
		if d.ID == id {
			return d.Name
		}
	}
	return id
}
