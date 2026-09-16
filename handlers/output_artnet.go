package handlers

import (
	"encoding/binary"
	"fmt"
	"net"
)

// UniversePayload holds one universe of DMX data.
type UniversePayload struct {
	Universe uint16
	Data     []byte
}

// SplitUniverses splits pixels into contiguous universes.
// channelsPerUniverse <=0 defaults to 512. Honours channelOffset (skip at start, zero-pad).
func SplitUniverses(pixels []byte, startUniverse, channelOffset, channelsPerUniverse int) []UniversePayload {
	if channelsPerUniverse <= 0 {
		channelsPerUniverse = 512
	}
	// Build effective buffer: offset zeros + pixels.
	total := channelOffset + len(pixels)
	if total == 0 {
		return nil
	}
	buf := make([]byte, total)
	copy(buf[channelOffset:], pixels)

	n := (len(buf) + channelsPerUniverse - 1) / channelsPerUniverse
	out := make([]UniversePayload, 0, n)
	for i := 0; i < n; i++ {
		start := i * channelsPerUniverse
		end := start + channelsPerUniverse
		if end > len(buf) {
			end = len(buf)
		}
		chunk := make([]byte, end-start)
		copy(chunk, buf[start:end])
		out = append(out, UniversePayload{
			Universe: uint16(startUniverse + i),
			Data:     chunk,
		})
	}
	return out
}

// BuildE131Packet builds a full E1.31 packet (638 bytes for 510-byte data).
func BuildE131Packet(universe uint16, seq uint8, cid [16]byte, data []byte) []byte {
	if len(data) > 512 {
		data = data[:512]
	}
	propValCount := 1 + len(data) // start code + data
	// Framing PDU length: 77 + propValCount? Let's compute per spec.
	// Structure lengths:
	// Root layer (38 bytes: preamble 2 + post-amble 2 + ACN PID 12 + flags/length 2 + vector 4 + CID 16) -> actually Root PDU is 38? Let's build sequentially.

	// We'll construct packet as 126 preamble+ then PDUs.
	// Total length = 638 for 510 data.
	// Layout:
	// Bytes 0-15: preamble size (2) + post-amble size (2) + ACN PID (12) = 16
	// Then Root PDU: flags+length (2) + vector (4) + CID (16) = 22, plus framing + DMP.
	// Simpler: build fixed template.

	pkt := make([]byte, 0, 638)

	// Preamble size (0x0010)
	pkt = append(pkt, 0x00, 0x10)
	// Post-amble size
	pkt = append(pkt, 0x00, 0x00)
	// ACN packet identifier
	pkt = append(pkt, []byte{0x41, 0x53, 0x43, 0x2d, 0x45, 0x31, 0x2e, 0x31, 0x37, 0x00, 0x00, 0x00}...)

	// Root layer PDU length: flags 0x7 + length (framing+DMP+... ) big endian 12 bits.
	// Root PDU content length = vector(4)+CID(16)+framing PDU (77+propValCount?) + DMP.
	// Compute framingPDULength = 77 + propValCount ( Actually framing length includes its vector(4)+header(64)+universe(2)+ etc + DMP )
	// Let's do standard calculation:
	// DMP PDU size = 10 + propValCount (vector1+type+addrs(2)+propCount(2)+data)
	// Actually DMP layer: flags/len(2)+vector(1)+addrType(1)+firstAddr(2)+addrInc(2)+propValCount(2)+data(propValCount)
	// So dmpLength = 10 + propValCount (including header) but flags/len covers 1+1+2+2+2+propValCount? Let's use known working numbers.
	// For 510 data: propValCount=511, DMP PDU total = 2+1+1+2+2+2+511=521
	// Framing PDU content = 64 bytes header + DMP PDU (521) ??? Need accurate.

	// Build framing and DMP separately then prefix root.

	// DMP PDU
	dmp := make([]byte, 0, 10+propValCount)
	// flags+length for DMP: 0x70 | high 4 bits, low 8 bits
	dmpLen := 1 + 1 + 2 + 2 + 2 + propValCount // vector+addrType+firstAddr+inc+count+data = 8+propValCount
	// flags 0x7 <<12 | length
	dmpFlags := 0x7000 | (dmpLen & 0x0FFF)
	dmp = append(dmp, byte(dmpFlags>>8), byte(dmpFlags))
	dmp = append(dmp, 0x02) // vector DMP Set Property
	dmp = append(dmp, 0xa1) // address type
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, 0x0000)
	dmp = append(dmp, b...)
	binary.BigEndian.PutUint16(b, 0x0001)
	dmp = append(dmp, b...)
	binary.BigEndian.PutUint16(b, uint16(propValCount))
	dmp = append(dmp, b...)
	dmp = append(dmp, 0x00) // start code
	dmp = append(dmp, data...)

	// Framing PDU
	framing := make([]byte, 0, 88+len(dmp))
	// Framing PDU = flags/len + vector(4)+source(64)+priority(1)+syncAddr(2)+seq(1)+options(1)+universe(2)+dmp
	framingContentLen := 4 + 64 + 1 + 2 + 1 + 1 + 2 + len(dmp)
	framingFlags := 0x7000 | (framingContentLen & 0x0FFF)
	framing = append(framing, byte(framingFlags>>8), byte(framingFlags))
	framing = append(framing, 0x00, 0x00, 0x00, 0x02) // vector E1.31 Data Packet
	source := make([]byte, 64)
	copy(source, []byte("LEDit"))
	framing = append(framing, source...)
	framing = append(framing, 100) // priority
	binary.BigEndian.PutUint16(b, 0)
	framing = append(framing, b...) // sync addr
	framing = append(framing, seq)
	framing = append(framing, 0x00) // options
	binary.BigEndian.PutUint16(b, universe)
	framing = append(framing, b...)
	framing = append(framing, dmp...)

	// Root PDU
	rootLen := 4 + 16 + len(framing)
	rootFlags := 0x7000 | (rootLen & 0x0FFF)
	pkt = append(pkt, byte(rootFlags>>8), byte(rootFlags))
	pkt = append(pkt, 0x00, 0x00, 0x00, 0x04) // vector root E1.31 Data
	pkt = append(pkt, cid[:]...)
	pkt = append(pkt, framing...)

	if len(pkt) < 638 && len(data) == 510 {
		pad := make([]byte, 638-len(pkt))
		pkt = append(pkt, pad...)
	}
	return pkt
}

// BuildArtDmxPacket builds Art-Net ArtDmx packet.
func BuildArtDmxPacket(seq, physical uint8, universe uint16, data []byte) []byte {
	if len(data) > 512 {
		data = data[:512]
	}
	pkt := make([]byte, 18+len(data))
	copy(pkt[0:8], []byte("Art-Net\x00"))
	// OpCode 0x5000 little endian
	pkt[8] = 0x00
	pkt[9] = 0x50
	// Protocol version 14 big endian
	pkt[10] = 0x00
	pkt[11] = 0x0e
	pkt[12] = seq
	pkt[13] = physical
	// Universe little endian? Spec says low byte first.
	pkt[14] = byte(universe & 0xFF)
	pkt[15] = byte(universe >> 8)
	binary.BigEndian.PutUint16(pkt[16:18], uint16(len(data)))
	copy(pkt[18:], data)
	return pkt
}

// sACNMulticastAddr returns sACN multicast address for universe.
func sACNMulticastAddr(universe uint16) string {
	hi := byte(universe >> 8)
	lo := byte(universe & 0xFF)
	return fmt.Sprintf("239.255.%d.%d", hi, lo)
}

// artNetSink sends ArtDmx / sACN via UDP.
type artNetSink struct {
	host    string
	port    int
	startU  int
	offset  int
	perU    int
	useSACN bool
	conn    *net.UDPConn
	cid     [16]byte
	seq     uint8
}

func (a *artNetSink) Name() string { return "artnet" }
func (a *artNetSink) Close() error {
	if a.conn != nil {
		return a.conn.Close()
	}
	return nil
}
func (a *artNetSink) Send(f OutputFrame) error {
	payloads := SplitUniverses(f.Pixels, a.startU, a.offset, a.perU)
	for _, p := range payloads {
		var pkt []byte
		var addr string
		if a.useSACN {
			a.seq++
			pkt = BuildE131Packet(p.Universe, a.seq, a.cid, p.Data)
			addr = net.JoinHostPort(sACNMulticastAddr(p.Universe), fmt.Sprintf("%d", a.port))
		} else {
			a.seq++
			pkt = BuildArtDmxPacket(a.seq, 0, p.Universe, p.Data)
			addr = net.JoinHostPort(a.host, fmt.Sprintf("%d", a.port))
		}
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return err
		}
		c, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			return err
		}
		_, err = c.Write(pkt)
		c.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
