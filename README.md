# station-satcom-invoice-parser

Parse Station Satcom PDF invoices into JSON using a deterministic pipeline:

- `pdftotext` (Poppler) to extract text
- Go regex/line parsing (no LLM)

## Install dependencies

On Ubuntu/Debian:

```bash
sudo apt-get update
sudo apt-get install -y poppler-utils
```

## Library usage

```go
import "github.com/ldiadam/station-satcom-invoice-parser/pkg/stationinvoice"

inv, err := stationinvoice.ParsePDF("/path/to/invoice.pdf")
```

## CLI usage

```bash
go run ./cmd/station-satcom-invoice-parser /path/to/invoice.pdf
```
