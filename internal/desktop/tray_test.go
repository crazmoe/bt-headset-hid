package desktop

import (
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/crazmoe/bt-headset-hid/internal/dbustest"
	"github.com/crazmoe/bt-headset-hid/internal/tray"
)

// fakeWatcher is the StatusNotifierWatcher of a desktop.
type fakeWatcher struct{ items chan string }

func (w fakeWatcher) RegisterStatusNotifierItem(name string) *dbus.Error {
	w.items <- name
	return nil
}

func TestTray(t *testing.T) {
	addr := dbustest.Bus(t)
	conn := dbustest.Connect(t, addr)
	host := dbustest.Connect(t, addr)

	clicks := make(chan int32, 4)
	tr, err := NewTray(conn, func(id int32) { clicks <- id })
	if err != nil {
		t.Fatal(err)
	}

	// The desktop's tray starts after the icon: the icon registers then.
	w := fakeWatcher{items: make(chan string, 4)}
	if err := host.Export(w, watcherPath, watcherIface); err != nil {
		t.Fatal(err)
	}
	if _, err := host.RequestName(watcherName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	select {
	case name := <-w.items:
		if name != tr.name {
			t.Errorf("registered %q, want %q", name, tr.name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("icon not registered")
	}

	sigs := make(chan *dbus.Signal, 16)
	host.Signal(sigs)
	if err := host.AddMatchSignal(dbus.WithMatchSender(tr.name)); err != nil {
		t.Fatal(err)
	}

	v := tray.Show(tray.State{Version: "1", Mode: "ofono", Headsets: []tray.Headset{
		{Address: "AA:AA:AA:AA:AA:AA", Name: "My_Headset", Connected: true, Selected: true},
	}})
	tr.Show(v)
	got := map[string]bool{}
	for len(got) < 2 {
		select {
		case s := <-sigs:
			got[s.Name] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("signals: got %v", got)
		}
	}
	for _, s := range []string{sniIface + ".NewToolTip", menuIface + ".LayoutUpdated"} {
		if !got[s] {
			t.Errorf("no signal %s, got %v", s, got)
		}
	}

	item := host.Object(tr.name, sniPath)
	tip, err := item.GetProperty(sniIface + ".ToolTip")
	if err != nil {
		t.Fatal(err)
	}
	if fields, _ := tip.Value().([]any); len(fields) != 4 || fields[3] != "My_Headset connected" {
		t.Errorf("ToolTip = %v", tip.Value())
	}
	if m, err := item.GetProperty(sniIface + ".Menu"); err != nil || m.Value() != menuPath {
		t.Errorf("Menu = %v, %v", m, err)
	}

	menu := host.Object(tr.name, menuPath)
	var rev uint32
	var layout menuLayout
	if err := menu.Call(menuIface+".GetLayout", 0, int32(0), int32(-1), []string{}).Store(&rev, &layout); err != nil {
		t.Fatal(err)
	}
	if rev != 1 || len(layout.Children) != len(v.Menu) {
		t.Fatalf("revision %d, %d items, want 1, %d", rev, len(layout.Children), len(v.Menu))
	}
	var toggle tray.Item
	for _, it := range v.Menu {
		if it.Action == tray.ToggleHeadset {
			toggle = it
		}
	}
	var props []menuProps
	if err := menu.Call(menuIface+".GetGroupProperties", 0, []int32{toggle.ID}, []string{}).Store(&props); err != nil {
		t.Fatal(err)
	}
	if len(props) != 1 || props[0].Props["label"].Value() != "My__Headset (connected)" ||
		props[0].Props["toggle-state"].Value() != int32(1) {
		t.Errorf("properties = %+v", props)
	}

	if err := menu.Call(menuIface+".Event", 0, toggle.ID, "clicked", dbus.MakeVariant(""), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	if err := menu.Call(menuIface+".Event", 0, toggle.ID, "hovered", dbus.MakeVariant(""), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-clicks:
		if id != toggle.ID {
			t.Errorf("clicked %d, want %d", id, toggle.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no click")
	}
	select {
	case id := <-clicks:
		t.Errorf("hover counted as click on %d", id)
	default:
	}

	// The same view again changes nothing.
	tr.Show(v)
	if err := menu.Call(menuIface+".GetLayout", 0, int32(0), int32(-1), []string{}).Store(&rev, &layout); err != nil {
		t.Fatal(err)
	}
	if rev != 1 {
		t.Errorf("revision %d after the same menu, want 1", rev)
	}
}
