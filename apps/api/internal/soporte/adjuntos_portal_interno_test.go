package soporte

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Adjuntos del portal de Argos (contrato 0.9.0 §11.6, 0.14.2 y revisión de seguridad M1): tipo por el
// contenido, dimensiones y memoria desde el encabezado antes de decodificar, recodificación limitada
// y nombre saneado.

func codigoDe(t *testing.T, err error) string {
	t.Helper()
	p, _ := nucleo.AProblema(err)
	if p == nil {
		t.Fatalf("sin problema: %v", err)
	}
	return p.Codigo()
}

// pngCabecera: un PNG con solo la firma y el IHDR (DecodeConfig no lee más). profundidad 16 y tipo 6
// = RGBA de 16 bits con alfa.
func pngCabecera(ancho, alto uint32, profundidad, tipo byte) []byte {
	datos := append([]byte("IHDR"), binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, ancho), alto)...)
	datos = append(datos, profundidad, tipo, 0, 0, 0)
	b := []byte("\x89PNG\r\n\x1a\n")
	b = binary.BigEndian.AppendUint32(b, uint32(len(datos)-4))
	b = append(b, datos...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(datos))
}

// jpegProgresivo: SOI, un SOF2 (progresivo) con las dimensiones y el SOS, sin datos de imagen:
// DecodeConfig no lee más.
func jpegProgresivo(ancho, alto uint16) []byte {
	sof := []byte{0xff, 0xc2, 0, 17, 8}
	sof = binary.BigEndian.AppendUint16(sof, alto)
	sof = binary.BigEndian.AppendUint16(sof, ancho)
	sof = append(sof, 3, 1, 0x22, 0, 2, 0x11, 1, 3, 0x11, 1)
	sos := []byte{0xff, 0xda, 0, 12, 3, 1, 0, 2, 0x11, 3, 0x11, 0, 0x3f, 0}
	return append(append([]byte{0xff, 0xd8}, sof...), sos...)
}

func TestAdjuntoGrandeSeRechazaSinDecodificar(t *testing.T) {
	// PNG de 16 bits con alfa: 27 MP (menos de 40) pero 216 MB decodificada.
	if _, err := comprobarArchivo("a.png", pngCabecera(5200, 5200, 16, 6)); codigoDe(t, err) != "imagen_demasiado_grande" {
		t.Fatalf("png 16 bits: %v", err)
	}
	// El mismo tamaño en 8 bits (108 MB) pasa la comprobación del encabezado.
	if _, err := comprobarArchivo("a.png", pngCabecera(5200, 5200, 8, 6)); err != nil {
		t.Fatalf("png 8 bits: %v", err)
	}
	// JPEG progresivo de 49 MP: más de 40 MP.
	if _, err := comprobarArchivo("a.jpg", jpegProgresivo(7000, 7000)); codigoDe(t, err) != "imagen_demasiado_grande" {
		t.Fatalf("jpeg progresivo: %v", err)
	}
	// Uno progresivo de 4000 × 3000 pasa el encabezado; no se puede recodificar (no trae datos): 422.
	a, err := comprobarArchivo("a.jpg", jpegProgresivo(4000, 3000))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recodificarArchivos([]archivoPortal{a}); codigoDe(t, err) != "mime_no_permitido" {
		t.Fatalf("jpeg sin datos: %v", err)
	}
}

func TestPNG16BitsConAlfaSeRecodifica(t *testing.T) {
	img := image.NewNRGBA64(image.Rect(0, 0, 8, 8))
	img.Set(2, 3, color.NRGBA64{R: 0xffff, A: 0x8000})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	a, err := comprobarArchivo("foto.png", b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	r, err := recodificarArchivos([]archivoPortal{a})
	if err != nil || len(r) != 1 || r[0].Mime != "image/png" {
		t.Fatalf("recodificar: %v %v", r, err)
	}
	if _, err := png.Decode(bytes.NewReader(r[0].Datos)); err != nil {
		t.Fatal(err)
	}
}

func TestRecodificacionesLimitadas(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, 3, 3)), nil); err != nil {
		t.Fatal(err)
	}
	a, err := comprobarArchivo("x.jpg", b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for range MaxRecodificaciones {
		recodificaciones <- struct{}{}
	}
	_, err = recodificarArchivos([]archivoPortal{a})
	for range MaxRecodificaciones {
		<-recodificaciones
	}
	p, _ := nucleo.AProblema(err)
	if p == nil || p.Status != 503 || p.GetHeaders().Get("Retry-After") == "" {
		t.Fatalf("lleno: %v", err)
	}
	if _, err := recodificarArchivos([]archivoPortal{a}); err != nil {
		t.Fatal(err)
	}
}

func TestArchivoPortal(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, 3, 3)), nil); err != nil {
		t.Fatal(err)
	}
	a, err := comprobarArchivo("C:\\fotos\\mi \"casa\"\r\n.png", b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if a.Mime != "image/jpeg" || a.Nombre != "mi casa.jpg" {
		t.Fatalf("jpeg: %s %q", a.Mime, a.Nombre)
	}
	// HTML, WebP y HEIC: fuera (0.14.2).
	webp := append([]byte("RIFF\x10\x00\x00\x00WEBPVP8L"), make([]byte, 8)...)
	heic := append([]byte("\x00\x00\x00\x18ftypheic"), make([]byte, 16)...)
	for nombre, datos := range map[string][]byte{"x.html": []byte("<html><script>alert(1)</script></html>"), "x.webp": webp, "x.heic": heic} {
		if _, err := comprobarArchivo(nombre, datos); codigoDe(t, err) != "mime_no_permitido" {
			t.Fatalf("%s: %v", nombre, err)
		}
	}
	if n := nombreSaneado(strings.Repeat("á", 200)+".pdf", "application/pdf"); len([]rune(n)) != 120 || !strings.HasSuffix(n, ".pdf") {
		t.Fatalf("largo: %d %q", len([]rune(n)), n)
	}
	if n := nombreSaneado("../..", "application/pdf"); n != "adjunto.pdf" {
		t.Fatalf("vacío: %q", n)
	}
}
