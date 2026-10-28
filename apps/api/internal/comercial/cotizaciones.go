package comercial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// --- Planes de Argos -------------------------------------------------------------------------

// PlanComercial es un plan de Argos para cotizar. Los planes de Argos son de toda la
// instalación (sin empresa).
type PlanComercial struct {
	ID     int64   `json:"id"`
	Nombre string  `json:"nombre"`
	Precio *string `json:"precio" nullable:"true" doc:"Mensualidad (decimal, 2 cifras); null si Argos no la publica"`
	Activo bool    `json:"activo"`
}

// LectorPlanes lee los planes activos de Argos (GET /planes?activo=true[&router_id=]) por el
// conector, fuera de toda transacción. Sin conector activo devuelve el problema 503
// conector_apagado; Argos caído, 503 argos_no_disponible.
type LectorPlanes interface {
	Planes(ctx context.Context, routerID *int64) ([]PlanComercial, error)
}

// PlanesDeArgos lee los planes con el cliente del conector Argos activo (protección SSRF).
func PlanesDeArgos(am *argos.Modulo) LectorPlanes { return planesArgos{am: am} }

type planesArgos struct{ am *argos.Modulo }

// planArgos: GET /planes de Argos 1.2 (precio texto decimal, activo booleano, sin empresa).
type planArgos struct {
	ID     int64          `json:"id"`
	Nombre string         `json:"nombre"`
	Precio *argos.Decimal `json:"precio"`
	Activo *bool          `json:"activo"`
}

func (p planesArgos) Planes(ctx context.Context, routerID *int64) ([]PlanComercial, error) {
	cli, err := p.am.ClienteActivo(ctx, nucleo.BDDe(ctx))
	if err != nil {
		return nil, err
	}
	q := url.Values{"activo": {"true"}}
	if routerID != nil {
		q.Set("router_id", strconv.FormatInt(*routerID, 10))
	}
	ps, err := conector.Todas[planArgos](ctx, cli.Cliente, "/planes", q)
	if err != nil {
		return nil, argos.Problema(err)
	}
	salida := make([]PlanComercial, 0, len(ps))
	for _, pl := range ps {
		if pl.Activo != nil && !*pl.Activo {
			continue
		}
		salida = append(salida, PlanComercial{ID: pl.ID, Nombre: pl.Nombre, Precio: pl.Precio.Texto(), Activo: true})
	}
	return salida, nil
}

// EntradaPlanes: GET /comercial/planes.
type EntradaPlanes struct {
	RouterID int64 `query:"routerId" minimum:"0" doc:"Router sugerido por la factibilidad: solo los planes que ofrece"`
}

// PlanesDisponibles: respuesta de GET /comercial/planes.
type PlanesDisponibles struct {
	Disponible bool            `json:"disponible" doc:"false = sin conector (cotizar con descripción y precio a mano) o Argos no disponible (motivo)"`
	Motivo     *string         `json:"motivo" nullable:"true" doc:"sin_conector, conector_apagado, argos_no_disponible…"`
	Data       []PlanComercial `json:"data"`
}

func (m *Modulo) nuevoVerPlanes() *auditoria.Caso[struct{}, previoPlanes] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, previoPlanes]{
		Accion: "cotizaciones.crear", Permiso: nucleo.PermCotizacionesCrear, AuditarExito: soloLecturas[previoPlanes],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (previoPlanes, error) {
			estado, err := comercialbd.New(t).EstadoConectorArgos(ctx)
			return previoPlanes{Conector: estado}, err
		},
	})
}

type previoPlanes struct {
	Conector string `json:"conector"`
}

// Planes: los planes activos de Argos (GET /comercial/planes), sin inventar nada.
func (m *Modulo) Planes(ctx context.Context, in EntradaPlanes) (PlanesDisponibles, error) {
	p, err := m.verPlanes.Ejecutar(ctx, struct{}{})
	if err != nil {
		return PlanesDisponibles{}, err
	}
	r := PlanesDisponibles{Data: []PlanComercial{}}
	motivo := ""
	if p.Conector == "" || m.cfg.Planes == nil {
		motivo = MotivoSinConector
	}
	if motivo == "" {
		var router *int64
		if in.RouterID > 0 {
			router = &in.RouterID
		}
		ps, err := m.cfg.Planes.Planes(ctx, router)
		var pr *nucleo.Problema
		switch {
		case errors.As(err, &pr) && pr.Status >= 500:
			motivo = pr.Codigo()
		case err != nil:
			return r, err
		default:
			r.Data = ps
		}
	}
	if motivo != "" {
		r.Motivo = &motivo
	}
	r.Disponible = motivo == ""
	return r, nil
}

// --- Cotizar -----------------------------------------------------------------------------------

// EntradaLinea es una línea de la cotización. Montos como texto decimal (hasta 2 cifras).
type EntradaLinea struct {
	Tipo           string  `json:"tipo" enum:"plan,instalacion,equipo,descuento,otro"`
	PlanID         *int64  `json:"planId,omitempty" minimum:"1" doc:"Plan de Argos (obligatorio en la línea de plan con conector; sin conector no va)"`
	ProductoID     *int64  `json:"productoId,omitempty" minimum:"1" doc:"Producto de Argos (equipo, instalación), opcional"`
	Descripcion    *string `json:"descripcion,omitempty" maxLength:"200" doc:"En la línea de plan con conector sale de Argos"`
	Cantidad       string  `json:"cantidad,omitempty" pattern:"^[0-9]{1,8}(\\.[0-9]{1,2})?$" doc:"Por defecto 1"`
	PrecioUnitario *string `json:"precioUnitario,omitempty" pattern:"^[0-9]{1,12}(\\.[0-9]{1,2})?$" doc:"En la línea de plan con conector sale de Argos (si Argos no publica el precio, va aquí). En un descuento, el monto a restar"`
	Recurrente     *bool   `json:"recurrente,omitempty" doc:"Mensual: el plan siempre; equipo, descuento y otro pueden serlo (por defecto, único)"`
}

// EntradaCrearCotizacion crea la cotización (o una versión nueva que reemplaza la anterior).
type EntradaCrearCotizacion struct {
	ProspectoID  int64          `json:"-"`
	Version      int32          `json:"version" minimum:"1" doc:"Versión del prospecto"`
	Lineas       []EntradaLinea `json:"lineas" minItems:"1" maxItems:"50" doc:"Exactamente una línea de plan"`
	VigenteHasta *time.Time     `json:"vigenteHasta,omitempty" doc:"Por defecto 15 días; hasta 180"`
	Moneda       string         `json:"moneda,omitempty" pattern:"^[A-Z]{3}$" doc:"GTQ por defecto"`
	Notas        string         `json:"notas,omitempty" maxLength:"2000"`

	ClaveIdempotencia string `json:"-"`
}

// previoCotizacion: lo leído antes de pedir los planes a Argos.
type previoCotizacion struct {
	EmpresaID *int64 `json:"empresa_id"`
	Conector  string `json:"conector"`
	RouterID  *int64 `json:"router_id"` // el sugerido por la última factibilidad (filtra los planes)
}

type entradaCrearCotizacion struct {
	EntradaCrearCotizacion
	previo previoCotizacion
	planes []PlanComercial
}

// MarshalJSON de la auditoría (la entrada sin los planes leídos).
func (e entradaCrearCotizacion) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.EntradaCrearCotizacion)
}

var estadosCotizar = []string{EstadoFactibilidad, EstadoCotizado, EstadoContrato}

// exigirFactible: la última factibilidad es viable o requiere obra.
func exigirFactible(ctx context.Context, q *comercialbd.Queries, prospecto int64) (comercialbd.ComercialFactibilidad, error) {
	fa, err := q.UltimaFactibilidad(ctx, prospecto)
	if errors.Is(err, pgx.ErrNoRows) {
		return fa, nucleo.ReglaNegocio("factibilidad_requerida", "Evalúa la factibilidad antes de cotizar")
	}
	if err != nil {
		return fa, err
	}
	if fa.Resultado != FactibleViable && fa.Resultado != FactibleRequiereObra {
		return fa, nucleo.ReglaNegocio("factibilidad_no_viable",
			fmt.Sprintf("La última factibilidad es %s; evalúa de nuevo (o registra la de una visita técnica)", fa.Resultado))
	}
	return fa, nil
}

func (m *Modulo) nuevoPrepararCotizacion() *auditoria.Caso[EntradaCrearCotizacion, previoCotizacion] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearCotizacion, previoCotizacion]{
		Accion: "cotizaciones.crear", Permiso: nucleo.PermCotizacionesCrear, AuditarExito: soloLecturas[previoCotizacion],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearCotizacion) (previoCotizacion, error) {
			q := comercialbd.New(t)
			f, err := leer(ctx, t, q, in.ProspectoID)
			if err != nil {
				return previoCotizacion{}, err
			}
			if err := verificarAcceso(ctx, string(nucleo.PermCotizacionesCrear), f); err != nil {
				return previoCotizacion{}, err
			}
			estado, err := q.EstadoConectorArgos(ctx)
			if err != nil {
				return previoCotizacion{}, err
			}
			previo := previoCotizacion{EmpresaID: f.EmpresaID, Conector: estado}
			if fa, err := q.UltimaFactibilidad(ctx, f.ID); err == nil {
				previo.RouterID = fa.RouterID
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return previoCotizacion{}, err
			}
			return previo, nil
		},
	})
}

// CrearCotizacion: valida el prospecto, lee los planes de Argos fuera de la transacción (si hay
// líneas de plan y conector), guarda la cotización (reemplaza la anterior) y genera su PDF.
func (m *Modulo) CrearCotizacion(ctx context.Context, in EntradaCrearCotizacion) (Cotizacion, error) {
	previo, err := m.prepararCotizacion.Ejecutar(ctx, in)
	if err != nil {
		return Cotizacion{}, err
	}
	e := entradaCrearCotizacion{EntradaCrearCotizacion: in, previo: previo}
	if previo.Conector != "" && slices.ContainsFunc(in.Lineas, func(l EntradaLinea) bool { return l.Tipo == "plan" && l.PlanID != nil }) {
		if m.cfg.Planes == nil {
			return Cotizacion{}, nucleo.NoDisponible("No hay lector de planes de Argos configurado")
		}
		if e.planes, err = m.cfg.Planes.Planes(ctx, previo.RouterID); err != nil {
			return Cotizacion{}, err
		}
	}
	c, err := m.crearCotizacion.Ejecutar(ctx, e)
	if err != nil {
		return Cotizacion{}, err
	}
	return m.conPDFCotizacion(ctx, c), nil
}

// conPDFCotizacion genera y guarda el PDF si falta (un error queda en el log: el PDF sale
// no_generado y se pide de nuevo con POST /cotizaciones/{id}/pdf).
func (m *Modulo) conPDFCotizacion(ctx context.Context, c Cotizacion) Cotizacion {
	if c.PDF == nil || c.PDF.Estado == "completo" || m.cfg.Documentos == nil {
		return c
	}
	d, err := m.datosPDFCotizacion.Ejecutar(ctx, EntradaVerCotizacion{ID: c.ID})
	if err == nil {
		var contenido []byte
		if contenido, err = m.pdfCotizacion(d); err == nil {
			pdf := PDF{ID: c.PDF.AdjuntoID, Entidad: EntidadCotizacion, EntidadID: idTexto(c.ID), Contenido: contenido}
			if err = m.guardarPDF(ctx, pdf); err == nil {
				sha := pdf.SHA256()
				c.PDF.Estado, c.PDF.SHA256 = "completo", &sha
			}
		}
	}
	if err != nil {
		slog.WarnContext(ctx, "comercial: no se pudo guardar el PDF de la cotización", "cotizacion", c.ID, "err", err)
	}
	return c
}

// lineaCalculada es una línea lista para guardar.
type lineaCalculada struct {
	comercialbd.CrearLineaCotizacionParams
	cant, precio, total int64
}

// calcularLineas valida las líneas y calcula totales (en centavos).
func calcularLineas(in entradaCrearCotizacion) ([]lineaCalculada, int64, int64, *int64, error) {
	var mensual, unico int64
	var plan *int64
	planes := 0
	salida := make([]lineaCalculada, 0, len(in.Lineas))
	for i, l := range in.Lineas {
		campo := func(c string) string { return "lineas[" + strconv.Itoa(i) + "]." + c }
		lc := lineaCalculada{}
		lc.Posicion, lc.Tipo, lc.ProductoID = int32(i+1), l.Tipo, l.ProductoID //nolint:gosec // máximo 50
		cantidad := l.Cantidad
		if cantidad == "" {
			cantidad = "1"
		}
		var err error
		if lc.cant, err = leerMonto(campo("cantidad"), cantidad, 99_999_999); err != nil {
			return nil, 0, 0, nil, err
		}
		if lc.cant == 0 {
			return nil, 0, 0, nil, campoInvalido(campo("cantidad"), "Debe ser mayor que 0", "min")
		}
		descripcion := ""
		if l.Descripcion != nil {
			if descripcion, err = texto(campo("descripcion"), *l.Descripcion, 0, 200); err != nil {
				return nil, 0, 0, nil, err
			}
		}
		precio := l.PrecioUnitario
		lc.Recurrente = l.Recurrente != nil && *l.Recurrente
		switch l.Tipo {
		case "plan":
			planes++
			lc.Recurrente = true
			if lc.cant != 100 {
				return nil, 0, 0, nil, campoInvalido(campo("cantidad"), "La línea de plan lleva cantidad 1 (el precio solo baja con una línea de descuento)", "plan")
			}
			switch {
			case in.previo.Conector == "" && l.PlanID != nil:
				return nil, 0, 0, nil, campoInvalido(campo("planId"), "Sin conector con Argos el plan va con descripción y precio", "sin_conector")
			case in.previo.Conector != "" && l.PlanID == nil:
				return nil, 0, 0, nil, campoInvalido(campo("planId"), "Con conector, el plan es uno de Argos (GET /comercial/planes)", "required")
			case l.PlanID != nil:
				i := slices.IndexFunc(in.planes, func(p PlanComercial) bool { return p.ID == *l.PlanID })
				if i < 0 {
					return nil, 0, 0, nil, campoInvalido(campo("planId"), "El plan no existe o no está activo en Argos (para el router sugerido)", "plan")
				}
				p := in.planes[i]
				lc.PlanID, plan = &p.ID, &p.ID
				if descripcion == "" {
					descripcion = p.Nombre
				}
				if p.Precio != nil {
					if precio != nil {
						return nil, 0, 0, nil, campoInvalido(campo("precioUnitario"), "El precio del plan viene de Argos; para rebajarlo usa una línea de descuento", "argos")
					}
					precio = p.Precio
				}
			}
		case "descuento", "instalacion", "equipo", "otro":
			if l.PlanID != nil {
				return nil, 0, 0, nil, campoInvalido(campo("planId"), "Solo la línea de plan lleva planId", "plan")
			}
			if l.Tipo == "instalacion" {
				lc.Recurrente = false
			}
		}
		if descripcion == "" {
			return nil, 0, 0, nil, campoInvalido(campo("descripcion"), "Es obligatoria", "required")
		}
		if precio == nil {
			return nil, 0, 0, nil, campoInvalido(campo("precioUnitario"), "Es obligatorio", "required")
		}
		if lc.precio, err = leerMonto(campo("precioUnitario"), *precio, maxCentavos); err != nil {
			return nil, 0, 0, nil, err
		}
		var cabe bool
		if lc.total, cabe = multiplicar(lc.cant, lc.precio); !cabe {
			return nil, 0, 0, nil, campoInvalido(campo("precioUnitario"), "El total de la línea es demasiado grande", "max")
		}
		if l.Tipo == "descuento" {
			lc.total = -lc.total
		}
		lc.Descripcion = descripcion
		lc.Cantidad, lc.PrecioUnitario, lc.Total = aNumeric(lc.cant), aNumeric(lc.precio), aNumeric(lc.total)
		if lc.Recurrente {
			mensual += lc.total
		} else {
			unico += lc.total
		}
		if mensual > maxCentavos || unico > maxCentavos || mensual < -maxCentavos || unico < -maxCentavos {
			return nil, 0, 0, nil, campoInvalido("lineas", "El total es demasiado grande", "max")
		}
		salida = append(salida, lc)
	}
	if planes != 1 {
		return nil, 0, 0, nil, nucleo.ReglaNegocio("plan_requerido", "La cotización lleva exactamente una línea de plan")
	}
	if mensual < 0 || unico < 0 {
		return nil, 0, 0, nil, nucleo.ReglaNegocio("descuento_excesivo", "Los descuentos dejan un total negativo")
	}
	if mensual > maxCentavos || unico > maxCentavos {
		return nil, 0, 0, nil, campoInvalido("lineas", "El total es demasiado grande", "max")
	}
	return salida, mensual, unico, plan, nil
}

var monedaValida = regexp.MustCompile(`^[A-Z]{3}$`)

func (m *Modulo) nuevoCrearCotizacion() *auditoria.Caso[entradaCrearCotizacion, Cotizacion] {
	return auditoria.Nuevo(auditoria.Caso[entradaCrearCotizacion, Cotizacion]{
		Accion: "cotizaciones.crear", Permiso: nucleo.PermCotizacionesCrear,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaCrearCotizacion) (Cotizacion, error) {
			q := comercialbd.New(t)
			idem := conIdempotencia(ctx, in.ClaveIdempotencia)
			if idem != nil {
				previa, err := q.CotizacionPorIdempotencia(ctx, idem)
				if err == nil && previa.ProspectoID == in.ProspectoID {
					if _, err := leer(ctx, t, q, previa.ProspectoID); err != nil {
						return Cotizacion{}, err
					}
					t.FijarEntidad("cotizacion", previa.ID)
					t.Anotar(map[string]any{"repetida": true})
					return m.aCotizacion(ctx, q, previa)
				}
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return Cotizacion{}, err
				}
			}
			antes, err := tomar(ctx, t, q, in.ProspectoID, nucleo.PermCotizacionesCrear, &in.Version)
			if err != nil {
				return Cotizacion{}, err
			}
			if err := exigirEstado(antes, "cotizar", estadosCotizar...); err != nil {
				return Cotizacion{}, err
			}
			if _, err := exigirFactible(ctx, q, antes.ID); err != nil {
				return Cotizacion{}, err
			}
			estado, err := q.EstadoConectorArgos(ctx)
			if err != nil {
				return Cotizacion{}, err
			}
			if estado != in.previo.Conector {
				return Cotizacion{}, nucleo.Conflicto("conector_cambio", "El conector con Argos cambió mientras cotizabas; vuelve a intentar")
			}
			lineas, mensual, unico, plan, err := calcularLineas(in)
			if err != nil {
				return Cotizacion{}, err
			}
			ahora := m.ahora()
			vence := ahora.Add(m.cfg.VigenciaCotizacion)
			if in.VigenteHasta != nil {
				vence = in.VigenteHasta.UTC().Truncate(time.Microsecond)
				if !vence.After(ahora) || vence.After(ahora.Add(180*24*time.Hour)) {
					return Cotizacion{}, campoInvalido("vigenteHasta", "Entre ahora y 180 días", "fecha")
				}
			}
			moneda := in.Moneda
			if moneda == "" {
				moneda = "GTQ"
			}
			if !monedaValida.MatchString(moneda) {
				return Cotizacion{}, campoInvalido("moneda", "Código ISO de 3 letras", "moneda")
			}
			notas, err := texto("notas", in.Notas, 0, 2000)
			if err != nil {
				return Cotizacion{}, err
			}
			// La versión anterior (vigente o aceptada) queda reemplazada; un contrato en borrador
			// se anula (era de esa cotización); uno firmado impide cotizar de nuevo.
			viva, err := q.CotizacionVivaParaActualizar(ctx, antes.ID)
			hayViva := err == nil
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return Cotizacion{}, err
			}
			if c, err := q.ContratoVivoDeProspecto(ctx, antes.ID); err == nil && c.Estado == "firmado" {
				return Cotizacion{}, transicionInvalida("El contrato ya está firmado: no se cotiza de nuevo")
			} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return Cotizacion{}, err
			}
			if err := anularContratoVivo(ctx, t, q, antes.ID, "Cotización reemplazada por una revisión nueva"); err != nil {
				return Cotizacion{}, err
			}
			if hayViva {
				if err := q.MarcarReemplazada(ctx, viva.ID); err != nil {
					return Cotizacion{}, err
				}
			}
			revision, err := q.SiguienteRevision(ctx, antes.ID)
			if err != nil {
				return Cotizacion{}, err
			}
			cliente, err := json.Marshal(clienteDe(antes))
			if err != nil {
				return Cotizacion{}, err
			}
			adjunto, err := uuid.NewV7()
			if err != nil {
				return Cotizacion{}, err
			}
			var pdfID *uuid.UUID
			if m.cfg.Documentos != nil {
				pdfID = &adjunto
			}
			f, err := q.CrearCotizacion(ctx, comercialbd.CrearCotizacionParams{
				EmpresaID: antes.EmpresaID, ProspectoID: antes.ID, Revision: revision, Moneda: moneda,
				TotalMensual: aNumeric(mensual), TotalUnico: aNumeric(unico), VigenteHasta: vence, PlanID: plan, Notas: notas,
				Cliente: cliente, PdfAdjuntoID: pdfID, CreadoPor: actorUsuario(t), Idempotencia: idem,
			})
			if err != nil {
				return Cotizacion{}, err
			}
			for _, l := range lineas {
				l.CotizacionID = f.ID
				if _, err := q.CrearLineaCotizacion(ctx, l.CrearLineaCotizacionParams); err != nil {
					return Cotizacion{}, err
				}
			}
			if hayViva {
				if err := q.EnlazarReemplazo(ctx, comercialbd.EnlazarReemplazoParams{ReemplazadaPor: &f.ID, ID: viva.ID}); err != nil {
					return Cotizacion{}, err
				}
			}
			despues := antes
			despues.Estado = EstadoCotizado
			p, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return Cotizacion{}, err
			}
			t.FijarEntidad("cotizacion", f.ID)
			t.RegistrarCambios("cotizacion", f.ID, nil, f)
			var reemplaza *int64
			if hayViva {
				reemplaza = &viva.ID
			}
			if err := historial(ctx, t, q, p.ID, "cotizacion", nil, nil, nil, map[string]any{
				"cotizacion_id": f.ID, "numero": f.Numero, "revision": f.Revision, "reemplaza_id": reemplaza,
			}); err != nil {
				return Cotizacion{}, err
			}
			if err := t.Emitir("cotizacion.creada", map[string]any{
				"cotizacion_id": f.ID, "prospecto_id": p.ID, "numero": f.Numero, "revision": f.Revision, "plan_id": f.PlanID,
				"total_mensual": aTexto(mensual), "total_unico": aTexto(unico), "moneda": f.Moneda, "reemplaza_id": reemplaza,
			}); err != nil {
				return Cotizacion{}, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, p, nil); err != nil {
				return Cotizacion{}, err
			}
			return m.aCotizacion(ctx, q, f)
		},
	})
}

func clienteDe(p comercialbd.ComercialProspecto) clientePDF {
	return clientePDF{Prospecto: p.Numero, Nombre: p.Nombre, Documento: deref(p.Documento), Direccion: p.Direccion,
		Telefono: deref(p.Telefono), Correo: deref(p.Correo)}
}

// aCotizacion arma la cotización con sus líneas y el estado de su PDF.
func (m *Modulo) aCotizacion(ctx context.Context, q *comercialbd.Queries, f comercialbd.ComercialCotizacion) (Cotizacion, error) {
	c := Cotizacion{
		ID: f.ID, Numero: f.Numero, ProspectoID: f.ProspectoID, Revision: f.Revision, Estado: f.Estado,
		Vencida: f.Estado == "vigente" && !f.VigenteHasta.After(m.ahora()), Moneda: f.Moneda,
		TotalMensual: aTexto(centavos(f.TotalMensual)), TotalUnico: aTexto(centavos(f.TotalUnico)), VigenteHasta: f.VigenteHasta,
		PlanID: f.PlanID, Notas: f.Notas, ReemplazadaPor: f.ReemplazadaPor, AceptadaEn: f.AceptadaEn, AceptadaPor: f.AceptadaPor,
		CreadoPor: f.CreadoPor, CreadoEn: f.CreatedAt, Version: f.Version, Lineas: []LineaCotizacion{},
	}
	ls, err := q.LineasDeCotizacion(ctx, f.ID)
	if err != nil {
		return c, err
	}
	for _, l := range ls {
		c.Lineas = append(c.Lineas, LineaCotizacion{Posicion: l.Posicion, Tipo: l.Tipo, PlanID: l.PlanID, ProductoID: l.ProductoID,
			Descripcion: l.Descripcion, Cantidad: aTexto(centavos(l.Cantidad)), PrecioUnitario: aTexto(centavos(l.PrecioUnitario)),
			Recurrente: l.Recurrente, Total: aTexto(centavos(l.Total))})
	}
	c.PDF, err = documento(ctx, q, f.PdfAdjuntoID)
	return c, err
}

// documento: el estado del adjunto (no_generado si todavía no se registró).
func documento(ctx context.Context, q *comercialbd.Queries, id *uuid.UUID) (*DocumentoPDF, error) {
	if id == nil {
		return nil, nil
	}
	a, err := q.EstadoAdjunto(ctx, *id)
	if errors.Is(err, pgx.ErrNoRows) {
		return documentoDe(id, nil, nil), nil
	}
	if err != nil {
		return nil, err
	}
	var sha *string
	if a.Estado == "completo" {
		sha = &a.Sha256
	}
	return documentoDe(id, &a.Estado, sha), nil
}

// EntradaVerCotizacion: GET /cotizaciones/{id}.
type EntradaVerCotizacion struct {
	ID int64 `path:"id" minimum:"1"`
}

// EntradaAceptarCotizacion: aceptar una cotización.
type EntradaAceptarCotizacion struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1" doc:"Versión de la cotización"`
}

// cotizacionVisible: la cotización y su prospecto, visible para el actor (404 si no).
func cotizacionVisible(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, id int64, candado bool) (comercialbd.ComercialCotizacion, comercialbd.ComercialProspecto, error) {
	var c comercialbd.ComercialCotizacion
	var err error
	if candado {
		c, err = q.CotizacionPorIDParaActualizar(ctx, id)
	} else {
		c, err = q.CotizacionPorID(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return c, comercialbd.ComercialProspecto{}, nucleo.NoEncontrado("La cotización no existe")
	}
	if err != nil {
		return c, comercialbd.ComercialProspecto{}, err
	}
	p, err := leer(ctx, t, q, c.ProspectoID)
	var pr *nucleo.Problema
	if errors.As(err, &pr) && pr.Status == 404 {
		return c, p, nucleo.NoEncontrado("La cotización no existe")
	}
	t.FijarEntidad("cotizacion", c.ID)
	return c, p, err
}

func (m *Modulo) nuevoVerCotizacion() *auditoria.Caso[EntradaVerCotizacion, Cotizacion] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerCotizacion, Cotizacion]{
		Accion: "prospectos.ver", Permiso: nucleo.PermProspectosVer, AuditarExito: soloLecturas[Cotizacion],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerCotizacion) (Cotizacion, error) {
			q := comercialbd.New(t)
			c, _, err := cotizacionVisible(ctx, t, q, in.ID, false)
			if err != nil {
				return Cotizacion{}, err
			}
			return m.aCotizacion(ctx, q, c)
		},
	})
}

func (m *Modulo) nuevoListarCotizaciones() *auditoria.Caso[EntradaVerProspecto, nucleo.Pagina[Cotizacion]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerProspecto, nucleo.Pagina[Cotizacion]]{
		Accion: "prospectos.ver", Permiso: nucleo.PermProspectosVer, AuditarExito: soloLecturas[nucleo.Pagina[Cotizacion]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerProspecto) (nucleo.Pagina[Cotizacion], error) {
			q := comercialbd.New(t)
			if _, err := leer(ctx, t, q, in.ID); err != nil {
				return nucleo.Pagina[Cotizacion]{}, err
			}
			fs, err := q.CotizacionesDeProspecto(ctx, in.ID)
			if err != nil {
				return nucleo.Pagina[Cotizacion]{}, err
			}
			salida := make([]Cotizacion, 0, len(fs))
			for _, f := range fs {
				c, err := m.aCotizacion(ctx, q, f)
				if err != nil {
					return nucleo.Pagina[Cotizacion]{}, err
				}
				salida = append(salida, c)
			}
			return nucleo.PaginaDe(salida), nil
		},
	})
}

func (m *Modulo) nuevoAceptarCotizacion() *auditoria.Caso[EntradaAceptarCotizacion, Cotizacion] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAceptarCotizacion, Cotizacion]{
		Accion: "cotizaciones.aceptar", Permiso: nucleo.PermCotizacionesAceptar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAceptarCotizacion) (Cotizacion, error) {
			q := comercialbd.New(t)
			c, _, err := cotizacionVisible(ctx, t, q, in.ID, false)
			if err != nil {
				return Cotizacion{}, err
			}
			antes, err := tomar(ctx, t, q, c.ProspectoID, nucleo.PermCotizacionesAceptar, nil)
			if err != nil {
				return Cotizacion{}, err
			}
			t.FijarEntidad("cotizacion", c.ID)
			if c, err = q.CotizacionPorIDParaActualizar(ctx, c.ID); err != nil {
				return Cotizacion{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, c.Version, "cotizacion", c.ID); err != nil {
				return Cotizacion{}, err
			}
			if err := exigirEstado(antes, "aceptar la cotización de", EstadoCotizado); err != nil {
				return Cotizacion{}, err
			}
			if c.Estado != "vigente" {
				return Cotizacion{}, transicionInvalida("Solo se acepta la cotización vigente (esta está " + c.Estado + ")")
			}
			ahora := m.ahora()
			if !c.VigenteHasta.After(ahora) {
				return Cotizacion{}, nucleo.ReglaNegocio("cotizacion_vencida", "La cotización venció el "+m.diaLocal(c.VigenteHasta)+"; crea una revisión nueva")
			}
			f, err := q.AceptarCotizacion(ctx, comercialbd.AceptarCotizacionParams{Ahora: &ahora, AceptadaPor: actorUsuario(t), ID: c.ID, Version: c.Version})
			if err != nil {
				return Cotizacion{}, nucleo.VerificarVersion(err, "cotizacion", c.ID)
			}
			t.RegistrarCambios("cotizacion", f.ID, c, f)
			// El prospecto sube de versión (cambió su embudo).
			if _, err := guardar(ctx, t, q, antes, antes); err != nil {
				return Cotizacion{}, err
			}
			if err := historial(ctx, t, q, antes.ID, "cotizacion_aceptada", nil, nil, nil, map[string]any{"cotizacion_id": f.ID, "numero": f.Numero}); err != nil {
				return Cotizacion{}, err
			}
			if err := t.Emitir("cotizacion.aceptada", map[string]any{"cotizacion_id": f.ID, "prospecto_id": antes.ID, "numero": f.Numero}); err != nil {
				return Cotizacion{}, err
			}
			return m.aCotizacion(ctx, q, f)
		},
	})
}

// datosPDFCotizacion lee lo que se imprime (permiso cotizaciones.crear; sin auditar el éxito).
func (m *Modulo) nuevoDatosPDFCotizacion() *auditoria.Caso[EntradaVerCotizacion, datosCotizacionPDF] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerCotizacion, datosCotizacionPDF]{
		Accion: "cotizaciones.crear", Permiso: nucleo.PermCotizacionesCrear, AuditarExito: soloLecturas[datosCotizacionPDF],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerCotizacion) (datosCotizacionPDF, error) {
			q := comercialbd.New(t)
			f, _, err := cotizacionVisible(ctx, t, q, in.ID, false)
			if err != nil {
				return datosCotizacionPDF{}, err
			}
			c, err := m.aCotizacion(ctx, q, f)
			if err != nil {
				return datosCotizacionPDF{}, err
			}
			d := datosCotizacionPDF{Numero: f.Numero, Moneda: f.Moneda, Notas: f.Notas, Revision: f.Revision, Creada: f.CreatedAt,
				VigenteHasta: f.VigenteHasta, Lineas: c.Lineas, TotalMensual: c.TotalMensual, TotalUnico: c.TotalUnico, CotizacionID: f.ID}
			if f.PdfAdjuntoID != nil {
				d.AdjuntoID = f.PdfAdjuntoID.String()
			}
			_ = json.Unmarshal(f.Cliente, &d.Cliente)
			return d, nil
		},
	})
}

// GenerarPDFCotizacion vuelve a generar y guardar el PDF si no está completo (POST
// /cotizaciones/{id}/pdf). Mismos datos = mismos bytes: el alta del adjunto es idempotente.
func (m *Modulo) GenerarPDFCotizacion(ctx context.Context, in EntradaVerCotizacion) (Cotizacion, error) {
	c, err := m.VerCotizacion.Ejecutar(ctx, in)
	if err != nil {
		return Cotizacion{}, err
	}
	if c.PDF == nil {
		return c, nucleo.NoDisponible("No hay almacenamiento de documentos configurado")
	}
	if c.PDF.Estado == "completo" {
		return c, nil
	}
	d, err := m.datosPDFCotizacion.Ejecutar(ctx, in)
	if err != nil {
		return Cotizacion{}, err
	}
	contenido, err := m.pdfCotizacion(d)
	if err != nil {
		return Cotizacion{}, err
	}
	if err := m.guardarPDF(ctx, PDF{ID: c.PDF.AdjuntoID, Entidad: EntidadCotizacion, EntidadID: idTexto(c.ID), Contenido: contenido}); err != nil {
		return Cotizacion{}, err
	}
	return m.VerCotizacion.Ejecutar(ctx, in)
}
