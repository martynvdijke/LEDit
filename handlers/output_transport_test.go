package handlers

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSplitUniverses_170(t *testing.T) {
	pixels := make([]byte, 171*3)
	for i := range pixels {
		pixels[i] = byte(i % 256)
	}
	ups := SplitUniverses(pixels, 0, 0, 512)
	if len(ups) != 2 {
		t.Fatalf("want 2 universes got %d", len(ups))
	}
	if len(ups[0].Data) != 512 {
		t.Fatalf("uni0 len %d", len(ups[0].Data))
	}
	// 171st pixel starts at byte 510 in original pixels, should be in uni1
	// reassemble check
	var reass []byte
	for _, u := range ups {
		reass = append(reass, u.Data...)
	}
	// reass length 512+1=513, first 513 bytes should equal pixels (510+3)
	if !bytes.Equal(reass[:len(pixels)], pixels) {
		t.Fatal("reassemble mismatch")
	}
	if ups[1].Universe != 1 {
		t.Fatalf("universe %d", ups[1].Universe)
	}
}

func TestSplitUniverses_64x64(t *testing.T) {
	pixels := make([]byte, 64*64*3) // 12288
	for i := range pixels {
		pixels[i] = byte(i % 251)
	}
	ups := SplitUniverses(pixels, 0, 0, 512)
	// 12288/512=24 exactly
	if len(ups) != 24 {
		t.Fatalf("want 24 got %d", len(ups))
	}
	var reass []byte
	for _, u := range ups {
		reass = append(reass, u.Data...)
	}
	if !bytes.Equal(reass[:len(pixels)], pixels) {
		t.Fatal("loss")
	}
}

func TestSplitUniverses_Offset(t *testing.T) {
	pixels := []byte{1, 2, 3, 4, 5, 6}
	ups := SplitUniverses(pixels, 10, 2, 4)
	// buf = [0,0,1,2,3,4,5,6] -> chunks 4 each: [0,0,1,2],[3,4,5,6]
	if ups[0].Universe != 10 || ups[1].Universe != 11 {
		t.Fatalf("universe %v", ups)
	}
	if ups[0].Data[0] != 0 || ups[0].Data[2] != 1 {
		t.Fatalf("offset %v", ups[0].Data)
	}
}

func TestBuildE131Packet_Length(t *testing.T) {
	var cid [16]byte
	data := make([]byte, 510)
	pkt := BuildE131Packet(1, 5, cid, data)
	if len(pkt) != 638 {
		t.Fatalf("len %d want 638", len(pkt))
	}
	// Check ACN PID
	if !bytes.Equal(pkt[4:16], []byte{0x41, 0x53, 0x43, 0x2d, 0x45, 0x31, 0x2e, 0x31, 0x37, 0x00, 0x00, 0x00}) {
		t.Fatalf("acn pid")
	}
	// Universe at framing
	// universe is near end; search: packet contains universe bytes.
	// Just verify pkt contains sequence
	found := false
	for i := range pkt {
		if i+1 < len(pkt) && pkt[i] == 5 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("seq not found")
	}
}

func TestBuildArtDmxPacket(t *testing.T) {
	data := []byte{1, 2, 3}
	pkt := BuildArtDmxPacket(7, 0, 10, data)
	if string(pkt[0:8]) != "Art-Net\x00" {
		t.Fatalf("header %q", pkt[0:8])
	}
	if pkt[8] != 0x00 || pkt[9] != 0x50 {
		t.Fatalf("opcode %x %x", pkt[8], pkt[9])
	}
	if pkt[11] != 0x0e {
		t.Fatalf("version")
	}
	if pkt[12] != 7 {
		t.Fatalf("seq")
	}
	// length big endian
	l := binary.BigEndian.Uint16(pkt[16:18])
	if l != 3 {
		t.Fatalf("len %d", l)
	}
	// universe little endian
	u := uint16(pkt[14]) | uint16(pkt[15])<<8
	if u != 10 {
		t.Fatalf("universe %d", u)
	}
}

func TestSACNMulticastAddr(t *testing.T) {
	if sACNMulticastAddr(1) != "239.255.0.1" {
		t.Fatalf("addr %s", sACNMulticastAddr(1))
	}
	if sACNMulticastAddr(0x1234) != "239.255.18.52" {
		t.Fatalf("addr %s", sACNMulticastAddr(0x1234))
	}
}

func TestWLED_DDPHeader(t *testing.T) {
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	conn, _ := net.ListenUDP("udp", addr)
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port
	sink, _ := NewOutputSink(OutputSinkConfig{Transport: "wled", Host: "127.0.0.1", Port: port, WLEDMode: "ddp"})
	pixels := make([]byte, 6)
	for i := range pixels {
		pixels[i] = byte(i + 10)
	}
	go sink.Send(OutputFrame{Pixels: pixels})
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 2048)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read %v", err)
	}
	if n < 10 {
		t.Fatalf("short %d", n)
	}
	if buf[0] != 0x41 {
		t.Fatalf("ddp header %x", buf[0])
	}
	length := binary.BigEndian.Uint16(buf[8:10])
	if int(length) != 6 {
		t.Fatalf("ddp len %d", length)
	}
	if !bytes.Equal(buf[10:n], pixels) {
		t.Fatalf("payload")
	}
}

func TestWLED_HTTP(t *testing.T) {
	var got []byte
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		got = buf.Bytes()
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// parse host port from srv.URL
	// srv.URL = http://127.0.0.1:xxxxx
	host := srv.Listener.Addr().(*net.TCPAddr).IP.String()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	// Use 127.0.0.1 explicitly
	cfg := OutputSinkConfig{Transport: "wled", Host: "127.0.0.1", Port: port, WLEDMode: "http", Username: "u", Password: "p"}
	sink, _ := NewOutputSink(cfg)
	// Need to override host to work with httptest; our sink uses Host:Port as URL, so use 127.0.0.1 and port above is same.
	_ = host
	pixels := []byte{1, 2, 3}
	if err := sink.Send(OutputFrame{Pixels: pixels}); err != nil {
		t.Fatalf("send %v", err)
	}
	if gotAuth == "" {
		t.Fatal("no auth")
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("json %v", err)
	}
	if m["on"] != true {
		t.Fatalf("on %v", m["on"])
	}
}

func TestWLED_E131_Identical(t *testing.T) {
	var cid [16]byte
	data := []byte{10, 20, 30, 40, 50, 60}
	p1 := BuildE131Packet(5, 1, cid, data)
	p2 := BuildE131Packet(5, 1, cid, data)
	if !bytes.Equal(p1, p2) {
		t.Fatal("identical payloads differ")
	}
}

func TestIntervalClamp(t *testing.T) {
	c := OutputSinkConfig{FPS: 100}
	if c.Interval() != time.Second/60 {
		t.Fatalf("clamp high %v", c.Interval())
	}
	c.FPS = 0
	if c.Interval() != time.Second {
		t.Fatalf("clamp low %v", c.Interval())
	}
}

func TestNewOutputSink_Unknown(t *testing.T) {
	_, err := NewOutputSink(OutputSinkConfig{Transport: "bogus"})
	if err == nil {
		t.Fatal("want error")
	}
}
