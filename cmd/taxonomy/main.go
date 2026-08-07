// Command taxonomy downloads / parses FRC (and related) taxonomy packages and
// emits reference CSVs: concepts.csv, and optionally labels.csv / calculations.csv.
//
//	taxonomy -out reference
//	taxonomy -entry https://xbrl.frc.org.uk/FRS-102/2021-01-01/FRS-102-2021-01-01.xsd -out reference
//	taxonomy -zip path/to/taxonomy.zip -out reference
//
// When no remote/local source succeeds, a curated seed concepts.csv is still written
// so the DuckDB pipeline can run offline against the sample extract.
package main

import (
	"encoding/csv"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Concept is one taxonomy concept definition.
type Concept struct {
	QName             string
	LocalName         string
	Namespace         string
	Balance           string
	PeriodType        string
	ItemType          string
	Abstract          string
	Nillable          string
	SubstitutionGroup string
}

func main() {
	outDir := flag.String("out", "reference", "output directory for CSVs")
	entry := flag.String("entry", "", "taxonomy entry-point XSD URL or path (repeatable via comma-sep)")
	zipPath := flag.String("zip", "", "local taxonomy package ZIP to scan for XSDs")
	seedOnly := flag.Bool("seed-only", false, "only write seed concepts (no download/parse)")
	timeout := flag.Duration("timeout", 60*time.Second, "HTTP timeout per request")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatal(err)
	}

	concepts := map[string]Concept{} // key = namespace|local

	// Always include seed concepts used by concept_map / samples.
	for _, c := range seedConcepts() {
		concepts[key(c)] = c
	}

	if !*seedOnly {
		entries := defaultEntries()
		if *entry != "" {
			entries = strings.Split(*entry, ",")
		}
		client := &http.Client{Timeout: *timeout}

		if *zipPath != "" {
			if err := loadFromZip(*zipPath, concepts); err != nil {
				log.Printf("zip: %v", err)
			}
		}

		for _, e := range entries {
			e = strings.TrimSpace(e)
			if e == "" {
				continue
			}
			log.Printf("loading entry: %s", e)
			if err := loadSchema(client, e, concepts, map[string]bool{}, 0); err != nil {
				log.Printf("  warn: %v", err)
			}
		}
	}

	list := make([]Concept, 0, len(concepts))
	for _, c := range concepts {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].LocalName == list[j].LocalName {
			return list[i].Namespace < list[j].Namespace
		}
		return list[i].LocalName < list[j].LocalName
	})

	path := filepath.Join(*outDir, "concepts.csv")
	if err := writeConcepts(path, list); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (%d concepts)", path, len(list))
}

func key(c Concept) string {
	return c.Namespace + "|" + c.LocalName
}

func defaultEntries() []string {
	return []string{
		"https://xbrl.frc.org.uk/FRS-102/2021-01-01/FRS-102-2021-01-01.xsd",
		"https://xbrl.frc.org.uk/FRS-102/2023-01-01/FRS-102-2023-01-01.xsd",
	}
}

// --- XSD parsing -------------------------------------------------------------

func loadSchema(client *http.Client, ref string, concepts map[string]Concept, seen map[string]bool, depth int) error {
	if depth > 40 {
		return fmt.Errorf("schema include depth exceeded at %s", ref)
	}
	if seen[ref] {
		return nil
	}
	seen[ref] = true

	data, base, err := fetch(client, ref)
	if err != nil {
		return err
	}

	// Use a token walk for flexibility (namespaces vary).
	return walkSchema(client, data, base, concepts, seen, depth)
}

func fetch(client *http.Client, ref string) (data []byte, base string, err error) {
	if strings.HasPrefix(ref, "zip://") {
		return nil, ref, fmt.Errorf("zip member fetch not supported for includes: %s", ref)
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		if client == nil {
			return nil, "", fmt.Errorf("no HTTP client for %s", ref)
		}
		resp, err := client.Get(ref)
		if err != nil {
			return nil, "", err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != 200 {
			return nil, "", fmt.Errorf("%s: HTTP %s", ref, resp.Status)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		return b, ref, err
	}
	b, err := os.ReadFile(ref)
	return b, ref, err
}

func walkSchema(client *http.Client, data []byte, base string, concepts map[string]Concept, seen map[string]bool, depth int) error {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.Strict = false

	targetNS := ""
	var imports []string

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		local := se.Name.Local
		switch local {
		case "schema":
			for _, a := range se.Attr {
				if a.Name.Local == "targetNamespace" {
					targetNS = a.Value
				}
			}
		case "import", "include", "redefine":
			loc := ""
			for _, a := range se.Attr {
				if a.Name.Local == "schemaLocation" {
					loc = a.Value
				}
			}
			if loc != "" {
				imports = append(imports, resolveRef(base, loc))
			}
		case "element":
			name := ""
			typ := ""
			abstract := ""
			nillable := ""
			subst := ""
			balance := ""
			periodType := ""
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "name":
					name = a.Value
				case "type":
					typ = a.Value
				case "abstract":
					abstract = a.Value
				case "nillable":
					nillable = a.Value
				case "substitutionGroup":
					subst = a.Value
				case "balance":
					balance = a.Value
				case "periodType":
					periodType = a.Value
				}
			}
			if name == "" {
				continue
			}
			// Only keep items that look like XBRL concepts (have periodType or item type / substitutionGroup)
			itemType := localPart(typ)
			if periodType == "" && balance == "" && !looksLikeItem(subst, typ) {
				// still keep if type ends with ItemType
				if !strings.Contains(strings.ToLower(typ), "itemtype") &&
					!strings.Contains(strings.ToLower(subst), "item") {
					continue
				}
			}
			c := Concept{
				QName:             clark(targetNS, name),
				LocalName:         name,
				Namespace:         targetNS,
				Balance:           balance,
				PeriodType:        periodType,
				ItemType:          itemType,
				Abstract:          abstract,
				Nillable:          nillable,
				SubstitutionGroup: localPart(subst),
			}
			concepts[key(c)] = c
		}
	}

	for _, imp := range imports {
		if err := loadSchema(client, imp, concepts, seen, depth+1); err != nil {
			log.Printf("  include %s: %v", imp, err)
		}
	}
	return nil
}

func looksLikeItem(subst, typ string) bool {
	s := strings.ToLower(subst + " " + typ)
	return strings.Contains(s, "item") || strings.Contains(s, "tuple")
}

func localPart(q string) string {
	if i := strings.Index(q, ":"); i >= 0 {
		return q[i+1:]
	}
	return q
}

func clark(ns, local string) string {
	if ns == "" {
		return local
	}
	return "{" + ns + "}" + local
}

func resolveRef(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		u, err := url.Parse(base)
		if err != nil {
			return ref
		}
		// resolve relative to base path directory
		dir := path.Dir(u.Path)
		if strings.HasPrefix(ref, "/") {
			u.Path = ref
		} else {
			u.Path = path.Join(dir, ref)
		}
		// path.Join cleans // which can break; rebuild
		return u.Scheme + "://" + u.Host + u.Path
	}
	// filesystem
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(filepath.Dir(base), filepath.FromSlash(ref))
}

func loadFromZip(zipPath string, concepts map[string]Concept) error {
	// Lightweight: shell out not available for zip easily without archive/zip
	return loadZipGo(zipPath, concepts)
}

func loadZipGo(zipPath string, concepts map[string]Concept) error {
	return scanZipFile(zipPath, concepts)
}

func writeConcepts(path string, list []Concept) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	_ = w.Write([]string{
		"concept_qname", "local_name", "namespace", "balance", "period_type", "item_type",
		"abstract", "nillable", "substitution_group",
	})
	for _, c := range list {
		_ = w.Write([]string{
			c.QName, c.LocalName, c.Namespace, c.Balance, c.PeriodType, c.ItemType,
			c.Abstract, c.Nillable, c.SubstitutionGroup,
		})
	}
	w.Flush()
	return w.Error()
}

// seedConcepts returns a minimal offline concept list covering concept_map.csv
// and common FRC core items present in the sample files.
func seedConcepts() []Concept {
	nsCore := "http://xbrl.frc.org.uk/fr/2021-01-01/core"
	nsBus := "http://xbrl.frc.org.uk/cd/2021-01-01/business"
	type row struct {
		local, balance, period, item, ns string
	}
	rows := []row{
		{"AverageNumberEmployeesDuringPeriod", "", "duration", "nonNegativeIntegerItemType", nsCore},
		{"EmployeesTotal", "", "duration", "nonNegativeIntegerItemType", nsCore},
		{"TurnoverRevenue", "credit", "duration", "monetaryItemType", nsCore},
		{"TurnoverGrossOperatingRevenue", "credit", "duration", "monetaryItemType", nsCore},
		{"FixedAssets", "debit", "instant", "monetaryItemType", nsCore},
		{"CurrentAssets", "debit", "instant", "monetaryItemType", nsCore},
		{"CashBankOnHand", "debit", "instant", "monetaryItemType", nsCore},
		{"CashBankInHand", "debit", "instant", "monetaryItemType", nsCore},
		{"NetAssetsLiabilities", "", "instant", "monetaryItemType", nsCore},
		{"NetCurrentAssetsLiabilities", "", "instant", "monetaryItemType", nsCore},
		{"TotalAssetsLessCurrentLiabilities", "", "instant", "monetaryItemType", nsCore},
		{"Equity", "credit", "instant", "monetaryItemType", nsCore},
		{"ProfitLoss", "credit", "duration", "monetaryItemType", nsCore},
		{"ProfitLossOnOrdinaryActivitiesBeforeTax", "credit", "duration", "monetaryItemType", nsCore},
		{"ProfitLossForPeriod", "credit", "duration", "monetaryItemType", nsCore},
		{"TaxTaxCreditOnProfitOrLossOnOrdinaryActivities", "debit", "duration", "monetaryItemType", nsCore},
		{"GrossProfitLoss", "credit", "duration", "monetaryItemType", nsCore},
		{"AdministrativeExpenses", "debit", "duration", "monetaryItemType", nsCore},
		{"OperatingProfitLoss", "credit", "duration", "monetaryItemType", nsCore},
		{"Debtors", "debit", "instant", "monetaryItemType", nsCore},
		{"Creditors", "credit", "instant", "monetaryItemType", nsCore},
		{"CreditorsDueWithinOneYear", "credit", "instant", "monetaryItemType", nsCore},
		{"CreditorsDueAfterOneYear", "credit", "instant", "monetaryItemType", nsCore},
		{"IntangibleAssets", "debit", "instant", "monetaryItemType", nsCore},
		{"PropertyPlantEquipment", "debit", "instant", "monetaryItemType", nsCore},
		{"InvestmentsFixedAssets", "debit", "instant", "monetaryItemType", nsCore},
		{"StocksInventory", "debit", "instant", "monetaryItemType", nsCore},
		{"CalledUpShareCapital", "credit", "instant", "monetaryItemType", nsCore},
		{"UKCompaniesHouseRegisteredNumber", "", "duration", "stringItemType", nsBus},
		{"EntityCurrentLegalOrRegisteredName", "", "duration", "stringItemType", nsBus},
		{"BalanceSheetDate", "", "duration", "dateItemType", nsBus},
		{"StartDateForPeriodCoveredByReport", "", "duration", "dateItemType", nsBus},
		{"EndDateForPeriodCoveredByReport", "", "duration", "dateItemType", nsBus},
	}
	out := make([]Concept, 0, len(rows))
	for _, r := range rows {
		out = append(out, Concept{
			QName:      clark(r.ns, r.local),
			LocalName:  r.local,
			Namespace:  r.ns,
			Balance:    r.balance,
			PeriodType: r.period,
			ItemType:   r.item,
		})
	}
	return out
}
