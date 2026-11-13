package usecase_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-runtime/correlation"
	"github.com/code-corhuila/multi-tour-worker-runtime/logging"

	"github.com/code-corhuila/multi-tour-worker-cash-close/internal/application/port/out"
	"github.com/code-corhuila/multi-tour-worker-cash-close/internal/application/usecase"
)

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	tenantB = "22222222-2222-4222-8222-222222222222"
	bogota  = "America/Bogota"
)

// fakeCash serves scripted pages and remembers the pages asked and the days closed.
type fakeCash struct {
	pages     [][]out.OpenDay
	asked     []int
	limits    []int
	closed    []string
	closeErr  map[string]error
	listError error
}

func (f *fakeCash) ListOpen(_ context.Context, page, limit int) (out.Page, error) {
	f.asked, f.limits = append(f.asked, page), append(f.limits, limit)
	if f.listError != nil {
		return out.Page{}, f.listError
	}
	if page > len(f.pages) {
		return out.Page{TotalPages: len(f.pages)}, nil
	}
	return out.Page{Days: f.pages[page-1], TotalPages: len(f.pages)}, nil
}

func (f *fakeCash) AutoClose(_ context.Context, tenantID, date string) error {
	key := tenantID + "/" + date
	f.closed = append(f.closed, key)
	return f.closeErr[key]
}

type fakeAlerts struct{ raised []string }

func (f *fakeAlerts) PendingClosure(_ context.Context, tenantID, date string) error {
	f.raised = append(f.raised, tenantID+"/"+date)
	return nil
}

// 2026-11-10 02:00 UTC is 2026-11-09 21:00 in Bogota: the 9th has not ended there, the 8th has.
var now = time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC)

func automatic(tenant, date string) out.OpenDay {
	return out.OpenDay{TenantID: tenant, BusinessDate: date, TimeZone: bogota, Origin: out.OriginAutomaticCreation}
}

func opened(tenant, date string) out.OpenDay {
	return out.OpenDay{TenantID: tenant, BusinessDate: date, TimeZone: bogota, Origin: "OPERATIONAL_OPENING", HasOperationalOpening: true}
}

func newJob(cash *fakeCash, alerts *fakeAlerts, log *bytes.Buffer) *usecase.CloseDays {
	return usecase.NewCloseDays(cash, alerts, logging.New(log), func() time.Time { return now })
}

func ctx() context.Context { return correlation.WithID(context.Background(), "run-1234") }

// AC-WK-08.1: the run lists the open days with the batch as the page size, and reads the next pages while there are more.
func TestA_lists_open_days_every_run(t *testing.T) {
	cash := &fakeCash{pages: [][]out.OpenDay{{automatic(tenantA, "2026-11-01")}, {automatic(tenantB, "2026-11-01")}}}

	processed, failed := newJob(cash, &fakeAlerts{}, &bytes.Buffer{}).Run(ctx(), 1)

	if len(cash.asked) != 2 || cash.asked[0] != 1 || cash.asked[1] != 2 || cash.limits[0] != 1 {
		t.Fatalf("asked pages %v with limits %v", cash.asked, cash.limits)
	}
	if processed != 2 || failed != 0 {
		t.Errorf("processed %d failed %d", processed, failed)
	}
}

// AC-WK-08.1: a batch above 100 is asked as pages of 100, the most the contract allows.
func TestA_a_big_batch_is_asked_in_pages_of_one_hundred(t *testing.T) {
	cash := &fakeCash{pages: [][]out.OpenDay{{}}}

	newJob(cash, &fakeAlerts{}, &bytes.Buffer{}).Run(ctx(), 500)

	if cash.limits[0] != 100 {
		t.Errorf("limit = %d, want 100", cash.limits[0])
	}
}

// AC-WK-08.2
func TestA_closes_an_automatic_empty_box_of_an_ended_day(t *testing.T) {
	cash := &fakeCash{pages: [][]out.OpenDay{{automatic(tenantA, "2026-11-08")}}}
	alerts := &fakeAlerts{}

	processed, failed := newJob(cash, alerts, &bytes.Buffer{}).Run(ctx(), 100)

	if len(cash.closed) != 1 || cash.closed[0] != tenantA+"/2026-11-08" || processed != 1 || failed != 0 {
		t.Fatalf("closed %v processed %d failed %d", cash.closed, processed, failed)
	}
	if len(alerts.raised) != 0 {
		t.Errorf("alerts = %v, want none", alerts.raised)
	}
}

// AC-WK-08.3: a box that was opened is never closed by the worker; the alert is raised instead.
func TestA_alerts_and_never_closes_a_box_with_operational_opening(t *testing.T) {
	cash := &fakeCash{pages: [][]out.OpenDay{{opened(tenantA, "2026-11-08")}}}
	alerts := &fakeAlerts{}

	newJob(cash, alerts, &bytes.Buffer{}).Run(ctx(), 100)

	if len(cash.closed) != 0 {
		t.Fatalf("the worker closed %v", cash.closed)
	}
	if len(alerts.raised) != 1 || alerts.raised[0] != tenantA+"/2026-11-08" {
		t.Errorf("alerts = %v", alerts.raised)
	}
}

// Plan D-06: the same pending box is alerted once, not every 15 minutes.
func TestA_the_same_pending_box_is_alerted_once(t *testing.T) {
	cash := &fakeCash{pages: [][]out.OpenDay{{opened(tenantA, "2026-11-08")}}}
	alerts := &fakeAlerts{}
	job := newJob(cash, alerts, &bytes.Buffer{})

	job.Run(ctx(), 100)
	job.Run(ctx(), 100)

	if len(alerts.raised) != 1 {
		t.Errorf("alerts = %v, want one", alerts.raised)
	}
}

// AC-WK-08.4: the day of the tenant, not the day in UTC, decides.
func TestA_leaves_a_day_that_has_not_ended_in_its_zone(t *testing.T) {
	cash := &fakeCash{pages: [][]out.OpenDay{{automatic(tenantA, "2026-11-09"), opened(tenantB, "2026-11-09")}}}
	alerts := &fakeAlerts{}

	newJob(cash, alerts, &bytes.Buffer{}).Run(ctx(), 100)

	if len(cash.closed) != 0 || len(alerts.raised) != 0 {
		t.Errorf("closed %v alerts %v, the 9th is still open in Bogota", cash.closed, alerts.raised)
	}
}

// AC-WK-08.5 and plan D-05: a day that the cash says it cannot close is skipped, not a failure.
func TestA_a_day_the_cash_cannot_close_is_a_quiet_skip(t *testing.T) {
	day := automatic(tenantA, "2026-11-08")
	cash := &fakeCash{pages: [][]out.OpenDay{{day}}, closeErr: map[string]error{tenantA + "/2026-11-08": out.ErrNotClosable}}

	_, failed := newJob(cash, &fakeAlerts{}, &bytes.Buffer{}).Run(ctx(), 100)

	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
}

// AC-WK-08.5: the same run twice. The cash answers 200 again for a day it already closed (the port returns no error), so the worker
// counts no failure, raises no alert and asks nothing else of the cash than that same close.
func TestA_a_day_already_closed_is_a_quiet_run(t *testing.T) {
	cash := &fakeCash{pages: [][]out.OpenDay{{automatic(tenantA, "2026-11-08")}}}
	alerts := &fakeAlerts{}
	job := newJob(cash, alerts, &bytes.Buffer{})

	job.Run(ctx(), 100)
	processed, failed := job.Run(ctx(), 100)

	if failed != 0 || processed != 1 {
		t.Errorf("second run: processed %d failed %d, want 1 and 0", processed, failed)
	}
	want := tenantA + "/2026-11-08"
	if len(cash.closed) != 2 || cash.closed[0] != want || cash.closed[1] != want {
		t.Errorf("closes asked = %v, want the same close twice and nothing else", cash.closed)
	}
	if len(alerts.raised) != 0 {
		t.Errorf("alerts = %v, want none", alerts.raised)
	}
}

// AC-WK-08.6: one failed day is counted and the others go on.
func TestA_a_failed_day_does_not_stop_the_others(t *testing.T) {
	cash := &fakeCash{
		pages:    [][]out.OpenDay{{automatic(tenantA, "2026-11-08"), automatic(tenantB, "2026-11-08")}},
		closeErr: map[string]error{tenantA + "/2026-11-08": errors.New("http 503")},
	}

	processed, failed := newJob(cash, &fakeAlerts{}, &bytes.Buffer{}).Run(ctx(), 100)

	if len(cash.closed) != 2 || processed != 1 || failed != 1 {
		t.Errorf("closed %v processed %d failed %d", cash.closed, processed, failed)
	}
}

// AC-WK-08.6: a failed list ends the run counted; the next run asks again.
func TestA_a_failed_list_ends_the_run_counted(t *testing.T) {
	cash := &fakeCash{listError: errors.New("http 503")}

	processed, failed := newJob(cash, &fakeAlerts{}, &bytes.Buffer{}).Run(ctx(), 100)

	if processed != 0 || failed != 1 {
		t.Errorf("processed %d failed %d", processed, failed)
	}
}

// AC-WK-08.8: a zone that is not valid is never guessed.
func TestA_a_bad_zone_is_skipped_and_counted(t *testing.T) {
	bad := automatic(tenantA, "2026-11-08")
	bad.TimeZone = "Mars/Olympus"
	cash := &fakeCash{pages: [][]out.OpenDay{{bad}}}

	_, failed := newJob(cash, &fakeAlerts{}, &bytes.Buffer{}).Run(ctx(), 100)

	if len(cash.closed) != 0 || failed != 1 {
		t.Errorf("closed %v failed %d", cash.closed, failed)
	}
}

// AC-WK-08.7: the lines carry the correlation id and the ids of tenant and date.
func TestA_log_lines_carry_ids(t *testing.T) {
	var log bytes.Buffer
	cash := &fakeCash{pages: [][]out.OpenDay{{opened(tenantA, "2026-11-08")}}}

	newJob(cash, &fakeAlerts{}, &log).Run(ctx(), 100)

	text := log.String()
	for _, want := range []string{"run-1234", tenantA, "2026-11-08"} {
		if !strings.Contains(text, want) {
			t.Errorf("the log does not carry %q: %s", want, text)
		}
	}
}
