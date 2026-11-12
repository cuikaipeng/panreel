package app

import (
	"embed"
	"encoding/json"
	"fmt"
	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/ui"
	"strings"
)

//go:embed locales/*.json
var localeFiles embed.FS

var dictionaries = func() map[string]map[string]string {
	all := map[string]map[string]string{}
	for _, lang := range []string{"en", "zh-Hans"} {
		data, err := localeFiles.ReadFile("locales/" + lang + ".json")
		if err != nil {
			panic(err)
		}
		var dict map[string]string
		if err = json.Unmarshal(data, &dict); err != nil {
			panic(err)
		}
		all[lang] = dict
	}
	return all
}()

func initializeLanguages() {
	// Our JSON dictionaries are independent of MyGo's catalog registry.
	// Without this declaration Locale() resolves every system to English.
	mygo.App.SetLanguages("en", "zh-Hans")
}

func (a *app) locale() string {
	lang := a.prefs.Language
	if lang == "system" {
		if a.window != nil {
			lang = mygo.App.Locale()
		} else {
			lang = "zh-Hans"
		}
	}
	if strings.HasPrefix(lang, "zh") {
		return "zh-Hans"
	}
	return "en"
}

func (a *app) tr(key string, args ...any) string {
	text := dictionaries[a.locale()][key]
	if text == "" {
		text = key
	}
	if len(args) > 0 {
		return fmt.Sprintf(text, args...)
	}
	return text
}

func (a *app) applyLocale() {
	if a.window == nil {
		return
	}
	a.setLocale()
	a.refreshLocale()
}
func (a *app) setLocale() {
	tag := a.prefs.Language
	if tag == "system" {
		tag = ""
	}
	mygo.App.SetLocale(tag)
}
func (a *app) refreshLocale() {
	mygo.App.SetMenu(a.applicationMenu())
	if a.settingsWin != nil {
		a.settingsWin.SetTitle(a.tr("Preferences"))
	}
	a.refreshViews()
}

// Translate only presentation; stored selections remain independent of locale.
func (a *app) selectValue(c *ui.Context, value *string, options []string) ui.Element {
	labels := make([]string, len(options))
	for i, k := range options {
		labels[i] = a.tr(k)
	}
	selected := a.tr(*value)
	e := enamelButton(c, ui.Select(c, &selected, labels), false).MinWidth(0).Height(30)
	if e.Changed() {
		for i, label := range labels {
			if label == selected {
				*value = options[i]
				break
			}
		}
	}
	return e
}

type message struct {
	key  string
	args []any
}

func msg(key string, args ...any) message { return message{key, args} }
func (m message) text(a *app) string      { return a.tr(m.key, m.args...) }
