package unit_test

import (
	"slices"
	"testing"
	"time"

	agenda "github.com/hyz-is/arandu-agenda"
)

// These tests hold the rules of the agenda as pure data: which times a day
// offers is a function of the schedule and of the moment it is asked, and
// nothing else. The zone is America/Sao_Paulo, which has had no daylight-saving
// time since 2019, so a day is always 24 hours and a time of day is the same
// offset from UTC all year.

func saoPaulo(t *testing.T) *time.Location {
	t.Helper()
	zone, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Skipf("the timezone database has no America/Sao_Paulo: %v", err)
	}
	return zone
}

// at is a local time in the zone.
func at(zone *time.Location, year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, zone)
}

// clocks are the starts as "15:04" in the zone.
func clocks(zone *time.Location, starts []time.Time) []string {
	out := make([]string, 0, len(starts))
	for _, start := range starts {
		out = append(out, start.In(zone).Format("15:04"))
	}
	return out
}

// workWeek is Monday to Friday, 09:00 to 12:00 and 14:00 to 18:00.
func workWeek() []agenda.Window {
	var out []agenda.Window
	for day := 1; day <= 5; day++ {
		out = append(out, agenda.Window{Weekday: day, StartMinute: 9 * 60, EndMinute: 12 * 60}, agenda.Window{Weekday: day, StartMinute: 14 * 60, EndMinute: 18 * 60})
	}
	return out
}

func TestADayOffersEveryStepWhereTheWholeServiceFits(t *testing.T) {
	t.Parallel()
	zone := saoPaulo(t)
	// A Monday, asked about the Friday before.
	now := at(zone, 2026, time.November, 6, 8, 0)
	monday := at(zone, 2026, time.November, 9, 0, 0)
	s := agenda.Schedule{Zone: zone, Weekly: workWeek(), Step: 20 * time.Minute}

	got := clocks(zone, s.Slots(now, monday, 40*time.Minute))
	want := []string{
		"09:00", "09:20", "09:40", "10:00", "10:20", "10:40", "11:00", "11:20", // the last 40 minutes that end by 12:00
		"14:00", "14:20", "14:40", "15:00", "15:20", "15:40", "16:00", "16:20", "16:40", "17:00", "17:20",
	}
	if !slices.Equal(got, want) {
		t.Errorf("a Monday offers %v, want %v", got, want)
	}

	// A longer service leaves fewer starts: it has to fit inside the window.
	long := clocks(zone, s.Slots(now, monday, 2*time.Hour))
	if want := []string{"09:00", "09:20", "09:40", "10:00", "14:00", "14:20", "14:40", "15:00", "15:20", "15:40", "16:00"}; !slices.Equal(long, want) {
		t.Errorf("a two-hour service offers %v, want %v", long, want)
	}
}

func TestADayWithNoWindowOffersNothing(t *testing.T) {
	t.Parallel()
	zone := saoPaulo(t)
	now := at(zone, 2026, time.November, 6, 8, 0)
	s := agenda.Schedule{Zone: zone, Weekly: workWeek()}

	if got := s.Slots(now, at(zone, 2026, time.November, 8, 0, 0), 40*time.Minute); len(got) != 0 {
		t.Errorf("a Sunday offers %v", got)
	}
	if got := (agenda.Schedule{Zone: zone}).Slots(now, at(zone, 2026, time.November, 9, 0, 0), 40*time.Minute); len(got) != 0 {
		t.Errorf("a team with no hours offers %v", got)
	}
	if got := (agenda.Schedule{}).Slots(now, at(zone, 2026, time.November, 9, 0, 0), 40*time.Minute); got != nil {
		t.Errorf("a schedule with no zone offers %v", got)
	}
}

func TestOnlyTheCalendarDateOfTheDayCounts(t *testing.T) {
	t.Parallel()
	zone := saoPaulo(t)
	now := at(zone, 2026, time.November, 6, 8, 0)
	s := agenda.Schedule{Zone: zone, Weekly: workWeek(), Step: time.Hour}

	// The same Monday, written at midnight UTC and at noon in Sao Paulo, is the
	// same day: the zone it was written in is not the schedule's.
	utc := time.Date(2026, time.November, 9, 0, 0, 0, 0, time.UTC)
	local := at(zone, 2026, time.November, 9, 23, 30)
	a, b := s.Slots(now, utc, 40*time.Minute), s.Slots(now, local, 40*time.Minute)
	if len(a) == 0 || !slices.Equal(clocks(zone, a), clocks(zone, b)) {
		t.Errorf("the same date gave %v and %v", clocks(zone, a), clocks(zone, b))
	}
	// The windows are Sao Paulo's: 09:00 there is 12:00 UTC.
	if first := a[0].UTC().Format("15:04"); first != "12:00" {
		t.Errorf("the first start of the day is %s UTC, want 12:00", first)
	}
}

func TestAClosureTakesTheStartsItTouches(t *testing.T) {
	t.Parallel()
	zone := saoPaulo(t)
	now := at(zone, 2026, time.November, 6, 8, 0)
	monday := at(zone, 2026, time.November, 9, 0, 0)
	s := agenda.Schedule{
		Zone: zone, Weekly: workWeek(), Step: 20 * time.Minute,
		Closures: []agenda.Span{{Start: at(zone, 2026, time.November, 9, 10, 0), End: at(zone, 2026, time.November, 9, 11, 0)}},
	}

	got := clocks(zone, s.Slots(now, monday, 40*time.Minute))
	// 09:20 ends at 10:00 exactly and is clear; 09:40 would run into the closure,
	// and 11:00 starts as it ends.
	for _, blocked := range []string{"09:40", "10:00", "10:20", "10:40"} {
		if slices.Contains(got, blocked) {
			t.Errorf("%s is offered inside a closure", blocked)
		}
	}
	for _, free := range []string{"09:00", "09:20", "11:00", "11:20"} {
		if !slices.Contains(got, free) {
			t.Errorf("%s is clear of the closure and not offered", free)
		}
	}
	// A closure over the whole day offers nothing.
	s.Closures = []agenda.Span{{Start: at(zone, 2026, time.November, 9, 0, 0), End: at(zone, 2026, time.November, 10, 0, 0)}}
	if got := s.Slots(now, monday, 40*time.Minute); len(got) != 0 {
		t.Errorf("a closed day offers %v", clocks(zone, got))
	}
}

func TestCapacityCountsTheBookingsThatShareTheTime(t *testing.T) {
	t.Parallel()
	zone := saoPaulo(t)
	now := at(zone, 2026, time.November, 6, 8, 0)
	monday := at(zone, 2026, time.November, 9, 0, 0)
	busy := []agenda.Span{{Start: at(zone, 2026, time.November, 9, 9, 0), End: at(zone, 2026, time.November, 9, 9, 40)}}
	s := agenda.Schedule{Zone: zone, Weekly: workWeek(), Step: 20 * time.Minute, Busy: busy}

	// With a place for one, the booking takes every start that overlaps it.
	got := clocks(zone, s.Slots(now, monday, 40*time.Minute))
	for _, taken := range []string{"09:00", "09:20"} {
		if slices.Contains(got, taken) {
			t.Errorf("%s overlaps the booking and is offered with a capacity of one", taken)
		}
	}
	if !slices.Contains(got, "09:40") {
		t.Error("09:40 starts as the booking ends and is not offered")
	}

	// With a place for two, the same booking leaves the time free.
	s.Capacity = 2
	if got := clocks(zone, s.Slots(now, monday, 40*time.Minute)); !slices.Contains(got, "09:00") || !slices.Contains(got, "09:20") {
		t.Errorf("a capacity of two still refuses the time of one booking: %v", got)
	}
	// Two bookings at once fill it, and one that overlaps only the end of the
	// new one still counts at the moment it starts.
	s.Busy = append(s.Busy,
		agenda.Span{Start: at(zone, 2026, time.November, 9, 9, 0), End: at(zone, 2026, time.November, 9, 9, 40)},
		agenda.Span{Start: at(zone, 2026, time.November, 9, 10, 0), End: at(zone, 2026, time.November, 9, 10, 40)},
	)
	got = clocks(zone, s.Slots(now, monday, 40*time.Minute))
	if slices.Contains(got, "09:00") {
		t.Error("two bookings at 09:00 leave a place with a capacity of two")
	}
	if !slices.Contains(got, "09:20") && !slices.Contains(got, "09:40") {
		t.Errorf("a single booking at 10:00 emptied the whole morning: %v", got)
	}
	// Bookings one after the other are never two at once.
	sequence := agenda.Schedule{Zone: zone, Weekly: workWeek(), Step: 20 * time.Minute, Capacity: 2, Busy: []agenda.Span{
		{Start: at(zone, 2026, time.November, 9, 9, 0), End: at(zone, 2026, time.November, 9, 9, 40)},
		{Start: at(zone, 2026, time.November, 9, 9, 40), End: at(zone, 2026, time.November, 9, 10, 20)},
	}}
	if !slices.Contains(clocks(zone, sequence.Slots(now, monday, 40*time.Minute)), "09:20") {
		t.Error("two bookings back to back were counted as concurrent for a start between them")
	}
}

func TestTheNoticeAndTheHorizonBoundTheStarts(t *testing.T) {
	t.Parallel()
	zone := saoPaulo(t)
	// Monday 09 November, 10:00. A day of notice: nothing before Tuesday 10:00.
	now := at(zone, 2026, time.November, 9, 10, 0)
	s := agenda.Schedule{Zone: zone, Weekly: workWeek(), Step: time.Hour, MinNotice: 24 * time.Hour, Horizon: 3 * 24 * time.Hour}

	if got := s.Slots(now, at(zone, 2026, time.November, 9, 0, 0), 40*time.Minute); len(got) != 0 {
		t.Errorf("today offers %v inside the notice", clocks(zone, got))
	}
	tuesday := clocks(zone, s.Slots(now, at(zone, 2026, time.November, 10, 0, 0), 40*time.Minute))
	if len(tuesday) == 0 || tuesday[0] != "10:00" {
		t.Errorf("tomorrow starts at %v, want 10:00: the notice ends exactly then", tuesday)
	}
	// The horizon is three days: Thursday 12th at 10:00 is the last start.
	thursday := clocks(zone, s.Slots(now, at(zone, 2026, time.November, 12, 0, 0), 40*time.Minute))
	if len(thursday) == 0 || thursday[len(thursday)-1] != "10:00" {
		t.Errorf("the last day ends at %v, want 10:00", thursday)
	}
	if got := s.Slots(now, at(zone, 2026, time.November, 13, 0, 0), 40*time.Minute); len(got) != 0 {
		t.Errorf("a day past the horizon offers %v", clocks(zone, got))
	}
	// No notice at all: the next start of today, but never one that has begun.
	s.MinNotice = 0
	if got := clocks(zone, s.Slots(now, at(zone, 2026, time.November, 9, 0, 0), 40*time.Minute)); len(got) == 0 || got[0] != "10:00" {
		t.Errorf("with no notice today starts at %v, want 10:00", got)
	}
}

func TestOnlyAnOfferedStartCanBeBooked(t *testing.T) {
	t.Parallel()
	zone := saoPaulo(t)
	now := at(zone, 2026, time.November, 6, 8, 0)
	s := agenda.Schedule{Zone: zone, Weekly: workWeek(), Step: 20 * time.Minute}

	if !s.Offers(now, at(zone, 2026, time.November, 9, 9, 20), 40*time.Minute) {
		t.Error("an offered start was refused")
	}
	// The same instant, spelled in UTC, is the same start.
	if !s.Offers(now, at(zone, 2026, time.November, 9, 9, 20).UTC(), 40*time.Minute) {
		t.Error("an offered start in UTC was refused")
	}
	for name, start := range map[string]time.Time{
		"between two steps": at(zone, 2026, time.November, 9, 9, 10),
		"outside the hours": at(zone, 2026, time.November, 9, 7, 0),
		"at the lunch hour": at(zone, 2026, time.November, 9, 12, 0),
		"too late to fit":   at(zone, 2026, time.November, 9, 17, 40),
		"on a Sunday":       at(zone, 2026, time.November, 8, 10, 0),
	} {
		if s.Offers(now, start, 40*time.Minute) {
			t.Errorf("a start %s was offered", name)
		}
	}
}

func TestFreeIgnoresTheHoursButNotTheClosuresOrTheCapacity(t *testing.T) {
	t.Parallel()
	zone := saoPaulo(t)
	s := agenda.Schedule{
		Zone:     zone,
		Closures: []agenda.Span{{Start: at(zone, 2026, time.December, 25, 0, 0), End: at(zone, 2026, time.December, 26, 0, 0)}},
		Busy:     []agenda.Span{{Start: at(zone, 2026, time.November, 9, 20, 0), End: at(zone, 2026, time.November, 9, 20, 40)}},
	}
	span := func(start time.Time) agenda.Span { return agenda.Span{Start: start, End: start.Add(40 * time.Minute)} }

	if !s.Free(span(at(zone, 2026, time.November, 9, 21, 0))) {
		t.Error("a time outside the weekly hours is not free for the team, which can fit somebody in")
	}
	if s.Free(span(at(zone, 2026, time.November, 9, 20, 20))) {
		t.Error("a time that overlaps a booking is free with a capacity of one")
	}
	if s.Free(span(at(zone, 2026, time.December, 25, 10, 0))) {
		t.Error("a time inside a closure is free")
	}
}
