-- Mode pipewire, WirePlumber 0.4 (Lua configuration):
-- ~/.config/wireplumber/bluetooth.lua.d/51-bt-headset-hid.lua
-- PipeWire keeps HFP and forwards answer/hang-up to the "modem" bt-headset-hid provides.
bluez_monitor.properties["bluez5.hfphsp-backend"] = "native"
bluez_monitor.properties["bluez5.hfphsp-backend-native-modem"] = "any"
