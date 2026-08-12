//go:build windows

package netagent

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/ipc"
)

const embeddedWintunSHA256 = "e5da8447dc2c320edc0fc52fa01885c103de8c118481f683643cacc3220dafce"

//go:embed assets/windows-amd64/wintun.dll
var embeddedWintun []byte

func prepareUserspaceWireGuard() (func(), error) {
	digest := sha256.Sum256(embeddedWintun)
	if hex.EncodeToString(digest[:]) != embeddedWintunSHA256 {
		return nil, fmt.Errorf("embedded Wintun DLL checksum mismatch")
	}
	cacheDirectory, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolve Wintun cache directory: %w", err)
	}
	directory := filepath.Join(cacheDirectory, "haos-one-net")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create private Wintun cache: %w", err)
	}
	// The WireGuard Windows TUN backend resolves the module by its canonical
	// basename after this preload. Keep that basename even though the payload is
	// version-pinned and checksum-verified.
	path := filepath.Join(directory, "wintun.dll")
	if err := ensureEmbeddedWintun(path); err != nil {
		return nil, err
	}
	dll, err := windows.LoadDLL(path)
	if err != nil {
		return nil, fmt.Errorf("load embedded Wintun DLL: %w", err)
	}
	return func() {
		_ = dll.Release()
	}, nil
}

func ensureEmbeddedWintun(path string) error {
	if existing, err := os.ReadFile(path); err == nil {
		digest := sha256.Sum256(existing)
		if hex.EncodeToString(digest[:]) == embeddedWintunSHA256 {
			return nil
		}
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "wintun-*.dll")
	if err != nil {
		return fmt.Errorf("create temporary Wintun DLL: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(embeddedWintun); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write embedded Wintun DLL: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close embedded Wintun DLL: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace invalid Wintun DLL: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install embedded Wintun DLL: %w", err)
	}
	return nil
}

func activateUserspaceWireGuard(wgDevice *device.Device) error {
	return wgDevice.Up()
}

func listenUserspaceWireGuard(name string) (net.Listener, error) {
	return ipc.UAPIListen(name)
}
