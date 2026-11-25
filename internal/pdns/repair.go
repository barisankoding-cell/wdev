package pdns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"wdev/internal/i18n"
)

// soaInfo adalah hasil parsing record SOA PowerDNS.
type soaInfo struct {
	Primary string
	Serial  int64
	Content string
}

func parseSOAContent(content string) soaInfo {
	fields := strings.Fields(strings.TrimSpace(content))
	info := soaInfo{Content: strings.TrimSpace(content)}
	if len(fields) > 0 {
		info.Primary = strings.ToLower(strings.TrimSuffix(fields[0], "."))
	}
	if len(fields) > 2 {
		info.Serial, _ = strconv.ParseInt(fields[2], 10, 64)
	}
	return info
}

func (z ZoneDoctorReport) soaBroken(info soaInfo) (bool, string) {
	return z.soaBrokenLocale(info, i18n.DefaultLocale)
}

func (z ZoneDoctorReport) soaBrokenLocale(info soaInfo, lang string) (bool, string) {
	if info.Primary == "" {
		return true, i18n.Pick(lang, "SOA record not found", "record SOA tidak ditemukan")
	}
	if strings.Contains(info.Primary, "misconfigured") {
		return true, i18n.Pick(
			lang,
			"SOA primary is still "+info.Primary+" (legacy zone created without nameservers)",
			"SOA primary masih "+info.Primary+" (zone lama dibuat tanpa nameserver)",
		)
	}
	if info.Serial == 0 {
		return true, i18n.Pick(lang, "SOA serial is still 0", "serial SOA masih 0")
	}
	return false, ""
}

// setZoneMetadata menulis metadata zone. Sebagian kind (SOA-EDIT-API,
// API-RECTIFY) bersifat read-only dan diturunkan dari SOA-EDIT, sehingga 422
// untuk kind tersebut bukan kegagalan.
func (m *Manager) setZoneMetadata(zone, kind string, values []string) error {
	conf := m.LoadConfig()
	body, err := json.Marshal(map[string]any{"kind": kind, "metadata": values})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s:%s/api/v1/servers/localhost/zones/%s/metadata/%s",
		conf.VPSNetbirdIP, conf.PDNSPort, strings.TrimSuffix(zone, ".")+".", kind)
	client := http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", conf.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	detail := strings.TrimSpace(string(msg))
	if resp.StatusCode == http.StatusUnprocessableEntity && strings.Contains(detail, "Unsupported metadata kind") {
		// Kind read-only: nilai efektif mengikuti SOA-EDIT.
		return nil
	}
	return fmt.Errorf("set metadata %s gagal: status %d %s", kind, resp.StatusCode, detail)
}

// RepairZone memperbaiki zone lama using the default locale (English).
func (m *Manager) RepairZone(domain string) (ZoneDoctorReport, error) {
	return m.RepairZoneLocale(domain, i18n.DefaultLocale)
}

// RepairZoneLocale memperbaiki zone lama: NS, glue A, SOA primary/serial, dan
// SOA-EDIT-API. Aman dijalankan berulang dengan pesan terlokalisasi.
func (m *Manager) RepairZoneLocale(domain, lang string) (ZoneDoctorReport, error) {
	report, _ := m.DoctorZoneLocale(domain, lang)
	zone, err := NormalizeZoneName(domain)
	if err != nil {
		return report, err
	}
	conf := m.LoadConfig()
	targetIP := strings.TrimSpace(conf.VPSPublicIP)
	switch conf.Mode {
	case "local":
		if targetIP == "" {
			targetIP = DetectLocalLANIP()
			if targetIP == "" {
				targetIP = "127.0.0.1"
			}
		}
	case "vps":
		if !IsPublicIPv4String(targetIP) {
			targetIP = DetectPublicIP()
		}
		if !IsPublicIPv4String(targetIP) {
			return report, fmt.Errorf("%s", i18n.Pick(lang,
				"valid VPS public IP not detected; set VPS_PUBLIC_IP in DNS config",
				"IP publik VPS valid belum terdeteksi; atur VPS_PUBLIC_IP di konfigurasi DNS",
			))
		}
		if os.Geteuid() == 0 && conf.HasAPIKey && !IsPublicIPv4String(m.readEdgeDNSFilePublicIP()) {
			_ = m.SaveConfig(conf.VPSNetbirdIP, targetIP, conf.APIKey, conf.PDNSPort, conf.AgentPort)
		}
	default:
		if targetIP == "" {
			return report, fmt.Errorf("%s", i18n.Pick(lang,
				"VPS public IP not recorded; run 'wdev pdns connect'",
				"IP publik VPS belum tercatat; jalankan 'wdev pdns connect'",
			))
		}
	}

	info, err := m.GetZone(zone)
	if err != nil {
		return report, fmt.Errorf(i18n.Pick(lang, "cannot read zone %s: %w", "zone %s tidak dapat dibaca: %w"), zone, err)
	}
	var current soaInfo
	for _, rr := range info.RRsets {
		if strings.EqualFold(rr.Type, "SOA") && len(rr.Records) > 0 {
			current = parseSOAContent(rr.Records[0].Content)
			break
		}
	}
	if current.Serial < 0 {
		current.Serial = 0
	}
	serial := time.Now().Unix()
	if current.Serial >= serial {
		serial = current.Serial + 1
	}
	if current.Serial == 0 {
		serial = time.Now().Unix()
	}

	ns1 := "ns1." + zone
	ns2 := "ns2." + zone
	soa := fmt.Sprintf("%s. hostmaster.%s. %d 10800 3600 604800 3600", ns1, zone, serial)

	if err := m.patchRecords(zone, "@", "SOA", []string{soa}, 3600); err != nil {
		return report, fmt.Errorf("gagal memperbaiki SOA: %w", err)
	}
	if err := m.patchRecords(zone, "@", "NS", []string{ns1 + ".", ns2 + "."}, 3600); err != nil {
		return report, fmt.Errorf("gagal memperbaiki NS: %w", err)
	}
	if err := m.patchRecords(zone, ns1, "A", []string{targetIP}, 300); err != nil {
		return report, fmt.Errorf("gagal memperbaiki glue %s: %w", ns1, err)
	}
	if err := m.patchRecords(zone, ns2, "A", []string{targetIP}, 300); err != nil {
		return report, fmt.Errorf("gagal memperbaiki glue %s: %w", ns2, err)
	}
	if err := m.setZoneMetadata(zone, "SOA-EDIT", []string{"INCEPTION-INCREMENT"}); err != nil {
		return report, err
	}

	final, err := m.DoctorZone(zone)
	if err != nil {
		return final, err
	}
	return final, nil
}
