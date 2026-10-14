package main

import (
	"fmt"
	"log"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// moveBar is the mixer's header. Pressing the left button on it asks the window
// manager to start moving the window, the same request a title bar makes
// (_NET_WM_MOVERESIZE), so Sway runs its own drag.
type moveBar struct {
	widget.BaseWidget
	window  fyne.Window
	content fyne.CanvasObject
}

func newMoveBar(window fyne.Window, content fyne.CanvasObject) *moveBar {
	b := &moveBar{window: window, content: content}
	b.ExtendBaseWidget(b)
	return b
}

func (b *moveBar) CreateRenderer() fyne.WidgetRenderer {
	background := canvas.NewRectangle(headerColor)
	return &moveBarRenderer{background: background, content: b.content}
}

func (b *moveBar) MouseDown(event *desktop.MouseEvent) {
	if event.Button != desktop.MouseButtonPrimary {
		return
	}
	native, ok := b.window.(driver.NativeWindow)
	if !ok {
		return
	}
	native.RunNative(func(context any) {
		if x11, ok := context.(driver.X11WindowContext); ok {
			if err := startWindowMove(xproto.Window(x11.WindowHandle)); err != nil {
				log.Printf("Start moving mixer: %v", err)
			}
		}
	})
}

func (b *moveBar) MouseUp(*desktop.MouseEvent) {}

type moveBarRenderer struct {
	background *canvas.Rectangle
	content    fyne.CanvasObject
}

func (r *moveBarRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	r.content.Resize(size)
}
func (r *moveBarRenderer) MinSize() fyne.Size { return r.content.MinSize() }
func (r *moveBarRenderer) Refresh()           { r.background.Refresh(); r.content.Refresh() }
func (r *moveBarRenderer) Destroy()           {}
func (r *moveBarRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.background, r.content}
}

var (
	x11Once       sync.Once
	x11Conn       *xgb.Conn
	x11MoveAtom   xproto.Atom
	x11ConnectErr error
)

// startWindowMove sends _NET_WM_MOVERESIZE with the "move" action for the button
// currently held, after releasing the pointer grab the button press created.
func startWindowMove(window xproto.Window) error {
	x11Once.Do(func() {
		x11Conn, x11ConnectErr = xgb.NewConn()
		if x11ConnectErr != nil {
			return
		}
		name := "_NET_WM_MOVERESIZE"
		reply, err := xproto.InternAtom(x11Conn, false, uint16(len(name)), name).Reply()
		if err != nil {
			x11ConnectErr = err
			return
		}
		x11MoveAtom = reply.Atom
	})
	if x11ConnectErr != nil {
		return fmt.Errorf("connect to X server: %w", x11ConnectErr)
	}
	const (
		moveAction   = 8 // _NET_WM_MOVERESIZE_MOVE
		leftButton   = 1
		sourceNormal = 1
	)
	if err := xproto.UngrabPointerChecked(x11Conn, xproto.TimeCurrentTime).Check(); err != nil {
		return err
	}
	root := xproto.Setup(x11Conn).DefaultScreen(x11Conn).Root
	pointer, err := xproto.QueryPointer(x11Conn, root).Reply()
	if err != nil {
		return err
	}
	event := xproto.ClientMessageEvent{
		Format: 32,
		Window: window,
		Type:   x11MoveAtom,
		Data: xproto.ClientMessageDataUnionData32New([]uint32{
			uint32(pointer.RootX), uint32(pointer.RootY), moveAction, leftButton, sourceNormal,
		}),
	}
	mask := uint32(xproto.EventMaskSubstructureRedirect | xproto.EventMaskSubstructureNotify)
	return xproto.SendEventChecked(x11Conn, false, root, mask, string(event.Bytes())).Check()
}
