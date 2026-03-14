//go:build android

package main

/*
#cgo LDFLAGS: -ldl
#include <android/log.h>
#include <dlfcn.h>
#include <pthread.h>
#include <stdio.h>

typedef int (*protect_fd_fn)(int fd, char *errbuf, size_t errlen);

static pthread_mutex_t protect_symbol_mutex = PTHREAD_MUTEX_INITIALIZER;
static void *protect_symbol_handle = NULL;
static protect_fd_fn protect_symbol_fn = NULL;

static int callProtectSocketFD(int fd, char *errbuf, size_t errlen) {
	pthread_mutex_lock(&protect_symbol_mutex);
	if (protect_symbol_handle == NULL) {
		protect_symbol_handle = dlopen("libhomoscale_jni.so", RTLD_NOW | RTLD_GLOBAL);
		if (protect_symbol_handle == NULL) {
			snprintf(errbuf, errlen, "dlopen libhomoscale_jni.so failed: %s", dlerror());
			__android_log_print(ANDROID_LOG_ERROR, "homoscale_protect", "%s", errbuf);
			pthread_mutex_unlock(&protect_symbol_mutex);
			return -1;
		}
	}
	if (protect_symbol_fn == NULL) {
		protect_symbol_fn = (protect_fd_fn)dlsym(protect_symbol_handle, "HomoscaleProtectSocketFD");
		if (protect_symbol_fn == NULL) {
			snprintf(errbuf, errlen, "dlsym HomoscaleProtectSocketFD failed: %s", dlerror());
			__android_log_print(ANDROID_LOG_ERROR, "homoscale_protect", "%s", errbuf);
			pthread_mutex_unlock(&protect_symbol_mutex);
			return -1;
		}
	}
	protect_fd_fn fn = protect_symbol_fn;
	pthread_mutex_unlock(&protect_symbol_mutex);
	return fn(fd, errbuf, errlen);
}

static void logProtectHookInstalled(void) {
	__android_log_print(ANDROID_LOG_INFO, "homoscale_protect", "Android protect hook installed");
}

static void logProtectCallbackInvoked(int fd) {
	__android_log_print(ANDROID_LOG_INFO, "homoscale_protect", "protect callback invoked fd=%d", fd);
}
*/
import "C"

import (
	"encoding/json"
	"errors"
	hs "homoscale/internal/homoscale"
	"net"
	"strings"
	"sync"

	"tailscale.com/net/netmon"
	"tailscale.com/net/netns"
)

type androidInterfaceSnapshot struct {
	Name         string   `json:"name"`
	Index        int      `json:"index"`
	MTU          int      `json:"mtu"`
	Flags        int      `json:"flags"`
	HardwareAddr string   `json:"hardware_addr,omitempty"`
	Addrs        []string `json:"addrs,omitempty"`
}

var (
	androidInterfacesMu sync.RWMutex
	androidInterfaces   []netmon.Interface
)

func init() {
	C.logProtectHookInstalled()
	netns.SetAndroidProtectFunc(func(fd int) error {
		C.logProtectCallbackInvoked(C.int(fd))
		var errbuf [256]C.char
		if rc := C.callProtectSocketFD(C.int(fd), &errbuf[0], C.size_t(len(errbuf))); rc == 0 {
			return nil
		}
		message := C.GoString(&errbuf[0])
		if message == "" {
			message = "VpnService.protect failed"
		}
		return errors.New(message)
	})
	netmon.RegisterInterfaceGetter(func() ([]netmon.Interface, error) {
		androidInterfacesMu.RLock()
		defer androidInterfacesMu.RUnlock()
		return append([]netmon.Interface(nil), androidInterfaces...), nil
	})
}

//export HomoscaleSetAndroidDefaultRouteInterface
func HomoscaleSetAndroidDefaultRouteInterface(interfaceName *C.char) {
	if interfaceName == nil {
		netmon.UpdateLastKnownDefaultRouteInterface("")
		return
	}
	ifName := strings.TrimSpace(C.GoString(interfaceName))
	netmon.UpdateLastKnownDefaultRouteInterface(ifName)
}

//export HomoscaleSetAndroidInterfaceSnapshot
func HomoscaleSetAndroidInterfaceSnapshot(snapshotJSON *C.char) {
	if snapshotJSON == nil {
		androidInterfacesMu.Lock()
		androidInterfaces = nil
		androidInterfacesMu.Unlock()
		return
	}

	var snapshots []androidInterfaceSnapshot
	if err := json.Unmarshal([]byte(C.GoString(snapshotJSON)), &snapshots); err != nil {
		return
	}

	next := make([]netmon.Interface, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if strings.TrimSpace(snapshot.Name) == "" {
			continue
		}

		iface := netmon.Interface{
			Interface: &net.Interface{
				Index: snapshot.Index,
				MTU:   snapshot.MTU,
				Name:  snapshot.Name,
				Flags: net.Flags(snapshot.Flags),
			},
		}
		if snapshot.HardwareAddr != "" {
			if hw, err := net.ParseMAC(snapshot.HardwareAddr); err == nil {
				iface.Interface.HardwareAddr = hw
			}
		}
		if len(snapshot.Addrs) > 0 {
			iface.AltAddrs = make([]net.Addr, 0, len(snapshot.Addrs))
			for _, raw := range snapshot.Addrs {
				raw = strings.TrimSpace(raw)
				if raw == "" {
					continue
				}
				if _, ipNet, err := net.ParseCIDR(raw); err == nil {
					iface.AltAddrs = append(iface.AltAddrs, ipNet)
					continue
				}
				if ip := net.ParseIP(raw); ip != nil {
					iface.AltAddrs = append(iface.AltAddrs, &net.IPAddr{IP: ip})
				}
			}
		}
		next = append(next, iface)
	}

	androidInterfacesMu.Lock()
	androidInterfaces = next
	androidInterfacesMu.Unlock()
}

//export HomoscaleSetAndroidInstalledAppsSnapshot
func HomoscaleSetAndroidInstalledAppsSnapshot(snapshotJSON *C.char) {
	if snapshotJSON == nil {
		hs.SetAndroidInstalledAppsSnapshot("")
		return
	}
	hs.SetAndroidInstalledAppsSnapshot(C.GoString(snapshotJSON))
}
