//go:build !windows

package main

import (
	"fmt"
	"sync/atomic"
)

// Stand-ins so the logic and the page can be tested away from Windows.
var fakeAutostart atomic.Bool

func messageBox(text string) bool    { fmt.Println("WARNING:", text); return false }
func performAction(cfg Config) error { fmt.Println("ACTION:", cfg.Mode, cfg.Force); return nil }
func openBrowser(url string)         { fmt.Println("OPEN:", url) }

// openApp opens a page in its own window. Away from Windows there is none.
func openApp(url string, w, h int, fallback bool) bool {
	fmt.Println("APP:", url, w, h)
	if fallback {
		openBrowser(url)
	}
	return false
}
func autostartOn() bool          { return fakeAutostart.Load() }
func setAutostart(on bool) error { fakeAutostart.Store(on); return nil }

func uninstall()        { fmt.Println("UNINSTALL") }
func startTray(*Engine) {}
func removeTray()       {}

func openScreen(url string) { fmt.Println("SCREEN:", url) }
func keepAwake(on bool)     {}
