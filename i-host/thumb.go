package main

import "image"
import "github.com/JamesDunne/go-util/imaging/resize"

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
	switch img := img.(type) {
	case *image.RGBA:
		boximg = img.SubImage(srcBounds)
	case *image.YCbCr:
		boximg = img.SubImage(srcBounds)
	case *image.Paletted:
		boximg = img.SubImage(srcBounds)
	}

	//log.Printf("'%s': resized to %v\n", filename, boximg.Bounds())

	// Apply resizing algorithm:
	thumbImg = resize.Resize(boximg, boximg.Bounds(), dimensions, dimensions)

	return
}
