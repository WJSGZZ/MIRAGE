//go:build !windows

package main

import (
	"flag"
	"log"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	serversFile := flag.String("servers", defaultServersFile(), "path to servers.json")
	noBrowser := flag.Bool("no-browser", false, "do not open browser automatically")
	flag.Parse()

	if err := runDashboard(*serversFile, true, *noBrowser); err != nil {
		log.Fatalf("miragec: %v", err)
	}
}
