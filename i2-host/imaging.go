package main

import (
	"fmt"
	"io"
	//"log"
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

func subImage(img image.Image, srcBounds image.Rectangle) image.Image {
	switch si := img.(type) {
	case *image.RGBA:
		return si.SubImage(srcBounds)
	case *image.YCbCr:
		return si.SubImage(srcBounds)
	case *image.Paletted:
		return si.SubImage(srcBounds)
	case *image.RGBA64:
		return si.SubImage(srcBounds)
	case *image.NRGBA:
		return si.SubImage(srcBounds)
	case *image.NRGBA64:
		return si.SubImage(srcBounds)
	case *image.Alpha:
		return si.SubImage(srcBounds)
	case *image.Alpha16:
		return si.SubImage(srcBounds)
	case *image.Gray:
		return si.SubImage(srcBounds)
	case *image.Gray16:
		return si.SubImage(srcBounds)
	default:
		panic(fmt.Errorf("Unhandled image format type: %s", reflect.TypeOf(img).Name()))
	}
}

// Copies an image to a new image:
func cloneImage(src image.Image) image.Image {
	srcBounds := src.Bounds().Canon()
	zeroedBounds := srcBounds.Sub(srcBounds.Min)

	switch si := src.(type) {
	case *image.RGBA:
		out := image.NewRGBA(zeroedBounds)
		for y := srcBounds.Min.Y; y <= srcBounds.Max.Y; y++ {
			for x := srcBounds.Min.X; x <= srcBounds.Max.X; x++ {
				out.SetRGBA(x-srcBounds.Min.X, y-srcBounds.Min.Y, si.At(x, y).(color.RGBA))
			}
		}
		return out
	case *image.YCbCr:
		out := image.NewYCbCr(zeroedBounds, si.SubsampleRatio)
		for y := srcBounds.Min.Y; y <= srcBounds.Max.Y; y++ {
			for x := srcBounds.Min.X; x <= srcBounds.Max.X; x++ {
				ycbcr := si.At(x, y).(color.YCbCr)
				out.Y[(y-srcBounds.Min.Y)*si.YStride+(x-srcBounds.Min.X)] = ycbcr.Y
				out.Cb[(y-srcBounds.Min.Y)*si.CStride+(x-srcBounds.Min.X)] = ycbcr.Cb
				out.Cr[(y-srcBounds.Min.Y)*si.CStride+(x-srcBounds.Min.X)] = ycbcr.Cr
			}
		}
		return out
	case *image.Paletted:
		out := image.NewPaletted(zeroedBounds, si.Palette)
		for y := srcBounds.Min.Y; y <= srcBounds.Max.Y; y++ {
			for x := srcBounds.Min.X; x <= srcBounds.Max.X; x++ {
				out.SetColorIndex(x-srcBounds.Min.X, y-srcBounds.Min.Y, si.ColorIndexAt(x, y))
			}
		}
		return out
	case *image.RGBA64:
		out := image.NewRGBA64(zeroedBounds)
		for y := srcBounds.Min.Y; y <= srcBounds.Max.Y; y++ {
			for x := srcBounds.Min.X; x <= srcBounds.Max.X; x++ {
				out.SetRGBA64(x-srcBounds.Min.X, y-srcBounds.Min.Y, si.At(x, y).(color.RGBA64))
			}
		}
		return out
	case *image.NRGBA:
		out := image.NewNRGBA(zeroedBounds)
		for y := srcBounds.Min.Y; y <= srcBounds.Max.Y; y++ {
			for x := srcBounds.Min.X; x <= srcBounds.Max.X; x++ {
				out.SetNRGBA(x-srcBounds.Min.X, y-srcBounds.Min.Y, si.At(x, y).(color.NRGBA))
			}
		}
		return out
	case *image.NRGBA64:
		out := image.NewNRGBA64(zeroedBounds)
		for y := srcBounds.Min.Y; y <= srcBounds.Max.Y; y++ {
			for x := srcBounds.Min.X; x <= srcBounds.Max.X; x++ {
				out.SetNRGBA64(x-srcBounds.Min.X, y-srcBounds.Min.Y, si.At(x, y).(color.NRGBA64))
			}
		}
		return out
	case *image.Alpha:
		out := image.NewAlpha(zeroedBounds)
		for y := srcBounds.Min.Y; y <= srcBounds.Max.Y; y++ {
			for x := srcBounds.Min.X; x <= srcBounds.Max.X; x++ {
				out.SetAlpha(x-srcBounds.Min.X, y-srcBounds.Min.Y, si.At(x, y).(color.Alpha))
			}
		}
		return out
	case *image.Alpha16:
		out := image.NewAlpha16(zeroedBounds)
		for y := srcBounds.Min.Y; y <= srcBounds.Max.Y; y++ {
			for x := srcBounds.Min.X; x <= srcBounds.Max.X; x++ {
				out.SetAlpha16(x-srcBounds.Min.X, y-srcBounds.Min.Y, si.At(x, y).(color.Alpha16))
			}
		}
		return out
	case *image.Gray:
		out := image.NewGray(zeroedBounds)
		for y := srcBounds.Min.Y; y <= srcBounds.Max.Y; y++ {
			for x := srcBounds.Min.X; x <= srcBounds.Max.X; x++ {
				out.SetGray(x-srcBounds.Min.X, y-srcBounds.Min.Y, si.At(x, y).(color.Gray))
			}
		}
		return out
	case *image.Gray16:
		out := image.NewGray16(zeroedBounds)
		for y := srcBounds.Min.Y; y <= srcBounds.Max.Y; y++ {
			for x := srcBounds.Min.X; x <= srcBounds.Max.X; x++ {
				out.SetGray16(x-srcBounds.Min.X, y-srcBounds.Min.Y, si.At(x, y).(color.Gray16))
			}
		}
		return out
	default:
		panic(fmt.Errorf("Unhandled image format type: %s", reflect.TypeOf(src).Name()))
	}
}

// Gets an image's configuration:
func getImageInfo(image_path string) (w int, h int, kind string, err error) {
	imf, err := os.Open(image_path)
	if err != nil {
		return 0, 0, "", err
	}
	defer imf.Close()

	config, kind, err := image.DecodeConfig(imf)
	if err != nil {
		return 0, 0, "", err
	}

	return config.Width, config.Height, kind, nil
}

// Crops an image and outputs a new image file:
func cropImage(image_path string, left, top, right, bottom int) (tmp_output string, err error) {
	cropBounds := image.Rect(left, top, right, bottom)

	// Open the image for reading:
	imf, err := os.Open(image_path)
	if err != nil {
		return "", err
	}
	defer imf.Close()

	// Figure out what kind of image it is:
	_, imageKind, err := image.DecodeConfig(imf)
	if err != nil {
		return "", err
	}
	imf.Seek(0, 0)

	// Crop images:
	switch imageKind {
	case "gif":
		// Decode all GIF frames and crop them:

		// FIXME(jsd): This approach clearly does not work. An integrated decoder-encoder needs to be written
		// for animated GIFs so as to preserve as much of the encoding details as possible and crop the
		// transparent animation subframes over the full image.

		var g *gif.GIF
		g, err = gif.DecodeAll(imf)
		if err != nil {
			return "", err
		}

		// Crop all the frames:
		for i, img := range g.Image {
			if !cropBounds.In(img.Bounds()) {
				return "", fmt.Errorf("Crop boundaries are not contained within image boundaries")
			}

			g.Image[i] = cloneImage(img.SubImage(cropBounds)).(*image.Paletted)
		}

		// Write the cropped images to a new GIF:
		tmpf, err := TempFile(tmp_folder(), "crop-", ".gif")
		if err != nil {
			return "", err
		}
		defer tmpf.Close()

		err = gif.EncodeAll(tmpf, g)
		if err != nil {
			return "", err
		}

		g.Image = nil
		g.Delay = nil
		g = nil

		return tmpf.Name(), nil
	case "jpeg":
		img, err := jpeg.Decode(imf)
		if err != nil {
			return "", err
		}

		if !cropBounds.In(img.Bounds()) {
			return "", fmt.Errorf("Crop boundaries are not contained within image boundaries")
		}

		tmpf, err := TempFile(tmp_folder(), "crop-", ".jpg")
		if err != nil {
			return "", err
		}
		defer tmpf.Close()

		img = cloneImage(subImage(img, cropBounds))
		err = jpeg.Encode(tmpf, img, &jpeg.Options{Quality: 100})
		if err != nil {
			return "", err
		}

		return tmpf.Name(), nil
	case "png":
		img, err := png.Decode(imf)
		if err != nil {
			return "", err
		}

		if !cropBounds.In(img.Bounds()) {
			return "", fmt.Errorf("Crop boundaries are not contained within image boundaries")
		}

		tmpf, err := TempFile(tmp_folder(), "crop-", ".png")
		if err != nil {
			return "", err
		}
		defer tmpf.Close()

		img = subImage(img, cropBounds)
		err = png.Encode(tmpf, img)
		if err != nil {
			return "", err
		}

		return tmpf.Name(), nil
	default:
		return "", fmt.Errorf("Unrecognized image kind '%s'", imageKind)
	}
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
	boximg := subImage(img, srcBounds)

	img = nil

	// Apply resizing algorithm:
	thumbImg = imaging.Resize(boximg, dimensions, dimensions, imaging.Lanczos)

	boximg = nil

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
	defer func() {
		firstImage = nil
		thumbImg = nil
	}()

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
			return nil, "", err
		}

		firstFrame := g.Image[0]
		g.Image = nil
		g.Delay = nil
		g = nil

		return firstFrame, imageKind, nil
	default:
		firstImage, imageKind, err = image.Decode(imf)
		if err != nil {
			return nil, "", err
		}
		return
	}
}
