package agenda

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// EntradaCrearCuadrilla crea una cuadrilla.
type EntradaCrearCuadrilla struct {
	Nombre  string  `json:"nombre" minLength:"1" maxLength:"120"`
	ZonaIDs []int64 `json:"zonaIds,omitempty" maxItems:"500" doc:"Zonas que atiende (vacío = todas)"`
}

// EntradaEditarCuadrilla reemplaza nombre y zonas.
type EntradaEditarCuadrilla struct {
	ID      int64   `json:"-"`
	Version int32   `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Nombre  string  `json:"nombre" minLength:"1" maxLength:"120"`
	ZonaIDs []int64 `json:"zonaIds" maxItems:"500" doc:"Zonas que atiende (vacío = todas); reemplaza las anteriores"`
}

// EntradaFijarCapacidad reemplaza la capacidad diaria de un técnico o una cuadrilla.
type EntradaFijarCapacidad struct {
	ID        int64       `json:"-"`
	Version   int32       `json:"version" minimum:"1" doc:"Versión leída de la ficha o la cuadrilla; si cambió, 409"`
	Capacidad []Capacidad `json:"capacidad" maxItems:"6" doc:"Una línea por tipo (o null = todos); vacío = sin límite"`
}

func cuadrillaNoEncontrada() *nucleo.Problema { return nucleo.NoEncontrado("La cuadrilla no existe") }

// cuadrillasDe arma las cuadrillas con sus zonas y capacidad.
func cuadrillasDe(ctx context.Context, q *agendabd.Queries, fs []agendabd.AgendaCuadrilla) ([]Cuadrilla, error) {
	ids := make([]int64, 0, len(fs))
	for _, f := range fs {
		ids = append(ids, f.ID)
	}
	zs, err := q.ZonasDeCuadrillas(ctx, ids)
	if err != nil {
		return nil, err
	}
	caps, err := q.CapacidadesDeTecnicos(ctx, agendabd.CapacidadesDeTecnicosParams{Tecnicos: []int64{}, Cuadrillas: ids})
	if err != nil {
		return nil, err
	}
	tipos, err := codigosDeTipos(ctx, q)
	if err != nil {
		return nil, err
	}
	salida := make([]Cuadrilla, 0, len(fs))
	for _, f := range fs {
		c := Cuadrilla{ID: f.ID, EmpresaID: f.EmpresaID, Nombre: f.Nombre, Activa: f.Activa, ZonaIDs: []int64{},
			Capacidad: []Capacidad{}, Version: f.Version, CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt}
		for _, z := range zs {
			if z.CuadrillaID == f.ID {
				c.ZonaIDs = append(c.ZonaIDs, z.ZonaID)
			}
		}
		for _, cp := range caps {
			if cp.CuadrillaID != nil && *cp.CuadrillaID == f.ID {
				c.Capacidad = append(c.Capacidad, aCapacidad(cp, tipos))
			}
		}
		salida = append(salida, c)
	}
	return salida, nil
}

func cuadrillaDe(ctx context.Context, q *agendabd.Queries, f agendabd.AgendaCuadrilla) (Cuadrilla, error) {
	cs, err := cuadrillasDe(ctx, q, []agendabd.AgendaCuadrilla{f})
	if err != nil {
		return Cuadrilla{}, err
	}
	return cs[0], nil
}

func (m *Modulo) nuevoListarCuadrillas() *auditoria.Caso[EntradaListarCatalogo, nucleo.Pagina[Cuadrilla]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarCatalogo, nucleo.Pagina[Cuadrilla]]{
		Accion: "agenda.ver", Permiso: nucleo.PermAgendaVer, AuditarExito: soloLecturas[nucleo.Pagina[Cuadrilla]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarCatalogo) (nucleo.Pagina[Cuadrilla], error) {
			q := agendabd.New(t)
			todas, id := filtroEmpresa(ctx)
			fs, err := q.ListarCuadrillas(ctx, agendabd.ListarCuadrillasParams{TodasEmpresas: todas, EmpresaID: id, IncluirInactivas: in.Todas})
			if err != nil {
				return nucleo.Pagina[Cuadrilla]{}, err
			}
			cs, err := cuadrillasDe(ctx, q, fs)
			if err != nil {
				return nucleo.Pagina[Cuadrilla]{}, err
			}
			return nucleo.PaginaDe(cs), nil
		},
	})
}

// zonasDeLaCuadrilla valida las zonas (existen, activas, de la empresa) sin repetir.
func zonasDeLaCuadrilla(ctx context.Context, t *auditoria.Tx, ids []int64, empresa *int64) ([]int64, error) {
	salida := []int64{}
	for _, id := range ids {
		if slices.Contains(salida, id) {
			continue
		}
		if err := ValidarZona(ctx, t, "zonaIds", id, empresa); err != nil {
			return nil, err
		}
		salida = append(salida, id)
	}
	slices.Sort(salida)
	return salida, nil
}

func fijarZonas(ctx context.Context, q *agendabd.Queries, cuadrilla int64, zonas []int64) error {
	if err := q.QuitarZonasDeCuadrilla(ctx, cuadrilla); err != nil {
		return err
	}
	if len(zonas) == 0 {
		return nil
	}
	return q.AgregarZonasACuadrilla(ctx, agendabd.AgregarZonasACuadrillaParams{CuadrillaID: cuadrilla, Zonas: zonas})
}

// nombreRepetido: el índice único (empresa, nombre) da 422 en el campo, no un 409 genérico.
func nombreRepetido(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return campoInvalido("nombre", "Ya hay una cuadrilla con ese nombre", "unico")
	}
	return err
}

func (m *Modulo) nuevoCrearCuadrilla() *auditoria.Caso[EntradaCrearCuadrilla, Cuadrilla] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearCuadrilla, Cuadrilla]{
		Accion: "agenda.crear_cuadrilla", Permiso: nucleo.PermAgendaCrearCuadrilla,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearCuadrilla) (Cuadrilla, error) {
			if err := exigirGlobal(ctx, nucleo.PermAgendaCrearCuadrilla); err != nil {
				return Cuadrilla{}, err
			}
			nombre, err := texto("nombre", in.Nombre, 1, 120)
			if err != nil {
				return Cuadrilla{}, err
			}
			empresa, err := empresaDeAlta(ctx)
			if err != nil {
				return Cuadrilla{}, err
			}
			t.FijarEmpresa(deref(empresa))
			zonas, err := zonasDeLaCuadrilla(ctx, t, in.ZonaIDs, empresa)
			if err != nil {
				return Cuadrilla{}, err
			}
			q := agendabd.New(t)
			f, err := q.CrearCuadrilla(ctx, agendabd.CrearCuadrillaParams{EmpresaID: empresa, Nombre: nombre})
			if err != nil {
				return Cuadrilla{}, nombreRepetido(err)
			}
			t.FijarEntidad("cuadrilla", f.ID)
			if err := fijarZonas(ctx, q, f.ID, zonas); err != nil {
				return Cuadrilla{}, err
			}
			t.RegistrarCambios("cuadrilla", f.ID, nil, f)
			c, err := cuadrillaDe(ctx, q, f)
			if err != nil {
				return Cuadrilla{}, err
			}
			return c, t.Emitir("cuadrilla.creada", map[string]any{"cuadrilla_id": f.ID, "nombre": f.Nombre, "zonas": zonas})
		},
	})
}

func tomarCuadrilla(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, id int64, version int32) (agendabd.AgendaCuadrilla, error) {
	t.FijarEntidad("cuadrilla", id)
	f, err := q.CuadrillaPorIDParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !catalogoVisible(ctx, f.EmpresaID)) {
		return f, cuadrillaNoEncontrada()
	}
	if err != nil {
		return f, err
	}
	t.FijarEmpresa(deref(f.EmpresaID))
	return f, nucleo.ExigirVersion(version, f.Version, "cuadrilla", id)
}

func (m *Modulo) nuevoEditarCuadrilla() *auditoria.Caso[EntradaEditarCuadrilla, Cuadrilla] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEditarCuadrilla, Cuadrilla]{
		Accion: "agenda.editar_cuadrilla", Permiso: nucleo.PermAgendaEditarCuadrilla,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEditarCuadrilla) (Cuadrilla, error) {
			if err := exigirGlobal(ctx, nucleo.PermAgendaEditarCuadrilla); err != nil {
				return Cuadrilla{}, err
			}
			q := agendabd.New(t)
			antes, err := tomarCuadrilla(ctx, t, q, in.ID, in.Version)
			if err != nil {
				return Cuadrilla{}, err
			}
			nombre, err := texto("nombre", in.Nombre, 1, 120)
			if err != nil {
				return Cuadrilla{}, err
			}
			zonas, err := zonasDeLaCuadrilla(ctx, t, in.ZonaIDs, antes.EmpresaID)
			if err != nil {
				return Cuadrilla{}, err
			}
			previas, err := cuadrillaDe(ctx, q, antes)
			if err != nil {
				return Cuadrilla{}, err
			}
			campos := []string{}
			if nombre != antes.Nombre {
				campos = append(campos, "nombre")
			}
			if !slices.Equal(previas.ZonaIDs, zonas) {
				campos = append(campos, "zonas")
			}
			if len(campos) == 0 {
				return Cuadrilla{}, nucleo.ReglaNegocio("sin_cambios", "La cuadrilla ya tiene esos datos")
			}
			f, err := q.GuardarCuadrilla(ctx, agendabd.GuardarCuadrillaParams{ID: antes.ID, Version: antes.Version, Nombre: nombre, Activa: antes.Activa})
			if err != nil {
				return Cuadrilla{}, nombreRepetido(nucleo.VerificarVersion(err, "cuadrilla", antes.ID))
			}
			if err := fijarZonas(ctx, q, f.ID, zonas); err != nil {
				return Cuadrilla{}, err
			}
			t.RegistrarCambios("cuadrilla", f.ID, map[string]any{"nombre": antes.Nombre, "zonas": previas.ZonaIDs},
				map[string]any{"nombre": f.Nombre, "zonas": zonas})
			c, err := cuadrillaDe(ctx, q, f)
			if err != nil {
				return Cuadrilla{}, err
			}
			return c, t.Emitir("cuadrilla.actualizada", map[string]any{"cuadrilla_id": f.ID, "campos": campos, "activa": f.Activa})
		},
	})
}

func (m *Modulo) nuevoActivarCuadrilla(activar bool) *auditoria.Caso[EntradaVersion, Cuadrilla] {
	accion, permiso := "agenda.desactivar_cuadrilla", nucleo.PermAgendaDesactivarCuadrilla
	if activar {
		accion, permiso = "agenda.activar_cuadrilla", nucleo.PermAgendaActivarCuadrilla
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaVersion, Cuadrilla]{
		Accion: accion, Permiso: permiso,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVersion) (Cuadrilla, error) {
			if err := exigirGlobal(ctx, permiso); err != nil {
				return Cuadrilla{}, err
			}
			q := agendabd.New(t)
			antes, err := tomarCuadrilla(ctx, t, q, in.ID, in.Version)
			if err != nil {
				return Cuadrilla{}, err
			}
			if antes.Activa == activar {
				return Cuadrilla{}, nucleo.ReglaNegocio("sin_cambios", "La cuadrilla ya está así")
			}
			f, err := q.GuardarCuadrilla(ctx, agendabd.GuardarCuadrillaParams{ID: antes.ID, Version: antes.Version, Nombre: antes.Nombre, Activa: activar})
			if err != nil {
				return Cuadrilla{}, nucleo.VerificarVersion(err, "cuadrilla", antes.ID)
			}
			t.RegistrarCambios("cuadrilla", f.ID, antes, f)
			c, err := cuadrillaDe(ctx, q, f)
			if err != nil {
				return Cuadrilla{}, err
			}
			return c, t.Emitir("cuadrilla.actualizada", map[string]any{"cuadrilla_id": f.ID, "campos": []string{"activa"}, "activa": f.Activa})
		},
	})
}

func (m *Modulo) nuevoCapacidadCuadrilla() *auditoria.Caso[EntradaFijarCapacidad, Cuadrilla] {
	return auditoria.Nuevo(auditoria.Caso[EntradaFijarCapacidad, Cuadrilla]{
		Accion: "agenda.fijar_capacidad", Permiso: nucleo.PermAgendaFijarCapacidad,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaFijarCapacidad) (Cuadrilla, error) {
			q := agendabd.New(t)
			antes, err := tomarCuadrilla(ctx, t, q, in.ID, in.Version)
			if err != nil {
				return Cuadrilla{}, err
			}
			if ok, err := alcanceDe(ctx, nucleo.PermAgendaFijarCapacidad).cubreCuadrilla(ctx, q, &antes.ID); err != nil || !ok {
				if err != nil {
					return Cuadrilla{}, err
				}
				return Cuadrilla{}, fueraDeAlcance("Tu permiso agenda.fijar_capacidad no cubre esta cuadrilla")
			}
			lineas, err := validarCapacidad(ctx, q, in.Capacidad)
			if err != nil {
				return Cuadrilla{}, err
			}
			previa, err := cuadrillaDe(ctx, q, antes)
			if err != nil {
				return Cuadrilla{}, err
			}
			if err := q.QuitarCapacidadesCuadrilla(ctx, &antes.ID); err != nil {
				return Cuadrilla{}, err
			}
			for _, l := range lineas {
				if err := q.CrearCapacidad(ctx, agendabd.CrearCapacidadParams{CuadrillaID: &antes.ID, TipoID: l.tipo, Cupos: l.cupos, Minutos: l.minutos}); err != nil {
					return Cuadrilla{}, err
				}
			}
			f, err := q.TocarCuadrilla(ctx, agendabd.TocarCuadrillaParams{ID: antes.ID, Version: antes.Version})
			if err != nil {
				return Cuadrilla{}, nucleo.VerificarVersion(err, "cuadrilla", antes.ID)
			}
			c, err := cuadrillaDe(ctx, q, f)
			if err != nil {
				return Cuadrilla{}, err
			}
			t.RegistrarCambios("cuadrilla", f.ID, map[string]any{"capacidad": previa.Capacidad}, map[string]any{"capacidad": c.Capacidad})
			return c, t.Emitir("cuadrilla.actualizada", map[string]any{"cuadrilla_id": f.ID, "campos": []string{"capacidad"}, "activa": f.Activa})
		},
	})
}
