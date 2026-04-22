//go:build windows

package main

import (
	"flag"
	"log"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	serversFile := flag.String("servers", defaultServersFile(), "path to servers.json")
	webMode := flag.Bool("web", false, "open the dashboard in the system browser instead of app window mode")
	noBrowser := flag.Bool("no-browser", false, "do not open any browser window automatically")
	flag.Parse()

	if err := runDashboard(*serversFile, *webMode, *noBrowser); err != nil {
		showStartupError(err)
	}
}
