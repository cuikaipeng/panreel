package watch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"

	"github.com/cuikaipeng/panreel/internal/model"
)

func (c *Client) tianyi(ctx context.Context, u *url.URL, password string) ([]model.ResourceFile, error) {
	id := shareID(u)
	if id == "" {
		id = u.Query().Get("code")
	}
	if id == "" {
		return nil, Protocol
	}
	headers := map[string]string{"Referer": "https://cloud.189.cn/web/share?code=" + url.QueryEscape(id), "Sign-Type": "1", "Accept": "application/json;charset=UTF-8"}
	meta, err := c.request(ctx, http.MethodGet, "https://cloud.189.cn/api/open/share/getShareInfoByCodeV2.action", url.Values{"shareCode": {id}}, nil, headers)
	if err != nil {
		return nil, err
	}
	if err = tianyiError(meta); err != nil {
		return nil, err
	}
	if meta.boolean("needAccessCode") {
		if password == "" {
			return nil, Password
		}
		r, err := c.request(ctx, http.MethodGet, "https://cloud.189.cn/api/open/share/checkAccessCode.action", url.Values{"shareCode": {id}, "accessCode": {password}}, nil, headers)
		if err != nil {
			return nil, err
		}
		if err = tianyiError(r); err != nil {
			return nil, err
		}
		if r.text("shareId") != "" {
			meta["shareId"] = r["shareId"]
		}
	}
	if meta.text("shareId") == "" || meta.text("fileId") == "" {
		return nil, Protocol
	}
	if _, ok := meta["isFolder"]; !ok {
		return nil, Protocol
	}
	if !meta.boolean("isFolder") {
		f := file(meta, "fileId", "fileName", "fileSize", "md5", false)
		if f.Name == "" {
			return nil, Protocol
		}
		return []model.ResourceFile{f.ResourceFile}, nil
	}
	return walk(ctx, meta.text("fileId"), func(parent string) ([]entry, error) {
		var out []entry
		for page := 1; ; page++ {
			q := url.Values{"shareId": {meta.text("shareId")}, "fileId": {parent}, "shareDirFileId": {meta.text("fileId")}, "shareMode": {meta.text("shareMode")}, "isFolder": {"true"}, "accessCode": {password}, "pageNum": {strconv.Itoa(page)}, "pageSize": {"100"}, "iconOption": {"0"}, "orderBy": {"filename"}, "descending": {"false"}}
			r, err := c.request(ctx, http.MethodGet, "https://cloud.189.cn/api/open/share/listShareDir.action", q, nil, headers)
			if err != nil {
				return nil, err
			}
			if err = tianyiError(r); err != nil {
				return nil, err
			}
			d := r.obj("fileListAO")
			dirs, err := d.list("folderList")
			if err != nil {
				return nil, err
			}
			files, err := d.list("fileList")
			if err != nil {
				return nil, err
			}
			for _, f := range dirs {
				out = append(out, file(f, "id", "name", "size", "", true))
			}
			for _, f := range files {
				out = append(out, file(f, "id", "name", "size", "md5", false))
			}
			if len(dirs)+len(files) < 100 {
				return out, nil
			}
		}
	})
}

func tianyiError(r object) error {
	if _, ok := r["res_code"]; !ok {
		return Protocol
	}
	if r.text("res_code") == "0" {
		return nil
	}
	return issue(r.text("res_code"), r.text("res_message"))
}

func (c *Client) thunder(ctx context.Context, u *url.URL, password, drive string) ([]model.ResourceFile, error) {
	id := shareID(u)
	if id == "" {
		return nil, Protocol
	}
	base, origin, clientID := "https://api-pan.xunlei.com/drive/v1/share", "https://pan.xunlei.com", "ZUBzD9J_XPXfn7f7"
	if drive == "pikpak" {
		base = "https://api-drive.mypikpak.net/drive/v1/share"
		origin = "https://mypikpak.com"
		clientID = "YUMx5nI8ZU8Ap8pm"
	}
	device := make([]byte, 16)
	if _, err := rand.Read(device); err != nil {
		return nil, err
	}
	headers := map[string]string{"Origin": origin, "Referer": origin + "/", "X-Client-ID": clientID, "X-Device-ID": hex.EncodeToString(device)}
	q := url.Values{"share_id": {id}, "pass_code": {password}, "limit": {"100"}, "thumbnail_size": {"SIZE_SMALL"}}
	meta, err := c.request(ctx, http.MethodGet, base, q, nil, headers)
	if err != nil {
		return nil, err
	}
	if err = thunderError(meta); err != nil {
		return nil, err
	}
	token := meta.text("pass_code_token")
	return walk(ctx, "", func(parent string) ([]entry, error) {
		var out []entry
		marker := ""
		seen := map[string]bool{}
		for {
			query := url.Values{"share_id": {id}, "parent_id": {parent}, "pass_code_token": {token}, "page_token": {marker}, "limit": {"100"}, "thumbnail_size": {"SIZE_SMALL"}, "with_audit": {"true"}}
			r, err := c.request(ctx, http.MethodGet, base+"/detail", query, nil, headers)
			if err != nil {
				return nil, err
			}
			if err = thunderError(r); err != nil {
				return nil, err
			}
			items, err := r.list("files")
			if err != nil {
				return nil, err
			}
			for _, f := range items {
				kind := f.text("kind")
				if kind != "drive#folder" && kind != "drive#file" {
					return nil, Protocol
				}
				out = append(out, file(f, "id", "name", "size", "hash", kind == "drive#folder"))
			}
			if _, ok := r["next_page_token"]; !ok {
				return nil, Protocol
			}
			marker = r.text("next_page_token")
			if marker == "" {
				return out, nil
			}
			if seen[marker] || len(items) == 0 {
				return nil, Incomplete
			}
			seen[marker] = true
		}
	})
}

func thunderError(r object) error {
	if r.number("error_code") == 9 || r.number("error_code") == 10 {
		return Verification
	}
	if r.number("error_code") != 0 || r.text("error") != "" {
		return issue(r.text("error"), r.text("error_description"))
	}
	if r.text("share_status") != "OK" {
		return issue(r.text("share_status"), r.text("share_status_text"))
	}
	return nil
}

func (c *Client) guangya(ctx context.Context, u *url.URL, password string) ([]model.ResourceFile, error) {
	id := shareID(u)
	if id == "" {
		return nil, Protocol
	}
	headers := map[string]string{"Origin": "https://www.guangyapan.com", "Referer": "https://www.guangyapan.com/", "Dt": "4"}
	const base = "https://api.guangyapan.com/userres/v1/"
	token, err := c.request(ctx, http.MethodPost, base+"get_share_access_token", nil, map[string]any{"shareId": id, "code": password}, headers)
	if err != nil {
		return nil, err
	}
	if err = guangyaError(token); err != nil {
		return nil, err
	}
	access := token.obj("data").text("accessToken")
	if access == "" {
		access = token.text("access_token")
	}
	if access == "" {
		return nil, Protocol
	}
	return walk(ctx, "", func(parent string) ([]entry, error) {
		var out []entry
		cursor := int64(0)
		seen := map[int64]bool{}
		for {
			body := map[string]any{"accessToken": access, "parentId": parent, "pageSize": 100, "orderBy": 0, "sortType": 0}
			if cursor != 0 {
				body["cursor"] = cursor
			}
			r, err := c.request(ctx, http.MethodPost, base+"get_share_page_files_list", nil, body, headers)
			if err != nil {
				return nil, err
			}
			if err = guangyaError(r); err != nil {
				return nil, err
			}
			d := r.obj("data")
			items, err := d.list("list")
			if err != nil {
				return nil, err
			}
			for _, f := range items {
				if _, ok := f["resType"]; !ok {
					return nil, Protocol
				}
				out = append(out, file(f, "fileId", "fileName", "fileSize", "md5", f.number("resType") == 2))
			}
			if len(items) == 0 || string(d["hasMore"]) == "false" {
				return out, nil
			}
			cursor = d.number("cursor")
			if cursor <= 0 {
				return out, nil
			}
			if seen[cursor] {
				return nil, Incomplete
			}
			seen[cursor] = true
		}
	})
}

func guangyaError(r object) error {
	if r.number("code") == 110 || r.number("code") == 117 || r.number("code") == 118 {
		return Verification
	}
	if r.number("code") != 0 {
		return issue(r.text("code"), r.text("msg"))
	}
	if _, ok := r["data"]; !ok && r.text("access_token") == "" {
		return Protocol
	}
	return nil
}
