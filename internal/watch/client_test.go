package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cuikaipeng/panreel/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(value any) *http.Response {
	raw, _ := json.Marshal(value)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}
}

// Fixtures exercise each wire format, directory recursion, extraction-code
// propagation and full-width numeric identifiers without contacting accounts.
func TestAllDriveAdaptersReadNestedFiles(t *testing.T) {
	cases := []struct{ drive, link string }{
		{"aliyun", "https://www.alipan.com/s/demo"}, {"quark", "https://pan.quark.cn/s/demo"},
		{"uc", "https://drive.uc.cn/s/demo"}, {"baidu", "https://pan.baidu.com/s/1demo"},
		{"115", "https://115.com/s/demo"}, {"123", "https://www.123pan.com/s/demo.html"},
		{"tianyi", "https://cloud.189.cn/t/demo"}, {"xunlei", "https://pan.xunlei.com/s/demo"},
		{"pikpak", "https://mypikpak.com/s/demo"}, {"guangya", "https://www.guangyapan.com/s/demo"},
		{"mobile", "https://yun.139.com/shareweb/#/w/i/demo"},
	}
	for _, tc := range cases {
		t.Run(tc.drive, func(t *testing.T) {
			calls := 0
			const largeID = "1234567890123456789"
			client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				q := req.URL.Query()
				endpoint := req.URL.Path
				var b object
				if req.Method == http.MethodPost && tc.drive != "mobile" && tc.drive != "baidu" {
					_ = json.NewDecoder(req.Body).Decode(&b)
				}
				folderName, fileName := "Season 1", "Episode 1.mkv"
				switch tc.drive {
				case "aliyun":
					if strings.HasSuffix(endpoint, "get_share_token") {
						if b.text("share_pwd") != "1234" || b.text("share_id") != "demo" {
							t.Error("missing Aliyun share credentials")
						}
						return response(map[string]any{"share_token": "token"}), nil
					}
					if req.Header.Get("x-share-token") != "token" {
						t.Error("missing share token")
					}
					f := map[string]any{"file_id": "season", "name": folderName, "type": "folder"}
					if b.text("parent_file_id") == "season" {
						f = map[string]any{"file_id": largeID, "name": fileName, "type": "file", "size": 100}
					}
					return response(map[string]any{"items": []any{f}, "next_marker": ""}), nil
				case "quark", "uc":
					if strings.HasSuffix(endpoint, "/token") {
						if b.text("passcode") != "1234" {
							t.Error("missing quark/UC extraction code")
						}
						return response(map[string]any{"code": 0, "data": map[string]any{"stoken": "token"}}), nil
					}
					if tc.drive == "uc" && endpoint != "/1/clouddrive/transfer_share/detail" {
						t.Error("wrong UC directory endpoint", endpoint)
					}
					if q.Get("stoken") != "token" {
						t.Error("missing share token")
					}
					f := map[string]any{"fid": "season", "file_name": folderName, "file": false}
					if q.Get("pdir_fid") == "season" {
						f = map[string]any{"fid": largeID, "file_name": fileName, "file": true, "size": 100}
					}
					return response(map[string]any{"code": 0, "data": map[string]any{"list": []any{f}}, "metadata": map[string]any{"_total": 1}}), nil
				case "baidu":
					if endpoint == "/share/verify" {
						_ = req.ParseForm()
						if req.Form.Get("pwd") != "1234" {
							t.Error("missing Baidu extraction code")
						}
						return response(map[string]any{"errno": 0, "randsk": "session"}), nil
					}
					if req.Header.Get("Cookie") != "BDCLND=session" {
						t.Error("missing Baidu verification cookie")
					}
					f := map[string]any{"fs_id": 1, "server_filename": folderName, "isdir": 1, "path": "/Season 1"}
					if q.Get("dir") == "/Season 1" {
						f = map[string]any{"fs_id": json.Number(largeID), "server_filename": fileName, "isdir": 0, "size": 100}
					}
					return response(map[string]any{"errno": 0, "list": []any{f}}), nil
				case "115":
					if q.Get("receive_code") != "1234" {
						t.Error("missing 115 extraction code")
					}
					f := map[string]any{"cid": "season", "n": folderName, "fc": 0}
					if q.Get("cid") == "season" {
						f = map[string]any{"fid": largeID, "n": fileName, "fc": 1, "s": "100"}
					}
					return response(map[string]any{"state": true, "errno": 0, "data": map[string]any{"list": []any{f}, "count": 1}}), nil
				case "123":
					if q.Get("shareKey") != "demo" || q.Get("SharePwd") != "1234" {
						t.Error("wrong 123 share credentials")
					}
					f := map[string]any{"FileId": 1, "FileName": folderName, "Type": 1}
					if q.Get("parentFileId") == "1" {
						f = map[string]any{"FileId": json.Number(largeID), "FileName": fileName, "Type": 0, "Size": 100}
					}
					return response(map[string]any{"code": 0, "data": map[string]any{"InfoList": []any{f}, "Next": "-1"}}), nil
				case "tianyi":
					if strings.HasSuffix(endpoint, "getShareInfoByCodeV2.action") {
						return response(map[string]any{"res_code": 0, "shareId": json.Number(largeID), "fileId": "root", "isFolder": true, "shareMode": 1, "needAccessCode": 1}), nil
					}
					if strings.HasSuffix(endpoint, "checkAccessCode.action") {
						if q.Get("accessCode") != "1234" {
							t.Error("missing Tianyi extraction code")
						}
						return response(map[string]any{"res_code": 0, "shareId": json.Number(largeID)}), nil
					}
					if q.Get("shareId") != largeID {
						t.Error("Tianyi numeric ID lost precision")
					}
					dirs, files := []any{map[string]any{"id": "season", "name": folderName}}, []any{}
					if q.Get("fileId") == "season" {
						dirs = []any{}
						files = []any{map[string]any{"id": json.Number(largeID), "name": fileName, "size": 100}}
					}
					return response(map[string]any{"res_code": 0, "fileListAO": map[string]any{"folderList": dirs, "fileList": files}}), nil
				case "xunlei", "pikpak":
					if strings.HasSuffix(endpoint, "/share") {
						if q.Get("pass_code") != "1234" {
							t.Error("missing extraction code")
						}
						return response(map[string]any{"share_status": "OK", "pass_code_token": "token"}), nil
					}
					if q.Get("pass_code_token") != "token" {
						t.Error("missing pass code token")
					}
					f := map[string]any{"id": "season", "name": folderName, "kind": "drive#folder"}
					if q.Get("parent_id") == "season" {
						f = map[string]any{"id": largeID, "name": fileName, "kind": "drive#file", "size": "100"}
					}
					return response(map[string]any{"share_status": "OK", "files": []any{f}, "next_page_token": ""}), nil
				case "guangya":
					if strings.HasSuffix(endpoint, "get_share_access_token") {
						if b.text("code") != "1234" {
							t.Error("missing extraction code")
						}
						return response(map[string]any{"code": 0, "data": map[string]any{"accessToken": "token"}}), nil
					}
					if b.text("accessToken") != "token" {
						t.Error("missing access token")
					}
					f := map[string]any{"fileId": "season", "fileName": folderName, "resType": 2}
					if b.text("parentId") == "season" {
						f = map[string]any{"fileId": largeID, "fileName": fileName, "resType": 1, "fileSize": 100}
					}
					return response(map[string]any{"code": 0, "data": map[string]any{"list": []any{f}, "hasMore": false}}), nil
				case "mobile":
					raw, _ := io.ReadAll(req.Body)
					plain, err := openMobile(raw)
					if err != nil {
						t.Fatal(err)
					}
					_ = json.Unmarshal(plain, &b)
					input := b.obj("getOutLinkInfoReq")
					if input.text("passwd") != "1234" || input.text("linkID") != "demo" {
						t.Error("missing mobile extraction code")
					}
					dirs, files := []any{map[string]any{"caID": "season", "caName": folderName}}, []any{}
					if input.text("pCaID") == "season" {
						dirs = []any{}
						files = []any{map[string]any{"coID": largeID, "coName": fileName, "coSize": 100}}
					}
					encoded, err := sealMobile(map[string]any{"resultCode": "0", "data": map[string]any{"caLst": dirs, "coLst": files}})
					if err != nil {
						t.Fatal(err)
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(encoded)), Header: make(http.Header)}, nil
				}
				return nil, fmt.Errorf("unhandled fixture %s", tc.drive)
			})}}
			files, err := client.Snapshot(context.Background(), model.Item{Drive: tc.drive, URL: tc.link, Password: "1234"})
			if err != nil || len(files) != 1 {
				t.Fatalf("files=%+v err=%v calls=%d", files, err, calls)
			}
			if files[0].ID != largeID || files[0].Size != 100 || files[0].Name != "Season 1/Episode 1.mkv" {
				t.Fatal(files)
			}
		})
	}
}

func TestIncompletePaginationAndMalformedResponsesFailClosed(t *testing.T) {
	for _, scenario := range []string{"marker-loop", "missing-list", "captcha", "nested-failure"} {
		t.Run(scenario, func(t *testing.T) {
			client := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "get_share_token") {
					return response(map[string]any{"share_token": "token"}), nil
				}
				var b object
				_ = json.NewDecoder(req.Body).Decode(&b)
				switch scenario {
				case "missing-list":
					return response(map[string]any{"next_marker": ""}), nil
				case "captcha":
					r := response(map[string]any{})
					r.StatusCode = 403
					return r, nil
				case "nested-failure":
					if b.text("parent_file_id") == "root" {
						return response(map[string]any{"items": []any{map[string]any{"file_id": "folder", "name": "Season", "type": "folder"}, map[string]any{"file_id": "ep1", "name": "one.mkv", "type": "file", "size": 1}}, "next_marker": ""}), nil
					}
					return nil, errors.New("network down")
				default:
					return response(map[string]any{"items": []any{map[string]any{"file_id": "ep1", "name": "one.mkv", "type": "file", "size": 1}}, "next_marker": "same"}), nil
				}
			})}}
			files, err := client.Snapshot(context.Background(), model.Item{Drive: "aliyun", URL: "https://www.alipan.com/s/demo"})
			if err == nil || len(files) > 0 {
				t.Fatal("partial/unreadable listing accepted", files, err)
			}
		})
	}
}

func TestShareScansRefuseRedirectsAndUnsupportedLinks(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++; w.Write([]byte(`{"items":[]}`)) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	c := &Client{HTTP: redirect.Client()}
	_, err := c.request(context.Background(), http.MethodPost, redirect.URL, nil, map[string]string{"password": "private"}, nil)
	if !errors.Is(err, Verification) || targetCalls != 0 {
		t.Fatal("followed redirect with credentials", err, targetCalls)
	}
	for _, item := range []model.Item{{Drive: "magnet", URL: "magnet:?xt=urn:btih:demo"}, {Drive: "others", URL: "http://localhost/secret"}, {Drive: "quark", URL: "https://example.org/s/id"}} {
		_, err = c.Snapshot(context.Background(), item)
		if err == nil {
			t.Fatal("invalid/unsupported URL accepted", item)
		}
	}
	for _, raw := range [][]byte{nil, []byte("not-base64"), []byte(`"c2hvcnQ="`)} {
		if _, err := openMobile(raw); err == nil {
			t.Fatal("malformed encrypted response accepted")
		}
	}
}
