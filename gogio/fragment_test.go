// SPDX-License-Identifier: Unlicense OR MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.example.app</string>
	<key>UIRequiresFullScreen</key>
	<true/>
	<key>UIDeviceFamily</key>
	<array>
		<integer>1</integer>
		<integer>2</integer>
	</array>
</dict>
</plist>`

func plistDict(t *testing.T, src []byte) map[string]string {
	t.Helper()
	entries, err := parsePlistDict(src)
	if err != nil {
		t.Fatalf("%v:\n%s", err, src)
	}
	m := make(map[string]string)
	for _, e := range entries {
		m[e.key] = e.value
	}
	return m
}

func TestMergePlist(t *testing.T) {
	t.Parallel()

	extra := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>UIRequiresFullScreen</key>
	<false/>
	<key>NSLocalNetworkUsageDescription</key>
	<string>Sync with your devices &amp; others</string>
	<key>NSBonjourServices</key>
	<array>
		<string>_pwdmgr._tcp</string>
	</array>
</dict>
</plist>`
	fixed := map[string]string{"CFBundleIdentifier": "-appid"}
	merged, err := mergePlist([]byte(testPlist), []byte(extra), fixed)
	if err != nil {
		t.Fatal(err)
	}
	got := plistDict(t, merged)
	want := map[string]string{
		"CFBundleIdentifier":             "<string>com.example.app</string>",
		"UIRequiresFullScreen":           "<false/>",
		"UIDeviceFamily":                 "<array>\n\t\t<integer>1</integer>\n\t\t<integer>2</integer>\n\t</array>",
		"NSLocalNetworkUsageDescription": "<string>Sync with your devices &amp; others</string>",
		"NSBonjourServices":              "<array>\n\t\t<string>_pwdmgr._tcp</string>\n\t</array>",
	}
	if len(got) != len(want) {
		t.Errorf("got %d keys, want %d:\n%s", len(got), len(want), merged)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	// Generated keys keep their place, new ones follow.
	entries, _ := parsePlistDict(merged)
	var order []string
	for _, e := range entries {
		order = append(order, e.key)
	}
	if o := strings.Join(order, ","); o != "CFBundleIdentifier,UIRequiresFullScreen,UIDeviceFamily,NSLocalNetworkUsageDescription,NSBonjourServices" {
		t.Errorf("key order %s", o)
	}
}

func TestMergePlistErrors(t *testing.T) {
	t.Parallel()

	fixed := map[string]string{"CFBundleIdentifier": "-appid"}
	tests := []struct {
		name, extra, err string
	}{
		{"fixed key", `<plist><dict><key>CFBundleIdentifier</key><string>x</string></dict></plist>`, "key CFBundleIdentifier is set by -appid"},
		{"twice", `<plist><dict><key>A</key><true/><key>A</key><false/></dict></plist>`, "key A is set twice"},
		{"no value", `<plist><dict><key>A</key></dict></plist>`, `key "A" has no value`},
		{"no key", `<plist><dict><string>x</string></dict></plist>`, "<string> without a key"},
		{"not a dict", `<plist><array/></plist>`, "expected a <plist> with a <dict>"},
		{"not a plist", `<dict/>`, "expected a <plist> with a <dict>"},
		{"malformed", `<plist><dict><key>A</key><string>x</dict></plist>`, "element <string> closed by </dict>"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := mergePlist([]byte(testPlist), []byte(test.extra), fixed)
			if err == nil || !strings.Contains(err.Error(), test.err) {
				t.Errorf("got error %v, want %q", err, test.err)
			}
		})
	}
}

func TestIOSInfoPlistFixed(t *testing.T) {
	t.Parallel()

	ver, err := parseSemver("1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	bi := &buildInfo{appID: "com.example.app", name: "app", target: "ios", minsdk: 15, version: ver, schemes: []string{"app"}}
	base := []byte(buildInfoPlist(bi))
	got := plistDict(t, base)
	for k := range iosPlistFixed(bi) {
		if _, ok := got[k]; !ok {
			t.Errorf("fixed key %s is not in the generated Info.plist", k)
		}
	}
	extra := `<plist><dict><key>UIApplicationSceneManifest</key><dict><key>UIApplicationSupportsMultipleScenes</key><false/></dict></dict></plist>`
	merged, err := mergePlist(base, []byte(extra), iosPlistFixed(bi))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged), "<key>UIApplicationSceneManifest</key>\n\t<dict><key>UIApplicationSupportsMultipleScenes</key><false/></dict>") {
		t.Errorf("scene manifest missing:\n%s", merged)
	}
}

func TestCopyPrivacyManifest(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Test.app")
	if err := os.Mkdir(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyPrivacyManifest(app, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(app, "PrivacyInfo.xcprivacy")); !os.IsNotExist(err) {
		t.Fatalf("no path, but a manifest was added: %v", err)
	}

	valid := filepath.Join(dir, "valid.xcprivacy")
	src := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>NSPrivacyTracking</key><false/></dict></plist>`
	if err := os.WriteFile(valid, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyPrivacyManifest(app, valid); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(app, "PrivacyInfo.xcprivacy")); err != nil || string(got) != src {
		t.Fatalf("copied %q, %v", got, err)
	}

	invalid := filepath.Join(dir, "invalid.xcprivacy")
	if err := os.WriteFile(invalid, []byte(`<dict/>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyPrivacyManifest(app, invalid); err == nil || !strings.Contains(err.Error(), "-privacymanifest") {
		t.Errorf("a manifest that is no property list: %v", err)
	}
}
