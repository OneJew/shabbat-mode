package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func cfgT() Config { c := defaultConfig(); c.Accepted = true; return c }

// Known civil dates of Rosh Hashana and Pesach.
func TestHolyDates(t *testing.T) {
	cases := []struct {
		y int
		m time.Month
		d int
		n string
	}{
		{2024, 10, 3, "ראש השנה"}, {2025, 9, 23, "ראש השנה"}, {2026, 9, 12, "ראש השנה"},
		{2025, 10, 2, "יום כיפור"}, {2026, 4, 2, "פסח"}, {2026, 5, 22, "שבועות"}, {2025, 10, 7, "סוכות"},
	}
	for _, c := range cases {
		if got, ok := holyDayName(rataDie(c.y, c.m, c.d)); !ok || got != c.n {
			t.Errorf("%v-%v-%v got %q want %q", c.y, c.m, c.d, got, c.n)
		}
	}
}

type rec struct {
	sync.Mutex
	warns   []string
	acts    int
	screens int
}

func engineAt(now *time.Time, r *rec, cancel bool) *Engine {
	e := newEngine(cfgT())
	e.now = func() time.Time { return *now }
	e.warn = func(n notice) bool { r.Lock(); r.warns = append(r.warns, n.Text); r.Unlock(); return cancel }
	e.act = func(Config) error { r.Lock(); r.acts++; r.Unlock(); return nil }
	e.save = func(Config) error { return nil }
	e.awake = func(bool) {}
	e.screen = func(string) { r.Lock(); r.screens++; r.Unlock() }
	return e
}

func settle() { time.Sleep(20 * time.Millisecond) }

func nextEntry(t *testing.T) Entry {
	loc, _ := time.LoadLocation("Asia/Jerusalem")
	return entriesFrom(time.Date(2026, 11, 6, 8, 0, 0, 0, loc), cfgT(), 1)[0]
}

func TestNormalFlow(t *testing.T) {
	en := nextEntry(t)
	now := en.Action.Add(-40 * time.Minute)
	r := &rec{}
	e := engineAt(&now, r, false)
	for now.Before(en.Action.Add(2 * time.Minute)) {
		e.tick()
		now = now.Add(15 * time.Second)
	}
	settle()
	if len(r.warns) != 3 || r.acts != 1 {
		t.Fatalf("warns=%d acts=%d", len(r.warns), r.acts)
	}
}

func TestLateStartGetsWarningAndGrace(t *testing.T) {
	en := nextEntry(t)
	now := en.Action.Add(5 * time.Minute)
	r := &rec{}
	e := engineAt(&now, r, false)
	e.tick()
	settle()
	if len(r.warns) != 1 || r.acts != 0 {
		t.Fatalf("first: warns=%d acts=%d", len(r.warns), r.acts)
	}
	now = now.Add(2 * time.Minute)
	e.tick()
	if r.acts != 0 {
		t.Fatal("acted before grace ended")
	}
	now = now.Add(70 * time.Second)
	e.tick()
	e.tick()
	settle()
	if r.acts != 1 || len(r.warns) != 1 {
		t.Fatalf("after: warns=%d acts=%d", len(r.warns), r.acts)
	}
}

func TestAfterCandleLightingNothingHappens(t *testing.T) {
	en := nextEntry(t)
	now := en.Candle.Add(time.Minute)
	r := &rec{}
	e := engineAt(&now, r, false)
	e.tick()
	settle()
	if r.acts != 0 || len(r.warns) != 0 {
		t.Fatalf("warns=%d acts=%d", len(r.warns), r.acts)
	}
}

func TestCancelFromPopup(t *testing.T) {
	en := nextEntry(t)
	now := en.Action.Add(-9 * time.Minute)
	r := &rec{}
	e := engineAt(&now, r, true)
	e.tick()
	settle()
	now = en.Action.Add(time.Minute)
	e.tick()
	settle()
	if r.acts != 0 || e.config().SkipDate != en.Date {
		t.Fatalf("acts=%d skip=%q", r.acts, e.config().SkipDate)
	}
}

func TestNothingRunsBeforeTermsAccepted(t *testing.T) {
	en := nextEntry(t)
	now := en.Action.Add(time.Minute)
	r := &rec{}
	e := engineAt(&now, r, false)
	c := e.config()
	c.Accepted = false
	e.setConfig(c)
	e.tick()
	settle()
	if r.acts != 0 || len(r.warns) != 0 {
		t.Fatal("ran before terms were accepted")
	}
}

func TestDisabled(t *testing.T) {
	en := nextEntry(t)
	now := en.Action.Add(time.Minute)
	r := &rec{}
	e := engineAt(&now, r, false)
	c := e.config()
	c.Enabled = false
	e.setConfig(c)
	e.tick()
	settle()
	if r.acts != 0 || len(r.warns) != 0 {
		t.Fatal("ran while disabled")
	}
}

func TestLocalPageProtection(t *testing.T) {
	os.Setenv("XDG_CONFIG_HOME", t.TempDir())
	now := time.Now()
	r := &rec{}
	h := handler(engineAt(&now, r, false), "secret", func() {})
	do := func(method, path, host, tok string) int {
		q := httptest.NewRequest(method, path, strings.NewReader("{}"))
		q.Host = host
		if tok != "" {
			q.Header.Set("X-Token", tok)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, q)
		return w.Code
	}
	for _, c := range []struct {
		m, p, h, tok string
		want         int
	}{
		{"GET", "/", address, "", 200},
		{"GET", "/", "evil.example", "", 403},
		{"POST", "/api/quit", address, "", 403},
		{"POST", "/api/quit", address, "wrong", 403},
		{"GET", "/api/state", address, "", 403},
		{"GET", "/api/state", address, "secret", 200},
		{"GET", "/api/test", address, "secret", http.StatusMethodNotAllowed},
	} {
		if got := do(c.m, c.p, c.h, c.tok); got != c.want {
			t.Errorf("%s %s host=%s tok=%q: %d want %d", c.m, c.p, c.h, c.tok, got, c.want)
		}
	}
}

// Writes every entry for a decade, for comparison with independent libraries.
func TestDump(t *testing.T) {
	path := os.Getenv("DUMP")
	if path == "" {
		t.Skip()
	}
	f, _ := os.Create(path)
	defer f.Close()
	loc, _ := time.LoadLocation("Asia/Jerusalem")
	for d := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC); d.Year() < 2036; d = d.AddDate(0, 0, 1) {
		rd := rataDie(d.Year(), d.Month(), d.Day())
		h, holy := holyDayName(rd)
		n, entry := entryFor(rd)
		if !holy {
			h = ""
		}
		if !entry {
			n = ""
		}
		var s string
		for _, c := range []string{"ירושלים", "בני ברק", "חיפה", "אילת"} {
			ci := findCity(c)
			s += fmt.Sprintf(",%s", sunsetUTC(d.Year(), d.Month(), d.Day(), ci.Lat, ci.Lon).In(loc).Format("15:04:05"))
		}
		fmt.Fprintf(f, "%s,%s,%s%s\n", d.Format("2006-01-02"), h, n, s)
	}
}

func TestEndOfShabbatAndYomTov(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Jerusalem")
	c := cfgT()
	c.Mode = "screen"
	en := entriesFrom(time.Date(2026, 11, 6, 8, 0, 0, 0, loc), c, 1)[0]
	if en.Date != "2026-11-06" || en.End.Format("2006-01-02") != "2026-11-07" {
		t.Fatalf("shabbat end %v %v", en.Date, en.End)
	}
	gap := en.End.Sub(sunsetUTC(2026, 11, 7, 31.778, 35.235).In(loc))
	if gap != 40*time.Minute {
		t.Fatalf("end minutes %v", gap)
	}
	// Erev Rosh Hashana 5787 (Friday 11.9.2026): two days of yom tov, then Shabbat? Check it runs to the last day.
	en = entriesFrom(time.Date(2026, 9, 11, 8, 0, 0, 0, loc), c, 1)[0]
	if en.End.Format("2006-01-02") != "2026-09-13" {
		t.Fatalf("rosh hashana end %v", en.End)
	}
}

func TestScreenMode(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Jerusalem")
	c := defaultConfig()
	c.Accepted = true
	c.Mode = "screen"
	if !c.valid() {
		t.Fatal("screen mode config")
	}
	en := entriesFrom(time.Date(2026, 11, 6, 8, 0, 0, 0, loc), c, 1)[0]
	now := en.Action.Add(-11 * time.Minute)
	r := &rec{}
	e := engineAt(&now, r, false)
	e.setConfig(c)
	for now.Before(en.Action.Add(10 * time.Minute)) {
		e.tick()
		if r.screens > 0 {
			e.pingScreen() // the open screen reports every 20 seconds
		}
		now = now.Add(15 * time.Second)
	}
	settle()
	if r.acts != 0 || r.screens != 1 || len(r.warns) != 3 {
		t.Fatalf("acts=%d screens=%d warns=%d", r.acts, r.screens, len(r.warns))
	}
	// Restarted mid-Shabbat: the screen comes back.
	now = en.Candle.Add(5 * time.Hour)
	r2 := &rec{}
	e2 := engineAt(&now, r2, false)
	e2.setConfig(c)
	e2.tick()
	if r2.screens != 1 {
		t.Fatal("screen not reopened after restart")
	}
	// Closed on purpose: it stays closed.
	e2.closeScreen(en.Date)
	now = now.Add(5 * time.Minute)
	e2.tick()
	if r2.screens != 1 {
		t.Fatal("screen reopened after it was closed")
	}
	// After Shabbat: nothing.
	now = en.End.Add(time.Minute)
	r3 := &rec{}
	e3 := engineAt(&now, r3, false)
	e3.setConfig(c)
	e3.tick()
	if r3.screens != 0 {
		t.Fatal("screen after the end")
	}
}
