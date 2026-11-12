package app

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/egoist/mygo/ui"
)

const (
	settingProxy       = "proxy"
	settingTimeout     = "timeout"
	settingConcurrency = "concurrency"
	settingCache       = "cache"
	settingInclude     = "include"
	settingExclude     = "exclude"
	settingExtensions  = "extensions"
	settingChannels    = "channels"
	settingPlugins     = "plugins"
	settingDrives      = "drives"
	settingsSaveDelay  = 400 * time.Millisecond
	engineApplyDelay   = 600 * time.Millisecond
)

// Text may temporarily be incomplete. Keep it in the editor, but only commit
// valid fields, independently of edits in another field or window.
func (a *app) queueSetting(field string) {
	a.markSetting(field)
	a.stopSettingsSave()
	if a.window == nil && a.dispatch == nil {
		return // Headless callers can flush explicitly, without background UI writes.
	}
	version := a.settingsSaveVersion
	a.settingsSaveTimer = time.AfterFunc(settingsSaveDelay, func() {
		a.update(func() {
			if version == a.settingsSaveVersion {
				a.flushSettings()
			}
		})
	})
}

func (a *app) markSetting(field string) {
	if a.pendingSettings == nil {
		a.pendingSettings = map[string]bool{}
		a.settingErrors = map[string]string{}
	}
	a.pendingSettings[field] = true
	delete(a.settingErrors, field)
}

func (a *app) changeSetting(field string) bool {
	a.markSetting(field)
	return a.flushSettings()
}

func (a *app) stopSettingsSave() {
	a.settingsSaveVersion++
	if a.settingsSaveTimer != nil {
		a.settingsSaveTimer.Stop()
		a.settingsSaveTimer = nil
	}
}

func (a *app) settingValue(s model.Settings, field string) (model.Settings, error) {
	s = s.Clone()
	var err error
	switch field {
	case settingProxy:
		s.ProxyMode, s.Proxy = a.draft.ProxyMode, strings.TrimSpace(a.draft.Proxy)
	case settingTimeout:
		s.Timeout, err = strconv.Atoi(strings.TrimSpace(a.timeoutText))
		if err != nil {
			return s, fmt.Errorf("请输入有效的超时秒数")
		}
	case settingConcurrency:
		s.Concurrency, err = strconv.Atoi(strings.TrimSpace(a.concurrencyText))
		if err != nil {
			return s, fmt.Errorf("请输入有效的并发数")
		}
	case settingCache:
		s.CacheMB, err = strconv.Atoi(strings.TrimSpace(a.cacheText))
		if err != nil {
			return s, fmt.Errorf("请输入有效的缓存上限")
		}
	case settingInclude:
		s.Include = a.draft.Include
	case settingExclude:
		s.Exclude = a.draft.Exclude
	case settingExtensions:
		s.Extensions = json.RawMessage(strings.TrimSpace(a.extensionsText))
	case settingChannels:
		s.Channels, s.CustomChannels = slices.Clone(a.draft.Channels), slices.Clone(a.draft.CustomChannels)
	case settingPlugins:
		s.Plugins = slices.Clone(a.draft.Plugins)
	case settingDrives:
		s.Drives = slices.Clone(a.draft.EnabledDrives())
	default:
		key, ok := strings.CutPrefix(field, "plugin:")
		if !ok {
			return s, fmt.Errorf("unknown setting")
		}
		if s.PluginEnv == nil {
			s.PluginEnv = map[string]string{}
		}
		s.PluginEnv[key] = a.draft.PluginEnv[key]
	}
	return s, s.Validate()
}

// Returns false only when a valid edit could not be persisted. Invalid text
// stays beside its own error; it never blocks unrelated source/settings edits.
func (a *app) flushSettings() bool {
	a.stopSettingsSave()
	if a.engineUpdating {
		return len(a.pendingSettings) == 0
	}
	next := a.settings.Clone()
	var valid []string
	for _, field := range slices.Sorted(maps.Keys(a.pendingSettings)) {
		candidate, err := a.settingValue(next, field)
		if err != nil {
			a.settingErrors[field] = err.Error()
			delete(a.pendingSettings, field)
			continue
		}
		delete(a.settingErrors, field)
		valid = append(valid, field)
		next = candidate
	}
	if (!reflect.DeepEqual(next, a.settings) || a.settingsNeedWrite) && a.store != nil {
		if err := a.store.SaveSettings(next); err != nil {
			a.settingsSaveError = err.Error()
			a.refreshViews()
			return false
		}
	}
	for _, field := range valid {
		delete(a.pendingSettings, field)
	}
	a.settingsSaveError = ""
	a.settingsNeedWrite = false
	restart := engineSettingsChanged(a.settings, next)
	if !slices.Equal(a.settings.EnabledDrives(), next.EnabledDrives()) && a.page != "saved" {
		if a.loading {
			a.stopSearch()
		}
		a.searchFilters.drive = "全部网盘"
		a.selected = -1
		a.cancelLinkCheck()
	}
	a.settings = next
	if restart && a.engine != nil {
		a.engineRestartPending = true
		a.online = false
		a.scheduleEngineApply()
	}
	a.refreshViews()
	return true
}

func engineSettingsChanged(before, after model.Settings) bool {
	return before.EffectiveProxyMode() != after.EffectiveProxyMode() ||
		(after.EffectiveProxyMode() == model.ProxyManual && before.Proxy != after.Proxy) ||
		before.Concurrency != after.Concurrency || before.CacheMB != after.CacheMB ||
		!slices.Equal(before.Channels, after.Channels) || !slices.Equal(before.Plugins, after.Plugins) ||
		!maps.Equal(before.PluginEnv, after.PluginEnv)
}

func (a *app) stopEngineApply() {
	a.engineApplyVersion++
	if a.engineApplyTimer != nil {
		a.engineApplyTimer.Stop()
		a.engineApplyTimer = nil
	}
}

func (a *app) scheduleEngineApply() {
	a.stopEngineApply()
	if a.closed || !a.engineRestartPending || a.engine == nil || a.starting || a.engineUpdating {
		return
	}
	if a.window == nil && a.dispatch == nil {
		return
	}
	version := a.engineApplyVersion
	a.engineApplyTimer = time.AfterFunc(engineApplyDelay, func() {
		a.update(func() {
			if version == a.engineApplyVersion && a.engineRestartPending {
				a.startEngine(true)
			}
		})
	})
}

func (a *app) settingsErrorView(c *ui.Context) {
	if a.settingsSaveError == "" {
		return
	}
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.tr("设置保存失败：%s", a.settingsSaveError)).TextColor(c.Theme().Danger).Wrap().Grow(1).Role(ui.RoleAlert)
		button(c, a.tr("Retry")).OnClick(func() { a.flushSettings() })
	})
}

func (a *app) settingError(c *ui.Context, input ui.Element, field string) {
	if key := a.settingErrors[field]; key != "" {
		input.Invalid(true).ErrorMessage(ui.Text(c, a.tr(key)).FontSize(12).TextColor(c.Theme().Danger).Wrap().Role(ui.RoleAlert))
	}
}
