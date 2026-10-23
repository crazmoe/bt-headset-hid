// Package sco opens the Bluetooth audio link (SCO/eSCO) of a headset for the
// call audio, which PipeWire then reads and writes. Only the socket setup
// lives here; the codec is negotiated over HFP before.
package sco

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Kernel constants from <bluetooth/bluetooth.h> and <bluetooth/sco.h>.
const (
	afBluetooth        = 31
	btprotoSCO         = 2
	solBluetooth       = 274
	btVoice            = 11
	btVoiceTransparent = 0x0003 // mSBC: the kernel passes the encoded frames through
	fDupfdCloexec      = 1030
	pollOut            = 0x4
)

// pollFd is struct pollfd.
type pollFd struct {
	fd      int32
	events  int16
	revents int16
}

// sockaddrSCO is struct sockaddr_sco.
type sockaddrSCO struct {
	family uint16
	addr   [6]byte
}

// ParseAddress turns "AA:BB:CC:DD:EE:FF" into a bdaddr_t, which stores the
// bytes in reverse order.
func ParseAddress(s string) ([6]byte, error) {
	var b [6]byte
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return b, fmt.Errorf("sco: bad address %q", s)
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 16, 8)
		if err != nil || len(p) != 2 {
			return b, fmt.Errorf("sco: bad address %q", s)
		}
		b[5-i] = byte(n)
	}
	return b, nil
}

// Dial opens the audio link from the adapter with address local to the
// headset remote and returns the connected socket, non-blocking and
// close-on-exec, as oFono hands it to PipeWire. transparent selects the
// mode for mSBC; otherwise the kernel converts CVSD. The caller owns the
// returned descriptor.
func Dial(local, remote string, transparent bool, timeout time.Duration) (int, error) {
	la, err := ParseAddress(local)
	if err != nil {
		return -1, err
	}
	ra, err := ParseAddress(remote)
	if err != nil {
		return -1, err
	}
	fd, err := syscall.Socket(afBluetooth, syscall.SOCK_SEQPACKET|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, btprotoSCO)
	if err != nil {
		return -1, os.NewSyscallError("socket", err)
	}
	ok := false
	defer func() {
		if !ok {
			syscall.Close(fd)
		}
	}()
	sa := sockaddrSCO{family: afBluetooth, addr: la}
	if _, _, e := syscall.Syscall(syscall.SYS_BIND, uintptr(fd), uintptr(unsafe.Pointer(&sa)), unsafe.Sizeof(sa)); e != 0 {
		return -1, os.NewSyscallError("bind", e)
	}
	if transparent {
		setting := uint16(btVoiceTransparent)
		if _, _, e := syscall.Syscall6(syscall.SYS_SETSOCKOPT, uintptr(fd), solBluetooth, btVoice,
			uintptr(unsafe.Pointer(&setting)), unsafe.Sizeof(setting), 0); e != 0 {
			return -1, os.NewSyscallError("setsockopt BT_VOICE", e)
		}
	}
	sa.addr = ra
	if _, _, e := syscall.Syscall(syscall.SYS_CONNECT, uintptr(fd), uintptr(unsafe.Pointer(&sa)), unsafe.Sizeof(sa)); e != 0 && e != syscall.EINPROGRESS {
		return -1, os.NewSyscallError("connect", e)
	}
	if err := wait(fd, timeout); err != nil {
		return -1, err
	}
	ok = true
	return fd, nil
}

// wait waits until the connect on fd finished. The Go poller does the
// waiting, on a copy of fd, so a timeout needs no extra thread.
func wait(fd int, timeout time.Duration) error {
	// os.File takes over the descriptor it is given, so give it a copy.
	cp, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), fDupfdCloexec, 0)
	if e != 0 {
		return os.NewSyscallError("fcntl", e)
	}
	f := os.NewFile(cp, "sco")
	defer f.Close()
	if err := f.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	// The socket becomes writable (or fails) once the link is up. The
	// poller only wakes on a change, so each call first looks itself.
	var connErr error
	err = rc.Write(func(uintptr) bool {
		pfd := pollFd{fd: int32(fd), events: pollOut}
		var zero syscall.Timespec
		n, _, e := syscall.Syscall6(syscall.SYS_PPOLL, uintptr(unsafe.Pointer(&pfd)), 1, uintptr(unsafe.Pointer(&zero)), 0, 0, 0)
		if e == syscall.EINTR || e == 0 && n == 0 {
			return false // still connecting
		}
		if e != 0 {
			connErr = os.NewSyscallError("ppoll", e)
			return true
		}
		soErr, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_ERROR)
		switch {
		case err != nil:
			connErr = os.NewSyscallError("getsockopt", err)
		case soErr != 0:
			connErr = os.NewSyscallError("connect", syscall.Errno(soErr))
		case pfd.revents&pollOut == 0:
			connErr = errors.New("sco: audio link closed while connecting")
		}
		return true
	})
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return fmt.Errorf("sco: no audio link after %v", timeout)
	}
	if err != nil {
		return err
	}
	return connErr
}
