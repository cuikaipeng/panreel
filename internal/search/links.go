package search

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var hosts = map[string][]string{
	"baidu": {"pan.baidu.com", "yun.baidu.com"}, "aliyun": {"www.alipan.com", "alipan.com", "www.aliyundrive.com", "aliyundrive.com"},
	"quark": {"pan.quark.cn"}, "guangya": {"guangyapan.com", "www.guangyapan.com"}, "tianyi": {"cloud.189.cn"}, "uc": {"drive.uc.cn"},
	"mobile": {"caiyun.139.com", "yun.139.com", "www.yun.139.com", "www.caiyun.139.com", "caiyun.feixin.10086.cn"},
	"115":    {"115.com", "115cdn.com", "anxia.com", "www.115.com"}, "pikpak": {"mypikpak.com", "www.mypikpak.com"}, "xunlei": {"pan.xunlei.com"},
	"123": {"123pan.com", "123pan.cn", "123684.com", "123685.com", "123865.com", "123912.com", "123592.com", "www.123pan.com", "www.123pan.cn", "www.123684.com", "www.123685.com", "www.123865.com", "www.123912.com", "www.123592.com"},
}
var ed2k = regexp.MustCompile(`(?i)^ed2k://\|file\|[^|\r\n]+\|[0-9]+\|[a-f0-9]{32}\|/(?:\|[^\r\n]*)?$`)

func NormalizeLink(raw, drive string) (link, id, password string, err error) {
	raw = strings.TrimSpace(raw)
	bad := func() (string, string, string, error) { return "", "", "", fmt.Errorf("不支持的分享链接") }
	if len(raw) > 16384 || strings.ContainsAny(raw, "\x00\r\n") {
		return bad()
	}
	if drive == "ed2k" {
		if !ed2k.MatchString(raw) {
			return bad()
		}
		return raw, hash(drive + raw), "", nil
	}
	u, e := url.Parse(raw)
	if e != nil || u.User != nil {
		return bad()
	}
	if drive == "magnet" {
		if u.Scheme != "magnet" {
			return bad()
		}
		xt := u.Query().Get("xt")
		if !strings.HasPrefix(xt, "urn:btih:") && !strings.HasPrefix(xt, "urn:btmh:") {
			return bad()
		}
		return raw, hash(drive + strings.ToLower(xt)), "", nil
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.Port() != "" {
		return bad()
	}
	host := strings.ToLower(u.Hostname())
	if drive == "others" {
		if net.ParseIP(host) != nil || !strings.Contains(host, ".") || host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") {
			return bad()
		}
	} else {
		found := false
		for _, h := range hosts[drive] {
			if host == h {
				found = true
				break
			}
		}
		if !found || u.Path == "" || u.Path == "/" {
			return bad()
		}
	}
	u.Host = host
	q := u.Query()
	for _, key := range []string{"pwd", "password", "passcode", "accessCode"} {
		if p := q.Get(key); p != "" {
			password = p
			break
		}
	}
	for key := range q {
		if strings.HasPrefix(strings.ToLower(key), "utm_") {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	link = u.String()
	for _, key := range []string{"pwd", "password", "passcode", "accessCode"} {
		q.Del(key)
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	if drive != "others" {
		u.Host = drive
	}
	u.Scheme = "https"
	return link, hash(u.String()), password, nil
}
func hash(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:16]) }
func CanCheck(drive string) bool {
	switch drive {
	case "baidu", "aliyun", "quark", "tianyi", "uc", "mobile", "115", "xunlei", "123":
		return true
	}
	return false
}
