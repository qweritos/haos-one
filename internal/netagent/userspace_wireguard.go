//go:build darwin || windows

package netagent

import (
	"fmt"
	"net"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

type userspaceWireGuard struct {
	device   *device.Device
	listener net.Listener
	close    func()
	once     sync.Once
}

func startUserspaceWireGuard(requestedName string, mtu int) (string, tunnelHelper, error) {
	platformClose, err := prepareUserspaceWireGuard()
	if err != nil {
		return "", nil, err
	}
	tunDevice, err := tun.CreateTUN(requestedName, mtu)
	if err != nil {
		platformClose()
		return "", nil, fmt.Errorf("create userspace WireGuard TUN: %w", err)
	}
	name, err := tunDevice.Name()
	if err != nil {
		_ = tunDevice.Close()
		platformClose()
		return "", nil, fmt.Errorf("read userspace WireGuard interface name: %w", err)
	}
	logger := device.NewLogger(device.LogLevelError, fmt.Sprintf("(%s) ", name))
	wgDevice := device.NewDevice(tunDevice, conn.NewDefaultBind(), logger)
	if err := activateUserspaceWireGuard(wgDevice); err != nil {
		wgDevice.Close()
		platformClose()
		return "", nil, fmt.Errorf("activate userspace WireGuard device: %w", err)
	}
	listener, err := listenUserspaceWireGuard(name)
	if err != nil {
		wgDevice.Close()
		platformClose()
		return "", nil, fmt.Errorf("listen on WireGuard UAPI: %w", err)
	}
	helper := &userspaceWireGuard{
		device:   wgDevice,
		listener: listener,
		close:    platformClose,
	}
	go helper.serve()
	return name, helper, nil
}

func (h *userspaceWireGuard) serve() {
	for {
		connection, err := h.listener.Accept()
		if err != nil {
			return
		}
		go h.device.IpcHandle(connection)
	}
}

func (h *userspaceWireGuard) Close() error {
	if h == nil {
		return nil
	}
	var closeErr error
	h.once.Do(func() {
		closeErr = h.listener.Close()
		h.device.Close()
		if h.close != nil {
			h.close()
		}
	})
	return closeErr
}

func (h *userspaceWireGuard) PID() int { return 0 }
