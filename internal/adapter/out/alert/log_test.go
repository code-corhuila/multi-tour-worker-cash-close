package alert_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/code-corhuila/multi-tour-worker-runtime/logging"

	"github.com/code-corhuila/multi-tour-worker-cash-close/internal/adapter/out/alert"
)

// G-J8-3: until the notification piece exists the alert is a line with ids and nothing else.
func TestLogAlert_writes_the_ids_and_nothing_else(t *testing.T) {
	var out bytes.Buffer

	err := alert.NewLog(logging.New(&out)).PendingClosure(context.Background(), "tenant-1", "2026-11-08")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"tenant-1", "2026-11-08", "pending"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the line does not carry %q: %s", want, out.String())
		}
	}
}
