package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
)

func TestDriveSelectionReachesSearchAndSnapshotsAndFiltersResponses(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Drives []string `json:"cloud_types"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || !slices.Equal(body.Drives, []string{"quark"}) {
			t.Error("selected types missing from request")
		}
		// Even an engine ignoring cloud_types must not leak deselected types
		// from the initial response or from a later cached snapshot.
		fmt.Fprint(w, `{"data":{"pending":false,"merged_by_type":{"quark":[{"url":"https://pan.quark.cn/s/keep"}],"baidu":[{"url":"https://pan.baidu.com/s/hide"}]}}}`)
	}))
	defer server.Close()
	s := model.Defaults()
	s.Channels, s.Drives = []string{"example_channel"}, []string{"quark"}
	client := NewClient(server.URL, "secret")
	var batches int
	err := client.searchUpdates(context.Background(), "test", s, false, time.Millisecond, func(items []model.Item) {
		batches++
		if len(items) != 1 || items[0].Drive != "quark" {
			t.Error("progress batch ignored drive selection")
		}
	})
	if err != nil || batches != 2 || calls.Load() != 2 {
		t.Fatal("progress flow failed", err)
	}
	s.Drives = []string{}
	if err = client.SearchUpdates(context.Background(), "test", s, false, func([]model.Item) { t.Error("empty selection emitted results") }); err == nil {
		t.Fatal("empty selection started a streaming search")
	}
	if calls.Load() != 2 {
		t.Fatal("empty selection reached engine")
	}
}
