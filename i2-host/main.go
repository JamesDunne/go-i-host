package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"image"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
)

import "github.com/JamesDunne/go-util/base"
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

func imageKindTo(imageKind string) (mimeType, ext, thumbExt string) {
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

func jsonErrorIf(rsp http.ResponseWriter, err error, statusCode int) bool {
	if err == nil {
		return false
	}

	rsp.Header().Set("Content-Type", "application/json; charset=utf-8")
	rsp.WriteHeader(statusCode)
	j, _ := json.Marshal(struct {
		StatusCode int    `json:"statusCode"`
		Error      string `json:"error"`
	}{
		StatusCode: statusCode,
		Error:      err.Error(),
	})
	rsp.Write(j)
	return true
}

func ensureThumbnail(image_path, thumb_path string) (err error) {
	// Thumbnail exists; leave it alone:
	if _, err = os.Stat(thumb_path); err == nil {
		return nil
	}

	// Attempt to parse the image:
	var firstImage image.Image
	var imageKind string

	firstImage, imageKind, err = decodeFirstImage(image_path)
	defer func() { firstImage = nil }()
	if err != nil {
		return err
	}

	return generateThumbnail(firstImage, imageKind, thumb_path)
}

func postImage(rsp http.ResponseWriter, req *http.Request) {
	// Accept file upload from client or download from URL supplied.
	var local_path string
	var title string
	var sourceURL string
	var imageKind string

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
			sourceURL = "file://" + part.FileName()
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
		sourceURL = imgurl_s
		imgurl, err := url.Parse(imgurl_s)
		if webErrorIf(rsp, err, 400) {
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
	} else if yturl_s := req.FormValue("youtube_url"); yturl_s != "" {
		// YouTube URL:
		imageKind = "youtube"

		// Require the 'title' form value:
		title = req.FormValue("title")
		if title == "" {
			rsp.WriteHeader(http.StatusBadRequest)
			rsp.Write([]byte("Missing title!"))
			return
		}

		// Parse the URL:
		yturl, err := url.Parse(yturl_s)
		if webErrorIf(rsp, err, 400) {
			return
		}

		// Validate our expectations:
		if yturl.Scheme != "http" && yturl.Scheme != "https" {
			webErrorIf(rsp, fmt.Errorf("YouTube URL must have http or https scheme!"), 400)
		}

		if yturl.Host != "www.youtube.com" {
			webErrorIf(rsp, fmt.Errorf("YouTube URL must be from www.youtube.com host!"), 400)
			return
		}

		if yturl.Path != "/watch" {
			webErrorIf(rsp, fmt.Errorf("Unrecognized YouTube URL form."), 400)
			return
		}

		sourceURL = yturl.Query().Get("v")
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

	var id int64

	if local_path != "" {
		// Do some local image processing first:
		var firstImage image.Image

		firstImage, imageKind, err = decodeFirstImage(local_path)
		defer func() { firstImage = nil }()
		if webErrorIf(rsp, err, 500) {
			return
		}

		_, ext, thumbExt := imageKindTo(imageKind)

		// Create the DB record:
		id, err = api.NewImage(Image{Kind: imageKind, Title: title, SourceURL: &sourceURL, IsClean: false})
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
	} else {
		// Create the DB record:
		id, err = api.NewImage(Image{Kind: imageKind, Title: title, SourceURL: &sourceURL, IsClean: false})
		if webErrorIf(rsp, err, 500) {
			return
		}
	}

	// Redirect to the black-background viewer:
	redir_url := path.Join("/b/", b62.Encode(id+10000))
	http.Redirect(rsp, req, redir_url, 302)
}

func getForm(rsp http.ResponseWriter, req *http.Request) {
	rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
}

type ImageViewModel struct {
	ID           int64   `json:"id"`
	Base62ID     string  `json:"base62id"`
	Title        string  `json:"title"`
	Kind         string  `json:"kind"`
	ImageURL     string  `json:"imageURL"`
	ThumbURL     string  `json:"thumbURL"`
	SourceURL    *string `json:"sourceURL,omitempty"`
	RedirectToID *int64  `json:"redirectToID,omitempty"`
	IsClean      bool    `json:"isClean"`
}

func xlatImageViewModel(i *Image, o *ImageViewModel) *ImageViewModel {
	if o == nil {
		o = new(ImageViewModel)
	}

	o.ID = i.ID
	o.Base62ID = b62.Encode(i.ID + 10000)
	o.Title = i.Title
	o.Kind = i.Kind
	o.SourceURL = i.SourceURL
	o.RedirectToID = i.RedirectToID
	o.IsClean = i.IsClean
	_, ext, thumbExt := imageKindTo(i.Kind)
	switch i.Kind {
	case "youtube":
		o.ImageURL = "//www.youtube.com/embed/" + *i.SourceURL
		o.ThumbURL = "//i1.ytimg.com/vi/" + *i.SourceURL + "/hqdefault.jpg"
	case "gif":
		fallthrough
	case "jpeg":
		fallthrough
	case "png":
		o.ImageURL = "/" + o.Base62ID + ext
		o.ThumbURL = "/t/" + o.Base62ID + thumbExt
	}

	return o
}

func projectModelList(list []Image) (modelList []ImageViewModel) {
	modelList = make([]ImageViewModel, 0, len(list))

	count := 0
	for _, img := range list {
		if img.IsHidden {
			continue
		}

		modelList = append(modelList, ImageViewModel{})
		xlatImageViewModel(&img, &modelList[count])
		count++
	}

	return
}

// handles requests to upload images and rehost with shortened URLs
func requestHandler(rsp http.ResponseWriter, req *http.Request) {
	//log.Printf("HTTP: %s %s", req.Method, req.URL.Path)
	if req.Method == "POST" {
		// POST:

		if req.URL.Path == "/a/new" {
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
	} else if req.URL.Path == "/" || req.URL.Path == "/a/all" {
		var err error

		// Render a list page:
		api, err := NewAPI()
		if webErrorIf(rsp, err, 500) {
			return
		}
		defer api.Close()

		var list []Image
		if _, ok := req.URL.Query()["newest"]; ok {
			list, err = api.GetList(ImagesOrderByIDDESC)
		} else if _, ok := req.URL.Query()["oldest"]; ok {
			list, err = api.GetList(ImagesOrderByIDASC)
		} else {
			list, err = api.GetList(ImagesOrderByTitleASC)
		}

		if webErrorIf(rsp, err, 500) {
			return
		}

		// Project into a view model:
		model := struct {
			List []ImageViewModel
		}{
			List: projectModelList(list),
		}

		var viewName string
		if req.URL.Path == "/a/all" {
			viewName = "all"
		} else {
			viewName = "frontPage"
		}

		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		rsp.WriteHeader(200)
		if err := uiTmpl.ExecuteTemplate(rsp, viewName, model); webErrorIf(rsp, err, 500) {
			return
		}
		return
	} else if req.URL.Path == "/a/new" {
		// GET the /a/new form to add a new image:
		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		rsp.WriteHeader(200)
		if err := uiTmpl.ExecuteTemplate(rsp, "new", nil); webErrorIf(rsp, err, 500) {
			return
		}
		return
	} else if req.URL.Path == "/api/list" {
		var err error

		api, err := NewAPI()
		if webErrorIf(rsp, err, 500) {
			return
		}
		defer api.Close()

		var list []Image
		if _, ok := req.URL.Query()["newest"]; ok {
			list, err = api.GetList(ImagesOrderByIDDESC)
		} else if _, ok := req.URL.Query()["oldest"]; ok {
			list, err = api.GetList(ImagesOrderByIDASC)
		} else {
			list, err = api.GetList(ImagesOrderByTitleASC)
		}

		if webErrorIf(rsp, err, 500) {
			return
		}

		// Project into a view model:
		model := struct {
			List []ImageViewModel `json:"list"`
		}{
			List: projectModelList(list),
		}

		jsonText, err := json.Marshal(model)
		if jsonErrorIf(rsp, err, 500) {
			return
		}

		rsp.Header().Set("Content-Type", "application/json; charset=utf-8")
		rsp.WriteHeader(200)
		rsp.Write(jsonText)
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

	// Follow redirect chain:
	for img.RedirectToID != nil {
		newimg, err := api.GetImage(*img.RedirectToID)
		if webErrorIf(rsp, err, 500) {
			return
		}
		img = newimg
	}

	// Determine mime-type and file extension:
	mime, ext, thumbExt := imageKindTo(img.Kind)

	// Find the image file:
	img_name := fmt.Sprintf("%d", img.ID)

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
			BGColor string
			Image   ImageViewModel
		}{
			BGColor: bgcolor,
			Image:   *xlatImageViewModel(img, nil),
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
			runtime.GC()
			return
		}

		if xrThumb != "" {
			// Pass request to nginx to serve static content file:
			redirPath := path.Join(xrThumb, img_name+thumbExt)

			//log.Printf("X-Accel-Redirect: %s", redirPath)
			rsp.Header().Set("X-Accel-Redirect", redirPath)
			rsp.Header().Set("Content-Type", mime)
			rsp.WriteHeader(200)
			runtime.GC()
			return
		} else {
			rsp.Header().Set("Content-Type", mime)
			http.ServeFile(rsp, req, thumb_path)
			runtime.GC()
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
		runtime.GC()
		return
	} else {
		// Serve content directly with the proper mime-type:
		local_path := path.Join(store_folder(), img_name+ext)

		rsp.Header().Set("Content-Type", mime)
		http.ServeFile(rsp, req, local_path)
		runtime.GC()
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
	base_folder = path.Clean(*fs)
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
