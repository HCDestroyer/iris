package mensajeria_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

/*
 * Medidas de la imagen de un mensaje (contratos/conversaciones.md §7.2.1): el servidor lee solo la
 * cabecera al guardar el adjunto (descarga desde WhatsApp y envío del panel) y las da en
 * `adjunto.ancho` / `adjunto.alto`, con la orientación EXIF aplicada. Sin medidas, null.
 */

func imagenDe(ancho, alto int) *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, ancho, alto))
	for y := range alto {
		for x := range ancho {
			im.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	return im
}

func pngDe(t testing.TB, ancho, alto int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, imagenDe(ancho, alto)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// jpegConOrientacion: un JPEG guardado de ancho×alto con un APP1 EXIF (TIFF little endian) cuya
// Orientation es o (6 = girada 90°: el teléfono la tomó de pie).
func jpegConOrientacion(t testing.TB, ancho, alto int, o uint16) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, imagenDe(ancho, alto), nil); err != nil {
		t.Fatal(err)
	}
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0}
	entrada := make([]byte, 12)
	binary.LittleEndian.PutUint16(entrada[0:], 0x0112)
	binary.LittleEndian.PutUint16(entrada[2:], 3)
	binary.LittleEndian.PutUint32(entrada[4:], 1)
	binary.LittleEndian.PutUint16(entrada[8:], o)
	tiff = append(append(tiff, entrada...), 0, 0, 0, 0)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(seg)+2))
	crudo := b.Bytes()
	return append(append(append([]byte{}, crudo[:2]...), append(app1, seg...)...), crudo[2:]...)
}

func init() {
	mediosFalsos["medio-girada"] = jpegConOrientacion(&testing.T{}, 40, 30, 6)
	mediosFalsos["medio-png"] = pngDe(&testing.T{}, 64, 48)
}

func medidasDe(t *testing.T, m map[string]any) (any, any) {
	t.Helper()
	adj, ok := m["adjunto"].(map[string]any)
	if !ok {
		t.Fatalf("sin adjunto: %v", m)
	}
	return adj["ancho"], adj["alto"]
}

func TestConvMedidasDeLaImagen(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	c0 := func() map[string]any { return b.conv(num) }
	bajar := func(wamid, cuerpo string) map[string]any {
		t.Helper()
		b.webhook(b.mensajeCliente(num, wamid, cuerpo))
		ms := b.mensajes(b.supervisor, cid(c0()))
		ultimo := ms[len(ms)-1]
		if a, l := medidasDe(t, ultimo); a != nil || l != nil {
			t.Fatalf("pendiente con medidas: %v", ultimo)
		}
		if err := b.m.DescargarMedio(b.ctx, uuid.MustParse(ultimo["id"].(string)), false); err != nil {
			t.Fatal(err)
		}
		ms = b.mensajes(b.supervisor, cid(c0()))
		return ms[len(ms)-1]
	}

	// Foto de pie desde WhatsApp: guardada 40×30 con Orientation 6, se ve 30×40.
	girada := bajar("wamid.md1", `"type":"image","image":{"id":"medio-girada","mime_type":"image/jpeg"}`)
	if a, l := medidasDe(t, girada); a != float64(30) || l != float64(40) {
		t.Fatalf("JPEG girado: ancho %v alto %v, quería 30×40", a, l)
	}
	// PNG: sus medidas tal cual.
	if a, l := medidasDe(t, bajar("wamid.md2", `"type":"image","image":{"id":"medio-png","mime_type":"image/png"}`)); a != float64(64) || l != float64(48) {
		t.Fatalf("PNG: ancho %v alto %v", a, l)
	}
	// Cabecera ilegible (JPEG cortado): sin medidas, el mensaje se guarda igual.
	corta := bajar("wamid.md3", `"type":"image","image":{"id":"medio-corta","mime_type":"image/jpeg"}`)
	if corta["adjunto"].(map[string]any)["estado"] != "completo" {
		t.Fatalf("la foto ilegible no se guardó: %v", corta)
	}
	if a, l := medidasDe(t, corta); a != nil || l != nil {
		t.Fatalf("JPEG cortado con medidas: %v %v", a, l)
	}

	// El agente la reenvía desde el panel: el envío lee la cabecera del adjunto ya subido.
	adj := girada["adjunto"].(map[string]any)["id"]
	r := b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c0())+"/mensajes", map[string]any{"id": nuevoID(), "adjuntoId": adj})
	b.exigir(r, http.StatusCreated, "tipo", "imagen")
	if a, l := medidasDe(t, r.cuerpo); a != float64(30) || l != float64(40) {
		t.Fatalf("saliente: ancho %v alto %v", a, l)
	}
	var ancho, alto *int32
	if err := b.bd.QueryRow(b.ctx, `SELECT adjunto_ancho, adjunto_alto FROM mensajeria.conversacion_mensajes WHERE id = $1`, r.cuerpo["id"]).Scan(&ancho, &alto); err != nil {
		t.Fatal(err)
	}
	if ancho == nil || alto == nil || *ancho != 30 || *alto != 40 {
		t.Fatalf("en la base: %v %v", ancho, alto)
	}
	// Un mensaje de texto no trae adjunto ni medidas.
	tx := b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c0())+"/mensajes", map[string]any{"id": nuevoID(), "texto": "Gracias"})
	b.exigir(tx, http.StatusCreated)
	if _, hay := tx.cuerpo["adjunto"]; hay {
		t.Fatalf("texto con adjunto: %v", tx.cuerpo)
	}
}
