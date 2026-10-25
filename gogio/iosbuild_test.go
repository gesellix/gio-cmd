// SPDX-License-Identifier: Unlicense OR MIT

package main

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestIOSInfoPlist(t *testing.T) {
	t.Parallel()

	ver, err := parseSemver("1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		target     string
		minsdk     int
		minVersion string
	}{
		{"ios", 0, "13.0"},
		{"ios", 10, "13.0"},
		{"ios", 15, "15.0"},
		{"tvos", 0, "13.0"},
	}
	for _, test := range tests {
		plist := buildInfoPlist(&buildInfo{
			appID:   "com.example.app",
			name:    "app",
			target:  test.target,
			minsdk:  test.minsdk,
			version: ver,
		})
		for _, want := range []string{
			"<key>MinimumOSVersion</key>\n\t<string>" + test.minVersion + "</string>",
			"<key>UIApplicationSceneManifest</key>\n\t<dict>",
		} {
			if !strings.Contains(plist, want) {
				t.Errorf("%s -minsdk %d: Info.plist is missing %q:\n%s", test.target, test.minsdk, want, plist)
			}
		}
		d := xml.NewDecoder(strings.NewReader(plist))
		for {
			if _, err := d.Token(); err != nil {
				if err != io.EOF {
					t.Errorf("%s -minsdk %d: Info.plist is not well-formed: %v", test.target, test.minsdk, err)
				}
				break
			}
		}
	}
}

func TestIOSMinSDK(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ minsdk, want int }{
		{0, 13},
		{12, 13},
		{13, 13},
		{17, 17},
	} {
		if got := iosMinSDK(&buildInfo{minsdk: test.minsdk}); got != test.want {
			t.Errorf("iosMinSDK with -minsdk %d = %d, want %d", test.minsdk, got, test.want)
		}
	}
}
