package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// The Kitty graphics protocol with Unicode placeholders lets real bitmaps live
// inside a TUI cell grid: the image is transmitted once (out of band, as a
// zero-width escape), then referenced by a rectangle of placeholder cells whose
// foreground color carries the image id and whose diacritic carries the row.
// The terminal paints the image over those cells. Kitty and Ghostty support it.

// placeholderChar is U+10EEEE, the Kitty Unicode placeholder (width 1).
const placeholderChar = "\U0010EEEE"

// rowDiacritics maps a row index to its combining mark (Kitty's
// rowcolumn-diacritics table). Only each row's first cell needs one; columns
// auto-increment from there.
var rowDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F,
	0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
}

// KittyCapable reports whether the terminal speaks the Kitty graphics protocol.
// GOOSENECK_IMAGES=halfblock|kitty|off forces a mode.
func KittyCapable() bool {
	switch os.Getenv("GOOSENECK_IMAGES") {
	case "kitty":
		return true
	case "halfblock", "off":
		return false
	}
	if os.Getenv("TMUX") != "" || strings.HasPrefix(os.Getenv("TERM"), "screen") {
		return false // multiplexers don't pass placeholders through reliably
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" || os.Getenv("GHOSTTY_BIN_DIR") != "" {
		return true
	}
	t := os.Getenv("TERM")
	return strings.Contains(t, "kitty") || strings.Contains(t, "ghostty")
}

// kittyTransmit uploads a PNG under imgID (chunked base64, replies suppressed).
func kittyTransmit(imgID uint32, png []byte) string {
	data := base64.StdEncoding.EncodeToString(png)
	var sb strings.Builder
	first := true
	for len(data) > 0 {
		n := min(4096, len(data))
		chunk := data[:n]
		data = data[n:]
		more := 0
		if len(data) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(&sb, "\x1b_Gf=100,a=t,i=%d,q=2,m=%d;%s\x1b\\", imgID, more, chunk)
			first = false
		} else {
			fmt.Fprintf(&sb, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
	return sb.String()
}

// kittyPlacement creates the virtual (placeholder) placement for imgID sized
// cols×rows cells.
func kittyPlacement(imgID uint32, cols, rows int) string {
	return fmt.Sprintf("\x1b_Ga=p,i=%d,p=1,U=1,c=%d,r=%d,q=2\x1b\\", imgID, cols, rows)
}

// kittyDeleteAll frees every image this program transmitted.
func kittyDeleteAll() string { return "\x1b_Ga=d,d=A,q=2\x1b\\" }

// placeholderBlock builds the cols×rows grid of placeholder cells for imgID.
func placeholderBlock(imgID uint32, cols, rows int) string {
	fgc := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", uint8(imgID>>16), uint8(imgID>>8), uint8(imgID))
	var sb strings.Builder
	for y := 0; y < rows; y++ {
		sb.WriteString(fgc)
		for x := 0; x < cols; x++ {
			sb.WriteString(placeholderChar)
			if x == 0 {
				sb.WriteRune(rowDiacritics[y%len(rowDiacritics)])
			}
		}
		sb.WriteString(reset)
		if y < rows-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
