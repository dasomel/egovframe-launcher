// Command launcher serves a local dashboard to clone/build/run eGovFrame
// sample repositories from VSCode. Single binary, no runtime dependencies.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"egovframe-launcher/internal/runner"
	"egovframe-launcher/internal/server"
	"egovframe-launcher/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7070", "listen address")
	workspace := flag.String("workspace", ".work", "clone workspace directory")
	noOpen := flag.Bool("no-open", false, "do not open the browser")
	flag.Parse()

	ws, err := filepath.Abs(*workspace)
	if err != nil {
		log.Fatalf("workspace path: %v", err)
	}

	assets, err := fs.Sub(web.Assets, ".")
	if err != nil {
		log.Fatalf("embed assets: %v", err)
	}

	r := runner.New(ws)
	handler := server.New(r, assets)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}
	url := fmt.Sprintf("http://%s/", ln.Addr().String())
	log.Printf("eGovFrame Launcher → %s (workspace: %s)", url, ws)

	if !*noOpen {
		go func() {
			time.Sleep(300 * time.Millisecond)
			_ = runner.OpenBrowser(url)
		}()
	}
	if err := http.Serve(ln, handler); err != nil {
		log.Fatal(err)
	}
}
