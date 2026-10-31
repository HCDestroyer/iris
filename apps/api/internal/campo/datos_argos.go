package campo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/formato"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// «Dato de Argos cambió» (contrato Iris ↔ ArgosISP 0.5.0; I1): cuando Argos avisa que cambió la
// dirección, las coordenadas, la referencia o el contacto de un servicio (servicio.datos_cambiados) o
// de un cliente (cliente.actualizado), el conector emite argos.datos_cambiados. Las órdenes abiertas
// conservan lo que copiaron al crearse (el técnico puede ir en camino): solo quedan marcadas con
// cuándo y qué cambió, para que despacho lo revise y aplique. Emite orden.datos_argos_cambiados por
// orden marcada.

// DatosArgosCambiados es la marca de la orden.
type DatosArgosCambiados struct {
	En     time.Time `json:"en" doc:"Último aviso de Argos"`
	Campos []string  `json:"campos" doc:"Campos que cambiaron en Argos desde que se copió la orden (servicio.direccion, cliente.referencia, servicio.contacto_telefono…), acumulados"`
}

func datosArgosDe(f campobd.CampoOrden) *DatosArgosCambiados {
	if f.DatosArgosCambiadosEn == nil {
		return nil
	}
	campos := f.DatosArgosCampos
	if campos == nil {
		campos = []string{}
	}
	return &DatosArgosCambiados{En: *f.DatosArgosCambiadosEn, Campos: campos}
}

// EntradaDatosArgos es el payload de argos.datos_cambiados.
type EntradaDatosArgos struct {
	Entidad    string   `json:"entidad"`
	ClienteID  *int64   `json:"cliente_id"`
	ServicioID *int64   `json:"servicio_id"`
	Campos     []string `json:"campos"`
	EntranteID int64    `json:"entrante_id"`
}

// ResultadoDatosArgos: las órdenes marcadas.
type ResultadoDatosArgos struct {
	Ordenes []int64 `json:"ordenes"`
}

func (m *Modulo) nuevoMarcarDatosArgos() *auditoria.Caso[EntradaDatosArgos, ResultadoDatosArgos] {
	return auditoria.Nuevo(auditoria.Caso[EntradaDatosArgos, ResultadoDatosArgos]{
		Accion: "ordenes.marcar_datos_argos", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoDatosArgos) bool { return len(r.Ordenes) > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDatosArgos) (ResultadoDatosArgos, error) {
			r := ResultadoDatosArgos{Ordenes: []int64{}}
			campos := slices.Clone(in.Campos)
			slices.Sort(campos)
			campos = slices.Compact(campos)
			if len(campos) == 0 || (in.ClienteID == nil && in.ServicioID == nil) {
				return r, nil
			}
			p := campobd.MarcarDatosArgosParams{CambiadoEn: m.ahora(), Campos: campos}
			if in.Entidad == "servicio" {
				p.ServicioID = in.ServicioID
			} else {
				// Un cambio del cliente: todas sus órdenes abiertas (las que tienen servicio copian la
				// dirección del servicio, pero el contacto y la referencia del cliente también cuentan).
				p.ClienteID = in.ClienteID
			}
			filas, err := campobd.New(t).MarcarDatosArgos(ctx, p)
			if err != nil {
				return r, err
			}
			for _, f := range filas {
				r.Ordenes = append(r.Ordenes, f.ID)
				if f.EmpresaID != nil {
					t.FijarEmpresa(*f.EmpresaID)
				} else {
					t.FijarEmpresa(0)
				}
				if err := t.Emitir("orden.datos_argos_cambiados", map[string]any{
					"orden_id": f.ID, "numero": f.Numero, "entidad": in.Entidad, "campos": campos, "entrante_id": in.EntranteID,
				}); err != nil {
					return r, err
				}
			}
			t.FijarEmpresa(0)
			t.Anotar(map[string]any{"ordenes": r.Ordenes, "campos": campos, "entrante_id": in.EntranteID})
			return r, nil
		},
	})
}

// SuscribirDatosArgos conecta argos.datos_cambiados (en cmd/worker). Idempotente: marcar dos veces
// deja la misma marca (los campos se acumulan sin repetir).
func (m *Modulo) SuscribirDatosArgos(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	bus.Suscribir("campo.datos_argos", argos.EventoDatosCambiados, func(ctx context.Context, e integracion.Evento) error {
		var in EntradaDatosArgos
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			log.ErrorContext(ctx, "campo: argos.datos_cambiados ilegible", "evento", e.ID, "err", err)
			return nil
		}
		_, err := m.MarcarDatosArgos.Ejecutar(nucleo.ConBD(ctx, bd), in)
		return err
	})
}

// Atender el aviso «Dato de Argos cambió» (despacho, supervisor): aplicar los datos actuales de la
// instantánea de Argos a la orden abierta, o descartar el aviso. Ambas quitan la marca, suben la
// versión (409 con una vieja), auditan el diff por campo y avisan al técnico (feed «actualizada»).

// Grupos de datos que se aplican.
const (
	GrupoDireccion   = "direccion"
	GrupoCoordenadas = "coordenadas"
	GrupoReferencia  = "referencia"
	GrupoContacto    = "contacto"
)

var gruposDatosArgos = []string{GrupoDireccion, GrupoCoordenadas, GrupoReferencia, GrupoContacto}

// EntradaAplicarDatosArgos es la entrada de ordenes.aplicar_datos_argos.
type EntradaAplicarDatosArgos struct {
	ID      int64     `json:"-"`
	Version int32     `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	AvisoEn time.Time `json:"avisoEn" doc:"datosArgosCambiados.en que leíste; si Argos avisó algo después, 409 aviso_argos_cambiado"`
	Campos  []string  `json:"campos,omitempty" required:"false" maxItems:"4" uniqueItems:"true" enum:"direccion,coordenadas,referencia,contacto" doc:"Qué aplicar; sin campos, todo. Lo que Argos no tiene se queda como está en la orden. Del aviso se quitan solo los campos de lo aplicado: si queda algo, sigue avisado"`
}

// EntradaDescartarDatosArgos es la entrada de ordenes.descartar_datos_argos.
type EntradaDescartarDatosArgos struct {
	ID      int64     `json:"-"`
	Version int32     `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	AvisoEn time.Time `json:"avisoEn" doc:"datosArgosCambiados.en que leíste; si Argos avisó algo después, 409 aviso_argos_cambiado"`
	Motivo  *string   `json:"motivo,omitempty" required:"false" maxLength:"500" doc:"Por qué la orden se queda con sus datos"`
}

func (e EntradaAplicarDatosArgos) ordenID() int64   { return e.ID }
func (e EntradaDescartarDatosArgos) ordenID() int64 { return e.ID }

// datosArgosActuales: lo que Argos dice hoy, con la regla de la creación de la orden: dirección y
// coordenadas del servicio y, si no tiene, del cliente; referencia del servicio o del cliente;
// contacto del servicio o, si no, el nombre y el teléfono principal del cliente. ok = false sin
// ninguna instantánea.
type datosArgos struct {
	direccion                        *string
	lat, lon                         *float64
	referencia                       *string
	contactoNombre, contactoTelefono *string
}

func noVacio(s string) *string {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	return &s
}

// Resultado de datosArgosActuales.
const (
	instantaneaSirve      = iota // hay una instantánea del cliente o del servicio de la orden
	instantaneaNoHay             // Iris no tiene ninguna
	instantaneaNoCoincide        // las hay, pero ya no son del cliente ni de la empresa de la orden
)

// datosArgosActuales lee la instantánea del servicio y del cliente de la orden. Solo se usa la que
// sigue siendo de la orden: el servicio, si es del cliente de la orden y de su empresa; el cliente, si
// es de su empresa (Argos pudo mover el servicio a otro cliente o empresa después de copiarlo).
func datosArgosActuales(ctx context.Context, q *campobd.Queries, f campobd.CampoOrden) (datosArgos, int, error) {
	var d datosArgos
	hay, sirve := false, false
	if f.ServicioID != nil {
		s, err := q.ServicioArgos(ctx, *f.ServicioID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return d, instantaneaNoHay, err
		default:
			hay = true
			if (f.ClienteID == nil || s.ClienteID == *f.ClienteID) && (f.EmpresaID == nil || s.EmpresaID == *f.EmpresaID) {
				sirve = true
				d.direccion, d.referencia = noVacio(s.Direccion), noVacio(s.Referencia)
				if s.Lat != nil && s.Lon != nil {
					d.lat, d.lon = s.Lat, s.Lon
				}
				d.contactoNombre = noVacio(s.ContactoNombre)
				if p := formato.Principal(formato.Telefonos("telefono", s.ContactoTelefono)); p != nil {
					d.contactoTelefono = &p.Mostrar
				}
			}
		}
	}
	if f.ClienteID != nil {
		c, err := q.ClienteArgos(ctx, *f.ClienteID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return d, instantaneaNoHay, err
		default:
			hay = true
			if f.EmpresaID == nil || c.EmpresaID == *f.EmpresaID || slices.Contains(c.Empresas, *f.EmpresaID) {
				sirve = true
				if d.direccion == nil {
					d.direccion = noVacio(c.Direccion)
				}
				if d.lat == nil && c.Lat != nil && c.Lon != nil {
					d.lat, d.lon = c.Lat, c.Lon
				}
				if d.referencia == nil {
					d.referencia = noVacio(c.Referencia)
				}
				if d.contactoNombre == nil && d.contactoTelefono == nil {
					d.contactoNombre = noVacio(c.Nombre)
					tels := append(formato.Telefonos("telefono", c.Telefono), formato.Telefonos("movil", c.Movil)...)
					if p := formato.Principal(tels); p != nil {
						d.contactoTelefono = &p.Mostrar
					}
				}
			}
		}
	}
	switch {
	case sirve:
		return d, instantaneaSirve, nil
	case hay:
		return d, instantaneaNoCoincide, nil
	}
	return d, instantaneaNoHay, nil
}

// camposDelGrupo: los campos de la marca (sin el prefijo servicio./cliente.) que atiende cada grupo.
var camposDelGrupo = map[string][]string{
	GrupoDireccion:   {"direccion"},
	GrupoCoordenadas: {"coordenadas", "lat", "lon"},
	GrupoReferencia:  {"referencia"},
	GrupoContacto:    {"contacto_nombre", "contacto_telefono", "nombre", "telefono", "movil"},
}

// camposRestantes: la marca sin los campos de los grupos aplicados (lo demás sigue avisado).
func camposRestantes(marca, grupos []string) []string {
	quitar := map[string]bool{}
	for _, g := range grupos {
		for _, c := range camposDelGrupo[g] {
			quitar[c] = true
		}
	}
	salida := []string{}
	for _, c := range marca {
		nombre := c
		if i := strings.IndexByte(c, '.'); i >= 0 {
			nombre = c[i+1:]
		}
		if !quitar[nombre] {
			salida = append(salida, c)
		}
	}
	return salida
}

func recortar(s *string, n int) *string {
	if s == nil {
		return nil
	}
	if r := []rune(*s); len(r) > n {
		v := string(r[:n])
		return &v
	}
	return s
}

// exigirAvisoAbierto: orden abierta, con aviso, y el aviso es el que despacho leyó (avisoEn =
// datosArgosCambiados.en): un aviso nuevo de Argos después de leer (marcar no sube la versión) da 409
// aviso_argos_cambiado, para no atender sin verlo lo que llegó después.
func exigirAvisoAbierto(f campobd.CampoOrden, accion string, avisoEn time.Time) error {
	if err := exigirEstado(f, accion, EstadoPendiente, EstadoProgramada, EstadoEnCamino, EstadoEnSitio, EstadoPausada); err != nil {
		return err
	}
	if f.DatosArgosCambiadosEn == nil {
		return nucleo.ReglaNegocio("sin_aviso_argos", "La orden no tiene el aviso «Dato de Argos cambió»")
	}
	if !f.DatosArgosCambiadosEn.Equal(avisoEn) {
		return nucleo.Conflicto("aviso_argos_cambiado", "Argos avisó otro cambio después de que leíste la orden: recárgala y revísalo")
	}
	return nil
}

func (m *Modulo) nuevoAplicarDatosArgos() *auditoria.Caso[EntradaAplicarDatosArgos, Orden] {
	return accionOrden("ordenes.aplicar_datos_argos", nucleo.PermOrdenesAplicarDatosArgos, sinLote[EntradaAplicarDatosArgos],
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaAplicarDatosArgos) (campobd.CampoOrden, string, error) {
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesAplicarDatosArgos, &in.Version, nil)
			if err != nil {
				return antes, "", err
			}
			if err := exigirAvisoAbierto(antes, "aplicar los datos de Argos a", in.AvisoEn); err != nil {
				return antes, "", err
			}
			grupos := gruposDatosArgos
			if len(in.Campos) > 0 {
				grupos = slices.Clone(in.Campos)
				slices.Sort(grupos)
				grupos = slices.Compact(grupos)
				for i, g := range grupos {
					if !slices.Contains(gruposDatosArgos, g) {
						return antes, "", campoInvalido(fmt.Sprintf("campos[%d]", i), "direccion, coordenadas, referencia o contacto", "enum")
					}
				}
			}
			d, estado, err := datosArgosActuales(ctx, q, antes)
			switch {
			case err != nil:
				return antes, "", err
			case estado == instantaneaNoHay:
				return antes, "", nucleo.ReglaNegocio("sin_instantanea_argos",
					"Iris no tiene la instantánea del cliente ni del servicio de Argos de esta orden: ábrela en la ficha (Refrescar) y vuelve a intentar")
			case estado == instantaneaNoCoincide:
				return antes, "", nucleo.ReglaNegocio("instantanea_no_coincide",
					"En Argos el servicio y el cliente ya no son de esta orden (otro cliente u otra empresa): revisa la orden a mano")
			}
			p := campobd.AplicarDatosArgosOrdenParams{
				Direccion: antes.Direccion, Lat: antes.Lat, Lon: antes.Lon, Referencia: antes.Referencia,
				ContactoNombre: antes.ContactoNombre, ContactoTelefono: antes.ContactoTelefono, ID: antes.ID, Version: antes.Version,
				CamposRestantes: camposRestantes(antes.DatosArgosCampos, grupos),
			}
			for _, g := range grupos {
				switch g {
				case GrupoDireccion:
					if d.direccion != nil {
						p.Direccion = *recortar(d.direccion, 500)
					}
				case GrupoCoordenadas:
					if d.lat != nil {
						p.Lat, p.Lon = d.lat, d.lon
					}
				case GrupoReferencia:
					if d.referencia != nil {
						p.Referencia = recortar(d.referencia, 500)
					}
				case GrupoContacto:
					if d.contactoNombre != nil || d.contactoTelefono != nil {
						p.ContactoNombre, p.ContactoTelefono = recortar(d.contactoNombre, 200), recortar(d.contactoTelefono, 50)
					}
				}
			}
			f, err := q.AplicarDatosArgosOrden(ctx, p)
			if err != nil {
				return f, "", nucleo.VerificarVersion(err, "orden", antes.ID)
			}
			t.RegistrarCambios("orden", f.ID, antes, f)
			if err := alimentarFeed(ctx, q, &antes, f); err != nil {
				return f, "", err
			}
			cambiados := []string{}
			if f.Direccion != antes.Direccion {
				cambiados = append(cambiados, "direccion")
			}
			if !igualPtr(f.Lat, antes.Lat) || !igualPtr(f.Lon, antes.Lon) {
				cambiados = append(cambiados, "coordenadas")
			}
			if !igualPtr(f.Referencia, antes.Referencia) {
				cambiados = append(cambiados, "referencia")
			}
			if !igualPtr(f.ContactoNombre, antes.ContactoNombre) || !igualPtr(f.ContactoTelefono, antes.ContactoTelefono) {
				cambiados = append(cambiados, "contacto")
			}
			return f, "", t.Emitir("orden.datos_argos_aplicados", map[string]any{
				"orden_id": f.ID, "numero": f.Numero, "campos": grupos, "cambiados": cambiados, "campos_argos": nuncaNil(antes.DatosArgosCampos),
				"campos_restantes": nuncaNil(f.DatosArgosCampos),
			})
		})
}

func (m *Modulo) nuevoDescartarDatosArgos() *auditoria.Caso[EntradaDescartarDatosArgos, Orden] {
	return accionOrden("ordenes.descartar_datos_argos", nucleo.PermOrdenesDescartarDatosArgos, sinLote[EntradaDescartarDatosArgos],
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaDescartarDatosArgos) (campobd.CampoOrden, string, error) {
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesDescartarDatosArgos, &in.Version, nil)
			if err != nil {
				return antes, "", err
			}
			if err := exigirAvisoAbierto(antes, "descartar el aviso de Argos de", in.AvisoEn); err != nil {
				return antes, "", err
			}
			var motivo *string
			if in.Motivo != nil {
				v, err := texto("motivo", *in.Motivo, 0, 500)
				if err != nil {
					return antes, "", err
				}
				motivo = noVacio(v)
			}
			f, err := q.DescartarDatosArgosOrden(ctx, campobd.DescartarDatosArgosOrdenParams{ID: antes.ID, Version: antes.Version})
			if err != nil {
				return f, "", nucleo.VerificarVersion(err, "orden", antes.ID)
			}
			t.RegistrarCambios("orden", f.ID, antes, f)
			if motivo != nil {
				t.Anotar(map[string]any{"motivo": *motivo})
			}
			return f, "", t.Emitir("orden.datos_argos_descartados", map[string]any{
				"orden_id": f.ID, "numero": f.Numero, "campos_argos": nuncaNil(antes.DatosArgosCampos), "motivo": motivo,
			})
		})
}

func igualPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func nuncaNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
