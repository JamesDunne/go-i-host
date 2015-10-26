package main

import (
	"flag"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"path"
)

import "github.com/JamesDunne/go-util/base"
import "github.com/JamesDunne/go-util/web"
import "github.com/JamesDunne/go-i-host/base62"

import _ "net/http/pprof"

var (
	base_folder = "."
	xrGif       = "/p-g/"
	xrThumb     = "/p-t/"
)

const thumbnail_dimensions = 200

func html_path() string    { return base_folder + "/html" }
func db_path() string      { return base_folder + "/sqlite.db" }
func store_folder() string { return base_folder + "/store" }
func thumb_folder() string { return base_folder + "/thumb" }
func tmp_folder() string   { return base_folder + "/tmp" }

var uiTmpl *template.Template
var b62 *base62.Encoder = base62.NewEncoderOrPanic(base62.ShuffledAlphabet)

func main() {
	// Define our commandline flags:
	fs := flag.String("fs", ".", "Root directory of served files and templates")
	xrGifArg := flag.String("xrg", "", "X-Accel-Redirect header prefix for serving images or blank to disable")
	xrThumbArg := flag.String("xrt", "", "X-Accel-Redirect header prefix for serving thumbnails or blank to disable")

	fl_listen_uri := flag.String("l", "tcp://0.0.0.0:8080", "listen URI (schemes available are tcp, unix)")
	flag.Parse()

	// Parse all the URIs:
	listen_addr, err := base.ParseListenable(*fl_listen_uri)
	base.PanicIf(err)

	// Parse the flags and set values:
	flag.Parse()

	// Make directories we need:
	base_folder = base.CanonicalPath(path.Clean(*fs))
	os.MkdirAll(store_folder(), 0775)
	os.MkdirAll(thumb_folder(), 0775)
	os.MkdirAll(tmp_folder(), 0775)

	xrGif = *xrGifArg
	xrThumb = *xrThumbArg

	// Create/update the DB schema if needed:
	api, err := NewAPI()
	if err != nil {
		log.Fatal(err)
		return
	}
	api.Close()

	// Watch the html templates for changes and reload them:
	_, cleanup, err := web.WatchTemplates("ui", html_path(), "*.html", nil, &uiTmpl)
	if err != nil {
		log.Println(err)
		return
	}
	defer cleanup()

	// Start profiler:
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	// Start the server:
	_, err = base.ServeMain(listen_addr, func(l net.Listener) error {
		return http.Serve(l, web.ReportErrors(web.Log(web.DefaultErrorLog, web.ErrorHandlerFunc(requestHandler))))
	})
	if err != nil {
		log.Println(err)
		return
	}
}
