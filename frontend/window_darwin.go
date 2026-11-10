package frontend

import "github.com/egoist/mygo"

// platformWindowOptions merges the title bar into the page on macOS: the
// traffic lights sit in the sidebar's top row, which is also the title bar.
func platformWindowOptions(o *mygo.WindowOptions) {
	o.TitleBarStyle = mygo.TitleBarHiddenInset
	o.TrafficLightPosition = &mygo.Point{X: 14, Y: 12}
}
