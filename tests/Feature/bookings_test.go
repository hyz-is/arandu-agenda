package feature_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"

	agenda "github.com/hyz-is/arandu-agenda"
)

// clocks are the starts as "15:04" in the team's zone.
func clocks(starts []time.Time) []string {
	out := make([]string, 0, len(starts))
	for _, start := range starts {
		out = append(out, start.In(zone).Format("15:04"))
	}
	return out
}

func TestTheCustomerIsOfferedOnlyWhatTheTeamActivated(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	if _, err := w.team.CreateService(ctx, agent("team-1"), agenda.ServiceInput{Key: "antiga", Name: "Serviço antigo", DurationMinutes: 30, Active: false}); err != nil {
		t.Fatal(err)
	}

	services, err := w.bookings.Services(ctx, member("acme", "user-ana"))
	if err != nil {
		t.Fatalf("listing the services: %v", err)
	}
	var keys []string
	for _, s := range services {
		keys = append(keys, s.Key)
	}
	if strings.Join(keys, ",") != "instalacao,reconfiguracao" {
		t.Fatalf("the customer is offered %v", keys)
	}
	// There is no price in what the customer is shown: it is the application's.
	if fields := reflect.TypeOf(agenda.ServiceView{}); fields.NumField() == 0 {
		t.Fatal("ServiceView has no field")
	} else {
		for i := 0; i < fields.NumField(); i++ {
			if strings.Contains(strings.ToLower(fields.Field(i).Name), "price") {
				t.Errorf("ServiceView carries %s, and the module has no price", fields.Field(i).Name)
			}
		}
	}

	// A service the team switched off is not found, and says so on the field.
	_, err = w.bookings.Book(ctx, member("acme", "user-ana"), agenda.BookInput{Service: "antiga", Start: firstSlot(t, w, member("acme", "user-ana"), "instalacao", wednesday)})
	var rejected validation.Errors
	if !errors.As(err, &rejected) || len(rejected["service"]) == 0 {
		t.Fatalf("booking a service nobody offers returned %v", err)
	}
	// A subject who may not book sees nothing of the services.
	if _, err := w.bookings.Services(ctx, security.Subject{ID: "u", Tenant: "acme", Verified: true, Actions: []security.Action{agenda.AgendaViewOwn}}); !errors.Is(err, security.ErrForbidden) {
		t.Fatalf("a subject who cannot book listed the services: %v", err)
	}
}

func TestTheFreeTimesFollowTheHoursTheNoticeAndTheClosures(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")

	// Monday noon, a day of notice: nothing before Tuesday noon, so the first
	// start is Tuesday at 14:00, the afternoon window.
	tue, err := w.bookings.Slots(ctx, ana, "reconfiguracao", tuesday)
	if err != nil {
		t.Fatal(err)
	}
	if got := clocks(tue); got[0] != "14:00" || got[len(got)-1] != "17:20" || len(got) != 11 {
		t.Errorf("Tuesday offers %v", got)
	}
	// Today is inside the notice, a Saturday has no hours, and a day past the
	// horizon of thirty days is not offered.
	for name, day := range map[string]time.Time{
		"today": time.Date(2026, time.November, 9, 0, 0, 0, 0, time.UTC), "Saturday": saturday,
		"past the horizon": time.Date(2026, time.December, 20, 0, 0, 0, 0, time.UTC),
	} {
		if got, err := w.bookings.Slots(ctx, ana, "reconfiguracao", day); err != nil || len(got) != 0 {
			t.Errorf("%s offers %v (%v)", name, clocks(got), err)
		}
	}
	// The times are instants: 09:00 in Sao Paulo is 12:00 UTC.
	wed, _ := w.bookings.Slots(ctx, ana, "reconfiguracao", wednesday)
	if wed[0].UTC().Format("15:04") != "12:00" || wed[0].In(zone).Format("15:04") != "09:00" {
		t.Errorf("the first start of Wednesday is %s UTC", wed[0].UTC().Format("15:04"))
	}

	// A closure takes the times it touches, and the booking of another space
	// takes its own.
	if _, err := w.team.CreateClosure(ctx, agent("team-1"), agenda.ClosureInput{
		StartsAt: local(2026, time.November, 11, 10, 0), EndsAt: local(2026, time.November, 11, 11, 0), Reason: "Treinamento",
	}); err != nil {
		t.Fatalf("closing: %v", err)
	}
	got, _ := w.bookings.Slots(ctx, ana, "reconfiguracao", wednesday)
	for _, blocked := range []string{"09:40", "10:00", "10:20", "10:40"} {
		if contains(clocks(got), blocked) {
			t.Errorf("%s is offered inside the closure", blocked)
		}
	}
	taken := local(2026, time.November, 11, 11, 20)
	book(t, w, member("globex", "user-gil"), "instalacao", taken)
	got, _ = w.bookings.Slots(ctx, ana, "reconfiguracao", wednesday)
	for _, blocked := range []string{"11:00", "11:20", "11:40"} {
		if contains(clocks(got), blocked) {
			t.Errorf("%s overlaps a booking of another space and is offered", blocked)
		}
	}
	// A day without a Grant to book is refused.
	if _, err := w.bookings.Slots(ctx, security.Subject{ID: "u", Tenant: "acme", Verified: true}, "reconfiguracao", wednesday); !errors.Is(err, security.ErrForbidden) {
		t.Errorf("a subject with no action looked at the times: %v", err)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestABookingForAPaidServiceWaitsForThePayment(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	start := firstSlot(t, w, ana, "reconfiguracao", wednesday)

	booked := book(t, w, ana, "reconfiguracao", start)
	if booked.Status != agenda.StatusAwaitingPayment || booked.Code != "AG-000001" || booked.TenantID != "acme" {
		t.Fatalf("the booking is %s %s in %s", booked.Status, booked.Code, booked.TenantID)
	}
	if booked.HoldUntil == nil || !booked.HoldUntil.Equal(startOfTime().Add(24*time.Hour)) {
		t.Errorf("the slot is held until %v, want a day from now", booked.HoldUntil)
	}
	if !booked.StartsAt.Equal(start) || !booked.EndsAt.Equal(start.Add(40*time.Minute)) {
		t.Errorf("the booking holds %s to %s", booked.StartsAt, booked.EndsAt)
	}
	if booked.RequesterID != "user-ana" || booked.RequesterName != "Ana Souza" || booked.ServiceName != "Reconfiguração de câmera" {
		t.Errorf("the requester is %q %q and the service %q", booked.RequesterID, booked.RequesterName, booked.ServiceName)
	}
	if booked.SubjectType != "camera" || booked.SubjectID != "42" || booked.SubjectLabel != "Portaria" || booked.CustomerNote == "" {
		t.Errorf("the booking is about %q %q %q", booked.SubjectType, booked.SubjectID, booked.SubjectLabel)
	}

	// The team and the customer are told, and the customer's copy says it waits.
	got := w.notices.of(agenda.EventBooked)
	if len(got) != 1 || !got[0].AwaitingPayment || got[0].By != agenda.ByCustomer || got[0].Booking.Code != "AG-000001" || !got[0].Kind.ForTeam() {
		t.Fatalf("the notifier was told %+v", w.notices.all())
	}

	// The same slot for a free service is a scheduled booking, with the next
	// protocol, whatever the space: the code names one booking.
	other := book(t, w, member("globex", "user-gil"), "instalacao", firstSlot(t, w, member("globex", "user-gil"), "instalacao", thursday))
	if other.Status != agenda.StatusScheduled || other.HoldUntil != nil || other.Code != "AG-000002" {
		t.Errorf("a free service made a %s booking %s with a hold %v", other.Status, other.Code, other.HoldUntil)
	}
	if got := w.notices.of(agenda.EventBooked); len(got) != 2 || got[1].AwaitingPayment {
		t.Errorf("the second booking said it waits: %+v", got)
	}

	// The customer reads it back, without what is the team's.
	shown, err := w.bookings.Booking(ctx, ana, booked.ID)
	if err != nil || shown.Code != "AG-000001" || shown.Status != agenda.StatusAwaitingPayment || shown.HoldUntil == nil {
		t.Fatalf("reading it back: %+v, %v", shown, err)
	}
}

func TestTheTenantOfABookingIsTheGrantsAndNeverTheInputs(t *testing.T) {
	t.Parallel()
	w := install(t)

	// The input has no field that could carry a tenant, a requester or a status.
	kind := reflect.TypeOf(agenda.BookInput{})
	for i := 0; i < kind.NumField(); i++ {
		name := strings.ToLower(kind.Field(i).Name)
		for _, forbidden := range []string{"tenant", "requester", "status", "space", "code"} {
			if strings.Contains(name, forbidden) {
				t.Errorf("BookInput has %s, which the request must never choose", kind.Field(i).Name)
			}
		}
	}
	// Whoever books, the booking is in their own space, and the requester is who
	// is signed in.
	for _, who := range []security.Subject{member("acme", "user-ana"), member("globex", "user-gil")} {
		b := book(t, w, who, "instalacao", firstSlot(t, w, who, "instalacao", map[string]time.Time{"acme": wednesday, "globex": thursday}[who.Tenant]))
		if b.TenantID != who.Tenant || b.RequesterID != who.ID {
			t.Errorf("%s booked into %s as %s", who.Tenant, b.TenantID, b.RequesterID)
		}
	}
}

func TestAStartThatIsNotOfferedIsRefusedLikeOneThatWasTaken(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	start := firstSlot(t, w, ana, "instalacao", wednesday)
	book(t, w, ana, "instalacao", start)

	for name, in := range map[string]time.Time{
		"a time that was taken":     start,
		"a time of another space's": start,
		"between two steps":         start.Add(7 * time.Minute),
		"outside the hours":         local(2026, time.November, 11, 3, 0),
		"on a Saturday":             local(2026, time.November, 14, 10, 0),
		"inside the notice":         local(2026, time.November, 10, 9, 0),
		"past the horizon":          local(2026, time.December, 21, 10, 0),
		"the lunch hour":            local(2026, time.November, 11, 12, 0),
	} {
		who := ana
		if name == "a time of another space's" {
			who = member("globex", "user-gil")
		}
		_, err := w.bookings.Book(ctx, who, agenda.BookInput{Service: "instalacao", Start: in})
		requireErr(t, err, agenda.ErrSlotTaken, name)
		// The screen gets the sentence on the field, and the caller gets the sentinel.
		var rejected validation.Errors
		if !errors.As(err, &rejected) || len(rejected["start"]) == 0 {
			t.Errorf("%s: the refusal carries no message for the form: %v", name, err)
		}
	}
	if got := w.notices.of(agenda.EventBooked); len(got) != 1 {
		t.Errorf("a refused booking was told about: %d events", len(got))
	}
}

func TestTheLastPlaceGoesToOnePersonWhenManyAskAtOnce(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	start := firstSlot(t, w, member("acme", "user-ana"), "instalacao", wednesday)

	const people = 8
	results := make(chan error, people)
	var wg sync.WaitGroup
	for i := 0; i < people; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := w.bookings.Book(ctx, member(fmt.Sprintf("space-%d", i), fmt.Sprintf("user-%d", i)), agenda.BookInput{Service: "instalacao", Start: start})
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	won, lost := 0, 0
	for err := range results {
		switch {
		case err == nil:
			won++
		case errors.Is(err, agenda.ErrSlotTaken):
			lost++
		default:
			t.Errorf("a booking failed for another reason: %v", err)
		}
	}
	if won != 1 || lost != people-1 {
		t.Fatalf("%d people got the last place and %d were told it was taken; want 1 and %d", won, lost, people-1)
	}
	page, err := w.team.Bookings(ctx, agent("team-1"), agenda.TeamQuery{})
	if err != nil || page.Total != 1 || page.Rows[0].Code != "AG-000001" {
		t.Fatalf("the agenda holds %d bookings (%v), the first %q; the lost bookings gave their number back", page.Total, err, page.Rows[0].Code)
	}
}

func TestTwoPlacesAreTakenByTwoPeopleAtOnceWhenTheCapacityIsTwo(t *testing.T) {
	t.Parallel()
	w := installWith(t, func(c *agenda.Config) { c.Capacity = 2 })
	ctx := context.Background()
	start := firstSlot(t, w, member("acme", "user-ana"), "instalacao", wednesday)

	var wg sync.WaitGroup
	results := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := w.bookings.Book(ctx, member(fmt.Sprintf("space-%d", i), "user"), agenda.BookInput{Service: "instalacao", Start: start})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	won := 0
	for err := range results {
		if err == nil {
			won++
		} else if !errors.Is(err, agenda.ErrSlotTaken) {
			t.Errorf("a booking failed for another reason: %v", err)
		}
	}
	if won != 2 {
		t.Fatalf("%d people got a place, want 2", won)
	}
}

func TestAHoldThatLapsedFreesTheSlotBeforeAnyTaskRuns(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana, gil := member("acme", "user-ana"), member("globex", "user-gil")
	start := firstSlot(t, w, ana, "reconfiguracao", thursday)
	first := book(t, w, ana, "reconfiguracao", start)

	// While the hold lasts, nobody else takes the slot.
	_, err := w.bookings.Book(ctx, gil, agenda.BookInput{Service: "reconfiguracao", Start: start})
	requireErr(t, err, agenda.ErrSlotTaken, "a slot held for a payment")

	// Past the hold, it is free for another space, though nothing marked the
	// first booking as expired yet.
	w.clock.advance(24*time.Hour + time.Minute)
	second := book(t, w, gil, "reconfiguracao", start)
	if second.Status != agenda.StatusAwaitingPayment {
		t.Fatalf("the second booking is %s", second.Status)
	}

	// The task keeps the statuses honest and tells the customer, once.
	n, err := w.module.ExpireHolds(ctx)
	if err != nil || n != 1 {
		t.Fatalf("ExpireHolds expired %d (%v), want 1", n, err)
	}
	if n, err := w.module.ExpireHolds(ctx); err != nil || n != 0 {
		t.Fatalf("a second run expired %d (%v), want none", n, err)
	}
	shown, err := w.bookings.Booking(ctx, ana, first.ID)
	if err != nil || shown.Status != agenda.StatusExpired {
		t.Fatalf("the first booking is %s (%v)", shown.Status, err)
	}
	expired := w.notices.of(agenda.EventHoldExpired)
	if len(expired) != 1 || expired[0].Booking.ID != first.ID || expired[0].By != agenda.BySystem || expired[0].ForTeam() {
		t.Fatalf("the notifier was told %+v", expired)
	}
	if got := lastEntry(t, w, "acme", "agenda.hold_expired"); got.Causer != "system" || got.Subject != first.ID {
		t.Errorf("the log says %+v", got)
	}
	// A booking that still holds is not touched.
	if shown, _ := w.bookings.Booking(ctx, gil, second.ID); shown.Status != agenda.StatusAwaitingPayment {
		t.Errorf("a booking inside its hold became %s", shown.Status)
	}
}

func TestACustomerReschedulesAndCancelsUntilTheWindowCloses(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")

	// Thursday 10:00 is the start; the window closes at 08:00.
	start := local(2026, time.November, 12, 10, 0)
	paid := book(t, w, ana, "reconfiguracao", start)
	if _, err := w.app.Settle(ctx, security.SystemGrant(agenda.AgendaSettle, "acme"), paid.ID, "pix-1"); err != nil {
		t.Fatal(err)
	}
	cancelled := book(t, w, ana, "instalacao", local(2026, time.November, 12, 14, 0))

	// An hour of the day before, a reschedule keeps the payment and the code.
	moved, err := w.bookings.Reschedule(ctx, ana, paid.ID, local(2026, time.November, 13, 10, 0).UTC())
	if err != nil {
		t.Fatalf("rescheduling: %v", err)
	}
	if moved.Reschedules != 1 || moved.Code != paid.Code || moved.PaymentRef != "pix-1" || moved.Status != agenda.StatusScheduled ||
		!moved.StartsAt.Equal(local(2026, time.November, 13, 10, 0)) || !moved.EndsAt.Equal(local(2026, time.November, 13, 10, 40)) {
		t.Fatalf("the rescheduled booking is %+v", moved)
	}
	events := w.notices.of(agenda.EventRescheduled)
	if len(events) != 1 || events[0].By != agenda.ByCustomer || !events[0].ForTeam() {
		t.Fatalf("the notifier was told %+v", events)
	}
	// The old slot is free again, and the new one is not.
	if got, _ := w.bookings.Slots(ctx, ana, "instalacao", thursday); !contains(clocks(got), "10:00") {
		t.Errorf("the slot the booking left is not free: %v", clocks(got))
	}
	if got, _ := w.bookings.Slots(ctx, ana, "instalacao", time.Date(2026, time.November, 13, 0, 0, 0, 0, time.UTC)); contains(clocks(got), "10:00") {
		t.Error("the slot the booking moved to is offered")
	}
	// Moving onto a time somebody holds is refused.
	_, err = w.bookings.Reschedule(ctx, ana, paid.ID, local(2026, time.November, 12, 14, 0).UTC())
	requireErr(t, err, agenda.ErrSlotTaken, "rescheduling onto a taken time")
	if shown, _ := w.bookings.Booking(ctx, ana, paid.ID); shown.Reschedules != 1 || !shown.StartsAt.Equal(local(2026, time.November, 13, 10, 0)) {
		t.Errorf("a refused move changed the booking: %+v", shown)
	}

	// The window closes exactly two hours before the start. For the booking
	// that now starts Thursday 14:00 that is 12:00.
	w.clock.set(local(2026, time.November, 12, 12, 0).UTC())
	if _, err := w.bookings.Cancel(ctx, ana, cancelled.ID, "Mudei de ideia"); err != nil {
		t.Fatalf("cancelling two hours before: %v", err)
	}
	got, err := w.bookings.Booking(ctx, ana, cancelled.ID)
	if err != nil || got.Status != agenda.StatusCancelled || got.CancelReason != "Mudei de ideia" || got.CancelledBy != agenda.ByCustomer {
		t.Fatalf("the cancelled booking is %+v (%v)", got, err)
	}
	if events := w.notices.of(agenda.EventCancelled); len(events) != 1 || events[0].By != agenda.ByCustomer || !events[0].ForTeam() {
		t.Fatalf("the notifier was told %+v", events)
	}
	_, err = w.bookings.Cancel(ctx, ana, cancelled.ID, "")
	var rejected validation.Errors
	if !errors.As(err, &rejected) {
		t.Fatalf("cancelling a cancelled booking returned %v", err)
	}

	// A minute later the window is closed for a booking starting at 14:00.
	late := book(t, w, ana, "instalacao", local(2026, time.November, 13, 14, 0))
	w.clock.set(local(2026, time.November, 13, 12, 1).UTC())
	for name, act := range map[string]func() error{
		"cancelling": func() error { _, err := w.bookings.Cancel(ctx, ana, late.ID, ""); return err },
		"rescheduling": func() error {
			_, err := w.bookings.Reschedule(ctx, ana, late.ID, local(2026, time.November, 16, 10, 0).UTC())
			return err
		},
	} {
		err := act()
		if !errors.As(err, &rejected) || len(rejected["start"]) == 0 {
			t.Errorf("%s after the window returned %v", name, err)
		}
	}
	if shown, _ := w.bookings.Booking(ctx, ana, late.ID); shown.Status != agenda.StatusScheduled || shown.Reschedules != 0 {
		t.Errorf("a refused change touched the booking: %+v", shown)
	}
	// The team is not held to the window.
	if _, err := w.team.Cancel(ctx, agent("team-1"), late.ID, "Técnico indisponível"); err != nil {
		t.Fatalf("the team cancelling inside the window: %v", err)
	}
}

func TestTheCustomerConfirmsAndTheReminderComesOnceBeforeTheStart(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	scheduled := book(t, w, ana, "instalacao", local(2026, time.November, 11, 9, 0))
	confirmed := book(t, w, ana, "instalacao", local(2026, time.November, 11, 9, 40))
	waiting := book(t, w, ana, "reconfiguracao", local(2026, time.November, 11, 10, 20))
	if _, err := w.bookings.Confirm(ctx, ana, confirmed.ID); err != nil {
		t.Fatalf("confirming: %v", err)
	}

	// More than three hours before the first start, nothing is due.
	w.clock.set(local(2026, time.November, 11, 5, 59).UTC())
	if n, err := w.module.Remind(ctx); err != nil || n != 0 {
		t.Fatalf("Remind sent %d (%v) too early", n, err)
	}
	// Three hours before 09:00, the reminder of the booking nobody confirmed. The
	// confirmed one, and the one still waiting for a payment, get none.
	w.clock.set(local(2026, time.November, 11, 6, 0).UTC())
	n, err := w.module.Remind(ctx)
	if err != nil || n != 1 {
		t.Fatalf("Remind sent %d (%v), want 1", n, err)
	}
	reminders := w.notices.of(agenda.EventReminder)
	if len(reminders) != 1 || reminders[0].Booking.ID != scheduled.ID {
		t.Fatalf("the reminders are %+v", reminders)
	}
	if want := "/agenda/" + scheduled.ID; reminders[0].ConfirmURL != want || reminders[0].RescheduleURL != want+"/rescheduling" {
		t.Errorf("the reminder links %q and %q", reminders[0].ConfirmURL, reminders[0].RescheduleURL)
	}
	if reminders[0].By != agenda.BySystem || reminders[0].ForTeam() {
		t.Errorf("the reminder is for the team or comes from %q", reminders[0].By)
	}
	// Never twice, however often the scheduler runs.
	for i := 0; i < 3; i++ {
		if n, err := w.module.Remind(ctx); err != nil || n != 0 {
			t.Fatalf("a repeated run sent %d (%v)", n, err)
		}
	}
	if len(w.notices.of(agenda.EventReminder)) != 1 {
		t.Fatalf("the reminder went %d times", len(w.notices.of(agenda.EventReminder)))
	}
	if got := lastEntry(t, w, "acme", "agenda.reminded"); got.Subject != scheduled.ID || got.Causer != "system" {
		t.Errorf("the log says %+v", got)
	}
	if n := countEvents(entries(t, w, "acme"), "agenda.reminded"); n != 1 {
		t.Errorf("the log holds %d reminders", n)
	}

	// The customer answers the reminder: confirms. A confirmed booking cannot be
	// confirmed again, and a start that has passed cannot be confirmed.
	got, err := w.bookings.Confirm(ctx, ana, scheduled.ID)
	if err != nil || got.Status != agenda.StatusConfirmed || got.ConfirmedAt == nil {
		t.Fatalf("confirming: %+v, %v", got, err)
	}
	var rejected validation.Errors
	if _, err := w.bookings.Confirm(ctx, ana, scheduled.ID); !errors.As(err, &rejected) {
		t.Errorf("confirming twice returned %v", err)
	}
	if _, err := w.bookings.Confirm(ctx, ana, waiting.ID); !errors.As(err, &rejected) {
		t.Errorf("confirming a booking that waits for a payment returned %v", err)
	}
	if events := w.notices.of(agenda.EventConfirmed); len(events) != 2 || !events[0].Kind.ForTeam() || events[0].By != agenda.ByCustomer {
		t.Fatalf("the notifier was told %+v", events)
	}
}

func countEvents(rows []entry, event string) int {
	n := 0
	for _, e := range rows {
		if e.Event == event {
			n++
		}
	}
	return n
}

func TestAReschedulingStartsTheReminderOver(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	b := book(t, w, ana, "instalacao", local(2026, time.November, 11, 9, 0))

	w.clock.set(local(2026, time.November, 11, 6, 0).UTC())
	if n, _ := w.module.Remind(ctx); n != 1 {
		t.Fatalf("the first reminder: %d", n)
	}
	if _, err := w.bookings.Reschedule(ctx, ana, b.ID, local(2026, time.November, 13, 9, 0).UTC()); err != nil {
		t.Fatalf("rescheduling: %v", err)
	}
	if n, _ := w.module.Remind(ctx); n != 0 {
		t.Fatalf("a reminder went for a start days away: %d", n)
	}
	w.clock.set(local(2026, time.November, 13, 6, 30).UTC())
	if n, _ := w.module.Remind(ctx); n != 1 {
		t.Fatalf("the booking was not reminded of its new time: %d", n)
	}
	if got := w.notices.of(agenda.EventReminder); len(got) != 2 {
		t.Errorf("%d reminders went", len(got))
	}
}

func TestASpaceNeverSeesAnotherSpacesBooking(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana, bia, boss, gil := member("acme", "user-ana"), member("acme", "user-bia"), owner("acme", "user-boss"), member("globex", "user-gil")
	b := book(t, w, ana, "instalacao", local(2026, time.November, 11, 9, 0))
	book(t, w, bia, "instalacao", local(2026, time.November, 11, 9, 40))
	book(t, w, gil, "instalacao", local(2026, time.November, 11, 10, 20))

	// Another space: not found, for every use case, whatever the identifier.
	for name, act := range map[string]func() error{
		"Booking": func() error { _, err := w.bookings.Booking(ctx, gil, b.ID); return err },
		"Confirm": func() error { _, err := w.bookings.Confirm(ctx, gil, b.ID); return err },
		"Reschedule": func() error {
			_, err := w.bookings.Reschedule(ctx, gil, b.ID, local(2026, time.November, 13, 9, 0))
			return err
		},
		"Cancel": func() error { _, err := w.bookings.Cancel(ctx, gil, b.ID, ""); return err },
	} {
		requireErr(t, act(), agenda.ErrNotFound, name+" of another space's booking")
	}
	// The same space, another person's booking: not found for one who reads only their own.
	requireErr(t, second(w.bookings.Booking(ctx, bia, b.ID)), agenda.ErrNotFound, "Booking of somebody else's")
	requireErr(t, second(w.bookings.Confirm(ctx, bia, b.ID)), agenda.ErrNotFound, "Confirm of somebody else's")
	// Whoever reads the space reads it all, and only it.
	if _, err := w.bookings.Booking(ctx, boss, b.ID); err != nil {
		t.Fatalf("the owner reading the space's booking: %v", err)
	}
	page, err := w.bookings.Bookings(ctx, boss, agenda.ListQuery{})
	if err != nil || page.Total != 2 {
		t.Fatalf("the owner's list holds %d bookings (%v), want the two of the space", page.Total, err)
	}
	page, err = w.bookings.Bookings(ctx, ana, agenda.ListQuery{})
	if err != nil || page.Total != 1 || page.Rows[0].ID != b.ID {
		t.Fatalf("the member's list holds %d bookings (%v), want their own", page.Total, err)
	}
	page, _ = w.bookings.Bookings(ctx, gil, agenda.ListQuery{})
	if page.Total != 1 || page.Rows[0].Tenant != "globex" {
		t.Fatalf("the other space's list: %+v", page)
	}
	// The application's hooks are scoped by the Grant they are given.
	grant := security.SystemGrant(agenda.AgendaSettle, "globex")
	requireErr(t, second(w.app.Find(ctx, grant, b.ID)), agenda.ErrNotFound, "Find of another space's booking by id")
	requireErr(t, second(w.app.Find(ctx, grant, b.Code)), agenda.ErrNotFound, "Find of another space's booking by code")
	requireErr(t, second(w.app.Settle(ctx, grant, b.ID, "pix-1")), agenda.ErrNotFound, "Settle of another space's booking")
}

func second[T any](_ T, err error) error { return err }

func TestTheListIsPagedSearchedFilteredAndOrderedOnTheServer(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	for i, slot := range []time.Time{local(2026, time.November, 11, 9, 0), local(2026, time.November, 11, 9, 40), local(2026, time.November, 11, 10, 20), local(2026, time.November, 11, 11, 0)} {
		service := "instalacao"
		if i%2 == 1 {
			service = "reconfiguracao"
		}
		book(t, w, ana, service, slot)
	}

	page, err := w.bookings.Bookings(ctx, ana, agenda.ListQuery{PerPage: 3})
	if err != nil || page.Total != 4 || len(page.Rows) != 3 || page.Pages != 2 {
		t.Fatalf("the first page: %+v, %v", page, err)
	}
	// Latest start first, until somebody orders it otherwise.
	if !page.Rows[0].StartsAt.After(page.Rows[1].StartsAt) {
		t.Errorf("the default order is not the latest start first")
	}
	asc, _ := w.bookings.Bookings(ctx, ana, agenda.ListQuery{Sort: "starts_at", Dir: "asc", PerPage: 3})
	if !asc.Rows[0].StartsAt.Before(asc.Rows[1].StartsAt) {
		t.Errorf("ascending order was not honoured")
	}
	if page2, _ := w.bookings.Bookings(ctx, ana, agenda.ListQuery{PerPage: 3, Page: 2}); len(page2.Rows) != 1 || page2.Page != 2 {
		t.Errorf("the second page: %+v", page2)
	}
	if found, _ := w.bookings.Bookings(ctx, ana, agenda.ListQuery{Search: "AG-000002"}); found.Total != 1 || found.Rows[0].Code != "AG-000002" {
		t.Errorf("searching by protocol: %+v", found)
	}
	if found, _ := w.bookings.Bookings(ctx, ana, agenda.ListQuery{Search: "reconfig"}); found.Total != 2 {
		t.Errorf("searching by service: %+v", found)
	}
	if found, _ := w.bookings.Bookings(ctx, ana, agenda.ListQuery{Statuses: []agenda.Status{agenda.StatusAwaitingPayment}}); found.Total != 2 {
		t.Errorf("filtering by status: %+v", found)
	}
	// A column that is not on the allowlist, and a status that is not one, are refused.
	var rejected validation.Errors
	if _, err := w.bookings.Bookings(ctx, ana, agenda.ListQuery{Sort: "tenant_id; DROP TABLE agenda_bookings"}); !errors.As(err, &rejected) {
		t.Errorf("an unknown sort was accepted: %v", err)
	}
	if _, err := w.bookings.Bookings(ctx, ana, agenda.ListQuery{Statuses: []agenda.Status{"bogus"}}); !errors.As(err, &rejected) {
		t.Errorf("an unknown status was accepted: %v", err)
	}
}

func TestWhatTheTeamWroteIsNotWhatTheCustomerReads(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	b := book(t, w, ana, "reconfiguracao", local(2026, time.November, 11, 9, 0))
	rita := agent("team-1")
	if _, err := w.team.Release(ctx, rita, b.ID, "Cliente antigo, cortesia"); err != nil {
		t.Fatal(err)
	}
	w.clock.set(local(2026, time.November, 11, 10, 0).UTC())
	if _, err := w.team.Complete(ctx, rita, b.ID, "Ângulo trocado, sem custo"); err != nil {
		t.Fatal(err)
	}

	mine, err := w.bookings.Booking(ctx, ana, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mine.TeamNote != "" || mine.ReleasedBy != "" || mine.ReleasedReason != "" || mine.CompletedBy != "" || mine.RemindedAt != nil {
		t.Errorf("the customer's booking carries the team's side: %+v", mine)
	}
	if page, _ := w.bookings.Bookings(ctx, ana, agenda.ListQuery{}); page.Rows[0].TeamNote != "" || page.Rows[0].ReleasedReason != "" {
		t.Errorf("the customer's list carries the team's side: %+v", page.Rows[0])
	}
	theirs, err := w.team.Booking(ctx, rita, b.ID)
	if err != nil || theirs.TeamNote != "Ângulo trocado, sem custo" || theirs.ReleasedReason != "Cliente antigo, cortesia" || theirs.CompletedByName != "Rita Prado" {
		t.Fatalf("the team's booking: %+v (%v)", theirs, err)
	}
}

func TestACustomerCanBookOnlyWhatTheApplicationConfirmsIsTheirs(t *testing.T) {
	t.Parallel()
	w := installWith(t, func(c *agenda.Config) {
		c.Subjects = subjects{"acme/camera/42": "Portaria (a do cadastro)"}
	})
	ctx := context.Background()
	ana := member("acme", "user-ana")
	start := firstSlot(t, w, ana, "instalacao", wednesday)

	// The label comes from the application, not from the request.
	b, err := w.bookings.Book(ctx, ana, agenda.BookInput{Service: "instalacao", Start: start, Subject: agenda.BookingSubject{Type: "camera", ID: "42", Label: "Qualquer coisa"}})
	if err != nil || b.SubjectLabel != "Portaria (a do cadastro)" {
		t.Fatalf("booking a camera of the space: %+v, %v", b, err)
	}
	// Another space's camera is refused.
	_, err = w.bookings.Book(ctx, member("globex", "user-gil"), agenda.BookInput{
		Service: "instalacao", Start: firstSlot(t, w, ana, "instalacao", thursday), Subject: agenda.BookingSubject{Type: "camera", ID: "42"},
	})
	var rejected validation.Errors
	if !errors.As(err, &rejected) || len(rejected["subject"]) == 0 {
		t.Fatalf("booking another space's camera returned %v", err)
	}
	// A booking about nothing in particular needs no subject.
	if _, err := w.bookings.Book(ctx, ana, agenda.BookInput{Service: "instalacao", Start: firstSlot(t, w, ana, "instalacao", thursday)}); err != nil {
		t.Fatalf("booking with no subject: %v", err)
	}
}

func TestAFailingNotifierChangesNothingAndTheLogCarriesNoPersonalText(t *testing.T) {
	t.Parallel()
	w := install(t)
	w.notices.fail = errors.New("whatsapp: could not reach 5511999990000")
	ctx := context.Background()
	ana := member("acme", "user-ana")

	b := book(t, w, ana, "instalacao", firstSlot(t, w, ana, "instalacao", wednesday))
	if shown, err := w.bookings.Booking(ctx, ana, b.ID); err != nil || shown.Status != agenda.StatusScheduled {
		t.Fatalf("a failing notifier undid the booking: %+v, %v", shown, err)
	}
	logged := w.logs.String()
	if !strings.Contains(logged, "notifier failed") || !strings.Contains(logged, b.ID) || !strings.Contains(logged, "agenda.booked") {
		t.Fatalf("the failure was not logged: %q", logged)
	}
	if strings.Contains(logged, "5511999990000") || strings.Contains(logged, "Ana Souza") || strings.Contains(logged, "A imagem") {
		t.Fatalf("the log carries personal text: %q", logged)
	}
}

func TestTheActivityLogRecordsEveryWriteWithoutWhatAPersonTyped(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	b := book(t, w, ana, "reconfiguracao", firstSlot(t, w, ana, "reconfiguracao", wednesday))

	if got := lastEntry(t, w, "acme", "agenda.booked"); got.Causer != "user-ana" || got.Subject != b.ID || !strings.Contains(got.Description, "AG-000001") || !strings.Contains(got.Properties, "AG-000001") {
		t.Fatalf("booked: %+v", got)
	}
	if _, err := w.bookings.Reschedule(ctx, ana, b.ID, local(2026, time.November, 13, 9, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.bookings.Cancel(ctx, ana, b.ID, "Meu segredo: cancelei por causa do João"); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"agenda.rescheduled", "agenda.cancelled"} {
		if got := lastEntry(t, w, "acme", event); got.Causer != "user-ana" {
			t.Errorf("%s: the causer is %q", event, got.Causer)
		}
	}
	for _, e := range entries(t, w, "acme") {
		for _, typed := range []string{"A imagem está escura", "segredo", "João", "Portaria", "Ana Souza"} {
			if strings.Contains(e.Properties, typed) || strings.Contains(e.Description, typed) {
				t.Errorf("the entry %s carries what a person typed (%q): %+v", e.Event, typed, e)
			}
		}
	}
}
