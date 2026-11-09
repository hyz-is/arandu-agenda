package feature_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"

	agenda "github.com/hyz-is/arandu-agenda"
)

// settleGrant is the Grant an application issues for a payment it decided is
// real, for the space of the booking.
func settleGrant(tenant string) security.Grant {
	return security.SystemGrant(agenda.AgendaSettle, tenant)
}

func TestAPaymentSettlesABookingAndTheSlotIsHeldForGood(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	b := book(t, w, ana, "reconfiguracao", firstSlot(t, w, ana, "reconfiguracao", wednesday))

	settled, err := w.module.Settle(ctx, settleGrant("acme"), b.ID, "pix-123")
	if err != nil {
		t.Fatalf("settling: %v", err)
	}
	if settled.Status != agenda.StatusScheduled || settled.PaymentRef != "pix-123" || settled.HoldUntil != nil {
		t.Fatalf("the settled booking is %s, %q, %v", settled.Status, settled.PaymentRef, settled.HoldUntil)
	}
	events := w.notices.of(agenda.EventScheduled)
	if len(events) != 1 || events[0].Booking.ID != b.ID || events[0].By != agenda.BySystem || events[0].Kind.ForTeam() {
		t.Fatalf("the notifier was told %+v", events)
	}
	if got := lastEntry(t, w, "acme", "agenda.settled"); got.Subject != b.ID || got.Causer != "system" || strings.Contains(got.Properties, "pix-123") {
		t.Errorf("the log says %+v", got)
	}

	// The same payment delivered again is the same answer, and nothing more is
	// written or told.
	for i := 0; i < 3; i++ {
		again, err := w.module.Settle(ctx, settleGrant("acme"), b.ID, "pix-123")
		if err != nil || again.Status != agenda.StatusScheduled || again.PaymentRef != "pix-123" {
			t.Fatalf("settling again: %+v, %v", again, err)
		}
	}
	if len(w.notices.of(agenda.EventScheduled)) != 1 || countEvents(entries(t, w, "acme"), "agenda.settled") != 1 {
		t.Errorf("a repeated payment was told or logged again")
	}
	// Another reference for a booking that is paid is another payment: the
	// application refunds it.
	requireErr(t, second(w.module.Settle(ctx, settleGrant("acme"), b.ID, "pix-999")), agenda.ErrNotSettleable, "a second payment")
}

func TestAPaymentIsTakenOnlyOnAGrantTheApplicationIssued(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	b := book(t, w, ana, "reconfiguracao", firstSlot(t, w, ana, "reconfiguracao", wednesday))

	for name, g := range map[string]security.Grant{
		"the zero Grant":             {},
		"a Grant for reading":        security.SystemGrant(agenda.AgendaViewOwn, "acme"),
		"a Grant for booking":        security.SystemGrant(agenda.AgendaBook, "acme"),
		"a Grant for the team":       security.SystemGrant(agenda.AgendaManage, "platform"),
		"a Grant with no tenant":     security.SystemGrant(agenda.AgendaSettle, ""),
		"a Grant for the tasks":      security.SystemGrant(agenda.AgendaRunTasks, "acme"),
		"a Grant for another tenant": security.SystemGrant(agenda.AgendaSettle, "globex"),
	} {
		_, err := w.app.Settle(ctx, g, b.ID, "pix-1")
		if name == "a Grant for another tenant" {
			requireErr(t, err, agenda.ErrNotFound, name)
			continue
		}
		requireErr(t, err, security.ErrForbidden, name)
		requireErr(t, second(w.app.Find(ctx, g, b.ID)), security.ErrForbidden, name+" to look it up")
	}
	// A booking nobody paid is still waiting.
	shown, _ := w.bookings.Booking(ctx, ana, b.ID)
	if shown.Status != agenda.StatusAwaitingPayment {
		t.Fatalf("a refused payment settled the booking: %s", shown.Status)
	}
	var rejected validation.Errors
	if _, err := w.app.Settle(ctx, settleGrant("acme"), b.ID, "  "); !errors.As(err, &rejected) {
		t.Errorf("a payment with no reference was taken: %v", err)
	}
	requireErr(t, second(w.app.Settle(ctx, settleGrant("acme"), "nobody", "pix-1")), agenda.ErrNotFound, "a booking that does not exist")
}

func TestABookingIsFoundByItsProtocolOrItsIdentifierInItsOwnSpace(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	b := book(t, w, member("acme", "user-ana"), "instalacao", local(2026, time.November, 11, 9, 0))

	for _, ref := range []string{b.ID, b.Code, " " + b.Code + " "} {
		found, err := w.module.Find(ctx, settleGrant("acme"), ref)
		if err != nil || found.ID != b.ID {
			t.Errorf("finding by %q: %+v, %v", ref, found, err)
		}
	}
	for _, ref := range []string{"", "AG-999999"} {
		requireErr(t, second(w.module.Find(ctx, settleGrant("acme"), ref)), agenda.ErrNotFound, "finding "+ref)
	}
}

func TestALatePaymentIsKeptIfTheSlotIsStillFree(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	b := book(t, w, ana, "reconfiguracao", firstSlot(t, w, ana, "reconfiguracao", thursday))

	// The hold lapsed, nothing else took the slot, and the payment arrives -- both
	// before the task marked the booking as expired and after.
	w.clock.advance(25 * time.Hour)
	if _, err := w.app.Settle(ctx, settleGrant("acme"), b.ID, "pix-late"); err != nil {
		t.Fatalf("a late payment on a free slot: %v", err)
	}
	if shown, _ := w.bookings.Booking(ctx, ana, b.ID); shown.Status != agenda.StatusScheduled {
		t.Fatalf("the booking is %s", shown.Status)
	}

	second := book(t, w, ana, "reconfiguracao", local(2026, time.November, 13, 14, 0))
	w.clock.advance(25 * time.Hour)
	if n, _ := w.module.ExpireHolds(ctx); n != 1 {
		t.Fatalf("ExpireHolds expired %d", n)
	}
	if shown, _ := w.bookings.Booking(ctx, ana, second.ID); shown.Status != agenda.StatusExpired {
		t.Fatalf("the booking is %s", shown.Status)
	}
	if _, err := w.app.Settle(ctx, settleGrant("acme"), second.ID, "pix-later"); err != nil {
		t.Fatalf("a payment on an expired booking whose slot is free: %v", err)
	}
	if shown, _ := w.bookings.Booking(ctx, ana, second.ID); shown.Status != agenda.StatusScheduled || shown.PaymentRef != "pix-later" {
		t.Fatalf("the booking is %s", shown.Status)
	}
}

func TestALatePaymentForASlotSomebodyTookIsToldSoTheApplicationCanRefund(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana, gil := member("acme", "user-ana"), member("globex", "user-gil")
	start := firstSlot(t, w, ana, "reconfiguracao", thursday)
	b := book(t, w, ana, "reconfiguracao", start)

	w.clock.advance(25 * time.Hour)
	book(t, w, gil, "instalacao", start)

	requireErr(t, second(w.app.Settle(ctx, settleGrant("acme"), b.ID, "pix-late")), agenda.ErrSlotTaken, "a late payment for a taken slot")
	shown, _ := w.bookings.Booking(ctx, ana, b.ID)
	if shown.Status == agenda.StatusScheduled || shown.PaymentRef != "" {
		t.Fatalf("the booking took a payment for a slot that is not its own: %s %q", shown.Status, shown.PaymentRef)
	}
	if len(w.notices.of(agenda.EventScheduled)) != 0 {
		t.Error("the customer was told a booking they cannot have was scheduled")
	}
}

func TestACancelledBookingTakesNoPayment(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	b := book(t, w, ana, "reconfiguracao", firstSlot(t, w, ana, "reconfiguracao", wednesday))
	if _, err := w.bookings.Cancel(ctx, ana, b.ID, ""); err != nil {
		t.Fatal(err)
	}
	requireErr(t, second(w.app.Settle(ctx, settleGrant("acme"), b.ID, "pix-1")), agenda.ErrNotSettleable, "a payment for a cancelled booking")
}

func TestTwoPaymentsAtOnceSettleOnceAndTellOnce(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	b := book(t, w, ana, "reconfiguracao", firstSlot(t, w, ana, "reconfiguracao", wednesday))

	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := w.app.Settle(ctx, settleGrant("acme"), b.ID, "pix-same")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("a repeated payment failed: %v", err)
		}
	}
	if len(w.notices.of(agenda.EventScheduled)) != 1 || countEvents(entries(t, w, "acme"), "agenda.settled") != 1 {
		t.Fatalf("the payment was told %d times and logged %d times", len(w.notices.of(agenda.EventScheduled)), countEvents(entries(t, w, "acme"), "agenda.settled"))
	}
}
