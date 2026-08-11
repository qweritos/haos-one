//go:build linux

package netagent

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
)

func prepareHost(ctx context.Context, cfg *Config, tunnel *Tunnel) (*State, error) {
	return nil, fmt.Errorf("host mode supports macOS and Windows; use guest mode inside HAOS One")
}

func cleanupHost(ctx context.Context, state *State) error { return nil }

func addGuestRoutes(ctx context.Context, tunnel string, cidrs []string) error {
	added := make([]string, 0, len(cidrs))
	for _, cidr := range cidrs {
		if err := runCommand(ctx, "ip", "route", "add", cidr, "dev", tunnel); err != nil {
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

func injectUDP(source net.IP, sourcePort int, destination net.IP, destinationPort int, ttl int, payload []byte) error {
	src := source.To4()
	dst := destination.To4()
	if src == nil || dst == nil {
		return fmt.Errorf("raw injection requires IPv4 addresses")
	}
	udpLen := 8 + len(payload)
	totalLen := 20 + udpLen
	packet := make([]byte, totalLen)
	packet[0] = 0x45
	packet[1] = 0
	binary.BigEndian.PutUint16(packet[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(packet[4:6], uint16(NewMessageID()))
	packet[6] = 0x40
	packet[8] = byte(ttl)
	if packet[8] == 0 {
		packet[8] = 64
	}
	packet[9] = syscall.IPPROTO_UDP
	copy(packet[12:16], src)
	copy(packet[16:20], dst)
	binary.BigEndian.PutUint16(packet[10:12], checksum(packet[:20]))
	binary.BigEndian.PutUint16(packet[20:22], uint16(sourcePort))
	binary.BigEndian.PutUint16(packet[22:24], uint16(destinationPort))
	binary.BigEndian.PutUint16(packet[24:26], uint16(udpLen))
	copy(packet[28:], payload)
	pseudo := make([]byte, 12+udpLen)
	copy(pseudo[0:4], src)
	copy(pseudo[4:8], dst)
	pseudo[9] = syscall.IPPROTO_UDP
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(udpLen))
	copy(pseudo[12:], packet[20:])
	binary.BigEndian.PutUint16(packet[26:28], checksum(pseudo))
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_RAW)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_HDRINCL, 1); err != nil {
		return err
	}
	addr := &syscall.SockaddrInet4{}
	copy(addr.Addr[:], dst)
	return syscall.Sendto(fd, packet, 0, addr)
}

func checksum(data []byte) uint16 {
	var sum uint32
	for len(data) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(data[:2]))
		data = data[2:]
	}
	if len(data) == 1 {
		sum += uint32(data[0]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func isAdministrator() bool { return os.Geteuid() == 0 }

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func stopOwnedHelper(ctx context.Context, state *State) error { return nil }
