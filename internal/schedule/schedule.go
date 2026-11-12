// Package schedule holds the scheduling rules: slot generation, booking
// validation and interval arithmetic. It is pure. It does no I/O and depends only
// on the standard library, so every rule can be tested without a database or an
// HTTP server (ADR-0007).
//
// Two conventions run through it (ADR-0003):
//   - Intervals are half-open, [Start, End). Bookings that touch do not overlap.
//   - Business hours are evaluated in the resource's own timezone, by walking
//     local civil days and converting to instants, never by fixed UTC offsets.
package schedule

import (
	"sort"
	"time"
)

// Rule is one recurring weekly window in a resource's local time.
type Rule struct {
	Weekday     time.Weekday
	StartMinute int // minutes after local midnight, inclusive
	EndMinute   int // minutes after local midnight, exclusive

	// EffectiveFrom and EffectiveTo are calendar dates, inclusive; only the
	// year, month and day are read. Nil means unbounded on that side.
	EffectiveFrom *time.Time
	EffectiveTo   *time.Time
}

// Policy is the service-level configuration a booking is held to.
type Policy struct {
	DurationMinutes    int
	GranularityMinutes int
	MinNoticeMinutes   int
	MaxHorizonDays     int
}

// Slot is a half-open time interval, [Start, End).
type Slot struct {
	Start time.Time
	End   time.Time
}

// Violation is a broken scheduling rule. Code is stable and machine-readable.
type Violation struct {
	Code   string
	Detail string
}

func (v *Violation) Error() string { return v.Code + ": " + v.Detail }

// Violation codes. Clients branch on these, so they are part of the API.
const (
	CodeInvalidDuration      = "invalid_duration"
	CodeMisalignedStart      = "misaligned_start"
	CodeOutsideBusinessHours = "outside_business_hours"
	CodeStartsInPast         = "starts_in_past"
	CodeInsufficientNotice   = "insufficient_notice"
	CodeBeyondHorizon        = "beyond_horizon"
	CodeInvalidWindow        = "invalid_window"
	CodeWindowTooLarge       = "window_too_large"
)

// ValidateWindow checks a query window [from, to) against the policy's cap.
func ValidateWindow(from, to time.Time, p Policy) *Violation {
	if !to.After(from) {
		return &Violation{CodeInvalidWindow, "ends_at must be after starts_at"}
	}
	if max := time.Duration(p.MaxHorizonDays) * 24 * time.Hour; to.Sub(from) > max {
		return &Violation{CodeWindowTooLarge, "the window may not exceed the service's booking horizon"}
	}
	return nil
}

// GenerateSlots returns every bookable slot inside [from, to), in ascending
// order, before any existing bookings are subtracted.
//
// A slot is included when it lies inside a rule for its local day, starts on the
// policy's grid (measured from local midnight), starts no earlier than
// now + MinNotice, and ends no later than now + MaxHorizon. A local time that
// does not exist, because the clock skipped it on a spring-forward day, yields
// no slot.
func GenerateSlots(loc *time.Location, rules []Rule, p Policy, from, to, now time.Time) []Slot {
	lo := from
	if earliest := now.Add(time.Duration(p.MinNoticeMinutes) * time.Minute); earliest.After(lo) {
		lo = earliest
	}
	hi := to
	if latest := now.Add(time.Duration(p.MaxHorizonDays) * 24 * time.Hour); latest.Before(hi) {
		hi = latest
	}
	if !hi.After(lo) || p.DurationMinutes <= 0 || p.GranularityMinutes <= 0 {
		return nil
	}

	dur := time.Duration(p.DurationMinutes) * time.Minute
	var out []Slot

	// Anchor each day at local noon so that adding a day is never thrown off
	// by a DST shift near midnight.
	first, last := lo.In(loc), hi.In(loc)
	day := time.Date(first.Year(), first.Month(), first.Day(), 12, 0, 0, 0, loc)
	lastDay := time.Date(last.Year(), last.Month(), last.Day(), 12, 0, 0, 0, loc)

	for ; !day.After(lastDay); day = day.AddDate(0, 0, 1) {
		y, m, d := day.Date()
		for _, r := range rules {
			if !r.appliesOn(y, m, d, day.Weekday()) {
				continue
			}
			for mm := ceilTo(r.StartMinute, p.GranularityMinutes); mm+p.DurationMinutes <= r.EndMinute; mm += p.GranularityMinutes {
				start := time.Date(y, m, d, mm/60, mm%60, 0, 0, loc)
				if start.Hour()*60+start.Minute() != mm {
					continue // this wall-clock time was skipped by a DST change
				}
				end := start.Add(dur)
				if start.Before(lo) || end.After(hi) {
					continue
				}
				out = append(out, Slot{Start: start, End: end})
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return dedupe(out)
}

// Subtract removes every slot that overlaps any busy interval.
func Subtract(slots, busy []Slot) []Slot {
	if len(busy) == 0 {
		return slots
	}
	out := make([]Slot, 0, len(slots))
	for _, s := range slots {
		if !overlapsAny(s, busy) {
			out = append(out, s)
		}
	}
	return out
}

// ValidateBooking reports why [start, end) is not a bookable slot, or nil if it
// is. It is the single source of truth for booking rules: a slot produced by
// GenerateSlots always passes it, which the tests assert.
//
// The order of checks is part of the contract, so a request that breaks several
// rules always reports the same one.
func ValidateBooking(loc *time.Location, rules []Rule, p Policy, start, end, now time.Time) *Violation {
	if end.Sub(start) != time.Duration(p.DurationMinutes)*time.Minute {
		return &Violation{CodeInvalidDuration, "a booking must last exactly the service's duration"}
	}

	ls := start.In(loc)
	sm := ls.Hour()*60 + ls.Minute()
	if ls.Second() != 0 || ls.Nanosecond() != 0 || sm%p.GranularityMinutes != 0 {
		return &Violation{CodeMisalignedStart, "a booking must start on the service's time grid"}
	}

	if !withinRules(rules, ls, end.In(loc), sm) {
		return &Violation{CodeOutsideBusinessHours, "the booking is outside the resource's business hours"}
	}

	if start.Before(now) {
		return &Violation{CodeStartsInPast, "a booking cannot start in the past"}
	}
	if start.Before(now.Add(time.Duration(p.MinNoticeMinutes) * time.Minute)) {
		return &Violation{CodeInsufficientNotice, "a booking needs more notice than this"}
	}
	if end.After(now.Add(time.Duration(p.MaxHorizonDays) * 24 * time.Hour)) {
		return &Violation{CodeBeyondHorizon, "a booking cannot be made this far ahead"}
	}
	return nil
}

func withinRules(rules []Rule, ls, le time.Time, startMinute int) bool {
	y, m, d := ls.Date()
	ey, em, ed := le.Date()
	if ey != y || em != m || ed != d {
		return false // a booking may not span local midnight
	}
	endMinute := le.Hour()*60 + le.Minute()
	for _, r := range rules {
		if r.appliesOn(y, m, d, ls.Weekday()) && startMinute >= r.StartMinute && endMinute <= r.EndMinute {
			return true
		}
	}
	return false
}

func (r Rule) appliesOn(y int, m time.Month, d int, wd time.Weekday) bool {
	if r.Weekday != wd {
		return false
	}
	today := ymd(y, m, d)
	if r.EffectiveFrom != nil && today < ymd(r.EffectiveFrom.Date()) {
		return false
	}
	if r.EffectiveTo != nil && today > ymd(r.EffectiveTo.Date()) {
		return false
	}
	return true
}

func ymd(y int, m time.Month, d int) int { return y*10000 + int(m)*100 + d }

// ceilTo returns the smallest multiple of step that is >= n.
func ceilTo(n, step int) int {
	if r := n % step; r != 0 {
		return n + step - r
	}
	return n
}

func dedupe(s []Slot) []Slot {
	if len(s) < 2 {
		return s
	}
	out := s[:1]
	for _, x := range s[1:] {
		if !x.Start.Equal(out[len(out)-1].Start) {
			out = append(out, x)
		}
	}
	return out
}

func overlapsAny(s Slot, busy []Slot) bool {
	for _, b := range busy {
		if s.Start.Before(b.End) && b.Start.Before(s.End) {
			return true
		}
	}
	return false
}
