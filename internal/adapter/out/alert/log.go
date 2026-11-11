// Package alert is the output adapter of the pending-closure alert. Until the notification piece exists it only writes a line.
package alert

import (
	"context"
	"log/slog"

	"github.com/code-corhuila/multi-tour-worker-cash-close/internal/application/port/out"
)

// Log is the double of the alert: tenant and date, never a name, an e-mail or an amount.
type Log struct{ log *slog.Logger }

var _ out.Alerts = (*Log)(nil)

// NewLog returns the double over a logger.
func NewLog(log *slog.Logger) *Log { return &Log{log: log} }

// PendingClosure writes that the box of the day was opened and not closed, so a person has to close it.
func (l *Log) PendingClosure(ctx context.Context, tenantID, businessDate string) error {
	l.log.WarnContext(ctx, "alert: cash pending closure", "tenantId", tenantID, "businessDate", businessDate)
	return nil
}
