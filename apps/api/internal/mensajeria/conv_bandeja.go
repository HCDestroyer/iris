package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Bandeja: lista con filtros, contadores, detalle, mensajes, lectura y las acciones de asignación,
// cola, etiquetas y estado (contratos/conversaciones.md §7).

// Lista y contadores ---------------------------------------------------------------------------------

// EntradaListarConvs es GET /conversaciones.
type EntradaListarConvs struct {
	nucleo.ParamsPagina
	Vista      string `query:"vista" enum:"mias,sin_asignar,todas" default:"todas"`
	CanalID    int64  `query:"canalId" minimum:"0"`
	ColaID     int64  `query:"colaId" minimum:"0"`
	EtiquetaID int64  `query:"etiquetaId" minimum:"0"`
	Estado     string `query:"estado" enum:"abierta,esperando_cliente,resuelta,activas,todas" default:"activas"`
	SLA        string `query:"sla" enum:"por_vencer,vencido"`
	Q          string `query:"q" maxLength:"80" doc:"Nombre, número o referencia (TK-004812)"`
}

func ptrSiPositivo(n int64) *int64 {
	if n <= 0 {
		return nil
	}
	return &n
}

func nuevoListarConvs(m *Modulo) *auditoria.Caso[EntradaListarConvs, nucleo.Pagina[ConversacionCliente]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarConvs, nucleo.Pagina[ConversacionCliente]]{
		Accion: "conversaciones.listar", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[nucleo.Pagina[ConversacionCliente]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarConvs) (nucleo.Pagina[ConversacionCliente], error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return nucleo.Pagina[ConversacionCliente]{}, err
			}
			var actividad string
			var ultimo int64
			con, err := nucleo.DecodificarCursor(in.Cursor, &actividad, &ultimo)
			if err != nil {
				return nucleo.Pagina[ConversacionCliente]{}, err
			}
			var cursorAct time.Time
			if con {
				if cursorAct, err = time.Parse(time.RFC3339Nano, actividad); err != nil {
					return nucleo.Pagina[ConversacionCliente]{}, campoConv("cursor", "Cursor inválido", "cursor")
				}
			}
			if in.Vista == "" {
				in.Vista = "todas"
			}
			if in.Estado == "" {
				in.Estado = "activas"
			}
			q := strings.TrimSpace(in.Q)
			digitos := soloDigitos(q)
			if len(digitos) < 3 {
				digitos = ""
			}
			limite := in.Limite()
			bd := mensajeriabd.New(t)
			filas, err := bd.ListarConversacionesCliente(ctx, mensajeriabd.ListarConversacionesClienteParams{Empresa: a.empresa, Usuario: a.usuario,
				Supervisa: a.supervisa, Vista: in.Vista, Estado: in.Estado, CanalID: ptrSiPositivo(in.CanalID), ColaID: ptrSiPositivo(in.ColaID),
				EtiquetaID: ptrSiPositivo(in.EtiquetaID), Sla: in.SLA, Ahora: m.ahora(), Q: escaparLike(q), QDigitos: digitos, ConCursor: con,
				CursorActividad: cursorAct, CursorID: ultimo, Limite: int32(limite + 1)}) //nolint:gosec // limite ≤ 200
			if err != nil {
				return nucleo.Pagina[ConversacionCliente]{}, err
			}
			hayMas := len(filas) > limite
			if hayMas {
				filas = filas[:limite]
			}
			ids := make([]int64, 0, len(filas))
			for _, f := range filas {
				ids = append(ids, f.ID)
			}
			cs, err := m.resumirConvs(ctx, bd, a.usuario, ids)
			if err != nil {
				return nucleo.Pagina[ConversacionCliente]{}, err
			}
			p := nucleo.Pagina[ConversacionCliente]{Data: cs}
			if hayMas && len(filas) > 0 {
				u := filas[len(filas)-1]
				c := nucleo.CodificarCursor(u.UltimaActividad.UTC().Format(time.RFC3339Nano), u.ID)
				p.NextCursor = &c
			}
			return p, nil
		},
	})
}

func escaparLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ConteoGrupo es un total por número, cola o etiqueta.
type ConteoGrupo struct {
	ID    int64 `json:"id"`
	Total int64 `json:"total"`
}

// ContadoresConversaciones son los números de la bandeja (sobre las activas que ve el usuario).
type ContadoresConversaciones struct {
	Mias         int64            `json:"mias"`
	SinAsignar   int64            `json:"sinAsignar"`
	Todas        int64            `json:"todas"`
	NoLeidosMias int64            `json:"noLeidosMias" doc:"Mensajes del cliente sin leer en las asignadas a ti"`
	PorVencer    int64            `json:"porVencer"`
	Vencidas     int64            `json:"vencidas"`
	PorEstado    map[string]int64 `json:"porEstado"`
	PorCanal     []ConteoGrupo    `json:"porCanal"`
	PorCola      []ConteoGrupo    `json:"porCola"`
	PorEtiqueta  []ConteoGrupo    `json:"porEtiqueta"`
}

func nuevoContadores(m *Modulo) *auditoria.Caso[struct{}, ContadoresConversaciones] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ContadoresConversaciones]{
		Accion: "conversaciones.contadores", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[ContadoresConversaciones],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ContadoresConversaciones, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ContadoresConversaciones{}, err
			}
			q := mensajeriabd.New(t)
			f, err := q.ContadoresConversaciones(ctx, mensajeriabd.ContadoresConversacionesParams{Usuario: a.usuario, Ahora: m.ahora(), Empresa: a.empresa, Supervisa: a.supervisa})
			if err != nil {
				return ContadoresConversaciones{}, err
			}
			out := ContadoresConversaciones{Mias: f.Mias, SinAsignar: f.SinAsignar, Todas: f.Todas, NoLeidosMias: f.NoLeidosMias, PorVencer: f.PorVencer,
				Vencidas: f.Vencidas, PorEstado: map[string]int64{EstadoConvAbierta: f.Abiertas, EstadoConvEsperando: f.Esperando},
				PorCanal: []ConteoGrupo{}, PorCola: []ConteoGrupo{}, PorEtiqueta: []ConteoGrupo{}}
			gs, err := q.ContadoresPorGrupo(ctx, mensajeriabd.ContadoresPorGrupoParams{Empresa: a.empresa, Usuario: a.usuario, Supervisa: a.supervisa})
			if err != nil {
				return ContadoresConversaciones{}, err
			}
			for _, g := range gs {
				c := ConteoGrupo{ID: g.ID, Total: g.Total}
				switch g.Grupo {
				case "canal":
					out.PorCanal = append(out.PorCanal, c)
				case "cola":
					out.PorCola = append(out.PorCola, c)
				case "etiqueta":
					out.PorEtiqueta = append(out.PorEtiqueta, c)
				}
			}
			return out, nil
		},
	})
}

// Catálogos -----------------------------------------------------------------------------------------

// NumeroConversaciones es un número de la empresa visto desde la bandeja (sin credenciales).
type NumeroConversaciones struct {
	ID            int64    `json:"id"`
	Nombre        string   `json:"nombre"`
	Numero        string   `json:"numero"`
	Usos          []string `json:"usos"`
	AvisosEmpresa bool     `json:"avisosEmpresa"`
	ColaID        *int64   `json:"colaId" nullable:"true"`
	Activo        bool     `json:"activo"`
	Version       int32    `json:"version" doc:"Para POST /canales/{id}/configurar"`
	HorarioID     *int64   `json:"horarioId" nullable:"true"`
	// FueraDeHorario: lo que hace el número con el horario cerrado.
	FueraDeHorario FueraDeHorario `json:"fueraDeHorario"`
	// Bot: lo que hace el número cuando no hay agentes (§14.6).
	Bot     BotNumero `json:"bot"`
	Iniciar bool      `json:"iniciar" doc:"Puedes iniciar conversaciones desde este número (conversaciones.iniciar y número de una de tus colas)"`
	// PrestadoPor: el número es de otra empresa y tu empresa lo usa (usos y cola del préstamo, §15.3).
	PrestadoPor *EmpresaRef `json:"prestadoPor,omitempty" doc:"Número prestado por otra empresa: sus usos y su cola son los del préstamo"`
}

// ListaNumeros es { data, nextCursor }.
type ListaNumeros struct {
	Data       []NumeroConversaciones `json:"data"`
	NextCursor *string                `json:"nextCursor" nullable:"true"`
}

func nuevoListarNumeros(*Modulo) *auditoria.Caso[struct{}, ListaNumeros] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ListaNumeros]{
		Accion: "conversaciones.listar_numeros", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[ListaNumeros],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ListaNumeros, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaNumeros{}, err
			}
			q := mensajeriabd.New(t)
			fs, err := q.CanalesDeEmpresa(ctx, a.empresa)
			if err != nil {
				return ListaNumeros{}, err
			}
			out := ListaNumeros{Data: make([]NumeroConversaciones, 0, len(fs))}
			for _, f := range fs {
				inicia, err := puedeIniciarDesde(ctx, q, a, f)
				if err != nil {
					return ListaNumeros{}, err
				}
				out.Data = append(out.Data, NumeroConversaciones{ID: f.ID, Nombre: f.Nombre, Numero: f.Numero, Usos: f.Usos, AvisosEmpresa: f.AvisosEmpresa,
					ColaID: f.ColaID, Activo: f.Activo, Version: f.Version,
					HorarioID: f.HorarioID, FueraDeHorario: fueraDeHorarioDe(f.FueraHorario), Bot: botDe(f.Bot),
					Iniciar: inicia && contexto.PermisosDe(ctx).Tiene(string(nucleo.PermConversacionesIniciar))})
			}
			prestados, err := q.CanalesPrestadosAEmpresa(ctx, a.empresa)
			if err != nil {
				return ListaNumeros{}, err
			}
			for _, p := range prestados {
				f := p.MensajeriaCanal
				inicia, err := puedeIniciarDesde(ctx, q, a, f)
				if err != nil {
					return ListaNumeros{}, err
				}
				out.Data = append(out.Data, NumeroConversaciones{ID: f.ID, Nombre: f.Nombre, Numero: f.Numero, Usos: p.MensajeriaCanalPrestamo.Usos,
					AvisosEmpresa: p.MensajeriaCanalPrestamo.AvisosEmpresa, ColaID: p.MensajeriaCanalPrestamo.ColaID, Activo: f.Activo,
					Version: p.MensajeriaCanalPrestamo.Version, FueraDeHorario: FueraDeHorario{}, Bot: botDe([]byte(`{}`)),
					Iniciar:     inicia && contexto.PermisosDe(ctx).Tiene(string(nucleo.PermConversacionesIniciar)),
					PrestadoPor: &EmpresaRef{EmpresaID: derefEmpresa(f.EmpresaID), Nombre: p.DuenoNombre}})
			}
			return out, nil
		},
	})
}

// AgenteConversaciones es un usuario que atiende conversaciones.
type AgenteConversaciones struct {
	ID        int64     `json:"id"`
	Nombre    string    `json:"nombre"`
	Supervisa bool      `json:"supervisa" doc:"Tiene conversaciones.supervisar: ve todas las colas"`
	Colas     []ColaRef `json:"colas" doc:"Colas de las que es miembro"`
	// ColasSupervisadas: colas que supervisa sin el permiso global (§14.1).
	ColasSupervisadas []ColaRef            `json:"colasSupervisadas"`
	Abiertas          int64                `json:"abiertas" doc:"Conversaciones activas (no resueltas) asignadas a esta persona en la empresa"`
	Disponibilidad    DisponibilidadAgente `json:"disponibilidad"`
}

// ListaAgentes es { data, nextCursor }.
type ListaAgentes struct {
	Data       []AgenteConversaciones `json:"data"`
	NextCursor *string                `json:"nextCursor" nullable:"true"`
}

// EntradaAgentes filtra por cola.
type EntradaAgentes struct {
	ColaID int64 `query:"colaId" minimum:"0" doc:"Los miembros y supervisores de la cola y los supervisores globales"`
}

func nuevoListarAgentes(*Modulo) *auditoria.Caso[EntradaAgentes, ListaAgentes] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAgentes, ListaAgentes]{
		Accion: "conversaciones.listar_agentes", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[ListaAgentes],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAgentes) (ListaAgentes, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaAgentes{}, err
			}
			q := mensajeriabd.New(t)
			fs, err := q.AgentesConversaciones(ctx, a.empresa)
			if err != nil {
				return ListaAgentes{}, err
			}
			ids := make([]int64, 0, len(fs))
			for _, f := range fs {
				ids = append(ids, f.ID)
			}
			colas, supervisadas, abiertas := map[int64][]ColaRef{}, map[int64][]ColaRef{}, map[int64]int64{}
			disp := map[int64]DisponibilidadAgente{}
			if len(ids) > 0 {
				cs, err := q.ColasDeUsuarios(ctx, mensajeriabd.ColasDeUsuariosParams{Usuarios: ids, Empresa: a.empresa})
				if err != nil {
					return ListaAgentes{}, err
				}
				for _, c := range cs {
					colas[c.UsuarioID] = append(colas[c.UsuarioID], ColaRef{ID: c.ID, Nombre: c.Nombre})
				}
				ss, err := q.ColasSupervisadasDeUsuarios(ctx, mensajeriabd.ColasSupervisadasDeUsuariosParams{Usuarios: ids, Empresa: a.empresa})
				if err != nil {
					return ListaAgentes{}, err
				}
				for _, c := range ss {
					if c.UsuarioID != nil {
						supervisadas[*c.UsuarioID] = append(supervisadas[*c.UsuarioID], ColaRef{ID: c.ID, Nombre: c.Nombre})
					}
				}
				as, err := q.AbiertasDeUsuarios(ctx, mensajeriabd.AbiertasDeUsuariosParams{Empresa: a.empresa, Usuarios: ids})
				if err != nil {
					return ListaAgentes{}, err
				}
				for _, x := range as {
					abiertas[x.UsuarioID] = x.Total
				}
				if disp, err = disponibilidades(ctx, q, a.empresa, ids); err != nil {
					return ListaAgentes{}, err
				}
			}
			out := ListaAgentes{Data: []AgenteConversaciones{}}
			es := func(c ColaRef) bool { return c.ID == in.ColaID }
			for _, f := range fs {
				cs, ss := colas[f.ID], supervisadas[f.ID]
				if cs == nil {
					cs = []ColaRef{}
				}
				if ss == nil {
					ss = []ColaRef{}
				}
				if in.ColaID > 0 && !f.Supervisa && !slices.ContainsFunc(cs, es) && !slices.ContainsFunc(ss, es) {
					continue
				}
				out.Data = append(out.Data, AgenteConversaciones{ID: f.ID, Nombre: f.Nombre, Supervisa: f.Supervisa, Colas: cs, ColasSupervisadas: ss,
					Abiertas: abiertas[f.ID], Disponibilidad: dispDe(disp, f.ID)})
			}
			return out, nil
		},
	})
}

// PlantillaDisponible es una plantilla aprobada en el número de la conversación.
type PlantillaDisponible struct {
	Codigo      string     `json:"codigo"`
	Nombre      string     `json:"nombre" doc:"Nombre de la plantilla en Meta"`
	Categoria   string     `json:"categoria" enum:"servicio,marketing,autenticacion" doc:"Categoría de Meta (servicio = utility)"`
	Variables   []Variable `json:"variables"`
	VistaPrevia string     `json:"vistaPrevia" doc:"Texto de referencia con {{variable}}"`
}

// ListaPlantillasConv es { data, nextCursor }.
type ListaPlantillasConv struct {
	Data       []PlantillaDisponible `json:"data"`
	NextCursor *string               `json:"nextCursor" nullable:"true"`
}

func nuevoPlantillasConv(*Modulo) *auditoria.Caso[EntradaConversacion, ListaPlantillasConv] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConversacion, ListaPlantillasConv]{
		Accion: "conversaciones.plantillas", Permiso: nucleo.PermConversacionesResponder, AuditarExito: sinAuditar[ListaPlantillasConv],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConversacion) (ListaPlantillasConv, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaPlantillasConv{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return ListaPlantillasConv{}, err
			}
			canal, err := q.CanalPorID(ctx, c.CanalID)
			if err != nil {
				return ListaPlantillasConv{}, err
			}
			return plantillasDisponibles(canal), nil
		},
	})
}

// Detalle, mensajes y lectura -----------------------------------------------------------------------

func nuevoVerConv(m *Modulo) *auditoria.Caso[EntradaConversacion, ConversacionClienteDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConversacion, ConversacionClienteDetalle]{
		Accion: "conversaciones.ver", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[ConversacionClienteDetalle],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConversacion) (ConversacionClienteDetalle, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ConversacionClienteDetalle{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return ConversacionClienteDetalle{}, err
			}
			return m.detalleConv(ctx, q, a.usuario, c)
		},
	})
}

// EntradaMensajesConv es GET /conversaciones/{id}/mensajes.
type EntradaMensajesConv struct {
	ID int64 `path:"id" minimum:"1"`
	nucleo.ParamsPagina
	Direccion string `query:"direccion" enum:"anteriores,siguientes" default:"anteriores"`
	DesdeSeq  int64  `query:"desdeSeq" minimum:"0" doc:"Ancla sin cursor"`
}

func nuevoMensajesConv(*Modulo) *auditoria.Caso[EntradaMensajesConv, nucleo.Pagina[MensajeConversacion]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaMensajesConv, nucleo.Pagina[MensajeConversacion]]{
		Accion: "conversaciones.mensajes", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[nucleo.Pagina[MensajeConversacion]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaMensajesConv) (nucleo.Pagina[MensajeConversacion], error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return nucleo.Pagina[MensajeConversacion]{}, err
			}
			q := mensajeriabd.New(t)
			if _, err := convVisible(ctx, q, a, in.ID, false); err != nil {
				return nucleo.Pagina[MensajeConversacion]{}, err
			}
			adelante := in.Direccion == "siguientes"
			var seq int64
			con, err := nucleo.DecodificarCursor(in.Cursor, &seq)
			if err != nil {
				return nucleo.Pagina[MensajeConversacion]{}, err
			}
			desde := in.DesdeSeq
			if con {
				desde = seq
			} else if !adelante && desde == 0 {
				desde = 1 << 62
			}
			limite := in.Limite()
			fs, err := q.ListarMensajesConversacion(ctx, mensajeriabd.ListarMensajesConversacionParams{ConversacionID: in.ID, Adelante: adelante,
				Desde: desde, Limite: int32(limite + 1)}) //nolint:gosec // limite ≤ 200
			if err != nil {
				return nucleo.Pagina[MensajeConversacion]{}, err
			}
			hayMas := len(fs) > limite
			if hayMas {
				fs = fs[:limite]
			}
			var siguiente *string
			if hayMas && len(fs) > 0 {
				c := nucleo.CodificarCursor(fs[len(fs)-1].Seq)
				siguiente = &c
			}
			if !adelante {
				slices.Reverse(fs)
			}
			data, err := mensajesConv(ctx, q, fs)
			return nucleo.Pagina[MensajeConversacion]{Data: data, NextCursor: siguiente}, err
		},
	})
}

// EntradaLeerConv es POST /conversaciones/{id}/leer.
type EntradaLeerConv struct {
	ID             int64     `json:"-"`
	HastaMensajeID uuid.UUID `json:"hastaMensajeId"`
}

// LecturaConv es tu lectura de la conversación.
type LecturaConv struct {
	ConversacionID int64 `json:"conversacionId"`
	NoLeidos       int64 `json:"noLeidos"`
}

func nuevoLeerConv(m *Modulo) *auditoria.Caso[EntradaLeerConv, LecturaConv] {
	return auditoria.Nuevo(auditoria.Caso[EntradaLeerConv, LecturaConv]{
		Accion: "conversaciones.leer", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[LecturaConv],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLeerConv) (LecturaConv, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return LecturaConv{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return LecturaConv{}, err
			}
			f, err := q.MensajeConversacionPorID(ctx, in.HastaMensajeID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && f.ConversacionID != c.ID) {
				return LecturaConv{}, campoConv("hastaMensajeId", "No es un mensaje de esta conversación", "mensaje")
			}
			if err != nil {
				return LecturaConv{}, err
			}
			if err := q.AvanzarLecturaConversacion(ctx, mensajeriabd.AvanzarLecturaConversacionParams{ConversacionID: c.ID, UsuarioID: a.usuario,
				HastaSeq: f.Seq, LeidoEn: m.ahora()}); err != nil {
				return LecturaConv{}, err
			}
			r, err := m.resumirConv(ctx, q, a.usuario, c.ID)
			return LecturaConv{ConversacionID: c.ID, NoLeidos: r.NoLeidos}, err
		},
	})
}

// Acciones ------------------------------------------------------------------------------------------

// EntradaAccionConv es el cuerpo común: la versión leída.
type EntradaAccionConv struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1"`
}

// EntradaAsignarConv es POST /conversaciones/{id}/asignar.
type EntradaAsignarConv struct {
	ID        int64  `json:"-"`
	Version   int32  `json:"version" minimum:"1"`
	UsuarioID *int64 `json:"usuarioId" minimum:"1" nullable:"true" doc:"null = quitar la asignación"`
}

// EntradaTransferirConv es POST /conversaciones/{id}/transferir.
type EntradaTransferirConv struct {
	ID                  int64  `json:"-"`
	Version             int32  `json:"version" minimum:"1"`
	ColaID              int64  `json:"colaId" minimum:"1"`
	Resumen             string `json:"resumen" minLength:"1" maxLength:"1000" doc:"Queda como nota interna para la otra cola"`
	ConservarAsignacion bool   `json:"conservarAsignacion,omitempty" doc:"Por defecto queda sin asignar"`
}

// MarshalJSON: sin la nota (puede tener datos del cliente).
func (e EntradaResolverConv) MarshalJSON() ([]byte, error) {
	largo := 0
	if e.Nota != nil {
		largo = len([]rune(*e.Nota))
	}
	return json.Marshal(map[string]any{"id": e.ID, "version": e.Version, "largo_nota": largo})
}

// MarshalJSON: sin el resumen (puede tener datos del cliente).
func (e EntradaTransferirConv) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": e.ID, "version": e.Version, "cola_id": e.ColaID, "conservar_asignacion": e.ConservarAsignacion,
		"largo_resumen": len([]rune(e.Resumen))})
}

// EntradaEtiquetarConv es POST /conversaciones/{id}/etiquetar.
type EntradaEtiquetarConv struct {
	ID          int64   `json:"-"`
	Version     int32   `json:"version" minimum:"1"`
	EtiquetaIDs []int64 `json:"etiquetaIds" maxItems:"10" uniqueItems:"true"`
}

// EntradaResolverConv es POST /conversaciones/{id}/resolver.
type EntradaResolverConv struct {
	ID      int64   `json:"-"`
	Version int32   `json:"version" minimum:"1"`
	Nota    *string `json:"nota,omitempty" maxLength:"1000" doc:"Nota interna de cierre"`
}

// accionConv es el esqueleto de una acción: alcance, conversación visible y bloqueada, versión,
// y al final guardar, feed y detalle.
func (m *Modulo) accionConv(ctx context.Context, t *auditoria.Tx, id int64, version int32,
	cambio func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error) (ConversacionClienteDetalle, error) {
	a, err := alcanceConvDe(ctx)
	if err != nil {
		return ConversacionClienteDetalle{}, err
	}
	q := mensajeriabd.New(t)
	c, err := convVisible(ctx, q, a, id, true)
	if err != nil {
		return ConversacionClienteDetalle{}, err
	}
	t.FijarEmpresa(empresaDeConv(c))
	t.FijarEntidad("conversacion", c.ID)
	if version != 0 {
		if err := exigirVersionConv(c, version); err != nil {
			return ConversacionClienteDetalle{}, err
		}
	}
	antes := c
	if err := cambio(a, q, &c); err != nil {
		return ConversacionClienteDetalle{}, err
	}
	// Tomar, asignar, pasar, transferir o resolver cancelan el aviso y el bot de la espera.
	limpiar := !mismoID(antes.AsignadoA, c.AsignadoA) || !mismoID(antes.ColaID, c.ColaID) || (c.Estado == EstadoConvResuelta && antes.Estado != EstadoConvResuelta)
	bot := c
	if c, err = guardarConv(ctx, q, c, true); err != nil {
		return ConversacionClienteDetalle{}, err
	}
	c.BotEsperaEn, c.BotVenceEn, c.BotMotivo, c.BotAtendioEn, c.BotMenu = bot.BotEsperaEn, bot.BotVenceEn, bot.BotMotivo, bot.BotAtendioEn, bot.BotMenu
	if limpiar {
		if err := limpiarEspera(ctx, q, &c); err != nil {
			return ConversacionClienteDetalle{}, err
		}
	}
	if err := m.tocarActividad(ctx, q, a); err != nil {
		return ConversacionClienteDetalle{}, err
	}
	t.RegistrarCambios("conversacion", c.ID, auditableConv(antes), auditableConv(c))
	if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
		return ConversacionClienteDetalle{}, err
	}
	return m.detalleConv(ctx, q, a.usuario, c)
}

func auditableConv(c mensajeriabd.MensajeriaConversacion) map[string]any {
	return map[string]any{"estado": c.Estado, "asignado_a": c.AsignadoA, "cola_id": c.ColaID, "cliente_id": c.ClienteID, "servicio_id": c.ServicioID,
		"contacto_iris_id": c.ContactoIrisID, "prospecto_id": c.ProspectoID}
}

func actorNombre(ctx context.Context, q *mensajeriabd.Queries, id int64) *string {
	u, err := q.UsuarioChat(ctx, id)
	if err != nil {
		return nil
	}
	return &u.Nombre
}

// puedeAtender: el agente atiende conversaciones en la empresa y ve la cola (miembro o supervisor
// de la cola, o supervisor global).
func puedeAtender(ctx context.Context, t *auditoria.Tx, c mensajeriabd.MensajeriaConversacion, usuario int64) (UsuarioRef, error) {
	u, sup, err := usuarioAtiende(ctx, t, empresaDeConv(c), usuario)
	if err != nil {
		return u, err
	}
	if sup || c.ColaID == nil {
		return u, nil
	}
	miembro, err := atiendeCola(ctx, mensajeriabd.New(t), *c.ColaID, usuario)
	if err != nil {
		return u, err
	}
	if !miembro {
		return u, nucleo.ReglaNegocio("usuario_no_disponible", "Ese agente no es de la cola de la conversación")
	}
	return u, nil
}

func nuevoTomarConv(m *Modulo) *auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle]{
		Accion: "conversaciones.tomar", Permiso: nucleo.PermConversacionesTomar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAccionConv) (ConversacionClienteDetalle, error) {
			// Sin exigir la versión antes de mirar la asignación: si otro la tomó, el error útil es
			// ya_asignada (con quién la tiene), no conflicto_version.
			return m.accionConv(ctx, t, in.ID, 0, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				switch {
				case c.AsignadoA != nil && *c.AsignadoA == a.usuario:
					return nil
				case c.AsignadoA != nil:
					quien := "otro agente"
					if n := actorNombre(ctx, q, *c.AsignadoA); n != nil {
						quien = *n
					}
					return nucleo.Conflicto("ya_asignada", "La conversación ya la tiene "+quien)
				}
				if err := exigirVersionConv(*c, in.Version); err != nil {
					return err
				}
				if _, err := puedeAtender(ctx, t, *c, a.usuario); err != nil {
					return err
				}
				ahora := m.ahora()
				c.AsignadoA, c.AsignadoEn = &a.usuario, &ahora
				if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "tomada", ActorNombre: actorNombre(ctx, q, a.usuario)}); err != nil {
					return err
				}
				return t.Emitir("conversacion.asignada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "usuario_id": a.usuario,
					"anterior_id": nil, "origen": "tomar", "actor_id": a.usuario})
			})
		},
	})
}

func nuevoAsignarConv(m *Modulo) *auditoria.Caso[EntradaAsignarConv, ConversacionClienteDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAsignarConv, ConversacionClienteDetalle]{
		// conversaciones.asignar o supervisor de la cola de la conversación (§14.1): se revisa adentro.
		Accion: "conversaciones.asignar", Permiso: nucleo.PermConversacionesVer,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAsignarConv) (ConversacionClienteDetalle, error) {
			return m.accionConv(ctx, t, in.ID, in.Version, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermConversacionesAsignar)) {
					sup, err := supervisaColaDe(ctx, q, *c, a.usuario)
					if err != nil {
						return err
					}
					if !sup {
						return nucleo.Prohibido("Falta el permiso conversaciones.asignar (o supervisar la cola de la conversación)")
					}
				}
				anterior := c.AsignadoA
				if (anterior == nil && in.UsuarioID == nil) || (anterior != nil && in.UsuarioID != nil && *anterior == *in.UsuarioID) {
					return nil
				}
				ev := EventoSistema{Tipo: "asignada", ActorNombre: actorNombre(ctx, q, a.usuario)}
				if in.UsuarioID != nil {
					u, err := puedeAtender(ctx, t, *c, *in.UsuarioID)
					if err != nil {
						return err
					}
					ahora := m.ahora()
					c.AsignadoA, c.AsignadoEn, ev.AsignadoNombre = &u.ID, &ahora, &u.Nombre
				} else {
					c.AsignadoA, c.AsignadoEn = nil, nil
				}
				if err := m.sistema(ctx, t, c, ev); err != nil {
					return err
				}
				return t.Emitir("conversacion.asignada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "usuario_id": c.AsignadoA,
					"anterior_id": anterior, "origen": "asignar", "actor_id": a.usuario})
			})
		},
	})
}

func nuevoTransferirConv(m *Modulo) *auditoria.Caso[EntradaTransferirConv, ConversacionClienteDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaTransferirConv, ConversacionClienteDetalle]{
		Accion: "conversaciones.transferir", Permiso: nucleo.PermConversacionesTransferir,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTransferirConv) (ConversacionClienteDetalle, error) {
			resumen := strings.TrimSpace(in.Resumen)
			if resumen == "" {
				return ConversacionClienteDetalle{}, campoConv("resumen", "Escribe un resumen para la otra cola", "required")
			}
			return m.accionConv(ctx, t, in.ID, in.Version, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				cola, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: in.ColaID, Empresa: empresaDeConv(*c)})
				if errors.Is(err, pgx.ErrNoRows) {
					return campoConv("colaId", "No es una cola de la empresa", "cola")
				}
				if err != nil {
					return err
				}
				if !cola.Activa {
					return reglaConv("cola_inactiva", "La cola está desactivada")
				}
				anterior := c.ColaID
				c.ColaID = &cola.ID
				if !in.ConservarAsignacion {
					c.AsignadoA, c.AsignadoEn = nil, nil
				}
				if c.EsperaDesde != nil {
					if c.SlaVenceEn, err = m.venceSLA(ctx, q, *c, cola, *c.EsperaDesde); err != nil {
						return err
					}
					c.SlaAvisado = false
				}
				if _, err := m.agregarMensaje(ctx, t, c, nuevoMensaje{direccion: DirNota, tipo: "texto", texto: &resumen, autor: &a.usuario}); err != nil {
					return err
				}
				if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "transferida", ActorNombre: actorNombre(ctx, q, a.usuario), ColaNombre: &cola.Nombre}); err != nil {
					return err
				}
				return t.Emitir("conversacion.transferida", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "de_cola_id": anterior,
					"a_cola_id": cola.ID, "asignado_a": c.AsignadoA, "actor_id": a.usuario})
			})
		},
	})
}

func nuevoEtiquetarConv(m *Modulo) *auditoria.Caso[EntradaEtiquetarConv, ConversacionClienteDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEtiquetarConv, ConversacionClienteDetalle]{
		Accion: "conversaciones.etiquetar", Permiso: nucleo.PermConversacionesEtiquetar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEtiquetarConv) (ConversacionClienteDetalle, error) {
			return m.accionConv(ctx, t, in.ID, in.Version, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				ids := in.EtiquetaIDs
				if ids == nil {
					ids = []int64{}
				}
				es, err := q.EtiquetasActivasPorIDs(ctx, mensajeriabd.EtiquetasActivasPorIDsParams{Ids: ids, Empresa: empresaDeConv(*c)})
				if err != nil {
					return err
				}
				if len(es) != len(ids) {
					return campoConv("etiquetaIds", "Alguna no es una etiqueta activa de la empresa", "etiqueta")
				}
				previas, err := q.EtiquetasDeConversaciones(ctx, []int64{c.ID})
				if err != nil {
					return err
				}
				antes := []int64{}
				for _, p := range previas {
					antes = append(antes, p.ID)
				}
				if err := q.BorrarEtiquetasConversacion(ctx, c.ID); err != nil {
					return err
				}
				for _, id := range ids {
					if err := q.InsertarEtiquetaConversacion(ctx, mensajeriabd.InsertarEtiquetaConversacionParams{ConversacionID: c.ID, EtiquetaID: id}); err != nil {
						return err
					}
				}
				t.RegistrarCambios("conversacion", c.ID, map[string]any{"etiquetas": antes}, map[string]any{"etiquetas": ids})
				return t.Emitir("conversacion.etiquetada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "etiquetas": ids,
					"anteriores": antes, "actor_id": a.usuario})
			})
		},
	})
}

func nuevoResolverConv(m *Modulo) *auditoria.Caso[EntradaResolverConv, ConversacionClienteDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaResolverConv, ConversacionClienteDetalle]{
		Accion: "conversaciones.resolver", Permiso: nucleo.PermConversacionesResolver,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaResolverConv) (ConversacionClienteDetalle, error) {
			return m.accionConv(ctx, t, in.ID, in.Version, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				if c.Estado == EstadoConvResuelta {
					return nil
				}
				ahora := m.ahora()
				c.Estado, c.ResueltaEn = EstadoConvResuelta, &ahora
				c.EsperaDesde, c.SlaVenceEn, c.SlaAvisado = nil, nil, false
				if in.Nota != nil && strings.TrimSpace(*in.Nota) != "" {
					nota := strings.TrimSpace(*in.Nota)
					if _, err := m.agregarMensaje(ctx, t, c, nuevoMensaje{direccion: DirNota, tipo: "texto", texto: &nota, autor: &a.usuario}); err != nil {
						return err
					}
				}
				if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "resuelta", ActorNombre: actorNombre(ctx, q, a.usuario)}); err != nil {
					return err
				}
				return t.Emitir("conversacion.resuelta", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "actor_id": a.usuario,
					"asignado_a": c.AsignadoA})
			})
		},
	})
}

func nuevoReabrirConv(m *Modulo) *auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle]{
		Accion: "conversaciones.reabrir", Permiso: nucleo.PermConversacionesResolver,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAccionConv) (ConversacionClienteDetalle, error) {
			return m.accionConv(ctx, t, in.ID, in.Version, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				if c.Estado != EstadoConvResuelta {
					return reglaConv("no_resuelta", "Solo se reabre una conversación resuelta")
				}
				ahora := m.ahora()
				c.Estado, c.ReabiertaEn, c.ResueltaEn = EstadoConvAbierta, &ahora, nil
				if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "reabierta", ActorNombre: actorNombre(ctx, q, a.usuario)}); err != nil {
					return err
				}
				return t.Emitir("conversacion.reabierta", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "origen": "agente",
					"asignado_a": c.AsignadoA, "actor_id": a.usuario})
			})
		},
	})
}
