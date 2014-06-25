package main

import (
	"encoding/json"
	"fmt"
	"image"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"strings"
)

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

type webError struct {
	StatusCode int   `json:"statusCode"`
	Error      error `json:"error"`
}

func asWebError(err error, statusCode int) *webError {
	if err == nil {
		return nil
	}
	return &webError{
		StatusCode: statusCode,
		Error:      err,
	}
}

func (e *webError) RespondHTML(rsp http.ResponseWriter) bool {
	if e == nil {
		return false
	}

	rsp.WriteHeader(e.StatusCode)
	rsp.Write([]byte(e.Error.Error()))
	return true
}

func (e *webError) RespondJSON(rsp http.ResponseWriter) bool {
	if e == nil {
		return false
	}

	rsp.Header().Set("Content-Type", "application/json; charset=utf-8")
	rsp.WriteHeader(e.StatusCode)
	j, _ := json.Marshal(e)
	rsp.Write(j)
	return true
}

func webErrorIf(rsp http.ResponseWriter, err error, statusCode int) bool {
	if err == nil {
		return false
	}

	return asWebError(err, statusCode).RespondHTML(rsp)
}

func jsonErrorIf(rsp http.ResponseWriter, err error, statusCode int) bool {
	if err == nil {
		return false
	}

	return asWebError(err, statusCode).RespondJSON(rsp)
}

type imageStoreRequest struct {
	kind           string
	localPath      string
	title          string
	sourceURL      string
	collectionName string
	submitter      string
	isClean        bool
}

func storeImage(req *imageStoreRequest) (id int64, werr *webError) {
	if req.title == "" {
		return 0, asWebError(fmt.Errorf("Missing title!"), http.StatusBadRequest)
	}

	// Open the database:
	api, err := NewAPI()
	if werr = asWebError(err, http.StatusInternalServerError); werr != nil {
		return
	}
	defer api.Close()

	newImage := &Image{
		Kind:           req.kind,
		Title:          req.title,
		SourceURL:      &req.sourceURL,
		CollectionName: req.collectionName,
		Submitter:      req.submitter,
		IsClean:        req.isClean,
	}

	if req.localPath != "" {
		// Do some local image processing first:
		var firstImage image.Image

		firstImage, req.kind, err = decodeFirstImage(req.localPath)
		defer func() { firstImage = nil }()
		if werr = asWebError(err, http.StatusInternalServerError); werr != nil {
			return
		}

		_, ext, thumbExt := imageKindTo(req.kind)

		// Create the DB record:
		id, err = api.NewImage(newImage)
		if werr = asWebError(err, http.StatusInternalServerError); werr != nil {
			return
		}

		// Rename the file:
		img_name := fmt.Sprintf("%d", id)
		store_path := path.Join(store_folder(), img_name+ext)
		if werr = asWebError(os.Rename(req.localPath, store_path), http.StatusInternalServerError); werr != nil {
			return
		}

		// Generate a thumbnail:
		thumb_path := path.Join(thumb_folder(), img_name+thumbExt)
		if werr = asWebError(generateThumbnail(firstImage, req.kind, thumb_path), http.StatusInternalServerError); werr != nil {
			return
		}
	} else {
		// Create the DB record:
		id, err = api.NewImage(newImage)
		if werr = asWebError(err, http.StatusInternalServerError); werr != nil {
			return
		}
	}

	return 0, nil
}

func postImage(rsp http.ResponseWriter, req *http.Request, collectionName string) {
	// Accept file upload from client or download from URL supplied.
	store := imageStoreRequest{
		collectionName: collectionName,
		submitter:      "",
	}

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
				store.title = string(t[:n])
				continue
			}
			if part.FileName() == "" {
				continue
			}

			// Copy upload data to a local file:
			store.sourceURL = "file://" + part.FileName()
			store.localPath = path.Join(tmp_folder(), part.FileName())
			//log.Printf("Accepting upload: '%s'\n", store.localPath)

			if err, statusCode := func() (error, int) {
				f, err := os.Create(store.localPath)
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
		store.title = req.FormValue("title")
		if store.title == "" {
			rsp.WriteHeader(http.StatusBadRequest)
			rsp.Write([]byte("Missing title!"))
			return
		}

		// Parse the URL so we get the file name:
		store.sourceURL = imgurl_s
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
			store.localPath = path.Join(tmp_folder(), filename)
			//log.Printf("to %s", store.localPath)

			local_file, err := os.Create(store.localPath)
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
		store.kind = "youtube"

		// Require the 'title' form value:
		store.title = req.FormValue("title")
		if store.title == "" {
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

		store.sourceURL = yturl.Query().Get("v")
	}

	id, err := storeImage(&store)
	if err.RespondHTML(rsp) {
		return
	}

	// Redirect to the black-background viewer:
	redir_url := path.Join("/b/", b62.Encode(id+10000))
	http.Redirect(rsp, req, redir_url, 302)
}

func getForm(rsp http.ResponseWriter, req *http.Request) {
	rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
}

func listCollection(rsp http.ResponseWriter, req *http.Request, collectionName string, showUnclean bool) {
	var err error

	// Render a list page:
	api, err := NewAPI()
	if webErrorIf(rsp, err, 500) {
		return
	}
	defer api.Close()

	var list []Image
	if _, ok := req.URL.Query()["newest"]; ok {
		list, err = api.GetList(collectionName, ImagesOrderByIDDESC)
	} else if _, ok := req.URL.Query()["oldest"]; ok {
		list, err = api.GetList(collectionName, ImagesOrderByIDASC)
	} else {
		list, err = api.GetList(collectionName, ImagesOrderByTitleASC)
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
	if showUnclean {
		viewName = "unclean"
	} else {
		viewName = "clean"
	}

	rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
	rsp.WriteHeader(200)
	if err := uiTmpl.ExecuteTemplate(rsp, viewName, model); webErrorIf(rsp, err, 500) {
		return
	}
	return
}

// matches path against "/a/b" routes or "/a/b/*" routes and returns "*" portion or "".
func matchSimpleRoute(path, route string) (remainder string, ok bool) {
	if path == route {
		return "", true
	}

	if strings.HasPrefix(path, route+"/") {
		return path[len(route)+1:], true
	}

	return "", false
}

// handles requests to upload images and rehost with shortened URLs
func requestHandler(rsp http.ResponseWriter, req *http.Request) {
	//log.Printf("HTTP: %s %s", req.Method, req.URL.Path)
	if req.Method == "POST" {
		// POST:

		if collectionName, ok := matchSimpleRoute(req.URL.Path, "/col/add"); ok {
			// POST a new image:
			postImage(rsp, req, collectionName)
			return
			// JSON API:
		} else if collectionName, ok := matchSimpleRoute(req.URL.Path, "/api/add"); ok {
			// POST a new image from JSON API:
			store := &imageStoreRequest{
				collectionName: collectionName,
			}

			jd := json.NewDecoder(req.Body)
			err := jd.Decode(store)
			if jsonErrorIf(rsp, err, http.StatusBadRequest) {
				return
			}

			// Process the store request:
			id, werr := storeImage(store)
			if werr.RespondJSON(rsp) {
				return
			}

			rsp.Header().Set("Content-Type", "application/json; charset=utf-8")
			json.Marshal(struct{}{})
			return
		}

		rsp.WriteHeader(http.StatusBadRequest)
		return
	}

	// GET:
	_, showUnclean := req.URL.Query()["all"]

	if req.URL.Path == "/favicon.ico" {
		rsp.WriteHeader(http.StatusNoContent)
		return
	} else if req.URL.Path == "/" {
		listCollection(rsp, req, "", showUnclean)
		return
	} else if collectionName, ok := matchSimpleRoute(req.URL.Path, "/col/list"); ok {
		listCollection(rsp, req, collectionName, showUnclean)
		return
	} else if collectionName, ok := matchSimpleRoute(req.URL.Path, "/col/add"); ok {
		model := &struct {
			AddURL    string
			UploadURL string
		}{}
		model.AddURL = "/col/add"
		model.UploadURL = "/col/upload"
		if collectionName != "" {
			model.AddURL = "/col/add/" + collectionName
			model.UploadURL = "/col/upload/" + collectionName
		}

		// GET the /col/add form to add a new image:
		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		rsp.WriteHeader(200)
		if err := uiTmpl.ExecuteTemplate(rsp, "new", model); webErrorIf(rsp, err, 500) {
			return
		}
		return
	} else if collectionName, ok := matchSimpleRoute(req.URL.Path, "/api/list"); ok {
		var err error

		api, err := NewAPI()
		if webErrorIf(rsp, err, 500) {
			return
		}
		defer api.Close()

		var list []Image
		if _, ok := req.URL.Query()["newest"]; ok {
			list, err = api.GetList(collectionName, ImagesOrderByIDDESC)
		} else if _, ok := req.URL.Query()["oldest"]; ok {
			list, err = api.GetList(collectionName, ImagesOrderByIDASC)
		} else {
			list, err = api.GetList(collectionName, ImagesOrderByTitleASC)
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
