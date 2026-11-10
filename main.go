// Command notes is the desktop notes app: a web frontend in the system's
// webview, and Go for the vault, the sync and the windows.
package main

import (
	"log"

	"notes/frontend"
)

func main() {
	if err := frontend.Run(); err != nil {
		log.Fatal(err)
	}
}
