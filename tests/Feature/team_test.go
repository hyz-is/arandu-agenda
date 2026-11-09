package feature_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"

	agenda "github.com/hyz-is/arandu-agenda"
)

func TestTheTeamListsEverySpacesBookingsAndFiltersThem(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	rita := agent("team-1")
	ana, gil := member("acme", "user-ana"), member("globex", "user-gil")
	paid := book(t, w, ana, "reconfiguracao", local(2026, time.November, 11, 9, 0))
	free := book(t, w, ana, "instalacao", local(2026, time.November, 11, 9, 40))
	other := book(t, w, gil, "instalacao", local(2026, time.November, 12, 10, 0))

	page, err := w.team.Bookings(ctx, rita, agenda.TeamQuery{})
	if err != nil || page.Total != 3 || len(page.Rows) != 3 {
		t.Fatalf("the agenda holds %d bookings (%v), want the three of both spaces", page.Total, err)
	}
	tenants := map[string]bool{}
	for _, row := range page.Rows {
		tenants[row.Tenant] = true
		// The list carries what the team needs to read a line, and not what is the
		// team's own note.
		if row.TeamNote != "" || row.CustomerNote != "" {
			t.Errorf("the list carries a note: %+v", row)
		}
	}
	if !tenants["acme"] || !tenants["globex"] {
		t.Errorf("the list reads %v", tenants)
	}

	for name, c := range map[string]struct {
		query agenda.TeamQuery
		want  []string
	}{
		"by status":        {agenda.TeamQuery{Statuses: []agenda.Status{agenda.StatusAwaitingPayment}}, []string{paid.Code}},
		"by day":           {agenda.TeamQuery{Day: "2026-11-12"}, []string{other.Code}},
		"by day, two":      {agenda.TeamQuery{Day: "2026-11-11", Sort: "code", Dir: "asc"}, []string{paid.Code, free.Code}},
		"by space":         {agenda.TeamQuery{Tenant: "globex"}, []string{other.Code}},
		"by code":          {agenda.TeamQuery{Search: free.Code}, []string{free.Code}},
		"by requester":     {agenda.TeamQuery{Search: "Gil"}, []string{other.Code}},
		"by service":       {agenda.TeamQuery{Search: "Reconfig"}, []string{paid.Code}},
		"by subject":       {agenda.TeamQuery{Search: "Portaria", Sort: "code", Dir: "asc"}, []string{paid.Code, free.Code, other.Code}},
		"nothing matching": {agenda.TeamQuery{Search: "zzz"}, nil},
	} {
		got, err := w.team.Bookings(ctx, rita, c.query)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		var codes []string
		for _, row := range got.Rows {
			codes = append(codes, row.Code)
		}
		if strings.Join(codes, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: got %v, want %v", name, codes, c.want)
		}
	}
	// A day is the team's day in its zone: 23:30 on Wednesday in Sao Paulo is
	// already Thursday in UTC.
	late := book(t, w, gil, "instalacao", local(2026, time.November, 11, 17, 0))
	if got, _ := w.team.Bookings(ctx, rita, agenda.TeamQuery{Day: "2026-11-11", Search: late.Code}); got.Total != 1 {
		t.Errorf("a booking at 17:00 on Wednesday is not on Wednesday: %+v", got)
	}

	var rejected validation.Errors
	for name, q := range map[string]agenda.TeamQuery{
		"an unknown sort": {Sort: "tenant_id; --"}, "an unknown status": {Statuses: []agenda.Status{"x"}},
		"a day that is not": {Day: "ontem"}, "a space that is not": {Tenant: "No/Pe"},
	} {
		if _, err := w.team.Bookings(ctx, rita, q); !errors.As(err, &rejected) {
			t.Errorf("%s was accepted: %v", name, err)
		}
	}
}

func TestOnlyTheTeamActsOnTheTeamsSide(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	ana := member("acme", "user-ana")
	b := book(t, w, ana, "reconfiguracao", local(2026, time.November, 11, 9, 0))

	all := agent("team-1")
	impersonated := all
	impersonated.Impersonator = &security.Subject{ID: "operator"}
	for name, who := range map[string]security.Subject{
		"a customer":                         ana,
		"a customer with the team's actions": {ID: "user-ana", Tenant: "acme", Verified: true, Actions: agenda.TeamActions()},
		"a customer who owns the space":      owner("acme", "user-boss"),
		"a team member with no action":       {ID: "team-2", Tenant: realm, Verified: true, Roles: []string{"admin"}},
		"a team member acting as another":    impersonated,
		"a guest":                            security.Guest(realm),
	} {
		for use, act := range map[string]func() error{
			"Bookings": func() error { _, err := w.team.Bookings(ctx, who, agenda.TeamQuery{}); return err },
			"Booking":  func() error { _, err := w.team.Booking(ctx, who, b.ID); return err },
			"Release":  func() error { _, err := w.team.Release(ctx, who, b.ID, "cortesia"); return err },
			"Complete": func() error { _, err := w.team.Complete(ctx, who, b.ID, ""); return err },
			"Cancel":   func() error { _, err := w.team.Cancel(ctx, who, b.ID, ""); return err },
			"Reschedule": func() error {
				_, err := w.team.Reschedule(ctx, who, b.ID, local(2026, time.November, 12, 9, 0))
				return err
			},
			"Services": func() error { _, err := w.team.Services(ctx, who, agenda.ConfigQuery{}); return err },
			"CreateService": func() error {
				_, err := w.team.CreateService(ctx, who, agenda.ServiceInput{Key: "x", Name: "X", DurationMinutes: 30})
				return err
			},
			"CreateHour": func() error {
				_, err := w.team.CreateHour(ctx, who, agenda.HourInput{Weekday: 6, StartMinute: 540, EndMinute: 600})
				return err
			},
			"CreateClosure": func() error {
				_, err := w.team.CreateClosure(ctx, who, agenda.ClosureInput{StartsAt: local(2026, time.December, 1, 0, 0), EndsAt: local(2026, time.December, 2, 0, 0)})
				return err
			},
		} {
			if err := act(); !errors.Is(err, security.ErrForbidden) {
				t.Errorf("%s was allowed %s: %v", name, use, err)
			}
		}
	}
	// A team member holds only the actions they were given: reading is not managing.
	reader := security.Subject{ID: "team-3", Tenant: realm, Verified: true, Actions: []security.Action{agenda.AgendaViewAll}}
	if _, err := w.team.Booking(ctx, reader, b.ID); err != nil {
		t.Fatalf("a reader reading: %v", err)
	}
	for use, act := range map[string]func() error{
		"Release": func() error { _, err := w.team.Release(ctx, reader, b.ID, "cortesia"); return err },
		"CreateService": func() error {
			_, err := w.team.CreateService(ctx, reader, agenda.ServiceInput{Key: "x", Name: "X", DurationMinutes: 30})
			return err
		},
	} {
		if err := act(); !errors.Is(err, security.ErrForbidden) {
			t.Errorf("a reader was allowed %s: %v", use, err)
		}
	}
	if shown, _ := w.bookings.Booking(ctx, ana, b.ID); shown.Status != agenda.StatusAwaitingPayment {
		t.Fatalf("a refused act changed the booking: %s", shown.Status)
	}
}

func TestTheTeamReleasesABookingAsACourtesyAndItIsLoggedWithWhoDidIt(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	rita := agent("team-1")
	ana := member("acme", "user-ana")
	b := book(t, w, ana, "reconfiguracao", local(2026, time.November, 11, 9, 0))

	var rejected validation.Errors
	if _, err := w.team.Release(ctx, rita, b.ID, "  "); !errors.As(err, &rejected) || len(rejected["reason"]) == 0 {
		t.Fatalf("a courtesy with no reason returned %v", err)
	}
	released, err := w.team.Release(ctx, rita, b.ID, "Cliente antigo")
	if err != nil {
		t.Fatalf("releasing: %v", err)
	}
	if released.Status != agenda.StatusScheduled || released.HoldUntil != nil || released.PaymentRef != "" || released.ReleasedBy != "team-1" || released.ReleasedReason != "Cliente antigo" {
		t.Fatalf("the released booking is %+v", released)
	}
	events := w.notices.of(agenda.EventScheduled)
	if len(events) != 1 || events[0].By != agenda.ByTeam || events[0].Booking.ID != b.ID {
		t.Fatalf("the notifier was told %+v", events)
	}
	// The act is in the customer's space log, with the team member as the causer.
	got := lastEntry(t, w, "acme", "agenda.released")
	if got.Causer != "team-1" || got.Subject != b.ID || strings.Contains(got.Properties, "Cliente antigo") {
		t.Errorf("the log says %+v", got)
	}
	// A booking that holds its slot cannot be released again.
	if _, err := w.team.Release(ctx, rita, b.ID, "Outra vez"); !errors.As(err, &rejected) {
		t.Errorf("releasing twice returned %v", err)
	}
	// And the slot is held now: nobody else takes it.
	_, err = w.bookings.Book(ctx, member("globex", "user-gil"), agenda.BookInput{Service: "instalacao", Start: released.StartsAt})
	requireErr(t, err, agenda.ErrSlotTaken, "booking a released slot")
}

func TestACourtesyAfterTheHoldNeedsTheSlotStillFree(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	rita := agent("team-1")
	ana, gil := member("acme", "user-ana"), member("globex", "user-gil")
	start := firstSlot(t, w, ana, "reconfiguracao", thursday)
	b := book(t, w, ana, "reconfiguracao", start)
	w.clock.advance(25 * time.Hour)
	if n, _ := w.module.ExpireHolds(ctx); n != 1 {
		t.Fatalf("expired %d", n)
	}

	if _, err := w.team.Release(ctx, rita, b.ID, "Cortesia depois do prazo"); err != nil {
		t.Fatalf("releasing a lapsed booking whose slot is free: %v", err)
	}
	if shown, _ := w.bookings.Booking(ctx, ana, b.ID); shown.Status != agenda.StatusScheduled {
		t.Fatalf("the booking is %s", shown.Status)
	}

	taken := book(t, w, ana, "reconfiguracao", local(2026, time.November, 13, 14, 0))
	w.clock.advance(25 * time.Hour)
	book(t, w, gil, "instalacao", taken.StartsAt)
	requireErr(t, second(w.team.Release(ctx, rita, taken.ID, "Cortesia")), agenda.ErrSlotTaken, "releasing a lapsed booking whose slot was taken")
}

func TestTheTeamCompletesMarksAbsencesCancelsAndReschedules(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	rita := agent("team-1")
	ana := member("acme", "user-ana")
	done := book(t, w, ana, "instalacao", local(2026, time.November, 11, 9, 0))
	absent := book(t, w, ana, "instalacao", local(2026, time.November, 11, 9, 40))
	cancelled := book(t, w, ana, "instalacao", local(2026, time.November, 11, 10, 20))
	moved := book(t, w, ana, "instalacao", local(2026, time.November, 11, 11, 0))

	// Complete, with a note the customer never reads.
	var rejected validation.Errors
	if _, err := w.team.Complete(ctx, rita, done.ID, strings.Repeat("x", 600)); !errors.As(err, &rejected) {
		t.Errorf("a note too long was accepted: %v", err)
	}
	c, err := w.team.Complete(ctx, rita, done.ID, "Câmera reposicionada.")
	if err != nil || c.Status != agenda.StatusCompleted || c.CompletedAt == nil || c.CompletedBy != "team-1" || c.TeamNote != "Câmera reposicionada." {
		t.Fatalf("completing: %+v, %v", c, err)
	}
	got := lastEntry(t, w, "acme", "agenda.completed")
	if got.Causer != "team-1" || got.Subject != done.ID || strings.Contains(got.Properties, "reposicionada") {
		t.Errorf("the log says %+v", got)
	}
	if events := w.notices.of(agenda.EventCompleted); len(events) != 1 || events[0].By != agenda.ByTeam {
		t.Errorf("the notifier was told %+v", events)
	}
	if _, err := w.team.Complete(ctx, rita, done.ID, ""); !errors.As(err, &rejected) {
		t.Errorf("completing twice returned %v", err)
	}
	// A completed booking holds its slot no more, and no customer moves it.
	if _, err := w.bookings.Cancel(ctx, ana, done.ID, ""); !errors.As(err, &rejected) {
		t.Errorf("a customer cancelled a booking that was done: %v", err)
	}

	// An absence only once the start has passed.
	if _, err := w.team.NoShow(ctx, rita, absent.ID); !errors.As(err, &rejected) {
		t.Errorf("an absence before the start was accepted: %v", err)
	}
	w.clock.set(local(2026, time.November, 11, 9, 41).UTC())
	n, err := w.team.NoShow(ctx, rita, absent.ID)
	if err != nil || n.Status != agenda.StatusNoShow {
		t.Fatalf("marking an absence: %+v, %v", n, err)
	}
	if got := lastEntry(t, w, "acme", "agenda.no_show"); got.Causer != "team-1" {
		t.Errorf("the log says %+v", got)
	}

	// Cancel, with the customer told.
	x, err := w.team.Cancel(ctx, rita, cancelled.ID, "Técnico doente")
	if err != nil || x.Status != agenda.StatusCancelled || x.CancelledBy != agenda.ByTeam || x.CancelReason != "Técnico doente" {
		t.Fatalf("cancelling: %+v, %v", x, err)
	}
	events := w.notices.of(agenda.EventCancelled)
	if len(events) != 1 || events[0].By != agenda.ByTeam || events[0].ForTeam() {
		t.Fatalf("the notifier was told %+v", events)
	}

	// Reschedule: the team may book outside the hours, and not into a closure or
	// onto a full time.
	sat := local(2026, time.November, 14, 10, 0)
	m, err := w.team.Reschedule(ctx, rita, moved.ID, sat.UTC())
	if err != nil || !m.StartsAt.Equal(sat) || m.Reschedules != 1 {
		t.Fatalf("rescheduling to a Saturday: %+v, %v", m, err)
	}
	if got := lastEntry(t, w, "acme", "agenda.rescheduled"); got.Causer != "team-1" {
		t.Errorf("the log says %+v", got)
	}
	if events := w.notices.of(agenda.EventRescheduled); len(events) != 1 || events[0].By != agenda.ByTeam || events[0].ForTeam() {
		t.Errorf("the notifier was told %+v", events)
	}
	if _, err := w.team.CreateClosure(ctx, rita, agenda.ClosureInput{StartsAt: local(2026, time.November, 16, 0, 0), EndsAt: local(2026, time.November, 17, 0, 0), Reason: "Feriado"}); err != nil {
		t.Fatal(err)
	}
	requireErr(t, second(w.team.Reschedule(ctx, rita, moved.ID, local(2026, time.November, 16, 10, 0).UTC())), agenda.ErrSlotTaken, "moving into a closure")
	busy := book(t, w, ana, "instalacao", local(2026, time.November, 18, 9, 0))
	requireErr(t, second(w.team.Reschedule(ctx, rita, moved.ID, busy.StartsAt)), agenda.ErrSlotTaken, "moving onto a full time")
	requireErr(t, second(w.team.Reschedule(ctx, rita, moved.ID, local(2026, time.November, 9, 8, 0).UTC())), agenda.ErrSlotTaken, "moving into the past")
}

func TestTheTeamActsInTheSpaceOfTheBookingAndNeverElsewhere(t *testing.T) {
	t.Parallel()
	w := install(t)
	ctx := context.Background()
	rita := agent("team-1")
	b := book(t, w, member("globex", "user-gil"), "instalacao", local(2026, time.November, 11, 9, 0))
	requireErr(t, second(w.team.Booking(ctx, rita, "nobody")), agenda.ErrNotFound, "a booking that does not exist")

	if _, err := w.team.Complete(ctx, rita, b.ID, ""); err != nil {
		t.Fatal(err)
	}
	// The entry is in the space the booking is in, and nowhere else.
	if got := events(t, w, "globex"); len(got) != 2 || got[0] != "agenda.booked" || got[1] != "agenda.completed" {
		t.Errorf("the space's log is %v", got)
	}
	for _, e := range entries(t, w, "platform") {
		if e.Subject == b.ID {
			t.Errorf("the team's own log holds the customer's booking: %+v", e)
		}
	}
	if got := events(t, w, "acme"); len(got) != 0 {
		t.Errorf("another space's log holds %v", got)
	}
}

func TestTheTeamKeepsItsServicesItsWeekAndItsClosures(t *testing.T) {
	t.Parallel()
	w := bare(t, nil)
	ctx := context.Background()
	rita := agent("team-1")
	var rejected validation.Errors

	// Services: validated, unique by key, changed, deleted.
	for name, in := range map[string]agenda.ServiceInput{
		"no key":           {Name: "X", DurationMinutes: 40},
		"a key with caps":  {Key: "Instalacao", Name: "X", DurationMinutes: 40},
		"a key with space": {Key: "a b", Name: "X", DurationMinutes: 40},
		"no name":          {Key: "x", DurationMinutes: 40},
		"too short":        {Key: "x", Name: "X", DurationMinutes: 4},
		"too long":         {Key: "x", Name: "X", DurationMinutes: 481},
	} {
		if _, err := w.team.CreateService(ctx, rita, in); !errors.As(err, &rejected) {
			t.Errorf("%s was accepted: %v", name, err)
		}
	}
	created, err := w.team.CreateService(ctx, rita, agenda.ServiceInput{Key: "reconfiguracao", Name: "Reconfiguração", DurationMinutes: 40, RequiresPayment: true, Active: true})
	if err != nil || created.Key != "reconfiguracao" {
		t.Fatalf("creating a service: %+v, %v", created, err)
	}
	if _, err := w.team.CreateService(ctx, rita, agenda.ServiceInput{Key: "reconfiguracao", Name: "Outra", DurationMinutes: 40}); !errors.As(err, &rejected) || len(rejected["key"]) == 0 {
		t.Errorf("a second service with the same key returned %v", err)
	}
	other, _ := w.team.CreateService(ctx, rita, agenda.ServiceInput{Key: "instalacao", Name: "Instalação", DurationMinutes: 60, Active: true})
	if _, err := w.team.UpdateService(ctx, rita, other.ID, agenda.ServiceInput{Key: "reconfiguracao", Name: "Instalação", DurationMinutes: 60}); !errors.As(err, &rejected) {
		t.Errorf("a key taken by another service was accepted: %v", err)
	}
	updated, err := w.team.UpdateService(ctx, rita, other.ID, agenda.ServiceInput{Key: "instalacao", Name: "Instalação completa", DurationMinutes: 90, RequiresPayment: true, Active: false})
	if err != nil || updated.Name != "Instalação completa" || updated.DurationMinutes != 90 || bool(updated.Active) || !bool(updated.RequiresPayment) {
		t.Fatalf("updating a service: %+v, %v", updated, err)
	}
	listing, err := w.team.Services(ctx, rita, agenda.ConfigQuery{Search: "completa"})
	if err != nil || listing.Total != 1 || listing.Rows[0].Key != "instalacao" || listing.Rows[0].Active {
		t.Fatalf("listing the services: %+v, %v", listing, err)
	}
	if all, _ := w.team.Services(ctx, rita, agenda.ConfigQuery{Sort: "duration", Dir: "desc"}); all.Total != 2 || all.Rows[0].Key != "instalacao" {
		t.Errorf("listing, ordered: %+v", all)
	}
	one, err := w.team.Service(ctx, rita, created.ID)
	if err != nil || one.Key != "reconfiguracao" {
		t.Fatalf("reading a service: %+v, %v", one, err)
	}
	if err := w.team.DeleteService(ctx, rita, other.ID); err != nil {
		t.Fatalf("deleting a service: %v", err)
	}
	requireErr(t, w.team.DeleteService(ctx, rita, other.ID), agenda.ErrNotFound, "deleting a service twice")
	requireErr(t, second(w.team.Service(ctx, rita, other.ID)), agenda.ErrNotFound, "reading a deleted service")
	if _, err := w.team.Services(ctx, rita, agenda.ConfigQuery{Sort: "key; --"}); !errors.As(err, &rejected) {
		t.Errorf("an unknown sort was accepted: %v", err)
	}

	// Hours: the weekday and the order are checked, and windows of a day do not overlap.
	for name, in := range map[string]agenda.HourInput{
		"a weekday that is not": {Weekday: 7, StartMinute: 540, EndMinute: 600},
		"a negative start":      {Weekday: 1, StartMinute: -1, EndMinute: 600},
		"an end past midnight":  {Weekday: 1, StartMinute: 540, EndMinute: 1441},
		"an end before start":   {Weekday: 1, StartMinute: 600, EndMinute: 600},
	} {
		if _, err := w.team.CreateHour(ctx, rita, in); !errors.As(err, &rejected) {
			t.Errorf("%s was accepted: %v", name, err)
		}
	}
	morning, err := w.team.CreateHour(ctx, rita, agenda.HourInput{Weekday: 1, StartMinute: 540, EndMinute: 720})
	if err != nil {
		t.Fatalf("creating a window: %v", err)
	}
	if _, err := w.team.CreateHour(ctx, rita, agenda.HourInput{Weekday: 1, StartMinute: 700, EndMinute: 800}); !errors.As(err, &rejected) {
		t.Errorf("an overlapping window was accepted: %v", err)
	}
	if _, err := w.team.CreateHour(ctx, rita, agenda.HourInput{Weekday: 1, StartMinute: 720, EndMinute: 800}); err != nil {
		t.Errorf("a window that starts as another ends was refused: %v", err)
	}
	if _, err := w.team.CreateHour(ctx, rita, agenda.HourInput{Weekday: 2, StartMinute: 540, EndMinute: 720}); err != nil {
		t.Errorf("the same window on another day was refused: %v", err)
	}
	hours, err := w.team.Hours(ctx, rita, agenda.ConfigQuery{PerPage: 2})
	if err != nil || hours.Total != 3 || len(hours.Rows) != 2 || hours.Pages != 2 || hours.Rows[0].Weekday != 1 || hours.Rows[0].StartMinute != 540 {
		t.Fatalf("listing the week: %+v, %v", hours, err)
	}
	if err := w.team.DeleteHour(ctx, rita, morning.ID); err != nil {
		t.Fatalf("deleting a window: %v", err)
	}
	requireErr(t, w.team.DeleteHour(ctx, rita, morning.ID), agenda.ErrNotFound, "deleting a window twice")

	// Closures.
	for name, in := range map[string]agenda.ClosureInput{
		"no start":      {EndsAt: local(2026, time.December, 25, 0, 0)},
		"no end":        {StartsAt: local(2026, time.December, 25, 0, 0)},
		"end before":    {StartsAt: local(2026, time.December, 25, 0, 0), EndsAt: local(2026, time.December, 24, 0, 0)},
		"a long reason": {StartsAt: local(2026, time.December, 25, 0, 0), EndsAt: local(2026, time.December, 26, 0, 0), Reason: strings.Repeat("x", 301)},
	} {
		if _, err := w.team.CreateClosure(ctx, rita, in); !errors.As(err, &rejected) {
			t.Errorf("%s was accepted: %v", name, err)
		}
	}
	closure, err := w.team.CreateClosure(ctx, rita, agenda.ClosureInput{StartsAt: local(2026, time.December, 25, 0, 0), EndsAt: local(2026, time.December, 26, 0, 0), Reason: "Natal"})
	if err != nil {
		t.Fatalf("creating a closure: %v", err)
	}
	closures, err := w.team.Closures(ctx, rita, agenda.ConfigQuery{Search: "nat"})
	if err != nil || closures.Total != 1 || closures.Rows[0].Reason != "Natal" || !closures.Rows[0].StartsAt.Equal(local(2026, time.December, 25, 0, 0)) {
		t.Fatalf("listing the closures: %+v, %v", closures, err)
	}
	if err := w.team.DeleteClosure(ctx, rita, closure.ID); err != nil {
		t.Fatalf("deleting a closure: %v", err)
	}
	requireErr(t, w.team.DeleteClosure(ctx, rita, closure.ID), agenda.ErrNotFound, "deleting a closure twice")

	// Every change is in the team's own log, with the member as the causer.
	got := events(t, w, realm)
	for _, want := range []string{"agenda.service_created", "agenda.service_updated", "agenda.service_deleted", "agenda.hour_created", "agenda.hour_deleted", "agenda.closure_created", "agenda.closure_deleted"} {
		if !contains(got, want) {
			t.Errorf("the team's log lacks %s: %v", want, got)
		}
	}
	for _, e := range entries(t, w, realm) {
		if e.Causer != "team-1" {
			t.Errorf("the entry %s has the causer %q", e.Event, e.Causer)
		}
	}
}
