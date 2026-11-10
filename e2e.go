//go:build e2e

package main

// Builds with -tags e2e can be driven from outside through MyGo's automation
// plugin (MYGO_AUTOMATION=<socket>). Release builds leave it out.
import _ "github.com/egoist/mygo/plugins/automation"
