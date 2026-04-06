// Package cmdutil provides output formatting helpers for the plsctl CLI.
package cmdutil

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// PrintTable writes a formatted table to stdout using text/tabwriter.
// headers is the column header row; rows is the data.
func PrintTable(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	// Print header in uppercase.
	upperHeaders := make([]string, len(headers))
	for i, h := range headers {
		upperHeaders[i] = strings.ToUpper(h)
	}
	fmt.Fprintln(w, strings.Join(upperHeaders, "\t"))

	// Print separator line.
	seps := make([]string, len(headers))
	for i, h := range upperHeaders {
		seps[i] = strings.Repeat("-", len(h))
	}
	fmt.Fprintln(w, strings.Join(seps, "\t"))

	// Print data rows.
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
}

// PrintJSON pretty-prints v as indented JSON to stdout.
func PrintJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		PrintError(fmt.Sprintf("JSON encoding error: %v", err))
	}
}

// PrintSuccess prints a success message to stdout with a checkmark prefix.
func PrintSuccess(message string) {
	fmt.Printf("✓ %s\n", message)
}

// PrintError prints an error message to stderr with an "ERROR:" prefix.
func PrintError(message string) {
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", message)
}
