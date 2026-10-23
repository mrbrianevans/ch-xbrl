package xmlscan

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// The oracle is encoding/xml configured the way ch-xbrl configured it before
// this package: Strict false, HTML entities, AutoClose unset, ASCII charset.

func asciiCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "ascii", "us-ascii":
		return input, nil
	default:
		return nil, fmt.Errorf("unsupported charset %q", charset)
	}
}

// event is one token as the parser sees it. Adjacent character data is
// merged; comments, processing instructions and directives are dropped.
type event struct {
	kind  Kind
	space string
	local string
	attrs []xml.Attr
	text  string
}

func (e event) String() string {
	switch e.kind {
	case StartElement:
		var b strings.Builder
		fmt.Fprintf(&b, "<{%s}%s", e.space, e.local)
		for _, a := range e.attrs {
			fmt.Fprintf(&b, " {%s}%s=%q", a.Name.Space, a.Name.Local, a.Value)
		}
		return b.String() + ">"
	case EndElement:
		return fmt.Sprintf("</{%s}%s>", e.space, e.local)
	}
	return fmt.Sprintf("text %q", e.text)
}

func appendEvent(evs []event, e event) []event {
	if e.kind == CharData && len(evs) > 0 && evs[len(evs)-1].kind == CharData {
		evs[len(evs)-1].text += e.text
		return evs
	}
	return append(evs, e)
}

func stdEvents(data []byte) ([]event, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = asciiCharsetReader
	var evs []event
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return evs, nil
		}
		if err != nil {
			return evs, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			evs = appendEvent(evs, event{kind: StartElement, space: t.Name.Space, local: t.Name.Local, attrs: append([]xml.Attr{}, t.Attr...)})
		case xml.EndElement:
			evs = appendEvent(evs, event{kind: EndElement, space: t.Name.Space, local: t.Name.Local})
		case xml.CharData:
			evs = appendEvent(evs, event{kind: CharData, text: string(t)})
		}
	}
}

func scanEvents(data []byte) ([]event, error) {
	s := New(data)
	var evs []event
	for {
		k, err := s.Next()
		if err == io.EOF {
			return evs, nil
		}
		if err != nil {
			return evs, err
		}
		switch k {
		case StartElement:
			e := event{kind: StartElement, space: s.Space(), local: string(s.Local()), attrs: []xml.Attr{}}
			for i := 0; i < s.NumAttr(); i++ {
				e.attrs = append(e.attrs, xml.Attr{
					Name:  xml.Name{Space: s.AttrSpace(i), Local: string(s.AttrLocal(i))},
					Value: s.AttrValue(i),
				})
			}
			evs = appendEvent(evs, e)
		case EndElement:
			evs = appendEvent(evs, event{kind: EndElement, space: s.Space(), local: string(s.Local())})
		case CharData:
			evs = appendEvent(evs, event{kind: CharData, text: string(s.Text())})
		}
	}
}

func equalEvents(a, b []event) (int, bool) {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		x, y := a[i], b[i]
		if x.kind != y.kind || x.space != y.space || x.local != y.local || x.text != y.text || len(x.attrs) != len(y.attrs) {
			return i, false
		}
		for j := range x.attrs {
			if x.attrs[j] != y.attrs[j] {
				return i, false
			}
		}
	}
	return n, len(a) == len(b)
}

// parity returns "" when the scanner and the oracle agree on data: the same
// events and, when either fails, both fail.
func parity(data []byte) string {
	want, werr := stdEvents(data)
	got, gerr := scanEvents(data)
	if (werr == nil) != (gerr == nil) {
		return fmt.Sprintf("error mismatch: encoding/xml %v, xmlscan %v", werr, gerr)
	}
	if i, ok := equalEvents(want, got); !ok {
		var b strings.Builder
		fmt.Fprintf(&b, "event %d differs (encoding/xml %d events err=%v, xmlscan %d events err=%v)\n", i, len(want), werr, len(got), gerr)
		for j := max(0, i-2); j < i+3; j++ {
			var w, g string
			if j < len(want) {
				w = want[j].String()
			}
			if j < len(got) {
				g = got[j].String()
			}
			fmt.Fprintf(&b, "  %d\n    encoding/xml %s\n    xmlscan      %s\n", j, w, g)
		}
		return b.String()
	}
	return ""
}

// stripXMLPreamble mirrors the parser: a UTF-8 BOM and anything before the
// first '<' are removed before tokenising.
func stripXMLPreamble(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if i := bytes.IndexByte(data, '<'); i > 0 {
		data = data[i:]
	}
	return data
}

var handCases = []string{
	``,
	`plain text`,
	`<a/>`,
	`<a />`,
	`<a/ >`,
	`<a></a>`,
	`<a>x</a>trailing`,
	`<a>x</a>trailing &amp`,
	`<a>x</a>&`,
	`<a>x</a>&#12`,
	`<a>x</a>&#x`,
	`<a>x</a>&#`,
	`<a>`,
	`</a>`,
	`<a></b>`,
	`<a><b></a>`,
	`<a><b><c></a>`,
	`<a><b>t</a>u`,
	`<html><body><p>one<p>two</body></html>`,
	`<a xmlns:p="u"><p:b xmlns:p="v"><p:c/></p:b><p:d/></a>`,
	`<a xmlns:p="u"><b xmlns="v"></a>`,
	`<a xmlns:p="u"><b xmlns:q="v"><c></b></a>`,
	`<p:a xmlns:p="u"></q:a>`,
	`<p:a></p:a>`,
	`<p:a xmlns:p="u"></p:a>`,
	`<a xmlns="d"><b/><c xmlns=""><d/></c><e/></a>`,
	`<a xmlns="d" xmlns:x="u"><x:b x:at="1" at="2" xml:lang="en" y:at="3"/></a>`,
	`<a xmlns:xml="other"><xml:b xml:c="1"/></a>`,
	`<xmlns a="1"/>`,
	`<xmlns:a xmlns:xmlns="u"/>`,
	`<a xmlns:f="http://www.w3.org/2000/xmlns/" f:p="v"/>`,
	`<a xmlns="one" xmlns="two"><b/></a>`,
	`<a xmlns:p="one" xmlns:p="two"><p:b/></a><p:c/>`,
	`<a xmlns:="x"/>`,
	`<a :xmlns="x" xmlns:="y"/>`,
	`<:a/>`,
	`<a:/>`,
	`<a:b:c/>`,
	`<a b:c:d="1"/>`,
	`<1a/>`,
	`<-a/>`,
	`<.a/>`,
	`<_a/>`,
	`<é/>`,
	`<aé/>`,
	"<a\u0300/>",
	"<\u0300a/>",
	"<a\xff/>",
	`< a/>`,
	`a < b`,
	`a <= b`,
	`<a b="1" c='2' d=3 e f=g:h-i_j>t</a>`,
	`<a b=3%/>`,
	`<a b= "x" />`,
	`<a b =x/>`,
	`<a b=/>`,
	`<a b=>t</a>`,
	`<a b="x<y"/>`,
	`<a b="x>y"/>`,
	`<a b="x]]>y"/>`,
	`<a>x]]>y</a>`,
	`<a>x]]y</a>`,
	`<a>x]>y</a>`,
	`<a>]]&gt;</a>`,
	`<a b="unterminated/>`,
	`<a b='unterminated`,
	`<a b`,
	`<a b=`,
	`<a b=x`,
	`<a`,
	`<`,
	`<a>`,
	`</a`,
	`</a >`,
	`<a></a >`,
	"<a></a\n>",
	`<a></a x>`,
	`<a></a/>`,
	`<a>&lt;&gt;&amp;&apos;&quot;</a>`,
	`<a>&nbsp;&pound;&euro;&copy;&hellip;&rsquo;&eacute;&Eacute;</a>`,
	`<a>&#160;&#xA0;&#xa0;&#x41;&#X41;&#065;&#0065;</a>`,
	`<a>&#;&#x;&#xg;&#12a;&#12 ;</a>`,
	`<a>&foo;&1;&;&amp &a b&lt</a>`,
	`<a>&nbsp</a>`,
	`<a>&#0;</a>`,
	`<a>&#1;</a>`,
	`<a>&#9;&#10;&#13;</a>`,
	`<a>&#x1F;</a>`,
	`<a>&#x20;</a>`,
	`<a>&#xD7FF;&#xE000;</a>`,
	`<a>&#xD800;</a>`,
	`<a>&#xDFFF;</a>`,
	`<a>&#xFFFD;</a>`,
	`<a>&#xFFFE;</a>`,
	`<a>&#xFFFF;</a>`,
	`<a>&#x10000;&#x10FFFF;</a>`,
	`<a>&#x110000;</a>`,
	`<a>&#99999999999999999999999;</a>`,
	`<a>&#xFFFFFFFFFFFFFFFFFF;</a>`,
	`<a b="&#0;"/>`,
	`<a b="&#xFFFF;"/>`,
	`<a b="&lt;&amp;&#160;&foo;&"/>`,
	`<a b="&"/>`,
	`<a b="&amp"/>`,
	"<a>&\xc3\xa9;</a>",
	"<a>&\xff;</a>",
	"<a>a\rb\r\nc\n\rd\r\r\ne</a>",
	"<a>\r&amp;\n</a>",
	"<a>&amp\r\n;</a>",
	"<a b=\"x\ry\r\nz\"/>",
	"<a b=\"&#13;&#10;\"/>",
	"<a>\x00</a>",
	"<a>\x01</a>",
	"<a>\x7f\u0085</a>",
	"<a>\xff</a>",
	"<a>\xed\xa0\x80</a>",
	"<a>\xef\xbf\xbe</a>",
	"<a>\xef\xbf\xbd</a>",
	"<a b=\"\x00\"/>",
	"<a b=\x00/>",
	"<a\x00/>",
	"<a>\xc3</a>",
	"<a>t</a>\xc3",
	"<!-- \x00 \xff --><a/>",
	"<?pi \x00 \xff ?><a/>",
	"<!DOCTYPE \x00 \xff><a/>",
	`<![CDATA[x<y&amp;]]>`,
	"<a><![CDATA[x<y&amp;\r\n]]z]]></a>",
	`<a><![CDATA[]]></a>`,
	`<a><![CDATA[x</a>`,
	`<a><![CDATA</a>`,
	`<a><![cdata[x]]></a>`,
	"<a><![CDATA[\x00]]></a>",
	`<a><!----></a>`,
	`<a><!-- x --></a>`,
	`<a><!-- a -- b --></a>`,
	`<a><!-- x ---></a>`,
	`<a><!---></a>`,
	`<a><!--></a>`,
	`<a><!-x--></a>`,
	`<a><!--`,
	`<a><!-`,
	`<a><!`,
	`<?xml version="1.0"?><a/>`,
	`<?xml version="1.0" encoding="UTF-8"?><a/>`,
	`<?xml version="1.0" encoding="utf-8"?><a/>`,
	`<?xml version='1.0' encoding='ASCII'?><a>&#160;</a>`,
	`<?xml version="1.0" encoding="us-ascii"?><a/>`,
	`<?xml version="1.0" encoding=" ASCII "?><a/>`,
	`<?xml version="1.0" encoding="ISO-8859-1"?><a/>`,
	`<?xml version="1.0" encoding="windows-1252"?><a/>`,
	`<?xml version = "1.0" encoding = "latin1"?><a/>`,
	`<?xml version="1.1"?><a/>`,
	`<?xml version="1.0" encoding=""?><a/>`,
	`<?xml?><a/>`,
	`<?xml-stylesheet type="text/xsl" href="x.xsl"?><a/>`,
	`<a/><?xml version="1.0" encoding="latin1"?>`,
	`<? xml?><a/>`,
	`<?1?><a/>`,
	`<?xml`,
	`<?xml version="1.0"`,
	`<?x?>`,
	`<?x ??>`,
	`<!DOCTYPE html><html/>`,
	`<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd"><html/>`,
	`<!DOCTYPE x [<!ENTITY a "b>"> <!-- c > --> <!ELEMENT x ANY>]><x/>`,
	`<!DOCTYPE x [<!ENTITY a 'b'> <x <y> >]><x/>`,
	`<!DOCTYPE x <!a> <!-x> <!--y-z--> >`,
	`<!DOCTYPE x "unterminated>`,
	`<!>text>`,
	`<!x`,
	`<!DOCTYPE x [<!--`,
	`<!DOCTYPE x [<!-- -- > -->]><a/>`,
	"\xef\xbb\xbf<a/>",
	"junk<a/>",
	`<a contextRef="c" xsi:nil="true" b:nil="x"/>`,
	`<ix:nonNumeric xmlns:ix="http://www.xbrl.org/2013/inlineXBRL" name="p:X" contextRef="c"><b>bold</b> &amp; <ix:exclude>hidden</ix:exclude></ix:nonNumeric>`,
	`<table><colgroup><col> </col><col> </col></colgroup></table>`,
	`<p>a<br>b</p>`,
}

func TestParityHandCases(t *testing.T) {
	for i, c := range handCases {
		if msg := parity([]byte(c)); msg != "" {
			t.Errorf("case %d %q:\n%s", i, c, msg)
		}
	}
}

// seedFiles is every instance under samples/ (zip packages contribute their
// instance members) and every fixture in internal/ixbrl/testdata.
func seedFiles(tb testing.TB) map[string][]byte {
	tb.Helper()
	out := map[string][]byte{}
	for _, dir := range []string{filepath.Join("..", "..", "..", "samples"), filepath.Join("..", "testdata")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			tb.Fatal(err)
		}
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			path := filepath.Join(dir, ent.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				tb.Fatal(err)
			}
			if !strings.EqualFold(filepath.Ext(path), ".zip") || !bytes.HasPrefix(data, []byte("PK")) {
				if isInstanceName(path) || filepath.Dir(path) != filepath.Join("..", "..", "..", "samples") {
					out[path] = data
				}
				continue
			}
			zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				tb.Fatalf("%s: %v", path, err)
			}
			for _, f := range zr.File {
				if f.FileInfo().IsDir() || !isInstanceName(f.Name) {
					continue
				}
				b, err := readZipFile(f)
				if err != nil {
					tb.Fatalf("%s!%s: %v", path, f.Name, err)
				}
				out[path+"!"+f.Name] = b
			}
		}
	}
	return out
}

func isInstanceName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xhtml", ".html", ".htm", ".xbrl", ".xml", ".zip":
		return true
	}
	return false
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func TestParitySeedFiles(t *testing.T) {
	files := seedFiles(t)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := files[name]
		for _, variant := range [][]byte{data, stripXMLPreamble(data)} {
			if msg := parity(variant); msg != "" {
				t.Errorf("%s:\n%s", name, msg)
			}
		}
	}
	if len(names) < 30 {
		t.Fatalf("only %d seed files", len(names))
	}
}

func FuzzParity(f *testing.F) {
	for _, c := range handCases {
		f.Add([]byte(c))
	}
	for _, data := range seedFiles(f) {
		// Large reports slow every fuzz iteration; their markup is covered
		// by TestParitySeedFiles.
		if len(data) <= 256<<10 {
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if msg := parity(data); msg != "" {
			t.Fatalf("%q:\n%s", data, msg)
		}
	})
}

// TestParityArchives compares the scanner with the oracle on every instance
// member of local Companies House zips. It is skipped unless
// CH_XBRL_PARITY_ZIPS lists zip paths or globs, separated by the OS path
// list separator. Members are stripped as the parser strips them.
//
//	CH_XBRL_PARITY_ZIPS=/data/Accounts_Monthly_Data-May2017.zip go test ./internal/ixbrl/xmlscan -run TestParityArchives -v
func TestParityArchives(t *testing.T) {
	spec := os.Getenv("CH_XBRL_PARITY_ZIPS")
	if spec == "" {
		t.Skip("CH_XBRL_PARITY_ZIPS not set")
	}
	var paths []string
	for _, p := range filepath.SplitList(spec) {
		m, err := filepath.Glob(p)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, m...)
	}
	if len(paths) == 0 {
		t.Fatalf("no zips match %q", spec)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) { archiveParity(t, path) })
	}
}

func archiveParity(t *testing.T, path string) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	type member struct {
		name string
		data []byte
	}
	jobs := make(chan *zip.File, 64)
	var members, rejected, mismatches atomic.Int64
	var mu sync.Mutex
	var report []string
	check := func(m member) {
		members.Add(1)
		data := stripXMLPreamble(m.data)
		if _, err := scanEvents(data); err != nil {
			rejected.Add(1)
		}
		if msg := parity(data); msg != "" {
			mismatches.Add(1)
			mu.Lock()
			if len(report) < 20 {
				report = append(report, m.name+": "+msg)
			}
			mu.Unlock()
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				data, err := readZipFile(f)
				if err != nil {
					t.Errorf("%s: %v", f.Name, err)
					continue
				}
				if !strings.EqualFold(filepath.Ext(f.Name), ".zip") || !bytes.HasPrefix(data, []byte("PK")) {
					check(member{f.Name, data})
					continue
				}
				inner, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
				if err != nil {
					continue
				}
				for _, g := range inner.File {
					if g.FileInfo().IsDir() || !isInstanceName(g.Name) || strings.EqualFold(filepath.Ext(g.Name), ".zip") {
						continue
					}
					b, err := readZipFile(g)
					if err == nil {
						check(member{f.Name + "!" + g.Name, b})
					}
				}
			}
		}()
	}
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() && isInstanceName(f.Name) {
			jobs <- f
		}
	}
	close(jobs)
	wg.Wait()
	t.Logf("members=%d rejected=%d mismatches=%d", members.Load(), rejected.Load(), mismatches.Load())
	for _, r := range report {
		t.Error(r)
	}
}
