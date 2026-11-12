// Command notes is the desktop notes app: a web frontend in the system's
// webview, and Go for the vault, the sync and the windows.
package main

import (
	"log"
	"os"

	"notes/frontend"
)

func main() {
	// --mcp-stdio carries a client's stdio to the MCP server of the running app.
	// It starts no window, so it is checked before the app is.
	if len(os.Args) > 1 && os.Args[1] == "--mcp-stdio" {
		if err := frontend.RunMCPStdio(os.Stdin, os.Stdout, os.Stderr); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := frontend.Run(); err != nil {
		log.Fatal(err)
	}
}
