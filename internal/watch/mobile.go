package watch

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cuikaipeng/panreel/internal/model"
)

// Public share protocol key, also used by PanSou's mobile link checker. This
// frames anonymous directory requests; it is not a user/login credential.
const mobileWireKey = "PVGDwmcvfs1uV3d1"

func sealMobile(payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	padding := aes.BlockSize - len(raw)%aes.BlockSize
	raw = append(raw, bytes.Repeat([]byte{byte(padding)}, padding)...)
	buf := make([]byte, aes.BlockSize+len(raw))
	if _, err = rand.Read(buf[:aes.BlockSize]); err != nil {
		return "", err
	}
	block, err := aes.NewCipher([]byte(mobileWireKey))
	if err != nil {
		return "", err
	}
	cipher.NewCBCEncrypter(block, buf[:aes.BlockSize]).CryptBlocks(buf[aes.BlockSize:], raw)
	return base64.StdEncoding.EncodeToString(buf), nil
}

func openMobile(raw []byte) ([]byte, error) {
	encoded := strings.TrimSpace(string(raw))
	if strings.HasPrefix(encoded, "\"") {
		if json.Unmarshal(raw, &encoded) != nil {
			return nil, Protocol
		}
	}
	buf, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(buf) < 2*aes.BlockSize || len(buf)%aes.BlockSize != 0 {
		return nil, Protocol
	}
	block, err := aes.NewCipher([]byte(mobileWireKey))
	if err != nil {
		return nil, Protocol
	}
	plain := make([]byte, len(buf)-aes.BlockSize)
	cipher.NewCBCDecrypter(block, buf[:aes.BlockSize]).CryptBlocks(plain, buf[aes.BlockSize:])
	padding := int(plain[len(plain)-1])
	if padding < 1 || padding > aes.BlockSize || !bytes.Equal(plain[len(plain)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) {
		return nil, Protocol
	}
	plain = plain[:len(plain)-padding]
	if bytes.HasPrefix(plain, []byte{0x1f, 0x8b}) {
		reader, err := gzip.NewReader(bytes.NewReader(plain))
		if err != nil {
			return nil, Protocol
		}
		defer reader.Close()
		plain, err = io.ReadAll(io.LimitReader(reader, (8<<20)+1))
		if err != nil {
			return nil, Protocol
		}
		if len(plain) > 8<<20 {
			return nil, Incomplete
		}
	}
	return plain, nil
}

func (c *Client) mobile(ctx context.Context, u *url.URL, password string) ([]model.ResourceFile, error) {
	id := ""
	route := u.Path
	if u.Fragment != "" {
		route = strings.SplitN(u.Fragment, "?", 2)[0]
	}
	parts := strings.Split(strings.Trim(route, "/"), "/")
	for i, p := range parts {
		if p == "i" && i+1 < len(parts) {
			id = parts[i+1]
			break
		}
	}
	if id == "" && strings.HasSuffix(u.Path, "/m/i") {
		id = strings.SplitN(u.RawQuery, "&", 2)[0]
	}
	if id == "" && u.Hostname() == "caiyun.feixin.10086.cn" && len(parts) == 1 {
		id = parts[0]
	}
	if id == "" {
		return nil, Protocol
	}
	return walk(ctx, "root", func(parent string) ([]entry, error) {
		var out []entry
		for start := 1; ; start += 100 {
			payload := map[string]any{"getOutLinkInfoReq": map[string]any{"account": "", "linkID": id, "passwd": password, "pCaID": parent, "bNum": start, "eNum": start + 99, "caSrt": 1, "coSrt": 1, "srtDr": 0}, "commonAccountInfo": map[string]any{"account": "", "accountType": 1}}
			body, err := sealMobile(payload)
			if err != nil {
				return nil, err
			}
			r, err := c.request(ctx, http.MethodPost, "https://share-kd-njs.yun.139.com/yun-share/richlifeApp/devapp/IOutLink/getOutLinkInfoV6", nil, body, map[string]string{"hcy-cool-flag": "1", "Origin": "https://yun.139.com", "Referer": "https://yun.139.com/", "X-Deviceinfo": "||3|12.27.0|chrome|131.0.0.0|||windows 10|1536X695|zh-CN|||"})
			if err != nil {
				return nil, err
			}
			if r.text("resultCode") != "0" {
				return nil, issue(r.text("resultCode"), r.text("desc"))
			}
			d := r.obj("data")
			dirs, err := d.list("caLst")
			if err != nil {
				return nil, err
			}
			files, err := d.list("coLst")
			if err != nil {
				return nil, err
			}
			for _, f := range dirs {
				out = append(out, file(f, "caID", "caName", "", "", true))
			}
			for _, f := range files {
				out = append(out, file(f, "coID", "coName", "coSize", "md5", false))
			}
			if len(dirs)+len(files) < 100 {
				return out, nil
			}
		}
	})
}
