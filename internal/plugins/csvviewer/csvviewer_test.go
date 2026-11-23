package csvviewer

import (
	"reflect"
	"testing"
)

func TestParseCSVAndAutoDelimiter(t *testing.T) {
	// Semicolon-delimited CSV with BOM
	raw := []byte("\xEF\xBB\xBFname;score\nAlice;98.5\nBob;87\n")
	td, err := ParseCSVBytes(".csv", raw)
	if err != nil {
		t.Fatalf("ParseCSVBytes failed: %v", err)
	}
	if td.Delimiter != ';' || td.NumCols != 2 || len(td.Rows) != 3 {
		t.Fatalf("unexpected parsed table: %+v", td)
	}
	if !reflect.DeepEqual(td.Rows[0], []string{"name", "score"}) {
		t.Fatalf("unexpected header row: %v", td.Rows[0])
	}

	// TSV
	td2, err := ParseCSVBytes(".tsv", []byte("a\tb\tc\n1\t2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if td2.Delimiter != '\t' || td2.NumCols != 3 || len(td2.Rows[1]) != 3 {
		t.Fatalf("unexpected TSV normalization: %+v", td2)
	}
}
