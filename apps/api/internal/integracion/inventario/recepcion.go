package inventario

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Límites de la firma del contrato 0.5.0 §3.3: PNG de 200 KB como máximo, de 50×20 a 1000×400 px.
const (
	FirmaMaxBytes = 200 * 1000
	FirmaMinAncho = 50
	FirmaMinAlto  = 20
	FirmaMaxAncho = 1000
	FirmaMaxAlto  = 400
)

// tolerancia de reloj del teléfono (recibido_en no puede ser futuro, con 5 min de margen).
const toleranciaReloj = 5 * time.Minute

// EntradaRecibirTraslado son los `datos` de la operación traslado.recibir del lote de campo.
type EntradaRecibirTraslado struct {
	TrasladoID     int64            `json:"trasladoId" minimum:"1" doc:"Id de la orden de traslado en Argos"`
	Lineas         []LineaRecepcion `json:"lineas,omitempty" maxItems:"200" doc:"Solo si faltó algo: por línea lo recibido y las series que no llegaron (lo faltante vuelve al origen)"`
	FirmaAdjuntoID string           `json:"firmaAdjuntoId" format:"uuid" doc:"Adjunto tipo firma (PNG) de la entidad «traslado» con ese id; puede no haber terminado de subir"`
	RecibidoEn     time.Time        `json:"recibidoEn" doc:"Hora del teléfono al recibir (RFC 3339)"`
}

// LineaRecepcion es una línea con faltantes.
type LineaRecepcion struct {
	LineaID          int64    `json:"lineaId" minimum:"1"`
	CantidadRecibida float64  `json:"cantidadRecibida" minimum:"0"`
	SeriesFaltantes  []string `json:"seriesFaltantes,omitempty" maxItems:"500"`
}

// lineasArgos: lineas[] en el cuerpo de Argos (snake_case).
func lineasArgos(ls []LineaRecepcion) []map[string]any {
	salida := make([]map[string]any, 0, len(ls))
	for _, l := range ls {
		series := l.SeriesFaltantes
		if series == nil {
			series = []string{}
		}
		salida = append(salida, map[string]any{"linea_id": l.LineaID, "cantidad_recibida": l.CantidadRecibida, "series_faltantes": series})
	}
	return salida
}

func problemaRecepcion(status int, codigo, detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(status, codigo, "Recepción rechazada", detalle)
}

// Encolar registra la recepción en la transacción de la operación del lote (campo la llama con su
// *auditoria.Tx después de reclamar la operación). Valida el vínculo, Argos ≥ 1.6.0, lo que Iris sabe
// del traslado (modelo de lectura), la firma y los datos; encola el pendiente `recepcion`
// (iris:recepcion:<traslado_id>) esperando la firma y el job que arma el cuerpo. Otra operación del
// mismo técnico para el mismo traslado devuelve la recepción existente; de otro técnico, 409.
func (m *Modulo) Encolar(ctx context.Context, t *auditoria.Tx, in EntradaRecibirTraslado) (EstadoRecepcion, error) {
	usuario, err := usuarioDe(ctx)
	if err != nil {
		return EstadoRecepcion{}, err
	}
	if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermCampoRecibirTraslado)) {
		return EstadoRecepcion{}, nucleo.Prohibido("Falta el permiso campo.recibir_traslado")
	}
	if m.cfg.Argos == nil {
		return EstadoRecepcion{}, m.sinArgosFn()
	}
	v, err := vinculoDe(ctx, t, usuario)
	if err != nil {
		return EstadoRecepcion{}, err
	}
	ok, err := m.cfg.Argos.InventarioPorTecnico(ctx, t)
	if err != nil {
		return EstadoRecepcion{}, err
	}
	if !ok {
		return EstadoRecepcion{}, argos.SinInventarioTecnico()
	}
	if in.TrasladoID <= 0 {
		return EstadoRecepcion{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "trasladoId", Message: "Obligatorio", Rule: "required"})
	}
	t.FijarEntidad(EntidadTraslado, in.TrasladoID)
	empresa := contexto.EmpresaDe(ctx)
	if err := m.exigirTraslado(ctx, t, in.TrasladoID, v, empresa); err != nil {
		return EstadoRecepcion{}, err
	}
	firma, err := uuid.Parse(in.FirmaAdjuntoID)
	if err != nil {
		return EstadoRecepcion{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "firmaAdjuntoId", Message: "Debe ser un uuid", Rule: "uuid"})
	}
	a, err := adjuntos.DeEntidad(ctx, t, firma, EntidadTraslado, strconv.FormatInt(in.TrasladoID, 10))
	if err != nil {
		return EstadoRecepcion{}, problemaRecepcion(http.StatusUnprocessableEntity, "firma_invalida",
			"La firma no está registrada como adjunto de este traslado (POST /adjuntos con entidad traslado)")
	}
	if a.Tipo != "firma" || a.Mime != "image/png" || a.Tamano > FirmaMaxBytes {
		return EstadoRecepcion{}, problemaRecepcion(http.StatusUnprocessableEntity, "firma_invalida",
			fmt.Sprintf("La firma debe ser un adjunto tipo firma en PNG de %d KB como máximo", FirmaMaxBytes/1000))
	}
	ahora := m.ahora()
	if in.RecibidoEn.IsZero() || in.RecibidoEn.After(ahora.Add(toleranciaReloj)) {
		return EstadoRecepcion{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "recibidoEn", Message: "Hora del teléfono inválida o futura", Rule: "fecha"})
	}
	vistas := map[int64]bool{}
	for i, l := range in.Lineas {
		if l.LineaID <= 0 || l.CantidadRecibida < 0 || vistas[l.LineaID] {
			return EstadoRecepcion{}, nucleo.Validacion(nucleo.ErrorCampo{Field: fmt.Sprintf("lineas[%d]", i), Message: "Línea repetida o inválida", Rule: "linea"})
		}
		vistas[l.LineaID] = true
	}
	borrador := map[string]any{
		"recibido_por": map[string]any{"id_externo": v.IDExterno}, "metodo_recepcion": "firma_digital",
		"recibido_en": in.RecibidoEn.UTC().Format(time.RFC3339), "firma_adjunto_id": firma.String(),
	}
	if len(in.Lineas) > 0 {
		borrador["lineas"] = lineasArgos(in.Lineas)
	}
	cuerpo, err := json.Marshal(borrador)
	if err != nil {
		return EstadoRecepcion{}, err
	}
	var empresaID *int64
	if empresa != 0 {
		empresaID = &empresa
	}
	clave := ClaveRecepcion(in.TrasladoID)
	r, creado, err := pendientes.Encolar(ctx, t, pendientes.Accion{
		EmpresaID: empresaID, Operacion: pendientes.OpRecepcion, IdempotencyKey: clave, Entidad: EntidadTraslado,
		EntidadID: strconv.FormatInt(in.TrasladoID, 10), Ruta: "/inventario/traslados/" + strconv.FormatInt(in.TrasladoID, 10) + "/recibir",
		Cuerpo: cuerpo, Desde: ahora, TecnicoUsuarioID: &usuario, Esperando: EsperandoFirma,
	}, nil)
	if err != nil {
		return EstadoRecepcion{}, err
	}
	if !creado {
		if r.TecnicoUsuarioID == nil || *r.TecnicoUsuarioID != usuario {
			return EstadoRecepcion{}, problemaRecepcion(http.StatusConflict, "recepcion_existente", "Otra persona ya registró la recepción de este traslado")
		}
		if r.Estado != pendientes.EstadoFallidoNegocio {
			t.Anotar(map[string]any{"recepcion_existente": r.ID})
			return estadoRecepcion(r), nil
		}
		// Rechazada (firma inválida, líneas inválidas…): la nueva operación la rehace con sus datos.
		rehecho, ok, err := pendientes.RehacerEsperando(ctx, t, r.ID, EsperandoFirma, cuerpo)
		if err != nil {
			return EstadoRecepcion{}, err
		}
		if !ok {
			return estadoRecepcion(r), nil
		}
		r = rehecho
		t.Anotar(map[string]any{"recepcion_rehecha": r.ID})
	}
	t.Anotar(map[string]any{"pendiente_id": r.ID, "firma_adjunto_id": firma})
	if m.cfg.Encolar != nil {
		if err := m.cfg.Encolar(ctx, t.Tx, ArgsArmarFirma{PendienteID: r.ID}); err != nil {
			return EstadoRecepcion{}, err
		}
	}
	return estadoRecepcion(r), nil
}

// exigirTraslado: lo que Iris sabe del traslado por los webhooks. Si no lo conoce (el webhook aún
// no llega), Argos lo valida al recibir.
func (m *Modulo) exigirTraslado(ctx context.Context, q argosbd.DBTX, id int64, v vinculo, empresa int64) error {
	f, err := argosbd.New(q).TrasladoPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	switch {
	case f.Estado == "cancelado":
		return problemaRecepcion(http.StatusConflict, "traslado_cancelado", "Argos canceló esta orden de traslado")
	case (f.TecnicoIDExterno != nil && *f.TecnicoIDExterno != v.IDExterno) ||
		(f.TecnicoIDExterno == nil && f.TecnicoCorreo != nil && !mismoCorreo(*f.TecnicoCorreo, v.Correo)) ||
		(empresa != 0 && f.EmpresaID != nil && *f.EmpresaID != empresa):
		return problemaRecepcion(http.StatusUnprocessableEntity, "traslado_de_otro_tecnico", "La orden de traslado es para la bodega de otro técnico")
	}
	return nil
}

func mismoCorreo(a, b string) bool {
	return a != "" && bytes.EqualFold([]byte(a), []byte(b))
}

// Armar el cuerpo con la firma ---------------------------------------------------------------

// ResultadoFirma: qué pasó al armar la firma.
type ResultadoFirma struct {
	PendienteID int64  `json:"pendiente_id"`
	Resultado   string `json:"resultado" doc:"lista (salió a la cola), esperando (la firma no termina de subir), invalida (fallido_negocio firma_invalida) o nada"`
}

type entradaArmar struct {
	PendienteID int64  `json:"pendiente_id"`
	Codigo      string `json:"codigo,omitempty"`
	Detalle     string `json:"detalle,omitempty"`
	cuerpo      []byte
}

func nuevoGuardarFirma(m *Modulo) *auditoria.Caso[entradaArmar, ResultadoFirma] {
	return auditoria.Nuevo(auditoria.Caso[entradaArmar, ResultadoFirma]{
		Accion: "integraciones.armar_recepcion", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoFirma) bool { return r.Resultado != "nada" },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaArmar) (ResultadoFirma, error) {
			t.FijarEntidad("pendiente", in.PendienteID)
			r := ResultadoFirma{PendienteID: in.PendienteID, Resultado: "nada"}
			if in.Codigo != "" {
				_, ok, err := pendientes.FallarEsperando(ctx, t, in.PendienteID, EsperandoFirma, in.Codigo, in.Detalle)
				if ok {
					r.Resultado = "invalida"
				}
				return r, err
			}
			var aviso pendientes.Encolador
			if m.cfg.Encolar != nil {
				aviso = m.cfg.Encolar
			}
			_, ok, err := pendientes.CompletarEsperando(ctx, t, in.PendienteID, EsperandoFirma, in.cuerpo, m.ahora(), aviso)
			if ok {
				r.Resultado = "lista"
			}
			return r, err
		},
	})
}

// ArmarFirma arma el cuerpo de una recepción que espera la firma: si el adjunto está completo, lee
// el PNG (fuera de transacción), verifica los límites del contrato y deja el pendiente en la cola con
// { firma: { png_base64, sha256 } }; si la firma no sirve (o se rechazó al subir), lo deja
// fallido_negocio con firma_invalida sin llamar a Argos. Si aún no termina de subir, no hace nada (lo
// retoma adjunto.completado o el barrido). Idempotente.
func (m *Modulo) ArmarFirma(ctx context.Context, pendienteID int64) (ResultadoFirma, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ResultadoFirma{}, nucleo.ErrSinBD
	}
	p, err := pendientes.PorID(ctx, bd, pendienteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ResultadoFirma{PendienteID: pendienteID, Resultado: "nada"}, nil
	}
	if err != nil {
		return ResultadoFirma{}, err
	}
	if p.Operacion != pendientes.OpRecepcion || p.Estado != pendientes.EstadoBloqueado || p.UltimoError == nil || *p.UltimoError != EsperandoFirma {
		return ResultadoFirma{PendienteID: pendienteID, Resultado: "nada"}, nil
	}
	var borrador map[string]any
	if err := json.Unmarshal(p.Payload, &borrador); err != nil {
		return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Codigo: "firma_invalida", Detalle: "cuerpo de la recepción ilegible"})
	}
	idFirma, _ := borrador["firma_adjunto_id"].(string)
	firma, err := uuid.Parse(idFirma)
	if err != nil {
		return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Codigo: "firma_invalida", Detalle: "la recepción no tiene firma"})
	}
	a, err := adjuntos.DeEntidad(ctx, bd, firma, EntidadTraslado, p.EntidadID)
	if err != nil {
		return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Codigo: "firma_invalida", Detalle: "la firma ya no es un adjunto del traslado"})
	}
	switch a.Estado {
	case adjuntos.EstadoRechazado:
		return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Codigo: "firma_invalida",
			Detalle: "la firma se rechazó al subir (" + deref(a.MotivoRechazo) + "): registra otra"})
	case adjuntos.EstadoCompleto:
	default:
		return ResultadoFirma{PendienteID: p.ID, Resultado: "esperando"}, nil
	}
	if m.cfg.Almacen == nil {
		return ResultadoFirma{PendienteID: p.ID, Resultado: "esperando"}, nil
	}
	png, _, err := adjuntos.Contenido(ctx, m.cfg.Almacen, bd, firma, EntidadTraslado, p.EntidadID, FirmaMaxBytes)
	var pr *nucleo.Problema
	if errors.As(err, &pr) && pr.Codigo() == adjuntos.MotivoTamanoExcedido {
		return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Codigo: "firma_invalida",
			Detalle: fmt.Sprintf("la firma pasa de %d KB", FirmaMaxBytes/1000)})
	}
	if err != nil {
		return ResultadoFirma{}, err
	}
	if motivo := validarPNG(png); motivo != "" {
		return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Codigo: "firma_invalida", Detalle: motivo})
	}
	suma := sha256.Sum256(png)
	delete(borrador, "firma_adjunto_id")
	borrador["firma"] = map[string]any{"png_base64": base64.StdEncoding.EncodeToString(png), "sha256": hex.EncodeToString(suma[:])}
	cuerpo, err := json.Marshal(borrador)
	if err != nil {
		return ResultadoFirma{}, err
	}
	return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, cuerpo: cuerpo})
}

// validarPNG: un PNG legible de 50×20 a 1000×400 px (contrato 0.5.0 §3.3). Vacío si sirve.
func validarPNG(b []byte) string {
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return "la firma no es un PNG"
	}
	if cfg.Width < FirmaMinAncho || cfg.Height < FirmaMinAlto || cfg.Width > FirmaMaxAncho || cfg.Height > FirmaMaxAlto {
		return fmt.Sprintf("la firma mide %d×%d px (debe medir de %d×%d a %d×%d)", cfg.Width, cfg.Height,
			FirmaMinAncho, FirmaMinAlto, FirmaMaxAncho, FirmaMaxAlto)
	}
	return ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Jobs y suscriptores (cmd/worker) -----------------------------------------------------------

// ArgsArmarFirma arma el cuerpo de una recepción (se encola al recibir la operación del lote).
type ArgsArmarFirma struct {
	PendienteID int64 `json:"pendiente_id"`
}

// Kind implementa river.JobArgs.
func (ArgsArmarFirma) Kind() string { return "integracion.recepcion_armar_firma" }

// InsertOpts: uno por pendiente a la vez.
func (ArgsArmarFirma) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 10, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Minute}}
}

// ArgsBarrerFirmas (periódico) retoma las recepciones que esperan una firma ya subida.
type ArgsBarrerFirmas struct{}

// Kind implementa river.JobArgs.
func (ArgsBarrerFirmas) Kind() string { return "integracion.recepcion_barrer_firmas" }

// InsertOpts: único por 5 min.
func (ArgsBarrerFirmas) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 5 * time.Minute}}
}

type trabajadorFirma struct {
	river.WorkerDefaults[ArgsArmarFirma]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorFirma) Work(ctx context.Context, job *river.Job[ArgsArmarFirma]) error {
	_, err := w.m.ArmarFirma(nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd), job.Args.PendienteID)
	return err
}

type trabajadorBarrido struct {
	river.WorkerDefaults[ArgsBarrerFirmas]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorBarrido) Work(ctx context.Context, _ *river.Job[ArgsBarrerFirmas]) error {
	_, err := w.m.BarrerFirmas(nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd))
	return err
}

// BarrerFirmas arma las recepciones que esperan firma (red de seguridad). Devuelve cuántas tocó.
func (m *Modulo) BarrerFirmas(ctx context.Context) (int, error) {
	ps, err := pendientes.Esperando(ctx, nucleo.BDDe(ctx), pendientes.OpRecepcion, EsperandoFirma, 200)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range ps {
		r, err := m.ArmarFirma(ctx, p.ID)
		if err != nil {
			return n, err
		}
		if r.Resultado != "esperando" && r.Resultado != "nada" {
			n++
		}
	}
	return n, nil
}

// RegistrarWorkers agrega los workers al cliente de River.
func (m *Modulo) RegistrarWorkers(workers *river.Workers, bd nucleo.BD) {
	river.AddWorker(workers, &trabajadorFirma{m: m, bd: bd})
	river.AddWorker(workers, &trabajadorBarrido{m: m, bd: bd})
}

// TrabajoPeriodico: el barrido cada intervalo (5 min por defecto).
func TrabajoPeriodico(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = 5 * time.Minute
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsBarrerFirmas{}, nil }, &river.PeriodicJobOpts{RunOnStart: true})
}

// Suscribir: cuando la firma de un traslado termina de subir (o se rechaza), se arma la recepción.
func (m *Modulo) Suscribir(bus *integracion.Bus, bd nucleo.BD) {
	alAdjunto := func(ctx context.Context, e integracion.Evento) error {
		var a struct {
			Entidad   string `json:"entidad"`
			EntidadID string `json:"entidad_id"`
		}
		if json.Unmarshal(e.Payload, &a) != nil || a.Entidad != EntidadTraslado {
			return nil
		}
		ctx = nucleo.ConBD(ctx, bd)
		id, err := strconv.ParseInt(a.EntidadID, 10, 64)
		if err != nil {
			return nil
		}
		p, err := pendientes.PorClave(ctx, bd, ClaveRecepcion(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // la operación del lote llega después y encola el armado
		}
		if err != nil {
			return err
		}
		_, err = m.ArmarFirma(ctx, p.ID)
		return err
	}
	bus.Suscribir("inventario.recepcion_firma", adjuntos.EventoCompletado, alAdjunto)
	bus.Suscribir("inventario.recepcion_firma_rechazada", adjuntos.EventoRechazado, alAdjunto)
}

// Adjuntos: la firma es un adjunto de la entidad «traslado» -----------------------------------

// RegistrarAdjuntos registra la entidad «traslado»: sube y ve la firma el técnico al que va la orden
// de traslado (por su vínculo), si Iris la conoce, o cualquier técnico vinculado si el webhook aún no
// llega (Argos valida al recibir). La empresa es la del traslado (o la activa).
func RegistrarAdjuntos() {
	registroAdjuntos.Do(registrarAdjuntos)
}

var registroAdjuntos sync.Once

func registrarAdjuntos() {
	adjuntos.RegistrarEntidadAdjuntable(EntidadTraslado, func(ctx context.Context, q adjuntos.DBTX, entidadID string, uso adjuntos.Uso) (int64, error) {
		id, err := strconv.ParseInt(entidadID, 10, 64)
		if err != nil || id <= 0 {
			return 0, nucleo.NoEncontrado("")
		}
		permiso := nucleo.PermCampoRecibirTraslado
		if uso == adjuntos.UsoVer {
			permiso = nucleo.PermCampoVerBodega
		}
		usuario, err := usuarioDe(ctx)
		if err != nil || !contexto.PermisosDe(ctx).Tiene(string(permiso)) {
			return 0, nucleo.NoEncontrado("")
		}
		v, err := vinculoDe(ctx, q, usuario)
		if err != nil {
			return 0, err
		}
		empresa := contexto.EmpresaDe(ctx)
		f, err := argosbd.New(q).TrasladoPorID(ctx, id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return empresa, nil
		case err != nil:
			return 0, err
		case (f.TecnicoIDExterno != nil && *f.TecnicoIDExterno != v.IDExterno) ||
			(f.TecnicoIDExterno == nil && f.TecnicoCorreo != nil && !mismoCorreo(*f.TecnicoCorreo, v.Correo)):
			return 0, nucleo.NoEncontrado("")
		}
		if f.EmpresaID != nil {
			return *f.EmpresaID, nil
		}
		return empresa, nil
	})
}
