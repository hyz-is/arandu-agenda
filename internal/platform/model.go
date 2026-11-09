// Package platform is the bookings table read without the tenant filter. It is
// the one read of arandu-agenda that crosses tenants, and it is needed for two
// things: the agenda is one for every space, so whether a slot is free depends
// on the bookings of all of them, and the team reads the bookings of every
// space.
//
// It is internal so no application can import it. Its table is Global, which is
// how Hesape spells a table with no tenant filter, and any Grant at all would
// read every tenant through it; the package that may reach it is the one whose
// callers have asked a policy first. A customer's use case reads it only for
// OccupancyColumns -- when a booking starts and ends and whether it still holds
// its slot -- and never a name, a note or a space. Its rows never leave as
// anything that can be saved: they are copied into snapshots that hold no
// connection, so nothing read across tenants can be saved or deleted through
// it, and every write to a booking goes through the tenant's own model under a
// Grant scoped to that tenant.
package platform

import (
	"time"

	"github.com/arandu-io/hesape/database/model"
)

// Booking is a row of the bookings table, of any tenant. It declares the
// columns a list shows and nothing else: a note the team wrote about a booking
// never travels with it.
type Booking struct {
	model.Model

	ID            string     `db:"id"`
	TenantID      string     `db:"tenant_id"`
	Code          string     `db:"code"`
	ServiceKey    string     `db:"service_key"`
	ServiceName   string     `db:"service_name"`
	SubjectType   string     `db:"subject_type"`
	SubjectID     string     `db:"subject_id"`
	SubjectLabel  string     `db:"subject_label"`
	RequesterID   string     `db:"requester_id"`
	RequesterName string     `db:"requester_name"`
	StartsAt      time.Time  `db:"starts_at"`
	EndsAt        time.Time  `db:"ends_at"`
	Status        string     `db:"status"`
	HoldUntil     *time.Time `db:"hold_until"`
	PaymentRef    string     `db:"payment_ref"`
	Reschedules   int        `db:"reschedules"`
	RemindedAt    *time.Time `db:"reminded_at"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
}

// bookingTable is the bookings table, Global: no tenant filter on any
// statement. The name is written here rather than imported, because the package
// that owns the constant imports this one.
var bookingTable = model.NewTable(model.TableSpec{
	Name:         "agenda_bookings",
	New:          func() model.Entity { return new(Booking) },
	ManualKey:    true,
	NoTimestamps: true,
	Global:       true,
})

// Columns are the columns Booking reads.
var Columns = []any{
	"id", "tenant_id", "code", "service_key", "service_name", "subject_type", "subject_id", "subject_label",
	"requester_id", "requester_name", "starts_at", "ends_at", "status", "hold_until", "payment_ref", "reschedules",
	"reminded_at", "created_at", "updated_at",
}

// OccupancyColumns are the only columns a customer's use case reads across
// tenants: when a booking holds the team, and whether it still does.
var OccupancyColumns = []any{"id", "starts_at", "ends_at", "status", "hold_until"}

// KeyColumns are the columns that say which booking, and in which tenant: what a
// task or the team reads to find the tenant it then acts in.
var KeyColumns = []any{"id", "tenant_id"}
