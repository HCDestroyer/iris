package comercial

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// Orígenes de un prospecto.
var origenes = []string{"panel", "web", "referido", "argos", "otro"}

// EntradaListarProspectos filtra el embudo.
type EntradaListarProspectos struct {
	nucleo.ParamsPagina
	Estado         string `query:"estado" maxLength:"200" doc:"Uno o varios separados por coma: nuevo, contactado, factibilidad, cotizado, contrato, ganado, perdido"`
	Origen         string `query:"origen" enum:"panel,web,referido,argos,otro,"`
	VendedorID     int64  `query:"vendedorId" minimum:"0"`
	SinVendedor    bool   `query:"sinVendedor"`
	ZonaID         int64  `query:"zonaId" minimum:"0"`
	ConCoordenadas bool   `query:"conCoordenadas" doc:"Solo los que tienen lat/lon (mapa de prospectos)"`
	Q              string `query:"q" maxLength:"100" doc:"Busca en número, nombre, teléfono, correo y dirección"`
}

// EntradaVerProspecto: GET /prospectos/{id} (y sus listas).
type EntradaVerProspecto struct {
	ID int64 `path:"id" minimum:"1"`
}

// EntradaCrearProspecto registra un prospecto.
type EntradaCrearProspecto struct {
	Origen            string   `json:"origen,omitempty" enum:"panel,web,referido,argos,otro" doc:"Por defecto panel (desde el panel) u otro; argos solo la integración de Argos o el sistema"`
	OrigenDetalle     *string  `json:"origenDetalle,omitempty" maxLength:"200" doc:"Quién lo refirió, campaña…"`
	Nombre            string   `json:"nombre" minLength:"2" maxLength:"200"`
	Documento         *string  `json:"documento,omitempty" maxLength:"40" doc:"NIT o DPI"`
	Telefono          *string  `json:"telefono,omitempty" maxLength:"30" doc:"Hasta 30 (lo que acepta Argos)"`
	Correo            *string  `json:"correo,omitempty" maxLength:"200" format:"email"`
	Direccion         string   `json:"direccion" minLength:"1" maxLength:"300"`
	Lat               *float64 `json:"lat,omitempty" minimum:"-90" maximum:"90"`
	Lon               *float64 `json:"lon,omitempty" minimum:"-180" maximum:"180"`
	ZonaID            *int64   `json:"zonaId,omitempty" minimum:"1" doc:"Zona (Argos o local). Con alcance por zona, obligatoria y tuya"`
	PlanInteresID     *int64   `json:"planInteresId,omitempty" minimum:"1" doc:"Plan de Argos que le interesa"`
	PlanInteresNombre *string  `json:"planInteresNombre,omitempty" maxLength:"200"`
	VendedorID        *int64   `json:"vendedorId,omitempty" minimum:"1" doc:"Responsable de ventas; exige prospectos.asignar"`
	Notas             string   `json:"notas,omitempty" maxLength:"4000"`
	EmpresaID         *int64   `json:"empresaId,omitempty" minimum:"1" doc:"Empresa en Argos: la activa (otra: 422 empresa_no_coincide); solo el sistema la elige aquí"`

	// ClaveIdempotencia es la Idempotency-Key de la petición (la pone el handler).
	ClaveIdempotencia string `json:"-"`
}

// EntradaEditarProspecto edita los datos (no el estado ni el vendedor).
type EntradaEditarProspecto struct {
	ID                int64    `json:"-"`
	Version           int32    `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	OrigenDetalle     *string  `json:"origenDetalle,omitempty" maxLength:"200"`
	Nombre            *string  `json:"nombre,omitempty" minLength:"2" maxLength:"200"`
	Documento         *string  `json:"documento,omitempty" maxLength:"40"`
	Telefono          *string  `json:"telefono,omitempty" maxLength:"30" doc:"Hasta 30 (lo que acepta Argos)"`
	Correo            *string  `json:"correo,omitempty" maxLength:"200" format:"email"`
	Direccion         *string  `json:"direccion,omitempty" minLength:"1" maxLength:"300"`
	Lat               *float64 `json:"lat,omitempty" minimum:"-90" maximum:"90" doc:"Cambiar las coordenadas obliga a evaluar de nuevo la factibilidad antes de ganar"`
	Lon               *float64 `json:"lon,omitempty" minimum:"-180" maximum:"180"`
	ZonaID            *int64   `json:"zonaId,omitempty" minimum:"1"`
	PlanInteresID     *int64   `json:"planInteresId,omitempty" minimum:"1"`
	PlanInteresNombre *string  `json:"planInteresNombre,omitempty" maxLength:"200"`
	Notas             *string  `json:"notas,omitempty" maxLength:"4000"`
}

// EntradaAsignarProspecto asigna (o quita) el vendedor.
type EntradaAsignarProspecto struct {
	ID         int64  `json:"-"`
	Version    int32  `json:"version" minimum:"1"`
	VendedorID *int64 `json:"vendedorId,omitempty" minimum:"1" doc:"Ausente = sin vendedor"`
}

// EntradaTransicion: contactar o reabrir.
type EntradaTransicion struct {
	ID      int64   `json:"-"`
	Version int32   `json:"version" minimum:"1"`
	Motivo  *string `json:"motivo,omitempty" maxLength:"1000"`
}

// EntradaPerder marca el prospecto como perdido.
type EntradaPerder struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1"`
	Motivo  string `json:"motivo" minLength:"1" maxLength:"1000" doc:"Por qué se perdió (precio, sin cobertura, competencia…)"`
}

func (m *Modulo) nuevoListarProspectos() *auditoria.Caso[EntradaListarProspectos, nucleo.Pagina[Prospecto]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarProspectos, nucleo.Pagina[Prospecto]]{
		Accion: "prospectos.ver", Permiso: nucleo.PermProspectosVer, AuditarExito: soloLecturas[nucleo.Pagina[Prospecto]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarProspectos) (nucleo.Pagina[Prospecto], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Prospecto]{}, err
			}
			estados := []string{}
			for _, e := range strings.Split(in.Estado, ",") {
				if e = strings.TrimSpace(e); e == "" {
					continue
				}
				if !slices.Contains([]string{EstadoNuevo, EstadoContactado, EstadoFactibilidad, EstadoCotizado, EstadoContrato, EstadoGanado, EstadoPerdido}, e) {
					return nucleo.Pagina[Prospecto]{}, campoInvalido("estado", "Estado desconocido: "+e, "enum")
				}
				estados = append(estados, e)
			}
			todas, empresa := filtroEmpresa(ctx)
			a := alcanceDe(ctx, string(nucleo.PermProspectosVer))
			p := comercialbd.ListarProspectosParams{
				DespuesDe: despues, Todas: todas, EmpresaID: empresa, Global: a.global, Zonas: a.zonas, Propio: a.propio,
				Estados: estados, SinVendedor: in.SinVendedor, ConCoordenadas: in.ConCoordenadas, Limite: int32(in.Limite() + 1), //nolint:gosec // acotado a 201
			}
			if in.Origen != "" {
				p.Origen = &in.Origen
			}
			if in.VendedorID > 0 {
				p.VendedorID = &in.VendedorID
			}
			if in.ZonaID > 0 {
				p.ZonaID = &in.ZonaID
			}
			if q := strings.TrimSpace(strings.NewReplacer("%", "", "_", "", "\\", "").Replace(in.Q)); q != "" {
				p.Q = &q
			}
			filas, err := comercialbd.New(t).ListarProspectos(ctx, p)
			if err != nil {
				return nucleo.Pagina[Prospecto]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(), aProspecto, func(f comercialbd.ComercialProspecto) []any { return []any{f.ID} }), nil
		},
	})
}

// validarZona: la zona existe en el catálogo de agenda, activa y de la empresa.
func validarZona(ctx context.Context, t *auditoria.Tx, zona *int64, empresa *int64) error {
	if zona == nil {
		return nil
	}
	return agenda.ValidarZona(ctx, t, "zonaId", *zona, empresa)
}

// validarVendedor: el vendedor es una persona activa que opera en la empresa del prospecto y
// puede verlo con su alcance de prospectos.ver (global o la zona del prospecto).
func validarVendedor(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, usuarioID int64, f comercialbd.ComercialProspecto) error {
	invalido := func(detalle string) error {
		return campoInvalido("vendedorId", detalle, "usuario")
	}
	u, err := q.UsuarioAsignable(ctx, usuarioID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !u.Activo) {
		return invalido("El usuario no existe o está inactivo")
	}
	if err != nil {
		return err
	}
	if u.Tipo != "persona" {
		return invalido("Solo se asigna a personas, no a integraciones")
	}
	alcances, err := q.AlcancesDePermisoDeUsuario(ctx, comercialbd.AlcancesDePermisoDeUsuarioParams{
		UsuarioID: usuarioID, Permiso: string(nucleo.PermProspectosVer),
	})
	if err != nil {
		return err
	}
	cubre := false
	for _, a := range alcances {
		if (a.ZonaID == nil && a.CuadrillaID == nil) || (a.ZonaID != nil && mismoID(a.ZonaID, f.ZonaID)) {
			cubre = true
			break
		}
	}
	if !cubre {
		return invalido("El usuario no puede ver prospectos de esa zona (prospectos.ver)")
	}
	enLaEmpresa, err := identidad.UsuarioEnEmpresa(ctx, t, usuarioID, deref(f.EmpresaID))
	if err != nil {
		return err
	}
	if !enLaEmpresa {
		return nucleo.ReglaNegocio("vendedor_fuera_de_empresa", "El usuario no opera en la empresa del prospecto")
	}
	return nil
}

// origenDe: el origen pedido o el del canal. argos solo la integración de Argos o el sistema.
func origenDe(ctx context.Context, pedido string) (string, error) {
	a := contexto.ActorDe(ctx)
	if pedido == "" {
		if contexto.CanalDe(ctx) == contexto.CanalPanel {
			return "panel", nil
		}
		return "otro", nil
	}
	if !slices.Contains(origenes, pedido) {
		return "", campoInvalido("origen", "Origen desconocido", "enum")
	}
	if pedido == "argos" && a.Tipo != contexto.ActorSistema && a.Tipo != contexto.ActorIntegracion {
		return "", nucleo.ReglaNegocio("origen_no_permitido", "El origen argos lo usa solo la integración con Argos")
	}
	return pedido, nil
}

func (m *Modulo) nuevoCrearProspecto() *auditoria.Caso[EntradaCrearProspecto, Prospecto] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearProspecto, Prospecto]{
		Accion: "prospectos.crear", Permiso: nucleo.PermProspectosCrear,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearProspecto) (Prospecto, error) {
			q := comercialbd.New(t)
			idem := conIdempotencia(ctx, in.ClaveIdempotencia)
			if idem != nil {
				previo, err := q.ProspectoPorIdempotencia(ctx, idem)
				if err == nil {
					if err := verificarAcceso(ctx, string(nucleo.PermProspectosVer), previo); err != nil {
						return Prospecto{}, err
					}
					t.FijarEntidad("prospecto", previo.ID)
					t.FijarEmpresa(deref(previo.EmpresaID))
					t.Anotar(map[string]any{"repetida": true})
					return aProspecto(previo), nil
				}
				if !errors.Is(err, pgx.ErrNoRows) {
					return Prospecto{}, err
				}
			}
			origen, err := origenDe(ctx, in.Origen)
			if err != nil {
				return Prospecto{}, err
			}
			d := comercialbd.CrearProspectoParams{Origen: origen, Lat: in.Lat, Lon: in.Lon, ZonaID: in.ZonaID,
				PlanInteresID: in.PlanInteresID, VendedorID: in.VendedorID, CreadoPor: actorUsuario(t), Idempotencia: idem}
			if d.Nombre, err = texto("nombre", in.Nombre, 2, 200); err != nil {
				return Prospecto{}, err
			}
			if d.Direccion, err = texto("direccion", in.Direccion, 1, 300); err != nil {
				return Prospecto{}, err
			}
			if d.Notas, err = texto("notas", in.Notas, 0, 4000); err != nil {
				return Prospecto{}, err
			}
			for _, c := range []struct {
				campo   string
				valor   *string
				destino **string
				max     int
			}{
				{"origenDetalle", in.OrigenDetalle, &d.OrigenDetalle, 200}, {"documento", in.Documento, &d.Documento, 40},
				{"telefono", in.Telefono, &d.Telefono, 30}, {"correo", in.Correo, &d.Correo, 200},
				{"planInteresNombre", in.PlanInteresNombre, &d.PlanInteresNombre, 200},
			} {
				if *c.destino, err = textoOpcional(c.campo, c.valor, c.max); err != nil {
					return Prospecto{}, err
				}
			}
			if err := validarCoordenadas(in.Lat, in.Lon); err != nil {
				return Prospecto{}, err
			}
			if d.EmpresaID, err = empresaDeAlta(ctx, q, in.EmpresaID); err != nil {
				return Prospecto{}, err
			}
			t.FijarEmpresa(deref(d.EmpresaID))
			if err := validarZona(ctx, t, in.ZonaID, d.EmpresaID); err != nil {
				return Prospecto{}, err
			}
			// Con alcance limitado, el prospecto debe caer en una zona propia.
			if a := alcanceDe(ctx, string(nucleo.PermProspectosCrear)); !a.global && (in.ZonaID == nil || !slices.Contains(a.zonas, *in.ZonaID)) {
				return Prospecto{}, fueraDeAlcance("Tu permiso prospectos.crear se limita a otras zonas; indica una zona tuya")
			}
			if in.VendedorID != nil {
				if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermProspectosAsignar)) {
					return Prospecto{}, nucleo.Prohibido("Asignar el vendedor al crear exige prospectos.asignar")
				}
				if err := validarVendedor(ctx, t, q, *in.VendedorID, comercialbd.ComercialProspecto{EmpresaID: d.EmpresaID, ZonaID: in.ZonaID}); err != nil {
					return Prospecto{}, err
				}
			}
			f, err := q.CrearProspecto(ctx, d)
			if err != nil {
				return Prospecto{}, err
			}
			t.FijarEntidad("prospecto", f.ID)
			t.RegistrarCambios("prospecto", f.ID, nil, f)
			if err := historial(ctx, t, q, f.ID, "creado", nil, &f.Estado, nil, map[string]any{"origen": f.Origen}); err != nil {
				return Prospecto{}, err
			}
			return aProspecto(f), t.Emitir("prospecto.creado", map[string]any{
				"prospecto_id": f.ID, "numero": f.Numero, "empresa_id": f.EmpresaID, "origen": f.Origen,
				"zona_id": f.ZonaID, "vendedor_id": f.VendedorID,
			})
		},
	})
}

func (m *Modulo) nuevoEditarProspecto() *auditoria.Caso[EntradaEditarProspecto, Prospecto] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEditarProspecto, Prospecto]{
		Accion: "prospectos.editar", Permiso: nucleo.PermProspectosEditar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEditarProspecto) (Prospecto, error) {
			q := comercialbd.New(t)
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermProspectosEditar, &in.Version)
			if err != nil {
				return Prospecto{}, err
			}
			if err := exigirEstado(antes, "editar", EstadoNuevo, EstadoContactado, EstadoFactibilidad, EstadoCotizado); err != nil {
				return Prospecto{}, err
			}
			d := antes
			if in.Nombre != nil {
				if d.Nombre, err = texto("nombre", *in.Nombre, 2, 200); err != nil {
					return Prospecto{}, err
				}
			}
			if in.Direccion != nil {
				if d.Direccion, err = texto("direccion", *in.Direccion, 1, 300); err != nil {
					return Prospecto{}, err
				}
			}
			if in.Notas != nil {
				if d.Notas, err = texto("notas", *in.Notas, 0, 4000); err != nil {
					return Prospecto{}, err
				}
			}
			for _, c := range []struct {
				campo   string
				valor   *string
				destino **string
				max     int
			}{
				{"origenDetalle", in.OrigenDetalle, &d.OrigenDetalle, 200}, {"documento", in.Documento, &d.Documento, 40},
				{"telefono", in.Telefono, &d.Telefono, 30}, {"correo", in.Correo, &d.Correo, 200},
				{"planInteresNombre", in.PlanInteresNombre, &d.PlanInteresNombre, 200},
			} {
				if c.valor == nil {
					continue
				}
				if *c.destino, err = textoOpcional(c.campo, c.valor, c.max); err != nil {
					return Prospecto{}, err
				}
			}
			if in.Lat != nil || in.Lon != nil {
				if err := validarCoordenadas(in.Lat, in.Lon); err != nil {
					return Prospecto{}, err
				}
				d.Lat, d.Lon = in.Lat, in.Lon
			}
			if in.PlanInteresID != nil {
				d.PlanInteresID = in.PlanInteresID
			}
			if in.ZonaID != nil && !mismoID(in.ZonaID, antes.ZonaID) {
				if err := validarZona(ctx, t, in.ZonaID, antes.EmpresaID); err != nil {
					return Prospecto{}, err
				}
				// Con alcance limitado no se saca un prospecto hacia una zona ajena.
				if a := alcanceDe(ctx, string(nucleo.PermProspectosEditar)); !a.global && !slices.Contains(a.zonas, *in.ZonaID) {
					return Prospecto{}, fueraDeAlcance("Tu permiso prospectos.editar no cubre esa zona")
				}
				d.ZonaID = in.ZonaID
			}
			cambiados := camposCambiados(antes, d)
			if len(cambiados) == 0 {
				return Prospecto{}, nucleo.ReglaNegocio("sin_cambios", "El prospecto ya tiene esos datos")
			}
			f, err := guardar(ctx, t, q, antes, d)
			if err != nil {
				return Prospecto{}, err
			}
			return aProspecto(f), t.Emitir("prospecto.actualizado", map[string]any{"prospecto_id": f.ID, "campos": cambiados})
		},
	})
}

// camposCambiados: nombres (snake_case) de los datos editables que cambiaron.
func camposCambiados(a, b comercialbd.ComercialProspecto) []string {
	ja, _ := json.Marshal(aProspecto(a))
	jb, _ := json.Marshal(aProspecto(b))
	var ma, mb map[string]any
	_ = json.Unmarshal(ja, &ma)
	_ = json.Unmarshal(jb, &mb)
	salida := []string{}
	for _, c := range []string{"origenDetalle", "nombre", "documento", "telefono", "correo", "direccion", "lat", "lon", "zonaId", "planInteresId", "planInteresNombre", "notas"} {
		x, _ := json.Marshal(ma[c])
		y, _ := json.Marshal(mb[c])
		if string(x) != string(y) {
			salida = append(salida, auditoria.SnakeCase(c))
		}
	}
	return salida
}

func (m *Modulo) nuevoAsignarProspecto() *auditoria.Caso[EntradaAsignarProspecto, Prospecto] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAsignarProspecto, Prospecto]{
		Accion: "prospectos.asignar", Permiso: nucleo.PermProspectosAsignar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAsignarProspecto) (Prospecto, error) {
			q := comercialbd.New(t)
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermProspectosAsignar, &in.Version)
			if err != nil {
				return Prospecto{}, err
			}
			if err := exigirEstado(antes, "asignar", EstadoNuevo, EstadoContactado, EstadoFactibilidad, EstadoCotizado, EstadoContrato); err != nil {
				return Prospecto{}, err
			}
			if mismoID(antes.VendedorID, in.VendedorID) {
				return Prospecto{}, nucleo.ReglaNegocio("sin_cambios", "El prospecto ya tiene ese vendedor")
			}
			if in.VendedorID != nil {
				if err := validarVendedor(ctx, t, q, *in.VendedorID, antes); err != nil {
					return Prospecto{}, err
				}
			}
			d := antes
			d.VendedorID = in.VendedorID
			f, err := guardar(ctx, t, q, antes, d)
			if err != nil {
				return Prospecto{}, err
			}
			if err := historial(ctx, t, q, f.ID, "asignado", nil, nil, nil, map[string]any{
				"vendedor_id": f.VendedorID, "vendedor_anterior_id": antes.VendedorID,
			}); err != nil {
				return Prospecto{}, err
			}
			return aProspecto(f), t.Emitir("prospecto.asignado", map[string]any{
				"prospecto_id": f.ID, "vendedor_id": f.VendedorID, "vendedor_anterior_id": antes.VendedorID,
			})
		},
	})
}

// transicion arma un caso que solo cambia el estado (contactar, reabrir).
func (m *Modulo) transicion(accion string, permiso nucleo.Permiso, desde []string, a string,
	cambiar func(*comercialbd.ComercialProspecto)) *auditoria.Caso[EntradaTransicion, Prospecto] {
	return auditoria.Nuevo(auditoria.Caso[EntradaTransicion, Prospecto]{
		Accion: accion, Permiso: permiso,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTransicion) (Prospecto, error) {
			q := comercialbd.New(t)
			antes, err := tomar(ctx, t, q, in.ID, permiso, &in.Version)
			if err != nil {
				return Prospecto{}, err
			}
			if err := exigirEstado(antes, strings.TrimPrefix(accion, "prospectos."), desde...); err != nil {
				return Prospecto{}, err
			}
			motivo, err := textoOpcional("motivo", in.Motivo, 1000)
			if err != nil {
				return Prospecto{}, err
			}
			d := antes
			d.Estado = a
			if cambiar != nil {
				cambiar(&d)
			}
			f, err := guardar(ctx, t, q, antes, d)
			if err != nil {
				return Prospecto{}, err
			}
			return aProspecto(f), cambioDeEstado(ctx, t, q, antes, f, motivo)
		},
	})
}

func (m *Modulo) nuevoContactar() *auditoria.Caso[EntradaTransicion, Prospecto] {
	return m.transicion("prospectos.contactar", nucleo.PermProspectosContactar, []string{EstadoNuevo}, EstadoContactado, nil)
}

func (m *Modulo) nuevoReabrir() *auditoria.Caso[EntradaTransicion, Prospecto] {
	return m.transicion("prospectos.reabrir", nucleo.PermProspectosReabrir, []string{EstadoPerdido}, EstadoContactado,
		func(d *comercialbd.ComercialProspecto) { d.MotivoPerdida, d.PerdidoEn = nil, nil })
}

func (m *Modulo) nuevoPerder() *auditoria.Caso[EntradaPerder, Prospecto] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPerder, Prospecto]{
		Accion: "prospectos.perder", Permiso: nucleo.PermProspectosPerder,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPerder) (Prospecto, error) {
			q := comercialbd.New(t)
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermProspectosPerder, &in.Version)
			if err != nil {
				return Prospecto{}, err
			}
			if err := exigirEstado(antes, "perder", EstadoNuevo, EstadoContactado, EstadoFactibilidad, EstadoCotizado, EstadoContrato); err != nil {
				return Prospecto{}, err
			}
			motivo, err := texto("motivo", in.Motivo, 1, 1000)
			if err != nil {
				return Prospecto{}, err
			}
			// El contrato vivo (borrador o firmado) se anula: el cliente desistió.
			if err := anularContratoVivo(ctx, t, q, antes.ID, "Prospecto perdido: "+motivo); err != nil {
				return Prospecto{}, err
			}
			d := antes
			d.Estado, d.MotivoPerdida, d.PerdidoEn = EstadoPerdido, &motivo, ptr(m.ahora())
			f, err := guardar(ctx, t, q, antes, d)
			if err != nil {
				return Prospecto{}, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, &motivo); err != nil {
				return Prospecto{}, err
			}
			return aProspecto(f), t.Emitir("prospecto.perdido", map[string]any{
				"prospecto_id": f.ID, "empresa_id": f.EmpresaID, "motivo": motivo, "de": antes.Estado,
			})
		},
	})
}

func (m *Modulo) nuevoVerProspecto() *auditoria.Caso[EntradaVerProspecto, ProspectoDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerProspecto, ProspectoDetalle]{
		Accion: "prospectos.ver", Permiso: nucleo.PermProspectosVer, AuditarExito: soloLecturas[ProspectoDetalle],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerProspecto) (ProspectoDetalle, error) {
			q := comercialbd.New(t)
			f, err := leer(ctx, t, q, in.ID)
			if err != nil {
				return ProspectoDetalle{}, err
			}
			return m.detalle(ctx, t, q, f)
		},
	})
}

// detalle arma el prospecto con su embudo.
func (m *Modulo) detalle(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, f comercialbd.ComercialProspecto) (ProspectoDetalle, error) {
	d := ProspectoDetalle{Prospecto: aProspecto(f), Pendientes: []PendienteArgos{}}
	if fa, err := q.UltimaFactibilidad(ctx, f.ID); err == nil {
		v := aFactibilidad(fa)
		d.Factibilidad = &v
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return d, err
	}
	if c, err := q.CotizacionViva(ctx, f.ID); err == nil {
		v, err := m.aCotizacion(ctx, q, c)
		if err != nil {
			return d, err
		}
		d.Cotizacion = &v
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return d, err
	}
	if c, err := q.ContratoVivoDeProspecto(ctx, f.ID); err == nil {
		v, err := aContrato(ctx, q, c)
		if err != nil {
			return d, err
		}
		d.Contrato = &v
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return d, err
	}
	ps, err := pendientes.DeEntidad(ctx, t, EntidadPendientes, idTexto(f.ID))
	if err != nil {
		return d, err
	}
	for _, p := range ps {
		d.Pendientes = append(d.Pendientes, PendienteArgos{ID: p.ID, Operacion: p.Operacion, IdempotencyKey: p.IdempotencyKey,
			Estado: p.Estado, Intentos: p.Intentos, UltimoError: p.UltimoError, Version: p.Version})
	}
	if d.Alta != nil && f.OrdenID != nil {
		if o, err := q.ResumenOrden(ctx, *f.OrdenID); err == nil {
			d.Alta.OrdenNumero = &o.Numero
		}
	}
	d.Acciones = acciones(m, d)
	return d, nil
}

// acciones: qué corresponde ahora (guía para el panel).
func acciones(m *Modulo, d ProspectoDetalle) AccionesDisponible {
	e := d.Estado
	a := AccionesDisponible{
		Contactar: e == EstadoNuevo,
		Evaluar:   slices.Contains([]string{EstadoNuevo, EstadoContactado, EstadoFactibilidad, EstadoCotizado}, e),
		Perder:    !slices.Contains([]string{EstadoGanado, EstadoPerdido}, e),
		Reabrir:   e == EstadoPerdido,
	}
	factible := d.Factibilidad != nil && (d.Factibilidad.Resultado == "viable" || d.Factibilidad.Resultado == "requiere_obra")
	a.Cotizar = factible && slices.Contains([]string{EstadoFactibilidad, EstadoCotizado, EstadoContrato}, e) &&
		(d.Contrato == nil || d.Contrato.Estado == "borrador")
	a.Aceptar = e == EstadoCotizado && d.Cotizacion != nil && d.Cotizacion.Estado == "vigente" && !d.Cotizacion.Vencida
	a.Contratar = e == EstadoCotizado && d.Cotizacion != nil && d.Cotizacion.Estado == "aceptada"
	a.Firmar = e == EstadoContrato && d.Contrato != nil && d.Contrato.Estado == "borrador"
	a.Ganar = e == EstadoContrato && d.Contrato != nil && d.Contrato.Estado == "firmado" && factible
	a.ConfirmaObra = a.Ganar && d.Factibilidad.Resultado == "requiere_obra"
	_ = m
	return a
}

func (m *Modulo) nuevoEventosProspecto() *auditoria.Caso[EntradaVerProspecto, []EventoProspecto] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerProspecto, []EventoProspecto]{
		Accion: "prospectos.ver", Permiso: nucleo.PermProspectosVer, AuditarExito: soloLecturas[[]EventoProspecto],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerProspecto) ([]EventoProspecto, error) {
			q := comercialbd.New(t)
			if _, err := leer(ctx, t, q, in.ID); err != nil {
				return nil, err
			}
			filas, err := q.EventosDeProspecto(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			salida := make([]EventoProspecto, 0, len(filas))
			for _, f := range filas {
				e := EventoProspecto{ID: f.ID, Tipo: f.Tipo, De: f.De, A: f.A, Motivo: f.Motivo, ActorTipo: f.ActorTipo,
					ActorID: f.ActorID, CreadoEn: f.CreatedAt, Datos: map[string]any{}}
				_ = json.Unmarshal(f.Datos, &e.Datos)
				salida = append(salida, e)
			}
			return salida, nil
		},
	})
}
