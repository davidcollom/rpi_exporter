// Package firmware accesses Raspberry Pi firmware without executing vcgencmd.
package firmware

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	tagGenCmd     = 0x00030080
	maxString     = 4096
	valueOffset   = 6
	totalWords    = valueOffset + maxString/4 + 1
	mailboxDevice = "/dev/vcio"
)

type Command interface {
	Run(command string) (string, error)
	Close() error
}

type Client struct {
	file *os.File
}

func Open() (*Client, error) {
	var errors []string
	for _, name := range []string{"/dev/vcio_gencmd", mailboxDevice} {
		file, err := os.OpenFile(name, os.O_RDWR, 0)
		if err == nil {
			return &Client{file: file}, nil
		}
		errors = append(errors, fmt.Sprintf("%s: %v", name, err))
	}
	return nil, fmt.Errorf("cannot open firmware mailbox: %s", strings.Join(errors, "; "))
}

func (c *Client) Close() error { return c.file.Close() }

func request(cmd string) ([]uint32, error) {
	if len(cmd) == 0 || len(cmd) >= maxString || strings.ContainsRune(cmd, 0) {
		return nil, fmt.Errorf("invalid firmware command length/content")
	}
	buf := make([]uint32, totalWords)
	buf[0] = uint32(totalWords * 4)
	buf[2] = tagGenCmd
	buf[3] = maxString
	for i, ch := range []byte(cmd) {
		buf[valueOffset+i/4] |= uint32(ch) << (8 * (i % 4))
	}
	return buf, nil
}

func response(buf []uint32) (string, error) {
	if len(buf) != totalWords || buf[1]&0x80000000 == 0 || buf[2] != tagGenCmd || buf[4]&0x80000000 == 0 {
		return "", fmt.Errorf("invalid mailbox response")
	}
	data := make([]byte, maxString)
	for i := 0; i < maxString/4; i++ {
		binary.LittleEndian.PutUint32(data[i*4:], buf[valueOffset+i])
	}
	body := string(bytes.TrimRight(data, "\x00"))
	if buf[5] != 0 {
		return "", fmt.Errorf("firmware error 0x%x: %s", buf[5], body)
	}
	return body, nil
}

func ioctlRequest() uintptr {
	// _IOWR('d', 0, char *) with pointer-width-dependent size.
	size := uintptr(4)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		size = 8
	}
	return (3 << 30) | (size << 16) | (100 << 8)
}

func (c *Client) Run(cmd string) (string, error) {
	buf, err := request(cmd)
	if err != nil { return "", err }
	if runtime.GOOS != "linux" { return "", fmt.Errorf("firmware mailbox requires Linux") }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, c.file.Fd(), ioctlRequest(), uintptr(unsafe.Pointer(&buf[0])))
	runtime.KeepAlive(buf)
	if errno != 0 { return "", fmt.Errorf("mailbox ioctl %q: %w", cmd, errno) }
	return response(buf)
}
