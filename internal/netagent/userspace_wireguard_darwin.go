//go:build darwin

package netagent

import (
	"net"

	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/ipc"
)

func prepareUserspaceWireGuard() (func(), error) {
	return func() {}, nil
}

func activateUserspaceWireGuard(_ *device.Device) error { return nil }

func listenUserspaceWireGuard(name string) (net.Listener, error) {
	file, err := ipc.UAPIOpen(name)
	if err != nil {
		return nil, err
	}
	listener, err := ipc.UAPIListen(name, file)
	_ = file.Close()
	return listener, err
}
