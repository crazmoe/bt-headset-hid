-- Mode ofono, WirePlumber 0.4 (Lua configuration):
-- ~/.config/wireplumber/bluetooth.lua.d/51-bt-headset-hid.lua
-- bt-headset-hid keeps HFP/HSP (all buttons) and poses as oFono; PipeWire takes the call audio
-- from it. ModemManager stays free for mobile broadband.
bluez_monitor.properties["bluez5.hfphsp-backend"] = "ofono"
bluez_monitor.properties["bluez5.enable-msbc"] = true
