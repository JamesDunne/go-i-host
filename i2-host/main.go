package main

import (
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"syscall"
)

import (
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
)

import "github.com/JamesDunne/go-util/fs/notify"
import "github.com/JamesDunne/i-host/base62"

// FIXME(jsd): Hard-coded system paths here!
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

func isMultipart(r *http.Request) bool {
	v := r.Header.Get("Content-Type")
	if v == "" {
		return false
	}
	d, _, err := mime.ParseMediaType(v)
	if err != nil || d != "multipart/form-data" {
		return false
	}
	return true
}

func imageKindTo(imageKind string) (ext, thumbExt, mimeType string) {
	switch imageKind {
	case "jpeg":
		return "image/jpeg", ".jpg", ".jpg"
	case "png":
		return "image/png", ".png", ".png"
	case "gif":
		return "image/gif", ".gif", ".png"
	}
	return "", "", ""
}

func webErrorIf(rsp http.ResponseWriter, err error, statusCode int) bool {
	if err == nil {
		return false
	}

	rsp.WriteHeader(statusCode)
	rsp.Write([]byte(err.Error()))
	return true
}

func ensureThumbnail(image_path, thumb_path string) error {
	// Thumbnail exists; leave it alone:
	if _, err := os.Stat(thumb_path); err == nil {
		return nil
	}

	// Attempt to parse the image:
	var firstImage image.Image
	var imageKind string

	imf, err := os.Open(image_path)
	if err != nil {
		return err
	}
	defer imf.Close()

	firstImage, imageKind, err = image.Decode(imf)
	if err != nil {
		return err
	}

	return generateThumbnail(firstImage, imageKind, thumb_path)
}

func generateThumbnail(firstImage image.Image, imageKind string, thumb_path string) error {
	if firstImage == nil {
		return fmt.Errorf("Cannot generate thumbnail with nil image")
	}

	var encoder func(w io.Writer, m image.Image) error
	switch imageKind {
	case "jpeg":
		encoder = func(w io.Writer, img image.Image) error { return jpeg.Encode(w, img, &jpeg.Options{Quality: 100}) }
	case "png":
		encoder = png.Encode
	case "gif":
		encoder = png.Encode
	}

	// Generate the thumbnail image:
	thumbImg := makeThumbnail(firstImage, thumbnail_dimensions)

	// Save it to a file:
	os.Remove(thumb_path)
	tf, err := os.Create(thumb_path)
	if err != nil {
		return err
	}
	defer tf.Close()

	// Write the thumbnail to the file:
	err = encoder(tf, thumbImg)
	if err != nil {
		return err
	}

	return nil
}

func postImage(rsp http.ResponseWriter, req *http.Request) {
	// Accept file upload from client or download from URL supplied.
	var local_path string
	var title string

	if isMultipart(req) {
		// Accept file upload:
		reader, err := req.MultipartReader()
		if webErrorIf(rsp, err, 500) {
			return
		}

		// Keep reading the multipart form data and handle file uploads:
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if part.FormName() == "title" {
				// TODO: parse content-length if it exists?
				//part.Header.Get("Content-Length")
				t := make([]byte, 200)
				n, err := part.Read(t)
				if webErrorIf(rsp, err, 500) {
					return
				}
				title = string(t[:n])
				continue
			}
			if part.FileName() == "" {
				continue
			}

			// Copy upload data to a local file:
			local_path = path.Join(tmp_folder(), part.FileName())
			//log.Printf("Accepting upload: '%s'\n", local_path)

			if err, statusCode := func() (error, int) {
				f, err := os.Create(local_path)
				if err != nil {
					return err, 500
				}
				defer f.Close()

				if _, err := io.Copy(f, part); err != nil {
					return err, 500
				}
				return nil, 200
			}(); webErrorIf(rsp, err, statusCode) {
				return
			}
		}
	} else if imgurl_s := req.FormValue("url"); imgurl_s != "" {
		// Handle download from URL:

		// Require the 'title' form value:
		title = req.FormValue("title")
		if title == "" {
			rsp.WriteHeader(http.StatusBadRequest)
			rsp.Write([]byte("Missing title!"))
			return
		}

		// Parse the URL so we get the file name:
		imgurl, err := url.Parse(imgurl_s)
		if webErrorIf(rsp, err, 500) {
			return
		}

		// Split the absolute path by dir and filename:
		_, filename := path.Split(imgurl.Path)
		//log.Printf("Downloading %s", filename)

		// GET the url:
		if err, statusCode := func() (error, int) {
			img_rsp, err := http.Get(imgurl_s)
			if err != nil {
				return err, 500
			}
			defer img_rsp.Body.Close()

			// Create a local file:
			local_path = path.Join(tmp_folder(), filename)
			//log.Printf("to %s", local_path)

			local_file, err := os.Create(local_path)
			if err != nil {
				return err, 500
			}
			defer local_file.Close()

			// Download file:
			_, err = io.Copy(local_file, img_rsp.Body)
			if err != nil {
				return err, 500
			}

			return nil, 200
		}(); webErrorIf(rsp, err, statusCode) {
			return
		}
	}

	if title == "" {
		rsp.WriteHeader(http.StatusBadRequest)
		rsp.Write([]byte("Missing title!"))
		return
	}

	// Open the database:
	api, err := NewAPI()
	if webErrorIf(rsp, err, 500) {
		return
	}
	defer api.Close()

	// Attempt to parse the image:
	var firstImage image.Image
	var imageKind string

	if err, statusCode := func() (error, int) {
		imf, err := os.Open(local_path)
		if err != nil {
			return err, 500
		}
		defer imf.Close()

		firstImage, imageKind, err = image.Decode(imf)
		if err != nil {
			return err, 400
		}

		return nil, 200
	}(); webErrorIf(rsp, err, statusCode) {
		return
	}

	_, ext, thumbExt := imageKindTo(imageKind)

	// Create the DB record:
	id, err := api.NewImage(Image{Kind: imageKind, Title: title})
	if webErrorIf(rsp, err, 500) {
		return
	}

	// Rename the file:
	img_name := fmt.Sprintf("%d", id)
	store_path := path.Join(store_folder(), img_name+ext)
	if err := os.Rename(local_path, store_path); webErrorIf(rsp, err, 500) {
		return
	}

	// Generate a thumbnail:
	thumb_path := path.Join(thumb_folder(), img_name+thumbExt)
	if err := generateThumbnail(firstImage, imageKind, thumb_path); webErrorIf(rsp, err, 500) {
		return
	}

	// Redirect to the black-background viewer:
	redir_url := path.Join("/b/", b62.Encode(id+10000))
	http.Redirect(rsp, req, redir_url, 302)
}

func getForm(rsp http.ResponseWriter, req *http.Request) {
	rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
}

type ImageListItem struct {
	ID             int64
	Base62ID       string
	Extension      string
	ThumbExtension string
	Title          string
}

// handles requests to upload images and rehost with shortened URLs
func requestHandler(rsp http.ResponseWriter, req *http.Request) {
	//log.Printf("HTTP: %s %s", req.Method, req.URL.Path)
	if req.Method == "POST" {
		// POST:

		if req.URL.Path == "/new" {
			// POST a new image:
			postImage(rsp, req)
			return
		}

		rsp.WriteHeader(http.StatusBadRequest)
		return
	}

	// GET:
	if req.URL.Path == "/favicon.ico" {
		rsp.WriteHeader(http.StatusNoContent)
		return
	} else if req.URL.Path == "/" {
		// Render a list page:
		api, err := NewAPI()
		if webErrorIf(rsp, err, 500) {
			return
		}
		defer api.Close()

		list, err := api.GetList()
		if webErrorIf(rsp, err, 500) {
			return
		}

		// Project into a view model:
		model := struct {
			List []ImageListItem
		}{
			List: make([]ImageListItem, 0, len(list)),
		}
		for _, img := range list {
			_, ext, thumbExt := imageKindTo(img.Kind)
			model.List = append(model.List, ImageListItem{
				ID:             img.ID,
				Base62ID:       b62.Encode(img.ID + 10000),
				Extension:      ext,
				ThumbExtension: thumbExt,
				Title:          img.Title,
			})
		}

		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		rsp.WriteHeader(200)
		if err := uiTmpl.ExecuteTemplate(rsp, "list", model); webErrorIf(rsp, err, 500) {
			return
		}
		return
	} else if req.URL.Path == "/new" {
		// GET the /new form to add a new image:
		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := uiTmpl.ExecuteTemplate(rsp, "new", nil); webErrorIf(rsp, err, 500) {
			return
		}
		return
	}

	dir := path.Dir(req.URL.Path)

	// Look up the image's record by base62 encoded ID:
	filename := path.Base(req.URL.Path)
	filename = filename[0 : len(filename)-len(path.Ext(req.URL.Path))]

	id := b62.Decode(filename) - 10000

	api, err := NewAPI()
	if webErrorIf(rsp, err, 500) {
		return
	}
	defer api.Close()
	img, err := api.GetImage(id)
	if webErrorIf(rsp, err, 500) {
		return
	}
	if img == nil {
		rsp.WriteHeader(http.StatusNotFound)
		return
	}

	// Determine mime-type and file extension:
	mime, ext, thumbExt := imageKindTo(img.Kind)

	// Find the image file:
	base62ID := b62.Encode(id + 10000)
	img_name := fmt.Sprintf("%d", id)

	if dir == "/b" || dir == "/w" {
		// Render a black or white BG centered image viewer:
		var bgcolor string
		switch dir {
		case "/b":
			bgcolor = "black"
		case "/w":
			bgcolor = "white"
		}

		model := struct {
			BGColor        string
			ID             int64
			Base62ID       string
			Extension      string
			ThumbExtension string
			Title          string
		}{
			BGColor:        bgcolor,
			ID:             img.ID,
			Base62ID:       base62ID,
			Extension:      ext,
			ThumbExtension: thumbExt,
			Title:          img.Title,
		}

		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := uiTmpl.ExecuteTemplate(rsp, "view", model); webErrorIf(rsp, err, 500) {
			return
		}

		return
	} else if dir == "/t" {
		// Serve thumbnail file:
		local_path := path.Join(store_folder(), img_name+ext)
		thumb_path := path.Join(thumb_folder(), img_name+thumbExt)
		if err := ensureThumbnail(local_path, thumb_path); webErrorIf(rsp, err, 500) {
			return
		}

		if xrThumb != "" {
			// Pass request to nginx to serve static content file:
			redirPath := path.Join(xrThumb, img_name+thumbExt)

			//log.Printf("X-Accel-Redirect: %s", redirPath)
			rsp.Header().Set("X-Accel-Redirect", redirPath)
			rsp.Header().Set("Content-Type", mime)
			rsp.WriteHeader(200)
			return
		} else {
			rsp.Header().Set("Content-Type", mime)
			http.ServeFile(rsp, req, thumb_path)
			return
		}
	}

	// Serve actual image contents:
	if xrGif != "" {
		// Pass request to nginx to serve static content file:
		redirPath := path.Join(xrGif, img_name+ext)

		//log.Printf("X-Accel-Redirect: %s", redirPath)
		rsp.Header().Set("X-Accel-Redirect", redirPath)
		rsp.Header().Set("Content-Type", mime)
		rsp.WriteHeader(200)
		return
	} else {
		// Serve content directly with the proper mime-type:
		local_path := path.Join(store_folder(), img_name+ext)

		rsp.Header().Set("Content-Type", mime)
		http.ServeFile(rsp, req, local_path)
		return
	}
}

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

func main() {
	// Define our commandline flags:
	socketType := flag.String("t", "tcp", "socket type to listen on: 'unix', 'tcp', 'udp'")
	socketAddr := flag.String("l", ":8080", "address to listen on")
	fs := flag.String("fs", ".", "Root directory of served files and templates")
	xrGifArg := flag.String("xrg", "", "X-Accel-Redirect header prefix for serving images or blank to disable")
	xrThumbArg := flag.String("xrt", "", "X-Accel-Redirect header prefix for serving thumbnails or blank to disable")

	// Parse the flags and set values:
	flag.Parse()
	base_folder = path.Clean(*fs)
	xrGif = *xrGifArg
	xrThumb = *xrThumbArg

	// Watch the html templates for changes and reload them:
	_, err, cleanup := watchTemplates("ui", html_path(), "*.html")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer cleanup()

	// Create the socket to listen on:
	l, err := net.Listen(*socketType, *socketAddr)
	if err != nil {
		log.Fatal(err)
		return
	}

	// NOTE(jsd): Unix sockets must be removed before being reused.

	// Handle common process-killing signals so we can gracefully shut down:
	// TODO(jsd): Go does not catch Windows' process kill signals (yet?)
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGQUIT)
	go func(c chan os.Signal) {
		// Wait for a signal:
		sig := <-c
		log.Printf("Caught signal '%s': shutting down.\n", sig)

		// Stop listening:
		l.Close()

		// Delete the unix socket, if applicable:
		if *socketType == "unix" {
			os.Remove(*socketAddr)
		}

		// And we're done:
		os.Exit(0)
	}(sigc)

	// Start the HTTP server:
	log.Fatal(http.Serve(l, http.HandlerFunc(requestHandler)))
}
