package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	hs "homoscale/internal/homoscale"
)

type bridgeResponse struct {
	OK      bool   `json:"ok"`
	Running bool   `json:"running,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	LogPath string `json:"log_path,omitempty"`
	Data    any    `json:"data,omitempty"`
}

type bridgeSession struct {
	configPath string
	logPath    string
	cancel     context.CancelFunc
	done       chan struct{}
}

var (
	bridgeMu      sync.Mutex
	activeSession *bridgeSession
	lastBridgeErr string
)

func main() {}

//export HomoscaleVersionJSON
func HomoscaleVersionJSON() *C.char {
	return jsonCString(bridgeResponse{
		OK:   true,
		Data: hs.VersionDetails(),
	})
}

//export HomoscaleStatusJSON
func HomoscaleStatusJSON(configPath *C.char) *C.char {
	path := goString(configPath)
	cfg, err := loadBridgeConfig(path)
	if err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}

	bridgeMu.Lock()
	running := activeSession != nil
	lastErr := lastBridgeErr
	logPath := ""
	if activeSession != nil {
		logPath = activeSession.logPath
	}
	bridgeMu.Unlock()

	authStatus, _ := hs.ReadAuthStatus(cfg)
	data := map[string]any{
		"status":     hs.ReadStatus(cfg),
		"auth":       authStatus,
		"engine":     hs.ReadEngineStatus(cfg),
		"version":    hs.VersionDetails(),
		"configPath": path,
		"lastError":  lastErr,
		"runtime":    hs.RuntimeDebugInfo(cfg),
	}
	return jsonCString(bridgeResponse{
		OK:      true,
		Running: running,
		LogPath: logPath,
		Data:    data,
	})
}

//export HomoscaleLoginJSON
func HomoscaleLoginJSON(configPath *C.char) *C.char {
	path := goString(configPath)
	cfg, err := loadBridgeConfig(path)
	if err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	if err := cfg.EnsureRuntimeDirs(); err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}

	var logs bytes.Buffer
	if err := hs.LoginTailscale(context.Background(), cfg, &logs); err != nil {
		return jsonCString(bridgeResponse{
			Error: err.Error(),
			Data: map[string]any{
				"logs": strings.TrimSpace(logs.String()),
				"auth": readAuthStatusNoError(cfg),
			},
		})
	}

	return jsonCString(bridgeResponse{
		OK:      true,
		Message: "tailscale login requested",
		Data: map[string]any{
			"logs": strings.TrimSpace(logs.String()),
			"auth": readAuthStatusNoError(cfg),
		},
	})
}

//export HomoscaleRefreshSubscriptionJSON
func HomoscaleRefreshSubscriptionJSON(configPath *C.char) *C.char {
	path := goString(configPath)
	cfg, err := loadBridgeConfig(path)
	if err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	if err := hs.RefreshSubscriptionProvider(cfg); err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	return jsonCString(bridgeResponse{
		OK:      true,
		Message: "subscription config refreshed",
	})
}

//export HomoscaleLogoutJSON
func HomoscaleLogoutJSON(configPath *C.char) *C.char {
	path := goString(configPath)
	cfg, err := loadBridgeConfig(path)
	if err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	if err := hs.LogoutTailscale(context.Background(), cfg); err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	return jsonCString(bridgeResponse{
		OK:      true,
		Message: "tailscale logout requested",
		Data: map[string]any{
			"auth": readAuthStatusNoError(cfg),
		},
	})
}

//export HomoscaleStartWithTunFDJSON
func HomoscaleStartWithTunFDJSON(configPath *C.char, tunFD C.int) *C.char {
	path := goString(configPath)
	cfg, err := loadBridgeConfig(path)
	if err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	tunFile, err := configureRuntimeTunFile(cfg, int(tunFD))
	if err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	if err := cfg.EnsureRuntimeDirs(); err != nil {
		closeFile(tunFile)
		return jsonCString(bridgeResponse{Error: err.Error()})
	}

	logPath := filepath.Join(cfg.RuntimeDir, "logs", "android-service.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		closeFile(tunFile)
		return jsonCString(bridgeResponse{Error: err.Error()})
	}

	ctx, cancel := context.WithCancel(context.Background())
	session := &bridgeSession{
		configPath: path,
		logPath:    logPath,
		cancel:     cancel,
		done:       make(chan struct{}),
	}

	bridgeMu.Lock()
	if activeSession != nil {
		existingLogPath := activeSession.logPath
		bridgeMu.Unlock()
		closeFile(tunFile)
		_ = logFile.Close()
		return jsonCString(bridgeResponse{
			Error:   "homoscale is already running inside the app service",
			Running: true,
			LogPath: existingLogPath,
		})
	}
	lastBridgeErr = ""
	activeSession = session
	bridgeMu.Unlock()

	go func() {
		err := hs.StartHomoscale(ctx, cfg, logFile)
		closeFile(tunFile)
		_ = logFile.Close()

		bridgeMu.Lock()
		if activeSession == session {
			activeSession = nil
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			lastBridgeErr = err.Error()
		}
		close(session.done)
		bridgeMu.Unlock()
	}()

	return jsonCString(bridgeResponse{
		OK:      true,
		Running: true,
		Message: "homoscale start requested",
		LogPath: logPath,
		Data: map[string]any{
			"configPath": path,
		},
	})
}

//export HomoscaleSetProxyModeJSON
func HomoscaleSetProxyModeJSON(configPath *C.char, mode *C.char) *C.char {
	path := goString(configPath)
	cfg, err := loadBridgeConfig(path)
	if err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	if err := hs.SetProxyMode(cfg, goString(mode)); err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	return jsonCString(bridgeResponse{
		OK:      true,
		Message: "proxy mode updated",
		Data: map[string]any{
			"engine": hs.ReadEngineStatus(cfg),
		},
	})
}

//export HomoscaleSelectProxyGroupJSON
func HomoscaleSelectProxyGroupJSON(configPath *C.char, groupName *C.char, proxyName *C.char) *C.char {
	path := goString(configPath)
	cfg, err := loadBridgeConfig(path)
	if err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	if err := hs.SelectProxyGroup(cfg, goString(groupName), goString(proxyName)); err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	return jsonCString(bridgeResponse{
		OK:      true,
		Message: "proxy group updated",
		Data: map[string]any{
			"engine": hs.ReadEngineStatus(cfg),
		},
	})
}

//export HomoscaleStopJSON
func HomoscaleStopJSON(configPath *C.char) *C.char {
	path := goString(configPath)

	bridgeMu.Lock()
	session := activeSession
	bridgeMu.Unlock()

	if session != nil {
		session.cancel()
		select {
		case <-session.done:
		case <-time.After(8 * time.Second):
		}
		return jsonCString(bridgeResponse{
			OK:      true,
			Message: "homoscale stop requested",
			LogPath: session.logPath,
		})
	}

	cfg, err := loadBridgeConfig(path)
	if err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	if err := hs.StopEngine(cfg); err != nil {
		return jsonCString(bridgeResponse{Error: err.Error()})
	}
	return jsonCString(bridgeResponse{
		OK:      true,
		Message: "engine stopped",
	})
}

//export HomoscaleFreeString
func HomoscaleFreeString(value *C.char) {
	if value == nil {
		return
	}
	C.free(unsafe.Pointer(value))
}

func jsonCString(value any) *C.char {
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte(`{"ok":false,"error":"marshal bridge response failed"}`)
	}
	return C.CString(string(data))
}

func goString(value *C.char) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(C.GoString(value))
}

func loadBridgeConfig(path string) (*hs.Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("config path is required")
	}
	cfg, err := hs.LoadConfig(path)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func readAuthStatusNoError(cfg *hs.Config) *hs.AuthStatus {
	status, _ := hs.ReadAuthStatus(cfg)
	return status
}

func configureRuntimeTunFile(cfg *hs.Config, tunFD int) (*os.File, error) {
	if cfg == nil || cfg.Engine.Tun.Enable == nil || !*cfg.Engine.Tun.Enable {
		return nil, nil
	}
	if tunFD <= 0 {
		return nil, errors.New("vpn tun fd is required")
	}
	if cfg.Engine.Tun.FileDescriptor == 0 {
		cfg.Engine.Tun.FileDescriptor = 3
	}
	if cfg.Engine.Tun.FileDescriptor != 3 {
		return nil, errors.New("engine tun file descriptor must be 3 on Android")
	}
	file := os.NewFile(uintptr(tunFD), "android-vpn-tun")
	if file == nil {
		return nil, errors.New("wrap vpn tun fd")
	}
	cfg.Engine.Tun.RuntimeFile = file
	return file, nil
}

func closeFile(file *os.File) {
	if file == nil {
		return
	}
	_ = file.Close()
}
