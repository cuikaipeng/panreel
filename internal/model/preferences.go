package model

import (
	"slices"
)

const DefaultAccent = "MistBlue"

var AccentOptions = []string{DefaultAccent, "Teal", "Blue", "Violet", "Green", "Orange", "Red", "Pink"}

// Preferences belongs to the desktop, independently of the search engine.
type Preferences struct {
	Theme              string `json:"theme"`
	Accent             string `json:"accent"`
	Language           string `json:"language"`
	CheckEngineUpdates bool   `json:"check_engine_updates"`
	CheckUpdates       bool   `json:"check_updates"`
}

func DefaultPreferences() Preferences {
	return Preferences{Theme: "system", Accent: DefaultAccent, Language: "system", CheckUpdates: true, CheckEngineUpdates: true}
}

func (p *Preferences) Normalize() {
	if !slices.Contains([]string{"system", "light", "dark"}, p.Theme) {
		p.Theme = "system"
	}
	if !slices.Contains(AccentOptions, p.Accent) {
		p.Accent = DefaultAccent
	}
	if !slices.Contains([]string{"system", "en", "zh-Hans"}, p.Language) {
		p.Language = "system"
	}
}
