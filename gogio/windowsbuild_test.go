// SPDX-License-Identifier: Unlicense OR MIT

package main

import "testing"

func TestWindowsFixedVersion(t *testing.T) {
	tests := []struct {
		version string
		want    [2]uint32
	}{
		{"0.0.0.0", [2]uint32{0x00000000, 0x00000000}},
		{"1.0.0.1", [2]uint32{0x00010000, 0x00000001}},
		{"1.2.3.4", [2]uint32{0x00010002, 0x00030004}},
		{"0.18.0.0", [2]uint32{0x00000012, 0x00000000}},
		// Each component occupies 16 bits, so 65535 is the largest value
		// a Windows version resource can represent. Android version codes
		// routinely exceed that; see TestVersion.
		{"65535.65535.65535.65535", [2]uint32{0xFFFFFFFF, 0xFFFFFFFF}},
	}

	for i, test := range tests {
		ver, err := parseSemver(test.version)
		if err != nil {
			t.Errorf("(%d): %q failed to parse: %v", i, test.version, err)
			continue
		}
		if got := windowsFixedVersion(ver); got != test.want {
			t.Errorf("(%d): %q packed to {%#08x, %#08x}, want {%#08x, %#08x}",
				i, test.version, got[0], got[1], test.want[0], test.want[1])
		}
	}
}
