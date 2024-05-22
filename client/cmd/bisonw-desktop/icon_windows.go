// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build windows

package main

/*
// This set_external_icon/set_resource_icon C implementation is originally from
// a proposed webview example:
// https://github.com/ldstein/webview/blob/2d7b6d32f5408b4a5e430711137aacd0a3791088/examples/icon-switching/main_windows.go

#include <windows.h>

void use_icon_file(const void *ptr, char* iconPath) {
	HICON iconBig = LoadImage(NULL, iconPath, IMAGE_ICON, GetSystemMetrics(SM_CXICON  ), GetSystemMetrics(SM_CXICON  ), LR_LOADFROMFILE);
	HICON iconSml = LoadImage(NULL, iconPath, IMAGE_ICON, GetSystemMetrics(SM_CXSMICON), GetSystemMetrics(SM_CXSMICON), LR_LOADFROMFILE);
	if (iconSml) SendMessage((HWND)ptr, WM_SETICON, ICON_SMALL, (LPARAM)iconSml);
	if (iconBig) SendMessage((HWND)ptr, WM_SETICON, ICON_BIG  , (LPARAM)iconBig);
}

void use_icon_from_resource(const void *ptr, char* name) {
	HINSTANCE hInstance = GetModuleHandle(NULL);
	HICON iconBig = (HICON)LoadImage(hInstance, name, IMAGE_ICON, GetSystemMetrics(SM_CXICON  ), GetSystemMetrics(SM_CXICON  ), LR_DEFAULTCOLOR);
	HICON iconSml = (HICON)LoadImage(hInstance, name, IMAGE_ICON, GetSystemMetrics(SM_CXSMICON), GetSystemMetrics(SM_CYSMICON), LR_DEFAULTCOLOR);
	if (iconSml) SendMessage((HWND)ptr, WM_SETICON, ICON_SMALL, (LPARAM)iconSml);
	if (iconBig) SendMessage((HWND)ptr, WM_SETICON, ICON_BIG  , (LPARAM)iconBig);
}

*/
import "C"
import (
	_ "embed"
	"os"
	"strings"
	"unsafe"

	"github.com/webview/webview"
)

//go:embed src/bisonw.png
var Icon []byte // windows doesn't use this

//go:embed src/favicon-32.ico
var FavIcon []byte

//go:embed src/favicon-bw-32.ico
var FavIconBW []byte

func fileExists(name string) bool {
	_, err := os.Stat(name)
	return !os.IsNotExist(err)
}

func useIcon(w webview.WebView, iconPath string) {
	iconPathC := C.CString(iconPath)
	defer C.free(unsafe.Pointer(iconPathC))

	if strings.HasPrefix(iconPath, "#") || !fileExists(iconPath) { // numbered or named resource in winres.json
		C.use_icon_from_resource(w.Window(), iconPathC)
	} else {
		C.use_icon_file(w.Window(), iconPathC)
	}
}

func useIconBytes(w webview.WebView, iconBytes []byte) {}
