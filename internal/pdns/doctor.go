package pdns

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
	"wdev/internal/i18n"
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

// DoctorZone memeriksa zone PowerDNS menggunakan locale default (English).
func (m *Manager) DoctorZone(domain string) (ZoneDoctorReport, error) {
	return m.DoctorZoneLocale(domain, i18n.DefaultLocale)
}

// DoctorZoneLocale memeriksa zone PowerDNS: record NS/SOA lokal, delegasi publik,
// glue A, serial authoritative, dan keterjangkauan port 53 dengan output terlokalisasi.
func (m *Manager) DoctorZoneLocale(domain, lang string) (ZoneDoctorReport, error) {
	report := ZoneDoctorReport{OK: true}
	zone, err := NormalizeZoneName(domain)
	if err != nil {
		report.OK = false
		report.add(i18n.Pick(lang, "Zone name", "Nama zone"), CheckFail, err.Error())
		return report, err
	}
	report.Zone = zone
	report.Nameservers = []string{"ns1." + zone, "ns2." + zone}
	expected := nameserverSet(report.Nameservers)

	conf := m.LoadConfig()
	if !conf.Configured || conf.VPSNetbirdIP == "" {
		report.OK = false
		msg := i18n.Pick(lang, "VPS Edge Gateway is not configured", "VPS Edge Gateway belum dikonfigurasi")
		if conf.Mode != "edge" {
			msg = i18n.Pick(lang,
				"Local PowerDNS is not installed or configured (run: wdev install pdns)",
				"PowerDNS lokal belum terpasang atau belum dikonfigurasi (jalankan: wdev install pdns)",
			)
		}
		report.add("PowerDNS API", CheckFail, msg)
		return report, fmt.Errorf("%s", msg)
	}

	info, err := m.GetZone(zone)
	if err != nil {
		report.OK = false
		report.add(i18n.Pick(lang, "Zone in PowerDNS", "Zone di PowerDNS"), CheckFail, err.Error())
		return report, fmt.Errorf(i18n.Pick(lang, "cannot read zone %s: %w", "zone %s tidak dapat dibaca: %w"), zone, err)
	}
	report.add(
		i18n.Pick(lang, "Zone in PowerDNS", "Zone di PowerDNS"),
		CheckOK,
		fmt.Sprintf(i18n.Pick(lang, "zone %s found", "zone %s ditemukan"), zone),
	)

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
		report.add(i18n.Pick(lang, "Zone NS", "NS di zone"), CheckOK, formatSet(nameserverSet(apiNS)))
	} else {
		report.add(
			i18n.Pick(lang, "Zone NS", "NS di zone"),
			CheckFail,
			fmt.Sprintf(i18n.Pick(lang, "expected %s, got %s", "diharapkan %s, terbaca %s"), formatSet(expected), formatSet(nameserverSet(apiNS))),
		)
	}

	if broken, detail := report.soaBrokenLocale(currentSOA, lang); broken {
		report.add(
			i18n.Pick(lang, "Zone SOA", "SOA zone"),
			CheckFail,
			detail+i18n.Pick(lang, " (run: wdev pdns zone repair "+zone+")", " (jalankan: wdev pdns zone repair "+zone+")"),
		)
	} else {
		report.add(
			i18n.Pick(lang, "Zone SOA", "SOA zone"),
			CheckOK,
			fmt.Sprintf("primary %s, serial %d", currentSOA.Primary, currentSOA.Serial),
		)
	}

	publicIP := strings.TrimSpace(conf.VPSPublicIP)
	if conf.Mode == "vps" {
		if !IsPublicIPv4String(publicIP) {
			publicIP = DetectPublicIP()
		}
		if !IsPublicIPv4String(publicIP) {
			publicIP = ""
		}
	}
	if publicIP == "" {
		msg := i18n.Pick(lang, "not recorded in edge.conf yet", "belum tercatat di edge.conf")
		if conf.Mode == "vps" {
			msg = i18n.Pick(lang,
				"valid VPS public IP not detected; ensure VPS_PUBLIC_IP in edge-dns.conf is set to a public IP",
				"IP publik VPS valid belum terdeteksi; pastikan VPS_PUBLIC_IP di edge-dns.conf terisi IP publik",
			)
		}
		report.add(i18n.Pick(lang, "VPS Public IP", "IP publik VPS"), CheckWarn, msg)
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
			report.add(i18n.Pick(lang, "Glue A (zone)", "Glue A (zone)"), CheckOK, i18n.Pick(lang, "ns1/ns2 point to ", "ns1/ns2 mengarah ke ")+publicIP)
		} else {
			report.add(
				i18n.Pick(lang, "Glue A (zone)", "Glue A (zone)"),
				CheckFail,
				fmt.Sprintf(
					i18n.Pick(lang, "ns1/ns2 glue does not point to %s (got: %s)", "glue ns1/ns2 belum mengarah ke %s (terbaca: %s)"),
					publicIP, strings.Join(apiGlue, ", "),
				),
			)
		}
	}

	if conf.Mode == "local" {
		report.add(
			i18n.Pick(lang, "Public Delegation (NS)", "Delegasi publik (NS)"),
			CheckOK,
			i18n.Pick(lang, "local standalone mode: public registrar delegation is optional", "mode lokal standalone: delegasi registrar publik tidak wajib"),
		)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		res := publicResolver()

		publicNS, err := res.LookupNS(ctx, zone)
		if err != nil {
			report.add(
				i18n.Pick(lang, "Public Delegation (NS)", "Delegasi publik (NS)"),
				CheckWarn,
				i18n.Pick(lang, "not resolvable from 1.1.1.1 yet: ", "belum terbaca dari 1.1.1.1: ")+err.Error(),
			)
		} else {
			got := make([]string, 0, len(publicNS))
			for _, ns := range publicNS {
				got = append(got, ns.Host)
			}
			if sameSet(nameserverSet(got), expected) {
				report.add(
					i18n.Pick(lang, "Public Delegation (NS)", "Delegasi publik (NS)"),
					CheckOK,
					i18n.Pick(lang, "registrar already points to ns1/ns2", "registrar sudah mengarah ke ns1/ns2"),
				)
			} else {
				report.add(
					i18n.Pick(lang, "Public Delegation (NS)", "Delegasi publik (NS)"),
					CheckWarn,
					fmt.Sprintf(
						i18n.Pick(lang, "expected %s, got %s (propagation/registrar)", "diharapkan %s, terbaca %s (propagasi/registrar)"),
						formatSet(expected), formatSet(nameserverSet(got)),
					),
				)
			}
		}

		for _, host := range report.Nameservers {
			label := i18n.Pick(lang, "Public glue ", "Glue publik ") + host
			ips, err := res.LookupIP(ctx, "ip4", host)
			if err != nil || len(ips) == 0 {
				report.add(label, CheckWarn, i18n.Pick(lang, "not resolving publicly yet", "belum resolve dari publik"))
				continue
			}
			if publicIP == "" {
				report.add(
					label,
					CheckWarn,
					fmt.Sprintf(i18n.Pick(lang, "resolved %s (VPS public IP not detected for verification)", "terbaca %s (IP publik VPS belum terdeteksi untuk verifikasi)"), ips[0]),
				)
				continue
			}
			matched := false
			for _, ip := range ips {
				if ip.String() == publicIP {
					matched = true
					break
				}
			}
			if matched {
				report.add(label, CheckOK, publicIP)
			} else {
				report.add(
					label,
					CheckWarn,
					fmt.Sprintf(i18n.Pick(lang, "resolved %s, expected %s", "terbaca %s, diharapkan %s"), ips[0], publicIP),
				)
			}
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
		report.add(
			i18n.Pick(lang, "Authoritative SOA", "SOA authoritative"),
			CheckWarn,
			fmt.Sprintf(i18n.Pick(lang, "SOA query failed from this machine (%s); check from another network", "query SOA gagal dari mesin ini (%s); cek dari jaringan lain"), lastErr.Error()),
		)
	} else if apiSerial > 0 && int64(serial) < apiSerial {
		report.add(
			i18n.Pick(lang, "Authoritative SOA", "SOA authoritative"),
			CheckWarn,
			fmt.Sprintf(i18n.Pick(lang, "DNS serial %d < PowerDNS %d; wait for propagation/notify", "serial DNS %d < PowerDNS %d; tunggu propagasi/notify"), serial, apiSerial),
		)
	} else {
		report.add(i18n.Pick(lang, "Authoritative SOA", "SOA authoritative"), CheckOK, fmt.Sprintf("serial %d", serial))
	}

	// Port 53 TCP.
	port53Target := authIP
	if port53Target == "" && len(authTargets) > 0 {
		port53Target = authTargets[0]
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(port53Target, "53"), 4*time.Second)
	if err != nil {
		report.add(
			"Port 53",
			CheckWarn,
			i18n.Pick(lang, "TCP 53 unreachable from this machine: ", "TCP 53 tidak terjangkau dari mesin ini: ")+err.Error(),
		)
	} else {
		_ = conn.Close()
		report.add("Port 53", CheckOK, i18n.Pick(lang, "TCP 53 reachable", "TCP 53 terjangkau"))
	}

	return report, nil
}
