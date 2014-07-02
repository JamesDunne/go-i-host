package main

import (
	"flag"
	"html/template"
	"log"
	"net"
	"net/http"
	"path"
	"path/filepath"
)

import "github.com/JamesDunne/go-util/base"
import "github.com/JamesDunne/go-util/fs/notify"
import "github.com/JamesDunne/i-host/base62"

var (
	base_folder = "/srv/bittwiddlers.org/i2"
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

// Watches the html/*.html templates for changes:
func watchTemplates(name, templatePath, glob string) (watcher *notify.Watcher, err error, deferClean func()) {
	// Parse template files:
	tmplGlob := path.Join(templatePath, glob)
	ui, err := template.New(name).ParseGlob(tmplGlob)
	if err != nil {
		return nil, err, nil
	}
	uiTmpl = ui

	// Watch template directory for file changes:
	watcher, err = notify.NewWatcher()
	if err != nil {
		return nil, err, nil
	}
	deferClean = func() { watcher.RemoveWatch(templatePath); watcher.Close() }

	// Process watcher events
	go func() {
		for {
			select {
			case ev := <-watcher.Event:
				if ev == nil {
					break
				}
				//log.Println("event:", ev)

				// Update templates:
				var err error
				ui, err := template.New(name).ParseGlob(tmplGlob)
				if err != nil {
					log.Println(err)
					break
				}
				uiTmpl = ui
			case err := <-watcher.Error:
				if err == nil {
					break
				}
				log.Println("watcher error:", err)
			}
		}
	}()

	// Watch template file for changes:
	watcher.Watch(templatePath)

	return
}

func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		panic(err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		panic(err)
	}
	return abs
}

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
	base_folder = canonicalPath(path.Clean(*fs))
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
	_, err, cleanup := watchTemplates("ui", html_path(), "*.html")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer cleanup()

	// Start the server:
	base.ServeMain(listen_addr, func(l net.Listener) error {
		return http.Serve(l, http.HandlerFunc(requestHandler))
	})
}
