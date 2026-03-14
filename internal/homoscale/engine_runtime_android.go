//go:build android

package homoscale

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>

typedef int (*protect_fd_fn)(int fd, char *errbuf, size_t errlen);
typedef int (*query_uid_fn)(int protocol, const char *source_addr, int source_port, const char *target_addr, int target_port, char *errbuf, size_t errlen);

static pthread_mutex_t android_protect_symbol_mutex = PTHREAD_MUTEX_INITIALIZER;
static void *android_protect_symbol_handle = NULL;
static protect_fd_fn android_protect_symbol_fn = NULL;
static query_uid_fn android_query_uid_symbol_fn = NULL;

static int callAndroidProtectSocketFD(int fd, char *errbuf, size_t errlen) {
	pthread_mutex_lock(&android_protect_symbol_mutex);
	if (android_protect_symbol_handle == NULL) {
		android_protect_symbol_handle = dlopen("libhomoscale_jni.so", RTLD_NOW | RTLD_GLOBAL);
		if (android_protect_symbol_handle == NULL) {
			snprintf(errbuf, errlen, "dlopen libhomoscale_jni.so failed: %s", dlerror());
			pthread_mutex_unlock(&android_protect_symbol_mutex);
			return -1;
		}
	}
	if (android_protect_symbol_fn == NULL) {
		android_protect_symbol_fn = (protect_fd_fn)dlsym(android_protect_symbol_handle, "HomoscaleProtectSocketFD");
		if (android_protect_symbol_fn == NULL) {
			snprintf(errbuf, errlen, "dlsym HomoscaleProtectSocketFD failed: %s", dlerror());
			pthread_mutex_unlock(&android_protect_symbol_mutex);
			return -1;
		}
	}
	protect_fd_fn fn = android_protect_symbol_fn;
	pthread_mutex_unlock(&android_protect_symbol_mutex);
	return fn(fd, errbuf, errlen);
}

static int callAndroidQueryConnectionOwnerUID(int protocol, const char *source_addr, int source_port, const char *target_addr, int target_port, char *errbuf, size_t errlen) {
	pthread_mutex_lock(&android_protect_symbol_mutex);
	if (android_protect_symbol_handle == NULL) {
		android_protect_symbol_handle = dlopen("libhomoscale_jni.so", RTLD_NOW | RTLD_GLOBAL);
		if (android_protect_symbol_handle == NULL) {
			snprintf(errbuf, errlen, "dlopen libhomoscale_jni.so failed: %s", dlerror());
			pthread_mutex_unlock(&android_protect_symbol_mutex);
			return -1;
		}
	}
	if (android_query_uid_symbol_fn == NULL) {
		android_query_uid_symbol_fn = (query_uid_fn)dlsym(android_protect_symbol_handle, "HomoscaleQueryConnectionOwnerUID");
		if (android_query_uid_symbol_fn == NULL) {
			snprintf(errbuf, errlen, "dlsym HomoscaleQueryConnectionOwnerUID failed: %s", dlerror());
			pthread_mutex_unlock(&android_protect_symbol_mutex);
			return -1;
		}
	}
	query_uid_fn fn = android_query_uid_symbol_fn;
	pthread_mutex_unlock(&android_protect_symbol_mutex);
	return fn(protocol, source_addr, source_port, target_addr, target_port, errbuf, errlen);
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	logrus "github.com/sirupsen/logrus"

	"github.com/metacubex/mihomo/common/observable"
	"github.com/metacubex/mihomo/component/dialer"
	mihomoProcess "github.com/metacubex/mihomo/component/process"
	mihomoConfig "github.com/metacubex/mihomo/config"
	mihomoConstant "github.com/metacubex/mihomo/constant"
	mihomoHub "github.com/metacubex/mihomo/hub"
	mihomoExecutor "github.com/metacubex/mihomo/hub/executor"
	"github.com/metacubex/mihomo/hub/route"
	mihomoLog "github.com/metacubex/mihomo/log"
	"gopkg.in/yaml.v3"
)

type androidEngineRuntime struct {
	running bool
}

type androidTailscaleConfigState struct {
	ManagedHosts map[string]string
	ManagedRules []string
	Proxy        map[string]any
	Signature    string
}

var (
	androidEngineMu      sync.Mutex
	androidEngineCurrent androidEngineRuntime
	androidPackagesMu    sync.RWMutex
	androidPackages      map[uint32]string
)

type androidInstalledApp struct {
	UID         uint32 `json:"uid"`
	PackageName string `json:"package_name"`
}

type AndroidRuntimeDebugInfo struct {
	RuntimeGOOS              string `json:"runtimeGoos"`
	RunningOnAndroid         bool   `json:"runningOnAndroid"`
	DefaultFindProcessMode   string `json:"defaultFindProcessMode"`
	ConfigFindProcessMode    string `json:"configFindProcessMode,omitempty"`
	InstalledAppsCount       int    `json:"installedAppsCount"`
	PackageResolverInstalled bool   `json:"packageResolverInstalled"`
}

func StartEngine(ctx context.Context, cfg *Config, logWriter io.Writer) error {
	if err := cfg.EnsureRuntimeDirs(); err != nil {
		return err
	}
	if err := cfg.ValidateProxy(); err != nil {
		return err
	}

	createdConfig, err := ensureEngineConfig(cfg)
	if err != nil {
		return err
	}

	androidEngineMu.Lock()
	if androidEngineCurrent.running {
		androidEngineMu.Unlock()
		return errors.New("homoscale engine is already running inside the app process")
	}
	androidEngineCurrent.running = true
	androidEngineMu.Unlock()
	defer func() {
		androidEngineMu.Lock()
		androidEngineCurrent.running = false
		androidEngineMu.Unlock()
	}()

	subscription := subscribeMihomoLogs(logWriter)
	defer subscription.Close()

	if createdConfig && logWriter != nil {
		_, _ = fmt.Fprintf(logWriter, "generated default engine config at %s\n", cfg.Engine.ConfigPath)
	}

	tailscaleState, err := startAndroidEmbeddedMihomo(cfg)
	if err != nil {
		stopAndroidEmbeddedMihomo(cfg)
		return err
	}
	defer stopAndroidEmbeddedMihomo(cfg)

	if err := writeProcessState(cfg.Engine.StateFile, ProcessState{
		PID:     os.Getpid(),
		Command: "embedded-mihomo",
	}); err != nil {
		return err
	}
	defer removeProcessState(cfg.Engine.StateFile)

	if err := waitForEngineReady(cfg, 15*time.Second); err != nil {
		return err
	}

	resolverCloser := installSystemResolver(cfg, logWriter)
	defer resolverCloser.Close()

	if logWriter != nil {
		_, _ = fmt.Fprintf(logWriter, "homoscale engine ready at %s\n", cfg.Engine.ControllerAddr)
	}

	go watchAndroidEmbeddedTailscaleConfig(ctx, cfg, logWriter, tailscaleState)

	<-ctx.Done()
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil
	}
	return ctx.Err()
}

func StopEngine(cfg *Config) error {
	androidEngineMu.Lock()
	running := androidEngineCurrent.running
	androidEngineMu.Unlock()
	if !running {
		removeProcessState(cfg.Engine.StateFile)
		return fmt.Errorf("homoscale engine is not running")
	}
	stopAndroidEmbeddedMihomo(cfg)
	removeProcessState(cfg.Engine.StateFile)
	return nil
}

func startAndroidEmbeddedMihomo(cfg *Config) (androidTailscaleConfigState, error) {
	if cfg == nil {
		return androidTailscaleConfigState{}, errors.New("config is required")
	}
	rendered, state, err := renderAndroidEmbeddedRuntimeConfig(cfg, androidTailscaleConfigState{})
	if err != nil {
		return androidTailscaleConfigState{}, err
	}

	mihomoConstant.SetHomeDir(cfg.Engine.WorkingDir)
	mihomoConstant.SetConfig(cfg.Engine.ConfigPath)
	if err := mihomoConfig.Init(cfg.Engine.WorkingDir); err != nil {
		return androidTailscaleConfigState{}, fmt.Errorf("initialize mihomo runtime: %w", err)
	}

	dialer.DefaultSocketHook = androidMihomoProtectSocket
	mihomoProcess.DefaultPackageNameResolver = androidMihomoPackageNameResolver

	parsed, err := mihomoExecutor.ParseWithBytes(rendered)
	if err != nil {
		dialer.DefaultSocketHook = nil
		mihomoProcess.DefaultPackageNameResolver = nil
		return androidTailscaleConfigState{}, fmt.Errorf("parse mihomo config: %w", err)
	}
	route.SetEmbedMode(true)
	mihomoHub.ApplyConfig(parsed)
	return state, nil
}

func renderAndroidEmbeddedRuntimeConfig(cfg *Config, previous androidTailscaleConfigState) ([]byte, androidTailscaleConfigState, error) {
	data, err := os.ReadFile(cfg.Engine.ConfigPath)
	if err != nil {
		return nil, androidTailscaleConfigState{}, fmt.Errorf("read engine config: %w", err)
	}
	var payload map[string]any
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return nil, androidTailscaleConfigState{}, fmt.Errorf("parse engine config yaml: %w", err)
	}
	if payload == nil {
		payload = map[string]any{}
	}

	current, err := currentAndroidTailscaleConfigState(cfg)
	if err != nil {
		return nil, androidTailscaleConfigState{}, err
	}
	replaceManagedTailscaleSections(payload, cfg, previous, current)

	base, err := yaml.Marshal(payload)
	if err != nil {
		return nil, androidTailscaleConfigState{}, fmt.Errorf("marshal refreshed engine config yaml: %w", err)
	}
	final, err := androidEmbeddedEngineConfigBytesFromRaw(cfg, base)
	if err != nil {
		return nil, androidTailscaleConfigState{}, err
	}
	if err := os.WriteFile(cfg.Engine.ConfigPath, final, 0o644); err != nil {
		return nil, androidTailscaleConfigState{}, fmt.Errorf("sync android engine config: %w", err)
	}
	return final, current, nil
}

func androidEmbeddedEngineConfigBytes(cfg *Config) ([]byte, error) {
	data, err := os.ReadFile(cfg.Engine.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("read engine config: %w", err)
	}
	return androidEmbeddedEngineConfigBytesFromRaw(cfg, data)
}

func androidEmbeddedEngineConfigBytesFromRaw(cfg *Config, data []byte) ([]byte, error) {
	if cfg == nil || cfg.Engine.Tun.RuntimeFile == nil {
		return data, nil
	}

	actualFD := int(cfg.Engine.Tun.RuntimeFile.Fd())
	if actualFD <= 0 {
		return nil, errors.New("android tun runtime file descriptor is unavailable")
	}

	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse engine config yaml: %w", err)
	}
	root := yamlMappingValue(&document, "tun")
	configRoot := yamlDocumentMapping(&document)
	if configRoot != nil {
		yamlSetMappingString(configRoot, "find-process-mode", "strict")
	}
	if root == nil {
		updated, err := yaml.Marshal(&document)
		if err != nil {
			return nil, fmt.Errorf("marshal engine config yaml: %w", err)
		}
		return updated, nil
	}
	yamlSetMappingInt(root, "file-descriptor", actualFD)
	updated, err := yaml.Marshal(&document)
	if err != nil {
		return nil, fmt.Errorf("marshal engine config yaml: %w", err)
	}
	return updated, nil
}

func currentAndroidTailscaleConfigState(cfg *Config) (androidTailscaleConfigState, error) {
	proxy, rules, err := buildTailscaleRouting(cfg)
	if err != nil {
		return androidTailscaleConfigState{}, err
	}
	hosts := tailscaleMagicDNSHosts(cfg)
	state := androidTailscaleConfigState{
		ManagedHosts: hosts,
		ManagedRules: append([]string(nil), rules...),
		Proxy:        cloneStringAnyMap(proxy),
	}
	state.Signature = androidTailscaleConfigSignature(state)
	return state, nil
}

func replaceManagedTailscaleSections(payload map[string]any, cfg *Config, previous, current androidTailscaleConfigState) {
	removeManagedHostEntries(payload, previous.ManagedHosts)
	removeManagedRules(payload, previous.ManagedRules)
	injectTailscaleDNSHosts(payload, cfg, current.ManagedHosts)
	injectTailscaleProxy(payload, current.Proxy)
	injectTailscaleRules(payload, current.ManagedRules)
}

func removeManagedHostEntries(payload map[string]any, hosts map[string]string) {
	if len(hosts) == 0 || payload == nil {
		return
	}
	hostSection, ok := payload["hosts"].(map[string]any)
	if !ok || hostSection == nil {
		return
	}
	for host := range hosts {
		delete(hostSection, host)
	}
}

func removeManagedRules(payload map[string]any, rules []string) {
	if len(rules) == 0 || payload == nil {
		return
	}
	existing := stringSlice(payload["rules"])
	if len(existing) == 0 {
		return
	}
	drop := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		drop[rule] = struct{}{}
	}
	filtered := make([]string, 0, len(existing))
	for _, rule := range existing {
		if _, ok := drop[rule]; ok {
			continue
		}
		filtered = append(filtered, rule)
	}
	payload["rules"] = filtered
}

func androidTailscaleConfigSignature(state androidTailscaleConfigState) string {
	parts := make([]string, 0, len(state.ManagedHosts)+len(state.ManagedRules)+4)
	parts = append(parts, "proxy="+serializeStringAnyMap(state.Proxy))
	hostKeys := make([]string, 0, len(state.ManagedHosts))
	for host := range state.ManagedHosts {
		hostKeys = append(hostKeys, host)
	}
	sortStrings(hostKeys)
	for _, host := range hostKeys {
		parts = append(parts, host+"="+strings.TrimSpace(state.ManagedHosts[host]))
	}
	rules := append([]string(nil), state.ManagedRules...)
	sortStrings(rules)
	for _, rule := range rules {
		parts = append(parts, "rule="+rule)
	}
	return strings.Join(parts, "\n")
}

func cloneStringAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func serializeStringAnyMap(src map[string]any) string {
	if len(src) == 0 {
		return ""
	}
	keys := make([]string, 0, len(src))
	for key := range src {
		keys = append(keys, key)
	}
	sortStrings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+fmt.Sprintf("%v", src[key]))
	}
	return strings.Join(parts, ",")
}

func sortStrings(items []string) {
	if len(items) < 2 {
		return
	}
	for index := 1; index < len(items); index++ {
		value := items[index]
		position := index - 1
		for position >= 0 && items[position] > value {
			items[position+1] = items[position]
			position--
		}
		items[position+1] = value
	}
}

func watchAndroidEmbeddedTailscaleConfig(ctx context.Context, cfg *Config, logWriter io.Writer, state androidTailscaleConfigState) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	current := state
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := currentAndroidTailscaleConfigState(cfg)
			if err != nil {
				continue
			}
			if next.Signature == current.Signature {
				continue
			}
			rendered, applied, err := renderAndroidEmbeddedRuntimeConfig(cfg, current)
			if err != nil {
				if logWriter != nil {
					_, _ = fmt.Fprintf(logWriter, "warning: refresh tailscale dns failed: %v\n", err)
				}
				continue
			}
			parsed, err := mihomoExecutor.ParseWithBytes(rendered)
			if err != nil {
				if logWriter != nil {
					_, _ = fmt.Fprintf(logWriter, "warning: parse refreshed mihomo config failed: %v\n", err)
				}
				continue
			}
			mihomoHub.ApplyConfig(parsed)
			current = applied
			if logWriter != nil {
				_, _ = fmt.Fprintf(logWriter, "refreshed tailscale dns hosts (%d entries)\n", len(applied.ManagedHosts))
			}
		}
	}
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil
		}
		return yamlMappingValue(node.Content[0], key)
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value == key {
				return node.Content[index+1]
			}
		}
	}
	return nil
}

func yamlDocumentMapping(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

func yamlSetMappingInt(node *yaml.Node, key string, value int) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	rendered := fmt.Sprintf("%d", value)
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value != key {
			continue
		}
		node.Content[index+1].Kind = yaml.ScalarNode
		node.Content[index+1].Tag = "!!int"
		node.Content[index+1].Value = rendered
		node.Content[index+1].Style = 0
		return
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: rendered},
	)
}

func yamlSetMappingString(node *yaml.Node, key string, value string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value != key {
			continue
		}
		node.Content[index+1].Kind = yaml.ScalarNode
		node.Content[index+1].Tag = "!!str"
		node.Content[index+1].Value = value
		node.Content[index+1].Style = 0
		return
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

func stopAndroidEmbeddedMihomo(cfg *Config) {
	route.ReCreateServer(&route.Config{})
	mihomoExecutor.Shutdown()
	dialer.DefaultSocketHook = nil
	mihomoProcess.DefaultPackageNameResolver = nil
}

func androidMihomoProtectSocket(network, address string, conn syscall.RawConn) error {
	var protectErr error
	controlErr := conn.Control(func(fd uintptr) {
		var errbuf [256]C.char
		if rc := C.callAndroidProtectSocketFD(C.int(fd), &errbuf[0], C.size_t(len(errbuf))); rc == 0 {
			return
		}
		message := strings.TrimSpace(C.GoString(&errbuf[0]))
		if message == "" {
			message = fmt.Sprintf("VpnService.protect failed for %s %s", network, address)
		}
		protectErr = errors.New(message)
	})
	if controlErr != nil {
		return controlErr
	}
	return protectErr
}

func SetAndroidInstalledAppsSnapshot(snapshotJSON string) {
	snapshotJSON = strings.TrimSpace(snapshotJSON)
	if snapshotJSON == "" {
		androidPackagesMu.Lock()
		androidPackages = nil
		androidPackagesMu.Unlock()
		return
	}

	var snapshots []androidInstalledApp
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshots); err != nil {
		return
	}

	next := make(map[uint32]string, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.UID == 0 {
			continue
		}
		name := strings.TrimSpace(snapshot.PackageName)
		if name == "" {
			continue
		}
		next[snapshot.UID] = name
	}

	androidPackagesMu.Lock()
	androidPackages = next
	androidPackagesMu.Unlock()
}

func androidMihomoPackageNameResolver(metadata *mihomoConstant.Metadata) (string, error) {
	if metadata == nil {
		return "", errors.New("metadata is required")
	}

	uid := metadata.Uid
	if uid == 0 {
		resolvedUID, err := androidQueryConnectionOwnerUID(metadata)
		if err != nil {
			return "", err
		}
		uid = resolvedUID
		metadata.Uid = uid
	}
	if uid == 0 {
		return "", errors.New("android package owner uid unavailable")
	}

	if packageName, ok := androidPackageNameForUID(uid); ok {
		return packageName, nil
	}
	if packageName, ok := androidPackageNameForUID(uid % 100000); ok {
		return packageName, nil
	}

	return "", fmt.Errorf("android package name not found for uid %d", uid)
}

func androidPackageNameForUID(uid uint32) (string, bool) {
	if uid == 0 {
		return "", false
	}
	androidPackagesMu.RLock()
	defer androidPackagesMu.RUnlock()
	if len(androidPackages) == 0 {
		return "", false
	}
	name, ok := androidPackages[uid]
	return name, ok
}

func androidQueryConnectionOwnerUID(metadata *mihomoConstant.Metadata) (uint32, error) {
	if metadata == nil {
		return 0, errors.New("metadata is required")
	}
	if !metadata.SourceValid() || !metadata.DstIP.IsValid() || metadata.DstPort == 0 {
		return 0, errors.New("android socket metadata is incomplete")
	}

	var protocol int
	switch metadata.NetWork {
	case mihomoConstant.TCP:
		protocol = 6
	case mihomoConstant.UDP:
		protocol = 17
	default:
		return 0, fmt.Errorf("unsupported android socket network %q", metadata.NetWork.String())
	}

	srcIP := metadata.SrcIP.Unmap()
	dstIP := metadata.DstIP.Unmap()
	sourceAddr := srcIP.String()
	targetAddr := dstIP.String()
	if sourceAddr == "" || targetAddr == "" {
		return 0, errors.New("android socket addresses are unavailable")
	}

	source := C.CString(sourceAddr)
	target := C.CString(targetAddr)
	defer C.free(unsafe.Pointer(source))
	defer C.free(unsafe.Pointer(target))

	var errbuf [256]C.char
	uid := C.callAndroidQueryConnectionOwnerUID(
		C.int(protocol),
		source,
		C.int(metadata.SrcPort),
		target,
		C.int(metadata.DstPort),
		&errbuf[0],
		C.size_t(len(errbuf)),
	)
	if uid < 0 {
		message := strings.TrimSpace(C.GoString(&errbuf[0]))
		if message == "" {
			message = fmt.Sprintf("queryConnectionOwnerUid failed for %s:%d -> %s:%d", sourceAddr, metadata.SrcPort, targetAddr, metadata.DstPort)
		}
		return 0, errors.New(message)
	}
	return uint32(uid), nil
}

func RuntimeDebugInfo(cfg *Config) any {
	info := AndroidRuntimeDebugInfo{
		RuntimeGOOS:              runtimeGOOS,
		RunningOnAndroid:         runningOnAndroid(),
		DefaultFindProcessMode:   defaultFindProcessMode(),
		InstalledAppsCount:       androidInstalledAppsCount(),
		PackageResolverInstalled: mihomoProcess.DefaultPackageNameResolver != nil,
	}
	if cfg != nil {
		info.ConfigFindProcessMode = readConfigFindProcessMode(cfg.Engine.ConfigPath)
	}
	return info
}

func androidInstalledAppsCount() int {
	androidPackagesMu.RLock()
	defer androidPackagesMu.RUnlock()
	return len(androidPackages)
}

func readConfigFindProcessMode(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return ""
	}
	if payload == nil {
		return ""
	}
	mode, _ := payload["find-process-mode"].(string)
	return strings.TrimSpace(mode)
}

type mihomoLogSubscription struct {
	done chan struct{}
	once sync.Once
	prev io.Writer
	sub  observable.Subscription[mihomoLog.Event]
}

func subscribeMihomoLogs(logWriter io.Writer) *mihomoLogSubscription {
	if logWriter == nil {
		return &mihomoLogSubscription{}
	}

	originalOutput := logrus.StandardLogger().Out
	logrus.SetOutput(logWriter)

	sub := mihomoLog.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for event := range sub {
			_, _ = fmt.Fprintln(logWriter, event.Payload)
		}
	}()

	return &mihomoLogSubscription{
		done: done,
		prev: originalOutput,
		sub:  sub,
		once: sync.Once{},
	}
}

func (s *mihomoLogSubscription) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		if s.sub != nil {
			mihomoLog.UnSubscribe(s.sub)
		}
		if s.done != nil {
			<-s.done
		}
		if s.prev != nil {
			logrus.SetOutput(s.prev)
		}
	})
}
