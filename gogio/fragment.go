// SPDX-License-Identifier: Unlicense OR MIT

package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// plistEntry is a key of a property list dictionary with its value as
// raw XML.
type plistEntry struct {
	key   string
	value string
}

// parsePlistDict returns the entries of the top-level dictionary of a
// property list in XML format, in order.
func parsePlistDict(src []byte) ([]plistEntry, error) {
	d := xml.NewDecoder(bytes.NewReader(src))
	var (
		entries []plistEntry
		path    []string
		key     *string
	)
	for {
		off := d.InputOffset()
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			name := tok.Name.Local
			inDict := slices.Equal(path, []string{"plist", "dict"})
			switch {
			case len(path) == 0 && name != "plist",
				len(path) == 1 && name != "dict":
				return nil, fmt.Errorf("expected a <plist> with a <dict>, found <%s>", name)
			case inDict && name == "key":
				var k string
				if err := d.DecodeElement(&k, &tok); err != nil {
					return nil, err
				}
				if key != nil {
					return nil, fmt.Errorf("key %q has no value", *key)
				}
				key = &k
				continue
			case inDict:
				if key == nil {
					return nil, fmt.Errorf("<%s> without a key", name)
				}
				if err := d.Skip(); err != nil {
					return nil, err
				}
				entries = append(entries, plistEntry{key: *key, value: string(src[off:d.InputOffset()])})
				key = nil
				continue
			}
			path = append(path, name)
		case xml.EndElement:
			path = path[:len(path)-1]
		}
	}
	if key != nil {
		return nil, fmt.Errorf("key %q has no value", *key)
	}
	return entries, nil
}

// mergePlist adds the top-level entries of the property list extra to
// the one in base, replacing entries of the same key. The fixed map holds
// keys that must not be replaced, with the flag that sets them.
func mergePlist(base, extra []byte, fixed map[string]string) ([]byte, error) {
	baseEntries, err := parsePlistDict(base)
	if err != nil {
		return nil, fmt.Errorf("generated Info.plist: %w", err)
	}
	extraEntries, err := parsePlistDict(extra)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, e := range extraEntries {
		if seen[e.key] {
			return nil, fmt.Errorf("key %s is set twice", e.key)
		}
		seen[e.key] = true
		if flag, ok := fixed[e.key]; ok {
			return nil, fmt.Errorf("key %s is set by %s", e.key, flag)
		}
		i := slices.IndexFunc(baseEntries, func(b plistEntry) bool { return b.key == e.key })
		if i >= 0 {
			baseEntries[i] = e
		} else {
			baseEntries = append(baseEntries, e)
		}
	}
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	for _, e := range baseEntries {
		b.WriteString("\t<key>")
		xml.EscapeText(&b, []byte(e.key))
		b.WriteString("</key>\n\t")
		b.WriteString(e.value)
		b.WriteString("\n")
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes(), nil
}

// mergePlistFile merges the property list file at path into base, see
// mergePlist. An empty path returns base unchanged.
func mergePlistFile(base []byte, path string, fixed map[string]string) ([]byte, error) {
	if path == "" {
		return base, nil
	}
	extra, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	merged, err := mergePlist(base, extra, fixed)
	if err != nil {
		return nil, fmt.Errorf("-plist %s: %w", path, err)
	}
	return merged, nil
}

// copyPrivacyManifest copies the privacy manifest at path into the app
// bundle as PrivacyInfo.xcprivacy, where Apple expects it. It
// must be a property list in XML format with a dictionary; an empty path
// adds nothing.
func copyPrivacyManifest(app, path string) error {
	if path == "" {
		return nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := parsePlistDict(src); err != nil {
		return fmt.Errorf("-privacymanifest %s: %w", path, err)
	}
	return os.WriteFile(filepath.Join(app, "PrivacyInfo.xcprivacy"), src, 0o644)
}

const androidNS = "http://schemas.android.com/apk/res/android"

// manifestExtra holds the parts of an Android manifest fragment that go
// into the generated manifest, as raw XML.
type manifestExtra struct {
	// ManifestAttrs and AppAttrs are attributes for the <manifest> and
	// <application> elements, each with a leading space.
	ManifestAttrs string
	AppAttrs      string
	// Manifest and App are child elements of <manifest> and
	// <application>.
	Manifest string
	App      string
}

// manifestFixed lists what the generated manifest sets from flags: the
// attributes of <manifest> and <application> and the permissions it
// requests.
type manifestFixed struct {
	manifestAttrs map[string]string
	appAttrs      map[string]string
	permissions   []string
}

// parseManifestExtra reads an Android manifest fragment: a <manifest>
// element whose attributes and children are added to the generated
// manifest, and whose <application> element adds its attributes and
// children to the generated <application>. Attributes the generated
// manifest sets from flags, <uses-sdk> and the Gio activity are errors;
// permissions the generated manifest requests already are left out.
func parseManifestExtra(src []byte, fixed manifestFixed) (manifestExtra, error) {
	var extra manifestExtra
	d := xml.NewDecoder(bytes.NewReader(src))
	var (
		depth   int
		inApp   bool
		seenApp bool
	)
	for {
		off := d.InputOffset()
		tok, err := d.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return manifestExtra{}, err
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			name := rawName(tok.Name)
			switch {
			case depth == 0:
				if name != "manifest" {
					return manifestExtra{}, fmt.Errorf("expected <manifest>, found <%s>", name)
				}
				attrs, err := extraAttrs(tok, fixed.manifestAttrs, true)
				if err != nil {
					return manifestExtra{}, err
				}
				extra.ManifestAttrs = attrs
			case depth == 1 && name == "application":
				if seenApp {
					return manifestExtra{}, errors.New("more than one <application>")
				}
				seenApp, inApp = true, true
				attrs, err := extraAttrs(tok, fixed.appAttrs, false)
				if err != nil {
					return manifestExtra{}, err
				}
				extra.AppAttrs = attrs
			case depth == 1:
				end, err := skipRaw(d)
				if err != nil {
					return manifestExtra{}, err
				}
				switch {
				case name == "uses-sdk":
					return manifestExtra{}, errors.New("<uses-sdk> is set by -minsdk and -targetsdk")
				case name == "uses-permission" && slices.Contains(fixed.permissions, attr(tok, "android:name")):
					// Requested already.
					continue
				}
				extra.Manifest += "\t" + string(src[off:end]) + "\n"
				continue
			case depth == 2 && inApp:
				end, err := skipRaw(d)
				if err != nil {
					return manifestExtra{}, err
				}
				if name == "activity" && attr(tok, "android:name") == "org.gioui.GioActivity" {
					return manifestExtra{}, errors.New("the activity org.gioui.GioActivity is generated by gogio")
				}
				extra.App += "\t\t" + string(src[off:end]) + "\n"
				continue
			}
			depth++
		case xml.EndElement:
			depth--
			if depth == 1 {
				inApp = false
			}
		}
	}
	return extra, nil
}

// parseManifestExtraFile reads the manifest fragment at path, see
// parseManifestExtra. An empty path returns an empty fragment.
func parseManifestExtraFile(path string, fixed manifestFixed) (manifestExtra, error) {
	if path == "" {
		return manifestExtra{}, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return manifestExtra{}, err
	}
	extra, err := parseManifestExtra(src, fixed)
	if err != nil {
		return manifestExtra{}, fmt.Errorf("-manifest %s: %w", path, err)
	}
	return extra, nil
}

// extraAttrs returns the attributes of a fragment's <manifest> or
// <application> element for the generated element, each with a leading
// space. The fixed map holds attributes the generated element sets, with
// the flag that sets them. Namespace declarations are allowed on
// <manifest> only, and only for the android namespace, which the
// generated manifest declares.
func extraAttrs(tok xml.StartElement, fixed map[string]string, root bool) (string, error) {
	var b strings.Builder
	for _, a := range tok.Attr {
		name := rawName(a.Name)
		if a.Name.Space == "xmlns" || name == "xmlns" {
			if !root || name != "xmlns:android" || a.Value != androidNS {
				return "", fmt.Errorf("namespace declaration %s=%q: only xmlns:android=%q is supported", name, a.Value, androidNS)
			}
			continue
		}
		if a.Name.Space != "" && a.Name.Space != "android" {
			return "", fmt.Errorf("attribute %s: only the android namespace is supported", name)
		}
		if flag, ok := fixed[name]; ok {
			return "", fmt.Errorf("attribute %s of <%s> is set by %s", name, rawName(tok.Name), flag)
		}
		b.WriteString(" ")
		b.WriteString(name)
		b.WriteString(`="`)
		xml.EscapeText(&b, []byte(a.Value))
		b.WriteString(`"`)
	}
	return b.String(), nil
}

// skipRaw reads the tokens up to the end of the element whose start
// element was just read, and returns the input offset after it.
func skipRaw(d *xml.Decoder) (int64, error) {
	depth := 1
	for depth > 0 {
		tok, err := d.RawToken()
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return d.InputOffset(), nil
}

// rawName returns the name as written, with its prefix.
func rawName(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

// attr returns the value of the attribute with the raw name, or "".
func attr(tok xml.StartElement, name string) string {
	for _, a := range tok.Attr {
		if rawName(a.Name) == name {
			return a.Value
		}
	}
	return ""
}
