package agenda

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	agendabd "github.com/HCDestroyer/iris/apps/api/internal/agenda/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// EntradaListarCatalogo: listar zonas o cuadrillas.
type EntradaListarCatalogo struct {
	Todas bool `query:"todas" doc:"Incluye las inactivas"`
}

// EntradaCrearZona crea una zona local.
type EntradaCrearZona struct {
	Nombre   string         `json:"nombre" minLength:"1" maxLength:"120"`
	Poligono map[string]any `json:"poligono,omitempty" doc:"GeoJSON Polygon o MultiPolygon ([lon, lat]), hasta 5000 vértices"`
}

// EntradaEditarZona edita una zona: nombre (solo las locales o migradas) y polígono.
type EntradaEditarZona struct {
	ID             int64          `json:"-"`
	Version        int32          `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Nombre         *string        `json:"nombre,omitempty" minLength:"1" maxLength:"120" doc:"No se edita en las zonas de Argos (vienen de su catálogo)"`
	Poligono       map[string]any `json:"poligono,omitempty" doc:"GeoJSON Polygon o MultiPolygon; ausente = se conserva"`
	QuitarPoligono bool           `json:"quitarPoligono,omitempty" doc:"Borra el polígono"`
}

// EntradaVersion: activar o desactivar (zonas, cuadrillas, técnicos).
type EntradaVersion struct {
	ID      int64   `json:"-"`
	Version int32   `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo  *string `json:"motivo,omitempty" maxLength:"500"`
}

// ResultadoEspejo del refresco de zonas desde el catálogo de Argos.
type ResultadoEspejo struct {
	Actualizadas int64 `json:"actualizadas"`
	Desactivadas int64 `json:"desactivadas"`
}

func zonaNoEncontrada() *nucleo.Problema { return nucleo.NoEncontrado("La zona no existe") }

// maxVertices del polígono de una zona.
const maxVertices = 5000

// validarPoligono acepta GeoJSON Polygon o MultiPolygon con anillos cerrados de al menos 4
// posiciones [lon, lat] en rango. Devuelve el GeoJSON normalizado ({type, coordinates}).
func validarPoligono(campo string, g map[string]any) ([]byte, error) {
	malo := func(detalle string) error { return campoInvalido(campo, detalle, "geojson") }
	crudo, err := json.Marshal(g)
	if err != nil {
		return nil, malo("GeoJSON ilegible")
	}
	var geo struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if err := json.Unmarshal(crudo, &geo); err != nil {
		return nil, malo("GeoJSON ilegible")
	}
	var poligonos [][][][]float64
	switch geo.Type {
	case "Polygon":
		var p [][][]float64
		if err := json.Unmarshal(geo.Coordinates, &p); err != nil {
			return nil, malo("coordinates de un Polygon: [[[lon, lat], …]]")
		}
		poligonos = [][][][]float64{p}
	case "MultiPolygon":
		if err := json.Unmarshal(geo.Coordinates, &poligonos); err != nil {
			return nil, malo("coordinates de un MultiPolygon: [[[[lon, lat], …]]]")
		}
	default:
		return nil, malo("Solo Polygon o MultiPolygon")
	}
	vertices := 0
	if len(poligonos) == 0 {
		return nil, malo("Sin polígonos")
	}
	for _, p := range poligonos {
		if len(p) == 0 {
			return nil, malo("Polígono sin anillos")
		}
		for _, anillo := range p {
			if len(anillo) < 4 {
				return nil, malo("Cada anillo necesita al menos 4 posiciones")
			}
			for _, pos := range anillo {
				if len(pos) < 2 || pos[0] < -180 || pos[0] > 180 || pos[1] < -90 || pos[1] > 90 {
					return nil, malo("Posición fuera de rango ([lon, lat])")
				}
				vertices++
			}
			a, z := anillo[0], anillo[len(anillo)-1]
			if a[0] != z[0] || a[1] != z[1] {
				return nil, malo("Cada anillo debe cerrarse (la última posición igual a la primera)")
			}
		}
	}
	if vertices > maxVertices {
		return nil, malo(fmt.Sprintf("Hasta %d vértices", maxVertices))
	}
	return json.Marshal(map[string]any{"type": geo.Type, "coordinates": geo.Coordinates})
}

func (m *Modulo) nuevoListarZonas() *auditoria.Caso[EntradaListarCatalogo, nucleo.Pagina[Zona]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarCatalogo, nucleo.Pagina[Zona]]{
		Accion: "agenda.ver", Permiso: nucleo.PermAgendaVer, AuditarExito: soloLecturas[nucleo.Pagina[Zona]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarCatalogo) (nucleo.Pagina[Zona], error) {
			todas, id := filtroEmpresa(ctx)
			fs, err := agendabd.New(t).ListarZonas(ctx, agendabd.ListarZonasParams{TodasEmpresas: todas, EmpresaID: id, IncluirInactivas: in.Todas})
			if err != nil {
				return nucleo.Pagina[Zona]{}, err
			}
			salida := make([]Zona, 0, len(fs))
			for _, f := range fs {
				salida = append(salida, aZona(f))
			}
			return nucleo.PaginaDe(salida), nil
		},
	})
}

func (m *Modulo) nuevoCrearZona() *auditoria.Caso[EntradaCrearZona, Zona] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearZona, Zona]{
		Accion: "agenda.crear_zona", Permiso: nucleo.PermAgendaCrearZona,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearZona) (Zona, error) {
			if err := exigirGlobal(ctx, nucleo.PermAgendaCrearZona); err != nil {
				return Zona{}, err
			}
			nombre, err := texto("nombre", in.Nombre, 1, 120)
			if err != nil {
				return Zona{}, err
			}
			var poligono []byte
			if in.Poligono != nil {
				if poligono, err = validarPoligono("poligono", in.Poligono); err != nil {
					return Zona{}, err
				}
			}
			empresa, err := empresaDeAlta(ctx)
			if err != nil {
				return Zona{}, err
			}
			t.FijarEmpresa(deref(empresa))
			f, err := agendabd.New(t).CrearZonaLocal(ctx, agendabd.CrearZonaLocalParams{EmpresaID: empresa, Nombre: nombre, Poligono: poligono})
			if err != nil {
				return Zona{}, err
			}
			t.FijarEntidad("zona", f.ID)
			t.RegistrarCambios("zona", f.ID, nil, f)
			return aZona(f), t.Emitir("zona.creada", map[string]any{"zona_id": f.ID, "origen": f.Origen, "nombre": f.Nombre})
		},
	})
}

// tomarZona: con candado, visible en la empresa y con la versión leída.
func tomarZona(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, id int64, version int32) (agendabd.AgendaZona, error) {
	t.FijarEntidad("zona", id)
	f, err := q.ZonaPorIDParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !catalogoVisible(ctx, f.EmpresaID)) {
		return f, zonaNoEncontrada()
	}
	if err != nil {
		return f, err
	}
	t.FijarEmpresa(deref(f.EmpresaID))
	if err := exigirModificable(ctx, f.EmpresaID); err != nil {
		return f, err
	}
	return f, nucleo.ExigirVersion(version, f.Version, "zona", id)
}

func guardarZona(ctx context.Context, t *auditoria.Tx, q *agendabd.Queries, antes, despues agendabd.AgendaZona, campos []string) (Zona, error) {
	f, err := q.GuardarZona(ctx, agendabd.GuardarZonaParams{ID: antes.ID, Version: antes.Version, Nombre: despues.Nombre,
		Poligono: despues.Poligono, Activa: despues.Activa})
	if err != nil {
		return Zona{}, nucleo.VerificarVersion(err, "zona", antes.ID)
	}
	t.RegistrarCambios("zona", f.ID, antes, f)
	return aZona(f), t.Emitir("zona.actualizada", map[string]any{"zona_id": f.ID, "campos": campos, "activa": f.Activa})
}

func (m *Modulo) nuevoEditarZona() *auditoria.Caso[EntradaEditarZona, Zona] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEditarZona, Zona]{
		Accion: "agenda.editar_zona", Permiso: nucleo.PermAgendaEditarZona,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEditarZona) (Zona, error) {
			if err := exigirGlobal(ctx, nucleo.PermAgendaEditarZona); err != nil {
				return Zona{}, err
			}
			q := agendabd.New(t)
			antes, err := tomarZona(ctx, t, q, in.ID, in.Version)
			if err != nil {
				return Zona{}, err
			}
			despues := antes
			campos := []string{}
			if in.Nombre != nil {
				nombre, err := texto("nombre", *in.Nombre, 1, 120)
				if err != nil {
					return Zona{}, err
				}
				if nombre != antes.Nombre {
					if antes.Origen == "argos" {
						return Zona{}, campoInvalido("nombre", "El nombre de una zona de Argos se cambia en Argos", "argos")
					}
					despues.Nombre = nombre
					campos = append(campos, "nombre")
				}
			}
			switch {
			case in.QuitarPoligono && in.Poligono != nil:
				return Zona{}, campoInvalido("poligono", "Manda poligono o quitarPoligono, no los dos", "poligono")
			case in.QuitarPoligono:
				if antes.Poligono != nil {
					despues.Poligono = nil
					campos = append(campos, "poligono")
				}
			case in.Poligono != nil:
				p, err := validarPoligono("poligono", in.Poligono)
				if err != nil {
					return Zona{}, err
				}
				despues.Poligono = p
				campos = append(campos, "poligono")
			}
			if len(campos) == 0 {
				return Zona{}, nucleo.ReglaNegocio("sin_cambios", "La zona ya tiene esos datos")
			}
			return guardarZona(ctx, t, q, antes, despues, campos)
		},
	})
}

func (m *Modulo) nuevoActivarZona(activar bool) *auditoria.Caso[EntradaVersion, Zona] {
	accion, permiso := "agenda.desactivar_zona", nucleo.PermAgendaDesactivarZona
	if activar {
		accion, permiso = "agenda.activar_zona", nucleo.PermAgendaActivarZona
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaVersion, Zona]{
		Accion: accion, Permiso: permiso,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVersion) (Zona, error) {
			if err := exigirGlobal(ctx, permiso); err != nil {
				return Zona{}, err
			}
			q := agendabd.New(t)
			antes, err := tomarZona(ctx, t, q, in.ID, in.Version)
			if err != nil {
				return Zona{}, err
			}
			if antes.Activa == activar {
				return Zona{}, nucleo.ReglaNegocio("sin_cambios", "La zona ya está así")
			}
			despues := antes
			despues.Activa = activar
			return guardarZona(ctx, t, q, antes, despues, []string{"activa"})
		},
	})
}

// nuevoSincronizarZonas: refresco del espejo desde integracion.argos_zonas (job del worker,
// actor sistema). Solo audita si cambió algo.
func (m *Modulo) nuevoSincronizarZonas() *auditoria.Caso[struct{}, ResultadoEspejo] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ResultadoEspejo]{
		Accion: "agenda.sincronizar_zonas", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoEspejo) bool { return r.Actualizadas+r.Desactivadas > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ResultadoEspejo, error) {
			if !esSistema(ctx) {
				return ResultadoEspejo{}, nucleo.Prohibido("Solo el sistema refresca el espejo de zonas")
			}
			q := agendabd.New(t)
			n, err := q.EspejarZonasArgos(ctx)
			if err != nil {
				return ResultadoEspejo{}, err
			}
			d, err := q.DesactivarZonasSinArgos(ctx)
			if err != nil {
				return ResultadoEspejo{}, err
			}
			return ResultadoEspejo{Actualizadas: n, Desactivadas: d}, nil
		},
	})
}

// --- Validación para tickets y órdenes (soporte y campo) -----------------------------------

// ZonaInvalida: la zona no existe, está inactiva o es de otra empresa (422 en el campo).
func ZonaInvalida(campo string) *nucleo.Problema {
	return campoInvalido(campo, "La zona no existe, está inactiva o es de otra empresa", "zona")
}

// CuadrillaInvalida: la cuadrilla no existe, está inactiva o es de otra empresa.
func CuadrillaInvalida(campo string) *nucleo.Problema {
	return campoInvalido(campo, "La cuadrilla no existe, está inactiva o es de otra empresa", "cuadrilla")
}

// ValidarZona: una zona que indicó una persona o integración para un ticket u orden de esa
// empresa (null = sin conector). Debe estar en el catálogo, activa y ser de esa empresa; una
// zona sin empresa solo sirve mientras no haya empresas registradas. Si Iris todavía no la copió pero está en la instantánea de Argos de esa empresa, la
// copia (espejo) y la acepta. Si no, 422 en campo.
func ValidarZona(ctx context.Context, db agendabd.DBTX, campo string, zonaID int64, empresa *int64) error {
	q := agendabd.New(db)
	z, err := q.ZonaPorID(ctx, zonaID)
	if errors.Is(err, pgx.ErrNoRows) {
		a, err := q.ZonaArgos(ctx, zonaID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !mismoID(&a.EmpresaID, empresa)) {
			return ZonaInvalida(campo)
		}
		if err != nil {
			return err
		}
		return asegurar(ctx, q, zonaID, &a.EmpresaID, a.Nombre, &a.SincronizadoEn)
	}
	if err != nil {
		return err
	}
	ok, err := sirveA(ctx, q, z.EmpresaID, empresa)
	if err != nil {
		return err
	}
	if !z.Activa || !ok {
		return ZonaInvalida(campo)
	}
	return nil
}

// ZonaDeArgos: una zona que viene de un dato de Argos (instantánea del servicio o del cliente,
// ficha del portal). Si Iris no la tiene, la copia de integracion.argos_zonas o, si el
// catálogo todavía no la trajo, crea el espejo «Zona <id>» (el refresco le pone el nombre).
// Devuelve nil (sin zona) si está en el catálogo con otra empresa: nunca mezcla empresas.
func ZonaDeArgos(ctx context.Context, db agendabd.DBTX, zonaID *int64, empresa *int64) (*int64, error) {
	if zonaID == nil {
		return nil, nil
	}
	q := agendabd.New(db)
	z, err := q.ZonaPorID(ctx, *zonaID)
	if err == nil {
		if ok, err := sirveA(ctx, q, z.EmpresaID, empresa); err != nil || !ok {
			return nil, err
		}
		return zonaID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	nombre := "Zona " + strconv.FormatInt(*zonaID, 10)
	var sinc *time.Time
	a, err := q.ZonaArgos(ctx, *zonaID)
	switch {
	case err == nil:
		if !mismoID(&a.EmpresaID, empresa) {
			return nil, nil
		}
		nombre, sinc = a.Nombre, &a.SincronizadoEn
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}
	if err := asegurar(ctx, q, *zonaID, empresa, nombre, sinc); err != nil {
		return nil, err
	}
	return zonaID, nil
}

func asegurar(ctx context.Context, q *agendabd.Queries, id int64, empresa *int64, nombre string, sinc *time.Time) error {
	if n := []rune(nombre); len(n) == 0 || len(n) > 120 {
		nombre = "Zona " + strconv.FormatInt(id, 10)
	}
	return q.AsegurarZonaArgos(ctx, agendabd.AsegurarZonaArgosParams{ID: id, EmpresaID: empresa, Nombre: nombre, SincronizadoEn: sinc})
}

// ValidarCuadrilla: la cuadrilla existe, está activa y es de la empresa (sin empresa, solo
// mientras no haya empresas registradas).
func ValidarCuadrilla(ctx context.Context, db agendabd.DBTX, campo string, cuadrillaID int64, empresa *int64) error {
	q := agendabd.New(db)
	c, err := q.CuadrillaPorID(ctx, cuadrillaID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CuadrillaInvalida(campo)
	}
	if err != nil {
		return err
	}
	ok, err := sirveA(ctx, q, c.EmpresaID, empresa)
	if err != nil {
		return err
	}
	if !c.Activa || !ok {
		return CuadrillaInvalida(campo)
	}
	return nil
}
