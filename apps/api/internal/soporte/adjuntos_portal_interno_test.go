package soporte

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"strings"
	"testing"
)

// Adjuntos del portal de Argos (contrato 0.9.0 §11.6): tipo por el contenido, metadatos fuera y
// nombre saneado.

func bloqueRIFF(id string, datos []byte) []byte {
	b := append([]byte(id), binary.LittleEndian.AppendUint32(nil, uint32(len(datos)))...)
	b = append(b, datos...)
	if len(datos)%2 == 1 {
		b = append(b, 0)
	}
	return b
}

func TestWebPSinMetadatos(t *testing.T) {
	vp8x := make([]byte, 10)
	vp8x[0] = 0x08 | 0x04     // EXIF y XMP
	vp8x[4], vp8x[7] = 99, 49 // 100 × 50
	cuerpo := append(bloqueRIFF("VP8X", vp8x), bloqueRIFF("EXIF", []byte("GPS secreto"))...)
	cuerpo = append(cuerpo, bloqueRIFF("VP8L", []byte{0x2f, 0, 0, 0, 0})...)
	cuerpo = append(cuerpo, bloqueRIFF("XMP ", []byte("<x:autor>Ana</x:autor>"))...)
	riff := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(cuerpo)+4))...)
	riff = append(append(riff, []byte("WEBP")...), cuerpo...)

	a, err := sanearArchivo("foto.webp", riff)
	if err != nil {
		t.Fatal(err)
	}
	if a.Mime != "image/webp" || bytes.Contains(a.Datos, []byte("secreto")) || bytes.Contains(a.Datos, []byte("Ana")) {
		t.Fatalf("webp: %s %q", a.Mime, a.Datos)
	}
	if a.Datos[20]&(0x08|0x04) != 0 {
		t.Fatal("las marcas de EXIF y XMP siguen en VP8X")
	}
	if int(binary.LittleEndian.Uint32(a.Datos[4:8])) != len(a.Datos)-8 {
		t.Fatal("tamaño del RIFF")
	}
}

func TestArchivoPortal(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, 3, 3)), nil); err != nil {
		t.Fatal(err)
	}
	a, err := sanearArchivo("C:\\fotos\\mi \"casa\"\r\n.png", b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if a.Mime != "image/jpeg" || a.Nombre != "mi casa.jpg" {
		t.Fatalf("jpeg: %s %q", a.Mime, a.Nombre)
	}
	if _, err := sanearArchivo("x.png", []byte("<html><script>alert(1)</script></html>")); err == nil {
		t.Fatal("un HTML no es una foto")
	}
	if n := nombreSaneado(strings.Repeat("á", 200)+".pdf", "application/pdf"); len([]rune(n)) != 120 || !strings.HasSuffix(n, ".pdf") {
		t.Fatalf("largo: %d %q", len([]rune(n)), n)
	}
	if n := nombreSaneado("../..", "application/pdf"); n != "adjunto.pdf" {
		t.Fatalf("vacío: %q", n)
	}
}
