package pdns

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"
)

// dnsRR adalah jawaban DNS minimal yang dibutuhkan dokter zone.
type dnsRR struct {
	Name   string
	Type   uint16
	TTL    uint32
	Target string
	IP     net.IP
	Serial uint32
}

func encodeDNSName(name string) ([]byte, error) {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	if name == "" {
		return []byte{0}, nil
	}
	var out []byte
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, fmt.Errorf("label DNS tidak valid: %q", label)
		}
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	return append(out, 0), nil
}

func buildDNSQuery(id uint16, name string, qtype uint16) ([]byte, error) {
	qname, err := encodeDNSName(name)
	if err != nil {
		return nil, err
	}
	msg := make([]byte, 12, 12+len(qname)+4)
	binary.BigEndian.PutUint16(msg[0:2], id)
	binary.BigEndian.PutUint16(msg[2:4], 0x0100) // RD
	binary.BigEndian.PutUint16(msg[4:6], 1)      // QDCOUNT
	msg = append(msg, qname...)
	msg = binary.BigEndian.AppendUint16(msg, qtype)
	msg = binary.BigEndian.AppendUint16(msg, 1) // IN
	return msg, nil
}

// readDNSName membaca nama pada offset, mengikuti compression pointer.
func readDNSName(msg []byte, off int) (string, int, error) {
	var labels []string
	next := -1
	for jumps := 0; jumps < 32; jumps++ {
		if off >= len(msg) {
			return "", 0, fmt.Errorf("nama DNS terpotong")
		}
		l := int(msg[off])
		if l == 0 {
			if next < 0 {
				next = off + 1
			}
			return strings.Join(labels, "."), next, nil
		}
		if l&0xC0 == 0xC0 {
			if off+1 >= len(msg) {
				return "", 0, fmt.Errorf("pointer DNS terpotong")
			}
			ptr := int(binary.BigEndian.Uint16(msg[off:off+2]) & 0x3FFF)
			if next < 0 {
				next = off + 2
			}
			off = ptr
			continue
		}
		if off+1+l > len(msg) {
			return "", 0, fmt.Errorf("label DNS terpotong")
		}
		labels = append(labels, string(msg[off+1:off+1+l]))
		off += 1 + l
	}
	return "", 0, fmt.Errorf("terlalu banyak pointer DNS")
}

func parseDNSResponse(msg []byte, wantType uint16) ([]dnsRR, error) {
	if len(msg) < 12 {
		return nil, fmt.Errorf("respons DNS terlalu pendek")
	}
	qd := int(binary.BigEndian.Uint16(msg[4:6]))
	an := int(binary.BigEndian.Uint16(msg[6:8]))
	off := 12
	for i := 0; i < qd; i++ {
		_, next, err := readDNSName(msg, off)
		if err != nil {
			return nil, err
		}
		off = next + 4
		if off > len(msg) {
			return nil, fmt.Errorf("question DNS terpotong")
		}
	}
	var out []dnsRR
	for i := 0; i < an; i++ {
		name, next, err := readDNSName(msg, off)
		if err != nil {
			return nil, err
		}
		off = next
		if off+10 > len(msg) {
			return nil, fmt.Errorf("answer DNS terpotong")
		}
		typ := binary.BigEndian.Uint16(msg[off : off+2])
		ttl := binary.BigEndian.Uint32(msg[off+4 : off+8])
		rdlen := int(binary.BigEndian.Uint16(msg[off+8 : off+10]))
		off += 10
		if off+rdlen > len(msg) {
			return nil, fmt.Errorf("rdata DNS terpotong")
		}
		rr := dnsRR{Name: name, Type: typ, TTL: ttl}
		switch typ {
		case 1: // A
			if rdlen == 4 {
				rr.IP = net.IPv4(msg[off], msg[off+1], msg[off+2], msg[off+3])
			}
		case 2, 5, 12: // NS, CNAME, PTR
			if target, _, err := readDNSName(msg, off); err == nil {
				rr.Target = target
			}
		case 6: // SOA
			_, o, err := readDNSName(msg, off)
			if err != nil {
				return nil, err
			}
			_, o, err = readDNSName(msg, o)
			if err != nil {
				return nil, err
			}
			if o+4 <= off+rdlen {
				rr.Serial = binary.BigEndian.Uint32(msg[o : o+4])
			}
		}
		if typ == wantType {
			out = append(out, rr)
		}
		off += rdlen
	}
	return out, nil
}

func dnsExchange(server, name string, qtype uint16, timeout time.Duration) ([]dnsRR, error) {
	if !strings.Contains(server, ":") {
		server = net.JoinHostPort(server, "53")
	}
	query, err := buildDNSQuery(uint16(time.Now().UnixNano()), name, qtype)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("udp", server, timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	msg := buf[:n]
	if msg[2]&0x02 != 0 { // TC -> coba TCP
		return dnsExchangeTCP(server, query, qtype, timeout)
	}
	return parseDNSResponse(msg, qtype)
}

func dnsExchangeTCP(server string, query []byte, qtype uint16, timeout time.Duration) ([]dnsRR, error) {
	conn, err := net.DialTimeout("tcp", server, timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	frame := make([]byte, 2, 2+len(query))
	binary.BigEndian.PutUint16(frame[0:2], uint16(len(query)))
	frame = append(frame, query...)
	if _, err := conn.Write(frame); err != nil {
		return nil, err
	}
	var lenBuf [2]byte
	if _, err := readFull(conn, lenBuf[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(lenBuf[:]))
	resp := make([]byte, size)
	if _, err := readFull(conn, resp); err != nil {
		return nil, err
	}
	return parseDNSResponse(resp, qtype)
}

func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func querySOASerial(server, zone string, timeout time.Duration) (uint32, error) {
	records, err := dnsExchange(server, zone, 6, timeout)
	if err != nil {
		return 0, err
	}
	for _, rr := range records {
		if rr.Type == 6 {
			return rr.Serial, nil
		}
	}
	return 0, fmt.Errorf("SOA tidak ditemukan pada respons %s", server)
}
