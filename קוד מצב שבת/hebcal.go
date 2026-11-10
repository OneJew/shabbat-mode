package main

import "time"

// Hebrew calendar arithmetic (Dershowitz & Reingold). Days are "rata die" numbers.
const hebrewEpoch = -1373427

func rataDie(y int, m time.Month, d int) int {
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix()/86400) + 719163
}

func hebrewElapsed(y int) int {
	months := (235*y - 234) / 19
	parts := 12084 + 13753*months
	day := 29*months + parts/25920
	if (3*(day+1))%7 < 3 {
		day++
	}
	return day
}

func hebrewDelay(y int) int {
	n0, n1, n2 := hebrewElapsed(y-1), hebrewElapsed(y), hebrewElapsed(y+1)
	if n2-n1 == 356 {
		return 2
	}
	if n1-n0 == 382 {
		return 1
	}
	return 0
}

// roshHashana returns the day of 1 Tishrei of Hebrew year y.
func roshHashana(y int) int {
	return hebrewEpoch + hebrewElapsed(y) + hebrewDelay(y)
}

// holyDays returns the Israeli yom tov days, in Hebrew year y, with their names.
// The Nisan and Sivan dates count back from the next Rosh Hashana, because the
// months from 1 Nisan to 1 Tishrei always total 177 days.
func holyDays(y int) map[int]string {
	rh, next := roshHashana(y), roshHashana(y+1)
	return map[int]string{
		rh:         "ראש השנה",
		rh + 1:     "ראש השנה",
		rh + 9:     "יום כיפור",
		rh + 14:    "סוכות",
		rh + 21:    "שמיני עצרת",
		next - 163: "פסח",
		next - 157: "שביעי של פסח",
		next - 113: "שבועות",
	}
}

func holyDayName(rd int) (string, bool) {
	approx := (rd-hebrewEpoch)*100/36525 + 1
	for y := approx - 1; y <= approx+1; y++ {
		if n, ok := holyDays(y)[rd]; ok {
			return n, true
		}
	}
	return "", false
}

func isShabbat(rd int) bool { return rd%7 == 6 }

// entryFor reports whether Shabbat or yom tov begins at the end of day rd, and its name.
// A day that is already Shabbat or yom tov is skipped: the computer should be off by then.
func entryFor(rd int) (string, bool) {
	if _, holy := holyDayName(rd); holy || isShabbat(rd) {
		return "", false
	}
	if n, ok := holyDayName(rd + 1); ok {
		return "ערב " + n, true
	}
	if isShabbat(rd + 1) {
		return "ערב שבת", true
	}
	return "", false
}
