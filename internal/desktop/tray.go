package desktop

import (
	"fmt"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"github.com/crazmoe/bt-headset-hid/internal/tray"
)

// The tray icon is a StatusNotifierItem (as KDE, Xfce, Cinnamon, MATE,
// LXQt, waybar and GNOME with the AppIndicator extension show them) with a
// com.canonical.dbusmenu menu.
const (
	sniIface     = "org.kde.StatusNotifierItem"
	sniPath      = dbus.ObjectPath("/StatusNotifierItem")
	menuIface    = "com.canonical.dbusmenu"
	menuPath     = dbus.ObjectPath("/MenuBar")
	watcherName  = "org.kde.StatusNotifierWatcher"
	watcherPath  = dbus.ObjectPath("/StatusNotifierWatcher")
	watcherIface = "org.kde.StatusNotifierWatcher"
)

// Tray is the tray icon of bt-headset-hid on the session bus.
type Tray struct {
	conn  *dbus.Conn
	name  string
	props *prop.Properties
	click func(id int32)

	mu       sync.Mutex
	view     tray.View
	revision uint32
}

// toolTip is the ToolTip property of a StatusNotifierItem, (sa(iiay)ss).
type toolTip struct {
	Icon    string
	Pixmaps []pixmap
	Title   string
	Text    string
}

type pixmap struct {
	Width, Height int32
	Data          []byte
}

// NewTray puts a tray icon on the session bus conn and registers it with
// the tray of the desktop, now and whenever that starts again. click is
// called with the ID of a clicked menu item, from another goroutine.
func NewTray(conn *dbus.Conn, click func(id int32)) (*Tray, error) {
	t := &Tray{conn: conn, name: fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid()), click: click}
	t.view = tray.View{Icon: "audio-headset", Title: "bt-headset-hid"}
	props, err := prop.Export(conn, sniPath, prop.Map{
		sniIface: {
			"Category":            {Value: "Hardware"},
			"Id":                  {Value: "bt-headset-hid"},
			"Title":               {Value: t.view.Title},
			"Status":              {Value: "Active"},
			"WindowId":            {Value: int32(0)},
			"IconName":            {Value: t.view.Icon},
			"IconThemePath":       {Value: ""},
			"OverlayIconName":     {Value: ""},
			"AttentionIconName":   {Value: "dialog-warning"},
			"AttentionMovieName":  {Value: ""},
			"ToolTip":             {Value: toolTip{Title: t.view.Title}},
			"ItemIsMenu":          {Value: true},
			"Menu":                {Value: menuPath},
			"IconPixmap":          {Value: []pixmap{}},
			"OverlayIconPixmap":   {Value: []pixmap{}},
			"AttentionIconPixmap": {Value: []pixmap{}},
		},
	})
	if err != nil {
		return nil, err
	}
	t.props = props
	menuProps, err := prop.Export(conn, menuPath, prop.Map{
		menuIface: {
			"Version":       {Value: uint32(3)},
			"TextDirection": {Value: "ltr"},
			"Status":        {Value: "normal"},
			"IconThemePath": {Value: []string{}},
		},
	})
	if err != nil {
		return nil, err
	}
	item, menu := (*sniItem)(t), (*dbusMenu)(t)
	if err := conn.Export(item, sniPath, sniIface); err != nil {
		return nil, err
	}
	if err := conn.Export(menu, menuPath, menuIface); err != nil {
		return nil, err
	}
	for _, e := range []struct {
		path  dbus.ObjectPath
		iface string
		v     any
		props *prop.Properties
		sigs  []introspect.Signal
	}{
		{sniPath, sniIface, item, props, []introspect.Signal{{Name: "NewIcon"}, {Name: "NewToolTip"}, {Name: "NewTitle"},
			{Name: "NewAttentionIcon"}, {Name: "NewStatus", Args: []introspect.Arg{{Name: "status", Type: "s"}}}}},
		{menuPath, menuIface, menu, menuProps, []introspect.Signal{{Name: "LayoutUpdated", Args: []introspect.Arg{
			{Name: "revision", Type: "u"}, {Name: "parent", Type: "i"}}}}},
	} {
		node := &introspect.Node{Name: string(e.path), Interfaces: []introspect.Interface{
			introspect.IntrospectData, prop.IntrospectData,
			{Name: e.iface, Methods: introspect.Methods(e.v), Properties: e.props.Introspection(e.iface), Signals: e.sigs},
		}}
		if err := conn.Export(introspect.NewIntrospectable(node), e.path, "org.freedesktop.DBus.Introspectable"); err != nil {
			return nil, err
		}
	}
	if _, err := conn.RequestName(t.name, dbus.NameFlagDoNotQueue); err != nil {
		return nil, err
	}
	// The tray of the desktop may start after the session service, or
	// start again; each time it needs the icon registered again.
	err = conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, watcherName),
	)
	if err != nil {
		return nil, err
	}
	sigs := make(chan *dbus.Signal, 4)
	conn.Signal(sigs)
	go func() {
		for sig := range sigs {
			if sig.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(sig.Body) < 3 {
				continue
			}
			if name, _ := sig.Body[0].(string); name != watcherName {
				continue
			}
			if owner, _ := sig.Body[2].(string); owner != "" {
				t.register()
			}
		}
	}()
	t.register()
	return t, nil
}

// register registers the icon with the tray of the desktop, if there is
// one; without, the icon is simply not shown.
func (t *Tray) register() {
	t.conn.Object(watcherName, watcherPath).Call(watcherIface+".RegisterStatusNotifierItem", dbus.FlagNoAutoStart, t.name)
}

// Show shows v: the icon, tooltip and, if it changed, the menu.
func (t *Tray) Show(v tray.View) {
	t.mu.Lock()
	old := t.view
	t.view = v
	menuChanged := !sameMenu(old.Menu, v.Menu)
	if menuChanged {
		t.revision++
	}
	rev := t.revision
	t.mu.Unlock()

	if v.Icon != old.Icon {
		t.props.SetMust(sniIface, "IconName", v.Icon)
		t.conn.Emit(sniPath, sniIface+".NewIcon")
	}
	if v.Title != old.Title {
		t.props.SetMust(sniIface, "Title", v.Title)
		t.conn.Emit(sniPath, sniIface+".NewTitle")
	}
	if v.Title != old.Title || v.Tooltip != old.Tooltip || v.Icon != old.Icon {
		t.props.SetMust(sniIface, "ToolTip", toolTip{Icon: v.Icon, Title: v.Title, Text: v.Tooltip})
		t.conn.Emit(sniPath, sniIface+".NewToolTip")
	}
	if v.Attention != old.Attention {
		status := "Active"
		if v.Attention {
			status = "NeedsAttention"
		}
		t.props.SetMust(sniIface, "Status", status)
		t.conn.Emit(sniPath, sniIface+".NewStatus", status)
	}
	if menuChanged {
		t.conn.Emit(menuPath, menuIface+".LayoutUpdated", rev, int32(0))
	}
}

func sameMenu(a, b []tray.Item) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sniItem has the methods of org.kde.StatusNotifierItem. The menu does
// everything, so clicks on the icon itself do nothing.
type sniItem Tray

func (*sniItem) Activate(x, y int32) *dbus.Error          { return nil }
func (*sniItem) SecondaryActivate(x, y int32) *dbus.Error { return nil }
func (*sniItem) ContextMenu(x, y int32) *dbus.Error       { return nil }
func (*sniItem) Scroll(delta int32, orientation string) *dbus.Error {
	return nil
}

// dbusMenu has the methods of com.canonical.dbusmenu.
type dbusMenu Tray

// menuLayout is an item of the menu with its children, (ia{sv}av).
type menuLayout struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

type menuProps struct {
	ID    int32
	Props map[string]dbus.Variant
}

type menuEvent struct {
	ID        int32
	EventID   string
	Data      dbus.Variant
	Timestamp uint32
}

func itemProps(it tray.Item) map[string]dbus.Variant {
	if it.Separator {
		return map[string]dbus.Variant{"type": dbus.MakeVariant("separator")}
	}
	p := map[string]dbus.Variant{
		"label":   dbus.MakeVariant(escapeLabel(it.Label)),
		"enabled": dbus.MakeVariant(it.Enabled),
	}
	if it.Check != tray.NoCheck {
		p["toggle-type"] = dbus.MakeVariant("checkmark")
		state := int32(0)
		if it.Check == tray.Checked {
			state = 1
		}
		p["toggle-state"] = dbus.MakeVariant(state)
	}
	return p
}

// escapeLabel doubles underscores, which dbusmenu takes as mnemonics.
func escapeLabel(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '_' {
			out = append(out, '_')
		}
		out = append(out, r)
	}
	return string(out)
}

func (m *dbusMenu) GetLayout(parent int32, depth int32, names []string) (uint32, menuLayout, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if parent != 0 {
		it, ok := tray.Find(m.view.Menu, parent)
		if !ok {
			return 0, menuLayout{}, dbus.MakeFailedError(fmt.Errorf("no menu item %d", parent))
		}
		return m.revision, menuLayout{ID: it.ID, Props: itemProps(it), Children: []dbus.Variant{}}, nil
	}
	root := menuLayout{ID: 0, Props: map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}, Children: []dbus.Variant{}}
	if depth != 0 {
		for _, it := range m.view.Menu {
			root.Children = append(root.Children, dbus.MakeVariant(menuLayout{ID: it.ID, Props: itemProps(it), Children: []dbus.Variant{}}))
		}
	}
	return m.revision, root, nil
}

func (m *dbusMenu) GetGroupProperties(ids []int32, names []string) ([]menuProps, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []menuProps{}
	for _, it := range m.view.Menu {
		if len(ids) == 0 || contains(ids, it.ID) {
			out = append(out, menuProps{ID: it.ID, Props: itemProps(it)})
		}
	}
	return out, nil
}

func (m *dbusMenu) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := tray.Find(m.view.Menu, id)
	if !ok {
		return dbus.Variant{}, dbus.MakeFailedError(fmt.Errorf("no menu item %d", id))
	}
	v, ok := itemProps(it)[name]
	if !ok {
		return dbus.Variant{}, dbus.MakeFailedError(fmt.Errorf("no property %s", name))
	}
	return v, nil
}

func (m *dbusMenu) Event(id int32, eventID string, data dbus.Variant, timestamp uint32) *dbus.Error {
	if eventID == "clicked" {
		m.click(id)
	}
	return nil
}

func (m *dbusMenu) EventGroup(events []menuEvent) ([]int32, *dbus.Error) {
	for _, e := range events {
		m.Event(e.ID, e.EventID, e.Data, e.Timestamp)
	}
	return []int32{}, nil
}

func (m *dbusMenu) AboutToShow(id int32) (bool, *dbus.Error) { return false, nil }

func (m *dbusMenu) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return []int32{}, []int32{}, nil
}

func contains(ids []int32, id int32) bool {
	for _, i := range ids {
		if i == id {
			return true
		}
	}
	return false
}
