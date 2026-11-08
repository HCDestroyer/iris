package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Préstamo de un número a otras empresas de la instalación (contratos/conversaciones.md §15.3): la
// empresa dueña lo comparte con usos y cola por empresa; un mensaje que llega va a la empresa que
// diga la regla de enrutamiento; la dueña puede pasar una conversación a quien lo usa; revocar deja
// las conversaciones de esa empresa en solo lectura.

// usosConversacion: los usos que permiten atender e iniciar conversaciones (no solo avisos).
var usosConversacion = []string{"soporte", "cobros", "ventas"}

func tieneUsoConversacion(usos []string) bool {
	return slices.ContainsFunc(usos, func(u string) bool { return slices.Contains(usosConversacion, u) })
}

// Motivos de conversacion.empresa_asignada.
const (
	RutaConversacionAbierta = "conversacion_abierta"
	RutaIniciada            = "iniciada_por_empresa"
	RutaClienteDeEmpresa    = "cliente_de_empresa"
	RutaSinCoincidencia     = "sin_coincidencia"
	RutaVariasCoincidencias = "varias_coincidencias"
	RutaPasada              = "pasada"
)

// ventanaInicio: una empresa que empezó la conversación (aviso o iniciar) la conserva 24 h.
const ventanaInicio = 24 * time.Hour

// EmpresaRef es la empresa dueña de un número prestado.
type EmpresaRef struct {
	EmpresaID int64  `json:"empresaId"`
	Nombre    string `json:"nombre"`
}

// PrestamoPropio es el préstamo visto por la empresa que usa el número.
type PrestamoPropio struct {
	Usos          []string  `json:"usos"`
	ColaID        *int64    `json:"colaId" nullable:"true"`
	AvisosEmpresa bool      `json:"avisosEmpresa"`
	Version       int32     `json:"version"`
	PrestadoEn    time.Time `json:"prestadoEn"`
}

// Prestamo es un préstamo visto por la empresa dueña.
type Prestamo struct {
	CanalID                int64      `json:"canalId"`
	EmpresaID              int64      `json:"empresaId"`
	EmpresaNombre          string     `json:"empresaNombre"`
	Usos                   []string   `json:"usos"`
	ColaID                 *int64     `json:"colaId" nullable:"true"`
	ColaNombre             *string    `json:"colaNombre" nullable:"true"`
	AvisosEmpresa          bool       `json:"avisosEmpresa"`
	Estado                 string     `json:"estado" enum:"activo,revocado"`
	PrestadoEn             time.Time  `json:"prestadoEn"`
	RevocadoEn             *time.Time `json:"revocadoEn" nullable:"true"`
	MotivoRevocacion       *string    `json:"motivoRevocacion" nullable:"true"`
	ConversacionesAbiertas int64      `json:"conversacionesAbiertas"`
	AvisosProgramados      int64      `json:"avisosProgramados"`
	Version                int32      `json:"version"`
}

// ListaPrestamos es { data, nextCursor }.
type ListaPrestamos struct {
	Data       []Prestamo `json:"data"`
	NextCursor *string    `json:"nextCursor" nullable:"true"`
}

// Vista de un número ---------------------------------------------------------------------------------

// compartidosDe: cuántas empresas usan cada número (préstamo activo).
func compartidosDe(ctx context.Context, q *mensajeriabd.Queries, ids []int64) (map[int64]int, error) {
	out := map[int64]int{}
	if len(ids) == 0 {
		return out, nil
	}
	fs, err := q.CompartidosPorCanal(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, f := range fs {
		out[f.CanalID] = int(f.Total)
	}
	return out, nil
}

// canalVisto: el número como lo ve su dueña (con cuántas empresas lo comparten).
func (m *Modulo) canalVisto(ctx context.Context, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaCanal, _ *int64) (Canal, error) {
	c := m.aCanal(f)
	n, err := compartidosDe(ctx, q, []int64{f.ID})
	if err != nil {
		return Canal{}, err
	}
	c.CompartidoCon = n[f.ID]
	return c, nil
}

// aCanalPrestado: el número visto por la empresa que lo usa (sin datos de Meta ni de conexión).
func (m *Modulo) aCanalPrestado(f mensajeriabd.MensajeriaCanal, p mensajeriabd.MensajeriaCanalPrestamo, dueno string) Canal {
	c := m.aCanal(f)
	empresa := p.EmpresaID
	c.EmpresaID = &empresa
	c.PhoneNumberID, c.WabaID, c.Plantillas = "", "", map[string]PlantillaMeta{}
	c.WebhookRuta, c.WebhookURL, c.WebhookURLCompleta = "", "", false
	c.UltimoEstado, c.UltimoDetalle, c.UltimaPruebaEn, c.WebhookVerificadoEn, c.UltimoMensajeEn = nil, nil, nil, nil, nil
	c.TokenGuardadoEn, c.AppSecretGuardadoEn, c.VerifyTokenGuardadoEn = nil, nil, nil
	c.Sincronizacion, c.UltimoEcoEn = nil, nil
	c.Usos, c.ColaID, c.AvisosEmpresa = p.Usos, p.ColaID, p.AvisosEmpresa
	c.HorarioID, c.Bot = nil, botDe([]byte(`{}`))
	c.FueraDeHorario = FueraDeHorario{}
	c.CompartidoCon = 0 // con quién más lo comparte la dueña no es de su incumbencia
	c.PrestadoPor = &EmpresaRef{EmpresaID: derefEmpresa(f.EmpresaID), Nombre: dueno}
	c.Prestamo = &PrestamoPropio{Usos: p.Usos, ColaID: p.ColaID, AvisosEmpresa: p.AvisosEmpresa, Version: p.Version, PrestadoEn: p.PrestadoEn}
	if c.Conexion != "desactivado" && c.Conexion != "desconectado" {
		c.Conexion = PruebaConectado
	}
	return c
}

func derefEmpresa(e *int64) int64 {
	if e == nil {
		return 0
	}
	return *e
}

// empresaActiva: la empresa activa del actor (0 sin empresa activa).
func empresaActiva(ctx context.Context) int64 {
	if contexto.EmpresasDe(ctx).Estado == contexto.EmpresaActiva {
		return contexto.EmpresaDe(ctx)
	}
	return 0
}

// canalPrestadoA: el número prestado a la empresa (préstamo activo) o pgx.ErrNoRows.
func canalPrestadoA(ctx context.Context, q *mensajeriabd.Queries, canal, empresa int64) (mensajeriabd.MensajeriaCanal, mensajeriabd.MensajeriaCanalPrestamo, string, error) {
	if empresa == 0 {
		return mensajeriabd.MensajeriaCanal{}, mensajeriabd.MensajeriaCanalPrestamo{}, "", pgx.ErrNoRows
	}
	p, err := q.PrestamoActivo(ctx, mensajeriabd.PrestamoActivoParams{CanalID: canal, EmpresaID: empresa})
	if err != nil {
		return mensajeriabd.MensajeriaCanal{}, p, "", err
	}
	f, err := q.CanalPorID(ctx, canal)
	if err != nil {
		return f, p, "", err
	}
	dueno := ""
	if f.EmpresaID != nil {
		if n, err := q.NombreEmpresa(ctx, *f.EmpresaID); err == nil {
			dueno = n
		}
	}
	return f, p, dueno, nil
}

// Casos de la empresa dueña -------------------------------------------------------------------------

// canalDeDuena: el número de la empresa activa (bloqueado si bloquear), solo personas; si no, 404.
func (m *Modulo) canalDeDuena(ctx context.Context, t *auditoria.Tx, id int64) (mensajeriabd.MensajeriaCanal, error) {
	if err := soloPersonas(ctx); err != nil {
		return mensajeriabd.MensajeriaCanal{}, err
	}
	f, err := mensajeriabd.New(t).CanalPorIDParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && derefEmpresa(f.EmpresaID) != empresaActiva(ctx)) {
		return f, nucleo.NoEncontrado("El número no existe")
	}
	return f, err
}

func aPrestamo(f mensajeriabd.PrestamosDeCanalRow) Prestamo {
	p := Prestamo{CanalID: f.CanalID, EmpresaID: f.EmpresaID, Usos: f.Usos, ColaID: f.ColaID, ColaNombre: f.ColaNombre, AvisosEmpresa: f.AvisosEmpresa,
		Estado: "revocado", PrestadoEn: f.PrestadoEn, RevocadoEn: f.RevocadoEn, MotivoRevocacion: f.MotivoRevocacion,
		ConversacionesAbiertas: f.ConversacionesAbiertas, AvisosProgramados: f.AvisosProgramados, Version: f.Version}
	if f.EmpresaNombre != nil {
		p.EmpresaNombre = *f.EmpresaNombre
	}
	if f.Activo {
		p.Estado = "activo"
	}
	return p
}

func prestamosDe(ctx context.Context, q *mensajeriabd.Queries, canal int64) ([]Prestamo, error) {
	fs, err := q.PrestamosDeCanal(ctx, canal)
	if err != nil {
		return nil, err
	}
	out := make([]Prestamo, 0, len(fs))
	for _, f := range fs {
		out = append(out, aPrestamo(f))
	}
	return out, nil
}

// EntradaCanalPrestamos es un número por id.
type EntradaCanalPrestamos struct {
	ID int64 `json:"canal_id"`
}

func nuevoListarPrestamos(m *Modulo) *auditoria.Caso[EntradaCanalPrestamos, ListaPrestamos] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCanalPrestamos, ListaPrestamos]{
		Accion: "canales.ver_prestamos", Permiso: nucleo.PermCanalesConfigurar, AuditarExito: sinAuditar[ListaPrestamos],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCanalPrestamos) (ListaPrestamos, error) {
			f, err := m.canalDeDuena(ctx, t, in.ID)
			if err != nil {
				return ListaPrestamos{}, err
			}
			ps, err := prestamosDe(ctx, mensajeriabd.New(t), f.ID)
			return ListaPrestamos{Data: ps}, err
		},
	})
}

// OpcionesPrestamo son las empresas a las que se puede prestar y sus colas.
type OpcionesPrestamo struct {
	Empresas []EmpresaOpcion `json:"empresas"`
}

// EmpresaOpcion es una empresa de la instalación con sus colas activas.
type EmpresaOpcion struct {
	ID     int64     `json:"id"`
	Nombre string    `json:"nombre"`
	Colas  []ColaRef `json:"colas"`
}

func nuevoOpcionesPrestamo(m *Modulo) *auditoria.Caso[EntradaCanalPrestamos, OpcionesPrestamo] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCanalPrestamos, OpcionesPrestamo]{
		Accion: "canales.ver_prestamos", Permiso: nucleo.PermCanalesConfigurar, AuditarExito: sinAuditar[OpcionesPrestamo],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCanalPrestamos) (OpcionesPrestamo, error) {
			f, err := m.canalDeDuena(ctx, t, in.ID)
			if err != nil {
				return OpcionesPrestamo{}, err
			}
			q := mensajeriabd.New(t)
			es, err := q.OtrasEmpresas(ctx, derefEmpresa(f.EmpresaID))
			if err != nil {
				return OpcionesPrestamo{}, err
			}
			out := OpcionesPrestamo{Empresas: make([]EmpresaOpcion, 0, len(es))}
			ids := make([]int64, 0, len(es))
			pos := map[int64]int{}
			for i, e := range es {
				out.Empresas = append(out.Empresas, EmpresaOpcion{ID: e.ID, Nombre: e.Nombre, Colas: []ColaRef{}})
				ids = append(ids, e.ID)
				pos[e.ID] = i
			}
			colas, err := q.ColasActivasDeEmpresas(ctx, ids)
			if err != nil {
				return OpcionesPrestamo{}, err
			}
			for _, c := range colas {
				if c.EmpresaID == nil {
					continue
				}
				if i, ok := pos[*c.EmpresaID]; ok {
					out.Empresas[i].Colas = append(out.Empresas[i].Colas, ColaRef{ID: c.ID, Nombre: c.Nombre})
				}
			}
			return out, nil
		},
	})
}

// EntradaPrestamo es POST /canales/{id}/prestamos.
type EntradaPrestamo struct {
	ID        int64    `json:"canal_id"`
	EmpresaID int64    `json:"empresaId" minimum:"1"`
	Usos      []string `json:"usos" minItems:"1" maxItems:"4" uniqueItems:"true"`
	ColaID    *int64   `json:"colaId" minimum:"1" nullable:"true"`
	Version   int32    `json:"version,omitempty" minimum:"0" doc:"La del préstamo si ya existe (0 o ausente: uno nuevo)"`
}

func empresaNoValida(detalle string) *nucleo.Problema { return reglaConv("empresa_no_valida", detalle) }

func nuevoGuardarPrestamo(m *Modulo) *auditoria.Caso[EntradaPrestamo, Prestamo] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPrestamo, Prestamo]{
		Accion: "canales.prestar", Permiso: nucleo.PermCanalesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPrestamo) (Prestamo, error) {
			f, err := m.canalDeDuena(ctx, t, in.ID)
			if err != nil {
				return Prestamo{}, err
			}
			if !f.Activo {
				return Prestamo{}, reglaConv("canal_inactivo", "Activa el número antes de compartirlo")
			}
			if err := validarUsos(in.Usos, false); err != nil {
				return Prestamo{}, err
			}
			q := mensajeriabd.New(t)
			if in.EmpresaID == derefEmpresa(f.EmpresaID) {
				return Prestamo{}, empresaNoValida("El número ya es de esa empresa")
			}
			nombre, err := q.NombreEmpresa(ctx, in.EmpresaID)
			if errors.Is(err, pgx.ErrNoRows) {
				return Prestamo{}, empresaNoValida("Esa empresa no existe en esta instalación")
			}
			if err != nil {
				return Prestamo{}, err
			}
			if in.ColaID != nil {
				co, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: *in.ColaID, Empresa: in.EmpresaID})
				if errors.Is(err, pgx.ErrNoRows) || (err == nil && !co.Activa) {
					return Prestamo{}, campoConv("colaId", "No es una cola activa de "+nombre, "cola")
				}
				if err != nil {
					return Prestamo{}, err
				}
			}
			antes, err := q.PrestamoParaActualizar(ctx, mensajeriabd.PrestamoParaActualizarParams{CanalID: f.ID, EmpresaID: in.EmpresaID})
			nuevo := errors.Is(err, pgx.ErrNoRows)
			if err != nil && !nuevo {
				return Prestamo{}, err
			}
			if (nuevo && in.Version != 0) || (!nuevo && antes.Version != in.Version) {
				return Prestamo{}, nucleo.ConflictoVersion("prestamo", in.EmpresaID)
			}
			p, err := q.GuardarPrestamo(ctx, mensajeriabd.GuardarPrestamoParams{CanalID: f.ID, EmpresaID: in.EmpresaID, Usos: in.Usos, ColaID: in.ColaID,
				PrestadoEn: m.ahora(), PrestadoPor: usuarioDe(ctx)})
			if err != nil {
				return Prestamo{}, err
			}
			fijarEmpresaCanal(t, f.EmpresaID)
			var previo any
			if !nuevo {
				previo = map[string]any{"usos": antes.Usos, "cola_id": antes.ColaID, "activo": antes.Activo, "avisos_empresa": antes.AvisosEmpresa}
			}
			t.RegistrarCambios("prestamo_canal", f.ID, previo, map[string]any{"empresa_id": p.EmpresaID, "usos": p.Usos, "cola_id": p.ColaID,
				"activo": p.Activo, "avisos_empresa": p.AvisosEmpresa})
			out, err := prestamoDe(ctx, q, f.ID, in.EmpresaID)
			if err != nil {
				return Prestamo{}, err
			}
			return out, t.Emitir("canal_mensajeria.prestado", map[string]any{"canal_id": f.ID, "empresa_id": f.EmpresaID, "prestatario_id": p.EmpresaID,
				"usos": p.Usos, "cola_id": p.ColaID, "nuevo": nuevo || !antes.Activo})
		},
	})
}

func prestamoDe(ctx context.Context, q *mensajeriabd.Queries, canal, empresa int64) (Prestamo, error) {
	ps, err := prestamosDe(ctx, q, canal)
	if err != nil {
		return Prestamo{}, err
	}
	for _, p := range ps {
		if p.EmpresaID == empresa {
			return p, nil
		}
	}
	return Prestamo{}, pgx.ErrNoRows
}

// EntradaRevocarPrestamo es POST /canales/{id}/prestamos/{empresa}/revocar.
type EntradaRevocarPrestamo struct {
	ID        int64  `json:"canal_id"`
	EmpresaID int64  `json:"empresa_id"`
	Version   int32  `json:"version" minimum:"1"`
	Motivo    string `json:"motivo" minLength:"5" maxLength:"300"`
}

func nuevoRevocarPrestamo(m *Modulo) *auditoria.Caso[EntradaRevocarPrestamo, Prestamo] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRevocarPrestamo, Prestamo]{
		Accion: "canales.revocar_prestamo", Permiso: nucleo.PermCanalesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRevocarPrestamo) (Prestamo, error) {
			f, err := m.canalDeDuena(ctx, t, in.ID)
			if err != nil {
				return Prestamo{}, err
			}
			q := mensajeriabd.New(t)
			antes, err := q.PrestamoParaActualizar(ctx, mensajeriabd.PrestamoParaActualizarParams{CanalID: f.ID, EmpresaID: in.EmpresaID})
			if errors.Is(err, pgx.ErrNoRows) {
				return Prestamo{}, nucleo.NoEncontrado("Ese número no está compartido con esa empresa")
			}
			if err != nil {
				return Prestamo{}, err
			}
			if antes.Version != in.Version {
				return Prestamo{}, nucleo.ConflictoVersion("prestamo", in.EmpresaID)
			}
			fijarEmpresaCanal(t, f.EmpresaID)
			if !antes.Activo {
				return prestamoDe(ctx, q, f.ID, in.EmpresaID)
			}
			motivo := strings.TrimSpace(in.Motivo)
			if len([]rune(motivo)) < 5 {
				return Prestamo{}, campoConv("motivo", "Escribe el motivo (5 a 300 letras)", "minLength")
			}
			ahora := m.ahora()
			p, err := q.RevocarPrestamo(ctx, mensajeriabd.RevocarPrestamoParams{CanalID: f.ID, EmpresaID: in.EmpresaID, RevocadoEn: &ahora,
				RevocadoPor: usuarioDe(ctx), Motivo: &motivo})
			if err != nil {
				return Prestamo{}, err
			}
			// Las conversaciones abiertas de esa empresa por este número se resuelven y quedan de solo
			// lectura para ella.
			convs, err := q.ConversacionesNoResueltasDePrestamo(ctx, mensajeriabd.ConversacionesNoResueltasDePrestamoParams{CanalID: f.ID, EmpresaID: &in.EmpresaID})
			if err != nil {
				return Prestamo{}, err
			}
			for _, c := range convs {
				c.Estado, c.ResueltaEn = EstadoConvResuelta, &ahora
				c.EsperaDesde, c.SlaVenceEn, c.SlaAvisado = nil, nil, false
				if err := m.sistema(ctx, t, &c, EventoSistema{Tipo: "prestamo_revocado"}); err != nil {
					return Prestamo{}, err
				}
				if c, err = guardarConv(ctx, q, c, true); err != nil {
					return Prestamo{}, err
				}
				if err := limpiarEspera(ctx, q, &c); err != nil {
					return Prestamo{}, err
				}
				if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
					return Prestamo{}, err
				}
				if err := t.Emitir("conversacion.resuelta", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "actor_id": nil,
					"asignado_a": c.AsignadoA, "motivo": "prestamo_revocado"}); err != nil {
					return Prestamo{}, err
				}
			}
			// Sus avisos que no salieron por este número se cancelan.
			avisos, err := q.AvisosEnColaDePrestamo(ctx, mensajeriabd.AvisosEnColaDePrestamoParams{CanalID: &f.ID, EmpresaID: in.EmpresaID})
			if err != nil {
				return Prestamo{}, err
			}
			for _, a := range avisos {
				if err := m.fallar(ctx, t, a, MotivoRechazado, texto("prestamo_revocado")); err != nil {
					return Prestamo{}, err
				}
			}
			t.RegistrarCambios("prestamo_canal", f.ID, map[string]any{"empresa_id": antes.EmpresaID, "activo": true, "avisos_empresa": antes.AvisosEmpresa},
				map[string]any{"empresa_id": p.EmpresaID, "activo": false, "avisos_empresa": false, "motivo": motivo})
			t.Anotar(map[string]any{"conversaciones_cerradas": len(convs), "avisos_cancelados": len(avisos)})
			out, err := prestamoDe(ctx, q, f.ID, in.EmpresaID)
			if err != nil {
				return Prestamo{}, err
			}
			return out, t.Emitir("canal_mensajeria.prestamo_revocado", map[string]any{"canal_id": f.ID, "empresa_id": f.EmpresaID,
				"prestatario_id": in.EmpresaID, "motivo": motivo, "conversaciones_cerradas": len(convs), "avisos_cancelados": len(avisos)})
		},
	})
}

// Caso de la empresa que usa el número ---------------------------------------------------------------

// EntradaConfigurarPrestamo es POST /canales/{id}/prestamo/configurar.
type EntradaConfigurarPrestamo struct {
	ID            int64 `json:"canal_id"`
	Version       int32 `json:"version" minimum:"1" doc:"La del préstamo (Canal.prestamo.version)"`
	AvisosEmpresa bool  `json:"avisosEmpresa"`
}

func usoNoPermitido(detalle string) *nucleo.Problema { return reglaConv("uso_no_permitido", detalle) }

func nuevoConfigurarPrestamo(m *Modulo) *auditoria.Caso[EntradaConfigurarPrestamo, Canal] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConfigurarPrestamo, Canal]{
		Accion: "canales.configurar_prestamo", Permiso: nucleo.PermCanalesConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConfigurarPrestamo) (Canal, error) {
			if err := soloPersonas(ctx); err != nil {
				return Canal{}, err
			}
			empresa := empresaActiva(ctx)
			q := mensajeriabd.New(t)
			antes, err := q.PrestamoParaActualizar(ctx, mensajeriabd.PrestamoParaActualizarParams{CanalID: in.ID, EmpresaID: empresa})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !antes.Activo) {
				return Canal{}, nucleo.NoEncontrado("El número no existe")
			}
			if err != nil {
				return Canal{}, err
			}
			if antes.Version != in.Version {
				return Canal{}, nucleo.ConflictoVersion("prestamo", in.ID)
			}
			if in.AvisosEmpresa && !slices.Contains(antes.Usos, "avisos") {
				return Canal{}, usoNoPermitido("La empresa dueña no permite usar este número para avisos")
			}
			t.FijarEmpresa(empresa)
			t.FijarEntidad("canal_mensajeria", in.ID)
			if in.AvisosEmpresa && !antes.AvisosEmpresa {
				// Deja de serlo su número propio de avisos y cualquier otro préstamo.
				e := empresa
				if err := fijarAvisos(ctx, t, q, &e, in.ID); err != nil {
					return Canal{}, err
				}
			}
			p, err := q.FijarAvisosPrestamo(ctx, mensajeriabd.FijarAvisosPrestamoParams{CanalID: in.ID, EmpresaID: empresa, AvisosEmpresa: in.AvisosEmpresa})
			if err != nil {
				return Canal{}, err
			}
			t.RegistrarCambios("prestamo_canal", in.ID, map[string]any{"avisos_empresa": antes.AvisosEmpresa}, map[string]any{"avisos_empresa": p.AvisosEmpresa})
			f, _, dueno, err := canalPrestadoA(ctx, q, in.ID, empresa)
			if err != nil {
				return Canal{}, err
			}
			return m.aCanalPrestado(f, p, dueno), t.Emitir("canal_mensajeria.actualizado", map[string]any{"canal_id": in.ID, "empresa_id": empresa,
				"campos": []string{"avisos_empresa"}})
		},
	})
}

// Uso del número por una empresa ---------------------------------------------------------------------

// usoDelCanal: lo que la empresa puede hacer con el número. propio = es la dueña; activo = la dueña
// o un préstamo activo; usos = los del préstamo (o todos para la dueña).
type usoDelCanal struct {
	propio bool
	activo bool
	usos   []string
	cola   *int64
}

func usoDe(ctx context.Context, q *mensajeriabd.Queries, canal mensajeriabd.MensajeriaCanal, empresa int64) (usoDelCanal, error) {
	if derefEmpresa(canal.EmpresaID) == empresa {
		return usoDelCanal{propio: true, activo: true, usos: UsosCanal, cola: canal.ColaID}, nil
	}
	p, err := q.PrestamoActivo(ctx, mensajeriabd.PrestamoActivoParams{CanalID: canal.ID, EmpresaID: empresa})
	if errors.Is(err, pgx.ErrNoRows) {
		return usoDelCanal{}, nil
	}
	if err != nil {
		return usoDelCanal{}, err
	}
	return usoDelCanal{activo: true, usos: p.Usos, cola: p.ColaID}, nil
}

func conversacionSoloLectura() *nucleo.Problema {
	return reglaConv("conversacion_solo_lectura", "El número de esta conversación ya no está compartido con tu empresa: queda de solo lectura")
}

// soloLecturaConv: la conversación es de una empresa que ya no usa el número (préstamo revocado).
func soloLecturaConv(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion) (bool, error) {
	canal, err := q.CanalPorID(ctx, c.CanalID)
	if err != nil {
		return false, err
	}
	u, err := usoDe(ctx, q, canal, empresaDeConv(c))
	return !u.activo, err
}

// Enrutamiento ----------------------------------------------------------------------------------------

// destinoEntrante es la empresa (y su cola) a la que va un mensaje que llega.
type destinoEntrante struct {
	empresa int64
	cola    *int64
	motivo  string
	// compartido: el número tiene (o tuvo) préstamos: se emite conversacion.empresa_asignada.
	compartido bool
}

// otraEmpresaConversa: 409 conversacion_en_otra_empresa si otra empresa del número (compartido)
// tiene una conversación no resuelta con ese teléfono.
func otraEmpresaConversa(ctx context.Context, q *mensajeriabd.Queries, canal, empresa int64, numero string) error {
	hay, err := q.HayPrestamos(ctx, canal)
	if err != nil || !hay {
		return err
	}
	abiertas, err := q.ConversacionesNoResueltasDeNumero(ctx, mensajeriabd.ConversacionesNoResueltasDeNumeroParams{CanalID: canal, Numero: numero})
	if err != nil {
		return err
	}
	for _, a := range abiertas {
		if derefEmpresa(a.EmpresaID) != empresa {
			return nucleo.Conflicto("conversacion_en_otra_empresa", "Otra empresa que usa este número tiene una conversación abierta con ese teléfono")
		}
	}
	return nil
}

// rutearEntrante aplica la regla de §15.3 para un número compartido. Sin préstamos, la dueña.
func (m *Modulo) rutearEntrante(ctx context.Context, q *mensajeriabd.Queries, canal mensajeriabd.MensajeriaCanal, numero string) (destinoEntrante, error) {
	return m.rutear(ctx, q, canal, numero, false)
}

// rutearEco: lo mandado desde el teléfono de la dueña solo sigue a una conversación abierta (regla
// 1); si no hay, es de la dueña.
func (m *Modulo) rutearEco(ctx context.Context, q *mensajeriabd.Queries, canal mensajeriabd.MensajeriaCanal, numero string) (destinoEntrante, error) {
	return m.rutear(ctx, q, canal, numero, true)
}

func (m *Modulo) rutear(ctx context.Context, q *mensajeriabd.Queries, canal mensajeriabd.MensajeriaCanal, numero string, soloAbierta bool) (destinoEntrante, error) {
	dueno := derefEmpresa(canal.EmpresaID)
	d := destinoEntrante{empresa: dueno, cola: canal.ColaID}
	hay, err := q.HayPrestamos(ctx, canal.ID)
	if err != nil || !hay {
		return d, err
	}
	d.compartido = true
	ps, err := q.PrestamosActivosDeCanal(ctx, canal.ID)
	if err != nil {
		return d, err
	}
	colas := map[int64]*int64{dueno: canal.ColaID}
	conversa := map[int64]bool{dueno: true}
	for _, p := range ps {
		colas[p.EmpresaID] = p.ColaID
		conversa[p.EmpresaID] = tieneUsoConversacion(p.Usos)
	}
	elegir := func(e int64, motivo string) destinoEntrante {
		return destinoEntrante{empresa: e, cola: colas[e], motivo: motivo, compartido: true}
	}
	// 1. Una conversación no resuelta en una empresa del número la conserva: la de la dueña si la
	// hay; si no, la más antigua.
	abiertas, err := q.ConversacionesNoResueltasDeNumero(ctx, mensajeriabd.ConversacionesNoResueltasDeNumeroParams{CanalID: canal.ID, Numero: numero})
	if err != nil {
		return d, err
	}
	primera := int64(-1)
	for _, a := range abiertas {
		e := derefEmpresa(a.EmpresaID)
		if _, ok := colas[e]; !ok {
			continue
		}
		if e == dueno {
			return elegir(dueno, RutaConversacionAbierta), nil
		}
		if primera < 0 {
			primera = e
		}
	}
	if primera >= 0 {
		return elegir(primera, RutaConversacionAbierta), nil
	}
	if soloAbierta {
		return elegir(dueno, RutaConversacionAbierta), nil
	}
	// 2. La empresa que la empezó en las últimas 24 h.
	inicios, err := q.IniciosRecientes(ctx, mensajeriabd.IniciosRecientesParams{CanalID: &canal.ID, Numero: &numero, Desde: ptrTiempo(m.ahora().Add(-ventanaInicio))})
	if err != nil {
		return d, err
	}
	for _, i := range inicios {
		if _, ok := colas[i.EmpresaID]; ok {
			return elegir(i.EmpresaID, RutaIniciada), nil
		}
	}
	// 3. El teléfono es cliente (Argos o contacto de Iris) de una sola empresa que conversa por él.
	digitos := soloDigitos(numero)
	coinciden := []int64{}
	for e, ok := range conversa {
		if !ok {
			continue
		}
		cs, err := q.ClientesPorTelefonoInstantanea(ctx, mensajeriabd.ClientesPorTelefonoInstantaneaParams{Digitos: digitos, Empresa: e})
		if err != nil {
			return d, err
		}
		es := len(cs) > 0
		if !es {
			if es, err = q.HayContactoIris(ctx, mensajeriabd.HayContactoIrisParams{Empresa: e, Telefono: numero}); err != nil {
				return d, err
			}
		}
		if es {
			coinciden = append(coinciden, e)
		}
	}
	switch len(coinciden) {
	case 0:
		return elegir(dueno, RutaSinCoincidencia), nil
	case 1:
		return elegir(coinciden[0], RutaClienteDeEmpresa), nil
	}
	return elegir(dueno, RutaVariasCoincidencias), nil
}

// Pasar una conversación a otra empresa ---------------------------------------------------------------

// EntradaPasarEmpresa es POST /conversaciones/{id}/pasar-empresa.
type EntradaPasarEmpresa struct {
	ID        int64   `json:"-"`
	Version   int32   `json:"version" minimum:"1"`
	EmpresaID int64   `json:"empresaId" minimum:"1"`
	Nota      *string `json:"nota,omitempty" maxLength:"1000"`
}

// MarshalJSON: sin la nota (puede tener datos del cliente).
func (e EntradaPasarEmpresa) MarshalJSON() ([]byte, error) {
	largo := 0
	if e.Nota != nil {
		largo = len([]rune(*e.Nota))
	}
	return json.Marshal(map[string]any{"id": e.ID, "version": e.Version, "empresa_id": e.EmpresaID, "largo_nota": largo})
}

func nuevoPasarEmpresa(m *Modulo) *auditoria.Caso[EntradaPasarEmpresa, ConversacionCliente] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPasarEmpresa, ConversacionCliente]{
		Accion: "conversaciones.pasar_empresa", Permiso: nucleo.PermConversacionesTransferir,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPasarEmpresa) (ConversacionCliente, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ConversacionCliente{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, true)
			if err != nil {
				return ConversacionCliente{}, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("conversacion", c.ID)
			if err := exigirVersionConv(c, in.Version); err != nil {
				return ConversacionCliente{}, err
			}
			canal, err := q.CanalPorID(ctx, c.CanalID)
			if err != nil {
				return ConversacionCliente{}, err
			}
			if derefEmpresa(canal.EmpresaID) != empresaDeConv(c) {
				return ConversacionCliente{}, empresaNoValida("Solo la empresa dueña del número puede pasar sus conversaciones a otra empresa")
			}
			p, err := q.PrestamoActivo(ctx, mensajeriabd.PrestamoActivoParams{CanalID: canal.ID, EmpresaID: in.EmpresaID})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !tieneUsoConversacion(p.Usos)) {
				return ConversacionCliente{}, empresaNoValida("Solo a una empresa que usa este número para atender conversaciones")
			}
			if err != nil {
				return ConversacionCliente{}, err
			}
			vinculos, err := q.ConversacionTieneVinculos(ctx, c.ID)
			if err != nil {
				return ConversacionCliente{}, err
			}
			if vinculos {
				return ConversacionCliente{}, reglaConv("conversacion_con_vinculos", "Tiene tickets, órdenes, prospectos, boletas o promesas de tu empresa: no se puede pasar")
			}
			if _, err := q.ConversacionPorContacto(ctx, mensajeriabd.ConversacionPorContactoParams{Empresa: in.EmpresaID, CanalID: canal.ID, Numero: c.Numero}); err == nil {
				return ConversacionCliente{}, nucleo.Conflicto("conversacion_existente", "Esa empresa ya tiene una conversación con este número")
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return ConversacionCliente{}, err
			}
			dueno := ""
			if canal.EmpresaID != nil {
				if dueno, err = q.NombreEmpresa(ctx, *canal.EmpresaID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return ConversacionCliente{}, err
				}
			}
			antes := c
			for _, quitar := range []func(context.Context, int64) error{q.QuitarEtiquetasConversacion, q.QuitarParticipantesConversacion, q.QuitarLecturasConversacion} {
				if err := quitar(ctx, c.ID); err != nil {
					return ConversacionCliente{}, err
				}
			}
			// Pasan los mensajes del cliente y los salientes (con sus adjuntos). Las notas internas,
			// sus menciones y los eventos de sistema de la dueña se quedan con la dueña: la otra
			// empresa no los ve (los mensajes se listan por la empresa de la conversación).
			destino := in.EmpresaID
			if err := q.MoverMensajesAEmpresa(ctx, mensajeriabd.MoverMensajesAEmpresaParams{EmpresaID: &destino, ConversacionID: c.ID}); err != nil {
				return ConversacionCliente{}, err
			}
			if err := q.MoverAdjuntosAEmpresa(ctx, mensajeriabd.MoverAdjuntosAEmpresaParams{EmpresaID: &destino, ConversacionID: c.ID}); err != nil {
				return ConversacionCliente{}, err
			}
			c, err = q.MoverConversacionAEmpresa(ctx, mensajeriabd.MoverConversacionAEmpresaParams{ID: c.ID, EmpresaID: &destino, ColaID: p.ColaID})
			if err != nil {
				return ConversacionCliente{}, err
			}
			// Ya en la otra empresa: la nota para quien la recibe y «Pasada por <dueña>».
			if in.Nota != nil && strings.TrimSpace(*in.Nota) != "" {
				nota := strings.TrimSpace(*in.Nota)
				if _, err := m.agregarMensaje(ctx, t, &c, nuevoMensaje{direccion: DirNota, tipo: "texto", texto: &nota, autor: &a.usuario}); err != nil {
					return ConversacionCliente{}, err
				}
			}
			if err := m.sistema(ctx, t, &c, EventoSistema{Tipo: "pasada_a_empresa", Detalle: &dueno}); err != nil {
				return ConversacionCliente{}, err
			}
			// El feed de las dos empresas: la anterior deja de verla, la nueva la recibe.
			if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
				return ConversacionCliente{}, err
			}
			t.RegistrarCambios("conversacion", c.ID, auditableConv(antes), auditableConv(c))
			if err := t.Emitir("conversacion.empresa_asignada", map[string]any{"conversacion_id": c.ID, "empresa_id": destino, "canal_id": c.CanalID,
				"motivo": RutaPasada, "anterior_empresa_id": antes.EmpresaID, "actor_id": a.usuario}); err != nil {
				return ConversacionCliente{}, err
			}
			out, err := m.resumirConv(ctx, q, a.usuario, c.ID)
			if err != nil {
				return ConversacionCliente{}, err
			}
			out.SoloLectura = true
			return out, nil
		},
	})
}

func ptrTiempo(t time.Time) *time.Time { return &t }
