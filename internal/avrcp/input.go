package avrcp

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/crazmoe/bt-headset-hid/internal/hfp"
)

// evIOCGRAB is _IOW('E', 0x90, int).
const evIOCGRAB = 0x40044590

// Device is an input device found in sysfs.
type Device struct {
	Path string // /dev/input/eventN
	Name string
}

// Scan lists the AVRCP devices below sysfs (normally /sys) with their nodes
// below dev (normally /dev).
func Scan(sysfs, dev string) ([]Device, error) {
	dirs, err := filepath.Glob(filepath.Join(sysfs, "class/input/event*"))
	if err != nil {
		return nil, err
	}
	var found []Device
	for _, d := range dirs {
		name, err := os.ReadFile(filepath.Join(d, "device/name"))
		if err != nil {
			continue
		}
		bus, err := os.ReadFile(filepath.Join(d, "device/id/bustype"))
		if err != nil {
			continue
		}
		b, err := strconv.ParseUint(strings.TrimSpace(string(bus)), 16, 16)
		if err != nil {
			continue
		}
		n := strings.TrimSpace(string(name))
		if Match(n, uint16(b)) {
			found = append(found, Device{Path: filepath.Join(dev, "input", filepath.Base(d)), Name: n})
		}
	}
	return found, nil
}

// Watcher grabs AVRCP devices as they appear and reports their play/pause
// presses.
type Watcher struct {
	Sysfs, Dev string        // "/sys", "/dev"
	Interval   time.Duration // how often to look for new devices
	Debounce   time.Duration
	OnEvent    func(hfp.Event)
	Logf       func(format string, args ...any)

	mu   sync.Mutex
	open map[string]io.Closer
}

// Run watches until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	w.open = map[string]io.Closer{}
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		w.scan()
		select {
		case <-ctx.Done():
			w.mu.Lock()
			for _, f := range w.open {
				f.Close()
			}
			w.mu.Unlock()
			return
		case <-t.C:
		}
	}
}

func (w *Watcher) scan() {
	devs, err := Scan(w.Sysfs, w.Dev)
	if err != nil {
		w.Logf("media keys: %v", err)
		return
	}
	for _, d := range devs {
		w.mu.Lock()
		_, busy := w.open[d.Path]
		w.mu.Unlock()
		if busy {
			continue
		}
		f, err := grab(d.Path)
		if err != nil {
			w.Logf("media keys: %s: %v", d.Name, err)
			continue
		}
		w.mu.Lock()
		w.open[d.Path] = f
		w.mu.Unlock()
		w.Logf("media keys of %q now go to the softphone", d.Name)
		go func() {
			err := w.serve(f)
			f.Close()
			w.mu.Lock()
			delete(w.open, d.Path)
			w.mu.Unlock()
			if err != nil && !errors.Is(err, os.ErrClosed) {
				w.Logf("media keys of %q released: %v", d.Name, err)
			}
		}()
	}
}

// grab opens an input device and takes it exclusively, so the desktop and
// media players no longer see its keys.
func grab(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	rc, err := f.SyscallConn()
	if err != nil {
		f.Close()
		return nil, err
	}
	var errno syscall.Errno
	if err := rc.Control(func(fd uintptr) {
		one := 1
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, evIOCGRAB, uintptr(unsafe.Pointer(&one)))
	}); err != nil {
		f.Close()
		return nil, err
	}
	if errno != 0 {
		f.Close()
		return nil, os.NewSyscallError("EVIOCGRAB", errno)
	}
	return f, nil
}

// serve reads input events from r until it fails.
func (w *Watcher) serve(r io.Reader) error {
	d := Decoder{Debounce: w.Debounce}
	buf := make([]byte, eventSize*16)
	for {
		n, err := r.Read(buf)
		for off := 0; off+eventSize <= n; off += eventSize {
			if e, ok, _ := d.Decode(buf[off:off+eventSize], time.Now()); ok {
				w.OnEvent(e)
			}
		}
		if err != nil {
			return err
		}
	}
}
