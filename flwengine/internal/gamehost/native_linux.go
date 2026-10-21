//go:build linux && !android

package gamehost

/*
#cgo pkg-config: gtk+-3.0
#include <stdlib.h>
#include <gtk/gtk.h>

// NOTE: GTK_WIN_POS_CENTER only applies at map time, so flwGtkCenter
// computes the monitor workarea and moves the window explicitly.
static void flwGtkMove(GtkWindow *w, int x, int y) {
	gtk_window_move(w, x, y);
}

static void flwGtkPos(GtkWindow *w, int *x, int *y) {
	gtk_window_get_position(w, x, y);
}

static void flwGtkFullscreen(GtkWindow *w, int on) {
	if (on) {
		gtk_window_fullscreen(w);
	} else {
		gtk_window_unfullscreen(w);
	}
}

static void flwGtkCenter(GtkWindow *w) {
	gint ww = 0, wh = 0;
	gtk_window_get_size(w, &ww, &wh);
	GtkWidget *wid = GTK_WIDGET(w);
	GdkWindow *gwin = gtk_widget_get_window(wid);
	GdkDisplay *disp = gtk_widget_get_display(wid);
	if (!disp) return;
	GdkMonitor *mon = gwin ? gdk_display_get_monitor_at_window(disp, gwin) : NULL;
	if (!mon) mon = gdk_display_get_monitor(disp, 0);
	if (!mon) return;
	GdkRectangle r;
	gdk_monitor_get_workarea(mon, &r);
	gtk_window_move(w, r.x + (r.width - ww) / 2, r.y + (r.height - wh) / 2);
}

static void flwGtkKeepAbove(GtkWindow *w, int on) {
	gtk_window_set_keep_above(w, on ? TRUE : FALSE);
}

static void flwGtkLower(GtkWindow *w) {
	GdkWindow *gwin = gtk_widget_get_window(GTK_WIDGET(w));
	if (gwin) {
		gdk_window_lower(gwin);
	}
}

// Non-blocking modal info dialog (matches the win32 side, which shows
// MessageBox from a helper goroutine): display it, destroy on response.
static void flwGtkDialog(GtkWindow *w, const char *title, const char *text) {
	GtkWidget *dlg = gtk_message_dialog_new(w, GTK_DIALOG_MODAL,
		GTK_MESSAGE_INFO, GTK_BUTTONS_OK, "%s", text);
	gtk_window_set_title(GTK_WINDOW(dlg), title);
	g_signal_connect(dlg, "response", G_CALLBACK(gtk_widget_destroy), NULL);
	gtk_widget_show_all(dlg);
}
*/
import "C"

import (
	"unsafe"
)

func nativeMoveWindow(hwnd unsafe.Pointer, x, y int) {
	if hwnd == nil {
		return
	}
	C.flwGtkMove((*C.GtkWindow)(hwnd), C.int(x), C.int(y))
}

func nativeWindowPos(hwnd unsafe.Pointer) (int, int, bool) {
	if hwnd == nil {
		return 0, 0, false
	}
	var gx, gy C.int
	C.flwGtkPos((*C.GtkWindow)(hwnd), &gx, &gy)
	return int(gx), int(gy), true
}

func nativeFullscreen(hwnd unsafe.Pointer, on bool, w, h int) {
	if hwnd == nil {
		return
	}
	if on {
		C.flwGtkFullscreen((*C.GtkWindow)(hwnd), 1)
	} else {
		C.flwGtkFullscreen((*C.GtkWindow)(hwnd), 0)
	}
}

func nativeCenterWindow(hwnd unsafe.Pointer, pos string) {
	if hwnd == nil {
		return
	}
	C.flwGtkCenter((*C.GtkWindow)(hwnd))
}

func nativeSetKeepAbove(hwnd unsafe.Pointer, on bool) {
	if hwnd == nil {
		return
	}
	C.flwGtkKeepAbove((*C.GtkWindow)(hwnd), boolToInt(on))
}

func nativeLowerWindow(hwnd unsafe.Pointer) {
	if hwnd == nil {
		return
	}
	C.flwGtkLower((*C.GtkWindow)(hwnd))
}

func boolToInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

func nativeDialog(hwnd unsafe.Pointer, title, text string) bool {
	if hwnd == nil {
		return false
	}
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	cx := C.CString(text)
	defer C.free(unsafe.Pointer(cx))
	C.flwGtkDialog((*C.GtkWindow)(hwnd), ct, cx)
	return true
}
