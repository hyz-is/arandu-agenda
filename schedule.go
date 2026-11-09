package agenda

import (
	"sort"
	"time"
)

// Span is a range of time: a booking that holds the team, or a closure. It
// holds from Start up to, and not including, End.
type Span struct {
	Start time.Time
	End   time.Time
}

// overlaps reports whether the two ranges share any time.
func (s Span) overlaps(o Span) bool { return s.Start.Before(o.End) && o.Start.Before(s.End) }

// Window is a stretch of the team's week: the team takes bookings on that
// weekday between the two minutes, counted since midnight in Schedule.Zone.
type Window struct {
	// Weekday is 0 for Sunday to 6 for Saturday, as time.Weekday counts.
	Weekday int
	// StartMinute and EndMinute are minutes since midnight: 540 to 1080 is 09:00
	// to 18:00.
	StartMinute int
	EndMinute   int
}

// Schedule is the team's availability as pure data: what a day offers, and
// whether a time is free. It reads no database and no clock, so the rules of
// the agenda are a function of what it is handed and nothing else, and an
// application can ask the same questions the screens do ("when is the next free
// start?") with the same answer.
type Schedule struct {
	// Zone is the timezone the weekly windows and the days are written in.
	Zone *time.Location
	// Weekly are the windows of the week.
	Weekly []Window
	// Closures are the ranges nothing is booked in.
	Closures []Span
	// Busy are the bookings that hold the team at the moment the schedule is
	// asked about. The caller decides which are: Booking.Occupies.
	Busy []Span
	// Capacity is how many bookings one time takes at once. Below one it is one.
	Capacity int
	// Step is the distance between two starts offered on a day. Below a minute
	// it is the DefaultSlotStep.
	Step time.Duration
	// MinNotice is how far ahead of now the earliest start is, and Horizon how
	// far ahead the latest is. A Horizon of zero or less leaves the future
	// unbounded.
	MinNotice time.Duration
	Horizon   time.Duration
}

// Slots are the starts a day offers for something that takes duration, sorted:
// every step inside a window of that weekday where the whole of it fits in the
// window, clears MinNotice and Horizon from now, stays out of every closure and
// finds a place free.
//
// Only the calendar date of day counts: the year, month and day it carries, read
// as written, and not the zone it is in. The weekday, the windows and the
// midnight they count from are Schedule.Zone's.
func (s Schedule) Slots(now, day time.Time, duration time.Duration) []time.Time {
	if s.Zone == nil || duration <= 0 {
		return nil
	}
	year, month, date := day.Date()
	weekday := int(time.Date(year, month, date, 12, 0, 0, 0, s.Zone).Weekday())
	step := s.Step
	if step < time.Minute {
		step = DefaultSlotStep
	}
	stepMinutes := int(step / time.Minute)
	needed := int((duration + time.Minute - 1) / time.Minute)

	seen := map[int64]bool{}
	var out []time.Time
	for _, w := range s.Weekly {
		if w.Weekday != weekday {
			continue
		}
		for minute := w.StartMinute; minute+needed <= w.EndMinute; minute += stepMinutes {
			start := time.Date(year, month, date, minute/60, minute%60, 0, 0, s.Zone)
			if seen[start.Unix()] {
				continue
			}
			if s.offered(now, Span{Start: start, End: start.Add(duration)}) {
				seen[start.Unix()] = true
				out = append(out, start)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// Offers reports whether start is one of the starts Slots offers for its day.
// It is how a booking is checked: what the screen offered is the only thing a
// request can ask for, so a start made up by hand is refused like a start that
// was taken in the meantime.
func (s Schedule) Offers(now, start time.Time, duration time.Duration) bool {
	if s.Zone == nil {
		return false
	}
	local := start.In(s.Zone)
	for _, offered := range s.Slots(now, local, duration) {
		if offered.Equal(start) {
			return true
		}
	}
	return false
}

// Free reports whether the range is outside every closure and has a place
// free, whatever the weekly windows, MinNotice and Horizon say. It is the check
// for a booking that is moved by the team, or one whose payment arrives after
// the hold: the time was offered once, and what can have changed since is who
// else holds it.
func (s Schedule) Free(span Span) bool {
	for _, closure := range s.Closures {
		if span.overlaps(closure) {
			return false
		}
	}
	return s.concurrent(span) < s.capacity()
}

func (s Schedule) capacity() int {
	if s.Capacity < 1 {
		return 1
	}
	return s.Capacity
}

// offered applies the rules a start has to pass to be offered to a customer.
func (s Schedule) offered(now time.Time, span Span) bool {
	if span.Start.Before(now.Add(s.MinNotice)) {
		return false
	}
	if s.Horizon > 0 && span.Start.After(now.Add(s.Horizon)) {
		return false
	}
	return s.Free(span)
}

// concurrent is the most bookings that hold the team at once at any moment of
// the range. The count only changes where a booking starts, so it is taken at
// the start of the range and at every start inside it.
func (s Schedule) concurrent(span Span) int {
	points := []time.Time{span.Start}
	for _, b := range s.Busy {
		if b.Start.After(span.Start) && b.Start.Before(span.End) {
			points = append(points, b.Start)
		}
	}
	most := 0
	for _, at := range points {
		count := 0
		for _, b := range s.Busy {
			if !b.Start.After(at) && b.End.After(at) {
				count++
			}
		}
		if count > most {
			most = count
		}
	}
	return most
}
