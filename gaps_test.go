package cashclose_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	runtimeconfig "github.com/code-corhuila/multi-tour-worker-runtime/config"
	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"
	"github.com/code-corhuila/multi-tour-worker-runtime/logging"
	"github.com/code-corhuila/multi-tour-worker-runtime/runner"

	alertlog "github.com/code-corhuila/multi-tour-worker-cash-close/internal/adapter/out/alert"
	cashhttp "github.com/code-corhuila/multi-tour-worker-cash-close/internal/adapter/out/http"
	"github.com/code-corhuila/multi-tour-worker-cash-close/internal/application/usecase"
)

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	token   = "service-token-value"
)

// 2026-11-13 12:00 UTC is 07:00 of the 13th in Bogota: the 12th has ended, the 13th has not.
var noon = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func day(date, origin string, opened bool, zone string) string {
	o := "false"
	if opened {
		o = "true"
	}
	return `{"tenantId":"` + tenantA + `","businessDate":"` + date + `","timeZone":"` + zone + `","origin":"` + origin +
		`","hasOperationalOpening":` + o + `,"pendingClosure":` + o + `}`
}

// cash is cash-reporting as the worker meets it over HTTP: the list of the open days and the status it answers to each auto-close.
type cash struct {
	mu      sync.Mutex
	items   []string
	status  map[string]int // business date -> status of its auto-close
	actions []string
}

func (c *cash) handler(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		_, _ = w.Write([]byte(`{"data":[` + strings.Join(c.items, ",") + `],"meta":{"page":1,"limit":100,"total":` +
			string(rune('0'+len(c.items))) + `,"totalPages":1}}`))
		return
	}
	c.actions = append(c.actions, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/api/v1/cash-reporting/internal"))
	for date, status := range c.status {
		if strings.Contains(r.URL.Path, date) && status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"NOT_CLOSABLE","message":"x","traceId":"t-1"}`))
			return
		}
	}
	_, _ = w.Write([]byte(`{}`))
}

func (c *cash) runner(t *testing.T, logs io.Writer, ticks <-chan time.Time) *runner.Runner {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(c.handler))
	t.Cleanup(api.Close)
	log := logging.New(logs)
	client := httpclient.New(api.URL, runtimeconfig.Secret(token), httpclient.WithAttempts(1))
	job := usecase.NewCloseDays(cashhttp.NewCashDays(client), alertlog.NewLog(log), log, func() time.Time { return noon })
	r, err := runner.New(job, runner.Options{Interval: 15 * time.Minute, BatchSize: 100, RunTimeout: 10 * time.Second, Logger: log, Ticks: ticks})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// AC-WK-08.2: an automatic box of an ended day is closed by the worker through the real HTTP route of the cash, once, with no body.
func TestHU_WRK_008_AC2_an_automatic_box_of_an_ended_day_is_closed_over_http(t *testing.T) {
	c := &cash{items: []string{day("2026-11-12", "AUTOMATIC_CREATION", false, "America/Bogota")}}

	res := c.runner(t, io.Discard, nil).RunOnce(t.Context())

	if len(c.actions) != 1 || c.actions[0] != "POST /tenants/"+tenantA+"/cash-days/2026-11-12/auto-close" || res.Processed != 1 || res.Failed != 0 {
		t.Fatalf("actions %v processed %d failed %d, want the auto-close of the 12th", c.actions, res.Processed, res.Failed)
	}
}

// AC-WK-08.2 "with no movements": that condition is the cash's (the worker only sees origin and opening). An automatic box that has movements is
// refused by the cash with a 422; the worker counts it as no failure, asks once and leaves the day for the next list.
func TestHU_WRK_008_AC2_an_automatic_box_with_movements_is_refused_by_the_cash_and_is_no_failure(t *testing.T) {
	c := &cash{items: []string{day("2026-11-12", "AUTOMATIC_CREATION", false, "America/Bogota")}, status: map[string]int{"2026-11-12": http.StatusUnprocessableEntity}}

	res := c.runner(t, io.Discard, nil).RunOnce(t.Context())

	if len(c.actions) != 1 || res.Failed != 0 {
		t.Errorf("actions %v failed %d, want one refused call and no failure", c.actions, res.Failed)
	}
}

// AC-WK-08.3 (story AC2, decision G-J8-3): the "pending closure" mark of a box that was opened and not closed is derived by the cash and arrives in
// the list (pendingClosure); the worker never closes that box: it calls no write route, raises one alert per tenant and date however many runs
// pass, and logs it with the tenant and the date.
func TestHU_WRK_008_AC3_an_opened_box_is_never_closed_and_is_alerted_once(t *testing.T) {
	c := &cash{items: []string{day("2026-11-12", "OPERATIONAL_OPENING", true, "America/Bogota")}}
	var logs bytes.Buffer
	r := c.runner(t, &logs, nil)

	first := r.RunOnce(t.Context())
	second := r.RunOnce(t.Context())

	if len(c.actions) != 0 {
		t.Errorf("the worker called %v: an opened box is never closed by a machine", c.actions)
	}
	if n := strings.Count(logs.String(), "alert: cash pending closure"); n != 1 {
		t.Errorf("alerts = %d over two runs, want 1", n)
	}
	if !strings.Contains(logs.String(), tenantA) || !strings.Contains(logs.String(), "2026-11-12") {
		t.Errorf("the alert log does not carry the tenant and the date: %s", logs.String())
	}
	if first.Failed != 0 || second.Failed != 0 {
		t.Errorf("failed %d then %d, want 0", first.Failed, second.Failed)
	}
}

// AC-WK-08.4: the day that has not ended in the zone of the tenant is untouched even if it ended in UTC; a tenant in Auckland, ahead of UTC,
// has its 13th already ended and is closed.
func TestHU_WRK_008_AC4_the_day_is_judged_in_the_zone_of_its_tenant(t *testing.T) {
	c := &cash{items: []string{
		day("2026-11-13", "AUTOMATIC_CREATION", false, "America/Bogota"),
		day("2026-11-13", "AUTOMATIC_CREATION", false, "Pacific/Auckland"),
	}}

	res := c.runner(t, io.Discard, nil).RunOnce(t.Context())

	if len(c.actions) != 1 || res.Processed != 1 || res.Failed != 0 {
		t.Errorf("actions %v processed %d failed %d, want only the day that ended in its zone", c.actions, res.Processed, res.Failed)
	}
}

// AC-WK-08.7: the log lines carry tenant and date and never the credential.
func TestHU_WRK_008_AC7_log_lines_carry_the_ids_and_never_the_token(t *testing.T) {
	c := &cash{items: []string{
		day("2026-11-12", "AUTOMATIC_CREATION", false, "America/Bogota"),
		day("2026-11-11", "OPERATIONAL_OPENING", true, "America/Bogota"),
	}}
	var logs bytes.Buffer

	c.runner(t, &logs, nil).RunOnce(t.Context())

	out := logs.String()
	if strings.Contains(out, token) || strings.Contains(strings.ToLower(out), "bearer") {
		t.Errorf("the log carries the credential: %s", out)
	}
	for _, want := range []string{tenantA, "2026-11-12", "2026-11-11"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log does not carry %q: %s", want, out)
		}
	}
}

// AC-WK-08.1 (every 15 minutes): each tick of the runner is one run that lists the open days and acts.
func TestHU_WRK_008_AC1_each_interval_is_one_run(t *testing.T) {
	c := &cash{items: []string{day("2026-11-12", "AUTOMATIC_CREATION", false, "America/Bogota")}}
	ticks := make(chan time.Time)
	r := c.runner(t, io.Discard, ticks)
	done := make(chan struct{})
	ctx, stop := context.WithCancel(t.Context())
	go func() { r.Start(ctx); close(done) }()

	for i := 1; i <= 3; i++ {
		ok := false
		for attempt := 0; attempt < 5 && !ok; attempt++ {
			ticks <- time.Now()
			until := time.Now().Add(time.Second)
			for time.Now().Before(until) && !ok {
				c.mu.Lock()
				ok = len(c.actions) >= i
				c.mu.Unlock()
				time.Sleep(5 * time.Millisecond)
			}
		}
		if !ok {
			t.Fatalf("interval %d did not run the job", i)
		}
	}
	stop()
	<-done
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.actions) != 3 {
		t.Errorf("closes asked = %d, want one per interval (3)", len(c.actions))
	}
}
