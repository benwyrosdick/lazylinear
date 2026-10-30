package ui

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jroimartin/gocui"
)

// markdownImagePattern matches markdown images: ![alt](url "optional title")
var markdownImagePattern = regexp.MustCompile(`!\[([^\]]*)\]\(\s*<?([^\s)>]+)>?(?:\s+"[^"]*")?\s*\)`)

// imageTokenPattern matches the placeholder that marks the top-left cell of an inline image
var imageTokenPattern = regexp.MustCompile(`LLIMG(\d+)X`)

// firstImageID is the first kitty image ID used, chosen to avoid colliding with other programs
const firstImageID = 0x4c4c0001

// maxImagePixels rejects images too large to decode comfortably
const maxImagePixels = 50_000_000

// hiDPICellHeight is the cell height, in pixels, at which a terminal is assumed to draw at
// twice the desktop's pixel size, as on a retina Mac or under fractional scaling
const hiDPICellHeight = 28

// inlineImage is a downloaded image, PNG encoded for the terminal
type inlineImage struct {
	id          uint32
	data        []byte
	width       int
	height      int
	scale       float64
	err         error
	transmitted bool
}

// imageRef is an image referenced from the inspected issue's markdown
type imageRef struct {
	url  string
	alt  string
	rows int
	cols int
}

// imagePlacement is part of an image drawn at a screen cell: the source pixels
// cropX, cropY, cropW, cropH scaled to fill cols by rows cells
type imagePlacement struct {
	image     *inlineImage
	label     string
	placement int
	x         int
	y         int
	cols      int
	rows      int
	cropX     int
	cropY     int
	cropW     int
	cropH     int
}

// openImageTerminal returns the terminal to draw images on when it supports the
// kitty graphics protocol (kitty, Ghostty, and others), or nil. Setting
// LAZYLINEAR_IMAGES=0 turns images off.
func openImageTerminal() *os.File {
	if os.Getenv("LAZYLINEAR_IMAGES") == "0" {
		return nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	if !detectKittyGraphics(tty) {
		tty.Close()
		return nil
	}
	return tty
}

// renderMarkdownWithImages renders markdown, reserving blank rows where inline images are drawn
func (ui *UI) renderMarkdownWithImages(text string) string {
	if ui.tty == nil {
		return ui.renderMarkdown(text)
	}

	text = markdownImagePattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := markdownImagePattern.FindStringSubmatch(match)
		ui.inspectorImages = append(ui.inspectorImages, imageRef{alt: parts[1], url: parts[2]})
		return "\n\n" + imageToken(len(ui.inspectorImages)-1) + "\n\n"
	})

	var lines []string
	for _, line := range strings.Split(ui.renderMarkdown(text), "\n") {
		plain := ansiPattern.ReplaceAllString(line, "")
		match := imageTokenPattern.FindStringSubmatchIndex(plain)
		if match == nil {
			lines = append(lines, line)
			continue
		}
		index, _ := strconv.Atoi(plain[match[2]:match[3]])
		indent := utf8.RuneCountInString(plain[:match[0]])
		lines = append(lines, ui.imageLines(index, indent)...)
	}
	return strings.Join(lines, "\n")
}

// imageLines returns the rows an image occupies: its token followed by blank rows,
// or a one-line note while it loads or when it can't be shown
func (ui *UI) imageLines(index int, indent int) []string {
	ref := &ui.inspectorImages[index]
	img := ui.loadImage(ref.url)
	padding := strings.Repeat(" ", indent)
	label := ref.alt
	if label == "" {
		label = "image"
	}

	if img == nil {
		return []string{padding + "\033[38;5;245mLoading " + label + "…\033[0m"}
	}
	maxCols := ui.mdWidth - indent - 1
	if img.err != nil || maxCols < 4 {
		reason := ""
		if img.err != nil {
			reason = " (" + img.err.Error() + ")"
		}
		return []string{padding + "\033[38;5;245mImage: " + label + reason + " → " + ref.url + "\033[0m"}
	}

	ref.cols, ref.rows = ui.imageCells(img, maxCols, max(ui.mdHeight-1, 1))
	lines := make([]string, ref.rows)
	lines[0] = padding + imageToken(index)
	return lines
}

// cellSize returns the terminal cell size in pixels, guessing when the terminal doesn't report it
func (ui *UI) cellSize() (int, int) {
	if ui.cellWidth == 0 || ui.cellHeight == 0 {
		return 10, 20
	}
	return ui.cellWidth, ui.cellHeight
}

// displayScale returns how many terminal pixels make up one desktop pixel
func (ui *UI) displayScale() float64 {
	if ui.imageScale > 0 {
		return ui.imageScale
	}
	if _, cellH := ui.cellSize(); cellH >= hiDPICellHeight {
		return 2
	}
	return 1
}

// naturalWidth returns the terminal pixels an image spans at 100%, which is one image pixel per
// desktop pixel, or per point for a retina image, as a browser would show it
func (ui *UI) naturalWidth(img *inlineImage) float64 {
	return float64(img.width) * ui.displayScale() / img.scale
}

// imageCells returns the cells an image fills at 100%, shrunk to fit maxCols by maxRows
func (ui *UI) imageCells(img *inlineImage, maxCols, maxRows int) (int, int) {
	cellW, cellH := ui.cellSize()
	width := max(int(math.Round(ui.naturalWidth(img))), 1)
	height := max(int(math.Round(float64(width)*float64(img.height)/float64(img.width))), 1)
	return imageSize(width, height, cellW, cellH, maxCols, maxRows)
}

func imageToken(index int) string {
	return fmt.Sprintf("LLIMG%dX", index)
}

// imageSize returns the cells an image fills: its natural size, shrunk to fit maxCols by maxRows
func imageSize(width, height, cellW, cellH, maxCols, maxRows int) (int, int) {
	cols := min((width+cellW-1)/cellW, maxCols)
	rows := int(math.Round(float64(cols*cellW) * float64(height) / float64(width) / float64(cellH)))
	if rows > maxRows {
		rows = maxRows
		cols = int(math.Round(float64(rows*cellH) * float64(width) / float64(height) / float64(cellW)))
	}
	return max(cols, 1), max(rows, 1)
}

// loadImage returns a downloaded image, or nil while it is still loading
func (ui *UI) loadImage(url string) *inlineImage {
	ui.imagesMu.Lock()
	defer ui.imagesMu.Unlock()
	if img, ok := ui.images[url]; ok {
		return img
	}
	ui.images[url] = nil
	go ui.fetchImage(url)
	return nil
}

// fetchImage downloads an image and redraws the inspector once it is ready
func (ui *UI) fetchImage(url string) {
	img := &inlineImage{scale: 1}
	if ui.client == nil {
		img.err = fmt.Errorf("no client")
	} else if data, err := ui.client.DownloadFile(context.Background(), url); err != nil {
		img.err = err
	} else {
		img.data, img.width, img.height, img.err = encodePNG(data)
		img.scale = pngPixelScale(img.data)
	}

	ui.imagesMu.Lock()
	img.id = ui.nextImageID
	ui.nextImageID++
	ui.images[url] = img
	ui.imagesMu.Unlock()

	ui.gui.Update(func(g *gocui.Gui) error {
		ui.lastRenderedID = ""
		return nil
	})
}

// encodePNG converts image data to PNG, the format sent to the terminal, and returns its size
func encodePNG(data []byte) ([]byte, int, int, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("unsupported image: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > maxImagePixels {
		return nil, 0, 0, fmt.Errorf("image is %dx%d", config.Width, config.Height)
	}
	if format == "png" {
		return data, config.Width, config.Height, nil
	}

	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, decoded); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), config.Width, config.Height, nil
}

// pngPixelScale returns the image pixels per point recorded in a PNG's pHYs chunk: 2 for a
// retina screenshot saved at 144 DPI, and 1 when the image isn't marked as high resolution
func pngPixelScale(data []byte) float64 {
	const signatureLen = 8
	for i := signatureLen; i+8 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[i:]))
		chunk := string(data[i+4 : i+8])
		body := i + 8
		if chunk == "IDAT" || length < 0 || body+length > len(data) {
			break
		}
		// pHYs holds pixels per unit for x and y, then a unit byte where 1 means meters
		if chunk == "pHYs" && length == 9 && data[body+8] == 1 {
			dpi := float64(binary.BigEndian.Uint32(data[body:])) * 0.0254
			// Only accept the exact multiples of 72 DPI that macOS writes, not print resolutions
			if scale := math.Round(dpi / 72); scale >= 2 && scale <= 3 && math.Abs(dpi-scale*72) < 1 {
				return scale
			}
			break
		}
		i = body + length + 4
	}
	return 1
}

// inspectorImagePlacements finds where each image token lands in the wrapped inspector
// content and crops each image to the rows visible in the view
func (ui *UI) inspectorImagePlacements(content string, v *gocui.View, x0, y0 int) []imagePlacement {
	width, height := v.Size()
	_, oy := v.Origin()
	visibleEnd := oy + height

	var placements []imagePlacement
	row := 0
	for _, line := range strings.Split(content, "\n") {
		plain := ansiPattern.ReplaceAllString(line, "")
		if match := imageTokenPattern.FindStringSubmatchIndex(plain); match != nil {
			index, _ := strconv.Atoi(plain[match[2]:match[3]])
			if index < len(ui.inspectorImages) {
				ref := ui.inspectorImages[index]
				ui.imagesMu.Lock()
				img := ui.images[ref.url]
				ui.imagesMu.Unlock()

				start, end := max(row, oy), min(row+ref.rows, visibleEnd)
				if img != nil && img.err == nil && ref.rows > 0 && end > start {
					placements = append(placements, imagePlacement{
						image: img,
						label: ref.alt,
						x:     x0 + 1 + utf8.RuneCountInString(plain[:match[0]]),
						y:     y0 + 1 + start - oy,
						cols:  ref.cols,
						rows:  end - start,
						cropY: (start - row) * img.height / ref.rows,
						cropW: img.width,
						cropH: max((end-start)*img.height/ref.rows, 1),
					})
				}
			}
		}
		row += wrappedRows(plain, width)
	}
	return placements
}

// cutOut splits placements around the screen rectangle x, y, w, h so that no image is drawn inside it
func cutOut(placements []imagePlacement, x, y, w, h int) []imagePlacement {
	var out []imagePlacement
	for _, p := range placements {
		// The rows and columns of p, from its top-left cell, that the rectangle covers
		top, bottom := min(max(y-p.y, 0), p.rows), min(max(y+h-p.y, 0), p.rows)
		left, right := min(max(x-p.x, 0), p.cols), min(max(x+w-p.x, 0), p.cols)
		if top == bottom || left == right {
			out = append(out, p)
			continue
		}
		out = appendPiece(out, p, 0, 0, p.cols, top)
		out = appendPiece(out, p, 0, bottom, p.cols, p.rows)
		out = appendPiece(out, p, 0, top, left, bottom)
		out = appendPiece(out, p, right, top, p.cols, bottom)
	}
	return out
}

// appendPiece appends the part of p covering columns c0 to c1 and rows r0 to r1, if it isn't empty
func appendPiece(out []imagePlacement, p imagePlacement, c0, r0, c1, r1 int) []imagePlacement {
	if c1 <= c0 || r1 <= r0 {
		return out
	}
	piece := p
	piece.x, piece.y = p.x+c0, p.y+r0
	piece.cols, piece.rows = c1-c0, r1-r0
	piece.cropX = p.cropX + c0*p.cropW/p.cols
	piece.cropY = p.cropY + r0*p.cropH/p.rows
	piece.cropW = max((c1-c0)*p.cropW/p.cols, 1)
	piece.cropH = max((r1-r0)*p.cropH/p.rows, 1)
	return append(out, piece)
}

// wrappedRows counts the rows gocui uses for a line in a wrapping view of the given width
func wrappedRows(line string, width int) int {
	n := utf8.RuneCountInString(line)
	if width <= 0 || n < width {
		return 1
	}
	return n/width + 1
}

// drawImages moves the terminal's image placements to match placements. Placements
// are written straight to the terminal, outside of gocui, and only when they change.
func (ui *UI) drawImages(placements []imagePlacement) {
	if ui.tty == nil || (!ui.redrawImages && slices.Equal(placements, ui.placedImages)) {
		return
	}
	ui.redrawImages = false
	for i := range placements {
		placements[i].placement = i + 1
	}

	var b strings.Builder
	b.WriteString("\x1b7")
	for _, p := range ui.placedImages {
		fmt.Fprintf(&b, "\x1b_Ga=d,d=i,i=%d,p=%d,q=2\x1b\\", p.image.id, p.placement)
	}
	for _, p := range placements {
		if !p.image.transmitted {
			writeImageData(&b, p.image)
			p.image.transmitted = true
		}
		fmt.Fprintf(&b, "\x1b[%d;%dH\x1b_Ga=p,i=%d,p=%d,x=%d,y=%d,w=%d,h=%d,c=%d,r=%d,C=1,q=2\x1b\\",
			p.y+1, p.x+1, p.image.id, p.placement, p.cropX, p.cropY, p.cropW, p.cropH, p.cols, p.rows)
	}
	b.WriteString("\x1b8")
	ui.tty.WriteString(b.String())
	ui.placedImages = placements
}

// writeImageData transmits an image to the terminal in base64 chunks
func writeImageData(b *strings.Builder, img *inlineImage) {
	const chunkSize = 4096
	encoded := base64.StdEncoding.EncodeToString(img.data)
	for offset := 0; offset < len(encoded); offset += chunkSize {
		end := min(offset+chunkSize, len(encoded))
		more := 1
		if end == len(encoded) {
			more = 0
		}
		if offset == 0 {
			fmt.Fprintf(b, "\x1b_Ga=t,f=100,t=d,i=%d,q=2,m=%d;%s\x1b\\", img.id, more, encoded[offset:end])
		} else {
			fmt.Fprintf(b, "\x1b_Gm=%d,q=2;%s\x1b\\", more, encoded[offset:end])
		}
	}
}

// closeImages removes every image from the terminal and releases it
func (ui *UI) closeImages() {
	if ui.tty == nil {
		return
	}
	var b strings.Builder
	ui.imagesMu.Lock()
	for _, img := range ui.images {
		if img != nil && img.transmitted {
			fmt.Fprintf(&b, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", img.id)
		}
	}
	ui.imagesMu.Unlock()
	ui.tty.WriteString(b.String())
	ui.tty.Close()
	ui.tty = nil
}

// openImageAt opens the image viewer when the screen cell x, y is on an inline image
func (ui *UI) openImageAt(x, y int) bool {
	for _, p := range ui.placedImages {
		if x >= p.x && x < p.x+p.cols && y >= p.y && y < p.y+p.rows {
			viewed := p
			ui.viewedImage = &viewed
			return true
		}
	}
	return false
}

func (ui *UI) closeImage(g *gocui.Gui, v *gocui.View) error {
	ui.viewedImage = nil
	g.SetCurrentView("details")
	return nil
}

// imageModalPlacement centers the viewed image at 100% size, or shrunk to fit the screen if that is smaller
func (ui *UI) imageModalPlacement(maxX, maxY int) imagePlacement {
	img := ui.viewedImage.image
	cols, rows := ui.imageCells(img, max(maxX-4, 1), max(maxY-4, 1))
	return imagePlacement{
		image: img,
		label: ui.viewedImage.label,
		x:     (maxX - cols) / 2,
		y:     (maxY - rows) / 2,
		cols:  cols,
		rows:  rows,
		cropW: img.width,
		cropH: img.height,
	}
}

// imageModalTitle names the viewed image and its zoom level, fitting the frame width
func (ui *UI) imageModalTitle(p imagePlacement) string {
	cellW, _ := ui.cellSize()
	zoom := min(100, int(math.Round(float64(p.cols*cellW)*100/ui.naturalWidth(p.image))))
	suffix := fmt.Sprintf(" - %d%% (esc to close)", zoom)

	// gocui positions title characters by byte, so keep the label ASCII
	label := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return -1
		}
		return r
	}, p.label)
	if label == "" {
		label = "Image"
	}
	if room := p.cols - len(suffix) - 1; len(label) > room {
		label = label[:max(room, 0)]
	}
	return label + suffix
}
