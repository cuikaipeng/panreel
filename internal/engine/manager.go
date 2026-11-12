package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/network"
	"github.com/cuikaipeng/panreel/internal/search"
)

type Manager struct {
	mu                 sync.Mutex
	Resources, DataDir string
	BinaryPath         string
	Catalog            *catalog.Catalog
	cmd                *exec.Cmd
	stdin              io.WriteCloser
	done               chan struct{}
	client             *search.Client
}

func (m *Manager) Client() *search.Client { m.mu.Lock(); defer m.mu.Unlock(); return m.client }
func (m *Manager) Binary() string {
	if m.BinaryPath != "" {
		return m.BinaryPath
	}
	name := "pansou"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// MyGo merges the matching platform directory into the installed resources.
	installed := filepath.Join(m.Resources, "bin", name)
	if _, err := os.Stat(installed); err == nil {
		return installed
	}
	return filepath.Join(m.Resources, runtime.GOOS+"-"+runtime.GOARCH, "bin", name)
}
func (m *Manager) Start(ctx context.Context, s model.Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	catalogue := catalog.Current()
	if m.Catalog != nil {
		catalogue = *m.Catalog
	}
	if err := s.ValidateCatalog(catalogue); err != nil {
		return err
	}
	if m.done != nil {
		select {
		case <-m.done:
		default:
			return fmt.Errorf("搜索引擎已在运行")
		}
	}
	if _, err := os.Stat(m.Binary()); err != nil {
		return fmt.Errorf("安装包缺少 %s/%s 搜索引擎，请重新安装完整版本", runtime.GOOS, runtime.GOARCH)
	}
	for _, dir := range []string{m.DataDir, filepath.Join(m.DataDir, "cache"), filepath.Join(m.DataDir, "logs")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return err
	}
	token := hex.EncodeToString(secret)
	proxyURL := ""
	var bridge *network.Bridge
	if s.EffectiveProxyMode() == model.ProxyManual {
		proxyURL = s.Proxy
	} else if s.EffectiveProxyMode() == model.ProxySystem {
		bridge, err = network.StartBridge()
		if err != nil {
			return fmt.Errorf("无法启动系统代理连接：%w", err)
		}
		proxyURL = bridge.URL()
	}
	started := false
	defer func() {
		if !started && bridge != nil {
			bridge.Close()
		}
	}()
	values := map[string]string{"PORT": strconv.Itoa(port), "PANREEL_TOKEN": token, "PANREEL_MANAGED": "1", "GIN_MODE": "release", "AUTH_ENABLED": "false", "ENABLED_PLUGINS": strings.Join(s.Plugins, ","), "CHANNELS": strings.Join(s.Channels, ","), "CACHE_PATH": filepath.Join(m.DataDir, "cache"), "CACHE_MAX_SIZE": strconv.Itoa(s.CacheMB), "CACHE_TTL": "60", "CACHE_ENABLED": "true", "ASYNC_PLUGIN_ENABLED": "true", "ASYNC_RESPONSE_TIMEOUT": "6", "PLUGIN_TIMEOUT": "15", "ASYNC_MAX_BACKGROUND_WORKERS": "16", "ASYNC_MAX_BACKGROUND_TASKS": "200", "ASYNC_LOG_ENABLED": "false", "PLUGIN_SEARCH_DETAIL_LOG": "false", "CONCURRENCY": strconv.Itoa(s.Concurrency), "OUTBOUND_MAX_CONCURRENCY": strconv.Itoa(s.Concurrency), "HTTP_MAX_CONNS": "100", "HTTP_WRITE_TIMEOUT": "180", "INSECURE_SKIP_TLS_VERIFY": "false"}
	for k, v := range s.PluginEnv {
		if v != "" {
			values[k] = v
		}
	}
	values["PROXY"] = proxyURL
	values["HTTP_PROXY"] = proxyURL
	values["HTTPS_PROXY"] = proxyURL
	values["ALL_PROXY"] = proxyURL
	values["NO_PROXY"] = "127.0.0.1,localhost"
	var env []string
	pluginKeys := catalogue.AllowedEnv()
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		upper := strings.ToUpper(k)
		if _, ok := values[k]; ok {
			continue
		}
		if pluginKeys[k] || strings.HasPrefix(upper, "AUTH_") || strings.HasPrefix(upper, "PANREEL_") || slices.Contains([]string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "PROXY"}, upper) {
			continue
		}
		env = append(env, v)
	}
	for k, v := range values {
		env = append(env, k+"="+v)
	}
	logPath := filepath.Join(m.DataDir, "logs", "engine.log")
	_ = os.Remove(logPath + ".previous")
	_ = os.Rename(logPath, logPath+".previous")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	cmd := exec.Command(m.Binary())
	configureProcess(cmd)
	cmd.Env = env
	cmd.Dir = m.DataDir
	logSettings := s
	logSettings.Proxy = proxyURL
	safeLog := newPrivateLog(log, logSettings, token)
	cmd.Stdout = safeLog
	cmd.Stderr = safeLog
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Close()
		return err
	}
	if err = cmd.Start(); err != nil {
		stdin.Close()
		log.Close()
		return err
	}
	started = true
	m.cmd = cmd
	m.stdin = stdin
	m.done = make(chan struct{})
	done := m.done
	go func() {
		_ = cmd.Wait()
		if bridge != nil {
			bridge.Close()
		}
		_ = safeLog.Flush()
		_ = log.Close()
		close(done)
	}()
	client := search.NewClient(fmt.Sprintf("http://127.0.0.1:%d", port), token)
	ready, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		if h, healthErr := client.Health(ready); healthErr == nil {
			identity, identityErr := client.Identity(ready)
			if identityErr == nil && identity.Protocol == 1 && identity.SearchUpdates && identity.Revision == catalogue.Revision && slices.Equal(h.Channels, s.Channels) {
				m.client = client
				return nil
			}
			stdin.Close()
			_ = cmd.Process.Kill()
			<-done
			return fmt.Errorf("搜索引擎版本或频道配置不兼容")
		}
		select {
		case <-done:
			stdin.Close()
			return fmt.Errorf("内置搜索引擎启动失败，可在设置中查看运行日志")
		case <-ready.Done():
			stdin.Close()
			_ = cmd.Process.Kill()
			<-done
			return fmt.Errorf("搜索引擎启动超时：%w", ready.Err())
		case <-ticker.C:
		}
	}
}
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.client = nil
	if m.done == nil {
		return
	}
	if m.stdin != nil {
		_ = m.stdin.Close()
		m.stdin = nil
	}
	select {
	case <-m.done:
	case <-time.After(14 * time.Second):
		if m.cmd != nil {
			_ = m.cmd.Process.Kill()
		}
		<-m.done
	}
	m.cmd = nil
	m.done = nil
}
