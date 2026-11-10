//go:build !darwin

package frontend

import "github.com/egoist/mygo"

// platformWindowOptions hides the system's window buttons on Windows and
// Linux: the page draws minimize, maximize and close itself, and the
// window keeps its resizing edges, shadow and rounded corners.
func platformWindowOptions(o *mygo.WindowOptions) {
	o.TitleBarStyle = mygo.TitleBarHidden
	o.HideWindowButtons = true
	o.AutoHideMenuBar = true
}
