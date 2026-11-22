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
	return pngCabeceraEntrelazado(ancho, alto, profundidad, tipo, 0)
}

func pngCabeceraEntrelazado(ancho, alto uint32, profundidad, tipo, entrelazado byte) []byte {
	datos := append([]byte("IHDR"), binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, ancho), alto)...)
	datos = append(datos, profundidad, tipo, 0, 0, entrelazado)
	b := []byte("\x89PNG\r\n\x1a\n")
	b = binary.BigEndian.AppendUint32(b, uint32(len(datos)-4))
	b = append(b, datos...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(datos))
}

// jpegProgresivo: SOI, un SOF2 (progresivo) con las dimensiones y el SOS, sin datos de imagen:
// DecodeConfig no lee más.
func jpegProgresivo(ancho, alto uint16) []byte { return jpegCabecera(0xc2, 3, ancho, alto) }

// jpegCabecera: SOI, un APP0 JFIF, el SOF dado (0xc0 base, 0xc2 progresivo) con n componentes y el SOS.
func jpegCabecera(sofMarca byte, n int, ancho, alto uint16) []byte {
	app0 := []byte{0xff, 0xe0, 0, 16, 'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0}
	sof := []byte{0xff, sofMarca, 0, byte(8 + 3*n), 8} //nolint:gosec // n ≤ 4
	sof = binary.BigEndian.AppendUint16(sof, alto)
	sof = binary.BigEndian.AppendUint16(sof, ancho)
	sof = append(sof, byte(n)) //nolint:gosec // n ≤ 4
	sos := []byte{0xff, 0xda, 0, byte(6 + 2*n), byte(n)} //nolint:gosec // n ≤ 4
	for c := 1; c <= n; c++ {
		sof = append(sof, byte(c), 0x11, 0) //nolint:gosec // n ≤ 4
		sos = append(sos, byte(c), 0)        //nolint:gosec // n ≤ 4
	}
	sos = append(sos, 0, 0x3f, 0)
	return append(append(append([]byte{0xff, 0xd8}, app0...), sof...), sos...)
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

// H1: el tope de memoria cuenta lo que el decodificador guarda aparte.
func TestMemoriaDecodificadaJPEGyPNG(t *testing.T) {
	// 40 MP con datos de escaneo: base (YCbCr, 160 MB) pasa; progresivo (más 480 MB de coeficientes) no.
	datos := append(make([]byte, 4096), 0xff, 0xd9)
	for i := range 4096 {
		datos[i] = byte(i*7 + 1)
		if datos[i] == 0xff {
			datos[i] = 0x7f
		}
	}
	base := append(jpegCabecera(0xc0, 3, 6400, 6250), datos...)
	if _, err := comprobarArchivo("b.jpg", base); err != nil {
		t.Fatalf("base 40 MP: %v", err)
	}
	prog := append(jpegCabecera(0xc2, 3, 6400, 6250), datos...)
	if _, err := comprobarArchivo("p.jpg", prog); codigoDe(t, err) != "imagen_demasiado_grande" {
		t.Fatalf("progresivo 40 MP: %v", err)
	}
	// CMYK: la imagen intermedia (36 MP: 144 + 144 MB).
	if _, err := comprobarArchivo("c.jpg", append(jpegCabecera(0xc0, 4, 6000, 6000), datos...)); codigoDe(t, err) != "imagen_demasiado_grande" {
		t.Fatalf("cmyk: %v", err)
	}
	if _, err := comprobarArchivo("c.jpg", append(jpegCabecera(0xc0, 4, 3000, 3000), datos...)); err != nil {
		t.Fatalf("cmyk chico: %v", err)
	}
	// PNG RGBA de 8 bits, 27 MP (108 MB): pasa; entrelazado (el doble), no.
	if _, err := comprobarArchivo("a.png", pngCabeceraEntrelazado(5200, 5200, 8, 6, 0)); err != nil {
		t.Fatalf("png: %v", err)
	}
	if _, err := comprobarArchivo("a.png", pngCabeceraEntrelazado(5200, 5200, 8, 6, 1)); codigoDe(t, err) != "imagen_demasiado_grande" {
		t.Fatalf("png entrelazado: %v", err)
	}
}

// H2: un pánico del decodificador no deja el semáforo tomado y responde mime_no_permitido.
func TestPanicoRecodificandoLiberaSemaforo(t *testing.T) {
	antes := recodificador
	t.Cleanup(func() { recodificador = antes })
	recodificador = func([]byte, string) ([]byte, error) { panic("decodificador roto") }
	a := archivoPortal{Mime: "image/png", Tipo: "foto", Datos: []byte("x")}
	for range MaxRecodificaciones + 2 {
		if _, err := recodificarArchivos([]archivoPortal{a}); codigoDe(t, err) != "mime_no_permitido" {
			t.Fatalf("pánico: %v", err)
		}
	}
	if n := len(recodificaciones); n != 0 {
		t.Fatalf("semáforo tomado: %d", n)
	}
}
