package agenda

import (
	"context"
	"errors"
	"time"
)

// The things the application provides. This package knows no table of users,
// sends no message of its own, keeps no list of the team and takes no payment:
// each of those is an interface the application implements with what it
// already has, and passes in Config.

// ErrNoAccount is what a Directory answers when it does not know the person.
var ErrNoAccount = errors.New("agenda: the directory does not know that person")

// ErrSlotTaken is what Book, Reschedule, Settle and Release answer when the
// start asked for is not free any more: somebody else took the last place, the
// team closed the day, or the hold of an unpaid booking lapsed and another
// booking was made in its place. An application that paid for a booking it
// then cannot keep refunds it, or books again.
var ErrSlotTaken = errors.New("agenda: that time is not available any more")

// ErrNotSettleable is what Settle answers for a booking that cannot take a
// payment: it was cancelled, completed or never waited for one.
var ErrNotSettleable = errors.New("agenda: that booking cannot take a payment in its state")

// Person is somebody the application knows, as the screens name them.
type Person struct {
	ID    string
	Name  string
	Email string
}

// Directory names people. It is optional: without one a booking keeps the name
// of its requester empty and a screen shows the identifier instead.
type Directory interface {
	// Person returns a user of a tenant, or ErrNoAccount when there is none.
	Person(ctx context.Context, tenant, id string) (Person, error)
}

// SubjectResolver checks what a booking is about and names it. It is optional,
// and it is the application's: the module keeps the subject opaque -- a type,
// an identifier and a label the application understands, "camera", "42",
// "Portaria" -- and cannot know whether the camera is the customer's.
type SubjectResolver interface {
	// Resolve returns the label of the subject, or an error when the subject
	// does not exist or is not the tenant's. An error rejects the booking.
	Resolve(ctx context.Context, tenant string, subject BookingSubject) (label string, err error)
}

// BookingSubject is what a booking is about, opaque to this package: the
// application passes a kind, its own identifier and a label for the screens.
type BookingSubject struct {
	Type  string
	ID    string
	Label string
}

// EventKind says what happened to a booking that somebody should be told about.
type EventKind string

// The events a Notifier is told about.
const (
	// EventBooked is a booking just made. Event.AwaitingPayment says whether it
	// waits for a payment. The team is the one to tell, and the customer too:
	// the customer's copy is the receipt, with the payment link when it waits.
	EventBooked EventKind = "agenda.booked"
	// EventScheduled is a booking that waited for a payment and now holds its
	// slot: the application settled it, or the team released it as a courtesy.
	EventScheduled EventKind = "agenda.scheduled"
	// EventReminder is the reminder that goes before the start, asking the
	// customer to confirm or to reschedule. It carries the two paths.
	EventReminder EventKind = "agenda.reminder"
	// EventConfirmed is a booking the customer confirmed.
	EventConfirmed EventKind = "agenda.confirmed"
	// EventRescheduled is a booking moved to another time.
	EventRescheduled EventKind = "agenda.rescheduled"
	// EventCancelled is a booking cancelled. Event.By says by whom.
	EventCancelled EventKind = "agenda.cancelled"
	// EventCompleted is a booking the team marked as done.
	EventCompleted EventKind = "agenda.completed"
	// EventHoldExpired is a booking that waited for a payment that never came,
	// and lost its slot.
	EventHoldExpired EventKind = "agenda.hold_expired"
)

// ForTeam says whether the team is the one to tell for an event whose side does
// not depend on who acted. When it is not, the requester of the booking is. For
// EventRescheduled and EventCancelled the side that did not act is the one to
// tell, which only the event knows: see Event.ForTeam.
func (k EventKind) ForTeam() bool { return k == EventBooked || k == EventConfirmed }

// The sides that can act on a booking, as Event.By and the activity log name
// them.
const (
	// ByCustomer is the requester or a member of their space.
	ByCustomer = "customer"
	// ByTeam is a member of the platform team.
	ByTeam = "team"
	// BySystem is a task: Remind and ExpireHolds.
	BySystem = "system"
)

// Event is what a Notifier is handed.
type Event struct {
	Kind EventKind
	// Booking is the booking after the change.
	Booking Summary
	// By is the side that acted: ByCustomer, ByTeam or BySystem.
	By string
	// AwaitingPayment, on EventBooked, says the booking holds its slot until
	// Booking.HoldUntil and becomes a scheduled one when the payment settles.
	AwaitingPayment bool
	// ConfirmURL and RescheduleURL, on EventReminder, are the paths of the
	// customer's own screens: the booking, which has the button that confirms,
	// and the form that moves it. They are paths and not addresses, because the
	// module does not know the host; the application puts its own in front. Both
	// need the customer signed in.
	ConfirmURL    string
	RescheduleURL string
}

// ForTeam says whether the team is the one to tell. For a rescheduling or a
// cancellation that is the case when the customer was the one who acted; when
// the team acted, the requester is told. A booking that expired and a reminder
// are the requester's.
func (e Event) ForTeam() bool {
	switch e.Kind {
	case EventRescheduled, EventCancelled:
		return e.By == ByCustomer
	}
	return e.Kind.ForTeam()
}

// Notifier tells people that something happened to a booking.
//
// The application implements it with its own channel -- a WhatsApp message, an
// e-mail: this package sends none. Notify runs after the change is stored, and
// its error does not undo the change: the booking is already made and the slot
// already taken, and a channel that is down is no reason for a customer to book
// it twice. The error is logged, with no personal text, and the request goes
// on. An application that must not lose a notification enqueues it here and
// delivers it from its queue.
type Notifier interface {
	Notify(ctx context.Context, e Event) error
}

// BookInput is what a customer asks for when booking. There is no tenant, no
// requester and no status here and there must never be one: the tenant and the
// requester come from the Grant and the subject, and the status is the
// module's.
type BookInput struct {
	// Service is the key of the service: "reconfiguracao".
	Service string
	// Start is the start asked for, an instant. It has to be one of the starts
	// Slots offers for its day.
	Start time.Time
	// Subject is what the booking is about; see BookingSubject.
	Subject BookingSubject
	// Note is a free line from the customer to the team.
	Note string
}

// ListQuery narrows the customer's list. Page counts from one.
type ListQuery struct {
	Search   string
	Statuses []Status
	Sort     string
	Dir      string
	Page     int
	PerPage  int
}

// TeamQuery narrows the team's list. Page counts from one.
type TeamQuery struct {
	// Search matches the code, the requester's name, the service and the
	// subject's label.
	Search   string
	Statuses []Status
	// Day narrows the list to the bookings that start on that day of
	// Config.Zone, as "2026-11-10". Empty means every day.
	Day string
	// Tenant narrows the list to one space.
	Tenant  string
	Sort    string
	Dir     string
	Page    int
	PerPage int
}

// ConfigQuery narrows a list of the team's settings. Page counts from one.
type ConfigQuery struct {
	Search  string
	Sort    string
	Dir     string
	Page    int
	PerPage int
}

// Page is one page of a list of bookings, and how many rows the whole list has.
type Page struct {
	Rows  []Summary
	Total int
	Page  int
	Pages int
}

// Listing is one page of a list of the team's settings.
type Listing[T any] struct {
	Rows  []T
	Total int
	Page  int
	Pages int
}

// pagesOf is how many pages of perPage rows hold total rows, at least one.
func pagesOf(total, perPage int) int {
	if perPage <= 0 || total <= 0 {
		return 1
	}
	return (total + perPage - 1) / perPage
}

// ServiceInput is what the team writes when it creates or changes a service.
// There is no price: what a service costs is the application's catalogue.
type ServiceInput struct {
	Key             string
	Name            string
	Description     string
	DurationMinutes int
	RequiresPayment bool
	Active          bool
}

// HourInput is a window of the team's week, in minutes since midnight of
// Config.Zone.
type HourInput struct {
	Weekday     int
	StartMinute int
	EndMinute   int
}

// ClosureInput is a range the team does not take bookings in.
type ClosureInput struct {
	StartsAt time.Time
	EndsAt   time.Time
	Reason   string
}

// clock is what the services read the time from.
type clock func() time.Time
