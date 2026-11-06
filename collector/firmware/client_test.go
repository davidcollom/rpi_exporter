package firmware

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestRequest(t *testing.T) {
	buf, err := request("get_throttled")
	if err != nil { t.Fatal(err) }
	if buf[0] != totalWords*4 || buf[2] != tagGenCmd || buf[3] != maxString || buf[len(buf)-1] != 0 {
		t.Fatal("invalid mailbox request header")
	}
	data := make([]byte, 16)
	for i := 0; i < 4; i++ { binary.LittleEndian.PutUint32(data[4*i:], buf[valueOffset+i]) }
	if !strings.HasPrefix(string(data), "get_throttled\x00") { t.Fatalf("wrong command bytes: %q", data) }
}

func TestRejectCommands(t *testing.T) {
	for _, cmd := range []string{"", "bad\x00command", strings.Repeat("x", maxString)} {
		if _, err := request(cmd); err == nil { t.Errorf("accepted %q", cmd[:min(len(cmd), 32)]) }
	}
}

func TestResponse(t *testing.T) {
	buf, _ := request("get_throttled")
	buf[1] = 0x80000000
	buf[4] = 0x80000000 | maxString
	for i := valueOffset; i < totalWords-1; i++ { buf[i] = 0 }
	for i, ch := range []byte("throttled=0x50000") {
		buf[valueOffset+i/4] |= uint32(ch) << (8 * (i % 4))
	}
	got, err := response(buf)
	if err != nil || got != "throttled=0x50000" { t.Fatalf("got=%q err=%v", got, err) }
	buf[5] = 0xffffffff
	if _, err := response(buf); err == nil { t.Fatal("expected firmware error") }
	buf[1] = 0
	if _, err := response(buf); err == nil { t.Fatal("expected bad header error") }
}

func TestIoctlNumber(t *testing.T) {
	if ioctlRequest() != 0xc0086400 && ioctlRequest() != 0xc0046400 {
		t.Fatalf("unexpected ioctl: %#x", ioctlRequest())
	}
}
