// SPDX-License-Identifier: Unlicense OR MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLatestPlatform(t *testing.T) {
	sdk := t.TempDir()
	for _, p := range []string{"android-34", "android-36", "android-36.1", "android-37.0", "android-TiramisuPrivacySandbox"} {
		if err := os.MkdirAll(filepath.Join(sdk, "platforms", p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := latestPlatform(sdk)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(sdk, "platforms", "android-37.0"); got != want {
		t.Errorf("latestPlatform = %s, want %s", got, want)
	}
}
