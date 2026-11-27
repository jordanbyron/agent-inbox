package snoozetime_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/snoozetime"
)

func TestParse(t *testing.T) {
	nyc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	at := func(year int, month time.Month, day, hour, minute int) time.Time {
		return time.Date(year, month, day, hour, minute, 0, 0, nyc)
	}
	wednesdayEvening := at(2026, 9, 16, 20, 30)

	cases := []struct {
		name string
		text string
		now  time.Time
		want time.Time
	}{
		{"minutes", "45m", wednesdayEvening, wednesdayEvening.Add(45 * time.Minute)},
		{"hours", "2h", wednesdayEvening, wednesdayEvening.Add(2 * time.Hour)},
		{"days", "3d", wednesdayEvening, wednesdayEvening.Add(3 * 24 * time.Hour)},
		{"a spaced unit with padding", " 90 min ", wednesdayEvening, wednesdayEvening.Add(90 * time.Minute)},
		{"a spelled-out unit", "1 hour", wednesdayEvening, wednesdayEvening.Add(time.Hour)},

		{"a clock time later today", "21:15", wednesdayEvening, at(2026, 9, 16, 21, 15)},
		{"a clock time already passed today", "17:00", wednesdayEvening, at(2026, 9, 17, 17, 0)},
		{"a pm time", "5pm", wednesdayEvening, at(2026, 9, 17, 17, 0)},
		{"an AM time with minutes and a space", "9:30 AM", wednesdayEvening, at(2026, 9, 17, 9, 30)},
		{"midnight as 12am", "12am", wednesdayEvening, at(2026, 9, 17, 0, 0)},
		{"noon as 12pm", "12pm", wednesdayEvening, at(2026, 9, 17, 12, 0)},
		{"9am in the evening is tomorrow", "9am", wednesdayEvening, at(2026, 9, 17, 9, 0)},
		{"9am early in the morning is today", "9am", at(2026, 9, 16, 3, 0), at(2026, 9, 16, 9, 0)},

		{"a weekday is that day at 9am", "fri", wednesdayEvening, at(2026, 9, 18, 9, 0)},
		{"a weekday spelled out", "Friday", wednesdayEvening, at(2026, 9, 18, 9, 0)},
		{"today's weekday once 9am has passed is a week on", "wed", wednesdayEvening, at(2026, 9, 23, 9, 0)},
		{"today's weekday before 9am is today", "wed", at(2026, 9, 16, 8, 0), at(2026, 9, 16, 9, 0)},
		{"a weekday with a time", "sun 5pm", wednesdayEvening, at(2026, 9, 20, 17, 0)},

		{"tomorrow is 9am", "tomorrow", wednesdayEvening, at(2026, 9, 17, 9, 0)},
		{"tomorrow with a time", "tomorrow 14:00", wednesdayEvening, at(2026, 9, 17, 14, 0)},
		{"tomorrow at the end of a month", "tomorrow", at(2026, 9, 30, 3, 0), at(2026, 10, 1, 9, 0)},
		{"tomorrow across the end of daylight saving", "tomorrow", at(2026, 10, 31, 20, 0), at(2026, 11, 1, 9, 0)},

		{"tonight is the next 6pm", "tonight", wednesdayEvening, at(2026, 9, 17, 18, 0)},
		{"evening before 6pm is today", "evening", at(2026, 9, 16, 11, 0), at(2026, 9, 16, 18, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := snoozetime.Parse(tc.text, tc.now)
			require.True(t, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseKeepsTheLocationOfNow(t *testing.T) {
	utc := time.Date(2026, 9, 16, 20, 30, 0, 0, time.UTC)

	got, ok := snoozetime.Parse("tomorrow", utc)

	require.True(t, ok)
	require.Equal(t, time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC), got)
}

func TestParseRejects(t *testing.T) {
	now := time.Date(2026, 9, 16, 20, 30, 0, 0, time.UTC)
	cases := []struct {
		name string
		text string
	}{
		{"nothing", ""},
		{"a word it does not know", "soon"},
		{"a zero duration", "0m"},
		{"a bare number", "5"},
		{"an hour past 23", "25:00"},
		{"a minute past 59", "9:60"},
		{"an hour past 12 with pm", "13pm"},
		{"zero with am", "0am"},
		{"words after a duration", "2h later"},
		{"a time after tonight", "tonight 8pm"},
		{"a weekday with a bare number", "fri at 5"},
		{"never", "never"},
		{"a count too big for a number", "99999999999999999999m"},
		{"a duration too long to add", "9999999999999d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := snoozetime.Parse(tc.text, now)
			require.False(t, ok)
			require.Zero(t, got)
		})
	}
}
