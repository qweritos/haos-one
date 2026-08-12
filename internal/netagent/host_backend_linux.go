//go:build linux

package netagent

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

func prepareHost(ctx context.Context, cfg *Config, tunnel *Tunnel) (*State, error) {
	return nil, fmt.Errorf("host mode supports macOS and Windows; use guest mode inside HAOS One")
}

func cleanupHost(ctx context.Context, state *State) error { return nil }

func addGuestRoutes(ctx context.Context, tunnel string, cidrs []string) error {
	added := make([]string, 0, len(cidrs))
	for _, cidr := range cidrs {
		if err := runCommand(ctx, "ip", "route", "replace", cidr, "dev", tunnel); err != nil {
			_ = removeGuestRoutes(ctx, tunnel, added)
			return err
		}
		added = append(added, cidr)
	}
	return nil
}

func removeGuestRoutes(ctx context.Context, tunnel string, cidrs []string) error {
	var first error
	for _, cidr := range cidrs {
		if err := runCommand(ctx, "ip", "route", "delete", cidr, "dev", tunnel); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func injectUDP(source net.IP, sourcePort int, destination net.IP, destinationPort int, ttl int, payload []byte, interfaceName string) error {
	src := source.To4()
	dst := destination.To4()
	if src == nil || dst == nil {
		return fmt.Errorf("UDP injection requires IPv4 addresses")
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, unix.IPPROTO_UDP)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	for _, option := range []int{unix.SO_REUSEADDR, unix.SO_REUSEPORT} {
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, option, 1); err != nil {
			return err
		}
	}
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_FREEBIND, 1); err != nil {
		return err
	}
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_TRANSPARENT, 1); err != nil {
		return err
	}
	if err := unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, interfaceName); err != nil {
		return err
	}
	sourceAddress := &unix.SockaddrInet4{Port: sourcePort}
	copy(sourceAddress.Addr[:], src)
	if err := unix.Bind(fd, sourceAddress); err != nil {
		return err
	}
	if ttl <= 0 || ttl > 255 {
		ttl = 64
	}
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_TTL, ttl); err != nil {
		return err
	}
	if dst.IsMulticast() {
		iface, ifaceErr := net.InterfaceByName(interfaceName)
		if ifaceErr != nil {
			return ifaceErr
		}
		localIP, addressErr := interfaceIPv4(iface)
		if addressErr != nil {
			return addressErr
		}
		var multicastInterface [4]byte
		copy(multicastInterface[:], localIP.To4())
		if err := unix.SetsockoptInet4Addr(fd, unix.IPPROTO_IP, unix.IP_MULTICAST_IF, multicastInterface); err != nil {
			return err
		}
		if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_MULTICAST_TTL, ttl); err != nil {
			return err
		}
		if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_MULTICAST_LOOP, 1); err != nil {
			return err
		}
	}
	addr := &unix.SockaddrInet4{Port: destinationPort}
	copy(addr.Addr[:], dst)
	return unix.Sendto(fd, payload, 0, addr)
}

func isAdministrator() bool { return os.Geteuid() == 0 }

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func stopOwnedHelper(ctx context.Context, state *State) error { return nil }
