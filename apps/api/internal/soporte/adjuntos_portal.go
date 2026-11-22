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
		if pixeles > MaxPixelesAdjuntoPortal || memoriaDecodificada(datos, tipo, int64(cfg.Width), int64(cfg.Height), cfg.ColorModel) > MaxBytesDecodificada {
			return archivoPortal{}, imagenDemasiadoGrande()
		}
	case "application/pdf":
		a.Tipo = "documento"
	default:
		return archivoPortal{}, mimeNoPermitido()
	}
	return a, nil
}

// memoriaDecodificada estima lo que ocupa decodificar la imagen (revisiones H1, B1 y B2): la imagen
// final (píxeles × bytes por píxel) más lo que el decodificador de Go guarda aparte:
//   - JPEG progresivo (SOF2): los coeficientes de cada componente, 4 B por píxel;
//   - JPEG CMYK (4 componentes): una imagen intermedia de 4 B por píxel;
//   - JPEG RGB (Adobe transform 0 o componentes R, G, B): se decodifica a YCbCr y se copia a RGBA,
//     3 B por píxel más;
//   - PNG entrelazado (Adam7): el doble (cada pasada se decodifica aparte y se copia).
//
// Falla cerrado: si no encuentra el SOF, cuenta la imagen como progresiva con los componentes de su
// modelo de color. En un JPEG todo se cuenta con el ancho y el alto redondeados a múltiplos de 32
// (el MCU más grande, 4×8 píxeles por muestra × 8): Go reserva la imagen y los coeficientes con ese
// relleno (revisión del relleno de MCU).
func memoriaDecodificada(datos []byte, tipo string, ancho, alto int64, modelo color.Model) int64 {
	pixeles := ancho * alto
	if tipo == "image/jpeg" {
		pixeles = rellenoMCU(ancho) * rellenoMCU(alto)
	}
	total := pixeles * bytesPorPixel(modelo)
	switch tipo {
	case "image/jpeg":
		j := marcadoresJPEG(datos)
		if !j.sof {
			j.progresivo, j.componentes = true, componentesDe(modelo)
		}
		if j.progresivo {
			total += 4 * int64(j.componentes) * pixeles
		}
		if j.componentes == 4 {
			total += 4 * pixeles
		}
		if j.componentes == 3 && j.rgb {
			total += 3 * pixeles
		}
	case "image/png":
		// IHDR: firma (8) + largo (4) + "IHDR" (4) + ancho, alto, profundidad, color, compresión, filtro (12) → entrelazado en 28.
		if len(datos) > 28 && datos[28] == 1 {
			total *= 2
		}
	}
	return total
}

// rellenoMCU redondea un lado del JPEG al múltiplo de 32 siguiente.
func rellenoMCU(n int64) int64 {
	return (n + 31) / 32 * 32
}

// componentesDe: los componentes de un JPEG según el modelo de color de DecodeConfig.
func componentesDe(m color.Model) int {
	switch m {
	case color.GrayModel:
		return 1
	case color.CMYKModel:
		return 4
	}
	return 3
}

// infoJPEG es lo que importa de los marcadores para estimar la memoria.
type infoJPEG struct {
	sof         bool // se encontró un SOF
	progresivo  bool
	componentes int
	// rgb: el decodificador puede tratarlo como RGB (algún APP14 de Adobe con transform 0, o
	// componentes R, G, B). Se cuenta aunque un APP0 JFIF lo anule: falla cerrado.
	rgb bool
}

// marcadoresJPEG recorre los marcadores como image/jpeg (reader.go): salta los bytes que no son 0xff,
// trata «\xff\x00» como relleno, salta los 0xff de relleno, los RST y los segmentos. Mira todo el
// archivo (también después del primer SOS) para no perder un SOF o un APP14 escondido.
func marcadoresJPEG(datos []byte) infoJPEG {
	var r infoJPEG
	var ids []byte
recorrer:
	for i := 2; i+1 < len(datos); {
		if datos[i] != 0xff {
			i++
			continue
		}
		j := i + 1
		for j < len(datos) && datos[j] == 0xff {
			j++
		}
		if j >= len(datos) {
			break
		}
		m := datos[j]
		i = j + 1
		switch {
		case m == 0x00, m >= 0xd0 && m <= 0xd7, m == 0xd8, m == 0x01:
			continue
		case m == 0xd9:
			break recorrer
		}
		if i+2 > len(datos) {
			break
		}
		largo := int(datos[i])<<8 | int(datos[i+1])
		if largo < 2 {
			i += 2
			continue
		}
		seg := datos[i+2 : min(len(datos), i+largo)]
		switch {
		case m >= 0xc0 && m <= 0xcf && m != 0xc4 && m != 0xc8 && m != 0xcc:
			if !r.sof && len(seg) >= 6 {
				r.sof = true
				r.progresivo = m == 0xc2 || m == 0xc6 || m == 0xca || m == 0xce
				r.componentes = int(seg[5])
				for k := 0; k < r.componentes && 6+3*k < len(seg); k++ {
					ids = append(ids, seg[6+3*k])
				}
			}
		case m == 0xee:
			if len(seg) >= 12 && string(seg[:5]) == "Adobe" && seg[11] == 0 {
				r.rgb = true
			}
		}
		// Tras un SOS vienen los datos del escaneo: el bucle salta lo que no es marcador (y los «\xff\x00»).
		i += largo
	}
	if len(ids) == 3 && ids[0] == 'R' && ids[1] == 'G' && ids[2] == 'B' {
		r.rgb = true
	}
	return r
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
		datos, err := recodificarUna(a.Datos, a.Mime)
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

// recodificarUna toma un lugar del semáforo y recodifica. El lugar se libera siempre (defer), también
// si el decodificador entra en pánico con un archivo malicioso: el pánico se vuelve 422
// mime_no_permitido (revisión H2).
func recodificarUna(datos []byte, tipo string) (salida []byte, err error) {
	select {
	case recodificaciones <- struct{}{}:
	default:
		return nil, ocupadoRecodificando()
	}
	defer func() { <-recodificaciones }()
	defer func() {
		if r := recover(); r != nil {
			salida, err = nil, mimeNoPermitido()
		}
	}()
	return recodificador(datos, tipo)
}

// recodificador: recodificar (las pruebas lo cambian para simular un pánico).
var recodificador = recodificar

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
