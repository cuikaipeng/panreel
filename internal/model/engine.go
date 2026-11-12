package model

import (
	"github.com/cuikaipeng/panreel/internal/catalog"
	"slices"
)

// ReconcileEngine preserves selections, enables new plugins for users who had
// all plugins enabled, and drops options no longer supported by this engine.
func ReconcileEngine(s Settings, previous, next catalog.Catalog) Settings {
	s = s.Clone()
	all := len(s.Plugins) > 0
	for _, id := range previous.PluginIDs() {
		all = all && slices.Contains(s.Plugins, id)
	}
	ids := next.PluginIDs()
	if all {
		s.Plugins = ids
	} else {
		s.Plugins = slices.DeleteFunc(s.Plugins, func(id string) bool { return !slices.Contains(ids, id) })
	}
	allowed := next.AllowedEnv()
	for key := range s.PluginEnv {
		if !allowed[key] || ReservedEnv(key) {
			delete(s.PluginEnv, key)
		}
	}
	return s
}
