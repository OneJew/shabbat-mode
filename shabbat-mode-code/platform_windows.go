//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	runKey  = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	runName = "ShabbatMode"

	mbYesNo      = 0x00000004
	mbIconWarn   = 0x00000030
	mbTopmost    = 0x00040000
	mbSetFore    = 0x00010000
	mbRight      = 0x00080000
	mbRTL        = 0x00100000
	mbDefButton2 = 0x00000100
	idYes        = 6
)

var pMessageBox = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")

func hidden(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return c
}

// messageBox is the plain fallback warning. It returns true when the person cancels.
func messageBox(text string) bool {
	body, _ := syscall.UTF16PtrFromString(text)
	title, _ := syscall.UTF16PtrFromString("מצב שבת")
	r, _, _ := pMessageBox.Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)),
		mbYesNo|mbIconWarn|mbTopmost|mbSetFore|mbRight|mbRTL|mbDefButton2)
	return r == idYes
}

func performAction(cfg Config) error {
	if cfg.Mode == "sleep" {
		return hidden("rundll32.exe", "powrprof.dll,SetSuspendState", "0,1,0").Run()
	}
	args := []string{"/s", "/t", "0"}
	if cfg.Force {
		args = append(args, "/f")
	}
	return hidden("shutdown.exe", args...).Run()
}

func edgePath() string {
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
		if base := os.Getenv(env); base != "" {
			p := filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// openApp shows a page in its own window, with no address bar or tabs, using the Edge
// that comes with Windows. With its own profile it never mixes with the person's browsing.
func openApp(url string, w, h int, fallback bool) bool {
	edge := edgePath()
	if edge != "" {
		profile := filepath.Join(filepath.Dir(installedPath()), "window")
		c := exec.Command(edge, "--app="+url, "--user-data-dir="+profile, "--no-first-run",
			"--no-default-browser-check", "--disable-features=Translate",
			"--autoplay-policy=no-user-gesture-required", fmt.Sprintf("--window-size=%d,%d", w, h))
		if c.Start() == nil {
			return true
		}
	}
	if fallback {
		openBrowser(url)
	}
	return false
}

func openBrowser(url string) {
	hidden("rundll32.exe", "url.dll,FileProtocolHandler", url).Start()
}

func autostartOn() bool {
	return hidden("reg.exe", "query", runKey, "/v", runName).Run() == nil
}

func installedPath() string {
	dir, err := os.UserCacheDir() // %LocalAppData%
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ShabbatMode", "ShabbatMode.exe")
}

// install keeps a copy in a fixed place, so autostart survives moving or deleting the download.
func install() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	target := installedPath()
	if target == "" || strings.EqualFold(exe, target) {
		return exe, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return exe, nil
	}
	in, err := os.Open(exe)
	if err != nil {
		return exe, nil
	}
	defer in.Close()
	tmp := target + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return exe, nil
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err != nil || cerr != nil {
		os.Remove(tmp)
		return exe, nil
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return exe, nil
	}
	return target, nil
}

func setAutostart(on bool) error {
	if !on {
		if autostartOn() {
			return hidden("reg.exe", "delete", runKey, "/v", runName, "/f").Run()
		}
		return nil
	}
	exe, err := install()
	if err != nil {
		return err
	}
	value := `"` + exe + `" --background`
	return hidden("reg.exe", "add", runKey, "/v", runName, "/t", "REG_SZ", "/d", value, "/f").Run()
}

// uninstall removes autostart, settings and the installed copy.
func uninstall() {
	setAutostart(false)
	os.RemoveAll(filepath.Dir(configPath()))
	if p := installedPath(); p != "" {
		dir := filepath.Dir(p)
		// The running program cannot delete itself, so wait a moment and remove it from outside.
		hidden("cmd.exe", "/c", "ping -n 4 127.0.0.1 >nul & rmdir /s /q \""+dir+"\"").Start()
	}
}
