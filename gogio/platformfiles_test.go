// SPDX-License-Identifier: Unlicense OR MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePlatformFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPlatformFilesNone(t *testing.T) {
	for _, target := range []string{"android", "ios", "tvos", "macos", "windows", "js"} {
		pf, err := findPlatformFiles(t.TempDir(), target)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if *pf != (platformFiles{}) {
			t.Errorf("%s: got %+v, want none", target, *pf)
		}
	}
}

func TestPlatformFilesAndroid(t *testing.T) {
	dir := t.TempDir()
	writePlatformFile(t, dir, "android/AndroidManifest.xml", `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="org.example.app">
	<uses-sdk android:minSdkVersion="24" android:targetSdkVersion="36"/>
	<application android:label="App"/>
</manifest>`)
	pf, err := findPlatformFiles(dir, "android")
	if err != nil {
		t.Fatal(err)
	}
	if pf.manifest != filepath.Join(dir, "android", "AndroidManifest.xml") || pf.appID != "org.example.app" || pf.minsdk != 24 {
		t.Errorf("got %+v", *pf)
	}
	// The files of other targets are not used.
	if pf, err := findPlatformFiles(dir, "ios"); err != nil || pf.manifest != "" {
		t.Errorf("ios: got %+v, %v; want none", pf, err)
	}
}

func TestPlatformFilesAndroidWithoutPackage(t *testing.T) {
	dir := t.TempDir()
	writePlatformFile(t, dir, "android/AndroidManifest.xml", `<manifest xmlns:android="http://schemas.android.com/apk/res/android"/>`)
	if _, err := findPlatformFiles(dir, "android"); err == nil || !strings.Contains(err.Error(), "package") {
		t.Errorf("got %v, want an error about the package", err)
	}
}

const testInfoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>App</string>
	<key>CFBundleIdentifier</key>
	<string>org.example.app</string>
	<key>UIApplicationSceneManifest</key>
	<dict>
		<key>CFBundleIdentifier</key>
		<string>not.the.top.level</string>
		<key>UIApplicationSupportsMultipleScenes</key>
		<false/>
	</dict>
	<key>MinimumOSVersion</key>
	<string>15.0</string>
</dict>
</plist>`

func TestPlatformFilesIOS(t *testing.T) {
	dir := t.TempDir()
	writePlatformFile(t, dir, "ios/Info.plist", testInfoPlist)
	writePlatformFile(t, dir, "ios/PrivacyInfo.xcprivacy", "<plist/>")
	pf, err := findPlatformFiles(dir, "ios")
	if err != nil {
		t.Fatal(err)
	}
	want := platformFiles{
		manifest:   filepath.Join(dir, "ios", "Info.plist"),
		privacy:    filepath.Join(dir, "ios", "PrivacyInfo.xcprivacy"),
		appID:      "org.example.app",
		minsdk:     15,
		executable: "App",
	}
	if *pf != want {
		t.Errorf("got %+v, want %+v", *pf, want)
	}
}

func TestPlatformFilesPrivacyOnly(t *testing.T) {
	dir := t.TempDir()
	writePlatformFile(t, dir, "tvos/PrivacyInfo.xcprivacy", "<plist/>")
	pf, err := findPlatformFiles(dir, "tvos")
	if err != nil {
		t.Fatal(err)
	}
	if pf.manifest != "" || pf.privacy != filepath.Join(dir, "tvos", "PrivacyInfo.xcprivacy") {
		t.Errorf("got %+v", *pf)
	}
}

func TestPlatformFilesInfoPlistErrors(t *testing.T) {
	for name, content := range map[string]string{
		"binary":        "bplist00...",
		"no identifier": `<plist><dict><key>CFBundleExecutable</key><string>App</string></dict></plist>`,
	} {
		dir := t.TempDir()
		writePlatformFile(t, dir, "macos/Info.plist", content)
		if _, err := findPlatformFiles(dir, "macos"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestPlatformFilesFlags(t *testing.T) {
	pf := &platformFiles{manifest: "Info.plist", appID: "org.example.app", minsdk: 15}
	for _, tc := range []struct {
		set    string
		appID  string
		minsdk int
		ok     bool
	}{
		{set: "", ok: true},
		{set: "name,icon,signkey,arch", ok: true},
		{set: "appid", appID: "org.example.app", ok: true},
		{set: "appid", appID: "org.example.other"},
		{set: "minsdk", minsdk: 15, ok: true},
		{set: "minsdk", minsdk: 16},
		{set: "version"},
		{set: "schemes"},
		{set: "targetsdk"},
		{set: "queries"},
	} {
		set := make(map[string]bool)
		for _, f := range strings.Split(tc.set, ",") {
			set[f] = f != ""
		}
		err := pf.checkFlags(set, tc.appID, tc.minsdk)
		if (err == nil) != tc.ok {
			t.Errorf("flags %q: got %v, want ok=%v", tc.set, err, tc.ok)
		}
	}
}
