// Package fact defines the long-format fact row emitted by ch-xbrl.
package fact

// Fact is one XBRL fact in long format.
type Fact struct {
	CompanyNumber string
	PeriodStart   string // ISO date; empty for pure instant if unknown
	PeriodEnd     string // ISO date (instant or duration end)
	Concept       string // local name (or full QName string)
	Value         string // raw/effective value as string
	Unit          string // unit measure or unitRef
	Dimensions    string // JSON object dimension→member; empty if none
	Taxonomy      string // schemaRef href
	SourceFile    string // archive member name
	Decimals      string // raw iXBRL decimals attribute (INF stays INF); empty if absent / non-numeric
}

// CSVHeader is the column order for long-format fact CSV.
var CSVHeader = []string{
	"company_number",
	"period_start",
	"period_end",
	"concept",
	"value",
	"unit",
	"dimensions",
	"taxonomy",
	"source_file",
	"decimals",
}

// Record returns fields in CSVHeader order.
func (f Fact) Record() []string {
	return []string{
		f.CompanyNumber,
		f.PeriodStart,
		f.PeriodEnd,
		f.Concept,
		f.Value,
		f.Unit,
		f.Dimensions,
		f.Taxonomy,
		f.SourceFile,
		f.Decimals,
	}
}
