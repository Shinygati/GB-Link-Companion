//go:build windows

package main

import (
	"fmt"
	"golang.org/x/sys/windows/registry"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

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
