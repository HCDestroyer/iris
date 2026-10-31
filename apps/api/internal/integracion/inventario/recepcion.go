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
	"sync/atomic"
	"time"
	"unicode/utf8"

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

// tolerancia de reloj del teléfono (recibido_en no puede ser futuro ni anterior al envío del
// traslado, con 5 min de margen).
const toleranciaReloj = 5 * time.Minute

// SerieMaxLargo: largo máximo de una serie faltante.
const SerieMaxLargo = 100

// Códigos de una recepción de un traslado que Iris no conocía (contratos/sync.md §3). Nunca se encola
// nada ni se toma la clave iris:recepcion:<id> sin saber a qué técnico va el traslado.
const (
	// CodigoTrasladoSinConfirmar: Argos no respondió al confirmar el traslado (caída, 5xx, 429 o cuota
	// de lecturas): reintentable, el teléfono espera solo esa operación (no detiene la cola).
	CodigoTrasladoSinConfirmar = "traslado_sin_confirmar"
	// CodigoTrasladoNoEncontrado: Argos no lo tiene (404): definitivo («Descartar»).
	CodigoTrasladoNoEncontrado = "traslado_no_encontrado"
	// CodigoTrasladoSinTecnico: Argos lo tiene pero no va a la bodega de un técnico: definitivo.
	CodigoTrasladoSinTecnico = "traslado_sin_tecnico"
	// CodigoRecepcionEnEnvio: la recepción que registró otro usuario se está mandando a Argos ahora:
	// reintentable (al terminar, el destinatario la toma o ve que ya quedó).
	CodigoRecepcionEnEnvio = "recepcion_en_envio"
)

// TrasladoSinConfirmar: el error reintentable de una recepción que no se pudo confirmar con Argos.
func TrasladoSinConfirmar(detalle string) *nucleo.Problema {
	return problemaRecepcion(http.StatusServiceUnavailable, CodigoTrasladoSinConfirmar, detalle)
}

func trasladoSinTecnico() *nucleo.Problema {
	return problemaRecepcion(http.StatusUnprocessableEntity, CodigoTrasladoSinTecnico,
		"Argos no dice a qué técnico va esta orden de traslado: no se puede recibir desde la app")
}

// ConfirmarTraslado corre antes de la transacción de la operación traslado.recibir: si Iris no
// conoce el traslado (o lo conoce sin técnico), lo confirma con Argos (GET /inventario/traslados/:id,
// con la protección de lecturas del conector) y lo guarda. Solo para quien puede recibir (permiso,
// vínculo, Argos ≥ 1.6.0); si no, no hace nada y Encolar da el error que toca. 404 →
// traslado_no_encontrado; Argos sin responder → traslado_sin_confirmar (reintentable); sin técnico
// después de leerlo → traslado_sin_tecnico.
func (m *Modulo) ConfirmarTraslado(ctx context.Context, id int64) error {
	bd := nucleo.BDDe(ctx)
	if m.cfg.Argos == nil || bd == nil || id <= 0 || !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermCampoRecibirTraslado)) {
		return nil
	}
	usuario, err := usuarioDe(ctx)
	if err != nil {
		return nil
	}
	if _, err := vinculoDe(ctx, bd, usuario); err != nil {
		return nil
	}
	if ok, err := m.cfg.Argos.InventarioPorTecnico(ctx, bd); err != nil || !ok {
		return err
	}
	q := argosbd.New(bd)
	f, err := q.TrasladoPorID(ctx, id)
	switch {
	case err == nil && conTecnico(f):
		return nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return err
	}
	err = m.cfg.Argos.ConfirmarTraslado(ctx, id, contexto.EmpresaDe(ctx))
	switch {
	case errors.Is(err, argos.ErrTrasladoNoEncontrado):
		return problemaRecepcion(http.StatusNotFound, CodigoTrasladoNoEncontrado, "Argos no tiene esta orden de traslado")
	case errors.Is(err, argos.ErrTrasladoNoConfirmado):
		return TrasladoSinConfirmar("No se pudo confirmar la orden de traslado con Argos: se reintenta sola")
	case err != nil:
		return err
	}
	f, err = q.TrasladoPorID(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return TrasladoSinConfirmar("Argos respondió, pero la orden de traslado no quedó guardada: se reintenta sola")
	case err != nil:
		return err
	case !conTecnico(f):
		return trasladoSinTecnico()
	}
	return nil
}

func conTecnico(f argosbd.IntegracionArgosTraslado) bool {
	return f.TecnicoIDExterno != nil || f.TecnicoCorreo != nil
}

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
	SeriesFaltantes  []string `json:"seriesFaltantes,omitempty" maxItems:"500" doc:"Cada serie de 1 a 100 caracteres"`
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
	tr, err := m.exigirTraslado(ctx, t, in.TrasladoID, v, empresa)
	if err != nil {
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
	if tr.EnviadoEn != nil && in.RecibidoEn.Before(tr.EnviadoEn.Add(-toleranciaReloj)) {
		return EstadoRecepcion{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "recibidoEn",
			Message: "Anterior al envío del traslado (" + tr.EnviadoEn.UTC().Format(time.RFC3339) + ")", Rule: "fecha"})
	}
	vistas := map[int64]bool{}
	for i, l := range in.Lineas {
		if l.LineaID <= 0 || l.CantidadRecibida < 0 || vistas[l.LineaID] {
			return EstadoRecepcion{}, nucleo.Validacion(nucleo.ErrorCampo{Field: fmt.Sprintf("lineas[%d]", i), Message: "Línea repetida o inválida", Rule: "linea"})
		}
		vistas[l.LineaID] = true
		for j, serie := range l.SeriesFaltantes {
			if n := utf8.RuneCountInString(serie); n < 1 || n > SerieMaxLargo {
				return EstadoRecepcion{}, nucleo.Validacion(nucleo.ErrorCampo{Field: fmt.Sprintf("lineas[%d].seriesFaltantes[%d]", i, j),
					Message: fmt.Sprintf("De 1 a %d caracteres", SerieMaxLargo), Rule: "maxLength"})
			}
		}
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
		existente := problemaRecepcion(http.StatusConflict, "recepcion_existente", "Otra persona ya registró la recepción de este traslado")
		var nuevoDueno *int64
		switch {
		case r.TecnicoUsuarioID != nil && *r.TecnicoUsuarioID == usuario:
			if r.Estado != pendientes.EstadoFallidoNegocio {
				t.Anotar(map[string]any{"recepcion_existente": r.ID})
				return estadoRecepcion(r), nil
			}
			// Rechazada (firma inválida, líneas inválidas…): la nueva operación la rehace con sus datos.
		case r.Estado == pendientes.EstadoHecho || r.Estado == pendientes.EstadoResueltoManual:
			return EstadoRecepcion{}, existente
		case r.Estado == pendientes.EstadoEnCurso:
			// Se está mandando ahora: al terminar se sabrá si quedó (o si el destinatario la toma).
			return EstadoRecepcion{}, problemaRecepcion(http.StatusConflict, CodigoRecepcionEnEnvio,
				"La recepción que registró otra persona se está enviando a Argos: se reintenta sola")
		default:
			// De otro usuario y sin terminar: el destinatario real (este, ya verificado) la toma si el
			// otro no lo es según lo que Iris sabe del traslado, o si Argos ya la rechazó. Así nadie
			// bloquea la recepción de otro tomando su clave.
			otroEsDestinatario := false
			if r.TecnicoUsuarioID != nil {
				if ov, err := vinculoDe(ctx, t, *r.TecnicoUsuarioID); err == nil {
					otroEsDestinatario = esDestinatario(tr, ov)
				}
			}
			if otroEsDestinatario && r.Estado != pendientes.EstadoFallidoNegocio {
				return EstadoRecepcion{}, existente
			}
			nuevoDueno = &usuario
		}
		rehecho, ok, err := pendientes.RehacerEsperando(ctx, t, r.ID, EsperandoFirma, cuerpo, nuevoDueno)
		if err != nil {
			return EstadoRecepcion{}, err
		}
		if !ok {
			if nuevoDueno != nil {
				return EstadoRecepcion{}, existente
			}
			return estadoRecepcion(r), nil
		}
		if nuevoDueno != nil {
			t.Anotar(map[string]any{"recepcion_tomada": r.ID, "de_usuario_id": r.TecnicoUsuarioID, "estado_anterior": r.Estado})
		} else {
			t.Anotar(map[string]any{"recepcion_rehecha": r.ID})
		}
		r = rehecho
	}
	t.Anotar(map[string]any{"pendiente_id": r.ID, "firma_adjunto_id": firma})
	if m.cfg.Encolar != nil {
		if err := m.cfg.Encolar(ctx, t.Tx, ArgsArmarFirma{PendienteID: r.ID}); err != nil {
			return EstadoRecepcion{}, err
		}
	}
	return estadoRecepcion(r), nil
}

// exigirTraslado: lo que Iris sabe del traslado (webhooks y ConfirmarTraslado). Sin conocerlo, o sin
// saber a qué técnico va, no se encola: si no, cualquiera podría tomar la clave iris:recepcion:<id> de
// un traslado ajeno (o futuro) y bloquear al destinatario.
func (m *Modulo) exigirTraslado(ctx context.Context, q argosbd.DBTX, id int64, v vinculo, empresa int64) (argosbd.IntegracionArgosTraslado, error) {
	f, err := argosbd.New(q).TrasladoPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, TrasladoSinConfirmar("Iris aún no conoce esta orden de traslado: se reintenta sola")
	}
	if err != nil {
		return f, err
	}
	switch {
	case f.Estado == "cancelado":
		return f, problemaRecepcion(http.StatusConflict, "traslado_cancelado", "Argos canceló esta orden de traslado")
	case !conTecnico(f):
		return f, trasladoSinTecnico()
	case !esDestinatario(f, v) || (empresa != 0 && f.EmpresaID != nil && *f.EmpresaID != empresa):
		return f, problemaRecepcion(http.StatusUnprocessableEntity, "traslado_de_otro_tecnico", "La orden de traslado es para la bodega de otro técnico")
	}
	return f, nil
}

// esDestinatario: el traslado va al técnico de ese vínculo (por id_externo o, sin él, por correo).
func esDestinatario(f argosbd.IntegracionArgosTraslado, v vinculo) bool {
	if f.TecnicoIDExterno != nil {
		return *f.TecnicoIDExterno == v.IDExterno
	}
	return f.TecnicoCorreo != nil && mismoCorreo(*f.TecnicoCorreo, v.Correo)
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
	Version     int32  `json:"version"`
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
				_, ok, err := pendientes.FallarEsperando(ctx, t, in.PendienteID, in.Version, EsperandoFirma, in.Codigo, in.Detalle)
				if ok {
					r.Resultado = "invalida"
				}
				return r, err
			}
			var aviso pendientes.Encolador
			if m.cfg.Encolar != nil {
				aviso = m.cfg.Encolar
			}
			_, ok, err := pendientes.CompletarEsperando(ctx, t, in.PendienteID, in.Version, EsperandoFirma, in.cuerpo, m.ahora(), aviso)
			if ok {
				r.Resultado = "lista"
			}
			return r, err
		},
	})
}

// ArmarFirma arma el cuerpo de una recepción que espera la firma: si el adjunto está completo, lee
// el PNG (fuera de transacción; al guardar exige la misma version, así que un armado viejo no pisa
// una recepción rehecha o tomada mientras tanto), verifica los límites del contrato y deja el pendiente en la cola con
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
		return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Version: p.Version, Codigo: "firma_invalida", Detalle: "cuerpo de la recepción ilegible"})
	}
	idFirma, _ := borrador["firma_adjunto_id"].(string)
	firma, err := uuid.Parse(idFirma)
	if err != nil {
		return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Version: p.Version, Codigo: "firma_invalida", Detalle: "la recepción no tiene firma"})
	}
	a, err := adjuntos.DeEntidad(ctx, bd, firma, EntidadTraslado, p.EntidadID)
	if err != nil {
		return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Version: p.Version, Codigo: "firma_invalida", Detalle: "la firma ya no es un adjunto del traslado"})
	}
	switch a.Estado {
	case adjuntos.EstadoRechazado:
		return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Version: p.Version, Codigo: "firma_invalida",
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
		return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Version: p.Version, Codigo: "firma_invalida",
			Detalle: fmt.Sprintf("la firma pasa de %d KB", FirmaMaxBytes/1000)})
	}
	if err != nil {
		return ResultadoFirma{}, err
	}
	if motivo := validarPNG(png); motivo != "" {
		return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Version: p.Version, Codigo: "firma_invalida", Detalle: motivo})
	}
	suma := sha256.Sum256(png)
	delete(borrador, "firma_adjunto_id")
	borrador["firma"] = map[string]any{"png_base64": base64.StdEncoding.EncodeToString(png), "sha256": hex.EncodeToString(suma[:])}
	cuerpo, err := json.Marshal(borrador)
	if err != nil {
		return ResultadoFirma{}, err
	}
	return m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: p.ID, Version: p.Version, cuerpo: cuerpo})
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
	// Decodificación completa (las medidas ya están acotadas): un PNG corrupto se rechaza aquí y no
	// en Argos.
	if _, err := png.Decode(bytes.NewReader(b)); err != nil {
		return "la firma es un PNG dañado"
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

// RegistrarAdjuntos registra la entidad «traslado»: sube y ve la firma solo el técnico al que va la
// orden de traslado (por su vínculo). Un traslado que Iris no conoce se confirma con Argos al subir
// la firma (ConfirmarTraslado de este módulo: la firma se sube antes de mandar traslado.recibir);
// desconocido, sin técnico o ajeno no admite ni muestra firmas (no encontrado; Argos sin responder:
// 503 traslado_sin_confirmar, reintentable). La empresa es la del traslado (o la activa).
func (m *Modulo) RegistrarAdjuntos() {
	confirmador.Store(m)
	registroAdjuntos.Do(registrarAdjuntos)
}

var (
	registroAdjuntos sync.Once
	// confirmador: el módulo que confirma con Argos un traslado desconocido al subir su firma (el
	// registro de la entidad adjuntable es global).
	confirmador atomic.Pointer[Modulo]
)

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
		if (errors.Is(err, pgx.ErrNoRows) || (err == nil && !conTecnico(f))) && uso != adjuntos.UsoVer {
			// Se confirma con Argos y se guarda aparte (ya confirmado al volver, aunque esta alta se
			// rechace): esta transacción lo lee enseguida (READ COMMITTED).
			if c := confirmador.Load(); c != nil {
				if err := c.ConfirmarTraslado(nucleo.FueraDeTx(ctx), id); err != nil {
					if p, _ := nucleo.AProblema(err); p != nil && p.Codigo() == CodigoTrasladoSinConfirmar {
						return 0, err
					}
					return 0, nucleo.NoEncontrado("")
				}
				f, err = argosbd.New(q).TrasladoPorID(ctx, id)
			}
		}
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return 0, nucleo.NoEncontrado("")
		case err != nil:
			return 0, err
		case !conTecnico(f) || !esDestinatario(f, v):
			return 0, nucleo.NoEncontrado("")
		}
		if f.EmpresaID != nil {
			return *f.EmpresaID, nil
		}
		return empresa, nil
	})
}
