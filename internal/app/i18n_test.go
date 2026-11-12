package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"slices"
	"testing"
)

func TestSystemChineseIsDeclaredAndMatched(t *testing.T) {
	initializeLanguages()
	if !slices.Equal(mygo.App.Languages(), []string{"en", "zh-Hans"}) {
		t.Fatal("MyGo cannot resolve our independent translations")
	}
	for _, tag := range []string{"zh-CN", "zh-Hans-CN", "zh-Hans"} {
		if got := i18n.Match([]string{tag}, mygo.App.Languages()); got != "zh-Hans" {
			t.Fatal(tag, got)
		}
	}
}
