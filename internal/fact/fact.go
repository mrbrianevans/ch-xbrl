// Package fact defines the long-format fact row emitted by the extractor.
package fact

// Fact is one XBRL fact in long format.
type Fact struct {
	CompanyID   string
	PeriodStart string // ISO date; empty for pure instant if unknown
	PeriodEnd   string // ISO date (instant or duration end)
	Concept     string // local name (or full QName string)
	Value       string // raw/effective value as string
	Unit        string // unit measure or unitRef
	Dimensions  string // JSON object dimension→member; empty if none
	Taxonomy    string // schemaRef href
	SourceFile  string // archive member name
}

// CSVHeader is the column order for long-format fact CSV.
var CSVHeader = []string{
	"company_id",
	"period_start",
	"period_end",
	"concept",
	"value",
	"unit",
	"dimensions",
	"taxonomy",
	"source_file",
}

// Record returns fields in CSVHeader order.
func (f Fact) Record() []string {
	return []string{
		f.CompanyID,
		f.PeriodStart,
		f.PeriodEnd,
		f.Concept,
		f.Value,
		f.Unit,
		f.Dimensions,
		f.Taxonomy,
		f.SourceFile,
	}
}
