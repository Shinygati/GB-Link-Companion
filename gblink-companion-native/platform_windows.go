//go:build windows

package main

import (
	"fmt"
	"golang.org/x/sys/windows/registry"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	pPostMessage              = user32.NewProc("PostMessageW")
	pEnumWindows              = user32.NewProc("EnumWindows")
	pGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	closePID                  uint32 // the process whose windows askClose is closing
	closeCb                   = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid != 0 && pid == atomic.LoadUint32(&closePID) {
			pPostMessage.Call(hwnd, 0x0010, 0, 0) // WM_CLOSE: the same as its close button
		}
		return 1
	})
	jobOnce   sync.Once
	jobHandle windows.Handle
)

// askClose closes mGBA's windows the way the close button does, so it quits cleanly.
func askClose(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	atomic.StoreUint32(&closePID, uint32(cmd.Process.Pid))
	pEnumWindows.Call(closeCb, 0)
}

// tieToApp puts mGBA in a job that ends when this app does, even if the app is killed or crashes.
func tieToApp(cmd *exec.Cmd) {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			windows.CloseHandle(h)
			return
		}
		jobHandle = h
	})
	if jobHandle == 0 || cmd.Process == nil {
		return
	}
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(ph)
	windows.AssignProcessToJobObject(jobHandle, ph)
}

var (
	comdlg32           = syscall.NewLazyDLL("comdlg32.dll")
	shell32            = syscall.NewLazyDLL("shell32.dll")
	ole32              = syscall.NewLazyDLL("ole32.dll")
	user32             = syscall.NewLazyDLL("user32.dll")
	pGetOpenFileName   = comdlg32.NewProc("GetOpenFileNameW")
	pCommDlgError      = comdlg32.NewProc("CommDlgExtendedError")
	pGetSaveFileName   = comdlg32.NewProc("GetSaveFileNameW")
	pSHBrowseForFolder = shell32.NewProc("SHBrowseForFolderW")
	pSHGetPathFromID   = shell32.NewProc("SHGetPathFromIDListW")
	pCoInitializeEx    = ole32.NewProc("CoInitializeEx")
	pCoTaskMemFree     = ole32.NewProc("CoTaskMemFree")
	pGetForeground     = user32.NewProc("GetForegroundWindow")
	pMessageBox        = user32.NewProc("MessageBoxW")
)

type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	Flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	FlagsEx           uint32
}

type browseInfo struct {
	hwndOwner      uintptr
	pidlRoot       uintptr
	pszDisplayName *uint16
	lpszTitle      *uint16
	ulFlags        uint32
	lpfn           uintptr
	lParam         uintptr
	iImage         int32
}

func utf16z(s string) []uint16 { u, _ := syscall.UTF16FromString(s); return u }

// filter: "GBA games (*.gba)\0*.gba\0All files\0*.*\0\0"
func filterString(name, pattern string) []uint16 {
	var f []uint16
	for _, s := range []string{name, pattern, "All files", "*.*"} {
		f = append(f, utf16z(s)...)
	}
	return append(f, 0)
}

// pickFile shows the Windows "Open" dialog. Returns "" if cancelled.
func pickFile(title, filterName, pattern, initialDir string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	buf := make([]uint16, 4096)
	filter := filterString(filterName, pattern)
	t := utf16z(title)
	var dir []uint16
	ofn := openFileName{
		lpstrFilter: &filter[0], nFilterIndex: 1, lpstrFile: &buf[0], nMaxFile: uint32(len(buf)),
		lpstrTitle: &t[0], Flags: 0x1000 | 0x800 | 0x8 | 0x80000, // FILEMUSTEXIST PATHMUSTEXIST NOCHANGEDIR EXPLORER
	}
	if initialDir != "" && initialDir != "." {
		dir = utf16z(initialDir)
		ofn.lpstrInitialDir = &dir[0]
	}
	ofn.hwndOwner, _, _ = pGetForeground.Call()
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := pGetOpenFileName.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		if code, _, _ := pCommDlgError.Call(); code != 0 {
			return "", fmt.Errorf("the file picker didn't open (error %#x)", code)
		}
		return "", nil // cancelled
	}
	return syscall.UTF16ToString(buf), nil
}

// pickFiles shows the Windows "Open" dialog with more than one file allowed. Returns nil if cancelled.
func pickFiles(title, filterName, pattern, initialDir string) ([]string, error) {
	if p := os.Getenv("GBLINK_TEST_PICK"); p != "" { // tests
		return []string{p}, nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	buf := make([]uint16, 1<<16)
	filter := filterString(filterName, pattern)
	t := utf16z(title)
	var dir []uint16
	ofn := openFileName{
		lpstrFilter: &filter[0], nFilterIndex: 1, lpstrFile: &buf[0], nMaxFile: uint32(len(buf)),
		lpstrTitle: &t[0], Flags: 0x1000 | 0x800 | 0x8 | 0x80000 | 0x200, // ... EXPLORER ALLOWMULTISELECT
	}
	if initialDir != "" && initialDir != "." {
		dir = utf16z(initialDir)
		ofn.lpstrInitialDir = &dir[0]
	}
	ofn.hwndOwner, _, _ = pGetForeground.Call()
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := pGetOpenFileName.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		if code, _, _ := pCommDlgError.Call(); code != 0 {
			return nil, fmt.Errorf("the file picker didn't open (error %#x)", code)
		}
		return nil, nil // cancelled
	}
	return splitMultiSelect(buf), nil
}

// splitMultiSelect: the dialog's answer, "C:\path\file.wc3\0\0" for one file or
// "C:\folder\0a.wc3\0b.wc3\0\0" for more.
func splitMultiSelect(buf []uint16) []string {
	var parts []string
	start := 0
	for i, c := range buf {
		if c != 0 {
			continue
		}
		if i == start {
			break
		}
		parts = append(parts, syscall.UTF16ToString(buf[start:i]))
		start = i + 1
	}
	if len(parts) <= 1 {
		return parts
	}
	var out []string
	for _, f := range parts[1:] {
		out = append(out, filepath.Join(parts[0], f))
	}
	return out
}

// pickFolder shows the Windows folder picker. Returns "" if cancelled.
func pickFolder(title string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pCoInitializeEx.Call(0, 2) // apartment threaded
	name := make([]uint16, 260)
	t := utf16z(title)
	bi := browseInfo{pszDisplayName: &name[0], lpszTitle: &t[0], ulFlags: 0x1 | 0x40 | 0x200}
	bi.hwndOwner, _, _ = pGetForeground.Call()
	pidl, _, _ := pSHBrowseForFolder.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", nil
	}
	defer pCoTaskMemFree.Call(pidl)
	path := make([]uint16, 1024)
	pSHGetPathFromID.Call(pidl, uintptr(unsafe.Pointer(&path[0])))
	return syscall.UTF16ToString(path), nil
}

func showInFolder(path string) {
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		exec.Command("explorer", path).Start()
		return
	}
	exec.Command("explorer", "/select,", path).Start()
}

func browserCandidates() []string {
	var c []string
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
		if b := os.Getenv(env); b != "" {
			c = append(c, filepath.Join(b, "Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join(b, "Google", "Chrome", "Application", "chrome.exe"))
		}
	}
	return c
}

func openDefault(url string) { exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start() }

func fatal(msg string) {
	t, m := utf16z("GB-Link Companion"), utf16z(msg)
	pMessageBox.Call(0, uintptr(unsafe.Pointer(&m[0])), uintptr(unsafe.Pointer(&t[0])), 0x10)
	os.Exit(1)
}

// osDarkMode: Windows' "app mode" setting (Settings > Personalisation > Colours).
func osDarkMode() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

// pickSaveFile shows the Windows "Save as" dialog. Returns "" if cancelled.
func pickSaveFile(title, dir, name string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	buf := make([]uint16, 4096)
	copy(buf, utf16z(name))
	filter := filterString("GBA games (*.gba)", "*.gba")
	t, d, ext := utf16z(title), utf16z(dir), utf16z("gba")
	ofn := openFileName{
		lpstrFilter: &filter[0], nFilterIndex: 1, lpstrFile: &buf[0], nMaxFile: uint32(len(buf)),
		lpstrTitle: &t[0], lpstrInitialDir: &d[0], lpstrDefExt: &ext[0],
		Flags: 0x2 | 0x800 | 0x8 | 0x80000, // OVERWRITEPROMPT PATHMUSTEXIST NOCHANGEDIR EXPLORER
	}
	ofn.hwndOwner, _, _ = pGetForeground.Call()
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := pGetSaveFileName.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		if code, _, _ := pCommDlgError.Call(); code != 0 {
			return "", fmt.Errorf("the file picker didn't open (error %#x)", code)
		}
		return "", nil
	}
	return syscall.UTF16ToString(buf), nil
}
