package agenda

import (
	"context"
	"fmt"
	"strings"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/translation"
	activitylog "github.com/hyz-is/arandu-activitylog"
)

// ActivityLogName is the log every entry of this package is recorded in.
const ActivityLogName = "agenda"

// The kinds of subject an entry names.
const (
	// ActivityRefBooking is the subject of an entry about a booking.
	ActivityRefBooking = "agenda_booking"
	// ActivityRefService is the subject of an entry about a service.
	ActivityRefService = "agenda_service"
	// ActivityRefHour is the subject of an entry about a window of the week.
	ActivityRefHour = "agenda_hour"
	// ActivityRefClosure is the subject of an entry about a closure.
	ActivityRefClosure = "agenda_closure"
)

// The events this package records, one per act that changes something. The
// subject of each entry is the booking, the service, the window or the closure.
//
// No entry carries free text a person typed -- the customer's note, the
// reason of a cancellation, the team's note -- and none carries a name: the
// code and the identifiers are what the log keeps, and what was said stays on
// the booking, where the side that may read it reads it.
const (
	ActivityBooked         = "agenda.booked"
	ActivitySettled        = "agenda.settled"
	ActivityReleased       = "agenda.released"
	ActivityConfirmed      = "agenda.confirmed"
	ActivityRescheduled    = "agenda.rescheduled"
	ActivityCancelled      = "agenda.cancelled"
	ActivityCompleted      = "agenda.completed"
	ActivityNoShow         = "agenda.no_show"
	ActivityHoldExpired    = "agenda.hold_expired"
	ActivityReminded       = "agenda.reminded"
	ActivityServiceCreated = "agenda.service_created"
	ActivityServiceUpdated = "agenda.service_updated"
	ActivityServiceDeleted = "agenda.service_deleted"
	ActivityHourCreated    = "agenda.hour_created"
	ActivityHourDeleted    = "agenda.hour_deleted"
	ActivityClosureCreated = "agenda.closure_created"
	ActivityClosureDeleted = "agenda.closure_deleted"
)

// entry is what an entry of the log is about.
type entry struct {
	// event is one of the Activity constants.
	event string
	// ref names the subject of the entry.
	refType, refID string
	// code is how a person calls the subject: the protocol of a booking, the key
	// of a service. The description says it.
	code string
}

// record writes one entry of the act just done, under the Grant it was
// authorized with: the tenant of the entry is the act's, data.Tenant(g), and
// the causer is the subject who acted, named explicitly because a team
// member's act is written under a Grant scoped to the customer's tenant, whose
// own subject is nobody. A task has no causer at all. Without Config.Activity
// it records nothing.
//
// The act already happened when this runs, so a failure to record it is
// returned to the caller rather than swallowed: an act the log does not show
// is the one an audit asks about.
func (c *core) record(ctx context.Context, g security.Grant, actor security.Subject, e entry, properties map[string]any) error {
	if c.cfg.Activity == nil {
		return nil
	}
	description := c.labels.line("activity."+strings.ReplaceAll(e.event, ".", "_"), translation.Replace{"code": e.code})
	log := c.cfg.Activity.In(ctx, g, ActivityLogName).Event(e.event).
		OnRef(activitylog.Ref{Type: e.refType, ID: e.refID})
	if actor.ID != "" {
		log = log.ByRef(activitylog.Ref{Type: "user", ID: actor.ID})
	}
	props := map[string]any{"code": e.code}
	for key, value := range properties {
		props[key] = value
	}
	_, err := log.WithProperties(props).Log(description)
	return err
}

// recordBooking writes the entry of an act on a booking.
func (c *core) recordBooking(ctx context.Context, g security.Grant, actor security.Subject, event string, b *Booking, properties map[string]any) error {
	return c.record(ctx, g, actor, entry{event: event, refType: ActivityRefBooking, refID: b.ID, code: b.Code}, properties)
}

// notify tells the application's Notifier what happened. It runs after the
// change is stored and its failure is logged, not returned: see Notifier. The
// log line carries the kind, the booking's identifier and the type of the error,
// and nothing a person wrote: the text of the error is the application's, and
// may hold a phone number, so the application logs that itself.
func (c *core) notify(ctx context.Context, e Event) {
	if c.cfg.Notifier == nil {
		return
	}
	if err := c.cfg.Notifier.Notify(ctx, e); err != nil {
		c.cfg.Logger.Warn("agenda: the notifier failed after the change was stored", "event", string(e.Kind), "booking", e.Booking.ID, "error_type", fmt.Sprintf("%T", err))
	}
}
