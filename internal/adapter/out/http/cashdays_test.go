package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-runtime/config"
	"github.com/code-corhuila/multi-tour-worker-runtime/correlation"
	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"

	cashhttp "github.com/code-corhuila/multi-tour-worker-cash-close/internal/adapter/out/http"
	"github.com/code-corhuila/multi-tour-worker-cash-close/internal/application/port/out"
)

const (
	token  = "s3cret-service-token"
	tenant = "11111111-1111-4111-8111-111111111111"
)

// stubCash stands for the cash api: the routes of the contract (code-corhuila/multi-tour-docs#840) may not be on its develop yet.
type stubCash struct {
	statuses []int
	body     string
	calls    atomic.Int32
	method   string
	path     string
	query    string
	header   http.Header
}

func newStub(t *testing.T, body string, statuses ...int) (*httptest.Server, *stubCash) {
	t.Helper()
	f := &stubCash{statuses: statuses, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(f.calls.Add(1))
		f.method, f.path, f.query, f.header = r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone()
		status := http.StatusOK
		if n <= len(f.statuses) {
			status = f.statuses[n-1]
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(f.body))
			return
		}
		_, _ = w.Write([]byte(`{"error":"X","message":"m","traceId":"t"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, f
}

func newClient(url string) *httpclient.Client {
	return httpclient.New(url, config.Secret(token),
		httpclient.WithSleeper(func(context.Context, time.Duration) error { return nil }))
}

func ctx() context.Context { return correlation.WithID(context.Background(), "run-1234") }

const listBody = `{"data":[{"tenantId":"` + tenant + `","businessDate":"2026-11-08","timeZone":"America/Bogota",
"origin":"AUTOMATIC_CREATION","hasOperationalOpening":false,"pendingClosure":true}],"meta":{"page":2,"limit":50,"total":120,"totalPages":3}}`

// AC-WK-08.1
func TestI_list_sends_token_and_correlation(t *testing.T) {
	srv, stub := newStub(t, listBody)

	page, err := cashhttp.NewCashDays(newClient(srv.URL)).ListOpen(ctx(), 2, 50)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.method != http.MethodGet || stub.path != "/api/v1/cash-reporting/internal/cash-days/open" || stub.query != "page=2&limit=50" {
		t.Errorf("asked %s %s?%s", stub.method, stub.path, stub.query)
	}
	if stub.header.Get("Authorization") != "Bearer "+token || stub.header.Get("X-Correlation-Id") != "run-1234" {
		t.Errorf("headers = %v", stub.header)
	}
	want := out.OpenDay{TenantID: tenant, BusinessDate: "2026-11-08", TimeZone: "America/Bogota",
		Origin: out.OriginAutomaticCreation, PendingClosure: true}
	if len(page.Days) != 1 || page.Days[0] != want || page.TotalPages != 3 {
		t.Errorf("page = %+v", page)
	}
}

// AC-WK-08.2: the path carries tenant and date, and the key is the same for the same tenant and date.
func TestI_auto_close_path_and_key(t *testing.T) {
	srv, stub := newStub(t, `{}`)

	err := cashhttp.NewCashDays(newClient(srv.URL)).AutoClose(ctx(), tenant, "2026-11-08")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.method != http.MethodPost || stub.path != "/api/v1/cash-reporting/internal/tenants/"+tenant+"/cash-days/2026-11-08/auto-close" {
		t.Errorf("asked %s %s", stub.method, stub.path)
	}
	if got := stub.header.Get("Idempotency-Key"); got != "cash-close-"+tenant+"-2026-11-08" {
		t.Errorf("key = %q", got)
	}
}

// Plan D-05: the cash says the day cannot be closed (no cash, or not an automatic box, or not ended).
func TestI_404_and_422_mean_not_closable_and_are_not_retried(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusUnprocessableEntity} {
		srv, stub := newStub(t, `{}`, status)

		err := cashhttp.NewCashDays(newClient(srv.URL)).AutoClose(ctx(), tenant, "2026-11-08")

		if !errors.Is(err, out.ErrNotClosable) || stub.calls.Load() != 1 {
			t.Errorf("status %d: err %v calls %d", status, err, stub.calls.Load())
		}
	}
}

// AC-WK-08.6: 5xx is retried a bounded number of times; a 4xx is not.
func TestI_retries_5xx_and_not_4xx(t *testing.T) {
	srv, stub := newStub(t, `{}`, 503, 503, 200)
	if err := cashhttp.NewCashDays(newClient(srv.URL)).AutoClose(ctx(), tenant, "2026-11-08"); err != nil || stub.calls.Load() != 3 {
		t.Errorf("5xx: err %v calls %d", err, stub.calls.Load())
	}
	srv, stub = newStub(t, `{}`, 400)
	if err := cashhttp.NewCashDays(newClient(srv.URL)).AutoClose(ctx(), tenant, "2026-11-08"); err == nil || stub.calls.Load() != 1 {
		t.Errorf("400: err %v calls %d", err, stub.calls.Load())
	}
	srv, _ = newStub(t, `{}`, 503, 503, 503)
	if err := cashhttp.NewCashDays(newClient(srv.URL)).AutoClose(ctx(), tenant, "2026-11-08"); err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("exhausted retries: err %v", err)
	}
}

func TestI_list_refuses_a_page_below_one(t *testing.T) {
	if _, err := cashhttp.NewCashDays(newClient("http://x")).ListOpen(ctx(), 0, 10); err == nil {
		t.Error("page 0 was accepted")
	}
}

// AC-WK-08.6: a 429 is retried with the same key and then it works.
func TestI_retries_a_429_and_then_closes(t *testing.T) {
	srv, stub := newStub(t, `{}`, http.StatusTooManyRequests, http.StatusTooManyRequests, http.StatusOK)

	err := cashhttp.NewCashDays(newClient(srv.URL)).AutoClose(ctx(), tenant, "2026-11-08")

	if err != nil || stub.calls.Load() != 3 {
		t.Errorf("429: err %v calls %d, want success after 3 calls", err, stub.calls.Load())
	}
}

// AC-WK-08.6: a network error (the cash drops the connection) is retried a bounded number of times and then it is an error that does not leak the token.
func TestI_retries_a_network_error_and_then_fails_without_the_token(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(srv.Close)

	err := cashhttp.NewCashDays(newClient(srv.URL)).AutoClose(ctx(), tenant, "2026-11-08")

	if err == nil || errors.Is(err, out.ErrNotClosable) || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v, want a plain failure without the token", err)
	}
	if n := calls.Load(); n < 2 || n > 5 {
		t.Errorf("calls = %d, want the bounded retries of the runtime (2 to 5)", n)
	}
}
