package modem

import (
	"sort"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// Introspection is not needed by PipeWire, which uses GetManagedObjects, but
// tools like busctl and d-feet walk the tree from "/" with it.

// staticPaths are the objects that always exist, including the parents that
// only carry Introspectable so the tree can be walked.
var staticPaths = []dbus.ObjectPath{
	"/", "/org", "/org/freedesktop", RootPath, RootPath + "/Modem", ModemPath, CallPrefix,
}

// introspectable implements org.freedesktop.DBus.Introspectable for one path.
type introspectable struct {
	s    *Server
	path dbus.ObjectPath
}

func (s *Server) exportIntrospectable(path dbus.ObjectPath) error {
	return s.conn.Export(introspectable{s, path}, path, introspect.IntrospectData.Name)
}

func (i introspectable) Introspect() (string, *dbus.Error) {
	i.s.mu.Lock()
	n := i.s.node(i.path)
	i.s.mu.Unlock()
	return introspect.NewIntrospectable(n).Introspect()
}

// node describes path with its interfaces and direct children. Must be
// called with s.mu held.
func (s *Server) node(path dbus.ObjectPath) *introspect.Node {
	n := &introspect.Node{Interfaces: []introspect.Interface{introspect.IntrospectData}}
	withProps := func(p *prop.Properties, ifaces map[string]any) {
		n.Interfaces = append(n.Interfaces, prop.IntrospectData)
		names := make([]string, 0, len(ifaces))
		for name := range ifaces {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			iface := introspect.Interface{Name: name, Properties: p.Introspection(name)}
			if v := ifaces[name]; v != nil {
				iface.Methods = introspect.Methods(v)
			}
			n.Interfaces = append(n.Interfaces, iface)
		}
	}
	switch {
	case path == RootPath:
		n.Interfaces = append(n.Interfaces, introspect.Interface{
			Name: ifaceObjMgr, Methods: introspect.Methods(objectManager{}),
		})
	case path == ModemPath:
		withProps(s.modem, map[string]any{ifaceModem: nil, iface3gpp: nil, ifaceVoice: voice{}})
	case s.callProps[path] != nil:
		withProps(s.callProps[path], map[string]any{ifaceCall: call{}})
	}
	all := append([]dbus.ObjectPath{}, staticPaths...)
	all = append(all, s.callPaths()...)
	prefix := string(path) + "/"
	if path == "/" {
		prefix = "/"
	}
	seen := map[string]bool{}
	for _, p := range all {
		rest, ok := strings.CutPrefix(string(p), prefix)
		if !ok || rest == "" || strings.Contains(rest, "/") || seen[rest] {
			continue
		}
		seen[rest] = true
		n.Children = append(n.Children, introspect.Node{Name: rest})
	}
	sort.Slice(n.Children, func(a, b int) bool { return n.Children[a].Name < n.Children[b].Name })
	return n
}
