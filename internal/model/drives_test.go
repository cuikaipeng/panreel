package model

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/cuikaipeng/panreel/internal/catalog"
)

func TestDriveSelectionDefaultsMigrationAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		data string
		want []string
	}{{`{}`, catalog.DriveIDs()}, {`{"drives":null}`, catalog.DriveIDs()}, {`{"drives":[]}`, []string{}}, {`{"drives":["quark","magnet"]}`, []string{"quark", "magnet"}}} {
		s := Defaults()
		if err := json.Unmarshal([]byte(tc.data), &s); err != nil {
			t.Fatal(err)
		}
		s.Channels = []string{"example_channel"}
		if !slices.Equal(s.EnabledDrives(), tc.want) || !slices.Equal(s.Clone().EnabledDrives(), tc.want) || s.Validate() != nil {
			t.Fatal("selection changed during migration or cloning", tc.data)
		}
		if _, err := s.EffectiveSource(); (err != nil) != (len(tc.want) == 0) {
			t.Fatal("empty selection did not block search", err)
		}
	}
	s := Defaults()
	s.Drives = []string{"not-a-drive"}
	if s.Validate() == nil {
		t.Fatal("unknown drive accepted")
	}
}
