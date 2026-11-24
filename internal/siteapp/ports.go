package siteapp

import (
	"fmt"
	"net"
)

const (
	DefaultPortMin = 3100
	DefaultPortMax = 39999
)

// SuggestFreePort mencari port TCP IPv4 yang tersedia pada host mulai dari 3100,
// sambil melewati daftar port yang sedang dipakai atau dipesan oleh layanan wdev.
func SuggestFreePort(usedPorts, reservedPorts map[int]bool) (int, error) {
	for port := DefaultPortMin; port <= DefaultPortMax; port++ {
		if usedPorts != nil && usedPorts[port] {
			continue
		}
		if reservedPorts != nil && reservedPorts[port] {
			continue
		}
		if isPortFreeForLoopback(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("tidak ditemukan port lokal yang tersedia pada rentang %d-%d", DefaultPortMin, DefaultPortMax)
}

func isPortFreeForLoopback(port int) bool {
	ln, err := net.Listen("tcp4", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
