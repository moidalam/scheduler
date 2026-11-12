package schedule

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var pacific = mustLoc("America/Los_Angeles")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// The brief's rules: Mon-Fri, 08:00-17:00 local; 30-minute sessions on :00/:30.
func businessWeek() []Rule {
	var rs []Rule
	for wd := time.Monday; wd <= time.Friday; wd++ {
		rs = append(rs, Rule{Weekday: wd, StartMinute: 8 * 60, EndMinute: 17 * 60})
	}
	return rs
}

var thirtyMinutes = Policy{DurationMinutes: 30, GranularityMinutes: 30, MinNoticeMinutes: 0, MaxHorizonDays: 60}

// farPolicy is thirtyMinutes with a horizon long enough to reach the DST dates
// under test. Without it those dates fall beyond the booking horizon and the
// tests would pass or fail for the wrong reason.
var farPolicy = Policy{DurationMinutes: 30, GranularityMinutes: 30, MinNoticeMinutes: 0, MaxHorizonDays: 400}

// at builds an instant from Pacific wall-clock time.
func at(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, pacific)
}

var longBefore = at(2029, time.December, 1, 0, 0) // "now", well before every window under test

func TestGenerateSlots_ordinaryWeekday(t *testing.T) {
	// Monday 2030-01-07.
	got := GenerateSlots(pacific, businessWeek(), thirtyMinutes,
		at(2030, time.January, 7, 0, 0), at(2030, time.January, 8, 0, 0), longBefore)

	require.Len(t, got, 18, "08:00 through 16:30 is eighteen half-hour slots")
	assert.True(t, got[0].Start.Equal(at(2030, time.January, 7, 8, 0)))
	assert.True(t, got[17].End.Equal(at(2030, time.January, 7, 17, 0)))
	for i := 1; i < len(got); i++ {
		assert.True(t, got[i].Start.Equal(got[i-1].End), "slots are contiguous")
	}
}

func TestGenerateSlots_weekendIsEmpty(t *testing.T) {
	// Saturday and Sunday, 2030-01-05 and 06.
	got := GenerateSlots(pacific, businessWeek(), thirtyMinutes,
		at(2030, time.January, 5, 0, 0), at(2030, time.January, 7, 0, 0), longBefore)
	assert.Empty(t, got)
}

func TestGenerateSlots_windowIsHalfOpen(t *testing.T) {
	// A window ending exactly at 09:00 includes the 08:30 slot, not 09:00.
	got := GenerateSlots(pacific, businessWeek(), thirtyMinutes,
		at(2030, time.January, 7, 8, 0), at(2030, time.January, 7, 9, 0), longBefore)
	require.Len(t, got, 2)
	assert.True(t, got[1].Start.Equal(at(2030, time.January, 7, 8, 30)))
}

// Business hours are local, so the same 08:00 is a different UTC instant either
// side of a DST change. This is the failure ADR-0003 exists to prevent.
func TestGenerateSlots_followsDST(t *testing.T) {
	// 2030 US DST: starts Sunday March 10, ends Sunday November 3.
	cases := []struct {
		name    string
		day     time.Time
		wantUTC int // expected UTC hour of the 08:00 local slot
	}{
		{"winter (PST, UTC-8)", at(2030, time.January, 7, 0, 0), 16},
		{"after spring forward (PDT, UTC-7)", at(2030, time.March, 11, 0, 0), 15},
		{"after fall back (PST, UTC-8)", at(2030, time.November, 4, 0, 0), 16},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := GenerateSlots(pacific, businessWeek(), farPolicy, c.day, c.day.AddDate(0, 0, 1), longBefore)
			require.Len(t, got, 18, "always eighteen slots regardless of the offset")
			assert.Equal(t, c.wantUTC, got[0].Start.UTC().Hour())
		})
	}
}

// 2030-03-10 is spring-forward: 02:00-03:00 local does not exist.
func TestGenerateSlots_skipsNonexistentLocalTimes(t *testing.T) {
	sunday := []Rule{{Weekday: time.Sunday, StartMinute: 2 * 60, EndMinute: 4 * 60}}
	got := GenerateSlots(pacific, sunday, farPolicy,
		at(2030, time.March, 10, 0, 0), at(2030, time.March, 11, 0, 0), longBefore)

	require.Len(t, got, 2, "02:00 and 02:30 never happen; only 03:00 and 03:30 do")
	assert.Equal(t, 3, got[0].Start.In(pacific).Hour())
	assert.Equal(t, 0, got[0].Start.In(pacific).Minute())
	assert.Equal(t, 30*time.Minute, got[0].End.Sub(got[0].Start), "a slot is still thirty real minutes")
}

func TestGenerateSlots_excludesThePast(t *testing.T) {
	now := at(2030, time.January, 7, 10, 15)
	got := GenerateSlots(pacific, businessWeek(), thirtyMinutes,
		at(2030, time.January, 7, 0, 0), at(2030, time.January, 8, 0, 0), now)

	require.NotEmpty(t, got)
	assert.True(t, got[0].Start.Equal(at(2030, time.January, 7, 10, 30)), "10:00 is already started; first is 10:30")
}

func TestGenerateSlots_honoursMinNotice(t *testing.T) {
	p := thirtyMinutes
	p.MinNoticeMinutes = 60
	now := at(2030, time.January, 7, 10, 0)
	got := GenerateSlots(pacific, businessWeek(), p,
		at(2030, time.January, 7, 0, 0), at(2030, time.January, 8, 0, 0), now)
	assert.True(t, got[0].Start.Equal(at(2030, time.January, 7, 11, 0)))
}

func TestGenerateSlots_honoursHorizon(t *testing.T) {
	p := thirtyMinutes
	p.MaxHorizonDays = 1
	now := at(2030, time.January, 7, 12, 0)
	got := GenerateSlots(pacific, businessWeek(), p,
		at(2030, time.January, 7, 0, 0), at(2030, time.January, 12, 0, 0), now)

	require.NotEmpty(t, got)
	horizon := now.Add(24 * time.Hour)
	for _, s := range got {
		assert.False(t, s.End.After(horizon), "no slot ends beyond now + horizon")
	}
}

func TestGenerateSlots_effectiveDates(t *testing.T) {
	from := time.Date(2030, time.January, 8, 0, 0, 0, 0, time.UTC) // Tuesday
	rules := []Rule{{Weekday: time.Monday, StartMinute: 8 * 60, EndMinute: 9 * 60},
		{Weekday: time.Tuesday, StartMinute: 8 * 60, EndMinute: 9 * 60, EffectiveFrom: &from}}

	monday := GenerateSlots(pacific, rules, thirtyMinutes, at(2030, time.January, 7, 0, 0), at(2030, time.January, 8, 0, 0), longBefore)
	tuesday := GenerateSlots(pacific, rules, thirtyMinutes, at(2030, time.January, 8, 0, 0), at(2030, time.January, 9, 0, 0), longBefore)
	assert.Len(t, monday, 2)
	assert.Len(t, tuesday, 2, "the Tuesday rule takes effect on its first day")

	early := time.Date(2030, time.January, 9, 0, 0, 0, 0, time.UTC)
	rules[1].EffectiveFrom = &early
	tuesday = GenerateSlots(pacific, rules, thirtyMinutes, at(2030, time.January, 8, 0, 0), at(2030, time.January, 9, 0, 0), longBefore)
	assert.Empty(t, tuesday, "a rule effective from tomorrow does not apply today")
}

func TestGenerateSlots_ruleOffTheGridStartsOnTheNextGridPoint(t *testing.T) {
	rules := []Rule{{Weekday: time.Monday, StartMinute: 8*60 + 15, EndMinute: 10 * 60}}
	got := GenerateSlots(pacific, rules, thirtyMinutes, at(2030, time.January, 7, 0, 0), at(2030, time.January, 8, 0, 0), longBefore)
	require.NotEmpty(t, got)
	assert.True(t, got[0].Start.Equal(at(2030, time.January, 7, 8, 30)), "08:15 is off-grid, so the first slot is 08:30")
}

func TestSubtract(t *testing.T) {
	slots := GenerateSlots(pacific, businessWeek(), thirtyMinutes,
		at(2030, time.January, 7, 8, 0), at(2030, time.January, 7, 10, 0), longBefore) // 08:00 08:30 09:00 09:30

	t.Run("a booking removes exactly its slot", func(t *testing.T) {
		busy := []Slot{{at(2030, time.January, 7, 8, 30), at(2030, time.January, 7, 9, 0)}}
		got := Subtract(slots, busy)
		require.Len(t, got, 3)
		assert.True(t, got[1].Start.Equal(at(2030, time.January, 7, 9, 0)))
	})
	t.Run("touching intervals do not overlap", func(t *testing.T) {
		busy := []Slot{{at(2030, time.January, 7, 9, 0), at(2030, time.January, 7, 9, 30)}}
		assert.Len(t, Subtract(slots, busy), 3, "08:30-09:00 ends where the booking starts")
	})
	t.Run("a long booking removes every slot it covers", func(t *testing.T) {
		busy := []Slot{{at(2030, time.January, 7, 8, 0), at(2030, time.January, 7, 9, 30)}}
		got := Subtract(slots, busy)
		require.Len(t, got, 1)
		assert.True(t, got[0].Start.Equal(at(2030, time.January, 7, 9, 30)))
	})
	t.Run("no bookings leaves everything", func(t *testing.T) {
		assert.Len(t, Subtract(slots, nil), 4)
	})
}

func TestValidateBooking(t *testing.T) {
	now := at(2030, time.January, 1, 0, 0)
	cases := []struct {
		name       string
		start, end time.Time
		want       string // "" means valid
	}{
		{"valid, start of day", at(2030, time.January, 7, 8, 0), at(2030, time.January, 7, 8, 30), ""},
		{"valid, last slot of day", at(2030, time.January, 7, 16, 30), at(2030, time.January, 7, 17, 0), ""},
		{"valid, on the half hour", at(2030, time.January, 7, 13, 30), at(2030, time.January, 7, 14, 0), ""},
		{"too long", at(2030, time.January, 7, 9, 0), at(2030, time.January, 7, 10, 0), CodeInvalidDuration},
		{"too short", at(2030, time.January, 7, 9, 0), at(2030, time.January, 7, 9, 15), CodeInvalidDuration},
		{"off the grid", at(2030, time.January, 7, 9, 15), at(2030, time.January, 7, 9, 45), CodeMisalignedStart},
		{"has seconds", time.Date(2030, time.January, 7, 9, 0, 30, 0, pacific), time.Date(2030, time.January, 7, 9, 30, 30, 0, pacific), CodeMisalignedStart},
		{"before opening", at(2030, time.January, 7, 7, 30), at(2030, time.January, 7, 8, 0), CodeOutsideBusinessHours},
		{"starts at closing", at(2030, time.January, 7, 17, 0), at(2030, time.January, 7, 17, 30), CodeOutsideBusinessHours},
		{"runs past closing", at(2030, time.January, 7, 16, 45), at(2030, time.January, 7, 17, 15), CodeMisalignedStart},
		{"saturday", at(2030, time.January, 5, 10, 0), at(2030, time.January, 5, 10, 30), CodeOutsideBusinessHours},
		{"in the past", at(2029, time.December, 31, 10, 0), at(2029, time.December, 31, 10, 30), CodeStartsInPast},
		{"beyond the horizon", at(2030, time.April, 1, 10, 0), at(2030, time.April, 1, 10, 30), CodeBeyondHorizon},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := ValidateBooking(pacific, businessWeek(), thirtyMinutes, c.start, c.end, now)
			if c.want == "" {
				assert.Nil(t, v)
				return
			}
			require.NotNil(t, v)
			assert.Equal(t, c.want, v.Code)
		})
	}
}

// Business hours are local time, so the same instant-of-day means different UTC
// hours across a DST change. On Monday 2030-03-11, 08:00 local is 15:00 UTC
// (PDT); 16:00 UTC is 09:00 local, still inside hours; 14:00 UTC is 07:00
// local, before opening.
func TestValidateBooking_acrossDST(t *testing.T) {
	now := at(2030, time.January, 1, 0, 0)
	start := time.Date(2030, time.March, 11, 15, 0, 0, 0, time.UTC) // Monday 08:00 PDT
	assert.Nil(t, ValidateBooking(pacific, businessWeek(), farPolicy, start, start.Add(30*time.Minute), now))

	stale := time.Date(2030, time.March, 11, 16, 0, 0, 0, time.UTC) // 09:00 PDT, still in hours
	assert.Nil(t, ValidateBooking(pacific, businessWeek(), farPolicy, stale, stale.Add(30*time.Minute), now))

	tooEarly := time.Date(2030, time.March, 11, 14, 0, 0, 0, time.UTC) // 07:00 PDT, before opening
	v := ValidateBooking(pacific, businessWeek(), farPolicy, tooEarly, tooEarly.Add(30*time.Minute), now)
	require.NotNil(t, v)
	assert.Equal(t, CodeOutsideBusinessHours, v.Code)
}

// The two halves of the system must agree: every slot we offer must be
// bookable. If this fails, clients are shown times we then reject.
func TestGeneratedSlotsAlwaysValidate(t *testing.T) {
	now := at(2030, time.January, 1, 0, 0)
	from, to := at(2030, time.February, 25, 0, 0), at(2030, time.April, 1, 0, 0) // spans spring forward
	slots := GenerateSlots(pacific, businessWeek(), farPolicy, from, to, now)
	require.NotEmpty(t, slots)

	// Guard against passing vacuously: the range must contain slots on both
	// sides of the spring-forward change, at different UTC offsets.
	offsets := map[int]bool{}
	for _, s := range slots {
		_, off := s.Start.In(pacific).Zone()
		offsets[off] = true
	}
	require.Len(t, offsets, 2, "the range must cross the DST change")

	for _, s := range slots {
		v := ValidateBooking(pacific, businessWeek(), farPolicy, s.Start, s.End, now)
		assert.Nilf(t, v, "offered slot %s was rejected", s.Start)
	}
}

func TestValidateWindow(t *testing.T) {
	from := at(2030, time.January, 7, 0, 0)
	assert.Nil(t, ValidateWindow(from, from.AddDate(0, 0, 7), thirtyMinutes))
	assert.Equal(t, CodeInvalidWindow, ValidateWindow(from, from, thirtyMinutes).Code, "empty window")
	assert.Equal(t, CodeInvalidWindow, ValidateWindow(from, from.Add(-time.Hour), thirtyMinutes).Code, "backwards window")
	assert.Equal(t, CodeWindowTooLarge, ValidateWindow(from, from.AddDate(0, 0, 61), thirtyMinutes).Code)
	assert.Nil(t, ValidateWindow(from, from.AddDate(0, 0, 60), thirtyMinutes), "exactly the cap is allowed")
}
