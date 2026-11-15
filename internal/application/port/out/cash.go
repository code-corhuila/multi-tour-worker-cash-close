// Package out holds the ports the job needs from the outside world.
package out

import (
	"context"
	"errors"
)

// OriginAutomaticCreation is the origin of a box that the system created by itself (cash contract, OpenCashDayItem).
const OriginAutomaticCreation = "AUTOMATIC_CREATION"

// OpenDay is one cash that is still open, as the sweep of job 8 sees it. It carries no money and no person.
type OpenDay struct {
	TenantID              string
	BusinessDate          string // YYYY-MM-DD
	TimeZone              string // IANA name of the tenant
	Origin                string
	HasOperationalOpening bool
	PendingClosure        bool
}

// Page is one page of the list across tenants.
type Page struct {
	Days       []OpenDay
	TotalPages int
}

// ErrNotClosable says that the cash refused to close the day by itself: there is no cash, it is not an automatic box or its day
// has not ended. It is not a failure of the run: the next list shows the truth.
var ErrNotClosable = errors.New("the cash did not close the day by itself")

// CashDays is cash-reporting as the job sees it: the application never learns that it is HTTP.
type CashDays interface {
	// ListOpen reads one page (from 1) of the open cash days across tenants, oldest date first.
	ListOpen(ctx context.Context, page, limit int) (Page, error)
	// AutoClose asks the cash to close the day of a tenant automatically. It is safe to repeat.
	AutoClose(ctx context.Context, tenantID, businessDate string) error
}

// Alerts raises the pending-closure alert of a box that was opened and not closed. Until the notification piece exists its
// adapter only writes a line without personal data.
type Alerts interface {
	PendingClosure(ctx context.Context, tenantID, businessDate string) error
}
