package sco

import (
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestParseAddress(t *testing.T) {
	got, err := ParseAddress("50:C2:75:0D:4E:03")
	if err != nil || got != [6]byte{0x03, 0x4e, 0x0d, 0x75, 0xc2, 0x50} {
		t.Fatalf("ParseAddress = % x, %v", got, err)
	}
	for _, bad := range []string{"", "50:C2:75:0D:4E", "50:C2:75:0D:4E:3", "50:C2:75:0D:4E:0G", "50-C2-75-0D-4E-03", "50:C2:75:0D:4E:03:00"} {
		if _, err := ParseAddress(bad); err == nil {
			t.Errorf("ParseAddress(%q) accepted", bad)
		}
	}
}

func TestSockaddrSize(t *testing.T) {
	if n := unsafe.Sizeof(sockaddrSCO{}); n != 8 {
		t.Fatalf("sizeof(sockaddr_sco) = %d, want 8", n)
	}
}

func TestDialBadAddress(t *testing.T) {
	if _, err := Dial("x", "50:C2:75:0D:4E:03", false, time.Second); err == nil {
		t.Fatal("bad local address accepted")
	}
}

// TestWait checks the waiting on a socket pair, where writing is possible at
// once, so wait must return without error well before the timeout.
func TestWait(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fds[0])
	defer syscall.Close(fds[1])
	start := time.Now()
	if err := wait(fds[0], 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("wait took %v", d)
	}
	// fds[0] must still be open.
	if _, err := syscall.Write(fds[0], []byte("x")); err != nil {
		t.Fatalf("write after wait: %v", err)
	}
}

// TestWaitTimeout fills a socket so it is not writable.
func TestWaitTimeout(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fds[0])
	defer syscall.Close(fds[1])
	buf := make([]byte, 65536)
	for {
		if _, err := syscall.Write(fds[0], buf); errors.Is(err, syscall.EAGAIN) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if err := wait(fds[0], 100*time.Millisecond); err == nil {
		t.Fatal("no timeout on a full socket")
	}
}

func TestDialBadRemote(t *testing.T) {
	if _, err := Dial("00:1A:7D:DA:71:13", "50:C2:75:0D:4E", false, time.Second); err == nil {
		t.Fatal("bad remote address accepted")
	}
}

// TestDialNoLink dials from an adapter that does not exist. Without
// Bluetooth in the kernel the socket fails, else bind or connect; either
// way Dial must fail and leave no descriptor behind.
func TestDialNoLink(t *testing.T) {
	for _, transparent := range []bool{false, true} {
		fd, err := Dial("00:00:00:00:00:01", "50:C2:75:0D:4E:03", transparent, 200*time.Millisecond)
		if err == nil {
			syscall.Close(fd)
			t.Fatalf("Dial(transparent %v) connected without an adapter", transparent)
		}
		if fd != -1 {
			t.Errorf("Dial failed but returned fd %d", fd)
		}
	}
}

// TestWaitRefused checks that a failed connect is reported.
func TestWaitRefused(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close() // nobody listens there now
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	err = syscall.Connect(fd, &syscall.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}})
	if err != nil && err != syscall.EINPROGRESS {
		if errors.Is(err, syscall.ECONNREFUSED) {
			t.Skip("connect refused at once")
		}
		t.Fatal(err)
	}
	if err := wait(fd, 2*time.Second); err == nil {
		t.Fatal("refused connect reported as success")
	}
}

func TestWaitBadFd(t *testing.T) {
	if err := wait(-1, time.Second); err == nil {
		t.Fatal("wait on fd -1 succeeded")
	}
}
