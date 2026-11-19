package mensajeria

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	_ "image/gif"  // registra GIF para image.DecodeConfig
	_ "image/jpeg" // registra JPEG
	_ "image/png"  // registra PNG
	"log/slog"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
)

// cabezaMedidas: cuántos bytes del archivo se leen para las medidas. La cabecera de JPEG va
// detrás de los segmentos APP (el EXIF con su miniatura cabe en 64 KiB por segmento); 256 KiB
// alcanzan para casi todas las fotos y nunca se lee el archivo entero.
const cabezaMedidas = 256 << 10

// ladoMaximo: medidas más grandes se descartan (cabecera rara o maliciosa): el panel reserva 4:3.
const ladoMaximo = 20000

// medidas es el ancho y alto de la imagen como se ve (con la orientación EXIF ya aplicada).
type medidas struct {
	ancho, alto *int32
}

// medidasImagen lee solo la cabecera de una imagen JPEG, PNG o GIF y devuelve su ancho y alto en
// píxeles. Nunca decodifica la imagen. WebP y cualquier otro tipo (o una cabecera ilegible, o
// medidas fuera de rango) dan medidas nulas: el panel reserva 4:3. En JPEG con orientación EXIF
// de 5 a 8 (girada 90°) intercambia ancho y alto, como la muestra el navegador
// (`image-orientation: from-image`, el valor por defecto).
func medidasImagen(mime string, cabeza []byte) medidas {
	switch mime {
	case "image/jpeg", "image/png", "image/gif":
	default:
		return medidas{}
	}
	if len(cabeza) > cabezaMedidas {
		cabeza = cabeza[:cabezaMedidas]
	}
	cfg, formato, err := image.DecodeConfig(bytes.NewReader(cabeza))
	if err != nil || "image/"+formato != mime || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > ladoMaximo || cfg.Height > ladoMaximo {
		return medidas{}
	}
	w, h := int32(cfg.Width), int32(cfg.Height) //nolint:gosec // acotados por ladoMaximo
	if formato == "jpeg" && orientacionGirada(cabeza) {
		w, h = h, w
	}
	return medidas{ancho: &w, alto: &h}
}

// orientacionGirada: el JPEG trae EXIF con Orientation (0x0112) entre 5 y 8, es decir, se
// muestra girado 90° o 270° respecto de cómo está guardado.
func orientacionGirada(b []byte) bool {
	o := orientacionExif(b)
	return o >= 5 && o <= 8
}

// orientacionExif busca el segmento APP1 «Exif» del JPEG y lee la etiqueta Orientation de la
// IFD0. 0 si no hay o no se puede leer. Nunca lee fuera de b.
func orientacionExif(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 0
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 0
		}
		marca := b[i+1]
		if marca == 0xD8 || (marca >= 0xD0 && marca <= 0xD7) || marca == 0x01 {
			i += 2
			continue
		}
		if marca == 0xDA || marca == 0xD9 { // inicio de los datos o fin: no hubo EXIF antes
			return 0
		}
		largo := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if largo < 2 || i+2+largo > len(b) {
			return 0
		}
		seg := b[i+4 : i+2+largo]
		if marca == 0xE1 && len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
			return orientacionTiff(seg[6:])
		}
		i += 2 + largo
	}
	return 0
}

func orientacionTiff(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var orden binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		orden = binary.LittleEndian
	case "MM":
		orden = binary.BigEndian
	default:
		return 0
	}
	if orden.Uint16(t[2:4]) != 42 {
		return 0
	}
	ifd := int(orden.Uint32(t[4:8]))
	if ifd < 8 || ifd+2 > len(t) {
		return 0
	}
	n := int(orden.Uint16(t[ifd : ifd+2]))
	for k := 0; k < n; k++ {
		e := ifd + 2 + k*12
		if e+12 > len(t) {
			return 0
		}
		if orden.Uint16(t[e:e+2]) == 0x0112 && orden.Uint16(t[e+2:e+4]) == 3 { // SHORT
			return int(orden.Uint16(t[e+8 : e+10]))
		}
	}
	return 0
}

// medidasDeAdjunto lee la cabecera de una imagen ya subida (el envío del panel). Si no se puede
// leer, el mensaje sale igual sin medidas: nunca bloquea el envío.
func (m *Modulo) medidasDeAdjunto(ctx context.Context, a adjuntos.Adjunto) medidas {
	if m.cfg.Almacen == nil {
		return medidas{}
	}
	switch a.Mime {
	case "image/jpeg", "image/png", "image/gif":
	default:
		return medidas{}
	}
	cabeza, err := adjuntos.Cabeza(ctx, m.cfg.Almacen, a, cabezaMedidas)
	if err != nil {
		slog.WarnContext(ctx, "mensajeria: no se leyeron las medidas de la imagen", "adjunto_id", a.ID, "error", err)
		return medidas{}
	}
	return medidasImagen(a.Mime, cabeza)
}
