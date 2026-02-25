package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ldiadam/station-satcom-invoice-parser/pkg/stationinvoice"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <invoice.pdf>\n", os.Args[0])
		os.Exit(2)
	}

	inv, err := stationinvoice.ParsePDF(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(inv)
}
