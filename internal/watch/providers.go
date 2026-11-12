package watch

import (
	"context"
	"crypto/rand"
	"hash/crc32"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
)

func file(o object, id, name, size, hash string, dir bool) entry {
	return entry{ResourceFile: model.ResourceFile{ID: o.text(id), Name: o.text(name), Size: o.number(size), Hash: o.text(hash)}, dir: dir}
}

func (c *Client) aliyun(ctx context.Context, u *url.URL, password string) ([]model.ResourceFile, error) {
	id := shareID(u)
	if id == "" {
		return nil, Protocol
	}
	headers := map[string]string{"Referer": "https://www.alipan.com/", "Origin": "https://www.alipan.com", "X-Canary": "client=web,app=share,version=v2.3.1"}
	token, err := c.request(ctx, http.MethodPost, "https://api.alipan.com/v2/share_link/get_share_token", nil, map[string]any{"share_id": id, "share_pwd": password}, headers)
	if err != nil {
		return nil, err
	}
	if token.text("code") != "" {
		return nil, issue(token.text("code"), token.text("message"))
	}
	if token.text("share_token") == "" {
		return nil, Protocol
	}
	headers["x-share-token"] = token.text("share_token")
	return walk(ctx, "root", func(parent string) ([]entry, error) {
		var out []entry
		marker := ""
		seen := map[string]bool{}
		for {
			r, err := c.request(ctx, http.MethodPost, "https://api.alipan.com/adrive/v3/file/list", nil, map[string]any{"share_id": id, "parent_file_id": parent, "limit": 100, "marker": marker, "order_by": "name", "order_direction": "ASC"}, headers)
			if err != nil {
				return nil, err
			}
			if r.text("code") != "" {
				return nil, issue(r.text("code"), r.text("message"))
			}
			items, err := r.list("items")
			if err != nil {
				return nil, err
			}
			for _, f := range items {
				if f.text("type") != "file" && f.text("type") != "folder" {
					return nil, Protocol
				}
				out = append(out, file(f, "file_id", "name", "size", "content_hash", f.text("type") == "folder"))
			}
			if _, ok := r["next_marker"]; !ok {
				return nil, Protocol
			}
			marker = r.text("next_marker")
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

func (c *Client) quark(ctx context.Context, u *url.URL, password, drive string) ([]model.ResourceFile, error) {
	id := shareID(u)
	if id == "" {
		return nil, Protocol
	}
	base, origin, pr := "https://drive-pc.quark.cn/1/clouddrive", "https://pan.quark.cn", "ucpro"
	if drive == "uc" {
		base = "https://pc-api.uc.cn/1/clouddrive"
		origin = "https://drive.uc.cn"
		pr = "UCBrowser"
	}
	headers := map[string]string{"Origin": origin, "Referer": origin + "/"}
	token, err := c.request(ctx, http.MethodPost, base+"/share/sharepage/token", url.Values{"pr": {pr}, "fr": {"pc"}}, map[string]any{"pwd_id": id, "passcode": password, "share_for_transfer": true}, headers)
	if err != nil {
		return nil, err
	}
	if err = quarkError(token); err != nil {
		return nil, err
	}
	stoken := token.obj("data").text("stoken")
	if stoken == "" {
		return nil, Protocol
	}
	return walk(ctx, "0", func(parent string) ([]entry, error) {
		var out []entry
		for page := 1; ; page++ {
			q := url.Values{"pr": {pr}, "fr": {"pc"}, "pwd_id": {id}, "stoken": {stoken}, "pdir_fid": {parent}, "force": {"0"}, "_page": {strconv.Itoa(page)}, "_size": {"100"}, "_sort": {"file_type:asc,file_name:asc"}, "_fetch_total": {"1"}}
			endpoint := base + "/share/sharepage/detail"
			if drive == "uc" {
				endpoint = base + "/transfer_share/detail"
				q.Set("fr", "h5")
				q.Set("passcode", password)
			}
			r, err := c.request(ctx, http.MethodGet, endpoint, q, nil, headers)
			if err != nil {
				return nil, err
			}
			if err = quarkError(r); err != nil {
				return nil, err
			}
			items, err := r.obj("data").list("list")
			if err != nil {
				return nil, err
			}
			for _, f := range items {
				if _, ok := f["file"]; !ok {
					return nil, Protocol
				}
				out = append(out, file(f, "fid", "file_name", "size", "md5", !f.boolean("file")))
			}
			_, hasTotal := r.obj("metadata")["_total"]
			if hasTotal && int64(len(out)) >= r.obj("metadata").number("_total") {
				return out, nil
			}
			if len(items) < 100 {
				if hasTotal {
					return nil, Incomplete
				}
				return out, nil
			}
		}
	})
}

func quarkError(r object) error {
	if _, ok := r["code"]; !ok {
		return Protocol
	}
	if r.number("code") == 41008 {
		return Password
	}
	if r.number("code") == 41004 || r.number("code") == 41010 || r.number("code") == 41011 {
		return Unavailable
	}
	if r.number("code") != 0 {
		return issue(r.text("code"), r.text("message"))
	}
	return nil
}

func (c *Client) baidu(ctx context.Context, u *url.URL, password string) ([]model.ResourceFile, error) {
	id := strings.TrimPrefix(shareID(u), "1")
	if id == "" {
		id = u.Query().Get("surl")
	}
	if id == "" {
		return nil, Protocol
	}
	headers := map[string]string{"Referer": "https://pan.baidu.com/s/1" + id}
	if password != "" {
		r, err := c.request(ctx, http.MethodPost, "https://pan.baidu.com/share/verify", url.Values{"surl": {id}}, url.Values{"pwd": {password}, "vcode": {""}, "vcode_str": {""}}, headers)
		if err != nil {
			return nil, err
		}
		if err = baiduError(r); err != nil {
			return nil, err
		}
		if r.text("randsk") == "" {
			return nil, Protocol
		}
		headers["Cookie"] = "BDCLND=" + url.QueryEscape(r.text("randsk"))
	}
	return walk(ctx, "", func(parent string) ([]entry, error) {
		var out []entry
		for page := 1; ; page++ {
			q := url.Values{"web": {"1"}, "page": {strconv.Itoa(page)}, "num": {"100"}, "order": {"name"}, "desc": {"0"}, "showempty": {"0"}, "shorturl": {id}, "clienttype": {"0"}}
			if parent == "" {
				q.Set("root", "1")
			} else {
				q.Set("dir", parent)
			}
			r, err := c.request(ctx, http.MethodGet, "https://pan.baidu.com/share/list", q, nil, headers)
			if err != nil {
				return nil, err
			}
			if err = baiduError(r); err != nil {
				return nil, err
			}
			items, err := r.list("list")
			if err != nil {
				return nil, err
			}
			for _, f := range items {
				if _, ok := f["isdir"]; !ok {
					return nil, Protocol
				}
				e := file(f, "fs_id", "server_filename", "size", "md5", f.boolean("isdir"))
				if e.dir {
					e.folder = f.text("path")
					if e.folder == "" {
						return nil, Protocol
					}
				}
				out = append(out, e)
			}
			if len(items) < 100 {
				return out, nil
			}
		}
	})
}

func baiduError(r object) error {
	if _, ok := r["errno"]; !ok {
		return Protocol
	}
	switch r.number("errno") {
	case 0:
		return nil
	case -9, -12:
		return Password
	case -7, 105, 115, 117, 145:
		return Unavailable
	case -62, -6, 2, 4:
		return Verification
	default:
		return issue(r.text("errno"), r.text("errmsg"))
	}
}

func (c *Client) pan115(ctx context.Context, u *url.URL, password string) ([]model.ResourceFile, error) {
	id := shareID(u)
	if id == "" {
		return nil, Protocol
	}
	if password == "" {
		return nil, Password
	}
	return walk(ctx, "0", func(parent string) ([]entry, error) {
		var out []entry
		for {
			q := url.Values{"share_code": {id}, "receive_code": {password}, "cid": {parent}, "limit": {"100"}, "offset": {strconv.Itoa(len(out))}}
			r, err := c.request(ctx, http.MethodGet, "https://115cdn.com/webapi/share/snap", q, nil, map[string]string{"Referer": "https://115cdn.com/", "X-Requested-With": "XMLHttpRequest"})
			if err != nil {
				return nil, err
			}
			if !r.boolean("state") {
				return nil, issue(r.text("errno"), r.text("error"))
			}
			d := r.obj("data")
			items, err := d.list("list")
			if err != nil {
				return nil, err
			}
			for _, f := range items {
				if _, ok := f["fc"]; !ok {
					return nil, Protocol
				}
				dir := f.number("fc") == 0
				key := "fid"
				if dir {
					key = "cid"
				}
				out = append(out, file(f, key, "n", "s", "sha", dir))
			}
			if _, ok := d["count"]; !ok {
				return nil, Protocol
			}
			if int64(len(out)) >= d.number("count") {
				return out, nil
			}
			if len(items) == 0 {
				return nil, Incomplete
			}
		}
	})
}

// The public 123 web API signs a request path and timestamp with CRC32. This
// is protocol framing, not an account credential. No login tokens are stored.
func sign123(q url.Values, endpoint string, now time.Time) error {
	random, err := rand.Int(rand.Reader, big.NewInt(10000000))
	if err != nil {
		return err
	}
	digits := now.In(time.FixedZone("UTC+8", 8*3600)).Format("200601021504")
	encoded := make([]byte, len(digits))
	for i, d := range []byte(digits) {
		encoded[i] = "adefghlmyi"[d-'0']
	}
	key := strconv.FormatUint(uint64(crc32.ChecksumIEEE(encoded)), 10)
	stamp := strconv.FormatInt(now.Unix(), 10)
	checksum := crc32.ChecksumIEEE([]byte(strings.Join([]string{stamp, random.String(), endpoint, "web", "3", key}, "|")))
	q.Set(key, stamp+"-"+random.String()+"-"+strconv.FormatUint(uint64(checksum), 10))
	return nil
}

func (c *Client) pan123(ctx context.Context, u *url.URL, password string) ([]model.ResourceFile, error) {
	id := strings.TrimSuffix(shareID(u), ".html")
	if id == "" {
		return nil, Protocol
	}
	return walk(ctx, "0", func(parent string) ([]entry, error) {
		var out []entry
		for page := 1; ; page++ {
			q := url.Values{"shareKey": {id}, "SharePwd": {password}, "parentFileId": {parent}, "limit": {"100"}, "next": {"0"}, "Page": {strconv.Itoa(page)}, "orderBy": {"file_id"}, "orderDirection": {"asc"}}
			if err := sign123(q, "/b/api/share/get", time.Now()); err != nil {
				return nil, err
			}
			r, err := c.request(ctx, http.MethodGet, "https://yun.123pan.com/b/api/share/get", q, nil, map[string]string{"Referer": "https://yun.123pan.com/", "Origin": "https://yun.123pan.com", "platform": "web", "app-version": "3"})
			if err != nil {
				return nil, err
			}
			if _, ok := r["code"]; !ok {
				return nil, Protocol
			}
			if r.number("code") != 0 {
				return nil, issue(r.text("code"), r.text("message"))
			}
			d := r.obj("data")
			items, err := d.list("InfoList")
			if err != nil {
				return nil, err
			}
			for _, f := range items {
				if _, ok := f["Type"]; !ok {
					return nil, Protocol
				}
				out = append(out, file(f, "FileId", "FileName", "Size", "Etag", f.number("Type") == 1))
			}
			if d.text("Next") == "-1" || len(items) == 0 {
				return out, nil
			}
			if _, ok := d["Next"]; !ok {
				return nil, Protocol
			}
		}
	})
}
