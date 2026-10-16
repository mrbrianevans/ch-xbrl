package taxonomy

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// Source is one pinned taxonomy zip. SHA256 is the hex digest of the
// zip bytes. FRC /documents/<id>/ URLs are not a stable index; a hash
// mismatch fails the run instead of accepting a replaced file.
type Source struct {
	ID     string
	URL    string
	SHA256 string
}

// pinned lists the UK suites scanned for the concept-key check on
// 16 October 2026: FRC, Charities, Irish, SECR, UKSEF, and the XBRL UK
// 2009 all-in-one. URLs were taken from the FRC taxonomy pages that day,
// plus https://www.xbrl.org.uk/techguidance/UK-ALL-2009-09-01-dated-2010-12-31.zip.
var pinned = []Source{
	{ID: "frc-2014", URL: "https://www.frc.org.uk/documents/5849/FRC_Taxomomies_2014_Zip_File.zip", SHA256: "393aa166cc17ddb185d11cd32da19d5b7196fbae6b8e80bea6db2315d3769573"},
	{ID: "frc-2018", URL: "https://www.frc.org.uk/documents/3917/FRC_2018_Taxonomy_v1-0-0_December-2017_Zip_File.zip", SHA256: "7bc838f8337d55f8395f262dbceca419cf727298613c160e5176057f74ee9401"},
	{ID: "frc-2019", URL: "https://www.frc.org.uk/documents/5687/FRC_2019_Taxonomy_v1-1-0_Zip_File.zip", SHA256: "936fb7aad3aa3b16c38c88450448ef4ee9b377018e61574451697fad092074b4"},
	{ID: "irish-2019", URL: "https://www.frc.org.uk/documents/1480/Irish_Extension_Taxonomy_2019_Zip_file_45gkrS6.zip", SHA256: "9786b941fd9483c50771d83fb920046736d2aefdc0c164eb341f6c38ec1280ba"},
	{ID: "secr-2019", URL: "https://www.frc.org.uk/documents/558/SECR_Taxonomy_v1-0-0_Zip_File.zip", SHA256: "f14f71e42d6e6fa5984a74f685ee7bb9330d6154c06edb50cfc6a8715331d0bc"},
	{ID: "frc-2021", URL: "https://www.frc.org.uk/documents/3923/FRC_2021_Taxonomy_v1-0-0_Zip_File.zip", SHA256: "7b89ed25268af23ef6e7eeb0cda076f17061028e82e23f72552513a52419adf5"},
	{ID: "charities-2021", URL: "https://www.frc.org.uk/documents/629/Charities_Taxonomy_2021.zip", SHA256: "475bc222daebaf6f23524d7d703d9650449ea11a4350efb289b7985a3cbc79cf"},
	{ID: "secr-2021", URL: "https://www.frc.org.uk/documents/4595/SECR_Taxonomy_v1-1-0_Zip_File.zip", SHA256: "1a20f71845e5d0450f90b4012a1e5241cb69e3ad6b73f119f1dfe03cef39c54f"},
	{ID: "uksef-2021", URL: "https://www.frc.org.uk/documents/2918/UKSEF_2021_Release_of_the_taxonomy_v1-1-0_2021_zip-file.zip", SHA256: "b58a34592deea61cfddf060a754ba1ec01fa0222c366f6bbd1a976074bdae341"},
	{ID: "frc-2022", URL: "https://www.frc.org.uk/documents/969/FRC-2022-Taxonomy.zip", SHA256: "e1828a7a86cbad3fbdeb88026c5c6fafbf21fec4c60706a8782dcaefc05f7ebe"},
	{ID: "charities-2022", URL: "https://www.frc.org.uk/documents/3709/Charities-2022-TaxonomyZip_file.zip", SHA256: "863539ca27d07268a101b70512805aa78e5ac67f144aaab3643a7b71db242a94"},
	{ID: "irish-2022", URL: "https://www.frc.org.uk/documents/1051/Irish_Extension_Taxonomy_2022_Zip_file.zip", SHA256: "cdd9db8bb7b23beaf297d351b23e72e45f8245f5a17dcc033dc3ad60d34f4545"},
	{ID: "uksef-2022", URL: "https://www.frc.org.uk/documents/323/UKSEF-2022-Taxonomy-v2_0_0.zip", SHA256: "715211b3e475c6f7a0af2ad0a4c5fe4047c8705f679b1b9bb011c13670a25778"},
	{ID: "frc-2023", URL: "https://www.frc.org.uk/documents/372/The_2023_Taxonomy_suite_v1.0.1.zip", SHA256: "9557b1b056deef6be0dbf80dfbc754cd5745c6bfccb93abc5f62d5a1022399c1"},
	{ID: "charities-2023", URL: "https://www.frc.org.uk/documents/3421/Charities_2023_Taxonomies_2023.zip", SHA256: "be40d433ebd039915febcd8bcbbbd5c6ce0e80246b7601113f14ff318917fca5"},
	{ID: "irish-2023", URL: "https://www.frc.org.uk/documents/4176/Irish_Extension_Taxonomy_2023.zip", SHA256: "7f8df34f59efe02dec6480794a06f8dd39a9f72f3c85655851f3d5b4c71f4818"},
	{ID: "frc-2024", URL: "https://www.frc.org.uk/documents/6566/FRC-2024-Taxonomy-v1.0.0_GJp67Do.zip", SHA256: "af37339e7aa1d601c1096db097ce3d8b3f3d692c2e24b8037441c888e15038e8"},
	{ID: "charities-2024", URL: "https://www.frc.org.uk/documents/6567/Charities-2024-Taxonomy-v1.0.0_l5PHCwA.zip", SHA256: "614d785d79af7fde39199eef692140af877602f769978043607a235f74dce712"},
	{ID: "frc-2025", URL: "https://www.frc.org.uk/documents/7759/FRC-2025-Taxonomy-v1.0.0_LK4mek8.zip", SHA256: "b21f3c09a8a658aa7a0176c29c9df1cfac4a2360a4581d931bc72e2bed318b6d"},
	{ID: "charities-2025", URL: "https://www.frc.org.uk/documents/7760/Charities-2025-Taxonomy-v1.0.0.zip", SHA256: "c62d077496051e7766a7a8036a39120daf73c9d5a398241474c72553382ee279"},
	{ID: "irish-2025", URL: "https://www.frc.org.uk/documents/7761/Irish-Revenue-2025-Taxonomy-v1.0.0.zip", SHA256: "611712e837e027cdadb483f7e1b9753ac56a8cf067a3fb143a09fe6dcff5a69d"},
	{ID: "frc-2026", URL: "https://www.frc.org.uk/documents/8907/FRC-2026-Taxonomy-v1.0.0.zip", SHA256: "ae80ae12d9d747ac531150b0051bcd67e7c9acf44313da19105b4cc013566462"},
	{ID: "charities-2026", URL: "https://www.frc.org.uk/documents/8898/Charities-2026-Taxonomy-v1.0.0.zip", SHA256: "4949d1572484f63049873981c02e9e7f8cd56d3e31c11a303b321a9b32bd2695"},
	{ID: "irish-2026", URL: "https://www.frc.org.uk/documents/8899/Irish-Revenue-2026-Taxonomy-v1.0.0.zip", SHA256: "4f50f957546c2342772add230bd1520d2fa5c4723cee6f94dcc54916faa139bd"},
	{ID: "uk-all-2009", URL: "https://www.xbrl.org.uk/techguidance/UK-ALL-2009-09-01-dated-2010-12-31.zip", SHA256: "873eca5295555ccbc456ed53166b96adf5ab701f37da973e087f9bdaa42e3f0c"},
}

// Pinned returns a copy of the compiled source list.
func Pinned() []Source {
	out := make([]Source, len(pinned))
	copy(out, pinned)
	return out
}

// ParseSources reads a CSV with header id,url,sha256. It replaces the
// pinned list when -sources is set.
func ParseSources(r io.Reader) ([]Source, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = 3
	cr.Comment = '#'
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("sources CSV is empty")
	}
	if strings.ToLower(rows[0][0]) != "id" || strings.ToLower(rows[0][1]) != "url" || strings.ToLower(rows[0][2]) != "sha256" {
		return nil, fmt.Errorf("sources CSV header must be id,url,sha256")
	}
	var out []Source
	seen := map[string]bool{}
	for i, row := range rows[1:] {
		src := Source{ID: strings.TrimSpace(row[0]), URL: strings.TrimSpace(row[1]), SHA256: strings.TrimSpace(row[2])}
		if err := validateID(src.ID); err != nil {
			return nil, fmt.Errorf("sources row %d: %w", i+2, err)
		}
		if seen[src.ID] {
			return nil, fmt.Errorf("sources row %d: duplicate id %s", i+2, src.ID)
		}
		seen[src.ID] = true
		if _, err := normalizeSHA(src.SHA256); err != nil {
			return nil, fmt.Errorf("sources row %d: %w", i+2, err)
		}
		if src.URL == "" {
			return nil, fmt.Errorf("sources row %d: empty url", i+2)
		}
		out = append(out, src)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sources CSV has no rows")
	}
	return out, nil
}
