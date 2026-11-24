package serviceconfig

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

var pgPrivateVPNCIDRs = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"100.64.0.0/10",
}

type firewallStep struct {
	args        []string
	ignoreError bool
}

func uniquePorts(ports ...int) []int {
	seen := make(map[int]bool, len(ports))
	var out []int
	for _, p := range ports {
		if p > 0 && p <= 65535 && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func firewalldRichRule(cidr string, port int) string {
	return fmt.Sprintf(`rule family="ipv4" source address="%s" port protocol="tcp" port="%d" accept`, cidr, port)
}

// buildUFWPostgresPlan menyusun daftar perintah ufw untuk menyelaraskan akses remote PostgreSQL.
func buildUFWPostgresPlan(origPort, newPort int, remoteMode string) []firewallStep {
	var steps []firewallStep
	for _, p := range uniquePorts(origPort, newPort) {
		pStr := strconv.Itoa(p)
		steps = append(steps,
			firewallStep{args: []string{"--force", "delete", "allow", pStr + "/tcp"}, ignoreError: true},
			firewallStep{args: []string{"--force", "delete", "allow", "proto", "tcp", "from", "any", "to", "any", "port", pStr}, ignoreError: true},
		)
		for _, cidr := range pgPrivateVPNCIDRs {
			steps = append(steps, firewallStep{
				args:        []string{"--force", "delete", "allow", "from", cidr, "to", "any", "port", pStr, "proto", "tcp"},
				ignoreError: true,
			})
		}
	}

	newPortStr := strconv.Itoa(newPort)
	switch remoteMode {
	case "all":
		steps = append(steps, firewallStep{
			args: []string{"allow", newPortStr + "/tcp", "comment", "wdev PostgreSQL (all)"},
		})
	case "private_vpn":
		for _, cidr := range pgPrivateVPNCIDRs {
			steps = append(steps, firewallStep{
				args: []string{"allow", "from", cidr, "to", "any", "port", newPortStr, "proto", "tcp", "comment", "wdev PostgreSQL (LAN/VPN)"},
			})
		}
	}

	steps = append(steps, firewallStep{args: []string{"reload"}, ignoreError: true})
	return steps
}

// buildFirewalldPostgresPlan menyusun daftar perintah firewall-cmd untuk menyelaraskan akses remote PostgreSQL.
func buildFirewalldPostgresPlan(origPort, newPort int, remoteMode string) []firewallStep {
	var steps []firewallStep
	for _, p := range uniquePorts(origPort, newPort) {
		pStr := strconv.Itoa(p)
		steps = append(steps, firewallStep{
			args:        []string{"--permanent", "--remove-port=" + pStr + "/tcp"},
			ignoreError: true,
		})
		for _, cidr := range pgPrivateVPNCIDRs {
			steps = append(steps, firewallStep{
				args:        []string{"--permanent", "--remove-rich-rule=" + firewalldRichRule(cidr, p)},
				ignoreError: true,
			})
		}
	}

	newPortStr := strconv.Itoa(newPort)
	switch remoteMode {
	case "all":
		steps = append(steps, firewallStep{
			args: []string{"--permanent", "--add-port=" + newPortStr + "/tcp"},
		})
	case "private_vpn":
		for _, cidr := range pgPrivateVPNCIDRs {
			steps = append(steps, firewallStep{
				args: []string{"--permanent", "--add-rich-rule=" + firewalldRichRule(cidr, newPort)},
			})
		}
	}

	steps = append(steps, firewallStep{args: []string{"--reload"}})
	return steps
}

// syncPostgresFirewall menyesuaikan aturan UFW atau firewalld yang sedang aktif
// agar sesuai dengan remote_access_mode dan port PostgreSQL.
func (m *Manager) syncPostgresFirewall(ctx context.Context, origPort, newPort int, remoteMode string) error {
	if newPort <= 0 {
		newPort = 5432
	}

	if _, err := exec.LookPath("ufw"); err == nil {
		if out, sErr := m.runPrivilegedCmd(ctx, "ufw", "status"); sErr == nil && strings.Contains(strings.ToLower(out), "status: active") {
			for _, step := range buildUFWPostgresPlan(origPort, newPort, remoteMode) {
				if _, rErr := m.runPrivilegedCmd(ctx, "ufw", step.args...); rErr != nil && !step.ignoreError {
					return fmt.Errorf("gagal menyelaraskan firewall UFW untuk PostgreSQL: %w", rErr)
				}
			}
			return nil
		}
	}

	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		if out, sErr := m.runPrivilegedCmd(ctx, "firewall-cmd", "--state"); sErr == nil && strings.EqualFold(strings.TrimSpace(out), "running") {
			for _, step := range buildFirewalldPostgresPlan(origPort, newPort, remoteMode) {
				if _, rErr := m.runPrivilegedCmd(ctx, "firewall-cmd", step.args...); rErr != nil && !step.ignoreError {
					return fmt.Errorf("gagal menyelaraskan firewalld untuk PostgreSQL: %w", rErr)
				}
			}
			return nil
		}
	}

	return nil
}
