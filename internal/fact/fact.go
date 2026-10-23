// Package fact defines the long-format fact row emitted by ch-xbrl.
package fact

// Fact is one XBRL fact in long format.
// There is no company-number field. Consumers derive that from SourceFile.
type Fact struct {
	PeriodStart string // ISO date; for an instant, equals period_end
	PeriodEnd   string // ISO date (instant or duration end)
	Concept     string // local name, not a QName
	Value       string // raw/effective value as string
	Unit        string // unit measure or unitRef
	Dimensions  string // JSON object dimension→member; empty if none
	Namespace   string // concept prefix resolved to a namespace URI; empty if the inline name has no prefix
	SourceFile  string // archive member basename, or the package name
	Decimals    string // raw iXBRL decimals attribute (INF stays INF); empty if absent / non-numeric
}

// CSVHeader is the column order for long-format fact CSV.
var CSVHeader = []string{
	"period_start",
	"period_end",
	"concept",
	"value",
	"unit",
	"dimensions",
	"namespace",
	"source_file",
	"decimals",
}

// Record returns fields in CSVHeader order.
func (f Fact) Record() []string {
	return []string{
		f.PeriodStart,
		f.PeriodEnd,
		f.Concept,
		f.Value,
		f.Unit,
		f.Dimensions,
		f.Namespace,
		f.SourceFile,
		f.Decimals,
	}
}
