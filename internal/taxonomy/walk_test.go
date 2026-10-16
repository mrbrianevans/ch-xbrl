package taxonomy

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const fixtureSchema = `<?xml version="1.0" encoding="UTF-8"?>
<schema xmlns="http://www.w3.org/2001/XMLSchema"
  xmlns:xbrli="http://www.xbrl.org/2003/instance"
  xmlns:xbrldt="http://xbrl.org/2005/xbrldt"
  xmlns:Q1="http://xbrl.org/2005/xbrldt"
  targetNamespace="http://example.com/2026">
  <element name="Turnover" type="xbrli:monetaryItemType" substitutionGroup="xbrli:item"
    nillable="true" xbrli:periodType="duration" xbrli:balance="credit"/>
  <element name="DirectorPay" substitutionGroup="xbrli:tuple" xbrli:periodType="duration"/>
  <element name="Hypercube" abstract="true" substitutionGroup="xbrldt:hypercubeItem" xbrli:periodType="duration"/>
  <element name="Dimension" abstract="true" substitutionGroup="xbrldt:dimensionItem" xbrli:periodType="duration"/>
  <element name="OtherHypercube" abstract="true" substitutionGroup="Q1:hypercubeItem"/>
  <element name="NoteHead" type="string"/>
  <complexType name="Note">
    <sequence>
      <element name="LocalNote" type="xbrli:stringItemType" substitutionGroup="xbrli:item"/>
    </sequence>
  </complexType>
</schema>
`

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tax.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScanZip_KeepsItemsAndTuples(t *testing.T) {
	path := writeZip(t, map[string]string{
		"a.xsd": fixtureSchema,
		"b.xsd": fixtureSchema,
	})
	got, err := ScanZip(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("concepts = %d, want 2 (identical duplicate collapsed)", len(got))
	}
	turnover := got[Concept{Namespace: "http://example.com/2026", LocalName: "Turnover"}.key()]
	if turnover.c.TypeNamespace != "http://www.xbrl.org/2003/instance" || turnover.c.TypeName != "monetaryItemType" {
		t.Fatalf("type = %s %s", turnover.c.TypeNamespace, turnover.c.TypeName)
	}
	if turnover.c.PeriodType != "duration" || turnover.c.Balance != "credit" || turnover.c.Nillable != "true" {
		t.Fatalf("attrs = %+v", turnover.c)
	}
	if turnover.c.SubstitutionGroupName != "item" {
		t.Fatalf("substitution group = %s", turnover.c.SubstitutionGroupName)
	}
	if _, ok := got[Concept{Namespace: "http://example.com/2026", LocalName: "DirectorPay"}.key()]; !ok {
		t.Fatal("tuple missing")
	}
	for _, name := range []string{"Hypercube", "Dimension", "OtherHypercube", "NoteHead", "LocalNote"} {
		if _, ok := got[Concept{Namespace: "http://example.com/2026", LocalName: name}.key()]; ok {
			t.Fatalf("kept %s", name)
		}
	}
}

func TestScanZip_Conflict(t *testing.T) {
	other := `<?xml version="1.0" encoding="UTF-8"?>
<schema xmlns="http://www.w3.org/2001/XMLSchema" xmlns:xbrli="http://www.xbrl.org/2003/instance" targetNamespace="http://example.com/2026">
  <element name="Turnover" type="xbrli:monetaryItemType" substitutionGroup="xbrli:item" xbrli:periodType="duration" xbrli:balance="debit"/>
</schema>`
	path := writeZip(t, map[string]string{"a.xsd": fixtureSchema, "b.xsd": other})
	_, err := ScanZip(path)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want ConflictError", err)
	}
	if conflict.Concept.LocalName != "Turnover" {
		t.Fatalf("concept = %s", conflict.Concept.LocalName)
	}
}

func TestParseSchema_UnboundPrefix(t *testing.T) {
	xsd := `<?xml version="1.0"?>
<schema xmlns="http://www.w3.org/2001/XMLSchema" targetNamespace="http://example.com/2026">
  <element name="Turnover" substitutionGroup="xbrli:item"/>
</schema>`
	_, err := parseSchema([]byte(xsd), "bad.xsd")
	if err == nil {
		t.Fatal("expected unbound prefix error")
	}
}

func TestWriteCSV_Sorted(t *testing.T) {
	concepts := []Concept{
		{Namespace: "http://b", LocalName: "A"},
		{Namespace: "http://a", LocalName: "B"},
		{Namespace: "http://a", LocalName: "A"},
	}
	// WriteCSV does not sort; callers pass sortedConcepts output.
	m := map[string]placed{}
	for _, c := range concepts {
		if err := addConcept(m, c, "t"); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, sortedConcepts(m)); err != nil {
		t.Fatal(err)
	}
	text := buf.String()
	want := "namespace,local_name,abstract,type_namespace,type_name,substitution_group_namespace,substitution_group_name,period_type,balance,nillable\n" +
		"http://a,A,,,,,,,,\n" +
		"http://a,B,,,,,,,,\n" +
		"http://b,A,,,,,,,,\n"
	if text != want {
		t.Fatalf("csv =\n%s\nwant\n%s", text, want)
	}
}
