// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build linux

package main

// https://docs.gtk.org/gtk3/method.Window.set_icon_from_file.html

/*
#cgo linux pkg-config: gtk+-3.0

#include <stdlib.h>
#include <gtk/gtk.h>

void use_icon_file(void *hwnd, char* iconPathC) {
	gtk_window_set_icon_from_file(hwnd, iconPathC, NULL);
}

void use_icon_bytes(void *hwnd, void* iconBytes, gsize len) {
	GdkPixbufLoader* loader = gdk_pixbuf_loader_new();
    gdk_pixbuf_loader_write(loader, (guchar *)(iconBytes), len, NULL);
	gdk_pixbuf_loader_close(loader, NULL);
    GdkPixbuf* pixbuf = gdk_pixbuf_loader_get_pixbuf(loader);
	gtk_window_set_icon(hwnd, pixbuf);
	g_object_unref(loader);
}
*/
import "C"
import (
	_ "embed"
	"unsafe"

	webview "github.com/bisoncraft/webview_go"
)

//go:embed src/bisonw.png
var Icon []byte

//go:embed src/favicon-32.png
var FavIcon []byte

//go:embed src/favicon-bw-32.png
var FavIconBW []byte

func useIcon(w webview.WebView, iconPath string) {
	iconPathC := C.CString(iconPath)
	defer C.free(unsafe.Pointer(iconPathC))

	C.use_icon_file(w.Window(), iconPathC)
}

func useIconBytes(w webview.WebView, iconBytes []byte) {
	C.use_icon_bytes(w.Window(), unsafe.Pointer(&iconBytes[0]), C.gsize(len(iconBytes)))
}
