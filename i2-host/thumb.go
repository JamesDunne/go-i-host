package main

import (
	"fmt"
	"io"
	"log"
	"os"
	//"path"
	"reflect"
)

import (
	"github.com/JamesDunne/go-util/imaging/gif" // my own patches to image/gif
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
)

import "github.com/JamesDunne/go-util/imaging"

func makeThumbnail(img image.Image, dimensions int) (thumbImg image.Image) {
	// Calculate the largest square bounds for a thumbnail to preserve aspect ratio
	b := img.Bounds()
	srcBounds := b
	dx, dy := b.Dx(), b.Dy()
	if dx > dy {
		offs := (dx - dy) / 2
		srcBounds.Min.X += offs
		srcBounds.Max.X -= offs
	} else if dy > dx {
		offs := (dy - dx) / 2
		srcBounds.Min.Y += offs
		srcBounds.Max.Y -= offs
	} else {
		// Already square.
	}

	//log.Printf("'%s': resize %v to %v\n", filename, b, srcBounds)

	// Cut out the center square to a new image:
	var boximg image.Image
	switch si := img.(type) {
	case *image.RGBA:
		boximg = si.SubImage(srcBounds)
	case *image.YCbCr:
		boximg = si.SubImage(srcBounds)
	case *image.Paletted:
		boximg = si.SubImage(srcBounds)
	case *image.RGBA64:
		panic(fmt.Errorf("Unhandled image format type: %s", "RGBA64"))
	case *image.NRGBA:
		boximg = si.SubImage(srcBounds)
	case *image.NRGBA64:
		panic(fmt.Errorf("Unhandled image format type: %s", "NRGBA64"))
	case *image.Alpha:
		panic(fmt.Errorf("Unhandled image format type: %s", "Alpha"))
	case *image.Alpha16:
		panic(fmt.Errorf("Unhandled image format type: %s", "Alpha16"))
	case *image.Gray:
		boximg = si.SubImage(srcBounds)
	case *image.Gray16:
		panic(fmt.Errorf("Unhandled image format type: %s", "Gray16"))
	default:
		panic(fmt.Errorf("Unhandled image format type: %s", reflect.TypeOf(img).Name()))
	}

	//log.Printf("'%s': resized to %v\n", filename, boximg.Bounds())

	//frame, err := os.Create(path.Join(tmp_folder(), "/fr.png"))
	//if err == nil {
	//	png.Encode(frame, boximg)
	//	frame.Close()
	//}

	// Apply resizing algorithm:
	thumbImg = imaging.Resize(boximg, dimensions, dimensions, imaging.Lanczos)

	//frame, err = os.Create(path.Join(tmp_folder(), "/fr-resize.png"))
	//if err == nil {
	//	png.Encode(frame, thumbImg)
	//	frame.Close()
	//}

	return
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

func blitGIFFrame(src *image.Paletted, dest *image.RGBA) {
	// Copy the non-transparent bits onto the current frame:
	for y := src.Rect.Min.Y; y < src.Rect.Max.Y; y++ {
		for x := src.Rect.Min.X; x < src.Rect.Max.X; x++ {
			c := src.At(x, y)
			_, _, _, a := c.RGBA()
			if a == 0 {
				continue
			}
			dest.Set(x, y, c)
		}
	}
}

func decodeFirstImage(local_path string) (firstImage image.Image, imageKind string, err error) {
	imf, err := os.Open(local_path)
	if err != nil {
		return nil, "", err
	}
	defer imf.Close()

	_, imageKind, err = image.DecodeConfig(imf)
	if err != nil {
		return nil, "", err
	}
	imf.Seek(0, 0)

	switch imageKind {
	case "gif":
		// Decode GIF frames until we reach the first fully opaque image:
		var g *gif.GIF
		g, err = gif.DecodeAll(imf)
		if err != nil {
			log.Printf("  err: %s\n", err.Error())
			return nil, "", err
		}

		// Process each frame until we've constructed an opaque image:
		firstFrame := g.Image[0]

		fr := image.NewRGBA(firstFrame.Bounds())

		// Clear to black:
		for y := fr.Rect.Min.Y; y < fr.Rect.Max.Y; y++ {
			for x := fr.Rect.Min.X; x < fr.Rect.Max.X; x++ {
				fr.SetRGBA(x, y, color.RGBA{R: 0, G: 0, B: 0, A: 0})
			}
		}

		// Try for the second image:
		for _, img := range g.Image {
			// Copy the non-transparent bits onto the current frame:
			blitGIFFrame(img, fr)

			if fr.Opaque() {
				break
			}

			//frame, err := os.Create(path.Join(tmp_folder(), fmt.Sprintf("/fr%d.png", i+1)))
			//if err == nil {
			//	png.Encode(frame, fr)
			//	frame.Close()
			//}
		}

		//blitGIFFrame(firstFrame, fr)

		return fr, imageKind, nil
	default:
		firstImage, imageKind, err = image.Decode(imf)
		if err != nil {
			return nil, "", err
		}
		return
	}
}
