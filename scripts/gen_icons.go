package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Render the TinyDrop (TD) icon at any resolution (e.g. 1024x1024, 256x256, 64x64, 32x32)
func renderTDIcon(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	fSize := float64(size)

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Normalize coordinates to 0..128 design space
			nx := (float64(x) + 0.5) / fSize * 128.0
			ny := (float64(y) + 0.5) / fSize * 128.0

			// 1. Squircle / Rounded Rect check: x,y in [8, 120], radius 28
			alpha := roundedRectCoverage(nx, ny, 8, 8, 112, 112, 28)
			if alpha <= 0 {
				continue
			}

			// 2. Base Diagonal Gradient: #38bdf8 -> #3b82f6 -> #6366f1
			t := ((nx - 8) + (ny - 8)) / 224.0
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
			var r, g, b float64
			if t < 0.5 {
				u := t * 2.0
				r = lerp(0x38, 0x3b, u)
				g = lerp(0xbd, 0x82, u)
				b = lerp(0xf8, 0xf6, u)
			} else {
				u := (t - 0.5) * 2.0
				r = lerp(0x3b, 0x63, u)
				g = lerp(0x82, 0x66, u)
				b = lerp(0xf6, 0xf1, u)
			}

			// Top subtle highlight gloss
			if ny >= 8 && ny <= 64 {
				gloss := (1.0 - (ny-8)/56.0) * 0.22
				r = lerp(r, 255, gloss)
				g = lerp(g, 255, gloss)
				b = lerp(b, 255, gloss)
			}

			// 3. Subtle dark water-drop silhouette in center
			if inWaterDrop(nx, ny) {
				r = lerp(r, 15, 0.18)
				g = lerp(g, 23, 0.18)
				b = lerp(b, 42, 0.18)
			}

			// 4. Crisp White "TD" Monogram
			tdCov := tdGlyphCoverage(nx, ny)
			if tdCov > 0 {
				r = lerp(r, 255, tdCov)
				g = lerp(g, 255, tdCov)
				b = lerp(b, 255, tdCov)
			}

			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(clamp(r, 0, 255)),
				G: uint8(clamp(g, 0, 255)),
				B: uint8(clamp(b, 0, 255)),
				A: uint8(clamp(alpha*255.0, 0, 255)),
			})
		}
	}
	return img
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func roundedRectCoverage(px, py, x, y, w, h, rad float64) float64 {
	cx := x + w*0.5
	cy := y + h*0.5
	hx := w*0.5 - rad
	hy := h*0.5 - rad
	dx := math.Max(math.Abs(px-cx)-hx, 0)
	dy := math.Max(math.Abs(py-cy)-hy, 0)
	dist := math.Hypot(dx, dy) - rad
	// Anti-aliased edge
	return clamp(0.5-dist, 0, 1)
}

func inWaterDrop(nx, ny float64) bool {
	if ny < 20 || ny > 112 {
		return false
	}
	if ny >= 78 {
		return math.Hypot(nx-64, ny-78) <= 34.0
	}
	// Upper cone from (64,20) to tangent of circle
	progress := (ny - 20.0) / (78.0 - 20.0)
	halfW := progress * 34.0
	return math.Abs(nx-64.0) <= halfW
}

func isPointInTD(nx, ny float64) bool {
	// Letter 'T': Top bar [25..62, 42..52], Stem [38..49, 42..88]
	if nx >= 25 && nx <= 62 && ny >= 42 && ny <= 52 {
		return true
	}
	if nx >= 38 && nx <= 49 && ny >= 42 && ny <= 88 {
		return true
	}

	// Letter 'D': Left stem [66..77, 42..88], outer rounded right side center (81, 65) rx=23, ry=23
	if nx >= 66 && ny >= 42 && ny <= 88 {
		// Outer boundary
		inOuter := false
		if nx <= 81 {
			inOuter = true
		} else {
			dx := (nx - 81.0) / 23.0
			dy := (ny - 65.0) / 23.0
			if dx*dx+dy*dy <= 1.0 {
				inOuter = true
			}
		}
		if !inOuter {
			return false
		}
		// Inner cutout: x >= 77, y in [52..78], rounded right center (81, 65) rx=12, ry=13
		if nx >= 77 && ny >= 52 && ny <= 78 {
			if nx <= 81 {
				return false
			}
			dx := (nx - 81.0) / 12.0
			dy := (ny - 65.0) / 13.0
			if dx*dx+dy*dy <= 1.0 {
				return false
			}
		}
		return true
	}
	return false
}

func tdGlyphCoverage(nx, ny float64) float64 {
	// 2x2 supersampling for crisp letter edges
	hits := 0
	offsets := []float64{-0.25, 0.25}
	for _, ox := range offsets {
		for _, oy := range offsets {
			if isPointInTD(nx+ox, ny+oy) {
				hits++
			}
		}
	}
	return float64(hits) / 4.0
}

// writeICO packs PNG images (256, 64, 48, 32, 16) into a standard Windows .ico file
func writeICO(path string, sizes []int) error {
	var pngBuffers [][]byte
	for _, s := range sizes {
		var buf bytes.Buffer
		_ = png.Encode(&buf, renderTDIcon(s))
		pngBuffers = append(pngBuffers, buf.Bytes())
	}

	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	// ICONDIR header (6 bytes)
	_ = binary.Write(out, binary.LittleEndian, uint16(0))
	_ = binary.Write(out, binary.LittleEndian, uint16(1)) // Type 1 = ICO
	_ = binary.Write(out, binary.LittleEndian, uint16(len(sizes)))

	offset := uint32(6 + 16*len(sizes))
	for i, s := range sizes {
		w := uint8(s)
		if s >= 256 {
			w = 0
		}
		dataLen := uint32(len(pngBuffers[i]))
		_ = binary.Write(out, binary.LittleEndian, w)          // Width
		_ = binary.Write(out, binary.LittleEndian, w)          // Height
		_ = binary.Write(out, binary.LittleEndian, uint8(0))   // ColorCount
		_ = binary.Write(out, binary.LittleEndian, uint8(0))   // Reserved
		_ = binary.Write(out, binary.LittleEndian, uint16(1))  // Planes
		_ = binary.Write(out, binary.LittleEndian, uint16(32)) // BitCount
		_ = binary.Write(out, binary.LittleEndian, dataLen)    // BytesInRes
		_ = binary.Write(out, binary.LittleEndian, offset)     // ImageOffset
		offset += dataLen
	}

	for _, b := range pngBuffers {
		_, _ = out.Write(b)
	}
	return nil
}

type iconResEntry struct {
	id   uint16
	data []byte
}

// writeWinSyso generates a COFF .syso object file (amd64 or arm64) embedding RT_GROUP_ICON & RT_ICON
// so `GOOS=windows go build` automatically produces an .exe with the TD icon without needing windres!
func writeWinSyso(path string, arch string, icoPath string) error {
	icoData, err := os.ReadFile(icoPath)
	if err != nil {
		return err
	}
	count := int(binary.LittleEndian.Uint16(icoData[4:6]))

	// Build RT_GROUP_ICON payload (GRPICONDIR)
	var grpBuf bytes.Buffer
	_ = grpBuf.WriteByte(0)
	_ = grpBuf.WriteByte(0)
	_ = binary.Write(&grpBuf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&grpBuf, binary.LittleEndian, uint16(count))

	var icons []iconResEntry

	for i := 0; i < count; i++ {
		entryOff := 6 + i*16
		rawEntry := icoData[entryOff : entryOff+16]
		bytesInRes := binary.LittleEndian.Uint32(rawEntry[8:12])
		imgOffset := binary.LittleEndian.Uint32(rawEntry[12:16])

		// GRPICONDIRENTRY is first 12 bytes + uint16 resource ID
		_, _ = grpBuf.Write(rawEntry[:12])
		resID := uint16(i + 1)
		_ = binary.Write(&grpBuf, binary.LittleEndian, resID)

		icons = append(icons, iconResEntry{
			id:   resID,
			data: icoData[imgOffset : imgOffset+bytesInRes],
		})
	}

	// Construct .rsrc section (2 types: RT_ICON = 3, RT_GROUP_ICON = 14)
	rsrcPayload, rvaFieldOffsets := buildRsrcSection(icons, grpBuf.Bytes())

	var machine uint16 = 0x8664 // IMAGE_FILE_MACHINE_AMD64
	var relocType uint16 = 0x0003 // IMAGE_REL_AMD64_ADDR32NB
	if arch == "arm64" {
		machine = 0xAA64 // IMAGE_FILE_MACHINE_ARM64
		relocType = 0x0002 // IMAGE_REL_ARM64_ADDR32NB
	}

	rawPtr := uint32(20 + 40)
	relocPtr := rawPtr + uint32(len(rsrcPayload))
	numRelocs := uint16(len(rvaFieldOffsets))
	symPtr := relocPtr + uint32(numRelocs)*10

	var coff bytes.Buffer
	// COFF File Header (20 bytes)
	_ = binary.Write(&coff, binary.LittleEndian, machine)
	_ = binary.Write(&coff, binary.LittleEndian, uint16(1)) // NumberOfSections = 1 (.rsrc)
	_ = binary.Write(&coff, binary.LittleEndian, uint32(0)) // TimeDateStamp
	_ = binary.Write(&coff, binary.LittleEndian, symPtr)    // PointerToSymbolTable
	_ = binary.Write(&coff, binary.LittleEndian, uint32(1)) // NumberOfSymbols = 1 (.rsrc section symbol)
	_ = binary.Write(&coff, binary.LittleEndian, uint16(0)) // SizeOfOptionalHeader
	_ = binary.Write(&coff, binary.LittleEndian, uint16(0)) // Characteristics

	// Section Header (40 bytes)
	var secName [8]byte
	copy(secName[:], ".rsrc")
	_, _ = coff.Write(secName[:])
	_ = binary.Write(&coff, binary.LittleEndian, uint32(0))                // VirtualSize
	_ = binary.Write(&coff, binary.LittleEndian, uint32(0))                // VirtualAddress
	_ = binary.Write(&coff, binary.LittleEndian, uint32(len(rsrcPayload))) // SizeOfRawData
	_ = binary.Write(&coff, binary.LittleEndian, rawPtr)                   // PointerToRawData
	_ = binary.Write(&coff, binary.LittleEndian, relocPtr)                 // PointerToRelocations
	_ = binary.Write(&coff, binary.LittleEndian, uint32(0))                // PointerToLinenumbers
	_ = binary.Write(&coff, binary.LittleEndian, numRelocs)                // NumberOfRelocations
	_ = binary.Write(&coff, binary.LittleEndian, uint16(0))                // NumberOfLinenumbers
	// Characteristics: IMAGE_SCN_CNT_INITIALIZED_DATA (0x40) | IMAGE_SCN_MEM_READ (0x40000000)
	_ = binary.Write(&coff, binary.LittleEndian, uint32(0x40000040))

	// Raw .rsrc data
	_, _ = coff.Write(rsrcPayload)

	// COFF Relocations (10 bytes each)
	for _, rvaOff := range rvaFieldOffsets {
		_ = binary.Write(&coff, binary.LittleEndian, uint32(rvaOff))
		_ = binary.Write(&coff, binary.LittleEndian, uint32(0)) // SymbolTableIndex = 0
		_ = binary.Write(&coff, binary.LittleEndian, relocType)
	}

	// Symbol Table (1 symbol, 18 bytes) + String Table Size (4 bytes)
	_, _ = coff.Write(secName[:])
	_ = binary.Write(&coff, binary.LittleEndian, uint32(0))  // Value = 0
	_ = binary.Write(&coff, binary.LittleEndian, int16(1))   // SectionNumber = 1
	_ = binary.Write(&coff, binary.LittleEndian, uint16(0))  // Type = 0
	_ = binary.Write(&coff, binary.LittleEndian, uint8(3))   // StorageClass = IMAGE_SYM_CLASS_STATIC (3)
	_ = binary.Write(&coff, binary.LittleEndian, uint8(0))   // NumberOfAuxSymbols = 0
	_ = binary.Write(&coff, binary.LittleEndian, uint32(4))  // Empty String Table (4 bytes)

	return os.WriteFile(path, coff.Bytes(), 0644)
}

func align8(n int) int { return (n + 7) &^ 7 }

func buildRsrcSection(icons []iconResEntry, grpData []byte) ([]byte, []int) {
	numIcons := len(icons)
	// Level 1 (Root): 16B dir + 2*8B entries (RT_ICON=3, RT_GROUP_ICON=14) = 32B
	// Level 2 (IDs):
	//   RT_ICON dir: 16B + numIcons*8B
	//   RT_GROUP_ICON dir: 16B + 1*8B (ID=1) = 24B
	// Level 3 (Langs):
	//   (numIcons + 1) dirs, each 16B + 1*8B = 24B
	// Data Entries:
	//   (numIcons + 1) * 16B
	l1Size := 16 + 2*8
	l2IconOff := l1Size
	l2IconSize := 16 + numIcons*8
	l2GrpOff := l2IconOff + l2IconSize
	l2GrpSize := 16 + 1*8
	l3Start := l2GrpOff + l2GrpSize
	l3Total := (numIcons + 1) * 24
	dataDescStart := l3Start + l3Total
	dataDescTotal := (numIcons + 1) * 16
	rawStart := align8(dataDescStart + dataDescTotal)

	var buf bytes.Buffer
	writeDir := func(numID uint16) {
		_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(0))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(0))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(0))
		_ = binary.Write(&buf, binary.LittleEndian, numID)
	}
	writeEntry := func(id uint32, offset uint32, isSubdir bool) {
		if isSubdir {
			offset |= 0x80000000
		}
		_ = binary.Write(&buf, binary.LittleEndian, id)
		_ = binary.Write(&buf, binary.LittleEndian, offset)
	}

	// L1
	writeDir(2)
	writeEntry(3, uint32(l2IconOff), true)  // RT_ICON
	writeEntry(14, uint32(l2GrpOff), true) // RT_GROUP_ICON

	// L2 RT_ICON
	writeDir(uint16(numIcons))
	for i := 0; i < numIcons; i++ {
		writeEntry(uint32(icons[i].id), uint32(l3Start+i*24), true)
	}

	// L2 RT_GROUP_ICON (ID = 1, matching LoadIcon(hInst, MAKEINTRESOURCE(1)))
	writeDir(1)
	writeEntry(1, uint32(l3Start+numIcons*24), true)

	// L3 Language directories (0x0409)
	for i := 0; i < numIcons+1; i++ {
		writeDir(1)
		writeEntry(0x0409, uint32(dataDescStart+i*16), false)
	}

	// Precompute raw offsets
	currRaw := rawStart
	var rawOffsets []int
	var rawSizes []int
	for i := 0; i < numIcons; i++ {
		rawOffsets = append(rawOffsets, currRaw)
		rawSizes = append(rawSizes, len(icons[i].data))
		currRaw = align8(currRaw + len(icons[i].data))
	}
	rawOffsets = append(rawOffsets, currRaw)
	rawSizes = append(rawSizes, len(grpData))
	currRaw = align8(currRaw + len(grpData))

	// Data Entries
	var rvaOffsets []int
	for i := 0; i < numIcons+1; i++ {
		rvaOffsets = append(rvaOffsets, buf.Len())
		_ = binary.Write(&buf, binary.LittleEndian, uint32(rawOffsets[i]))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(rawSizes[i]))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	}

	for buf.Len() < rawStart {
		_ = buf.WriteByte(0)
	}
	for i := 0; i < numIcons; i++ {
		_, _ = buf.Write(icons[i].data)
		for buf.Len() < align8(buf.Len()) {
			_ = buf.WriteByte(0)
		}
	}
	_, _ = buf.Write(grpData)
	for buf.Len() < align8(buf.Len()) {
		_ = buf.WriteByte(0)
	}
	return buf.Bytes(), rvaOffsets
}

func main() {
	_ = os.MkdirAll("internal/assets", 0755)

	// 1. Generate 64x64 tray icon PNG for macOS status bar
	fTray, err := os.Create("internal/assets/tray_icon.png")
	if err != nil {
		panic(err)
	}
	_ = png.Encode(fTray, renderTDIcon(64))
	_ = fTray.Close()

	// 2. Generate Windows .ico (containing 256, 64, 48, 32, 16) & .syso resource files
	icoPath := "internal/assets/icon.ico"
	if err := writeICO(icoPath, []int{256, 64, 48, 32, 16}); err != nil {
		panic(err)
	}
	if err := writeWinSyso("rsrc_windows_amd64.syso", "amd64", icoPath); err != nil {
		panic(err)
	}
	if err := writeWinSyso("rsrc_windows_arm64.syso", "arm64", icoPath); err != nil {
		panic(err)
	}

	// 3. On macOS, generate AppIcon.icns via native iconutil
	if runtime.GOOS == "darwin" {
		iconsetDir := "internal/assets/AppIcon.iconset"
		_ = os.RemoveAll(iconsetDir)
		_ = os.MkdirAll(iconsetDir, 0755)

		spec := map[string]int{
			"icon_16x16.png":      16,
			"icon_16x16@2x.png":   32,
			"icon_32x32.png":      32,
			"icon_32x32@2x.png":   64,
			"icon_128x128.png":    128,
			"icon_128x128@2x.png": 256,
			"icon_256x256.png":    256,
			"icon_256x256@2x.png": 512,
			"icon_512x512.png":    512,
			"icon_512x512@2x.png": 1024,
		}
		for name, sz := range spec {
			f, err := os.Create(filepath.Join(iconsetDir, name))
			if err != nil {
				panic(err)
			}
			_ = png.Encode(f, renderTDIcon(sz))
			_ = f.Close()
		}
		_ = exec.Command("iconutil", "-c", "icns", iconsetDir, "-o", "internal/assets/AppIcon.icns").Run()
		_ = os.RemoveAll(iconsetDir)
	}
}
