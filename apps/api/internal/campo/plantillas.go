package campo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Plantillas de formularios (checklist) administrables: crear, editar (versión nueva si la
// actual ya se usa en alguna orden), activar y desactivar. Una plantilla es de una empresa o
// común (empresa null); con empresas registradas, las comunes solo las modifica quien puede
// tocar lo común (empresas.todas global, el sistema o el modo sin conector), como el catálogo de
// agenda.

// TiposItem son los tipos de ítem de una plantilla.
var TiposItem = []string{ItemSiNo, ItemTexto, ItemNumero, ItemFoto, ItemMedicion, ItemSeleccion}

var (
	codigoValido = regexp.MustCompile(`^[a-z0-9_]{2,60}$`)
	codigoItem   = regexp.MustCompile(`^[a-z0-9_]{1,60}$`)
)

// PlantillaFormulario es una plantilla con su versión actual (o la pedida).
type PlantillaFormulario struct {
	ID            int64           `json:"id"`
	Codigo        string          `json:"codigo" doc:"Único por empresa (las comunes, entre las comunes)"`
	Nombre        string          `json:"nombre"`
	Descripcion   string          `json:"descripcion"`
	Tipo          *string         `json:"tipo" nullable:"true" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento" doc:"Tipo de orden para el que se ofrece; null = cualquiera"`
	EmpresaID     *int64          `json:"empresaId" nullable:"true" doc:"null = común a todas las empresas"`
	RequiereFirma bool            `json:"requiereFirma"`
	Activa        bool            `json:"activa" doc:"Solo las activas se pueden agregar a una orden; los formularios ya agregados siguen"`
	PorDefecto    bool            `json:"porDefecto" doc:"Es la plantilla por defecto de un tipo de orden (no se puede desactivar)"`
	Borrador      bool            `json:"borrador" required:"false" doc:"Nunca se activó: editarla reescribe su versión. Si es false, toda edición crea una versión nueva"`
	Version       int32           `json:"version" doc:"Versión de la fila (control de concurrencia: editar, activar y desactivar la piden)"`
	VersionID     int64           `json:"versionId" doc:"Versión de la plantilla de estos ítems"`
	NumeroVersion int32           `json:"numeroVersion" doc:"Número de la versión de estos ítems (1, 2, …)"`
	VersionActual int32           `json:"versionActual" doc:"Número de la versión actual (la que se agrega a las órdenes nuevas)"`
	Items         []ItemPlantilla `json:"items"`
	CreadoEn      time.Time       `json:"creadoEn"`
	ActualizadoEn time.Time       `json:"actualizadoEn"`
}

// VersionPlantilla resume una versión.
type VersionPlantilla struct {
	ID            int64     `json:"id"`
	Numero        int32     `json:"numero"`
	Nombre        string    `json:"nombre"`
	RequiereFirma bool      `json:"requiereFirma"`
	CreadaPor     *int64    `json:"creadaPor" nullable:"true"`
	CreadaEn      time.Time `json:"creadaEn"`
	Formularios   int64     `json:"formularios" doc:"Formularios de órdenes que la usan (con uso, editar crea otra versión)"`
}

// PlantillaFormularioDetalle es la plantilla con su historial de versiones.
type PlantillaFormularioDetalle struct {
	PlantillaFormulario
	Versiones []VersionPlantilla `json:"versiones" doc:"De la más nueva a la más vieja"`
}

// EntradaItemPlantilla es un ítem al crear o editar una plantilla (posición = orden en la lista).
type EntradaItemPlantilla struct {
	Codigo      string   `json:"codigo" pattern:"^[a-z0-9_]{1,60}$" doc:"Único en la plantilla (las fotos se suben con claveChecklist = codigo)"`
	Etiqueta    string   `json:"etiqueta" minLength:"1" maxLength:"200"`
	Tipo        string   `json:"tipo" enum:"si_no,texto,numero,foto,medicion,seleccion"`
	Obligatorio bool     `json:"obligatorio,omitempty" doc:"El cierre lo exige"`
	Unidad      string   `json:"unidad,omitempty" maxLength:"20" doc:"numero y medicion (dBm, Mbps…)"`
	Opciones    []string `json:"opciones,omitempty" maxItems:"50" doc:"Solo seleccion (al menos una; sin repetir)"`
}

// EntradaCrearPlantilla es la entrada de plantillas.crear.
type EntradaCrearPlantilla struct {
	Codigo        string                 `json:"codigo" pattern:"^[a-z0-9_]{2,60}$"`
	Nombre        string                 `json:"nombre" minLength:"1" maxLength:"120"`
	Descripcion   string                 `json:"descripcion,omitempty" maxLength:"1000"`
	Tipo          *string                `json:"tipo,omitempty" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento" doc:"Tipo de orden; ausente = cualquiera"`
	RequiereFirma bool                   `json:"requiereFirma,omitempty"`
	Comun         bool                   `json:"comun,omitempty" doc:"Común a todas las empresas (exige empresas.todas con empresas registradas); por defecto, de la empresa activa"`
	Borrador      bool                   `json:"borrador,omitempty" doc:"Nace desactivada: mientras nunca se haya activado, editarla reescribe la versión 1 (sin versiones nuevas). Activa (por defecto) o activada alguna vez, toda edición crea una versión"`
	Items         []EntradaItemPlantilla `json:"items" minItems:"1" maxItems:"100"`
}

// EntradaEditarPlantilla es la entrada de plantillas.editar: reemplaza la definición completa.
type EntradaEditarPlantilla struct {
	ID            int64                  `json:"-"`
	Version       int32                  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Nombre        string                 `json:"nombre" minLength:"1" maxLength:"120"`
	Descripcion   string                 `json:"descripcion,omitempty" maxLength:"1000"`
	Tipo          *string                `json:"tipo,omitempty" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento" doc:"Ausente = cualquiera"`
	RequiereFirma bool                   `json:"requiereFirma,omitempty"`
	Items         []EntradaItemPlantilla `json:"items" minItems:"1" maxItems:"100"`
}

// EntradaVersionPlantilla es la entrada de plantillas.activar y plantillas.desactivar.
type EntradaVersionPlantilla struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
}

// EntradaListarPlantillas filtra el catálogo (por nombre, paginado por cursor).
type EntradaListarPlantillas struct {
	nucleo.ParamsPagina
	Tipo  string `query:"tipo" enum:"instalacion,visita,retiro,cambio_equipo,mantenimiento" doc:"Las de ese tipo y las de cualquier tipo"`
	Q     string `query:"q" maxLength:"120" doc:"Busca en nombre y código"`
	Todas bool   `query:"todas" doc:"Incluye las inactivas"`
}

// EntradaVerPlantilla: la plantilla con la versión pedida (por defecto, la actual).
type EntradaVerPlantilla struct {
	ID            int64 `path:"id" minimum:"1"`
	NumeroVersion int32 `query:"numeroVersion" minimum:"0" doc:"Número de versión; 0 o ausente = la actual"`
}

func plantillaNoEncontrada() *nucleo.Problema { return nucleo.NoEncontrado("La plantilla no existe") }

// plantillaVisible: común (empresa null) o de la empresa activa (el sistema ve todas).
func plantillaVisible(ctx context.Context, empresa *int64) bool {
	todas, id := filtroEmpresa(ctx)
	return todas || empresa == nil || mismoID(empresa, id)
}

// plantillaSirveA: la plantilla se puede usar en una orden de esa empresa.
func plantillaSirveA(p campobd.CampoPlantillasChecklist, empresaOrden *int64) bool {
	return p.EmpresaID == nil || mismoID(p.EmpresaID, empresaOrden)
}

// exigirGlobal: administrar plantillas exige el permiso sin límite de zona ni cuadrilla.
func exigirGlobal(ctx context.Context, permiso nucleo.Permiso) error {
	if contexto.PermisosDe(ctx).TieneGlobal(string(permiso)) {
		return nil
	}
	return nucleo.Prohibido(fmt.Sprintf("Administrar plantillas exige %s sin límite de zona o cuadrilla", permiso))
}

// validarItems revisa la lista y la devuelve como JSON para CrearItems.
func validarItems(items []EntradaItemPlantilla) ([]byte, error) {
	if len(items) == 0 {
		return nil, campoObligatorio("items")
	}
	if len(items) > 100 {
		return nil, campoInvalido("items", "Hasta 100 ítems", "maxItems")
	}
	type fila struct {
		Posicion    int             `json:"posicion"`
		Codigo      string          `json:"codigo"`
		Etiqueta    string          `json:"etiqueta"`
		Tipo        string          `json:"tipo"`
		Obligatorio bool            `json:"obligatorio"`
		Unidad      string          `json:"unidad"`
		Opciones    json.RawMessage `json:"opciones"`
	}
	filas := make([]fila, 0, len(items))
	vistos := map[string]bool{}
	for i, it := range items {
		campo := fmt.Sprintf("items[%d]", i)
		codigo := strings.TrimSpace(it.Codigo)
		if !codigoItem.MatchString(codigo) {
			return nil, campoInvalido(campo+".codigo", "Solo minúsculas, números y _ (hasta 60)", "pattern")
		}
		if vistos[codigo] {
			return nil, campoInvalido(campo+".codigo", "Código repetido en la plantilla", "unico")
		}
		vistos[codigo] = true
		etiqueta, err := texto(campo+".etiqueta", it.Etiqueta, 1, 200)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(TiposItem, it.Tipo) {
			return nil, campoInvalido(campo+".tipo", "Tipo de ítem desconocido", "enum")
		}
		unidad := strings.TrimSpace(it.Unidad)
		if len([]rune(unidad)) > 20 {
			return nil, campoInvalido(campo+".unidad", "Hasta 20 caracteres", "length")
		}
		opciones := []string{}
		switch {
		case it.Tipo == ItemSeleccion:
			if len(it.Opciones) == 0 || len(it.Opciones) > 50 {
				return nil, campoInvalido(campo+".opciones", "Una selección lleva entre 1 y 50 opciones", "opciones")
			}
			for j, o := range it.Opciones {
				v, err := texto(fmt.Sprintf("%s.opciones[%d]", campo, j), o, 1, 100)
				if err != nil {
					return nil, err
				}
				if slices.Contains(opciones, v) {
					return nil, campoInvalido(fmt.Sprintf("%s.opciones[%d]", campo, j), "Opción repetida", "unico")
				}
				opciones = append(opciones, v)
			}
		case len(it.Opciones) > 0:
			return nil, campoInvalido(campo+".opciones", "Solo los ítems de selección llevan opciones", "opciones")
		}
		crudo, _ := json.Marshal(opciones)
		filas = append(filas, fila{Posicion: i + 1, Codigo: codigo, Etiqueta: etiqueta, Tipo: it.Tipo,
			Obligatorio: it.Obligatorio, Unidad: unidad, Opciones: crudo})
	}
	return json.Marshal(filas)
}

func validarTipoPlantilla(tipo *string) (*string, error) {
	if tipo == nil || strings.TrimSpace(*tipo) == "" {
		return nil, nil
	}
	if !slices.Contains([]string{"instalacion", "visita", "retiro", "cambio_equipo", "mantenimiento"}, *tipo) {
		return nil, campoInvalido("tipo", "Tipo de orden desconocido", "enum")
	}
	return tipo, nil
}

// leerPlantilla arma la plantilla con los ítems de una versión.
func leerPlantilla(ctx context.Context, q *campobd.Queries, p campobd.CampoPlantillasChecklist, versionID int64) (PlantillaFormulario, error) {
	v, err := q.VersionPorID(ctx, versionID)
	if err != nil {
		return PlantillaFormulario{}, err
	}
	actual := v
	if p.VersionActualID != v.ID {
		if actual, err = q.VersionPorID(ctx, p.VersionActualID); err != nil {
			return PlantillaFormulario{}, err
		}
	}
	items, err := q.ItemsDeVersiones(ctx, []int64{v.ID})
	if err != nil {
		return PlantillaFormulario{}, err
	}
	porDefecto, err := q.EsPlantillaDeTipo(ctx, p.ID)
	if err != nil {
		return PlantillaFormulario{}, err
	}
	salida := PlantillaFormulario{ID: p.ID, Codigo: p.Codigo, Nombre: v.Nombre, Descripcion: p.Descripcion, Tipo: p.TipoCodigo,
		EmpresaID: p.EmpresaID, RequiereFirma: v.RequiereFirma, Activa: p.Activa, PorDefecto: porDefecto, Borrador: !p.AlgunaVezActiva, Version: p.Version,
		VersionID: v.ID, NumeroVersion: v.Numero, VersionActual: actual.Numero, Items: make([]ItemPlantilla, 0, len(items)),
		CreadoEn: p.CreatedAt, ActualizadoEn: p.UpdatedAt}
	for _, it := range items {
		salida.Items = append(salida.Items, aItemPlantilla(it))
	}
	return salida, nil
}

// plantillaConTipo: una plantilla del catálogo con su tipo (para agrupar por tipo de orden).
type plantillaConTipo struct {
	Plantilla
	TipoCodigo *string
}

// catalogoActivo: las plantillas activas que ve la empresa activa, con los ítems de su versión
// actual (catálogo de /tipos-orden y del paquete de campo).
func catalogoActivo(ctx context.Context, q *campobd.Queries) ([]plantillaConTipo, error) {
	formularios, err := catalogoFormularios(ctx, q)
	if err != nil {
		return nil, err
	}
	salida := make([]plantillaConTipo, 0, len(formularios))
	for _, p := range formularios {
		salida = append(salida, plantillaConTipo{TipoCodigo: p.Tipo, Plantilla: Plantilla{ID: p.ID, Codigo: p.Codigo,
			Nombre: p.Nombre, RequiereFirma: p.RequiereFirma, Items: p.Items}})
	}
	return salida, nil
}

// catalogoFormularios: las plantillas activas visibles, completas (versión actual).
func catalogoFormularios(ctx context.Context, q *campobd.Queries) ([]PlantillaFormulario, error) {
	todas, empresa := filtroEmpresa(ctx)
	filas, err := q.PlantillasVisibles(ctx, campobd.PlantillasVisiblesParams{TodasEmpresas: todas, EmpresaID: empresa, SoloActivas: true})
	if err != nil {
		return nil, err
	}
	return conItems(ctx, q, filas)
}

// conItems completa las plantillas con su versión actual y sus ítems (pocas consultas).
func conItems(ctx context.Context, q *campobd.Queries, filas []campobd.CampoPlantillasChecklist) ([]PlantillaFormulario, error) {
	versionIDs := make([]int64, 0, len(filas))
	for _, p := range filas {
		versionIDs = append(versionIDs, p.VersionActualID)
	}
	versiones, err := q.VersionesPorIDs(ctx, versionIDs)
	if err != nil {
		return nil, err
	}
	numero := map[int64]int32{}
	for _, v := range versiones {
		numero[v.ID] = v.Numero
	}
	items, err := q.ItemsDeVersiones(ctx, versionIDs)
	if err != nil {
		return nil, err
	}
	tipos, err := q.ListarTipos(ctx)
	if err != nil {
		return nil, err
	}
	salida := make([]PlantillaFormulario, 0, len(filas))
	for _, p := range filas {
		o := PlantillaFormulario{ID: p.ID, Codigo: p.Codigo, Nombre: p.Nombre, Descripcion: p.Descripcion, Tipo: p.TipoCodigo,
			EmpresaID: p.EmpresaID, RequiereFirma: p.RequiereFirma, Activa: p.Activa, Borrador: !p.AlgunaVezActiva, Version: p.Version,
			VersionID: p.VersionActualID, NumeroVersion: numero[p.VersionActualID], VersionActual: numero[p.VersionActualID],
			Items: []ItemPlantilla{}, CreadoEn: p.CreatedAt, ActualizadoEn: p.UpdatedAt}
		for _, tp := range tipos {
			if tp.Activo && tp.PlantillaID != nil && *tp.PlantillaID == p.ID {
				o.PorDefecto = true
			}
		}
		for _, it := range items {
			if it.VersionID == p.VersionActualID {
				o.Items = append(o.Items, aItemPlantilla(it))
			}
		}
		salida = append(salida, o)
	}
	return salida, nil
}

func (m *Modulo) nuevoListarPlantillas() *auditoria.Caso[EntradaListarPlantillas, nucleo.Pagina[PlantillaFormulario]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarPlantillas, nucleo.Pagina[PlantillaFormulario]]{
		Accion:       "plantillas.ver",
		Permiso:      nucleo.PermPlantillasVer,
		AuditarExito: soloLecturas[nucleo.Pagina[PlantillaFormulario]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarPlantillas) (nucleo.Pagina[PlantillaFormulario], error) {
			q := campobd.New(t)
			todas, empresa := filtroEmpresa(ctx)
			p := campobd.PaginaPlantillasParams{TodasEmpresas: todas, EmpresaID: empresa, SoloActivas: !in.Todas,
				Limite: int32(in.Limite() + 1)} //nolint:gosec // ≤ 201
			var nombre string
			var id int64
			hay, err := nucleo.DecodificarCursor(in.Cursor, &nombre, &id)
			if err != nil {
				return nucleo.Pagina[PlantillaFormulario]{}, err
			}
			if hay {
				p.DespuesNombre, p.DespuesID = &nombre, &id
			}
			if in.Tipo != "" {
				p.Tipo = &in.Tipo
			}
			if qq := strings.TrimSpace(in.Q); qq != "" {
				p.Q = &qq
			}
			filas, err := q.PaginaPlantillas(ctx, p)
			if err != nil {
				return nucleo.Pagina[PlantillaFormulario]{}, err
			}
			pagina := nucleo.Paginar(filas, in.Limite(), func(f campobd.CampoPlantillasChecklist) campobd.CampoPlantillasChecklist { return f },
				func(f campobd.CampoPlantillasChecklist) []any { return []any{f.Nombre, f.ID} })
			salida, err := conItems(ctx, q, pagina.Data)
			return nucleo.Pagina[PlantillaFormulario]{Data: salida, NextCursor: pagina.NextCursor}, err
		},
	})
}

func (m *Modulo) nuevoVerPlantilla() *auditoria.Caso[EntradaVerPlantilla, PlantillaFormularioDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerPlantilla, PlantillaFormularioDetalle]{
		Accion:       "plantillas.ver",
		Permiso:      nucleo.PermPlantillasVer,
		AuditarExito: soloLecturas[PlantillaFormularioDetalle],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerPlantilla) (PlantillaFormularioDetalle, error) {
			q := campobd.New(t)
			t.FijarEntidad("plantilla", in.ID)
			p, err := q.PlantillaPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !plantillaVisible(ctx, p.EmpresaID)) {
				return PlantillaFormularioDetalle{}, plantillaNoEncontrada()
			}
			if err != nil {
				return PlantillaFormularioDetalle{}, err
			}
			version := p.VersionActualID
			if in.NumeroVersion > 0 {
				v, err := q.VersionPorNumero(ctx, campobd.VersionPorNumeroParams{PlantillaID: p.ID, Numero: in.NumeroVersion})
				if errors.Is(err, pgx.ErrNoRows) {
					return PlantillaFormularioDetalle{}, nucleo.NoEncontrado("La plantilla no tiene esa versión")
				}
				if err != nil {
					return PlantillaFormularioDetalle{}, err
				}
				version = v.ID
			}
			base, err := leerPlantilla(ctx, q, p, version)
			if err != nil {
				return PlantillaFormularioDetalle{}, err
			}
			versiones, err := q.VersionesDePlantilla(ctx, p.ID)
			if err != nil {
				return PlantillaFormularioDetalle{}, err
			}
			d := PlantillaFormularioDetalle{PlantillaFormulario: base, Versiones: make([]VersionPlantilla, 0, len(versiones))}
			for _, v := range versiones {
				d.Versiones = append(d.Versiones, VersionPlantilla{ID: v.ID, Numero: v.Numero, Nombre: v.Nombre,
					RequiereFirma: v.RequiereFirma, CreadaPor: v.CreadaPor, CreadaEn: v.CreatedAt, Formularios: v.Formularios})
			}
			return d, nil
		},
	})
}

func (m *Modulo) nuevoCrearPlantilla() *auditoria.Caso[EntradaCrearPlantilla, PlantillaFormulario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearPlantilla, PlantillaFormulario]{
		Accion:  "plantillas.crear",
		Permiso: nucleo.PermPlantillasCrear,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearPlantilla) (PlantillaFormulario, error) {
			if err := sinNulos(in); err != nil {
				return PlantillaFormulario{}, err
			}
			if err := exigirGlobal(ctx, nucleo.PermPlantillasCrear); err != nil {
				return PlantillaFormulario{}, err
			}
			q := campobd.New(t)
			codigo := strings.TrimSpace(in.Codigo)
			if !codigoValido.MatchString(codigo) {
				return PlantillaFormulario{}, campoInvalido("codigo", "Solo minúsculas, números y _ (2 a 60)", "pattern")
			}
			nombre, err := texto("nombre", in.Nombre, 1, 120)
			if err != nil {
				return PlantillaFormulario{}, err
			}
			descripcion := strings.TrimSpace(in.Descripcion)
			tipo, err := validarTipoPlantilla(in.Tipo)
			if err != nil {
				return PlantillaFormulario{}, err
			}
			items, err := validarItems(in.Items)
			if err != nil {
				return PlantillaFormulario{}, err
			}
			var empresa *int64
			if in.Comun {
				if !agenda.PuedeTocarLoComun(ctx) {
					return PlantillaFormulario{}, nucleo.Prohibido("Una plantilla común a todas las empresas solo la crea un administrador global (empresas.todas)")
				}
			} else {
				todas, id := filtroEmpresa(ctx)
				switch {
				case todas:
					// Sin empresa activa (modo «todas»), común solo si se pide: nunca por omisión.
					return PlantillaFormulario{}, nucleo.NuevoProblema(http.StatusUnprocessableEntity, "empresa_requerida", "Falta la empresa",
						"Elige una empresa activa (X-Iris-Empresa) para la plantilla o márcala como común (comun: true)")
				case id != nil && *id == 0:
					return PlantillaFormulario{}, nucleo.EmpresaRequerida()
				default:
					empresa = id
				}
			}
			t.FijarEmpresa(deref(empresa))
			p, err := q.CrearPlantilla(ctx, campobd.CrearPlantillaParams{Codigo: codigo, Nombre: nombre, Descripcion: descripcion,
				TipoCodigo: tipo, RequiereFirma: in.RequiereFirma, EmpresaID: empresa, CreadoPor: actorUsuario(t), Activa: !in.Borrador})
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return PlantillaFormulario{}, nucleo.ReglaNegocio("codigo_duplicado", "Ya hay una plantilla con ese código")
			}
			if err != nil {
				return PlantillaFormulario{}, err
			}
			t.FijarEntidad("plantilla", p.ID)
			v, err := q.CrearVersion(ctx, campobd.CrearVersionParams{PlantillaID: p.ID, Nombre: nombre, RequiereFirma: in.RequiereFirma,
				CreadaPor: actorUsuario(t), CreadaEn: m.ahora()})
			if err != nil {
				return PlantillaFormulario{}, err
			}
			if err := q.CrearItems(ctx, campobd.CrearItemsParams{PlantillaID: p.ID, VersionID: v.ID, Items: items}); err != nil {
				return PlantillaFormulario{}, err
			}
			if p, err = q.FijarVersionActual(ctx, campobd.FijarVersionActualParams{ID: p.ID, VersionID: v.ID, Nombre: nombre,
				RequiereFirma: in.RequiereFirma}); err != nil {
				return PlantillaFormulario{}, err
			}
			salida, err := leerPlantilla(ctx, q, p, v.ID)
			if err != nil {
				return PlantillaFormulario{}, err
			}
			t.RegistrarCambios("plantilla", p.ID, nil, salida)
			return salida, t.Emitir("plantilla.creada", map[string]any{"plantilla_id": p.ID, "codigo": p.Codigo,
				"empresa_id": p.EmpresaID, "version_id": v.ID, "numero_version": v.Numero})
		},
	})
}

// tomarPlantilla: con candado, visible en la empresa, modificable por el actor y con la versión
// leída.
func tomarPlantilla(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, id int64, version int32) (campobd.CampoPlantillasChecklist, error) {
	t.FijarEntidad("plantilla", id)
	p, err := q.PlantillaPorIDParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !plantillaVisible(ctx, p.EmpresaID)) {
		return p, plantillaNoEncontrada()
	}
	if err != nil {
		return p, err
	}
	t.FijarEmpresa(deref(p.EmpresaID))
	if p.EmpresaID == nil && !agenda.PuedeTocarLoComun(ctx) {
		return p, nucleo.Prohibido("Las plantillas comunes a todas las empresas solo las modifica un administrador global (empresas.todas)")
	}
	return p, nucleo.ExigirVersion(version, p.Version, "plantilla", id)
}

func (m *Modulo) nuevoEditarPlantilla() *auditoria.Caso[EntradaEditarPlantilla, PlantillaFormulario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEditarPlantilla, PlantillaFormulario]{
		Accion:  "plantillas.editar",
		Permiso: nucleo.PermPlantillasEditar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEditarPlantilla) (PlantillaFormulario, error) {
			if err := sinNulos(in); err != nil {
				return PlantillaFormulario{}, err
			}
			if err := exigirGlobal(ctx, nucleo.PermPlantillasEditar); err != nil {
				return PlantillaFormulario{}, err
			}
			q := campobd.New(t)
			antes, err := tomarPlantilla(ctx, t, q, in.ID, in.Version)
			if err != nil {
				return PlantillaFormulario{}, err
			}
			nombre, err := texto("nombre", in.Nombre, 1, 120)
			if err != nil {
				return PlantillaFormulario{}, err
			}
			tipo, err := validarTipoPlantilla(in.Tipo)
			if err != nil {
				return PlantillaFormulario{}, err
			}
			items, err := validarItems(in.Items)
			if err != nil {
				return PlantillaFormulario{}, err
			}
			previa, err := leerPlantilla(ctx, q, antes, antes.VersionActualID)
			if err != nil {
				return PlantillaFormulario{}, err
			}
			// Versión nueva si la plantilla estuvo activa alguna vez (la actual ya viajó en el
			// catálogo de los teléfonos: cambiarla en el lugar movería obligatorios sin aviso) o si
			// la versión ya está en alguna orden (lo respondido no cambia). Solo un borrador que
			// nunca se activó ni se usó se reescribe.
			enUso := antes.AlgunaVezActiva
			if !enUso {
				if enUso, err = q.VersionEnUso(ctx, antes.VersionActualID); err != nil {
					return PlantillaFormulario{}, err
				}
			}
			var v campobd.CampoPlantillaVersion
			if enUso {
				v, err = q.CrearVersion(ctx, campobd.CrearVersionParams{PlantillaID: antes.ID, Nombre: nombre,
					RequiereFirma: in.RequiereFirma, CreadaPor: actorUsuario(t), CreadaEn: m.ahora()})
			} else {
				if err = q.BorrarItemsDeVersion(ctx, antes.VersionActualID); err == nil {
					v, err = q.ReescribirVersion(ctx, campobd.ReescribirVersionParams{ID: antes.VersionActualID, Nombre: nombre,
						RequiereFirma: in.RequiereFirma})
				}
			}
			if err != nil {
				return PlantillaFormulario{}, err
			}
			if err := q.CrearItems(ctx, campobd.CrearItemsParams{PlantillaID: antes.ID, VersionID: v.ID, Items: items}); err != nil {
				return PlantillaFormulario{}, err
			}
			p, err := q.GuardarPlantilla(ctx, campobd.GuardarPlantillaParams{ID: antes.ID, Version: antes.Version, Nombre: nombre,
				Descripcion: strings.TrimSpace(in.Descripcion), TipoCodigo: tipo, RequiereFirma: in.RequiereFirma,
				Activa: antes.Activa, VersionActualID: v.ID})
			if err != nil {
				return PlantillaFormulario{}, nucleo.VerificarVersion(err, "plantilla", antes.ID)
			}
			salida, err := leerPlantilla(ctx, q, p, v.ID)
			if err != nil {
				return PlantillaFormulario{}, err
			}
			t.RegistrarCambios("plantilla", p.ID, previa, salida)
			t.Anotar(map[string]any{"version_nueva": enUso, "numero_version": v.Numero})
			return salida, t.Emitir("plantilla.actualizada", map[string]any{"plantilla_id": p.ID, "version_id": v.ID,
				"numero_version": v.Numero, "version_nueva": enUso, "activa": p.Activa})
		},
	})
}

// nuevoActivarPlantilla arma plantillas.activar (activa = true) o plantillas.desactivar.
func (m *Modulo) nuevoActivarPlantilla(activa bool) *auditoria.Caso[EntradaVersionPlantilla, PlantillaFormulario] {
	accion, permiso, evento := "plantillas.activar", nucleo.PermPlantillasActivar, "plantilla.activada"
	if !activa {
		accion, permiso, evento = "plantillas.desactivar", nucleo.PermPlantillasDesactivar, "plantilla.desactivada"
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaVersionPlantilla, PlantillaFormulario]{
		Accion:  accion,
		Permiso: permiso,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVersionPlantilla) (PlantillaFormulario, error) {
			if err := exigirGlobal(ctx, permiso); err != nil {
				return PlantillaFormulario{}, err
			}
			q := campobd.New(t)
			antes, err := tomarPlantilla(ctx, t, q, in.ID, in.Version)
			if err != nil {
				return PlantillaFormulario{}, err
			}
			if antes.Activa == activa {
				return PlantillaFormulario{}, nucleo.ReglaNegocio("sin_cambios", "La plantilla ya está así")
			}
			if !activa {
				porDefecto, err := q.EsPlantillaDeTipo(ctx, antes.ID)
				if err != nil {
					return PlantillaFormulario{}, err
				}
				if porDefecto {
					return PlantillaFormulario{}, nucleo.ReglaNegocio("plantilla_por_defecto",
						"Es la plantilla por defecto de un tipo de orden: no se puede desactivar")
				}
			}
			p, err := q.GuardarPlantilla(ctx, campobd.GuardarPlantillaParams{ID: antes.ID, Version: antes.Version, Nombre: antes.Nombre,
				Descripcion: antes.Descripcion, TipoCodigo: antes.TipoCodigo, RequiereFirma: antes.RequiereFirma, Activa: activa,
				VersionActualID: antes.VersionActualID})
			if err != nil {
				return PlantillaFormulario{}, nucleo.VerificarVersion(err, "plantilla", antes.ID)
			}
			t.RegistrarCambios("plantilla", p.ID, map[string]any{"activa": antes.Activa}, map[string]any{"activa": p.Activa})
			salida, err := leerPlantilla(ctx, q, p, p.VersionActualID)
			if err != nil {
				return PlantillaFormulario{}, err
			}
			return salida, t.Emitir(evento, map[string]any{"plantilla_id": p.ID})
		},
	})
}
