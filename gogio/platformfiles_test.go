// SPDX-License-Identifier: Unlicense OR MIT

package main

import (
	"os"
	"path/filepath"
	"reflect"
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
		pf, err := findPlatformFiles(t.TempDir(), target, []string{"amd64"})
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if pf.manifest != "" || pf.privacy != "" || pf.resources != nil {
			t.Errorf("%s: got %+v, want none", target, *pf)
		}
	}
}

func TestPlatformFilesWithoutFlag(t *testing.T) {
	// Without -platformdir, files in the package directory are not used.
	pf, err := findPlatformFiles("", "ios", nil)
	if err != nil || pf.manifest != "" {
		t.Errorf("got %+v, %v; want none", pf, err)
	}
}

func TestPlatformFilesAndroid(t *testing.T) {
	dir := t.TempDir()
	writePlatformFile(t, dir, "AndroidManifest.xml", `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="org.example.app">
	<uses-sdk android:minSdkVersion="24" android:targetSdkVersion="36"/>
	<application android:label="App"/>
</manifest>`)
	pf, err := findPlatformFiles(dir, "android", nil)
	if err != nil {
		t.Fatal(err)
	}
	if pf.manifest != filepath.Join(dir, "AndroidManifest.xml") || pf.appID != "org.example.app" || pf.minsdk != 24 {
		t.Errorf("got %+v", *pf)
	}
	// The files of other targets are not used.
	if pf, err := findPlatformFiles(dir, "ios", nil); err != nil || pf.manifest != "" {
		t.Errorf("ios: got %+v, %v; want none", pf, err)
	}
}

func TestPlatformFilesAndroidWithoutPackage(t *testing.T) {
	dir := t.TempDir()
	writePlatformFile(t, dir, "AndroidManifest.xml", `<manifest xmlns:android="http://schemas.android.com/apk/res/android"/>`)
	if _, err := findPlatformFiles(dir, "android", nil); err == nil || !strings.Contains(err.Error(), "package") {
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
	writePlatformFile(t, dir, "Info_ios.plist", testInfoPlist)
	writePlatformFile(t, dir, "PrivacyInfo_ios.xcprivacy", "<plist/>")
	pf, err := findPlatformFiles(dir, "ios", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := platformFiles{
		manifest:   filepath.Join(dir, "Info_ios.plist"),
		privacy:    filepath.Join(dir, "PrivacyInfo_ios.xcprivacy"),
		appID:      "org.example.app",
		minsdk:     15,
		executable: "App",
	}
	if !reflect.DeepEqual(*pf, want) {
		t.Errorf("got %+v, want %+v", *pf, want)
	}
}

func TestPlatformFilesFallback(t *testing.T) {
	dir := t.TempDir()
	writePlatformFile(t, dir, "Info.plist", testInfoPlist)
	writePlatformFile(t, dir, "PrivacyInfo.xcprivacy", "<plist/>")
	writePlatformFile(t, dir, "Info_macos.plist", `<plist><dict><key>CFBundleExecutable</key><string>Mac</string><key>CFBundleIdentifier</key><string>org.example.mac</string></dict></plist>`)
	// iOS and tvOS share Info.plist and PrivacyInfo.xcprivacy.
	for _, target := range []string{"ios", "tvos"} {
		pf, err := findPlatformFiles(dir, target, nil)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if pf.manifest != filepath.Join(dir, "Info.plist") || pf.privacy != filepath.Join(dir, "PrivacyInfo.xcprivacy") {
			t.Errorf("%s: got %+v", target, *pf)
		}
	}
	// macOS has its own.
	pf, err := findPlatformFiles(dir, "macos", nil)
	if err != nil {
		t.Fatal(err)
	}
	if pf.manifest != filepath.Join(dir, "Info_macos.plist") || pf.executable != "Mac" || pf.privacy != "" {
		t.Errorf("macos: got %+v", *pf)
	}
}

func TestPlatformFilesWindows(t *testing.T) {
	dir := t.TempDir()
	writePlatformFile(t, dir, "rsrc_windows_amd64.syso", "")
	writePlatformFile(t, dir, "rsrc_windows_arm64.syso", "")
	pf, err := findPlatformFiles(dir, "windows", []string{"amd64", "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"amd64": filepath.Join(dir, "rsrc_windows_amd64.syso"),
		"arm64": filepath.Join(dir, "rsrc_windows_arm64.syso"),
	}
	if !reflect.DeepEqual(pf.resources, want) {
		t.Errorf("got %v, want %v", pf.resources, want)
	}
	// Either every architecture has its resources, or none.
	if _, err := findPlatformFiles(dir, "windows", []string{"amd64", "386"}); err == nil || !strings.Contains(err.Error(), "rsrc_windows_386.syso") {
		t.Errorf("got %v, want an error about rsrc_windows_386.syso", err)
	}
	// Also when no architecture of the build has them.
	if _, err := findPlatformFiles(dir, "windows", []string{"386"}); err == nil || !strings.Contains(err.Error(), "rsrc_windows_386.syso") {
		t.Errorf("got %v, want an error about rsrc_windows_386.syso", err)
	}
}

func TestPlatformFilesPrivacyOnly(t *testing.T) {
	dir := t.TempDir()
	writePlatformFile(t, dir, "PrivacyInfo_tvos.xcprivacy", "<plist/>")
	pf, err := findPlatformFiles(dir, "tvos", nil)
	if err != nil {
		t.Fatal(err)
	}
	if pf.manifest != "" || pf.privacy != filepath.Join(dir, "PrivacyInfo_tvos.xcprivacy") {
		t.Errorf("got %+v", *pf)
	}
}

func TestPlatformFilesInfoPlistErrors(t *testing.T) {
	for name, content := range map[string]string{
		"binary":        "bplist00...",
		"no identifier": `<plist><dict><key>CFBundleExecutable</key><string>App</string></dict></plist>`,
	} {
		dir := t.TempDir()
		writePlatformFile(t, dir, "Info_macos.plist", content)
		if _, err := findPlatformFiles(dir, "macos", nil); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestPlatformFilesFlags(t *testing.T) {
	plist := &platformFiles{manifest: "Info.plist", appID: "org.example.app", minsdk: 15}
	android := &platformFiles{manifest: "AndroidManifest.xml", appID: "org.example.app", minsdk: 24}
	windows := &platformFiles{resources: map[string]string{"amd64": "rsrc_windows_amd64.syso"}}
	for _, tc := range []struct {
		target string
		pf     *platformFiles
		set    string
		appID  string
		minsdk int
		ok     bool
	}{
		{target: "ios", pf: plist, set: "", ok: true},
		{target: "ios", pf: plist, set: "name,icon,signkey,arch", ok: true},
		{target: "ios", pf: plist, set: "appid", appID: "org.example.app", ok: true},
		{target: "ios", pf: plist, set: "appid", appID: "org.example.other"},
		{target: "ios", pf: plist, set: "minsdk", minsdk: 15, ok: true},
		{target: "ios", pf: plist, set: "minsdk", minsdk: 16},
		{target: "ios", pf: plist, set: "version"},
		{target: "ios", pf: plist, set: "schemes"},
		{target: "macos", pf: plist, set: "schemes"},
		{target: "android", pf: android, set: "version"},
		{target: "android", pf: android, set: "schemes"},
		{target: "android", pf: android, set: "targetsdk"},
		{target: "android", pf: android, set: "queries"},
		{target: "android", pf: android, set: "icon", ok: true},
		{target: "windows", pf: windows, set: "version"},
		{target: "windows", pf: windows, set: "icon"},
		{target: "windows", pf: windows, set: "minsdk", minsdk: 10},
		// Gio registers the schemes on Windows at run time.
		{target: "windows", pf: windows, set: "schemes,appid", appID: "org.example.app", ok: true},
	} {
		set := make(map[string]bool)
		for _, f := range strings.Split(tc.set, ",") {
			set[f] = f != ""
		}
		err := tc.pf.checkFlags(tc.target, set, tc.appID, tc.minsdk)
		if (err == nil) != tc.ok {
			t.Errorf("%s flags %q: got %v, want ok=%v", tc.target, tc.set, err, tc.ok)
		}
	}
}

func TestPlatformFilesIOSRequirements(t *testing.T) {
	const scene = `<key>UIApplicationSceneManifest</key><dict><key>UIApplicationSupportsMultipleScenes</key><false/></dict>`
	plist := func(entries string) string {
		return `<plist><dict><key>CFBundleExecutable</key><string>App</string><key>CFBundleIdentifier</key><string>org.example.app</string>` + entries + `</dict></plist>`
	}
	for _, tc := range []struct {
		name    string
		target  string
		entries string
		err     string
	}{
		{"complete", "ios", scene + `<key>MinimumOSVersion</key><string>13.0</string>`, ""},
		{"tvOS", "tvos", scene + `<key>MinimumOSVersion</key><string>14.2</string>`, ""},
		{"no scene manifest", "ios", `<key>MinimumOSVersion</key><string>15.0</string>`, "UIApplicationSceneManifest"},
		{"scene manifest as a string", "ios", `<key>MinimumOSVersion</key><string>15.0</string><key>Other</key><string>UIApplicationSceneManifest</string>`, "UIApplicationSceneManifest"},
		{"iOS 12", "ios", scene + `<key>MinimumOSVersion</key><string>12.4</string>`, "MinimumOSVersion"},
		{"no minimum", "ios", scene, "MinimumOSVersion"},
		{"macOS needs neither", "macos", "", ""},
	} {
		dir := t.TempDir()
		writePlatformFile(t, dir, "Info_"+tc.target+".plist", plist(tc.entries))
		_, err := findPlatformFiles(dir, tc.target, nil)
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%s: %v", tc.name, err)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%s: got %v, want an error about %s", tc.name, err, tc.err)
		}
	}
}
