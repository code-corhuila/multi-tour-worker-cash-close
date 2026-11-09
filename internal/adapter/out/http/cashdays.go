// Package http is the output adapter that calls cash-reporting on its internal routes.
package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"

	"github.com/code-corhuila/multi-tour-worker-cash-close/internal/application/port/out"
)

const (
	listPath  = "/api/v1/cash-reporting/internal/cash-days/open"
	closePath = "/api/v1/cash-reporting/internal/tenants/%s/cash-days/%s/auto-close"
)

// CashDays calls cash-reporting through the client of the runtime.
type CashDays struct {
	client *httpclient.Client
}

var _ out.CashDays = (*CashDays)(nil)

// NewCashDays returns the adapter over a client whose base URL is cash-reporting.
func NewCashDays(client *httpclient.Client) *CashDays { return &CashDays{client: client} }

type openDayItem struct {
	TenantID              string `json:"tenantId"`
	BusinessDate          string `json:"businessDate"`
	TimeZone              string `json:"timeZone"`
	Origin                string `json:"origin"`
	HasOperationalOpening bool   `json:"hasOperationalOpening"`
	PendingClosure        bool   `json:"pendingClosure"`
}

// ListOpen reads one page of GET /internal/cash-days/open.
func (c *CashDays) ListOpen(ctx context.Context, page, limit int) (out.Page, error) {
	if page < 1 || limit < 1 {
		return out.Page{}, fmt.Errorf("page and limit must be positive, got %d and %d", page, limit)
	}
	got, err := httpclient.List[openDayItem](ctx, c.client, fmt.Sprintf("%s?page=%d&limit=%d", listPath, page, limit))
	if err != nil {
		return out.Page{}, fmt.Errorf("list the open cash days: %w", err)
	}
	result := out.Page{TotalPages: got.Meta.TotalPages}
	for _, item := range got.Data {
		result.Days = append(result.Days, out.OpenDay(item))
	}
	return result, nil
}

// AutoClose is a POST that changes nothing twice: the key is the same for the same tenant and date, so the runtime may repeat it
// after a network failure, a 429 or a 5xx. A 404 or a 422 is not a failure: the cash says the day cannot close by itself.
func (c *CashDays) AutoClose(ctx context.Context, tenantID, businessDate string) error {
	err := c.client.Do(ctx, httpclient.Request{
		Method:         http.MethodPost,
		Path:           fmt.Sprintf(closePath, tenantID, businessDate),
		IdempotencyKey: "cash-close-" + tenantID + "-" + businessDate,
	}, &struct{}{})
	var api *httpclient.APIError
	if errors.As(err, &api) && (api.Status == http.StatusNotFound || api.Status == http.StatusUnprocessableEntity) {
		return out.ErrNotClosable
	}
	if err != nil {
		return fmt.Errorf("auto-close the cash day: %w", err)
	}
	return nil
}
