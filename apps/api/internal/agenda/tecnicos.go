package agenda

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// EntradaListarTecnicos filtra las fichas visibles.
type EntradaListarTecnicos struct {
	nucleo.ParamsPagina
	CuadrillaID int64  `query:"cuadrillaId" minimum:"0" doc:"Cuadrilla"`
	ZonaID      int64  `query:"zonaId" minimum:"0" doc:"Técnicos que atienden la zona (por su cuadrilla)"`
	UsuarioID   int64  `query:"usuarioId" minimum:"0" doc:"La ficha de ese usuario"`
	Activo      string `query:"activo" enum:"true,false," doc:"Solo activos (true) o inactivos (false)"`
	Habilidad   string `query:"habilidad" maxLength:"40"`
	Q           string `query:"q" maxLength:"100" doc:"Nombre o correo"`
}

// EntradaVerTecnico: GET /tecnicos/{id}.
type EntradaVerTecnico struct {
	ID int64 `path:"id" minimum:"1"`
}

// EntradaCrearTecnico da de alta la ficha de un usuario.
type EntradaCrearTecnico struct {
	UsuarioID   int64    `json:"usuarioId" minimum:"1" doc:"Usuario persona con ordenes.ver_propias u ordenes.ver"`
	CuadrillaID *int64   `json:"cuadrillaId,omitempty" minimum:"1"`
	BodegaID    *int64   `json:"bodegaId,omitempty" minimum:"1" doc:"Bodega del técnico en Argos"`
	Habilidades []string `json:"habilidades,omitempty" maxItems:"30" doc:"Códigos libres (fibra, inalambrico, altura…)"`
	BaseLat     *float64 `json:"baseLat,omitempty" minimum:"-90" maximum:"90"`
	BaseLon     *float64 `json:"baseLon,omitempty" minimum:"-180" maximum:"180"`
	Telefono    string   `json:"telefono,omitempty" maxLength:"30" doc:"Teléfono de contacto: dígitos, espacios, +, -, paréntesis y puntos"`
}

// EntradaEditarTecnico reemplaza los datos de la ficha (lo ausente queda vacío).
type EntradaEditarTecnico struct {
	ID          int64    `json:"-"`
	Version     int32    `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	CuadrillaID *int64   `json:"cuadrillaId,omitempty" minimum:"1"`
	BodegaID    *int64   `json:"bodegaId,omitempty" minimum:"1"`
	Habilidades []string `json:"habilidades,omitempty" maxItems:"30"`
	BaseLat     *float64 `json:"baseLat,omitempty" minimum:"-90" maximum:"90"`
	BaseLon     *float64 `json:"baseLon,omitempty" minimum:"-180" maximum:"180"`
	Telefono    string   `json:"telefono,omitempty" maxLength:"30" doc:"Teléfono de contacto (vacío = sin teléfono)"`
}

// EntradaFijarTurnos reemplaza los turnos semanales.
type EntradaFijarTurnos struct {
	ID      int64   `json:"-"`
	Version int32   `json:"version" minimum:"1" doc:"Versión leída de la ficha; si cambió, 409"`
	Turnos  []Turno `json:"turnos" maxItems:"28" doc:"Vacío = sin turnos (no se le puede programar)"`
}

func tecnicoNoEncontrado() *nucleo.Problema { return nucleo.NoEncontrado("El técnico no existe") }

// codigosDeTipos: id → código del tipo de orden (y al revés).
type tiposOrden struct {
	codigo   map[int64]string
	id       map[string]int64
	duracion map[int64]int32
}

func codigosDeTipos(ctx context.Context, q *agendabd.Queries) (tiposOrden, error) {
	fs, err := q.TiposOrden(ctx)
	if err != nil {
		return tiposOrden{}, err
	}
	t := tiposOrden{codigo: map[int64]string{}, id: map[string]int64{}, duracion: map[int64]int32{}}
	for _, f := range fs {
		t.codigo[f.ID], t.id[f.Codigo], t.duracion[f.ID] = f.Codigo, f.ID, f.DuracionMin
	}
	return t, nil
}

func aCapacidad(f agendabd.AgendaCapacidad, tipos tiposOrden) Capacidad {
	c := Capacidad{Cupos: f.Cupos, Minutos: f.Minutos}
	if f.TipoID != nil {
		c.Tipo = ptr(tipos.codigo[*f.TipoID])
	}
	return c
}

type lineaCapacidad struct {
	tipo           *int64
	cupos, minutos *int32
}

// validarCapacidad: una línea por tipo (o todos), con cupos o minutos.
func validarCapacidad(ctx context.Context, q *agendabd.Queries, cs []Capacidad) ([]lineaCapacidad, error) {
	tipos, err := codigosDeTipos(ctx, q)
	if err != nil {
		return nil, err
	}
	vistos := map[string]bool{}
	salida := make([]lineaCapacidad, 0, len(cs))
	for i, c := range cs {
		campo := fmt.Sprintf("capacidad[%d]", i)
		clave := ""
		l := lineaCapacidad{cupos: c.Cupos, minutos: c.Minutos}
		if c.Tipo != nil {
			id, ok := tipos.id[*c.Tipo]
			if !ok {
				return nil, campoInvalido(campo+".tipo", "Tipo de orden desconocido", "enum")
			}
			l.tipo, clave = &id, *c.Tipo
		}
		if vistos[clave] {
			return nil, campoInvalido(campo+".tipo", "Una sola línea por tipo", "unico")
		}
		vistos[clave] = true
		if c.Cupos == nil && c.Minutos == nil {
			return nil, campoInvalido(campo, "Indica cupos, minutos o ambos", "required")
		}
		if (c.Cupos != nil && *c.Cupos < 1) || (c.Minutos != nil && *c.Minutos < 1) {
			return nil, campoInvalido(campo, "Cupos y minutos deben ser mayores que cero", "minimum")
		}
		salida = append(salida, l)
	}
	return salida, nil
}

// detallesDe arma las fichas con usuario, turnos, capacidad y ausencias vigentes o futuras.
func (m *Modulo) detallesDe(ctx context.Context, q *agendabd.Queries, fs []agendabd.AgendaTecnico) ([]TecnicoDetalle, error) {
	ids, usuarios := make([]int64, 0, len(fs)), make([]int64, 0, len(fs))
	for _, f := range fs {
		ids, usuarios = append(ids, f.ID), append(usuarios, f.UsuarioID)
	}
	turnos, err := q.TurnosDeTecnicos(ctx, ids)
	if err != nil {
		return nil, err
	}
	caps, err := q.CapacidadesDeTecnicos(ctx, agendabd.CapacidadesDeTecnicosParams{Tecnicos: ids, Cuadrillas: []int64{}})
	if err != nil {
		return nil, err
	}
	ahora := m.ahora()
	aus, err := q.AusenciasEnRango(ctx, agendabd.AusenciasEnRangoParams{Tecnicos: ids, Desde: ahora, Hasta: ahora.AddDate(10, 0, 0)})
	if err != nil {
		return nil, err
	}
	nombres, err := q.UsuariosPorIDs(ctx, usuarios)
	if err != nil {
		return nil, err
	}
	tipos, err := codigosDeTipos(ctx, q)
	if err != nil {
		return nil, err
	}
	salida := make([]TecnicoDetalle, 0, len(fs))
	for _, f := range fs {
		d := TecnicoDetalle{Tecnico: aTecnico(f, "", ""), Turnos: []Turno{}, Capacidad: []Capacidad{}, Ausencias: []Ausencia{}}
		for _, u := range nombres {
			if u.ID == f.UsuarioID {
				d.Nombre = u.Nombre
			}
		}
		for _, tu := range turnos {
			if tu.TecnicoID == f.ID {
				d.Turnos = append(d.Turnos, Turno{Dia: tu.Dia, Inicio: hhmm(tu.Inicio), Fin: hhmm(tu.Fin)})
			}
		}
		for _, c := range caps {
			if c.TecnicoID != nil && *c.TecnicoID == f.ID {
				d.Capacidad = append(d.Capacidad, aCapacidad(c, tipos))
			}
		}
		propia, err := fichaDeLaEmpresa(ctx, q, f)
		if err != nil {
			return nil, err
		}
		for _, a := range aus {
			if a.TecnicoID == f.ID {
				au := aAusencia(a)
				if !propia {
					au.Detalle = "" // el detalle solo lo ve la empresa de la ficha
				}
				d.Ausencias = append(d.Ausencias, au)
			}
		}
		salida = append(salida, d)
	}
	return salida, nil
}

func (m *Modulo) detalleDe(ctx context.Context, q *agendabd.Queries, f agendabd.AgendaTecnico) (TecnicoDetalle, error) {
	ds, err := m.detallesDe(ctx, q, []agendabd.AgendaTecnico{f})
	if err != nil {
		return TecnicoDetalle{}, err
	}
	u, err := q.UsuarioParaTecnico(ctx, f.UsuarioID)
	if err != nil {
		return TecnicoDetalle{}, err
	}
	ds[0].Correo, ds[0].Activo = u.Correo, f.Activo && u.Activo
	return ds[0], nil
}

func aTecnico(f agendabd.AgendaTecnico, nombre, correo string) Tecnico {
	h := f.Habilidades
	if h == nil {
		h = []string{}
	}
	return Tecnico{ID: f.ID, UsuarioID: f.UsuarioID, Nombre: nombre, Correo: correo, CuadrillaID: f.CuadrillaID,
		BodegaID: f.BodegaID, Habilidades: h, BaseLat: f.BaseLat, BaseLon: f.BaseLon, Telefono: f.Telefono, Activo: f.Activo,
		Argos: vinculoDe(f), Version: f.Version, CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt}
}

func (m *Modulo) nuevoListarTecnicos() *auditoria.Caso[EntradaListarTecnicos, nucleo.Pagina[Tecnico]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarTecnicos, nucleo.Pagina[Tecnico]]{
		Accion: "agenda.ver", Permiso: nucleo.PermAgendaVer, AuditarExito: soloLecturas[nucleo.Pagina[Tecnico]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarTecnicos) (nucleo.Pagina[Tecnico], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Tecnico]{}, err
			}
			p := m.paramsTecnicos(ctx, nucleo.PermAgendaVer)
			p.Limite = int32(in.Limite() + 1) //nolint:gosec // Limite() está acotado a 200
			if despues > 0 {
				p.DespuesDe = &despues
			}
			p.CuadrillaID, p.ZonaID, p.UsuarioID = filtro(in.CuadrillaID), filtro(in.ZonaID), filtro(in.UsuarioID)
			switch in.Activo {
			case "true":
				p.Activo = ptr(true)
			case "false":
				p.Activo = ptr(false)
			}
			if h := strings.TrimSpace(in.Habilidad); h != "" {
				p.Habilidad = &h
			}
			if s := strings.TrimSpace(in.Q); s != "" {
				p.Q = &s
			}
			fs, err := agendabd.New(t).ListarTecnicos(ctx, p)
			if err != nil {
				return nucleo.Pagina[Tecnico]{}, err
			}
			return nucleo.Paginar(fs, in.Limite(), func(f agendabd.ListarTecnicosRow) Tecnico {
				tc := aTecnico(agendabd.AgendaTecnico{ID: f.ID, UsuarioID: f.UsuarioID, CuadrillaID: f.CuadrillaID, BodegaID: f.BodegaID,
					Habilidades: f.Habilidades, BaseLat: f.BaseLat, BaseLon: f.BaseLon, Telefono: f.Telefono, Activo: f.Activo && f.UsuarioActivo,
					Version: f.Version, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt}, f.Nombre, f.Correo)
				return tc
			}, func(f agendabd.ListarTecnicosRow) []any { return []any{f.ID} }), nil
		},
	})
}

func filtro(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

// paramsTecnicos: empresa activa y alcance del actor (para ListarTecnicos).
func (m *Modulo) paramsTecnicos(ctx context.Context, permiso nucleo.Permiso) agendabd.ListarTecnicosParams {
	todas, id := filtroEmpresa(ctx)
	a := alcanceDe(ctx, permiso)
	return agendabd.ListarTecnicosParams{TodasEmpresas: todas, EmpresaID: id, AlcanceGlobal: a.global,
		Zonas: a.zonas, Cuadrillas: a.cuadrillas, CuadrillaComun: comun(ctx), Limite: 1000}
}

// tomarTecnico: la ficha (con candado si para actualizar), visible en la empresa activa (el
// usuario opera en ella) y con el alcance del permiso sobre su cuadrilla.
func (m *Modulo) tomarTecnico(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, id int64, permiso nucleo.Permiso, version *int32) (agendabd.AgendaTecnico, error) {
	t.FijarEntidad("tecnico", id)
	var (
		f   agendabd.AgendaTecnico
		err error
	)
	if version != nil {
		f, err = q.TecnicoPorIDParaActualizar(ctx, id)
	} else {
		f, err = q.TecnicoPorID(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return f, tecnicoNoEncontrado()
	}
	if err != nil {
		return f, err
	}
	todas, empresa := filtroEmpresa(ctx)
	if !todas {
		en, err := identidad.UsuarioEnEmpresa(ctx, t, f.UsuarioID, deref(empresa))
		if err != nil {
			return f, err
		}
		if !en {
			return f, tecnicoNoEncontrado()
		}
		t.FijarEmpresa(deref(empresa))
	}
	if ok, err := alcanceDe(ctx, nucleo.PermAgendaVer).cubreCuadrilla(ctx, q, f.CuadrillaID); err != nil || !ok {
		if err != nil {
			return f, err
		}
		return f, tecnicoNoEncontrado()
	}
	if ok, err := alcanceDe(ctx, permiso).cubreCuadrilla(ctx, q, f.CuadrillaID); err != nil || !ok {
		if err != nil {
			return f, err
		}
		return f, fueraDeAlcance(fmt.Sprintf("Tu permiso %s no cubre la cuadrilla de este técnico", permiso))
	}
	// Ficha, turnos, capacidad y ausencias se modifican solo desde la empresa de su cuadrilla.
	if permiso != nucleo.PermAgendaVer {
		if err := exigirFichaDeLaEmpresa(ctx, q, f); err != nil {
			return f, err
		}
	}
	if version != nil {
		return f, nucleo.ExigirVersion(*version, f.Version, "tecnico", id)
	}
	return f, nil
}

func exigirFichaDeLaEmpresa(ctx context.Context, q *agendabd.Queries, f agendabd.AgendaTecnico) error {
	ok, err := fichaDeLaEmpresa(ctx, q, f)
	if err != nil {
		return err
	}
	if !ok {
		return nucleo.Prohibido("La ficha se modifica desde la empresa de su cuadrilla; sin cuadrilla de tu empresa hace falta empresas.todas")
	}
	return nil
}

func (m *Modulo) nuevoVerTecnico() *auditoria.Caso[EntradaVerTecnico, TecnicoDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerTecnico, TecnicoDetalle]{
		Accion: "agenda.ver", Permiso: nucleo.PermAgendaVer, AuditarExito: soloLecturas[TecnicoDetalle],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerTecnico) (TecnicoDetalle, error) {
			q := agendabd.New(t)
			f, err := m.tomarTecnico(ctx, t, q, in.ID, nucleo.PermAgendaVer, nil)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			return m.detalleDe(ctx, q, f)
		},
	})
}

// datosFicha valida cuadrilla (de la empresa y en el alcance del permiso), habilidades y base.
func (m *Modulo) datosFicha(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, permiso nucleo.Permiso, cuadrilla *int64,
	habilidades []string, lat, lon *float64) ([]string, error) {
	if cuadrilla != nil {
		_, empresa := filtroEmpresa(ctx)
		if todas, _ := filtroEmpresa(ctx); todas {
			c, err := q.CuadrillaPorID(ctx, *cuadrilla)
			if err == nil {
				empresa = c.EmpresaID
			}
		}
		if err := ValidarCuadrilla(ctx, t, "cuadrillaId", *cuadrilla, empresa); err != nil {
			return nil, err
		}
	}
	if ok, err := alcanceDe(ctx, permiso).cubreCuadrilla(ctx, q, cuadrilla); err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, fueraDeAlcance(fmt.Sprintf("Tu permiso %s se limita a otras cuadrillas o zonas; indica una tuya", permiso))
	}
	if (lat == nil) != (lon == nil) {
		return nil, campoInvalido("baseLat", "baseLat y baseLon van juntas", "coordenadas")
	}
	salida := []string{}
	for i, h := range habilidades {
		v, err := texto(fmt.Sprintf("habilidades[%d]", i), strings.ToLower(h), 1, 40)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(salida, v) {
			salida = append(salida, v)
		}
	}
	slices.Sort(salida)
	return salida, nil
}

func (m *Modulo) nuevoCrearTecnico() *auditoria.Caso[EntradaCrearTecnico, TecnicoDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearTecnico, TecnicoDetalle]{
		Accion: "agenda.crear_tecnico", Permiso: nucleo.PermAgendaCrearTecnico,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearTecnico) (TecnicoDetalle, error) {
			q := agendabd.New(t)
			invalido := func(detalle string) error { return campoInvalido("usuarioId", detalle, "tecnico") }
			u, err := q.UsuarioParaTecnico(ctx, in.UsuarioID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !u.Activo) {
				return TecnicoDetalle{}, invalido("El usuario no existe o está inactivo")
			}
			if err != nil {
				return TecnicoDetalle{}, err
			}
			if u.Tipo != "persona" {
				return TecnicoDetalle{}, invalido("Solo personas, no integraciones")
			}
			alc, err := q.AlcancesDePermisoDeUsuario(ctx, agendabd.AlcancesDePermisoDeUsuarioParams{UsuarioID: u.ID,
				Permisos: []string{string(nucleo.PermOrdenesVerPropias), string(nucleo.PermOrdenesVer)}})
			if err != nil {
				return TecnicoDetalle{}, err
			}
			if len(alc) == 0 {
				return TecnicoDetalle{}, invalido("El usuario no trabaja órdenes (ordenes.ver_propias u ordenes.ver)")
			}
			todas, empresa := filtroEmpresa(ctx)
			if !todas {
				en, err := identidad.UsuarioEnEmpresa(ctx, t, u.ID, deref(empresa))
				if err != nil {
					return TecnicoDetalle{}, err
				}
				if !en {
					return TecnicoDetalle{}, nucleo.ReglaNegocio("tecnico_fuera_de_empresa", "El usuario no opera en la empresa activa")
				}
				t.FijarEmpresa(deref(empresa))
			}
			habilidades, err := m.datosFicha(ctx, t, q, nucleo.PermAgendaCrearTecnico, in.CuadrillaID, in.Habilidades, in.BaseLat, in.BaseLon)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			if err := exigirFichaDeLaEmpresa(ctx, q, agendabd.AgendaTecnico{CuadrillaID: in.CuadrillaID}); err != nil {
				return TecnicoDetalle{}, err
			}
			telefono, err := telefonoValido(in.Telefono)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			f, err := q.CrearTecnico(ctx, agendabd.CrearTecnicoParams{UsuarioID: u.ID, CuadrillaID: in.CuadrillaID, BodegaID: in.BodegaID,
				Habilidades: habilidades, BaseLat: in.BaseLat, BaseLon: in.BaseLon, Telefono: telefono})
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return TecnicoDetalle{}, nucleo.ReglaNegocio("ficha_existente", "El usuario ya tiene ficha de técnico")
			}
			if err != nil {
				return TecnicoDetalle{}, err
			}
			t.FijarEntidad("tecnico", f.ID)
			t.RegistrarCambios("tecnico", f.ID, nil, f)
			if err := t.Emitir("tecnico.creado", map[string]any{"tecnico_id": f.ID, "usuario_id": f.UsuarioID, "cuadrilla_id": f.CuadrillaID}); err != nil {
				return TecnicoDetalle{}, err
			}
			return m.detalleDe(ctx, q, f)
		},
	})
}

func (m *Modulo) guardarTecnico(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, antes, despues agendabd.AgendaTecnico, campos []string) (TecnicoDetalle, error) {
	f, err := q.GuardarTecnico(ctx, agendabd.GuardarTecnicoParams{ID: antes.ID, Version: antes.Version, CuadrillaID: despues.CuadrillaID,
		BodegaID: despues.BodegaID, Habilidades: despues.Habilidades, BaseLat: despues.BaseLat, BaseLon: despues.BaseLon,
		Telefono: despues.Telefono, Activo: despues.Activo})
	if err != nil {
		return TecnicoDetalle{}, nucleo.VerificarVersion(err, "tecnico", antes.ID)
	}
	t.RegistrarCambios("tecnico", f.ID, antes, f)
	if err := t.Emitir("tecnico.actualizado", map[string]any{"tecnico_id": f.ID, "usuario_id": f.UsuarioID, "campos": campos, "activo": f.Activo}); err != nil {
		return TecnicoDetalle{}, err
	}
	return m.detalleDe(ctx, q, f)
}

func (m *Modulo) nuevoEditarTecnico() *auditoria.Caso[EntradaEditarTecnico, TecnicoDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEditarTecnico, TecnicoDetalle]{
		Accion: "agenda.editar_tecnico", Permiso: nucleo.PermAgendaEditarTecnico,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEditarTecnico) (TecnicoDetalle, error) {
			q := agendabd.New(t)
			antes, err := m.tomarTecnico(ctx, t, q, in.ID, nucleo.PermAgendaEditarTecnico, &in.Version)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			habilidades, err := m.datosFicha(ctx, t, q, nucleo.PermAgendaEditarTecnico, in.CuadrillaID, in.Habilidades, in.BaseLat, in.BaseLon)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			despues := antes
			despues.CuadrillaID, despues.BodegaID, despues.Habilidades = in.CuadrillaID, in.BodegaID, habilidades
			if err := exigirFichaDeLaEmpresa(ctx, q, despues); err != nil {
				return TecnicoDetalle{}, err
			}
			despues.BaseLat, despues.BaseLon = in.BaseLat, in.BaseLon
			if despues.Telefono, err = telefonoValido(in.Telefono); err != nil {
				return TecnicoDetalle{}, err
			}
			campos := []string{}
			if !mismoID(antes.CuadrillaID, despues.CuadrillaID) {
				campos = append(campos, "cuadrilla_id")
			}
			if !mismoID(antes.BodegaID, despues.BodegaID) {
				campos = append(campos, "bodega_id")
			}
			if !slices.Equal(antes.Habilidades, despues.Habilidades) {
				campos = append(campos, "habilidades")
			}
			if !mismoFloat(antes.BaseLat, despues.BaseLat) || !mismoFloat(antes.BaseLon, despues.BaseLon) {
				campos = append(campos, "base")
			}
			if antes.Telefono != despues.Telefono {
				campos = append(campos, "telefono")
			}
			if len(campos) == 0 {
				return TecnicoDetalle{}, nucleo.ReglaNegocio("sin_cambios", "La ficha ya tiene esos datos")
			}
			return m.guardarTecnico(ctx, t, q, antes, despues, campos)
		},
	})
}

// telefonoValido: recorta espacios; vacío = sin teléfono. Hasta 30 caracteres, con al menos
// 7 dígitos, y solo dígitos, espacios, +, -, paréntesis y puntos.
func telefonoValido(v string) (string, error) {
	t, err := texto("telefono", v, 0, 30)
	if err != nil || t == "" {
		return t, err
	}
	digitos := 0
	for _, r := range t {
		switch {
		case r >= '0' && r <= '9':
			digitos++
		case strings.ContainsRune(" +-().", r):
		default:
			return "", campoInvalido("telefono", "Usa solo dígitos, espacios, +, -, paréntesis y puntos", "telefono")
		}
	}
	if digitos < 7 {
		return "", campoInvalido("telefono", "El teléfono necesita al menos 7 dígitos", "telefono")
	}
	return t, nil
}

func mismoFloat(a, b *float64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func (m *Modulo) nuevoActivarTecnico(activar bool) *auditoria.Caso[EntradaVersion, TecnicoDetalle] {
	accion, permiso := "agenda.desactivar_tecnico", nucleo.PermAgendaDesactivarTecnico
	if activar {
		accion, permiso = "agenda.activar_tecnico", nucleo.PermAgendaActivarTecnico
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaVersion, TecnicoDetalle]{
		Accion: accion, Permiso: permiso,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVersion) (TecnicoDetalle, error) {
			q := agendabd.New(t)
			antes, err := m.tomarTecnico(ctx, t, q, in.ID, permiso, &in.Version)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			if antes.Activo == activar {
				return TecnicoDetalle{}, nucleo.ReglaNegocio("sin_cambios", "La ficha ya está así")
			}
			despues := antes
			despues.Activo = activar
			return m.guardarTecnico(ctx, t, q, antes, despues, []string{"activo"})
		},
	})
}

// minutosDelDia: 24:00 = 1440 minutos.
const minutosDelDia = 24 * time.Hour

func (m *Modulo) nuevoFijarTurnos() *auditoria.Caso[EntradaFijarTurnos, TecnicoDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaFijarTurnos, TecnicoDetalle]{
		Accion: "agenda.fijar_turnos", Permiso: nucleo.PermAgendaFijarTurnos,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaFijarTurnos) (TecnicoDetalle, error) {
			q := agendabd.New(t)
			antes, err := m.tomarTecnico(ctx, t, q, in.ID, nucleo.PermAgendaFijarTurnos, &in.Version)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			type tramo struct {
				dia      int16
				ini, fin time.Duration
				turno    Turno
			}
			tramos := make([]tramo, 0, len(in.Turnos))
			for i, tu := range in.Turnos {
				campo := fmt.Sprintf("turnos[%d]", i)
				ini, ok1 := aTime(tu.Inicio)
				fin, ok2 := aTime(tu.Fin)
				if !ok1 || !ok2 || duracionDe(ini) >= minutosDelDia {
					return TecnicoDetalle{}, campoInvalido(campo, "Horas en formato HH:MM (el fin hasta 24:00)", "hora")
				}
				if tu.Dia < 1 || tu.Dia > 7 {
					return TecnicoDetalle{}, campoInvalido(campo+".dia", "Día ISO de 1 (lunes) a 7 (domingo)", "dia")
				}
				if duracionDe(fin) <= duracionDe(ini) {
					return TecnicoDetalle{}, campoInvalido(campo+".fin", "El fin debe ser posterior al inicio (un turno nocturno se parte en dos)", "rango")
				}
				tramos = append(tramos, tramo{dia: tu.Dia, ini: duracionDe(ini), fin: duracionDe(fin), turno: tu})
			}
			slices.SortFunc(tramos, func(a, b tramo) int {
				if a.dia != b.dia {
					return int(a.dia - b.dia)
				}
				return int(a.ini - b.ini)
			})
			for i := 1; i < len(tramos); i++ {
				if tramos[i].dia == tramos[i-1].dia && tramos[i].ini < tramos[i-1].fin {
					return TecnicoDetalle{}, campoInvalido("turnos", fmt.Sprintf("Turnos que se cruzan el día %d", tramos[i].dia), "solape")
				}
			}
			previo, err := m.detalleDe(ctx, q, antes)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			if err := q.QuitarTurnos(ctx, antes.ID); err != nil {
				return TecnicoDetalle{}, err
			}
			for _, tr := range tramos {
				ini, _ := aTime(tr.turno.Inicio)
				fin, _ := aTime(tr.turno.Fin)
				if err := q.CrearTurno(ctx, agendabd.CrearTurnoParams{TecnicoID: antes.ID, Dia: tr.dia, Inicio: ini, Fin: fin}); err != nil {
					return TecnicoDetalle{}, err
				}
			}
			f, err := q.TocarTecnico(ctx, agendabd.TocarTecnicoParams{ID: antes.ID, Version: antes.Version})
			if err != nil {
				return TecnicoDetalle{}, nucleo.VerificarVersion(err, "tecnico", antes.ID)
			}
			d, err := m.detalleDe(ctx, q, f)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			t.RegistrarCambios("tecnico", f.ID, map[string]any{"turnos": previo.Turnos}, map[string]any{"turnos": d.Turnos})
			return d, t.Emitir("tecnico.actualizado", map[string]any{"tecnico_id": f.ID, "usuario_id": f.UsuarioID,
				"campos": []string{"turnos"}, "activo": f.Activo})
		},
	})
}

func (m *Modulo) nuevoCapacidadTecnico() *auditoria.Caso[EntradaFijarCapacidad, TecnicoDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaFijarCapacidad, TecnicoDetalle]{
		Accion: "agenda.fijar_capacidad", Permiso: nucleo.PermAgendaFijarCapacidad,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaFijarCapacidad) (TecnicoDetalle, error) {
			q := agendabd.New(t)
			antes, err := m.tomarTecnico(ctx, t, q, in.ID, nucleo.PermAgendaFijarCapacidad, &in.Version)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			lineas, err := validarCapacidad(ctx, q, in.Capacidad)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			previo, err := m.detalleDe(ctx, q, antes)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			if err := q.QuitarCapacidadesTecnico(ctx, &antes.ID); err != nil {
				return TecnicoDetalle{}, err
			}
			for _, l := range lineas {
				if err := q.CrearCapacidad(ctx, agendabd.CrearCapacidadParams{TecnicoID: &antes.ID, TipoID: l.tipo, Cupos: l.cupos, Minutos: l.minutos}); err != nil {
					return TecnicoDetalle{}, err
				}
			}
			f, err := q.TocarTecnico(ctx, agendabd.TocarTecnicoParams{ID: antes.ID, Version: antes.Version})
			if err != nil {
				return TecnicoDetalle{}, nucleo.VerificarVersion(err, "tecnico", antes.ID)
			}
			d, err := m.detalleDe(ctx, q, f)
			if err != nil {
				return TecnicoDetalle{}, err
			}
			t.RegistrarCambios("tecnico", f.ID, map[string]any{"capacidad": previo.Capacidad}, map[string]any{"capacidad": d.Capacidad})
			return d, t.Emitir("tecnico.actualizado", map[string]any{"tecnico_id": f.ID, "usuario_id": f.UsuarioID,
				"campos": []string{"capacidad"}, "activo": f.Activo})
		},
	})
}

// EntradaCandidatosTecnico: GET /tecnicos/candidatos.
type EntradaCandidatosTecnico struct {
	nucleo.ParamsPagina
	Q string `query:"q" maxLength:"100" doc:"Nombre o correo"`
}

// CandidatoTecnico es un usuario que puede recibir ficha de técnico.
type CandidatoTecnico struct {
	UsuarioID int64  `json:"usuarioId"`
	Nombre    string `json:"nombre"`
	Correo    string `json:"correo"`
}

// nuevoCandidatosTecnico: usuarios a los que se les puede dar ficha (personas activas sin ficha,
// con ordenes.ver_propias —el técnico de campo—, que operan en la empresa activa). Con el permiso
// de crear limitado a zonas o cuadrillas, solo las personas cuyo rol de técnico es de ese alcance.
// Es la lista del selector «Usuario» del alta: no exige usuarios.ver, sino el permiso de crear.
func (m *Modulo) nuevoCandidatosTecnico() *auditoria.Caso[EntradaCandidatosTecnico, nucleo.Pagina[CandidatoTecnico]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCandidatosTecnico, nucleo.Pagina[CandidatoTecnico]]{
		Accion: "agenda.candidatos_tecnico", Permiso: nucleo.PermAgendaCrearTecnico, AuditarExito: soloLecturas[nucleo.Pagina[CandidatoTecnico]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCandidatosTecnico) (nucleo.Pagina[CandidatoTecnico], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[CandidatoTecnico]{}, err
			}
			todas, empresa := filtroEmpresa(ctx)
			a := alcanceDe(ctx, nucleo.PermAgendaCrearTecnico)
			p := agendabd.CandidatosTecnicoParams{TodasEmpresas: todas, EmpresaID: empresa,
				AlcanceGlobal: a.global, Zonas: a.zonas, Cuadrillas: a.cuadrillas,
				Limite: int32(in.Limite() + 1)} //nolint:gosec // Limite() está acotado a 200
			if despues > 0 {
				p.DespuesDe = &despues
			}
			if s := strings.TrimSpace(in.Q); s != "" {
				p.Q = &s
			}
			fs, err := agendabd.New(t).CandidatosTecnico(ctx, p)
			if err != nil {
				return nucleo.Pagina[CandidatoTecnico]{}, err
			}
			return nucleo.Paginar(fs, in.Limite(), func(f agendabd.CandidatosTecnicoRow) CandidatoTecnico {
				return CandidatoTecnico{UsuarioID: f.ID, Nombre: f.Nombre, Correo: f.Correo}
			}, func(f agendabd.CandidatosTecnicoRow) []any { return []any{f.ID} }), nil
		},
	})
}
