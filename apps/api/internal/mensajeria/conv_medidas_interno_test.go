package mensajeria

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

func pngCabecera(ancho, alto uint32) []byte {
	// Firma + IHDR sin datos: DecodeConfig solo mira esto.
	b := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13, 'I', 'H', 'D', 'R'}
	d := make([]byte, 13)
	binary.BigEndian.PutUint32(d[0:], ancho)
	binary.BigEndian.PutUint32(d[4:], alto)
	d[8], d[9] = 8, 2
	crc := make([]byte, 4)
	binary.BigEndian.PutUint32(crc, crc32.ChecksumIEEE(append([]byte("IHDR"), d...)))
	return append(append(b, d...), crc...)
}

func TestMedidasImagenSoloCabecera(t *testing.T) {
	var p bytes.Buffer
	if err := png.Encode(&p, image.NewGray(image.Rect(0, 0, 7, 5))); err != nil {
		t.Fatal(err)
	}
	var g bytes.Buffer
	if err := gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 9, 4), color.Palette{color.Black}), nil); err != nil {
		t.Fatal(err)
	}
	casos := []struct {
		nombre, mime string
		datos        []byte
		ancho, alto  int32 // 0 = null
	}{
		{"png", "image/png", p.Bytes(), 7, 5},
		{"gif", "image/gif", g.Bytes(), 9, 4},
		// La cabecera dice 9000×6000 aunque no haya píxeles: nunca se decodifica la imagen.
		{"png grande sin datos", "image/png", pngCabecera(9000, 6000), 9000, 6000},
		{"lado fuera de rango", "image/png", pngCabecera(30000, 10), 0, 0},
		{"webp: sin decodificador", "image/webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), 0, 0},
		{"pdf", "application/pdf", []byte("%PDF-1.4"), 0, 0},
		{"jpeg cortado", "image/jpeg", []byte("\xff\xd8\xff\xe0 foto"), 0, 0},
		{"mime que no coincide", "image/jpeg", p.Bytes(), 0, 0},
	}
	for _, c := range casos {
		md := medidasImagen(c.mime, c.datos)
		if c.ancho == 0 {
			if md.ancho != nil || md.alto != nil {
				t.Errorf("%s: quería null, dio %d×%d", c.nombre, *md.ancho, *md.alto)
			}
			continue
		}
		if md.ancho == nil || *md.ancho != c.ancho || *md.alto != c.alto {
			t.Errorf("%s: quería %d×%d, dio %v×%v", c.nombre, c.ancho, c.alto, md.ancho, md.alto)
		}
	}
}

func TestOrientacionExifNoLeeFuera(t *testing.T) {
	// Segmentos rotos, cortos o con offsets absurdos: nunca pánico, siempre 0.
	rotos := [][]byte{
		nil,
		{0xFF, 0xD8},
		{0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF},
		append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0, 16}, []byte("Exif\x00\x00MM\x00\x2a\xff\xff\xff\xff")...),
		append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0, 18}, []byte("Exif\x00\x00II\x2a\x00\x08\x00\x00\x00\xff\xff")...),
	}
	for i, b := range rotos {
		if o := orientacionExif(b); o != 0 {
			t.Errorf("caso %d: orientación %d", i, o)
		}
	}
}
