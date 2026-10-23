// SPDX-License-Identifier: Unlicense OR MIT

package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
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
