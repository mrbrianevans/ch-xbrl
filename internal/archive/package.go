package archive

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// packageKind is how a zip is read. Bulk archives stream every instance
// member. A report package or a Companies House accounts package is one filing.
type packageKind int

const (
	packageBulk packageKind = iota
	packageReport
	packageCH
)

// packageSelection is the reports to emit from a package zip.
// Files are archive paths. UKFRS is set for a report package: the parser
// keeps only facts whose target attribute is UKFRS.
type packageSelection struct {
	Kind  packageKind
	Files []string
	UKFRS bool
}

// prefixCRN matches a Companies House package top-level folder, PREFIX-CRN.
// The CRN is optional letters (up to two) plus 6–8 digits.
var prefixCRN = regexp.MustCompile(`(?i)^[A-Za-z][A-Za-z0-9]*-[A-Z]{0,2}[0-9]{6,8}$`)

const (
	reportPackageJSON = "META-INF/reportPackage.json"
	reportsDirName    = "reports"
)

// selectPackage classifies zip entry names and returns the reports to emit.
// Directory entries are optional: a path implies its parent directories.
// A trailing slash is ignored. A package with no selected report has
// Kind set and an empty Files slice; the caller reports that as a member error.
func selectPackage(names []string) packageSelection {
	norm := normaliseZipNames(names)
	if len(norm) == 0 {
		return packageSelection{}
	}
	if prefix, ok := reportPackageRoot(norm); ok {
		return packageSelection{
			Kind:  packageReport,
			Files: discoverReports(norm, prefix),
			UKFRS: true,
		}
	}
	if chPackageRoot(norm) {
		return packageSelection{
			Kind:  packageCH,
			Files: selectCHReports(norm),
		}
	}
	return packageSelection{Kind: packageBulk}
}

func normaliseZipNames(names []string) []string {
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.Trim(path.Clean("/"+strings.ReplaceAll(name, "\\", "/")), "/")
		if name == "" || name == "." {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// reportPackageRoot reports whether names are an XBRL report package.
// Markers are META-INF/reportPackage.json or a reports directory, either at
// the zip root or under the single top-level directory (the STLD).
// The returned prefix is "" at the root, or the STLD name.
func reportPackageRoot(names []string) (string, bool) {
	if hasMarker(names, "") {
		return "", true
	}
	roots := topLevels(names)
	if len(roots) == 1 && hasMarker(names, roots[0]) {
		return roots[0], true
	}
	return "", false
}

func hasMarker(names []string, prefix string) bool {
	jsonName := reportPackageJSON
	reports := reportsDirName
	if prefix != "" {
		jsonName = prefix + "/" + reportPackageJSON
		reports = prefix + "/" + reportsDirName
	}
	for _, name := range names {
		if name == jsonName {
			return true
		}
		if name == reports || strings.HasPrefix(name, reports+"/") {
			return true
		}
	}
	return false
}

// chPackageRoot reports a single top-level folder named PREFIX-CRN.
func chPackageRoot(names []string) bool {
	roots := topLevels(names)
	return len(roots) == 1 && prefixCRN.MatchString(roots[0])
}

func topLevels(names []string) []string {
	seen := map[string]bool{}
	var roots []string
	for _, name := range names {
		i := strings.IndexByte(name, '/')
		top := name
		if i >= 0 {
			top = name[:i]
		}
		if top == "" || seen[top] {
			continue
		}
		seen[top] = true
		roots = append(roots, top)
	}
	sort.Strings(roots)
	return roots
}

// discoverReports implements XBRL Report Package §5.2 under prefix/reports.
// prefix is empty when reports/ sits at the zip root.
// .json reports are not parsed. A subdirectory with several non-HTML
// recognised files (rpe:multipleReportsInSubdirectory) contributes nothing.
func discoverReports(names []string, prefix string) []string {
	root := reportsDirName
	if prefix != "" {
		root = prefix + "/" + reportsDirName
	}
	var direct []string
	subs := map[string][]string{}
	for _, name := range names {
		rel, ok := strings.CutPrefix(name, root+"/")
		if !ok || rel == "" || strings.HasSuffix(name, "/") {
			continue
		}
		if !memberNameOK(name) {
			continue
		}
		parts := strings.Split(rel, "/")
		switch len(parts) {
		case 1:
			if isReportExt(parts[0]) {
				direct = append(direct, name)
			}
		case 2:
			if isReportExt(parts[1]) {
				subs[parts[0]] = append(subs[parts[0]], name)
			}
		default:
			// Nested subdirectories are ignored for discovery.
		}
	}
	if len(direct) > 0 {
		sort.Strings(direct)
		return direct
	}
	subNames := make([]string, 0, len(subs))
	for sub := range subs {
		subNames = append(subNames, sub)
	}
	sort.Strings(subNames)
	var out []string
	for _, sub := range subNames {
		files := subs[sub]
		sort.Strings(files)
		if len(files) == 1 || allInlineExt(files) {
			out = append(out, files...)
		}
	}
	return out
}

func selectCHReports(names []string) []string {
	var subsidiary, accounts []string
	for _, name := range names {
		if !memberNameOK(name) || isZipName(name) || !isXBRLName(name) {
			continue
		}
		switch {
		case hasPathSegment(name, "subsidiary-accounts"):
			subsidiary = append(subsidiary, name)
		case hasPathSegment(name, "accounts"):
			accounts = append(accounts, name)
		}
	}
	if len(subsidiary) > 0 {
		return subsidiary
	}
	return accounts
}

// recognised report extensions from Report Package §5.1, excluding .json.
func isReportExt(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".xhtml") ||
		strings.HasSuffix(l, ".html") ||
		strings.HasSuffix(l, ".htm") ||
		strings.HasSuffix(l, ".xbrl")
}

func allInlineExt(names []string) bool {
	if len(names) == 0 {
		return false
	}
	for _, name := range names {
		l := strings.ToLower(name)
		if !strings.HasSuffix(l, ".xhtml") && !strings.HasSuffix(l, ".html") && !strings.HasSuffix(l, ".htm") {
			return false
		}
	}
	return true
}

// packageSkipNames returns inner members that are logged and not emitted:
// zip files, and iXBRL/XBRL files that were not selected.
func packageSkipNames(names []string, selected []string) (zips, instances []string) {
	keep := map[string]bool{}
	for _, name := range selected {
		keep[name] = true
	}
	for _, name := range normaliseZipNames(names) {
		if keep[name] || !memberNameOK(name) {
			continue
		}
		base := path.Base(name)
		if strings.HasSuffix(name, "/") || base == "" {
			continue
		}
		switch {
		case isZipName(name):
			zips = append(zips, name)
		case isXBRLName(name):
			instances = append(instances, name)
		}
	}
	return zips, instances
}
