package main

import (
	"fmt"
	"sync"
	"time"
)

// Minutes before the action, narrowest first: a late start shows one popup, not three.
var warnAt = []int{1, 10, 30}

// Someone who switches the computer on inside the window still gets this much time.
const lateGrace = 3 * time.Minute

type Engine struct {
	mu       sync.Mutex
	cfg      Config
	warned   map[string]bool
	done     map[string]bool
	now      func() time.Time
	warn     func(notice) bool // returns true when the person cancels
	act      func(Config) error
	save     func(Config) error
	lastErr  string
	actAfter map[string]time.Time

	// Screen mode: the computer stays on behind a "שבת שלום" screen
	screen       func(url string)
	awake        func(on bool)
	screenOpened time.Time
	screenPing   time.Time
	screenClosed map[string]bool
}

func newEngine(cfg Config) *Engine {
	return &Engine{cfg: cfg, warned: map[string]bool{}, done: map[string]bool{}, actAfter: map[string]time.Time{}, screenClosed: map[string]bool{}, now: time.Now,
		screen: openScreen, awake: keepAwake,
		warn: showNotice, act: performAction, save: saveConfig}
}

func (e *Engine) config() Config { e.mu.Lock(); defer e.mu.Unlock(); return e.cfg }

func (e *Engine) setConfig(c Config) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = c
	return e.save(c)
}

func (e *Engine) setSkip(date string) error {
	c := e.config()
	c.SkipDate = date
	return e.setConfig(c)
}

// notice is one warning before the action.
type notice struct {
	Name, Date, Mode, Text string
	At                     time.Time
	Test                   bool
}

func newNotice(name, date, mode string, at time.Time, minutes int) notice {
	return notice{Name: name, Date: date, Mode: mode, At: at,
		Text: fmt.Sprintf("%s: בעוד %d דקות המחשב %s.\n\nכדאי לשמור עכשיו את כל העבודה.\n\nלבטל את הפעולה להיום?", name, minutes, actionWord(mode))}
}

func actionWord(mode string) string {
	if mode == "screen" {
		return "יציג שומר מסך של שבת שלום"
	}
	if mode == "sleep" {
		return "יעבור למצב שינה"
	}
	return "ייכבה"
}

// tick decides what is due now. It only runs between the action time and candle lighting,
// so a computer that wakes up after candle lighting is never switched off by surprise.
func (e *Engine) tick() {
	now := e.now()
	cfg := e.config()
	if !cfg.Enabled || !cfg.Accepted || cfg.Mode != "screen" {
		e.awake(false)
	}
	if !cfg.Enabled || !cfg.Accepted {
		return
	}
	list := entriesFrom(now, cfg, 1)
	if len(list) == 0 || list[0].Skipped {
		return
	}
	en := list[0]
	left := en.Action.Sub(now)
	if cfg.Mode == "screen" {
		e.tickScreen(now, en)
		if left <= 0 {
			return
		}
	}
	if left > 0 {
		for _, m := range warnAt {
			key := fmt.Sprintf("%s-%d", en.Date, m)
			e.mu.Lock()
			due := left <= time.Duration(m)*time.Minute && !e.warned[key]
			if due {
				for _, w := range warnAt {
					if w >= m {
						e.warned[fmt.Sprintf("%s-%d", en.Date, w)] = true
					}
				}
			}
			e.mu.Unlock()
			if due {
				date := en.Date
				n := newNotice(en.Name, en.Date, cfg.Mode, en.Action, int(left.Minutes())+1)
				go func() {
					if e.warn(n) {
						e.setSkip(date)
					}
				}()
				break
			}
		}
		return
	}
	// Inside the window. Nobody saw the last warning if the computer was just switched
	// on or woken, so warn now and wait rather than shutting down without notice.
	last := fmt.Sprintf("%s-%d", en.Date, warnAt[0])
	e.mu.Lock()
	warned := e.warned[last]
	if !warned {
		for _, w := range warnAt {
			e.warned[fmt.Sprintf("%s-%d", en.Date, w)] = true
		}
		e.actAfter[en.Date] = now.Add(lateGrace)
	}
	wait := e.actAfter[en.Date]
	ready := !now.Before(wait) && !e.done[en.Date]
	if ready {
		e.done[en.Date] = true
	}
	e.mu.Unlock()
	if !warned {
		date := en.Date
		n := newNotice(en.Name, en.Date, cfg.Mode, wait, int(lateGrace.Minutes()))
		go func() {
			if e.warn(n) {
				e.setSkip(date)
			}
		}()
		return
	}
	if ready {
		if err := e.act(cfg); err != nil {
			e.mu.Lock()
			e.lastErr = err.Error()
			e.mu.Unlock()
		}
	}
}

func (e *Engine) run() {
	for {
		e.tick()
		time.Sleep(15 * time.Second)
	}
}

// screenQuiet is how long the screen may go without word before it is opened again,
// for example after the computer restarted during Shabbat.
const screenQuiet = 75 * time.Second

func (e *Engine) tickScreen(now time.Time, en Entry) {
	active := !now.Before(en.Action) && now.Before(en.End)
	e.awake(active)
	if !active {
		return
	}
	e.mu.Lock()
	closed := e.screenClosed[en.Date]
	quiet := now.Sub(e.screenPing) > screenQuiet && now.Sub(e.screenOpened) > screenQuiet
	if !closed && quiet {
		e.screenOpened = now
	}
	e.mu.Unlock()
	if !closed && quiet {
		e.screen("http://" + address + "/screen")
	}
}

func (e *Engine) pingScreen() {
	e.mu.Lock()
	e.screenPing = e.now()
	e.mu.Unlock()
}

func (e *Engine) closeScreen(date string) {
	e.mu.Lock()
	e.screenClosed[date] = true
	e.mu.Unlock()
}
