package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/engine"
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/storage"
	"github.com/egoist/mygo/ui"
)

func settingsTestApp(t *testing.T) *app {
	t.Helper()
	a := newApp()
	ctx, cancel := context.WithCancel(context.Background())
	a.ctx = ctx
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.store = store
	t.Cleanup(func() {
		cancel()
		a.stopSettingsSave()
		a.stopEngineApply()
		a.engineWork.Wait()
		if a.engine != nil {
			a.engine.Stop()
		}
		a.store.Close()
	})
	return a
}

func TestPreferencesAutoSaveWithoutSourcePageState(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		t.Run(lang, func(t *testing.T) {
			a := settingsTestApp(t)
			a.prefs.Language = lang
			a.prefsTab = preferencesEngine
			prefs := ui.NewTester(a.preferencesView, 760, 560)
			for _, key := range []string{model.ProxySystem, model.ProxyDirect} {
				if err := prefs.Click(a.tr(key)); err != nil {
					t.Fatal(err)
				}
			}
			stored, err := a.store.Settings()
			if err != nil || stored.EffectiveProxyMode() != model.ProxyDirect {
				t.Fatal("proxy selection did not save immediately", err)
			}
			a.timeoutText = "not a number"
			a.queueSetting(settingTimeout)
			a.flushSettings() // Same flush used when the settings window closes.
			prefs.Frame()
			field, ok := prefs.AccessByRole(ui.RoleTextField, a.tr("搜索超时（秒）"))
			if !ok || !field.Invalid || len(field.ErrorMessage) == 0 {
				t.Fatal("invalid input has no local error", field)
			}
			a.navigate("sources")
			sources := ui.NewTester(a.view, 960, 640)
			for _, view := range []*ui.Tester{prefs, sources} {
				for _, key := range []string{"撤销更改", "保存并应用", "有未保存的更改", "Unsaved changes", "Save your changes?"} {
					if _, ok := view.Access(a.tr(key)); ok {
						t.Fatalf("obsolete manual save UI remains: %s", key)
					}
				}
			}
			if sources.HasText(a.tr("请输入有效的超时秒数")) {
				t.Fatal("preference field error leaked into sources")
			}
			a.newChannels = "@Channel_One,channel_one，channel_two"
			a.addChannels()
			stored, err = a.store.Settings()
			if err != nil || len(stored.Channels) != 2 || stored.Timeout != 45 || stored.EffectiveProxyMode() != model.ProxyDirect {
				t.Fatal("invalid field blocked or corrupted an independent source edit", err)
			}
		})
	}
}

func TestIndependentFieldsKeepLastValidValuesAndFlushOnClose(t *testing.T) {
	a := settingsTestApp(t)
	a.draft.ProxyMode, a.draft.Proxy = model.ProxyManual, "http://"
	a.queueSetting(settingProxy)
	a.extensionsText = `{"token":`
	a.queueSetting(settingExtensions)
	a.concurrencyText = "65"
	a.queueSetting(settingConcurrency)
	a.timeoutText = "90"
	a.queueSetting(settingTimeout)
	a.draft.Include = "4K"
	a.queueSetting(settingInclude)
	if !a.flushSettings() {
		t.Fatal("invalid text blocked automatic saves")
	}
	stored, err := a.store.Settings()
	if err != nil || stored.Timeout != 90 || stored.Include != "4K" || stored.Concurrency != 16 || stored.EffectiveProxyMode() != model.ProxySystem || len(stored.Extensions) != 0 {
		t.Fatal("invalid values replaced working settings", err)
	}
	if len(a.settingErrors) != 3 || len(a.pendingSettings) != 0 || a.draft.Proxy != "http://" {
		t.Fatal("invalid drafts were discarded or left pending")
	}
	a.draft.Proxy = "http://127.0.0.1:7890"
	a.queueSetting(settingProxy)
	a.extensionsText = `{"token":"value"}`
	a.queueSetting(settingExtensions)
	a.concurrencyText = "24"
	a.queueSetting(settingConcurrency)
	a.flushSettings()
	stored, err = a.store.Settings()
	if err != nil || stored.Proxy != a.draft.Proxy || stored.EffectiveProxyMode() != model.ProxyManual || stored.Concurrency != 24 || string(stored.Extensions) != a.extensionsText || len(a.settingErrors) != 0 {
		t.Fatal("corrected input did not automatically commit", err)
	}
}

func TestAutoSaveFailureCanRetryWithoutApplyingUnsavedValues(t *testing.T) {
	a := settingsTestApp(t)
	dir := t.TempDir()
	a.store.Close()
	var err error
	a.store, err = storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store.Close()
	a.engine = &engine.Manager{}
	a.draft.ProxyMode = model.ProxyDirect
	if a.changeSetting(settingProxy) || a.settingsSaveError == "" || a.settings.EffectiveProxyMode() != model.ProxySystem || a.engineRestartPending {
		t.Fatal("failed save was silently applied")
	}
	a.store, err = storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !a.flushSettings() || a.settingsSaveError != "" || len(a.pendingSettings) != 0 || !a.engineRestartPending {
		t.Fatal("retry lost the pending edit")
	}
	stored, err := a.store.Settings()
	if err != nil || stored.EffectiveProxyMode() != model.ProxyDirect {
		t.Fatal("retry did not persist configuration", err)
	}
}

func TestAutoSaveDebounceRejectsStaleCallbacksAndFlushesBeforeExit(t *testing.T) {
	a := settingsTestApp(t)
	queue := make(chan func(), 10)
	a.dispatch = func(fn func()) { queue <- fn }
	a.timeoutText = "60"
	a.queueSetting(settingTimeout)
	var stale func()
	select {
	case stale = <-queue:
	case <-time.After(2 * time.Second):
		t.Fatal("auto-save timer did not run")
	}
	a.timeoutText = "75"
	a.queueSetting(settingTimeout)
	stale()
	if a.settings.Timeout != 45 {
		t.Fatal("superseded timer committed a value early")
	}
	select {
	case fn := <-queue:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("latest automatic save did not run")
	}
	if a.settings.Timeout != 75 {
		t.Fatal("latest value was not automatically saved")
	}
	a.timeoutText = "100"
	a.queueSetting(settingTimeout)
	a.flushSettings() // App quit and Preferences close must flush before the timer.
	stored, err := a.store.Settings()
	if err != nil || stored.Timeout != 100 || a.settingsSaveTimer != nil {
		t.Fatal("closing lost the last keystroke", err)
	}
}

func TestRequestOnlySettingsDoNotRestartEngine(t *testing.T) {
	a := settingsTestApp(t)
	a.engine = &engine.Manager{}
	a.online = true
	a.timeoutText, a.extensionsText, a.draft.Include, a.draft.Exclude = "90", `{"size":10}`, "4K", "trailer"
	for _, field := range []string{settingTimeout, settingExtensions, settingInclude, settingExclude} {
		a.changeSetting(field)
	}
	if a.engineRestartPending || !a.online {
		t.Fatal("request-only settings restarted the engine")
	}
	a.concurrencyText = "24"
	a.changeSetting(settingConcurrency)
	if !a.engineRestartPending || a.online {
		t.Fatal("engine configuration was not queued for automatic application")
	}
}

func TestRealEngineAppliesChangesDuringStartupAndCoalescesRestarts(t *testing.T) {
	if os.Getenv("PANREEL_ENGINE_TEST") != "1" {
		t.Skip("requires prepared desktop engine")
	}
	a := settingsTestApp(t)
	resources, err := filepath.Abs("../../resources")
	if err != nil {
		t.Fatal(err)
	}
	a.engine = &engine.Manager{Resources: resources, DataDir: t.TempDir()}
	queue := make(chan func(), 20)
	a.dispatch = func(fn func()) { queue <- fn }
	a.startEngine(false)
	// The initial startup already captured its settings; these must survive
	// its completion and be applied by one subsequent restart.
	a.draft.Channels, a.draft.CustomChannels = []string{"test_channel"}, []string{"test_channel"}
	a.changeSetting(settingChannels)
	a.draft.Plugins = catalog.PluginIDs()[:2]
	a.changeSetting(settingPlugins)
	untilReady := func() {
		t.Helper()
		deadline := time.NewTimer(30 * time.Second)
		defer deadline.Stop()
		for a.starting || a.engineRestartPending || !a.online {
			select {
			case fn := <-queue:
				fn()
				if a.err != "" {
					t.Fatal(a.err)
				}
			case <-deadline.C:
				t.Fatal("latest settings were never applied")
			}
		}
	}
	untilReady()
	h, err := a.engine.Client().Health(a.ctx)
	if err != nil || !slices.Equal(h.Channels, a.settings.Channels) || len(h.Plugins) != 2 {
		t.Fatal("startup overwrote a newer edit", err)
	}
	oldClient := a.engine.Client()
	a.draft.Channels = append(a.draft.Channels, "second_channel")
	a.changeSetting(settingChannels)
	var stale func()
	select {
	case stale = <-queue:
	case <-time.After(2 * time.Second):
		t.Fatal("engine apply timer did not run")
	}
	a.draft.Channels = append(a.draft.Channels, "third_channel")
	a.changeSetting(settingChannels)
	stale()
	if a.starting || a.engine.Client() != oldClient {
		t.Fatal("superseded timer restarted the engine")
	}
	untilReady()
	h, err = a.engine.Client().Health(a.ctx)
	if err != nil || len(h.Channels) != 3 || a.engineApplyTimer != nil {
		t.Fatal("grouped restart did not apply the latest channel list", err)
	}
}
