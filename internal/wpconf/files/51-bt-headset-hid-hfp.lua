-- Mode hfp, WirePlumber 0.4 (Lua configuration):
-- ~/.config/wireplumber/bluetooth.lua.d/51-bt-headset-hid.lua
-- PipeWire leaves HFP/HSP to bt-headset-hid and only uses A2DP (music, no microphone).
bluez_monitor.properties["bluez5.headset-roles"] = "[ ]"
