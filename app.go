package agenda

import (
	"context"
	"unicode/utf8"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/database"

	"github.com/hyz-is/arandu-agenda/internal/platform"
)

// AppService is what the application's own code calls: the payment that settles
// a booking, the lookup of one by its protocol, and the two tasks the scheduler
// runs.
//
// It authorizes by Grant and not by subject. Settle and Find take a Grant the
// application issued for AgendaSettle for the space of the booking, after it
// decided that the payment is real -- a webhook it verified, a balance it
// debited. The tasks have nobody signed in, and issue their own, in grants.go.
// Nothing here is reachable from a route.
type AppService struct {
	*core
}

// NewAppService wires the service over the application's database handle.
func NewAppService(db *data.DB, cfg Config) *AppService {
	return &AppService{core: newCore(db, cfg.withDefaults())}
}

// batch is how many due bookings one pass of a task takes. A task keeps passing
// until a pass finds fewer.
const batch = 200

// Find returns a booking of the Grant's space by its identifier or by its
// protocol code ("AG-000123"). A booking of another space is not found.
func (s *AppService) Find(ctx context.Context, g security.Grant, ref string) (*Booking, error) {
	g, err := appGrant(g)
	if err != nil {
		return nil, err
	}
	ref = trimmed(ref)
	if ref == "" {
		return nil, ErrNotFound
	}
	b, err := Bookings(s.db).WhereKey(ref).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if b == nil {
		b, err = Bookings(s.db).Where("code", "=", ref).First(ctx, g)
		if err != nil {
			return nil, err
		}
	}
	if b == nil {
		return nil, ErrNotFound
	}
	return b, nil
}

// Settle puts the application's payment on a booking that waits for one: it
// holds its slot for good, and the customer is told.
//
// It is idempotent. A payment delivered twice -- a webhook that is retried -- is
// the second call finding the booking settled with the same reference, and it
// answers the booking as it is, with nothing written and nobody told again.
//
// A booking whose hold lapsed is settled only if its slot is still free. When it
// is not -- somebody booked it in the meantime -- the answer is ErrSlotTaken, and
// the application refunds the payment or books again. A booking in a state that
// takes no payment -- cancelled, or already settled with another reference --
// answers ErrNotSettleable, for the same reason.
func (s *AppService) Settle(ctx context.Context, g security.Grant, bookingID, paymentRef string) (*Booking, error) {
	g, err := appGrant(g)
	if err != nil {
		return nil, err
	}
	paymentRef = trimmed(paymentRef)
	if paymentRef == "" || utf8.RuneCountInString(paymentRef) > maxRefLen {
		return nil, s.labels.invalid("payment_ref", "payment_ref")
	}

	var (
		settled *Booking
		already bool
	)
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		now := s.now()
		if err := s.lock(ctx, g, now); err != nil {
			return err
		}
		b, err := Bookings(s.db).WhereKey(trimmed(bookingID)).First(ctx, g)
		if err != nil {
			return err
		}
		if b == nil {
			return ErrNotFound
		}
		if !b.CanSettle() {
			if b.PaymentRef == paymentRef && (b.Status == StatusScheduled || b.Status == StatusConfirmed || b.Status == StatusCompleted || b.Status == StatusNoShow) {
				settled, already = b, true
				return nil
			}
			return ErrNotSettleable
		}
		if b.Lapsed(now) {
			if err := s.holdAgain(ctx, g, b, now); err != nil {
				return ErrSlotTaken
			}
		}
		if err := b.Settle(paymentRef, now); err != nil {
			return ErrNotSettleable
		}
		if _, err := b.Save(ctx, g); err != nil {
			return err
		}
		settled = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	if already {
		return settled, nil
	}
	if err := s.recordBooking(ctx, g, security.Subject{}, ActivitySettled, settled, nil); err != nil {
		return nil, err
	}
	s.notify(ctx, Event{Kind: EventScheduled, Booking: summaryOf(settled), By: BySystem})
	return settled, nil
}

// Remind tells the customer of every booking that is still to be confirmed and
// starts within Config.RemindBefore: one Reminder each, carrying the paths of
// the screens where they confirm or move it. A booking is reminded once -- the
// moment is written before the Notifier is asked, by a statement that only the
// first of two overlapping runs wins -- and a booking moved to another time is
// reminded again for the new one. It returns how many it reminded.
//
// The application schedules it every few minutes.
func (s *AppService) Remind(ctx context.Context) (int, error) {
	realm, err := taskGrant(s.cfg.TeamTenant)
	if err != nil {
		return 0, err
	}
	reminded := 0
	for {
		now := s.now()
		due, err := platform.Bookings(s.db).
			Where("status", "=", string(StatusScheduled)).WhereNull("reminded_at").
			Where("starts_at", ">", now).Where("starts_at", "<=", now.Add(s.cfg.RemindBefore)).
			OrderBy("starts_at").OrderBy("id").Limit(batch).Get(ctx, realm, platform.KeyColumns...)
		if err != nil {
			return reminded, err
		}
		for _, row := range due {
			g, err := scopedGrant(realm, AgendaRunTasks, row.TenantID)
			if err != nil {
				return reminded, err
			}
			won, err := Bookings(s.db).WhereKey(row.ID).Where("status", "=", string(StatusScheduled)).WhereNull("reminded_at").
				Update(ctx, g, map[string]any{"reminded_at": now, "updated_at": now})
			if err != nil {
				return reminded, err
			}
			if won == 0 {
				continue
			}
			b, err := Bookings(s.db).WhereKey(row.ID).First(ctx, g)
			if err != nil {
				return reminded, err
			}
			if b == nil {
				continue
			}
			reminded++
			if err := s.recordBooking(ctx, g, security.Subject{}, ActivityReminded, b, nil); err != nil {
				return reminded, err
			}
			urls := URLs{cfg: s.cfg}
			s.notify(ctx, Event{
				Kind: EventReminder, Booking: summaryOf(b), By: BySystem,
				ConfirmURL: urls.Show(b.ID), RescheduleURL: urls.Reschedule(b.ID),
			})
		}
		if len(due) < batch {
			return reminded, nil
		}
	}
}

// ExpireHolds frees the slot of every booking that waited for a payment past its
// hold: it becomes expired, and the customer is told. A booking whose payment
// arrives afterwards can still be settled if its slot is free. It returns how
// many it expired.
//
// The application schedules it every few minutes. Until it runs, a lapsed hold
// already frees its slot -- availability reads the clock, not the status -- so
// the task only keeps the statuses honest and tells the customer.
func (s *AppService) ExpireHolds(ctx context.Context) (int, error) {
	realm, err := taskGrant(s.cfg.TeamTenant)
	if err != nil {
		return 0, err
	}
	expired := 0
	for {
		now := s.now()
		due, err := platform.Bookings(s.db).
			Where("status", "=", string(StatusAwaitingPayment)).WhereNotNull("hold_until").Where("hold_until", "<=", now).
			OrderBy("hold_until").OrderBy("id").Limit(batch).Get(ctx, realm, platform.KeyColumns...)
		if err != nil {
			return expired, err
		}
		for _, row := range due {
			g, err := scopedGrant(realm, AgendaRunTasks, row.TenantID)
			if err != nil {
				return expired, err
			}
			won, err := Bookings(s.db).WhereKey(row.ID).Where("status", "=", string(StatusAwaitingPayment)).Where("hold_until", "<=", now).
				Update(ctx, g, map[string]any{"status": string(StatusExpired), "updated_at": now})
			if err != nil {
				return expired, err
			}
			if won == 0 {
				continue
			}
			b, err := Bookings(s.db).WhereKey(row.ID).First(ctx, g)
			if err != nil {
				return expired, err
			}
			if b == nil {
				continue
			}
			expired++
			if err := s.recordBooking(ctx, g, security.Subject{}, ActivityHoldExpired, b, nil); err != nil {
				return expired, err
			}
			s.notify(ctx, Event{Kind: EventHoldExpired, Booking: summaryOf(b), By: BySystem})
		}
		if len(due) < batch {
			return expired, nil
		}
	}
}
