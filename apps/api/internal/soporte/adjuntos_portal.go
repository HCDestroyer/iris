package soporte

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
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

// Adjuntos del portal del cliente de Argos (contrato 0.9.0 §11.6; 0.14.2). Argos sube los bytes
// dentro de POST …/tickets y POST …/responder (multipart/form-data: la parte «datos» con el JSON y
// hasta 3 partes «archivos»); nadie descarga URLs del otro. Solo JPEG, PNG y PDF, comprobados por el
// contenido (HEIC y WebP: 422 mime_no_permitido; el portal de Argos los convierte a JPEG).
//
// En dos pasos, para no gastar memoria en peticiones que se van a rechazar (revisión de seguridad M1):
//
//  1. al leer el cuerpo (comprobarArchivo): tipo por el contenido, tamaño y dimensiones desde el
//     encabezado (SOF, IHDR) con DecodeConfig, sin decodificar: 40 MP y unos 200 MB decodificada
//     (ancho × alto × bytes por píxel según el modelo de color) → 422 imagen_demasiado_grande;
//  2. dentro del caso de uso, después de comprobar el ticket, los topes y el permiso del contacto
//     (recodificarArchivos): JPEG y PNG se decodifican y se vuelven a codificar sin ningún metadato
//     ni bloque del original, con a lo sumo MaxRecodificaciones a la vez en el proceso (lleno: 503
//     con Retry-After); PDF se guarda tal cual.

// Límites de §11.6.
const (
	MaxAdjuntosPortal       = 3
	MaxTamanoAdjuntoPortal  = 10 << 20
	MaxPixelesAdjuntoPortal = 40_000_000
	// MaxBytesDecodificada: la imagen decodificada en memoria (ancho × alto × bytes por píxel).
	MaxBytesDecodificada = 200 << 20
	// MaxRecodificaciones: decodificaciones a la vez en el proceso.
	MaxRecodificaciones = 2
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
	return nucleo.ReglaNegocio("mime_no_permitido", "Solo se aceptan fotos JPEG o PNG y documentos PDF")
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
			a, err := comprobarArchivo(p.FileName(), b)
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

func imagenDemasiadoGrande() *nucleo.Problema {
	return nucleo.ReglaNegocio("imagen_demasiado_grande", "La foto es demasiado grande (máximo 40 MP)")
}

// bytesPorPixel: lo que ocupa un píxel de la imagen decodificada según su modelo de color.
func bytesPorPixel(m color.Model) int64 {
	switch m {
	case color.GrayModel, color.AlphaModel:
		return 1
	case color.Gray16Model, color.Alpha16Model:
		return 2
	case color.YCbCrModel, color.NYCbCrAModel:
		return 4
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	}
	if _, ok := m.(color.Palette); ok {
		return 1
	}
	return 4 // RGBA, NRGBA, CMYK
}

// comprobarArchivo: tipo por el contenido y dimensiones desde el encabezado, sin decodificar. El
// archivo queda con sus bytes crudos (se recodifica después, en el caso de uso).
func comprobarArchivo(nombre string, datos []byte) (archivoPortal, error) {
	if len(datos) == 0 {
		return archivoPortal{}, mimeNoPermitido()
	}
	tipo := http.DetectContentType(datos)
	a := archivoPortal{Mime: tipo, Tipo: "foto", Datos: datos, Nombre: nombreSaneado(nombre, tipo)}
	switch tipo {
	case "image/jpeg", "image/png":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(datos))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
			return archivoPortal{}, mimeNoPermitido()
		}
		pixeles := int64(cfg.Width) * int64(cfg.Height)
		if pixeles > MaxPixelesAdjuntoPortal || pixeles*bytesPorPixel(cfg.ColorModel) > MaxBytesDecodificada {
			return archivoPortal{}, imagenDemasiadoGrande()
		}
	case "application/pdf":
		a.Tipo = "documento"
	default:
		return archivoPortal{}, mimeNoPermitido()
	}
	return a, nil
}

// recodificaciones limita las decodificaciones a la vez en el proceso.
var recodificaciones = make(chan struct{}, MaxRecodificaciones)

func ocupadoRecodificando() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusServiceUnavailable, "recodificacion_ocupada", "Ocupado",
		"Iris está procesando otras fotos; intenta de nuevo en unos segundos").ConCabecera("Retry-After", "5")
}

// recodificarArchivos recodifica las fotos ya comprobadas (sin metadatos). Si ya hay
// MaxRecodificaciones en curso, 503 con Retry-After (sin esperar: la petición se reintenta).
func recodificarArchivos(archivos []archivoPortal) ([]archivoPortal, error) {
	salida := make([]archivoPortal, 0, len(archivos))
	for _, a := range archivos {
		if a.Tipo != "foto" {
			salida = append(salida, a)
			continue
		}
		select {
		case recodificaciones <- struct{}{}:
		default:
			return nil, ocupadoRecodificando()
		}
		datos, err := recodificar(a.Datos, a.Mime)
		<-recodificaciones
		if err != nil {
			return nil, err
		}
		if len(datos) > MaxTamanoAdjuntoPortal {
			return nil, tamanoExcedido()
		}
		a.Datos = datos
		salida = append(salida, a)
	}
	return salida, nil
}

// recodificar decodifica y vuelve a codificar la imagen: el archivo nuevo no lleva metadatos. Las
// dimensiones ya se comprobaron (comprobarArchivo).
func recodificar(datos []byte, tipo string) ([]byte, error) {
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
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "application/pdf": ".pdf"}[tipo]
	if r := []rune(base); len(r)+len(ext) > 120 {
		base = string(r[:120-len(ext)])
	}
	return base + ext
}
