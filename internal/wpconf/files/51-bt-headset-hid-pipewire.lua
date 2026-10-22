-- Mode pipewire, WirePlumber 0.4 (Lua configuration):
-- ~/.config/wireplumber/bluetooth.lua.d/51-bt-headset-hid.lua
-- PipeWire keeps HFP and forwards answer/hang-up to the "modem" bt-headset-hid provides.
-- Only the headset roles: without A2DP the headset stays in the phone profile, ready for calls,
-- and its button never goes to a media player (music sounds like on the phone).
bluez_monitor.properties["bluez5.hfphsp-backend"] = "native"
bluez_monitor.properties["bluez5.hfphsp-backend-native-modem"] = "any"
bluez_monitor.properties["bluez5.roles"] = "[ hfp_hf hfp_ag ]"
