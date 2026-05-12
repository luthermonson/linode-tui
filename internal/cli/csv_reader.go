package cli

import (
	"encoding/csv"
	"strings"
)

// newCsvReader returns a csv.Reader for a string. Local helper to keep
// list.go tidy.
func newCsvReader(s string) *csv.Reader {
	r := csv.NewReader(strings.NewReader(s))
	r.FieldsPerRecord = -1
	return r
}
