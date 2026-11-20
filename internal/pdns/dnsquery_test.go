package pdns

import (
	"encoding/binary"
	"testing"
)

func TestBuildAndParseDNSQueryRoundTrip(t *testing.T) {
	msg, err := buildDNSQuery(0x1234, "example.com", 6)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(msg[0:2]) != 0x1234 {
		t.Fatalf("id salah: %x", msg[0:2])
	}
	if binary.BigEndian.Uint16(msg[4:6]) != 1 {
		t.Fatalf("qdcount salah")
	}
	name, next, err := readDNSName(msg, 12)
	if err != nil || name != "example.com" {
		t.Fatalf("readDNSName = %q,%v", name, err)
	}
	if binary.BigEndian.Uint16(msg[next:next+2]) != 6 {
		t.Fatalf("qtype salah")
	}
}

func TestReadDNSNameCompression(t *testing.T) {
	// "example.com" di offset 0, lalu pointer ke offset 0.
	msg := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		3, 'w', 'w', 'w', 0xC0, 0x00}
	name, next, err := readDNSName(msg, 13)
	if err != nil {
		t.Fatal(err)
	}
	if name != "www.example.com" {
		t.Fatalf("nama = %q, want www.example.com", name)
	}
	if next != 19 {
		t.Fatalf("next = %d, want 19", next)
	}
}

func TestParseDNSResponseSOA(t *testing.T) {
	// Header + question + satu answer SOA minimal.
	qname, _ := encodeDNSName("example.com")
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	binary.BigEndian.PutUint16(msg[6:8], 1)
	msg = append(msg, qname...)
	msg = binary.BigEndian.AppendUint16(msg, 6)
	msg = binary.BigEndian.AppendUint16(msg, 1)

	msg = append(msg, qname...)
	msg = binary.BigEndian.AppendUint16(msg, 6)
	msg = binary.BigEndian.AppendUint16(msg, 1)
	msg = binary.BigEndian.AppendUint32(msg, 300)

	var rdata []byte
	mname, _ := encodeDNSName("ns1.example.com")
	rname, _ := encodeDNSName("hostmaster.example.com")
	rdata = append(rdata, mname...)
	rdata = append(rdata, rname...)
	rdata = binary.BigEndian.AppendUint32(rdata, 2026112001)
	rdata = binary.BigEndian.AppendUint32(rdata, 3600)
	rdata = binary.BigEndian.AppendUint32(rdata, 600)
	rdata = binary.BigEndian.AppendUint32(rdata, 86400)
	rdata = binary.BigEndian.AppendUint32(rdata, 300)

	msg = binary.BigEndian.AppendUint16(msg, uint16(len(rdata)))
	msg = append(msg, rdata...)

	rrs, err := parseDNSResponse(msg, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(rrs) != 1 || rrs[0].Serial != 2026112001 {
		t.Fatalf("SOA serial salah: %+v", rrs)
	}
}

func TestParseDNSResponseA(t *testing.T) {
	qname, _ := encodeDNSName("mail.example.com")
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	binary.BigEndian.PutUint16(msg[6:8], 1)
	msg = append(msg, qname...)
	msg = binary.BigEndian.AppendUint16(msg, 1)
	msg = binary.BigEndian.AppendUint16(msg, 1)
	msg = append(msg, qname...)
	msg = binary.BigEndian.AppendUint16(msg, 1)
	msg = binary.BigEndian.AppendUint16(msg, 1)
	msg = binary.BigEndian.AppendUint32(msg, 300)
	msg = binary.BigEndian.AppendUint16(msg, 4)
	msg = append(msg, 203, 0, 113, 10)

	rrs, err := parseDNSResponse(msg, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rrs) != 1 || rrs[0].IP.String() != "203.0.113.10" {
		t.Fatalf("A record salah: %+v", rrs)
	}
}

func TestSameSet(t *testing.T) {
	a := nameserverSet([]string{"ns1.example.com.", "ns2.example.com"})
	b := nameserverSet([]string{"ns2.example.com.", "NS1.example.com"})
	if !sameSet(a, b) {
		t.Fatal("set NS ekuivalen harus sama")
	}
	if sameSet(a, nameserverSet([]string{"ns1.example.com"})) {
		t.Fatal("jumlah berbeda harus false")
	}
}

func TestParseSOAContent(t *testing.T) {
	info := parseSOAContent("ns1.example.com. hostmaster.example.com. 2026112003 10800 3600 604800 3600")
	if info.Primary != "ns1.example.com" || info.Serial != 2026112003 {
		t.Fatalf("parse SOA salah: %+v", info)
	}
}

func TestParseSOAContentBroken(t *testing.T) {
	info := parseSOAContent("a.misconfigured.dns.server.invalid. hostmaster.rtzsoft.com. 0 10800 3600 604800 3600")
	report := ZoneDoctorReport{OK: true}
	broken, detail := report.soaBroken(info)
	if !broken {
		t.Fatal("SOA misconfigured harus terdeteksi")
	}
	if !contains(detail, "misconfigured") {
		t.Fatalf("detail tidak informatif: %s", detail)
	}

	zero := parseSOAContent("ns1.example.com. hostmaster.example.com. 0 10800 3600 604800 3600")
	if broken, _ := report.soaBroken(zero); !broken {
		t.Fatal("serial 0 harus terdeteksi")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOfStr(s, sub) >= 0)
}

func indexOfStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
