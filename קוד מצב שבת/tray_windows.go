//go:build windows

package main

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// The icon beside the clock. If anything here fails the program keeps working;
// the settings can still be opened by starting it again.
const (
	wmDestroy  = 0x0002
	wmCommand  = 0x0111
	wmTrayIcon = 0x0400 + 1
	wmLButton  = 0x0202
	wmRButton  = 0x0205
	nimAdd     = 0
	nimDelete  = 2
	nifMessage = 1
	nifIcon    = 2
	nifTip     = 4
	mfString   = 0
	mfSep      = 0x800
	tpmReturn  = 0x0100
	tpmRTL     = 0x0008 // TPM_RIGHTALIGN
	cmdOpen    = 1
	cmdSkip    = 2
	cmdQuit    = 3
)

type wndClassEx struct {
	size       uint32
	style      uint32
	proc       uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

type notifyIconData struct {
	size        uint32
	wnd         uintptr
	id          uint32
	flags       uint32
	callback    uint32
	icon        uintptr
	tip         [128]uint16
	state       uint32
	stateMask   uint32
	info        [256]uint16
	version     uint32
	infoTitle   [64]uint16
	infoFlags   uint32
	guid        [16]byte
	balloonIcon uintptr
}

type point struct{ x, y int32 }

type msg struct {
	wnd     uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

var (
	u32            = syscall.NewLazyDLL("user32.dll")
	shell          = syscall.NewLazyDLL("shell32.dll")
	kernel         = syscall.NewLazyDLL("kernel32.dll")
	pRegisterClass = u32.NewProc("RegisterClassExW")
	pCreateWindow  = u32.NewProc("CreateWindowExW")
	pDefWindowProc = u32.NewProc("DefWindowProcW")
	pGetMessage    = u32.NewProc("GetMessageW")
	pTranslate     = u32.NewProc("TranslateMessage")
	pDispatch      = u32.NewProc("DispatchMessageW")
	pLoadIcon      = u32.NewProc("LoadIconW")
	pCreateMenu    = u32.NewProc("CreatePopupMenu")
	pAppendMenu    = u32.NewProc("AppendMenuW")
	pTrackMenu     = u32.NewProc("TrackPopupMenu")
	pDestroyMenu   = u32.NewProc("DestroyMenu")
	pGetCursor     = u32.NewProc("GetCursorPos")
	pForeground    = u32.NewProc("SetForegroundWindow")
	pNotify        = shell.NewProc("Shell_NotifyIconW")
	pModuleHandle  = kernel.NewProc("GetModuleHandleW")
	trayMu         sync.Mutex
	trayData       notifyIconData
	trayLive       bool
	trayEngine     *Engine
)

func appendItem(menu uintptr, id int, text string) {
	p, _ := syscall.UTF16PtrFromString(text)
	pAppendMenu.Call(menu, mfString, uintptr(id), uintptr(unsafe.Pointer(p)))
}

func showTrayMenu(wnd uintptr) {
	menu, _, _ := pCreateMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	appendItem(menu, cmdOpen, "פתיחת "+appTitle())
	appendItem(menu, cmdSkip, "לא לפעול הפעם")
	pAppendMenu.Call(menu, mfSep, 0, 0)
	appendItem(menu, cmdQuit, "סגירת התוכנה")
	var pt point
	pGetCursor.Call(uintptr(unsafe.Pointer(&pt)))
	pForeground.Call(wnd)
	r, _, _ := pTrackMenu.Call(menu, tpmReturn|tpmRTL, uintptr(pt.x), uintptr(pt.y), 0, wnd, 0)
	switch r {
	case cmdOpen:
		openMain()
	case cmdSkip:
		if list := entriesFrom(trayEngine.now(), trayEngine.config(), 1); len(list) > 0 {
			trayEngine.setSkip(list[0].Date)
		}
	case cmdQuit:
		removeTray()
		exitProgram()
	}
}

func trayProc(wnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if message == wmTrayIcon {
		switch uint32(lParam) & 0xffff {
		case wmLButton:
			openMain()
		case wmRButton:
			showTrayMenu(wnd)
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(wnd, uintptr(message), wParam, lParam)
	return r
}

func startTray(e *Engine) {
	trayEngine = e
	go func() {
		runtime.LockOSThread()
		instance, _, _ := pModuleHandle.Call(0)
		name, _ := syscall.UTF16PtrFromString(appID() + "Tray")
		class := wndClassEx{proc: syscall.NewCallback(trayProc), instance: instance, className: name}
		class.size = uint32(unsafe.Sizeof(class))
		if r, _, _ := pRegisterClass.Call(uintptr(unsafe.Pointer(&class))); r == 0 {
			return
		}
		wnd, _, _ := pCreateWindow.Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)),
			0, 0, 0, 0, 0, 0, 0, instance, 0)
		if wnd == 0 {
			return
		}
		icon, _, _ := pLoadIcon.Call(instance, 1) // resource id 1 in the program file
		if icon == 0 {
			icon, _, _ = pLoadIcon.Call(0, 32512) // IDI_APPLICATION
		}
		trayMu.Lock()
		trayData = notifyIconData{wnd: wnd, id: 1, flags: nifMessage | nifIcon | nifTip, callback: wmTrayIcon, icon: icon}
		trayData.size = uint32(unsafe.Sizeof(trayData))
		tip, _ := syscall.UTF16FromString(appTitle() + " | יהודי אחד ומיוחד")
		copy(trayData.tip[:], tip)
		r, _, _ := pNotify.Call(nimAdd, uintptr(unsafe.Pointer(&trayData)))
		trayLive = r != 0
		trayMu.Unlock()
		var m msg
		for {
			r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if r == 0 || r == ^uintptr(0) {
				return
			}
			pTranslate.Call(uintptr(unsafe.Pointer(&m)))
			pDispatch.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
}

func removeTray() {
	trayMu.Lock()
	defer trayMu.Unlock()
	if trayLive {
		pNotify.Call(nimDelete, uintptr(unsafe.Pointer(&trayData)))
		trayLive = false
	}
}
