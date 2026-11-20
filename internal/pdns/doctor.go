package pdns

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

const (
	CheckOK   = "ok"
	CheckWarn = "warn"
	CheckFail = "fail"
)

type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type ZoneDoctorReport struct {
	Zone        string        `json:"zone"`
	Nameservers []string      `json:"nameservers"`
	OK          bool          `json:"ok"`
	Checks      []DoctorCheck `json:"checks"`
}

func (r *ZoneDoctorReport) add(name, status, detail string) {
	r.Checks = append(r.Checks, DoctorCheck{Name: name, Status: status, Detail: detail})
	if status == CheckFail {
		r.OK = false
	}
}

func publicResolver() *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 4 * time.Second}
			return d.DialContext(ctx, "udp", "1.1.1.1:53")
		},
	}
}

func nameserverSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		v = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(v), "."))
		if v != "" {
			set[v] = true
		}
	}
	return set
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func formatSet(set map[string]bool) string {
	list := make([]string, 0, len(set))
	for k := range set {
		list = append(list, k)
	}
	sort.Strings(list)
	return strings.Join(list, ", ")
}

// DoctorZone memeriksa zone PowerDNS: record NS/SOA lokal, delegasi publik,
// glue A, serial authoritative, dan keterjangkauan port 53.
func (m *Manager) DoctorZone(domain string) (ZoneDoctorReport, error) {
	report := ZoneDoctorReport{OK: true}
	zone, err := NormalizeZoneName(domain)
	if err != nil {
		report.OK = false
		report.add("Nama zone", CheckFail, err.Error())
		return report, err
	}
	report.Zone = zone
	report.Nameservers = []string{"ns1." + zone, "ns2." + zone}
	expected := nameserverSet(report.Nameservers)

	conf := m.LoadConfig()
	if !conf.Configured || conf.VPSNetbirdIP == "" {
		report.OK = false
		report.add("Edge Gateway", CheckFail, "VPS Edge Gateway belum dikonfigurasi")
		return report, fmt.Errorf("VPS Edge Gateway belum dikonfigurasi")
	}

	info, err := m.GetZone(zone)
	if err != nil {
		report.OK = false
		report.add("Zone di PowerDNS", CheckFail, err.Error())
		return report, fmt.Errorf("zone %s tidak dapat dibaca: %w", zone, err)
	}
	report.add("Zone di PowerDNS", CheckOK, fmt.Sprintf("zone %s ditemukan", zone))

	var apiNS, apiGlue []string
	var currentSOA soaInfo
	apiSerial := info.Serial
	for _, rr := range info.RRsets {
		switch strings.ToUpper(rr.Type) {
		case "SOA":
			if len(rr.Records) > 0 {
				currentSOA = parseSOAContent(rr.Records[0].Content)
				apiSerial = currentSOA.Serial
			}
		case "NS":
			for _, rec := range rr.Records {
				apiNS = append(apiNS, rec.Content)
			}
		case "A":
			for _, rec := range rr.Records {
				name := strings.ToLower(strings.TrimSuffix(rr.Name, "."))
				if expected[name] {
					apiGlue = append(apiGlue, name+"="+rec.Content)
				}
			}
		}
	}
	if sameSet(nameserverSet(apiNS), expected) {
		report.add("NS di zone", CheckOK, formatSet(nameserverSet(apiNS)))
	} else {
		report.add("NS di zone", CheckFail, fmt.Sprintf("diharapkan %s, terbaca %s", formatSet(expected), formatSet(nameserverSet(apiNS))))
	}

	if broken, detail := report.soaBroken(currentSOA); broken {
		report.add("SOA zone", CheckFail, detail+" (jalankan: wdev pdns zone repair "+zone+")")
	} else {
		report.add("SOA zone", CheckOK, fmt.Sprintf("primary %s, serial %d", currentSOA.Primary, currentSOA.Serial))
	}

	publicIP := strings.TrimSpace(conf.VPSPublicIP)
	if publicIP == "" {
		report.add("IP publik VPS", CheckWarn, "belum tercatat di edge.conf")
	} else {
		glueOK := true
		for _, host := range report.Nameservers {
			found := false
			for _, g := range apiGlue {
				parts := strings.SplitN(g, "=", 2)
				if len(parts) == 2 && parts[0] == host && parts[1] == publicIP {
					found = true
					break
				}
			}
			if !found {
				glueOK = false
			}
		}
		if glueOK {
			report.add("Glue A (zone)", CheckOK, "ns1/ns2 mengarah ke "+publicIP)
		} else {
			report.add("Glue A (zone)", CheckFail, fmt.Sprintf("glue ns1/ns2 belum mengarah ke %s (terbaca: %s)", publicIP, strings.Join(apiGlue, ", ")))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	res := publicResolver()

	publicNS, err := res.LookupNS(ctx, zone)
	if err != nil {
		report.add("Delegasi publik (NS)", CheckWarn, "belum terbaca dari 1.1.1.1: "+err.Error())
	} else {
		got := make([]string, 0, len(publicNS))
		for _, ns := range publicNS {
			got = append(got, ns.Host)
		}
		if sameSet(nameserverSet(got), expected) {
			report.add("Delegasi publik (NS)", CheckOK, "registrar sudah mengarah ke ns1/ns2")
		} else {
			report.add("Delegasi publik (NS)", CheckWarn, fmt.Sprintf("diharapkan %s, terbaca %s (propagasi/registrar)", formatSet(expected), formatSet(nameserverSet(got))))
		}
	}

	for _, host := range report.Nameservers {
		ips, err := res.LookupIP(ctx, "ip4", host)
		if err != nil || len(ips) == 0 {
			report.add("Glue publik "+host, CheckWarn, "belum resolve dari publik")
			continue
		}
		if publicIP != "" && ips[0].String() == publicIP {
			report.add("Glue publik "+host, CheckOK, ips[0].String())
		} else if publicIP != "" {
			report.add("Glue publik "+host, CheckWarn, fmt.Sprintf("terbaca %s, diharapkan %s", ips[0], publicIP))
		}
	}

	// Serial SOA authoritative harus >= serial PowerDNS. Query lewat NetBird
	// lebih andal daripada IP publik (NAT hairpin bisa memblokir UDP).
	authTargets := []string{}
	if conf.VPSNetbirdIP != "" {
		authTargets = append(authTargets, conf.VPSNetbirdIP)
	}
	if publicIP != "" && publicIP != conf.VPSNetbirdIP {
		authTargets = append(authTargets, publicIP)
	}
	authIP := ""
	var lastErr error
	var serial uint32
	for _, target := range authTargets {
		serial, lastErr = querySOASerial(net.JoinHostPort(target, "53"), zone, 4*time.Second)
		if lastErr == nil {
			authIP = target
			break
		}
	}
	if lastErr != nil {
		report.add("SOA authoritative", CheckWarn, "query SOA gagal dari mesin ini ("+lastErr.Error()+"); cek dari jaringan lain")
	} else if apiSerial > 0 && int64(serial) < apiSerial {
		report.add("SOA authoritative", CheckWarn, fmt.Sprintf("serial DNS %d < PowerDNS %d; tunggu propagasi/notify", serial, apiSerial))
	} else {
		report.add("SOA authoritative", CheckOK, fmt.Sprintf("serial %d", serial))
	}

	// Port 53 TCP.
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(authIP, "53"), 4*time.Second)
	if err != nil {
		report.add("Port 53", CheckWarn, "TCP 53 tidak terjangkau dari mesin ini: "+err.Error())
	} else {
		_ = conn.Close()
		report.add("Port 53", CheckOK, "TCP 53 terjangkau")
	}

	return report, nil
}
