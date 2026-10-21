//go:build windows



package gdi

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var (
	errWine          = errorsNew("gdi effects disabled under wine")
	errNoWindow      = errorsNew("gdi window not ready")
	errUnknownEffect = errorsNew("unknown gdi effect")
)

type point struct {
	X, Y int32
}

type rectW struct {
	Left, Top, Right, Bottom int32
}

type msgW struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassExW struct {
	Size         uint32
	Style        uint32
	WndProc      uintptr
	ClsExtra     int32
	WndExtra     int32
	Instance     uintptr
	Icon         uintptr
	Cursor       uintptr
	Background   uintptr
	MenuName     *uint16
	ClassName    *uint16
	IconSm       uintptr
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type blendFunction struct {
	Op, Flags, Alpha, Format byte
}

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	gdi32                   = syscall.NewLazyDLL("gdi32.dll")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
	procPeekMessageW        = user32.NewProc("PeekMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procUpdateLayeredWindow = user32.NewProc("UpdateLayeredWindow")
	procGetDC               = user32.NewProc("GetDC")
	procGetModuleHandleW    = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	procCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
)

const (
	wsPopup       = 0x80000000
	wsExLayered   = 0x00080000
	wsExTransient = 0x00000020
	wsExTopmost   = 0x00000008
	wsExToolWin   = 0x00000080
	wsExNoAct     = 0x08000000
	ulwAlpha      = 2
	acSrcAlpha    = 1
	dibRGBColors  = 0
	biRGB         = 0
	swHide        = 0
	swShowNA      = 8
	hwndTopmost   = ^uintptr(1)
	swPNoSize     = 0x0001
	swPNoActivate = 0x0010
	wmQuit        = 0x0012
	pmRemove      = 1
	smXVScreen    = 76
	smYVScreen    = 77
	smCXVScreen   = 78
	smCYVScreen   = 79
)

type dibBuffer struct {
	srcDC  uintptr
	memDC  uintptr
	bmp    uintptr
	oldBmp uintptr
	bits   []byte
	w      int
	h      int
}

type windowCtx struct {
	hwnd     uintptr
	target   uintptr
	dib      *dibBuffer
	anim     animState
	shown    bool
}

func wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

var wndProcCb = syscall.NewCallback(wndProc)

func (m *Manager) Start(targetHwnd uintptr) bool {
	if m.wine {
		return false
	}
	go func() {
		runtime.LockOSThread()
		m.runThread(targetHwnd)
		runtime.UnlockOSThread()
	}()
	for i := 0; i < 50; i++ {
		m.mu.Lock()
		live := m.live
		m.mu.Unlock()
		if live {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func (m *Manager) runThread(targetHwnd uintptr) {
	className, _ := syscall.UTF16PtrFromString("flwgdiwnd")
	inst, _, _ := procGetModuleHandleW.Call(0)
	wc := &wndClassExW{
		Size:      uint32(unsafe.Sizeof(wndClassExW{})),
		WndProc:   wndProcCb,
		ClassName: className,
		Instance:  inst,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(wc)))
	title, _ := syscall.UTF16PtrFromString("flwgdi")
	exStyle := wsExLayered | wsExTransient | wsExTopmost | wsExToolWin | wsExNoAct
	hwnd, _, _ := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		uintptr(wsPopup),
		uintptr(uint32(0xFFF82BE0)), uintptr(uint32(0xFFF82BE0)), 4, 4,
		0, 0, inst, 0,
	)
	if hwnd == 0 {
		return
	}
	ctx := &windowCtx{hwnd: hwnd, target: targetHwnd}
	m.mu.Lock()
	m.live = true
	m.mu.Unlock()
	m.stopped.Add(1)
	defer m.stopped.Done()
	defer func() {
		procDestroyWindow.Call(hwnd)
		if ctx.dib != nil {
			ctx.dib.release()
		}
		m.mu.Lock()
		m.live = false
		m.mu.Unlock()
	}()
	var msg msgW
	last := time.Now()
	for {
		for {
			ret, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, pmRemove)
			if ret == 0 {
				break
			}
			if msg.Message == wmQuit {
				return
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}
		select {
		case <-m.quit:
			return
		case j := <-m.jobs:
			e := m.applyJob(&ctx.anim, j)
			select {
			case m.acks <- e:
			default:
			}
		default:
		}
		now := time.Now()
		dt := float64(now.Sub(last).Microseconds()) / 1000.0
		last = now
		dirty := m.tick(&ctx.anim, dt)
		select {
		case <-m.quit:
			return
		default:
		}
		if dirty {
			m.render(ctx)
		}
		if !ctx.anim.active && ctx.anim.curA == 0 && !ctx.anim.hold && ctx.shown {
			procShowWindow.Call(ctx.hwnd, swHide)
			ctx.shown = false
		}
		time.Sleep(8 * time.Millisecond)
	}
}

func (m *Manager) render(ctx *windowCtx) {
	var bounds rectW
	boundsOk := false
	if ctx.target != 0 {
		if r, _, _ := procGetWindowRect.Call(ctx.target, uintptr(unsafe.Pointer(&bounds))); r != 0 {
			boundsOk = true
		}
	}
	if !boundsOk {
		x0, _, _ := procGetSystemMetrics.Call(smXVScreen)
		y0, _, _ := procGetSystemMetrics.Call(smYVScreen)
		w, _, _ := procGetSystemMetrics.Call(smCXVScreen)
		h, _, _ := procGetSystemMetrics.Call(smCYVScreen)
		bounds = rectW{Left: int32(x0), Top: int32(y0), Right: int32(x0 + w), Bottom: int32(y0 + h)}
	}
	w := int(bounds.Right - bounds.Left)
	h := int(bounds.Bottom - bounds.Top)
	if w < 4 || h < 4 {
		return
	}
	if err := m.ensureDib(ctx, w, h); err != nil {
		return
	}
	a := ctx.anim
	if a.curA == 0 {
		if ctx.shown {
			procShowWindow.Call(ctx.hwnd, swHide)
			ctx.shown = false
		}
		return
	}
	premB := uint32(a.b) * uint32(a.curA) / 255
	premG := uint32(a.g) * uint32(a.curA) / 255
	premR := uint32(a.r) * uint32(a.curA) / 255
	b0, g0, r0, al := byte(premB), byte(premG), byte(premR), a.curA
	bits := ctx.dib.bits
	for i := 0; i < len(bits); i += 4 {
		bits[i] = b0
		bits[i+1] = g0
		bits[i+2] = r0
		bits[i+3] = al
	}
	ptDst := point{X: bounds.Left, Y: bounds.Top}
	size := point{X: int32(w), Y: int32(h)}
	ptSrc := point{X: 0, Y: 0}
	blend := blendFunction{Op: 0, Flags: 0, Alpha: al, Format: acSrcAlpha}
	if !ctx.shown {
		procShowWindow.Call(ctx.hwnd, swShowNA)
		ctx.shown = true
	}
	procSetWindowPos.Call(ctx.hwnd, hwndTopmost, uintptr(ptDst.X), uintptr(ptDst.Y), uintptr(w), uintptr(h), swPNoSize|swPNoActivate)
	procUpdateLayeredWindow.Call(
		ctx.hwnd,
		0,
		uintptr(unsafe.Pointer(&ptDst)),
		uintptr(unsafe.Pointer(&size)),
		ctx.dib.memDC,
		uintptr(unsafe.Pointer(&ptSrc)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		ulwAlpha,
	)
}

func (m *Manager) ensureDib(ctx *windowCtx, w, h int) error {
	if ctx.dib != nil && ctx.dib.w == w && ctx.dib.h == h {
		return nil
	}
	if ctx.dib != nil {
		ctx.dib.release()
		ctx.dib = nil
	}
	srcDC, _, _ := procGetDC.Call(0)
	if srcDC == 0 {
		return errNoWindow
	}
	memDC, _, _ := procCreateCompatibleDC.Call(srcDC)
	if memDC == 0 {
		procReleaseDC.Call(0, srcDC)
		return errNoWindow
	}
	var bmi struct {
		Header bitmapInfoHeader
	}
	bmi.Header.Size = uint32(unsafe.Sizeof(bmi.Header))
	bmi.Header.Width = int32(w)
	bmi.Header.Height = int32(-h)
	bmi.Header.Planes = 1
	bmi.Header.BitCount = 32
	bmi.Header.Compression = biRGB
	var bits uintptr
	bmp, _, _ := procCreateDIBSection.Call(
		memDC,
		uintptr(unsafe.Pointer(&bmi)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0, 0,
	)
	if bmp == 0 {
		procDeleteDC.Call(memDC)
		procReleaseDC.Call(0, srcDC)
		return errNoWindow
	}
	oldBmp, _, _ := procSelectObject.Call(memDC, bmp)
	ctx.dib = &dibBuffer{
		srcDC:  srcDC,
		memDC:  memDC,
		bmp:    bmp,
		oldBmp: oldBmp,
		bits:   unsafe.Slice((*byte)(unsafe.Pointer(bits)), w*h*4),
		w:      w,
		h:      h,
	}
	return nil
}

func (d *dibBuffer) release() {
	if d == nil {
		return
	}
	procSelectObject.Call(d.memDC, d.oldBmp)
	procDeleteObject.Call(d.bmp)
	procDeleteDC.Call(d.memDC)
	procReleaseDC.Call(0, d.srcDC)
}

func (m *Manager) Stop() {
	select {
	case <-m.quit:
		return
	default:
	}
	close(m.quit)
	m.stopped.Wait()
}
