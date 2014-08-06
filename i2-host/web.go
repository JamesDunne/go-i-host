package main

import (
	"encoding/json"
	"fmt"
	"image"
	"io"
	//"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"strconv"
)

import "github.com/JamesDunne/go-util/web"

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

type ImageViewModel struct {
	ID             int64   `json:"id"`
	Base62ID       string  `json:"base62id"`
	Title          string  `json:"title"`
	Kind           string  `json:"kind"`
	ImageURL       string  `json:"imageURL"`
	ThumbURL       string  `json:"thumbURL"`
	Submitter      string  `json:"submitter,omitempty"`
	CollectionName string  `json:"collectionName,omitempty"`
	SourceURL      *string `json:"sourceURL,omitempty"`
	RedirectToID   *int64  `json:"redirectToID,omitempty"`
	IsClean        bool    `json:"isClean"`
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
	o.CollectionName = i.CollectionName
	o.Submitter = i.Submitter

	if o.Kind == "" {
		o.Kind = "gif"
	}
	_, ext, thumbExt := imageKindTo(o.Kind)
	switch o.Kind {
	case "youtube":
		o.ImageURL = "//www.youtube.com/embed/" + *i.SourceURL
		o.ThumbURL = "//i1.ytimg.com/vi/" + *i.SourceURL + "/hqdefault.jpg"
	default:
		o.ImageURL = "/" + o.Base62ID + ext
		o.ThumbURL = "/t/" + o.Base62ID + thumbExt
	}

	return o
}

func projectModelList(list []Image) (modelList []ImageViewModel) {
	modelList = make([]ImageViewModel, 0, len(list))

	count := 0
	for i, _ := range list {
		img := &list[i]
		if img.IsHidden {
			continue
		}

		modelList = append(modelList, ImageViewModel{})
		xlatImageViewModel(img, &modelList[count])
		count++
	}

	return
}

func useAPI(use func(api *API) *web.WebError) *web.WebError {
	api, err := NewAPI()
	if err != nil {
		return web.AsWebError(err, http.StatusInternalServerError)
	}
	defer api.Close()

	return use(api)
}

type imageStoreRequest struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	SourceURL string `json:"sourceURL"`
	Submitter string `json:"submitter"`
	IsClean   bool   `json:"isClean"`

	CollectionName string
	LocalPath      string
}

func storeImage(req *imageStoreRequest) (id int64, werr *web.WebError) {
	if req.Title == "" {
		return 0, web.AsWebError(fmt.Errorf("Missing title!"), http.StatusBadRequest)
	}

	// Open the database:
	werr = useAPI(func(api *API) (werr *web.WebError) {
		var err error

		newImage := &Image{
			Kind:           req.Kind,
			Title:          req.Title,
			SourceURL:      &req.SourceURL,
			CollectionName: req.CollectionName,
			Submitter:      req.Submitter,
			IsClean:        req.IsClean,
		}

		if req.LocalPath != "" {
			// Do some local image processing first:
			var firstImage image.Image

			firstImage, newImage.Kind, err = decodeFirstImage(req.LocalPath)
			defer func() { firstImage = nil }()
			if werr = web.AsWebError(err, http.StatusInternalServerError); werr != nil {
				return
			}

			if newImage.Kind == "" {
				newImage.Kind = "gif"
			}
			_, ext, thumbExt := imageKindTo(newImage.Kind)

			// Create the DB record:
			id, err = api.NewImage(newImage)
			if werr = web.AsWebError(err, http.StatusInternalServerError); werr != nil {
				return
			}

			// Rename the file:
			img_name := strconv.FormatInt(id, 10)
			os.MkdirAll(store_folder(), 0755)
			store_path := path.Join(store_folder(), img_name+ext)
			if werr = web.AsWebError(os.Rename(req.LocalPath, store_path), http.StatusInternalServerError); werr != nil {
				return
			}

			// Generate a thumbnail:
			os.MkdirAll(thumb_folder(), 0755)
			thumb_path := path.Join(thumb_folder(), img_name+thumbExt)
			if werr = web.AsWebError(generateThumbnail(firstImage, newImage.Kind, thumb_path), http.StatusInternalServerError); werr != nil {
				return
			}
		} else {
			if newImage.Kind == "" {
				newImage.Kind = "gif"
			}

			// Create the DB record:
			id, err = api.NewImage(newImage)
			if werr = web.AsWebError(err, http.StatusInternalServerError); werr != nil {
				return
			}
		}

		return nil
	})
	if werr != nil {
		return 0, werr
	}

	return id, nil
}

func downloadImageFor(store *imageStoreRequest) *web.WebError {
	// Validate the URL:
	imgurl, err := url.Parse(store.SourceURL)
	if err != nil {
		return web.AsWebError(err, http.StatusBadRequest)
	}

	// Check if it's a youtube link:
	if (imgurl.Scheme == "http" || imgurl.Scheme == "https") && (imgurl.Host == "www.youtube.com") {
		// Process youtube links specially:
		if imgurl.Path != "/watch" {
			return web.AsWebError(fmt.Errorf("Unrecognized YouTube URL form."), http.StatusBadRequest)
		}

		store.Kind = "youtube"
		store.LocalPath = ""
		store.SourceURL = imgurl.Query().Get("v")
		return nil
	}

	// Do a HTTP GET to fetch the image:
	img_rsp, err := http.Get(store.SourceURL)
	if err != nil {
		return web.AsWebError(err, http.StatusInternalServerError)
	}
	defer img_rsp.Body.Close()

	// Create a local temporary file to download to:
	os.MkdirAll(tmp_folder(), 0755)
	local_file, err := TempFile(tmp_folder(), "dl-", "")
	if err != nil {
		return web.AsWebError(err, http.StatusInternalServerError)
	}
	defer local_file.Close()

	store.LocalPath = local_file.Name()

	// Download file:
	_, err = io.Copy(local_file, img_rsp.Body)
	if err != nil {
		return web.AsWebError(err, http.StatusInternalServerError)
	}

	return nil
}

func getForm(rsp http.ResponseWriter, req *http.Request) {
	rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
}

func listCollection(rsp http.ResponseWriter, req *http.Request, collectionName string, list []Image, showUnclean bool) {
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
	if web.AsWebError(uiTmpl.ExecuteTemplate(rsp, viewName, model), http.StatusInternalServerError).RespondHTML(rsp) {
		return
	}
	return
}

func getImage(id int64) (img *Image, werr *web.WebError) {
	werr = useAPI(func(api *API) *web.WebError {
		var err error

		img, err = api.GetImage(id)

		return web.AsWebError(err, http.StatusInternalServerError)
	})

	return
}

func getAll(orderBy ImagesOrderBy) (list []Image, werr *web.WebError) {
	werr = useAPI(func(api *API) *web.WebError {
		var err error

		list, err = api.GetAll(orderBy)

		return web.AsWebError(err, http.StatusInternalServerError)
	})

	return
}

func getList(collectionName string, orderBy ImagesOrderBy) (list []Image, werr *web.WebError) {
	werr = useAPI(func(api *API) *web.WebError {
		var err error

		list, err = api.GetList(collectionName, orderBy)

		return web.AsWebError(err, http.StatusInternalServerError)
	})

	return
}

func getListOnly(collectionName string, orderBy ImagesOrderBy) (list []Image, werr *web.WebError) {
	werr = useAPI(func(api *API) *web.WebError {
		var err error

		list, err = api.GetListOnly(collectionName, orderBy)

		return web.AsWebError(err, http.StatusInternalServerError)
	})

	return
}

type viewTemplateModel struct {
	BGColor    string
	FillScreen bool
	Query      map[string]string
	Image      ImageViewModel
}

func flattenQuery(query map[string][]string) (flat map[string]string) {
	flat = make(map[string]string)
	for k, v := range query {
		flat[k] = v[0]
	}
	return
}

// handles requests to upload images and rehost with shortened URLs
func requestHandler(rsp http.ResponseWriter, req *http.Request) {
	// Set RemoteAddr for forwarded requests:
	{
		ip := req.Header.Get("X-Real-IP")
		if ip == "" {
			ip = req.Header.Get("X-Forwarded-For")
		}
		if ip != "" {
			req.RemoteAddr = ip
		}
	}
	//log.Printf("%s %s %s %s\nHeaders: %v\n\n", req.RemoteAddr, req.Method, req.Host, req.URL, req.Header)

	if req.Method == "POST" {
		// POST:

		if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/col/add"); ok {
			// Add a new image via URL to download from:
			imgurl_s := req.FormValue("url")
			if imgurl_s == "" {
				web.AsWebError(fmt.Errorf("Missing required 'url' form value!"), http.StatusBadRequest).RespondHTML(rsp)
				return
			}

			store := &imageStoreRequest{
				CollectionName: collectionName,
				Submitter:      req.RemoteAddr,
				Title:          req.FormValue("title"),
				SourceURL:      imgurl_s,
			}

			// Require the 'title' form value:
			if store.Title == "" {
				rsp.WriteHeader(http.StatusBadRequest)
				rsp.Write([]byte("Missing title!"))
				return
			}

			// Download the image from the URL:
			if downloadImageFor(store).RespondHTML(rsp) {
				return
			}

			// Store it in the database and generate thumbnail:
			id, err := storeImage(store)
			if err.RespondHTML(rsp) {
				return
			}

			// Redirect to a black-background view of the image:
			redir_url := path.Join("/b/", b62.Encode(id+10000))
			http.Redirect(rsp, req, redir_url, 302)
			return
		} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/col/upload"); ok {
			// Upload a new image:
			store := &imageStoreRequest{
				CollectionName: collectionName,
				Submitter:      req.RemoteAddr,
			}

			if !web.IsMultipart(req) {
				web.AsWebError(fmt.Errorf("Upload request must be multipart form data"), http.StatusBadRequest).RespondHTML(rsp)
				return
			}

			// Accept file upload:
			reader, err := req.MultipartReader()
			if web.AsWebError(err, http.StatusBadRequest).RespondHTML(rsp) {
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
					if web.AsWebError(err, http.StatusInternalServerError).RespondHTML(rsp) {
						return
					}
					store.Title = string(t[:n])
					continue
				}
				if part.FileName() == "" {
					continue
				}

				// Copy upload data to a local file:
				store.SourceURL = "file://" + part.FileName()

				if func() *web.WebError {
					os.MkdirAll(tmp_folder(), 0755)
					f, err := TempFile(tmp_folder(), "up-", path.Ext(part.FileName()))
					if err != nil {
						return web.AsWebError(err, http.StatusInternalServerError)
					}
					defer f.Close()

					store.LocalPath = f.Name()

					if _, err := io.Copy(f, part); err != nil {
						return web.AsWebError(err, http.StatusInternalServerError)
					}
					return nil
				}().RespondHTML(rsp) {
					return
				}
			}

			// Store it in the database and generate thumbnail:
			id, werr := storeImage(store)
			if werr.RespondHTML(rsp) {
				return
			}

			// Redirect to a black-background view of the image:
			redir_url := path.Join("/b/", b62.Encode(id+10000))
			http.Redirect(rsp, req, redir_url, 302)
			return
		} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/add"); ok {
			// Add a new image via URL to download from via JSON API:
			store := &imageStoreRequest{
				CollectionName: collectionName,
			}

			jd := json.NewDecoder(req.Body)
			err := jd.Decode(store)
			if web.JsonErrorIf(rsp, err, http.StatusBadRequest) {
				return
			}

			// Download Image locally:
			if downloadImageFor(store).RespondJSON(rsp) {
				return
			}

			// Process the store request:
			id, werr := storeImage(store)
			if werr.RespondJSON(rsp) {
				return
			}

			web.JsonSuccess(rsp, &struct {
				ID       int64  `json:"id"`
				Base62ID string `json:"base62id"`
			}{
				ID:       id,
				Base62ID: b62.Encode(id + 10000),
			})
			return
		} else if id_s, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/update"); ok {
			// TODO: Lock down based on basic_auth.username == collectionName.

			id := b62.Decode(id_s) - 10000

			// Get existing image for update:
			img, werr := getImage(id)
			if werr.RespondJSON(rsp) {
				return
			}

			// Decode JSON straight onto Image record:
			jd := json.NewDecoder(req.Body)
			err := jd.Decode(img)
			if web.JsonErrorIf(rsp, err, http.StatusBadRequest) {
				return
			}

			// Process the update request:
			if useAPI(func(api *API) *web.WebError {
				return web.AsWebError(api.Update(img), http.StatusInternalServerError)
			}).RespondJSON(rsp) {
				return
			}

			web.JsonSuccess(rsp, &struct {
				Success bool `json:"success"`
			}{
				Success: true,
			})
			return
		} else if id_s, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/delete"); ok {
			// TODO: Lock down based on basic_auth.username == collectionName.

			id := b62.Decode(id_s) - 10000

			if useAPI(func(api *API) *web.WebError {
				return web.AsWebError(api.Delete(id), http.StatusInternalServerError)
			}).RespondJSON(rsp) {
				return
			}

			web.JsonSuccess(rsp, &struct {
				Success bool `json:"success"`
			}{
				Success: true,
			})
			return
		} else if id_s, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/crop"); ok {
			id := b62.Decode(id_s) - 10000

			cr := &struct {
				Left   int `json:"left"`
				Top    int `json:"top"`
				Right  int `json:"right"`
				Bottom int `json:"bottom"`
			}{}

			jd := json.NewDecoder(req.Body)
			err := jd.Decode(cr)
			if web.JsonErrorIf(rsp, err, http.StatusBadRequest) {
				return
			}

			var img *Image
			if useAPI(func(api *API) *web.WebError {
				var err error
				img, err = api.GetImage(id)
				return web.AsWebError(err, http.StatusInternalServerError)
			}).RespondJSON(rsp) {
				return
			}
			if img == nil {
				web.AsWebError(fmt.Errorf("Could not find image by ID"), http.StatusNotFound).RespondJSON(rsp)
				return
			}

			// Crop the image:
			// _, ext, thumbExt := imageKindTo(img.Kind)
			_, ext, _ := imageKindTo(img.Kind)
			local_path := path.Join(store_folder(), strconv.FormatInt(img.ID, 10)+ext)
			tmp_output, err := cropImage(local_path, cr.Left, cr.Top, cr.Right, cr.Bottom)
			if web.AsWebError(err, http.StatusInternalServerError).RespondJSON(rsp) {
				return
			}

			// Clone the image record to a new record:
			if useAPI(func(api *API) *web.WebError {
				var err error
				img.ID = 0
				img.ID, err = api.NewImage(img)
				return web.AsWebError(err, http.StatusInternalServerError)
			}).RespondJSON(rsp) {
				return
			}

			// Move the temp file to the final storage path:
			img_name := strconv.FormatInt(img.ID, 10)
			os.MkdirAll(store_folder(), 0755)
			store_path := path.Join(store_folder(), img_name+ext)
			if web.AsWebError(os.Rename(tmp_output, store_path), http.StatusInternalServerError).RespondJSON(rsp) {
				return
			}

			//// Generate a thumbnail:
			//os.MkdirAll(thumb_folder(), 0755)
			//thumb_path := path.Join(thumb_folder(), img_name+thumbExt)
			//if web.AsWebError(generateThumbnail(firstImage, newImage.Kind, thumb_path), http.StatusInternalServerError).RespondJSON(rsp) {
			//	return
			//}
			width, height := cr.Right-cr.Left, cr.Bottom-cr.Top

			web.JsonSuccess(rsp, &struct {
				ID             int64  `json:"id"`
				Base62ID       string `json:"base62id"`
				Title          string `json:"title"`
				CollectionName string `json:"collectionName,omitempty"`
				Submitter      string `json:"submitter,omitempty"`
				Kind           string `json:"kind"`
				Width          *int   `json:"width,omitempty"`
				Height         *int   `json:"height,omitempty"`
			}{
				ID:             img.ID,
				Base62ID:       b62.Encode(img.ID + 10000),
				Kind:           img.Kind,
				Title:          img.Title,
				CollectionName: img.CollectionName,
				Submitter:      img.Submitter,
				Width:          &width,
				Height:         &height,
			})
			return
		}

		rsp.WriteHeader(http.StatusBadRequest)
		return
	}

	// GET:
	_, showUnclean := req.URL.Query()["all"]

	var orderBy ImagesOrderBy
	if _, ok := req.URL.Query()["newest"]; ok {
		orderBy = ImagesOrderByIDDESC
	} else if _, ok := req.URL.Query()["oldest"]; ok {
		orderBy = ImagesOrderByIDASC
	} else {
		orderBy = ImagesOrderByTitleASC
	}

	if req.URL.Path == "/favicon.ico" {
		rsp.WriteHeader(http.StatusNoContent)
		return
	} else if req.URL.Path == "/" {
		// Render a list page:
		list, werr := getAll(orderBy)
		if werr.RespondHTML(rsp) {
			return
		}

		listCollection(rsp, req, "", list, showUnclean)
		return
	} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/col/list"); ok {
		list, werr := getList(collectionName, orderBy)
		if werr.RespondHTML(rsp) {
			return
		}

		listCollection(rsp, req, collectionName, list, showUnclean)
		return
	} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/col/only"); ok {
		list, werr := getListOnly(collectionName, orderBy)
		if werr.RespondHTML(rsp) {
			return
		}

		listCollection(rsp, req, collectionName, list, showUnclean)
		return
	} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/col/add"); ok {
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
		if web.AsWebError(uiTmpl.ExecuteTemplate(rsp, "new", model), http.StatusInternalServerError).RespondHTML(rsp) {
			return
		}
		return
	} else if v_id, ok := web.MatchSimpleRoute(req.URL.Path, "/view/yt"); ok {
		// GET /view/yt/<video_id> to display a youtube player page for <video_id>, e.g. `dQw4w9WgXcQ`
		model := viewTemplateModel{
			BGColor:    "black",
			FillScreen: true,
			Query:      flattenQuery(req.URL.Query()),
			Image: *xlatImageViewModel(&Image{
				ID:             int64(0),
				Kind:           "youtube",
				Title:          v_id,
				SourceURL:      &v_id,
				CollectionName: "",
				Submitter:      "",
				RedirectToID:   nil,
				IsHidden:       true,
				IsClean:        false,
			}, nil),
		}

		// Set controls=1 if it's missing:
		if _, ok := model.Query["controls"]; !ok {
			model.Query["controls"] = "1"
		}

		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		if web.AsWebError(uiTmpl.ExecuteTemplate(rsp, "view", model), http.StatusInternalServerError).RespondHTML(rsp) {
			return
		}
		return
	} else if imgurl, ok := web.MatchSimpleRouteRaw(req.URL.Path, "/view/img/"); ok {
		// GET /view/img/<imgurl> to display an image viewer page for any URL <imgurl>, e.g. `//`
		model := viewTemplateModel{
			BGColor: "black",
			Query:   flattenQuery(req.URL.Query()),
			Image: ImageViewModel{
				ID:             int64(0),
				Base62ID:       "_",
				Title:          imgurl,
				Kind:           "jpeg",
				ImageURL:       imgurl,
				ThumbURL:       "",
				Submitter:      "",
				CollectionName: "",
				SourceURL:      &imgurl,
				RedirectToID:   nil,
				IsClean:        false,
			},
		}

		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		if web.AsWebError(uiTmpl.ExecuteTemplate(rsp, "view", model), http.StatusInternalServerError).RespondHTML(rsp) {
			return
		}
		return
	} else if ok := web.MatchExactRoute(req.URL.Path, "/api/v1/all"); ok {
		list, werr := getAll(orderBy)
		if werr.RespondJSON(rsp) {
			return
		}

		// Project into a view model:
		model := struct {
			List []ImageViewModel `json:"list"`
		}{
			List: projectModelList(list),
		}

		web.JsonSuccess(rsp, &model)
		return
	} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/list"); ok {
		list, werr := getList(collectionName, orderBy)
		if werr.RespondJSON(rsp) {
			return
		}

		// Project into a view model:
		model := struct {
			List []ImageViewModel `json:"list"`
		}{
			List: projectModelList(list),
		}

		web.JsonSuccess(rsp, &model)
		return
	} else if collectionName, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/only"); ok {
		list, werr := getListOnly(collectionName, orderBy)
		if werr.RespondJSON(rsp) {
			return
		}

		// Project into a view model:
		model := struct {
			List []ImageViewModel `json:"list"`
		}{
			List: projectModelList(list),
		}

		web.JsonSuccess(rsp, &model)
		return
	} else if id_s, ok := web.MatchSimpleRoute(req.URL.Path, "/api/v1/info"); ok {
		id := b62.Decode(id_s) - 10000

		var img *Image
		if useAPI(func(api *API) *web.WebError {
			var err error
			img, err = api.GetImage(id)
			return web.AsWebError(err, http.StatusInternalServerError)
		}).RespondJSON(rsp) {
			return
		}
		if img == nil {
			web.AsWebError(fmt.Errorf("Could not find image by ID"), http.StatusNotFound).RespondJSON(rsp)
			return
		}

		// Decode the image and grab its properties:
		_, ext, _ := imageKindTo(img.Kind)
		local_path := path.Join(store_folder(), strconv.FormatInt(img.ID, 10)+ext)

		model := &struct {
			ID             int64   `json:"id"`
			Base62ID       string  `json:"base62id"`
			Title          string  `json:"title"`
			CollectionName string  `json:"collectionName,omitempty"`
			Submitter      string  `json:"submitter,omitempty"`
			Kind           string  `json:"kind"`
			SourceURL      *string `json:"sourceURL,omitempty"`
			RedirectToID   *int64  `json:"redirectToID,omitempty"`
			Width          *int    `json:"width,omitempty"`
			Height         *int    `json:"height,omitempty"`
		}{
			ID:             id,
			Base62ID:       b62.Encode(id + 10000),
			Kind:           img.Kind,
			Title:          img.Title,
			CollectionName: img.CollectionName,
			Submitter:      img.Submitter,
			SourceURL:      img.SourceURL,
			RedirectToID:   img.RedirectToID,
		}
		if model.Kind == "" {
			model.Kind = "gif"
		}

		if model.Kind != "youtube" {
			var width, height int
			var err error

			width, height, model.Kind, err = getImageInfo(local_path)
			if web.AsWebError(err, http.StatusInternalServerError).RespondJSON(rsp) {
				return
			}

			model.Width = &width
			model.Height = &height
		}

		web.JsonSuccess(rsp, model)
		return
	}

	dir := path.Dir(req.URL.Path)

	// Look up the image's record by base62 encoded ID:
	filename := path.Base(req.URL.Path)
	filename = filename[0 : len(filename)-len(path.Ext(req.URL.Path))]

	id := b62.Decode(filename) - 10000

	var img *Image
	var err error
	if useAPI(func(api *API) *web.WebError {
		img, err = api.GetImage(id)
		if err != nil {
			return web.AsWebError(err, http.StatusInternalServerError)
		}
		if img == nil {
			return web.AsWebError(fmt.Errorf("No record for ID exists"), http.StatusNotFound)
		}

		// Follow redirect chain:
		for img.RedirectToID != nil {
			newimg, err := api.GetImage(*img.RedirectToID)
			if err != nil {
				return web.AsWebError(err, http.StatusInternalServerError)
			}
			img = newimg
		}

		return nil
	}).RespondHTML(rsp) {
		return
	}

	// Determine mime-type and file extension:
	if img.Kind == "" {
		img.Kind = "gif"
	}
	mime, ext, thumbExt := imageKindTo(img.Kind)

	// Find the image file:
	img_name := strconv.FormatInt(img.ID, 10)

	if dir == "/b" || dir == "/w" || dir == "/g" {
		// Render a black or white BG centered image viewer:
		var bgcolor string
		switch dir {
		case "/b":
			bgcolor = "black"
		case "/w":
			bgcolor = "white"
		case "/g":
			bgcolor = "gray"
		}

		model := viewTemplateModel{
			BGColor: bgcolor,
			Query:   flattenQuery(req.URL.Query()),
			Image:   *xlatImageViewModel(img, nil),
		}

		rsp.Header().Set("Content-Type", "text/html; charset=utf-8")
		if web.AsWebError(uiTmpl.ExecuteTemplate(rsp, "view", model), http.StatusInternalServerError).RespondHTML(rsp) {
			return
		}

		return
	} else if dir == "/t" {
		// Serve thumbnail file:
		local_path := path.Join(store_folder(), img_name+ext)
		thumb_path := path.Join(thumb_folder(), img_name+thumbExt)
		if web.AsWebError(ensureThumbnail(local_path, thumb_path), http.StatusInternalServerError).RespondHTML(rsp) {
			runtime.GC()
			return
		}

		if xrThumb != "" {
			// Pass request to nginx to serve static content file:
			redirPath := path.Join(xrThumb, img_name+thumbExt)

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
