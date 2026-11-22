package soporte

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Adjuntos del portal del cliente de Argos (contrato 0.9.0 §11.6). Argos sube los bytes dentro de
// POST …/tickets y POST …/responder (multipart/form-data: la parte «datos» con el JSON y hasta 3
// partes «archivos»); nadie descarga URLs del otro. El tipo se comprueba por el contenido, nunca
// por el nombre, y antes de guardar Iris quita los metadatos:
//
//   - JPEG y PNG se decodifican y se vuelven a codificar (se pierden EXIF, XMP e ICC), con un tope de
//     píxeles;
//   - WebP se reescribe sin los bloques EXIF y XMP (Go no trae codificador de WebP y el repositorio no
//     suma dependencias sin acuerdo), con el mismo tope de píxeles según su cabecera;
//   - PDF se guarda tal cual (no es una foto);
//   - HEIC no se acepta todavía: quitarle los metadatos exige un decodificador que Iris no tiene
//     (422 mime_no_permitido; anotado en el informe de la PR).

// Límites de §11.6.
const (
	MaxAdjuntosPortal       = 3
	MaxTamanoAdjuntoPortal  = 10 << 20
	MaxPixelesAdjuntoPortal = 40_000_000
	// MaxCuerpoPortal: 3 archivos de 10 MB más el JSON y las cabeceras de cada parte.
	MaxCuerpoPortal = MaxAdjuntosPortal*MaxTamanoAdjuntoPortal + 1<<20
)

// archivoPortal es un archivo ya comprobado y saneado, listo para guardar.
type archivoPortal struct {
	Nombre string
	Mime   string
	Tipo   string // foto o documento
	Datos  []byte
}

func tamanoExcedido() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusRequestEntityTooLarge, "tamano_excedido", "Archivo demasiado grande",
		"Cada archivo puede pesar hasta 10 MB")
}

func mimeNoPermitido() *nucleo.Problema {
	return nucleo.ReglaNegocio("mime_no_permitido", "Solo se aceptan fotos JPEG, PNG o WebP y documentos PDF")
}

// cuerpoPortal separa el JSON y los archivos de un cuerpo JSON o multipart/form-data.
// Con una sesión ip (sinArchivos) cualquier archivo es 422 adjuntos_no_permitidos (§11.2).
func cuerpoPortal(contentType string, crudo []byte, destino any, sinArchivos bool) ([]archivoPortal, error) {
	mt, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, nucleo.SolicitudInvalida("Content-Type debe ser application/json o multipart/form-data")
	}
	switch mt {
	case "application/json":
		return nil, decodificarJSONPortal(crudo, destino)
	case "multipart/form-data":
	default:
		return nil, nucleo.NuevoProblema(http.StatusUnsupportedMediaType, "tipo_no_soportado", "Tipo de contenido no soportado",
			"Content-Type debe ser application/json o multipart/form-data")
	}
	if params["boundary"] == "" {
		return nil, nucleo.SolicitudInvalida("multipart/form-data sin boundary")
	}
	r := multipart.NewReader(bytes.NewReader(crudo), params["boundary"])
	var archivos []archivoPortal
	conDatos := false
	for {
		p, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nucleo.SolicitudInvalida("Cuerpo multipart inválido")
		}
		switch p.FormName() {
		case "datos":
			if conDatos {
				return nil, nucleo.SolicitudInvalida("La parte «datos» va una sola vez")
			}
			b, err := io.ReadAll(io.LimitReader(p, 1<<20+1))
			if err != nil || len(b) > 1<<20 {
				return nil, nucleo.SolicitudInvalida("La parte «datos» no se pudo leer o es demasiado grande")
			}
			if err := decodificarJSONPortal(b, destino); err != nil {
				return nil, err
			}
			conDatos = true
		case "archivos":
			if sinArchivos {
				return nil, nucleo.ReglaNegocio("adjuntos_no_permitidos", "Con una sesión abierta por la IP no se envían adjuntos")
			}
			if len(archivos) >= MaxAdjuntosPortal {
				return nil, nucleo.ReglaNegocio("demasiados_adjuntos", "Hasta 3 archivos por mensaje")
			}
			b, err := io.ReadAll(io.LimitReader(p, MaxTamanoAdjuntoPortal+1))
			if err != nil {
				return nil, nucleo.SolicitudInvalida("Un archivo no se pudo leer")
			}
			if len(b) > MaxTamanoAdjuntoPortal {
				return nil, tamanoExcedido()
			}
			a, err := sanearArchivo(p.FileName(), b)
			if err != nil {
				return nil, err
			}
			archivos = append(archivos, a)
		default:
			// Partes desconocidas: se ignoran (sin leerlas más allá de lo que el lector descarta).
		}
	}
	if !conDatos {
		return nil, nucleo.Validacion(nucleo.ErrorCampo{Field: "datos", Message: "Falta la parte «datos» con el JSON", Rule: "required"})
	}
	return archivos, nil
}

func decodificarJSONPortal(crudo []byte, destino any) error {
	dec := json.NewDecoder(bytes.NewReader(crudo))
	if err := dec.Decode(destino); err != nil {
		return nucleo.SolicitudInvalida("El cuerpo no es un JSON válido para esta operación")
	}
	if dec.More() {
		return nucleo.SolicitudInvalida("El cuerpo tiene más de un JSON")
	}
	return nil
}

// sanearArchivo comprueba el tipo por el contenido y quita los metadatos (ver el comentario del
// archivo). El nombre queda saneado para Content-Disposition.
func sanearArchivo(nombre string, datos []byte) (archivoPortal, error) {
	if len(datos) == 0 {
		return archivoPortal{}, mimeNoPermitido()
	}
	tipo := http.DetectContentType(datos)
	a := archivoPortal{Mime: tipo, Tipo: "foto"}
	var err error
	switch tipo {
	case "image/jpeg", "image/png":
		a.Datos, err = recodificar(datos, tipo)
	case "image/webp":
		a.Datos, err = webpSinMetadatos(datos)
	case "application/pdf":
		a.Datos, a.Tipo = datos, "documento"
	default:
		return archivoPortal{}, mimeNoPermitido()
	}
	if err != nil {
		return archivoPortal{}, err
	}
	if len(a.Datos) > MaxTamanoAdjuntoPortal {
		return archivoPortal{}, tamanoExcedido()
	}
	a.Nombre = nombreSaneado(nombre, tipo)
	return a, nil
}

// recodificar decodifica y vuelve a codificar la imagen: el archivo nuevo no lleva metadatos.
func recodificar(datos []byte, tipo string) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(datos))
	if err != nil {
		return nil, mimeNoPermitido()
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixelesAdjuntoPortal {
		return nil, nucleo.ReglaNegocio("imagen_demasiado_grande", "La foto tiene demasiados píxeles (máximo 40 MP)")
	}
	img, _, err := image.Decode(bytes.NewReader(datos))
	if err != nil {
		return nil, mimeNoPermitido()
	}
	var out bytes.Buffer
	if tipo == "image/png" {
		err = png.Encode(&out, img)
	} else {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 88})
	}
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// webpSinMetadatos reescribe el RIFF sin los bloques EXIF y XMP y apaga sus marcas en VP8X. Comprueba
// el tope de píxeles con las dimensiones de la cabecera.
func webpSinMetadatos(datos []byte) ([]byte, error) {
	if len(datos) < 12 || string(datos[0:4]) != "RIFF" || string(datos[8:12]) != "WEBP" {
		return nil, mimeNoPermitido()
	}
	out := bytes.NewBuffer(make([]byte, 0, len(datos)))
	out.Write([]byte("RIFF\x00\x00\x00\x00WEBP"))
	ancho, alto := 0, 0
	for i := 12; i < len(datos); {
		if i+8 > len(datos) {
			return nil, mimeNoPermitido()
		}
		id := string(datos[i : i+4])
		n := int(binary.LittleEndian.Uint32(datos[i+4 : i+8]))
		fin := i + 8 + n
		if n < 0 || fin > len(datos) {
			return nil, mimeNoPermitido()
		}
		relleno := n % 2
		bloque := datos[i:fin]
		switch id {
		case "EXIF", "XMP ":
			// fuera
		case "VP8X":
			if n >= 10 {
				b := append([]byte(nil), bloque...)
				b[8] &^= 0x08 | 0x04 // EXIF y XMP
				ancho = 1 + int(uint32(b[12])|uint32(b[13])<<8|uint32(b[14])<<16)
				alto = 1 + int(uint32(b[15])|uint32(b[16])<<8|uint32(b[17])<<16)
				bloque = b
			}
			out.Write(bloque)
			if relleno == 1 {
				out.WriteByte(0)
			}
		default:
			if id == "VP8 " && ancho == 0 && n >= 10 {
				ancho = int(binary.LittleEndian.Uint16(datos[i+14:i+16]) & 0x3fff)
				alto = int(binary.LittleEndian.Uint16(datos[i+16:i+18]) & 0x3fff)
			}
			if id == "VP8L" && ancho == 0 && n >= 5 {
				v := binary.LittleEndian.Uint32(datos[i+9 : i+13])
				ancho, alto = int(v&0x3fff)+1, int((v>>14)&0x3fff)+1
			}
			out.Write(bloque)
			if relleno == 1 {
				out.WriteByte(0)
			}
		}
		i = fin + relleno
	}
	if ancho == 0 || alto == 0 {
		return nil, mimeNoPermitido()
	}
	if int64(ancho)*int64(alto) > MaxPixelesAdjuntoPortal {
		return nil, nucleo.ReglaNegocio("imagen_demasiado_grande", "La foto tiene demasiados píxeles (máximo 40 MP)")
	}
	b := out.Bytes()
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(b)-8)) //nolint:gosec // ≤ 10 MB
	return b, nil
}

// nombreSaneado: sin rutas, sin /, \, ", CR ni LF, ni caracteres de control; hasta 120 caracteres,
// con la extensión del tipo real.
func nombreSaneado(nombre, tipo string) string {
	nombre = path.Base(strings.ReplaceAll(nombre, "\\", "/"))
	var b strings.Builder
	for _, r := range nombre {
		if r == utf8.RuneError || unicode.IsControl(r) || strings.ContainsRune("/\\\"", r) {
			continue
		}
		b.WriteRune(r)
	}
	base := strings.TrimSpace(b.String())
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	if base == "" || base == "." || base == ".." {
		base = "adjunto"
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "application/pdf": ".pdf"}[tipo]
	if r := []rune(base); len(r)+len(ext) > 120 {
		base = string(r[:120-len(ext)])
	}
	return base + ext
}
