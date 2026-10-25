// SPDX-License-Identifier: Unlicense OR MIT

package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Complete platform files replace the ones gogio generates. They are looked
// up in a folder named after the target, below -platformdir (by default the
// directory of the main package):
//
//	android/AndroidManifest.xml
//	ios/Info.plist, ios/PrivacyInfo.xcprivacy
//	tvos/Info.plist, tvos/PrivacyInfo.xcprivacy
//	macos/Info.plist
//
// gogio uses them as they are, and takes the values it needs beyond the
// manifest from them: the app id (for app.ID and for signing), the minimum
// SDK (for the compiler) and, on Apple platforms, the name of the
// executable.
type platformFiles struct {
	// manifest is the path of the AndroidManifest.xml or Info.plist, or
	// empty.
	manifest string
	// privacy is the path of the privacy manifest, or empty.
	privacy string
	// The values read from the manifest; zero if it has none.
	appID      string
	minsdk     int
	executable string
}

// manifestFlags are the flags whose values only end up in the generated
// manifest, so they have no effect with a complete one.
var manifestFlags = []string{"version", "schemes", "targetsdk", "queries"}

func findPlatformFiles(dir, target string) (*platformFiles, error) {
	pf := new(platformFiles)
	if *buildMode != "exe" {
		return pf, nil
	}
	dir = filepath.Join(dir, target)
	exists := func(name string) (string, error) {
		p := filepath.Join(dir, name)
		switch _, err := os.Stat(p); {
		case err == nil:
			return p, nil
		case errors.Is(err, os.ErrNotExist):
			return "", nil
		default:
			return "", err
		}
	}
	var err error
	switch target {
	case "android":
		if pf.manifest, err = exists("AndroidManifest.xml"); err != nil || pf.manifest == "" {
			return pf, err
		}
		err = pf.readAndroidManifest()
	case "ios", "tvos", "macos":
		if target != "macos" {
			if pf.privacy, err = exists("PrivacyInfo.xcprivacy"); err != nil {
				return nil, err
			}
		}
		if pf.manifest, err = exists("Info.plist"); err != nil || pf.manifest == "" {
			return pf, err
		}
		err = pf.readInfoPlist(target)
	}
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return pf, pf.checkFlags(set, *appID, *minsdk)
}

// checkFlags reports the flags, of those set, that a complete manifest
// makes useless or contradicts.
func (pf *platformFiles) checkFlags(set map[string]bool, appID string, minsdk int) error {
	for _, name := range manifestFlags {
		if set[name] {
			return fmt.Errorf("-%s has no effect with %s; set it there", name, pf.manifest)
		}
	}
	if set["appid"] && pf.appID != "" && appID != pf.appID {
		return fmt.Errorf("-appid %s differs from %s in %s", appID, pf.appID, pf.manifest)
	}
	if set["minsdk"] && pf.minsdk != 0 && minsdk != pf.minsdk {
		return fmt.Errorf("-minsdk %d differs from %d in %s", minsdk, pf.minsdk, pf.manifest)
	}
	return nil
}

func (pf *platformFiles) readAndroidManifest() error {
	var m struct {
		Package string `xml:"package,attr"`
		UsesSDK struct {
			MinSDK string `xml:"http://schemas.android.com/apk/res/android minSdkVersion,attr"`
		} `xml:"uses-sdk"`
	}
	data, err := os.ReadFile(pf.manifest)
	if err != nil {
		return err
	}
	if err := xml.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("%s: %v", pf.manifest, err)
	}
	if m.Package == "" {
		return fmt.Errorf("%s: the <manifest> element has no package attribute", pf.manifest)
	}
	pf.appID = m.Package
	if m.UsesSDK.MinSDK != "" {
		if pf.minsdk, err = strconv.Atoi(m.UsesSDK.MinSDK); err != nil {
			return fmt.Errorf("%s: android:minSdkVersion %q is not a number", pf.manifest, m.UsesSDK.MinSDK)
		}
	}
	return nil
}

func (pf *platformFiles) readInfoPlist(target string) error {
	data, err := os.ReadFile(pf.manifest)
	if err != nil {
		return err
	}
	if bytes.HasPrefix(data, []byte("bplist")) {
		return fmt.Errorf("%s: a binary property list; convert it with plutil -convert xml1", pf.manifest)
	}
	keys, present, err := plistTopLevel(data)
	if err != nil {
		return fmt.Errorf("%s: %v", pf.manifest, err)
	}
	pf.appID = keys["CFBundleIdentifier"]
	pf.executable = keys["CFBundleExecutable"]
	if pf.appID == "" || pf.executable == "" {
		return fmt.Errorf("%s: CFBundleIdentifier and CFBundleExecutable are required", pf.manifest)
	}
	if v := keys["MinimumOSVersion"]; v != "" {
		major, _, _ := strings.Cut(v, ".")
		if pf.minsdk, err = strconv.Atoi(major); err != nil {
			return fmt.Errorf("%s: MinimumOSVersion %q is not a version", pf.manifest, v)
		}
	}
	if target == "macos" {
		return nil
	}
	// Gio needs the UIScene life cycle, which an app only gets with the
	// scene manifest, and so iOS or tvOS 13.
	if pf.minsdk < minIOSVersion {
		return fmt.Errorf("%s: MinimumOSVersion must be %d.0 or later, which Gio needs", pf.manifest, minIOSVersion)
	}
	if !present["UIApplicationSceneManifest"] {
		return fmt.Errorf("%s: UIApplicationSceneManifest is missing; without it the app starts without a window", pf.manifest)
	}
	return nil
}

// plistTopLevel returns the string values of the top-level dictionary of an
// XML property list, and the keys it has, of any type.
func plistTopLevel(data []byte) (map[string]string, map[string]bool, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	vals := make(map[string]string)
	present := make(map[string]bool)
	depth := 0
	key := ""
	for {
		t, err := d.Token()
		if err == io.EOF {
			return vals, present, nil
		}
		if err != nil {
			return nil, nil, err
		}
		switch t := t.(type) {
		case xml.StartElement:
			depth++
			// depth 1 is <plist>, 2 the top-level <dict>, 3 its entries.
			if depth != 3 {
				continue
			}
			if t.Name.Local != "key" && key != "" {
				present[key] = true
			}
			if t.Name.Local != "key" && t.Name.Local != "string" {
				// A dictionary or array value: skip its contents.
				if err := d.Skip(); err != nil {
					return nil, nil, err
				}
				depth--
				key = ""
				continue
			}
			var s string
			if err := d.DecodeElement(&s, &t); err != nil {
				return nil, nil, err
			}
			depth--
			switch t.Name.Local {
			case "key":
				key = s
			case "string":
				vals[key] = s
				key = ""
			}
		case xml.EndElement:
			depth--
		}
	}
}
