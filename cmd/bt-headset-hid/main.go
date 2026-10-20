// Command bt-headset-hid bridges Bluetooth headset AT commands to a virtual HID telephony device.
package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func main() {
	fmt.Fprintf(os.Stderr, "bt-headset-hid %s: not implemented yet\n", version)
	os.Exit(1)
}
