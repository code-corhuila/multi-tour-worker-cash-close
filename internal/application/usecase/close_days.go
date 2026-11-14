// Package usecase holds the jobs of the worker.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	workerruntime "github.com/code-corhuila/multi-tour-worker-runtime"

	"github.com/code-corhuila/multi-tour-worker-cash-close/internal/application/port/out"
)

// maxPageSize is the most the list of the cash contract gives in one page.
const maxPageSize = 100

// maxPages bounds a run: more pages wait for the next run (Annex D, bounded run).
const maxPages = 5

// CloseDays is job 8: per tenant, at the end of its local calendar day, it closes the boxes that the system created by itself and
// raises the pending-closure alert for the ones that were opened and not closed. It never closes a box that was opened.
type CloseDays struct {
	cash    out.CashDays
	alerts  out.Alerts
	log     *slog.Logger
	now     func() time.Time
	mu      sync.Mutex
	alerted map[string]bool
	cursor  int // first page of the next run; 0 means the first page, the list wraps at its end
}

var _ workerruntime.Job = (*CloseDays)(nil)

// NewCloseDays wires the job; now is injected so a test decides what time it is.
func NewCloseDays(cash out.CashDays, alerts out.Alerts, log *slog.Logger, now func() time.Time) *CloseDays {
	return &CloseDays{cash: cash, alerts: alerts, log: log, now: now, alerted: map[string]bool{}}
}

// Name is how the runner logs the job.
func (*CloseDays) Name() string { return "cash-close" }

// action is what the job decides for a day.
type action int

const (
	leave action = iota
	closeIt
	alert
)

// Run lists the open days in pages of the batch size, up to maxPages (pages first, then the work, so closing a day does not shift the pages), and acts on the ones
// whose day has ended in the zone of their tenant.
func (j *CloseDays) Run(ctx context.Context, batch int) (processed, failed int) {
	days, err := j.list(ctx, batch)
	if err != nil {
		j.log.WarnContext(ctx, "the open cash days could not be listed, the next run asks again", "error", err)
		return 0, 1
	}
	var work []todo
	for _, day := range days {
		what, err := j.decide(day)
		if err != nil {
			failed++
			j.log.WarnContext(ctx, "the day of a tenant could not be judged", "tenantId", day.TenantID, "businessDate", day.BusinessDate, "error", err)
			continue
		}
		if what != leave {
			work = append(work, todo{day, what})
		}
	}
	done, bad := workerruntime.Each(ctx, j.log, work, j.act)
	processed, failed = processed+done, failed+bad
	j.log.InfoContext(ctx, "cash days judged", "listed", len(days), "acted", done, "failed", failed)
	return processed, failed
}

type todo struct {
	day  out.OpenDay
	what action
}

// list reads up to maxPages pages from where the previous run stopped and wraps to the first page at the end of the list: a box that
// is only alerted never leaves the list, so a fixed first window would hide the automatic boxes that sit behind it.
func (j *CloseDays) list(ctx context.Context, batch int) ([]out.OpenDay, error) {
	limit := min(batch, maxPageSize)
	j.mu.Lock()
	page := max(j.cursor, 1)
	j.mu.Unlock()
	var days []out.OpenDay
	next := 1
	for read := 0; read < maxPages; read++ {
		got, err := j.cash.ListOpen(ctx, page, limit)
		if err != nil {
			return nil, err
		}
		days = append(days, got.Days...)
		if page >= got.TotalPages {
			break
		}
		page++
		next = page
	}
	j.mu.Lock()
	j.cursor = next
	j.mu.Unlock()
	return days, nil
}

// decide applies the rules of the spec: only a day that ended in the zone of its tenant is touched; a box with operational
// opening is alerted, never closed; an automatic box that was never used is closed.
func (j *CloseDays) decide(day out.OpenDay) (action, error) {
	zone, err := time.LoadLocation(day.TimeZone)
	if err != nil || day.TimeZone == "" {
		return leave, fmt.Errorf("the time zone is not valid")
	}
	local := j.now().In(zone).Format(time.DateOnly)
	if day.BusinessDate >= local {
		return leave, nil
	}
	switch {
	case day.HasOperationalOpening:
		return alert, nil
	case day.Origin == out.OriginAutomaticCreation:
		return closeIt, nil
	}
	return leave, nil
}

func (j *CloseDays) act(ctx context.Context, t todo) error {
	if t.what == alert {
		return j.raise(ctx, t.day)
	}
	err := j.cash.AutoClose(ctx, t.day.TenantID, t.day.BusinessDate)
	if errors.Is(err, out.ErrNotClosable) {
		j.log.InfoContext(ctx, "the cash did not close the day, left for the next list", "tenantId", t.day.TenantID, "businessDate", t.day.BusinessDate)
		return nil
	}
	if err != nil {
		return fmt.Errorf("close the day %s of %s: %w", t.day.BusinessDate, t.day.TenantID, err)
	}
	j.log.InfoContext(ctx, "cash day closed automatically", "tenantId", t.day.TenantID, "businessDate", t.day.BusinessDate)
	return nil
}

// raise asks the alert once per tenant, date and process; a restart may repeat it, so the receiver must be idempotent.
func (j *CloseDays) raise(ctx context.Context, day out.OpenDay) error {
	key := day.TenantID + "/" + day.BusinessDate
	j.mu.Lock()
	seen := j.alerted[key]
	j.mu.Unlock()
	if seen {
		return nil
	}
	if err := j.alerts.PendingClosure(ctx, day.TenantID, day.BusinessDate); err != nil {
		return fmt.Errorf("raise the alert of %s: %w", key, err)
	}
	j.mu.Lock()
	j.alerted[key] = true
	j.mu.Unlock()
	j.log.InfoContext(ctx, "cash day pending closure, alert raised", "tenantId", day.TenantID, "businessDate", day.BusinessDate)
	return nil
}
