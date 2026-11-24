// Package snoozetime reads the wake time a typed snooze means.
package snoozetime

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var units = map[string]time.Duration{
	"m": time.Minute, "min": time.Minute, "mins": time.Minute, "minute": time.Minute, "minutes": time.Minute,
	"h": time.Hour, "hr": time.Hour, "hrs": time.Hour, "hour": time.Hour, "hours": time.Hour,
	"d": 24 * time.Hour, "day": 24 * time.Hour, "days": 24 * time.Hour,
}

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

var (
	durationPattern = regexp.MustCompile(`^(\d+)\s*(m|min|mins|minute|minutes|h|hr|hrs|hour|hours|d|day|days)$`)
	weekdayPattern  = regexp.MustCompile(`^(sun|mon|tue|wed|thu|fri|sat)[a-z]*$`)
	clockPattern    = regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?\s*(am|pm)?$`)
)

type clock struct{ hour, minute int }

var (
	morning = clock{9, 0}
	evening = clock{18, 0}
)

// Parse reads a duration (`45m`, `2h`, `3d`), a clock time (`17:00`, `5pm`)
// for the next such time, a weekday (`fri`) for that day at 9am,
// `tomorrow`, or `tonight` or `evening` for the next 6pm. A weekday or
// `tomorrow` may carry a time after it (`fri 5pm`). Days and clock times
// are in now's location. ok is false for text it does not read.
func Parse(text string, now time.Time) (wake time.Time, ok bool) {
	text = strings.ToLower(strings.TrimSpace(text))
	if m := durationPattern.FindStringSubmatch(text); m != nil {
		return afterDuration(m[1], units[m[2]], now)
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return time.Time{}, false
	}
	head, rest := words[0], strings.Join(words[1:], " ")
	switch {
	case head == "tonight" || head == "evening":
		if rest != "" {
			return time.Time{}, false
		}
		return next(now, evening), true
	case head == "tomorrow":
		c, ok := timeOfDay(rest)
		return on(now, 1, c), ok
	case weekdayPattern.MatchString(head):
		c, ok := timeOfDay(rest)
		return onWeekday(now, weekdays[head[:3]], c), ok
	default:
		c, ok := parseClock(text)
		return next(now, c), ok
	}
}

func afterDuration(count string, unit time.Duration, now time.Time) (time.Time, bool) {
	n, err := strconv.ParseInt(count, 10, 64)
	if err != nil || n <= 0 || n > int64(math.MaxInt64/unit) {
		return time.Time{}, false
	}
	return now.Add(time.Duration(n) * unit), true
}

func parseClock(text string) (clock, bool) {
	m := clockPattern.FindStringSubmatch(text)
	if m == nil || (m[2] == "" && m[3] == "") {
		return clock{}, false
	}
	hour, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])
	if minute > 59 {
		return clock{}, false
	}
	switch m[3] {
	case "am", "pm":
		if hour < 1 || hour > 12 {
			return clock{}, false
		}
		hour %= 12
		if m[3] == "pm" {
			hour += 12
		}
	default:
		if hour > 23 {
			return clock{}, false
		}
	}
	return clock{hour, minute}, true
}

func timeOfDay(rest string) (clock, bool) {
	if rest == "" {
		return morning, true
	}
	return parseClock(rest)
}

// on builds the time from now's date and location, so a day that crosses a
// daylight saving change still lands on the wall-clock time asked for.
func on(now time.Time, days int, c clock) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day()+days, c.hour, c.minute, 0, 0, now.Location())
}

func next(now time.Time, c clock) time.Time {
	if t := on(now, 0, c); t.After(now) {
		return t
	}
	return on(now, 1, c)
}

func onWeekday(now time.Time, day time.Weekday, c clock) time.Time {
	days := (int(day) - int(now.Weekday()) + 7) % 7
	if t := on(now, days, c); t.After(now) {
		return t
	}
	return on(now, days+7, c)
}
