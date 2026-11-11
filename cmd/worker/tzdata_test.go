package main

import (
	"go/parser"
	"go/token"
	"testing"
	"time"
)

// The job decides whether a day ended in the zone of its tenant, so it needs the zone database. The runtime image is plain
// Alpine with no tzdata package: without the embedded database every day is judged "the time zone is not valid" and nothing
// is closed. Whether a host happens to have zone files cannot be told from a test, so the test checks the import itself.
func TestTheBinaryEmbedsTheZoneDatabase(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imported := range file.Imports {
		if imported.Path.Value == `"time/tzdata"` {
			return
		}
	}
	t.Fatal(`main.go must import _ "time/tzdata": the runtime image has no zoneinfo`)
}

// The zone the cash gives to every tenant today must load (it would fail in the image without the embedded database).
func TestTheZoneOfAColombianTenantLoads(t *testing.T) {
	if _, err := time.LoadLocation("America/Bogota"); err != nil {
		t.Fatal(err)
	}
}
