package pendientes

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
)

// Cambios e intentos al aplicarse (contrato con Argos 0.14.0 §3.10): con un Argos ≥ 0.14.0,
// equipo.cambiar y equipo.no_recuperado salen a Argos cuando se aplica la operación del lote, no al
// cerrar la orden (orden.cerrada solo los resume, con enviado_al_aplicar). Un intento (o un cambio
// con foto) sale solo con la foto completo: mientras tanto el pendiente espera (esperando_foto); si
// la foto se rechaza, foto_invalida; si en 72 h desde la fecha no quedó completo,
// foto_fuera_de_ventana. Las dos, conflicto para una persona.

// EsperandoFoto: el motivo del pendiente de un cambio o intento mientras su foto no termina de subir.
const EsperandoFoto = "esperando_foto"

// Códigos locales de la espera de la foto.
const (
	CodigoFotoInvalida             = "foto_invalida"
	CodigoFotoFueraDeVentana       = "foto_fuera_de_ventana"
	VentanaFoto                    = 72 * time.Hour
	entidadOrdenAdjunto            = "orden"
	limiteBarridoFotos       int32 = 200
)

// ClavesPorOperacion indica si Argos informa contrato ≥ 0.14.0 (falla cerrado: false sin conector).
func (m *Modulo) ClavesPorOperacion(ctx context.Context, t *auditoria.Tx) (bool, error) {
	if m.cfg.Argos == nil {
		return false, nil
	}
	return m.cfg.Argos.ClavesPorOperacion(ctx, t)
}

// EncolarAlAplicar encola el cambio o el intento de orden (el cuerpo de orden.cerrada con un solo
// equipo, de acción cambio o no_recuperado) en la transacción que lo aplica. Devuelve true si quedó
// encolado; false si no corresponde (sin conector, Argos < 0.14.0 o datos que solo el cierre puede
// completar): entonces lo encola el cierre, como en 0.12.0.
func (m *Modulo) EncolarAlAplicar(ctx context.Context, t *auditoria.Tx, orden []byte) (bool, error) {
	if m.cfg.Argos == nil {
		return false, nil
	}
	var o OrdenCerrada
	if err := json.Unmarshal(orden, &o); err != nil || o.OrdenID == "" || len(o.Equipos) != 1 {
		return false, errors.New("pendientes.EncolarAlAplicar: se espera una orden con un solo equipo")
	}
	e := o.Equipos[0]
	if e.Accion != AccionCambio && e.Accion != AccionNoRecuperado {
		return false, errors.New("pendientes.EncolarAlAplicar: solo cambio o no_recuperado")
	}
	registrado, err := m.cfg.Argos.HayConectorRegistrado(ctx, t)
	if err != nil || !registrado {
		return false, err
	}
	porOperacion, err := m.cfg.Argos.ClavesPorOperacion(ctx, t)
	if err != nil || !porOperacion {
		return false, err
	}
	q := pendientesbd.New(t)
	notas, err := notasDeEquipos(ctx, q, o)
	if err != nil {
		return false, err
	}
	o.Resultado = ResultadoExitosa
	o.RetirarServicio = false
	if e.Accion == AccionCambio {
		o.Tipo = TipoCambioEquipo
	} else {
		o.Tipo = TipoRetiro
	}
	planes, _ := planificar(o, opcionesPlan{equipos: true, porOperacion: true, notas: notas})
	var p *plan
	for i := range planes {
		if planes[i].operacion == OpCambio || planes[i].operacion == OpNoRecuperado {
			p = &planes[i]
		}
	}
	if p == nil || p.incompleto != "" || p.esperaPersona != "" || p.actor == nil {
		return false, nil
	}
	cuerpo, err := json.Marshal(p.payload)
	if err != nil {
		return false, err
	}
	esperando := ""
	if e.FotoRef != "" {
		listo, err := fotoCompleta(ctx, t, e.FotoRef, string(o.OrdenID))
		if err != nil {
			return false, err
		}
		if !listo {
			esperando = EsperandoFoto
		}
	}
	t.FijarEntidad("orden", string(o.OrdenID))
	if o.EmpresaID != nil {
		t.FijarEmpresa(*o.EmpresaID)
	}
	_, _, err = Encolar(ctx, t, Accion{
		EmpresaID: o.EmpresaID, Operacion: p.operacion, IdempotencyKey: p.clave, Entidad: "orden", EntidadID: string(o.OrdenID),
		Ruta: p.ruta, Cuerpo: cuerpo, Desde: m.ahora(), TecnicoUsuarioID: p.actor, Esperando: esperando,
	}, m.cfg.Encolar)
	if err != nil {
		return false, err
	}
	return true, nil
}

// fotoCompleta: la foto es un adjunto completo de la orden.
func fotoCompleta(ctx context.Context, q adjuntos.DBTX, foto, orden string) (bool, error) {
	id, err := uuid.Parse(foto)
	if err != nil {
		return false, nil
	}
	a, err := adjuntos.DeEntidad(ctx, q, id, entidadOrdenAdjunto, orden)
	if err != nil {
		var p *nucleo.Problema
		if errors.As(err, &p) || errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return a.Estado == adjuntos.EstadoCompleto, nil
}

// ArmarIntento revisa un pendiente que espera su foto: completo → a la cola con el mismo cuerpo;
// rechazada → foto_invalida; sin completar 72 h después de la fecha → foto_fuera_de_ventana. Si
// no, sigue esperando. Idempotente.
func (m *Modulo) ArmarIntento(ctx context.Context, id int64) (string, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return "", nucleo.ErrSinBD
	}
	return m.armarIntento.Ejecutar(ctx, entradaArmarIntento{PendienteID: id})
}

type entradaArmarIntento struct {
	PendienteID int64 `json:"pendiente_id"`
}

func (m *Modulo) nuevoArmarIntento() *auditoria.Caso[entradaArmarIntento, string] {
	return auditoria.Nuevo(auditoria.Caso[entradaArmarIntento, string]{
		Accion: "integraciones.armar_intento", Acceso: auditoria.Autenticado,
		AuditarExito: func(r string) bool { return r != "esperando" && r != "nada" },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaArmarIntento) (string, error) {
			f, err := pendientesbd.New(t).PendientePorID(ctx, in.PendienteID)
			if errors.Is(err, pgx.ErrNoRows) {
				return "nada", nil
			}
			if err != nil {
				return "", err
			}
			if (f.Operacion != OpCambio && f.Operacion != OpNoRecuperado) || f.Estado != EstadoBloqueado ||
				f.UltimoError == nil || *f.UltimoError != EsperandoFoto {
				return "nada", nil
			}
			t.FijarEntidad("pendiente", f.ID)
			if f.EmpresaID != nil {
				t.FijarEmpresa(*f.EmpresaID)
			}
			var cuerpo struct {
				FotoRef string     `json:"foto_ref"`
				Fecha   *time.Time `json:"fecha"`
			}
			_ = json.Unmarshal(f.Payload, &cuerpo)
			falla := func(codigo, detalle string) (string, error) {
				_, _, err := FallarEsperando(ctx, t, f.ID, f.Version, EsperandoFoto, codigo, detalle)
				return codigo, err
			}
			id, err := uuid.Parse(cuerpo.FotoRef)
			if err != nil {
				return falla(CodigoFotoInvalida, "el pendiente no tiene foto")
			}
			a, err := adjuntos.DeEntidad(ctx, t, id, entidadOrdenAdjunto, f.EntidadID)
			if err != nil {
				return falla(CodigoFotoInvalida, "la foto ya no es un adjunto de la orden")
			}
			switch a.Estado {
			case adjuntos.EstadoCompleto:
				_, ok, err := CompletarEsperando(ctx, t, f.ID, f.Version, EsperandoFoto, f.Payload, m.ahora(), m.cfg.Encolar)
				if err != nil || !ok {
					return "nada", err
				}
				return "lista", nil
			case adjuntos.EstadoRechazado:
				return falla(CodigoFotoInvalida, "la foto se rechazó al subir: revisa el intento con el técnico")
			}
			if cuerpo.Fecha != nil && m.ahora().Sub(*cuerpo.Fecha) > VentanaFoto {
				return falla(CodigoFotoFueraDeVentana, "la foto no terminó de subir en 72 h desde la fecha: el intento no sale; decide una persona")
			}
			return "esperando", nil
		},
	})
}

// BarrerFotos revisa los cambios e intentos que esperan su foto (red de seguridad del evento y la
// ventana de 72 h). Devuelve cuántos cambiaron.
func (m *Modulo) BarrerFotos(ctx context.Context) (int, error) {
	n := 0
	for _, op := range []string{OpNoRecuperado, OpCambio} {
		ps, err := Esperando(ctx, nucleo.BDDe(ctx), op, EsperandoFoto, limiteBarridoFotos)
		if err != nil {
			return n, err
		}
		for _, p := range ps {
			r, err := m.ArmarIntento(ctx, p.ID)
			if err != nil {
				return n, err
			}
			if r != "esperando" && r != "nada" {
				n++
			}
		}
	}
	return n, nil
}

// suscribirFotos: cuando una foto de una orden termina de subir (o se rechaza), se arman los
// cambios e intentos que la esperan.
func (m *Modulo) suscribirFotos(bus *integracion.Bus) {
	alAdjunto := func(ctx context.Context, e integracion.Evento) error {
		var a struct {
			AdjuntoID string `json:"adjunto_id"`
			Entidad   string `json:"entidad"`
		}
		if json.Unmarshal(e.Payload, &a) != nil || a.Entidad != entidadOrdenAdjunto || a.AdjuntoID == "" {
			return nil
		}
		bd := nucleo.BDDe(ctx)
		if bd == nil {
			return nil
		}
		ids, err := pendientesbd.New(bd).EsperandoFoto(ctx, pendientesbd.EsperandoFotoParams{Motivo: EsperandoFoto, FotoRef: a.AdjuntoID})
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := m.ArmarIntento(ctx, id); err != nil {
				slog.ErrorContext(ctx, "pendientes: no se pudo armar el intento al subir la foto", "pendiente", id, "err", err)
				return err
			}
		}
		return nil
	}
	bus.Suscribir("integracion.pendientes_foto_intento", adjuntos.EventoCompletado, alAdjunto)
	bus.Suscribir("integracion.pendientes_foto_intento_rechazada", adjuntos.EventoRechazado, alAdjunto)
}
