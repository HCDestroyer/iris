package soporte

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// Caídas repetidas y falla general desde Argos (contrato Iris ↔ ArgosISP 0.13.0 §3.11; pedidos B29 y
// B30). El conector (I1) valida el webhook firmado y publica argos.caidas_repetidas,
// argos.falla_general_abierta y argos.falla_general_cerrada; soporte decide con estos casos de uso
// (actor sistema). Idempotencia por soporte.red_eventos (una fila por entrante_id).
//
// Caídas (B29), por servicio:
//  1. bajo el umbral de la empresa (de fábrica 3 en 60 min): no hace nada;
//  2. el servicio está en una falla general abierta: nota en el ticket general;
//  3. hay un ticket de caídas abierto de ese servicio: nota en él;
//  4. hay una «posible falla general» abierta en la empresa: nota en ella;
//  5. la empresa ya tiene N tickets de caídas en 10 min (más de N con este; 20 de fábrica): no abre
//     otro: abre la «posible falla general», agrupa ahí los de esos 10 min y avisa a supervisión;
//  6. si no, abre el ticket de caídas del servicio.
//
// Falla general (B30), por incidente y empresa:
//   - abierta: un ticket general por incidente_id (idempotente). Si el mismo alcance_id se cerró hace
//     menos de 30 min, se reusa su ticket; si Horus ya abrió el maestro de ese router, ese es el ticket
//     general. Absorbe los tickets de conectividad abiertos de los servicios afectados creados después
//     de `desde` (y los que nacen mientras siga abierta), y avisa a sus clientes («hay una falla en tu
//     zona», sin servicio ni dirección).
//   - cerrada: el ticket general se resuelve y se cierra a los 30 min si no se reabrió; los hijos no
//     se cierran: pasan a «en espera» para verificar con el cliente, con un aviso de cierre por cliente
//     e incidente (y como máximo uno cada 6 h).

// Ventanas de §3.11.
const (
	VentanaPosibleFalla = 10 * time.Minute
	VentanaReusoFalla   = 30 * time.Minute
	VentanaAvisoCierre  = 6 * time.Hour
)

// Ajustes de fábrica (§10.9).
const (
	CaidasUmbralFabrica      = 3
	CaidasVentanaMinFabrica  = 60
	FallaUmbralFabrica       = 20
	VerificacionHorasFabrica = 24
)

// Categoría de los tickets de caídas.
const CategoriaCaidas = "caidas_repetidas"

// Resultados (soporte.red_eventos.resultado).
const (
	RedRepetido         = "repetido"
	RedBajoUmbral       = "bajo_umbral"
	RedNotaFalla        = "nota_falla_general"
	RedNotaTicket       = "nota_ticket"
	RedNotaPosible      = "nota_posible_falla"
	RedPosibleCreada    = "posible_falla_creada"
	RedTicketCreado     = "ticket_creado"
	RedFallaCreada      = "falla_creada"
	RedFallaReusada     = "falla_reusada"
	RedFallaHorus       = "falla_maestro_horus"
	RedFallaActualizada = "falla_actualizada"
	RedFallaCerrada     = "falla_cerrada"
	RedSinIncidente     = "sin_incidente"
	RedYaCerrada        = "ya_cerrada"
	RedEmpresaNoReg     = "empresa_no_registrada"
	RedCierreAntes      = "cierre_antes_de_apertura"
)

// Avisos a los hijos de una falla general (avisos_hijos.go).
const (
	AvisoFallaAbierta = "falla_abierta"
	AvisoFallaCerrada = "falla_cerrada"
)

// ResultadoRed dice qué hizo soporte con un evento de red de Argos.
type ResultadoRed struct {
	Resultado string `json:"resultado"`
	TicketID  *int64 `json:"ticketId"`
}

// EntradaCaidas es argos.caidas_repetidas.
type EntradaCaidas struct {
	EntranteID     int64      `json:"entrante_id"`
	ServicioID     int64      `json:"servicio_id"`
	EmpresaID      int64      `json:"empresa_id"`
	ClienteID      *int64     `json:"cliente_id"`
	RouterID       *int64     `json:"router_id"`
	Sesiones       int        `json:"sesiones"`
	VentanaMinutos int        `json:"ventana_minutos"`
	Desde          *time.Time `json:"desde"`
	Hasta          *time.Time `json:"hasta"`
}

// EntradaFalla es argos.falla_general_abierta / _cerrada.
type EntradaFalla struct {
	EntranteID  int64   `json:"entrante_id"`
	Evento      string  `json:"-"`
	IncidenteID string  `json:"incidente_id"`
	EmpresaID   int64   `json:"empresa_id"`
	Alcance     string  `json:"alcance"`
	AlcanceID   string  `json:"alcance_id"`
	Servicios   []int64 `json:"servicios"`
	Descartados int     `json:"descartados"`
	// PorVerificar: ids que Iris no conocía; el conector los lee aparte y llegan por
	// argos.falla_general_servicios. Paginando: hay más afectados por leer de Argos.
	PorVerificar int        `json:"por_verificar"`
	Paginando    bool       `json:"paginando"`
	Desde        *time.Time `json:"desde"`
	Hasta        *time.Time `json:"hasta"`
}

func auditarRed(r ResultadoRed) bool {
	return r.Resultado != RedRepetido && r.Resultado != RedBajoUmbral
}

func reclamarRed(ctx context.Context, q *soportebd.Queries, entrante int64, evento string, empresa int64) (bool, ResultadoRed, error) {
	n, err := q.ReclamarEventoRed(ctx, soportebd.ReclamarEventoRedParams{EntranteID: entrante, Evento: evento, EmpresaID: &empresa})
	if err != nil || n == 1 {
		return n == 1, ResultadoRed{}, err
	}
	previo, err := q.EventoRed(ctx, entrante)
	if err != nil {
		return false, ResultadoRed{}, err
	}
	return false, ResultadoRed{Resultado: RedRepetido, TicketID: previo.TicketID}, nil
}

func cerrarRed(ctx context.Context, q *soportebd.Queries, entrante int64, r ResultadoRed, detalle *string) (ResultadoRed, error) {
	return r, q.CerrarEventoRed(ctx, soportebd.CerrarEventoRedParams{EntranteID: entrante, Resultado: r.Resultado, TicketID: r.TicketID, Detalle: detalle})
}

// AjustesRed son los umbrales de la empresa (de fábrica si no los cambió).
type AjustesRed struct {
	CaidasUmbral      int32 `json:"caidasUmbral" minimum:"1" maximum:"100" doc:"Caídas que abren un ticket (de fábrica 3)"`
	CaidasVentanaMin  int32 `json:"caidasVentanaMin" minimum:"5" maximum:"1440" doc:"En cuántos minutos (de fábrica 60)"`
	FallaUmbral       int32 `json:"fallaUmbral" minimum:"2" maximum:"1000" doc:"Tickets de caídas en 10 min a partir de los cuales Iris deja de abrirlos y avisa una posible falla general (de fábrica 20)"`
	VerificacionHoras int32 `json:"verificacionHoras" minimum:"1" maximum:"168" doc:"Horas que un ticket espera la respuesta del cliente tras cerrarse una falla general antes de volver a abierto (de fábrica 24)"`
	Version           int32 `json:"version" doc:"0 = de fábrica"`
}

func ajustesRed(ctx context.Context, q *soportebd.Queries, empresa int64) (AjustesRed, error) {
	a, err := q.AjustesRed(ctx, empresa)
	if errors.Is(err, pgx.ErrNoRows) {
		return AjustesRed{CaidasUmbral: CaidasUmbralFabrica, CaidasVentanaMin: CaidasVentanaMinFabrica, FallaUmbral: FallaUmbralFabrica,
			VerificacionHoras: VerificacionHorasFabrica}, nil
	}
	if err != nil {
		return AjustesRed{}, err
	}
	return AjustesRed{CaidasUmbral: a.CaidasUmbral, CaidasVentanaMin: a.CaidasVentanaMin, FallaUmbral: a.FallaUmbral,
		VerificacionHoras: a.VerificacionHoras, Version: a.Version}, nil
}

// empresaRed: la empresa del evento debe estar registrada en Iris.
func empresaRed(ctx context.Context, q *soportebd.Queries, empresa int64) (bool, error) {
	if empresa <= 0 {
		return false, nil
	}
	return q.EmpresaRegistrada(ctx, empresa)
}

func (m *Modulo) categoria(ctx context.Context, q *soportebd.Queries, codigo string, empresa int64) (*int64, error) {
	c, err := q.CategoriaPorCodigo(ctx, soportebd.CategoriaPorCodigoParams{Codigo: codigo, EmpresaID: &empresa})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c.ID, nil
}

// Caídas repetidas (B29) ---------------------------------------------------------------------------

func (m *Modulo) nuevoCaidasArgos() *auditoria.Caso[EntradaCaidas, ResultadoRed] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCaidas, ResultadoRed]{
		Accion: "tickets.argos_caidas", Acceso: auditoria.Autenticado, AuditarExito: auditarRed,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCaidas) (ResultadoRed, error) {
			if err := esSistema(ctx); err != nil {
				return ResultadoRed{}, err
			}
			if in.EntranteID <= 0 || in.ServicioID <= 0 || in.EmpresaID <= 0 {
				return ResultadoRed{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "servicio_id", Message: "Evento de caídas inválido", Rule: "argos"})
			}
			q := soportebd.New(t)
			t.FijarEntidad("argos_entrante", in.EntranteID)
			nuevo, previo, err := reclamarRed(ctx, q, in.EntranteID, argos.EventoInternoCaidas, in.EmpresaID)
			if err != nil || !nuevo {
				return previo, err
			}
			if ok, err := empresaRed(ctx, q, in.EmpresaID); err != nil || !ok {
				if err != nil {
					return ResultadoRed{}, err
				}
				return cerrarRed(ctx, q, in.EntranteID, ResultadoRed{Resultado: RedEmpresaNoReg}, nil)
			}
			t.FijarEmpresa(in.EmpresaID)
			aj, err := ajustesRed(ctx, q, in.EmpresaID)
			if err != nil {
				return ResultadoRed{}, err
			}
			// Iris decide con su umbral: Argos cuenta con el suyo (§3.11).
			if in.Sesiones < int(aj.CaidasUmbral) || in.VentanaMinutos > int(aj.CaidasVentanaMin) {
				return cerrarRed(ctx, q, in.EntranteID, ResultadoRed{Resultado: RedBajoUmbral}, nil)
			}
			empresa := in.EmpresaID
			// Candados: la empresa (posible falla y conteo) y el servicio, siempre en ese orden.
			if err := q.BloquearIncidente(ctx, fmt.Sprintf("soporte.red:empresa:%d", empresa)); err != nil {
				return ResultadoRed{}, err
			}
			nota := fmt.Sprintf("Argos: %d caídas del servicio #%d en %d min%s.", in.Sesiones, in.ServicioID, in.VentanaMinutos, rangoRed(in.Desde, in.Hasta))
			r, err := m.aplicarCaidas(ctx, t, q, in, aj, nota)
			if err != nil {
				return ResultadoRed{}, err
			}
			if r.TicketID != nil {
				t.FijarEntidad("ticket", *r.TicketID)
			}
			t.Anotar(map[string]any{"entrante_id": in.EntranteID, "resultado": r.Resultado})
			return cerrarRed(ctx, q, in.EntranteID, r, nil)
		},
	})
}

func rangoRed(desde, hasta *time.Time) string {
	if desde == nil || hasta == nil {
		return ""
	}
	return " (" + desde.UTC().Format(time.RFC3339) + " a " + hasta.UTC().Format(time.RFC3339) + ")"
}

func (m *Modulo) aplicarCaidas(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, in EntradaCaidas, aj AjustesRed, nota string) (ResultadoRed, error) {
	empresa := in.EmpresaID
	// 2. Falla general abierta con el servicio: nota en el ticket general.
	inc, err := q.IncidenteAbiertoDeServicio(ctx, soportebd.IncidenteAbiertoDeServicioParams{EmpresaID: empresa, ServicioID: in.ServicioID})
	switch {
	case err == nil:
		g, err := q.TicketPorID(ctx, inc.TicketID)
		if err != nil {
			return ResultadoRed{}, err
		}
		if _, err := mensajeSistema(ctx, q, g, false, nota+" El servicio está en la falla general "+inc.IncidenteID+"."); err != nil {
			return ResultadoRed{}, err
		}
		return ResultadoRed{Resultado: RedNotaFalla, TicketID: &g.ID}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return ResultadoRed{}, err
	}
	cat, err := m.categoria(ctx, q, CategoriaCaidas, empresa)
	if err != nil {
		return ResultadoRed{}, err
	}
	if cat == nil {
		return ResultadoRed{}, nucleo.ReglaNegocio("categoria_caidas", "No existe la categoría de caídas repetidas")
	}
	// 3. Un ticket de caídas abierto de ese servicio: nota.
	abierto, err := q.TicketCaidasAbierto(ctx, soportebd.TicketCaidasAbiertoParams{EmpresaID: &empresa, ServicioID: &in.ServicioID, CategoriaID: cat})
	switch {
	case err == nil:
		if _, err := mensajeSistema(ctx, q, abierto, false, nota); err != nil {
			return ResultadoRed{}, err
		}
		return ResultadoRed{Resultado: RedNotaTicket, TicketID: &abierto.ID}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return ResultadoRed{}, err
	}
	// 4. Posible falla general abierta que todavía agrupa (sin padre, o dentro de su ventana): nota en
	// ella (no se abren más). Una que ya confirmó una falla general no se traga las caídas nuevas.
	desde := m.ahora().Add(-VentanaPosibleFalla)
	posible, err := q.PosibleFallaVigente(ctx, soportebd.PosibleFallaVigenteParams{EmpresaID: &empresa, Desde: desde})
	switch {
	case err == nil:
		if _, err := mensajeSistema(ctx, q, posible, false, nota); err != nil {
			return ResultadoRed{}, err
		}
		return ResultadoRed{Resultado: RedNotaPosible, TicketID: &posible.ID}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return ResultadoRed{}, err
	}
	// 5. Más de N tickets de caídas en 10 min: posible falla general (salvo que la empresa ya tenga una
	// abierta, confirmada por una falla general: solo puede haber una).
	n, err := q.ContarCaidasRecientes(ctx, soportebd.ContarCaidasRecientesParams{EmpresaID: &empresa, CategoriaID: cat, Desde: desde})
	if err != nil {
		return ResultadoRed{}, err
	}
	if n >= aj.FallaUmbral {
		_, err := q.PosibleFallaAbierta(ctx, &empresa)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return m.abrirPosibleFalla(ctx, t, q, empresa, *cat, desde, n, nota)
		case err != nil:
			return ResultadoRed{}, err
		}
	}
	// 6. Ticket de caídas del servicio (se vincula a un maestro de Horus o a una falla general como cualquiera).
	f, err := m.crearTicket(ctx, t, EntradaCrearTicket{
		EmpresaID: &empresa, ClienteID: in.ClienteID, ServicioID: &in.ServicioID, CategoriaID: cat, Prioridad: "media", Origen: "argos",
		Asunto: fmt.Sprintf("Caídas repetidas del servicio #%d", in.ServicioID),
	}, opcionesCrear{nota: nota})
	if err != nil {
		return ResultadoRed{}, err
	}
	return ResultadoRed{Resultado: RedTicketCreado, TicketID: &f.ID}, nil
}

func (m *Modulo) abrirPosibleFalla(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, empresa, cat int64, desde time.Time, n int32, nota string) (ResultadoRed, error) {
	inc, err := m.categoria(ctx, q, "incidente_red", empresa)
	if err != nil {
		return ResultadoRed{}, err
	}
	maestro := MaestroPosibleFalla
	texto := fmt.Sprintf("Iris dejó de abrir tickets de caídas: %d en los últimos 10 min. Se agrupan aquí. %s", n, nota)
	g, err := m.crearTicket(ctx, t, EntradaCrearTicket{
		EmpresaID: &empresa, CategoriaID: inc, Prioridad: "alta", Origen: "argos", Asunto: "Posible falla general (caídas repetidas)",
	}, opcionesCrear{incidente: &datosIncidente{maestro: &maestro}, nota: texto})
	if err != nil {
		return ResultadoRed{}, err
	}
	recientes, err := q.CaidasRecientesSinPadre(ctx, soportebd.CaidasRecientesSinPadreParams{EmpresaID: &empresa, CategoriaID: &cat, Desde: desde})
	if err != nil {
		return ResultadoRed{}, err
	}
	for _, h := range recientes {
		if err := vincularHijo(ctx, q, h, g, "Agrupado en la posible falla general "+g.Numero+"."); err != nil {
			return ResultadoRed{}, err
		}
	}
	if err := t.Emitir("soporte.posible_falla_general", map[string]any{"ticket_id": g.ID, "empresa_id": empresa, "tickets": n, "agrupados": len(recientes)}); err != nil {
		return ResultadoRed{}, err
	}
	return ResultadoRed{Resultado: RedPosibleCreada, TicketID: &g.ID}, nil
}

// Maestros de Argos.
const (
	MaestroFallaGeneral = "falla_general"
	MaestroPosibleFalla = "posible_falla"
)

func vincularHijo(ctx context.Context, q *soportebd.Queries, h, padre soportebd.SoporteTicket, nota string) error {
	if _, err := q.FijarPadreTicket(ctx, soportebd.FijarPadreTicketParams{ID: h.ID, PadreID: &padre.ID}); err != nil {
		return err
	}
	_, err := mensajeSistema(ctx, q, h, false, nota)
	return err
}

// Falla general (B30) --------------------------------------------------------------------------

func (m *Modulo) nuevoFallaArgos() *auditoria.Caso[EntradaFalla, ResultadoRed] {
	return auditoria.Nuevo(auditoria.Caso[EntradaFalla, ResultadoRed]{
		Accion: "tickets.argos_falla_general", Acceso: auditoria.Autenticado, AuditarExito: auditarRed,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaFalla) (ResultadoRed, error) {
			if err := esSistema(ctx); err != nil {
				return ResultadoRed{}, err
			}
			if in.EntranteID <= 0 || in.IncidenteID == "" || in.EmpresaID <= 0 || in.AlcanceID == "" || in.Desde == nil {
				return ResultadoRed{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "incidente_id", Message: "Falla general inválida", Rule: "argos"})
			}
			q := soportebd.New(t)
			t.FijarEntidad("argos_entrante", in.EntranteID)
			nuevo, previo, err := reclamarRed(ctx, q, in.EntranteID, in.Evento, in.EmpresaID)
			if err != nil || !nuevo {
				return previo, err
			}
			if ok, err := empresaRed(ctx, q, in.EmpresaID); err != nil || !ok {
				if err != nil {
					return ResultadoRed{}, err
				}
				return cerrarRed(ctx, q, in.EntranteID, ResultadoRed{Resultado: RedEmpresaNoReg}, nil)
			}
			t.FijarEmpresa(in.EmpresaID)
			// Candados: la empresa (como las caídas) y el alcance (reuso dentro de 30 min).
			if err := q.BloquearIncidente(ctx, fmt.Sprintf("soporte.red:empresa:%d", in.EmpresaID)); err != nil {
				return ResultadoRed{}, err
			}
			var r ResultadoRed
			if in.Evento == argos.EventoInternoFallaCerrada {
				r, err = m.cerrarFalla(ctx, t, q, in)
			} else {
				r, err = m.abrirFalla(ctx, t, q, in)
			}
			if err != nil {
				return ResultadoRed{}, err
			}
			if r.TicketID != nil {
				t.FijarEntidad("ticket", *r.TicketID)
			}
			t.Anotar(map[string]any{"entrante_id": in.EntranteID, "incidente_id": in.IncidenteID, "resultado": r.Resultado,
				"servicios": len(in.Servicios), "descartados": in.Descartados})
			return cerrarRed(ctx, q, in.EntranteID, r, nil)
		},
	})
}

func (in EntradaFalla) que() string {
	return fmt.Sprintf("%s %s", in.Alcance, in.AlcanceID)
}

func (m *Modulo) abrirFalla(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, in EntradaFalla) (ResultadoRed, error) {
	ahora := m.ahora()
	empresa := in.EmpresaID
	inc, err := q.IncidenteRed(ctx, soportebd.IncidenteRedParams{IncidenteID: in.IncidenteID, EmpresaID: empresa})
	switch {
	case err == nil:
		// El mismo incidente otra vez (Argos lo reenvió con más servicios): se suman.
		if inc.Estado == "cerrada" {
			return ResultadoRed{Resultado: RedYaCerrada, TicketID: &inc.TicketID}, nil
		}
		g, err := q.TicketPorID(ctx, inc.TicketID)
		if err != nil {
			return ResultadoRed{}, err
		}
		if _, err := m.sumarAfectados(ctx, t, q, inc, g, in.Servicios); err != nil {
			return ResultadoRed{}, err
		}
		return ResultadoRed{Resultado: RedFallaActualizada, TicketID: &g.ID}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return ResultadoRed{}, err
	}
	// Argos reintenta y puede desordenar: un cierre que llegó antes, o una apertura que ya trae
	// hasta, no abren nada.
	cerrada, err := q.HayCierreAnticipado(ctx, soportebd.HayCierreAnticipadoParams{IncidenteID: in.IncidenteID, EmpresaID: empresa})
	if err != nil {
		return ResultadoRed{}, err
	}
	if cerrada || in.Hasta != nil {
		if err := q.RegistrarCierreAnticipado(ctx, soportebd.RegistrarCierreAnticipadoParams{IncidenteID: in.IncidenteID, EmpresaID: empresa, Hasta: in.Hasta}); err != nil {
			return ResultadoRed{}, err
		}
		return ResultadoRed{Resultado: RedYaCerrada}, nil
	}
	nota := fmt.Sprintf("Argos: falla general %s en %s desde %s. Servicios afectados: %d.", in.IncidenteID, in.que(),
		in.Desde.UTC().Format(time.RFC3339), len(in.Servicios))
	if in.Descartados > 0 {
		nota += fmt.Sprintf(" Se descartaron %d ids que no son de esta empresa.", in.Descartados)
	}
	if in.PorVerificar > 0 || in.Paginando {
		nota += fmt.Sprintf(" Quedan %d ids por verificar en Argos (y más por leer: %t); se suman al verificarse.", in.PorVerificar, in.Paginando)
	}
	resultado := RedFallaCreada
	var general soportebd.SoporteTicket
	var reusa *int64
	previo, err := q.IncidenteReusable(ctx, soportebd.IncidenteReusableParams{EmpresaID: empresa, Alcance: in.Alcance, AlcanceID: in.AlcanceID,
		Desde: ptr(ahora.Add(-VentanaReusoFalla))})
	reusable := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ResultadoRed{}, err
	}
	if reusable {
		// El mismo alcance reabre antes de 30 min: se reusa el ticket general, salvo que ya se haya
		// cerrado (el job de los 30 min ganó la carrera): entonces uno nuevo.
		g, err := q.TicketPorIDParaActualizar(ctx, previo.TicketID)
		if err != nil {
			return ResultadoRed{}, err
		}
		if g.Estado == EstadoCerrado {
			reusable = false
		} else {
			if general, err = m.reabrirGeneral(ctx, t, q, g, nota+" Se reusa el ticket de la falla "+previo.IncidenteID+" (cerrada hace menos de 30 min)."); err != nil {
				return ResultadoRed{}, err
			}
			reusa, resultado = &previo.ID, RedFallaReusada
		}
	}
	if !reusable {
		var maestro soportebd.SoporteTicket
		hayHorus := false
		if in.Alcance == "router" {
			if router, err := strconv.ParseInt(in.AlcanceID, 10, 64); err == nil {
				maestro, err = q.MaestroRouterAbierto(ctx, soportebd.MaestroRouterAbiertoParams{EmpresaID: &empresa, RouterID: &router})
				switch {
				case err == nil:
					hayHorus = true
				case !errors.Is(err, pgx.ErrNoRows):
					return ResultadoRed{}, err
				}
			}
		}
		if hayHorus {
			// Horus ya avisó la caída de ese router: su maestro es el ticket general.
			if _, err := mensajeSistema(ctx, q, maestro, false, nota+" Se vincula a este incidente de Horus."); err != nil {
				return ResultadoRed{}, err
			}
			general, resultado = maestro, RedFallaHorus
		} else {
			cat, err := m.categoria(ctx, q, "incidente_red", empresa)
			if err != nil {
				return ResultadoRed{}, err
			}
			tipo := MaestroFallaGeneral
			nombre := recortarRunas(in.IncidenteID, 200)
			if general, err = m.crearTicket(ctx, t, EntradaCrearTicket{
				EmpresaID: &empresa, CategoriaID: cat, Prioridad: "alta", Origen: "argos",
				Asunto: recortarRunas("Falla general en "+in.que(), 200),
			}, opcionesCrear{incidente: &datosIncidente{maestro: &tipo, nombre: &nombre}, nota: nota}); err != nil {
				return ResultadoRed{}, err
			}
		}
	}
	inc, err = q.CrearIncidenteRed(ctx, soportebd.CrearIncidenteRedParams{IncidenteID: in.IncidenteID, EmpresaID: empresa, Alcance: in.Alcance,
		AlcanceID: in.AlcanceID, TicketID: general.ID, Desde: in.Desde.UTC(), Hasta: in.Hasta, ReusaDe: reusa})
	if err != nil {
		return ResultadoRed{}, err
	}
	if reusa != nil {
		// Los hijos abiertos del incidente anterior pasan al nuevo: la falla volvió.
		if err := m.retomarHijos(ctx, t, q, *reusa, inc, general); err != nil {
			return ResultadoRed{}, err
		}
	}
	t.RegistrarCambios("incidente_red", inc.ID, nil, map[string]any{"incidente_id": inc.IncidenteID, "ticket_id": inc.TicketID, "alcance": inc.Alcance,
		"alcance_id": inc.AlcanceID})
	// La posible falla general de la empresa pasa a ser hija del ticket general.
	posible, err := q.PosibleFallaAbierta(ctx, &empresa)
	switch {
	case err == nil:
		if posible.ID != general.ID {
			if err := vincularHijo(ctx, q, posible, general, "Confirmada como falla general "+in.IncidenteID+" ("+general.Numero+")."); err != nil {
				return ResultadoRed{}, err
			}
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return ResultadoRed{}, err
	}
	if _, err := m.sumarAfectados(ctx, t, q, inc, general, in.Servicios); err != nil {
		return ResultadoRed{}, err
	}
	return ResultadoRed{Resultado: resultado, TicketID: &general.ID}, nil
}

// sumarAfectados guarda los servicios, absorbe los tickets de conectividad de esos servicios creados
// después de desde y encola el aviso de falla a sus clientes.
func (m *Modulo) sumarAfectados(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, inc soportebd.SoporteIncidentesRed, general soportebd.SoporteTicket, servicios []int64) (int, error) {
	if len(servicios) > 0 {
		if _, err := q.AgregarServiciosIncidente(ctx, soportebd.AgregarServiciosIncidenteParams{Incidente: inc.ID, Servicios: servicios}); err != nil {
			return 0, err
		}
	}
	hijos, err := q.TicketsParaAbsorber(ctx, soportebd.TicketsParaAbsorberParams{EmpresaID: &inc.EmpresaID, GeneralID: general.ID, Desde: inc.Desde,
		Incidente: inc.ID})
	if err != nil {
		return 0, err
	}
	for _, h := range hijos {
		if err := vincularHijo(ctx, q, h, general, "Vinculado a la falla general "+inc.IncidenteID+" ("+general.Numero+"): el servicio está afectado según Argos."); err != nil {
			return 0, err
		}
		if err := q.RegistrarHijoIncidente(ctx, soportebd.RegistrarHijoIncidenteParams{Incidente: inc.ID, TicketID: h.ID}); err != nil {
			return 0, err
		}
	}
	t.Anotar(map[string]any{"absorbidos": len(hijos)})
	return len(hijos), m.encolarAvisoFalla(ctx, t, general, inc, AvisoFallaAbierta)
}

// retomarHijos: al reusar el ticket general, todos los hijos abiertos del incidente anterior pasan al
// incidente nuevo (así reciben su apertura y su cierre); los que esperaban verificar vuelven a abierto
// (su espera empieza de nuevo cuando se cierre el incidente nuevo).
func (m *Modulo) retomarHijos(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, anterior int64, inc soportebd.SoporteIncidentesRed,
	general soportebd.SoporteTicket) error {
	hijos, err := q.HijosAbiertosDeIncidente(ctx, soportebd.HijosAbiertosDeIncidenteParams{Incidente: anterior, GeneralID: general.ID})
	if err != nil {
		return err
	}
	ahora := m.ahora()
	for _, h := range hijos {
		if h.Estado != EstadoEnEspera {
			if err := q.RegistrarHijoIncidente(ctx, soportebd.RegistrarHijoIncidenteParams{Incidente: inc.ID, TicketID: h.ID}); err != nil {
				return err
			}
			continue
		}
		cal, err := cargarCalendario(ctx, q, h.CalendarioID)
		if err != nil {
			return err
		}
		despues := h
		salirDeEspera(&despues, cal, ahora)
		despues.Estado = EstadoAbierto
		f, err := guardar(ctx, t, q, h, despues, cal)
		if err != nil {
			return err
		}
		if _, err := mensajeSistema(ctx, q, f, false, "La falla general volvió ("+inc.IncidenteID+"): se deja de esperar la verificación del cliente."); err != nil {
			return err
		}
		if err := cambioDeEstado(ctx, t, q, h, f, ptr("Argos: la falla general volvió")); err != nil {
			return err
		}
		if err := q.RegistrarHijoIncidente(ctx, soportebd.RegistrarHijoIncidenteParams{Incidente: inc.ID, TicketID: f.ID}); err != nil {
			return err
		}
	}
	t.Anotar(map[string]any{"hijos_retomados": len(hijos)})
	return nil
}

// reabrirGeneral reabre (o deja abierto) el ticket general de un incidente que vuelve antes de 30 min.
func (m *Modulo) reabrirGeneral(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, g soportebd.SoporteTicket, nota string) (soportebd.SoporteTicket, error) {
	if g.Estado == EstadoResuelto {
		cal, err := cargarCalendario(ctx, q, g.CalendarioID)
		if err != nil {
			return g, err
		}
		despues := g
		acumularPausa(&despues, cal, g.ResueltoEn, m.ahora())
		despues.Estado, despues.ResueltoEn = EstadoAbierto, nil
		f, err := guardar(ctx, t, q, g, despues, cal)
		if err != nil {
			return g, err
		}
		if err := cambioDeEstado(ctx, t, q, g, f, ptr("Argos: la falla general volvió")); err != nil {
			return g, err
		}
		g = f
	}
	_, err := mensajeSistema(ctx, q, g, false, nota)
	return g, err
}

func (m *Modulo) cerrarFalla(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, in EntradaFalla) (ResultadoRed, error) {
	ahora := m.ahora()
	inc, err := q.IncidenteRed(ctx, soportebd.IncidenteRedParams{IncidenteID: in.IncidenteID, EmpresaID: in.EmpresaID})
	if errors.Is(err, pgx.ErrNoRows) {
		// El cierre llegó antes que la apertura: queda la marca y la apertura atrasada no abre nada.
		if err := q.RegistrarCierreAnticipado(ctx, soportebd.RegistrarCierreAnticipadoParams{IncidenteID: in.IncidenteID, EmpresaID: in.EmpresaID,
			Hasta: in.Hasta}); err != nil {
			return ResultadoRed{}, err
		}
		return ResultadoRed{Resultado: RedCierreAntes}, nil
	}
	if err != nil {
		return ResultadoRed{}, err
	}
	if inc.Estado == "cerrada" {
		return ResultadoRed{Resultado: RedYaCerrada, TicketID: &inc.TicketID}, nil
	}
	if inc, err = q.CerrarIncidenteRed(ctx, soportebd.CerrarIncidenteRedParams{ID: inc.ID, Hasta: in.Hasta, Ahora: &ahora}); err != nil {
		return ResultadoRed{}, err
	}
	t.RegistrarCambios("incidente_red", inc.ID, map[string]any{"estado": "abierta"}, map[string]any{"estado": "cerrada", "hasta": inc.Hasta})
	g, err := q.TicketPorIDParaActualizar(ctx, inc.TicketID)
	if err != nil {
		return ResultadoRed{}, err
	}
	nota := "Argos: la falla general " + inc.IncidenteID + " se cerró."
	// La posible falla general que confirmó este incidente se resuelve: deja de agrupar caídas.
	if err := m.resolverPosiblesFalla(ctx, t, q, g, nota, ahora); err != nil {
		return ResultadoRed{}, err
	}
	if g.Maestro == nil || *g.Maestro != MaestroFallaGeneral {
		// El general es el maestro de Horus: sus hijos esperan a que Horus confirme (su «arriba»).
		_, err := mensajeSistema(ctx, q, g, false, nota+" Los tickets vinculados siguen igual hasta que Horus confirme que el router volvió.")
		return ResultadoRed{Resultado: RedFallaCerrada, TicketID: &g.ID}, err
	}
	aj, err := ajustesRed(ctx, q, in.EmpresaID)
	if err != nil {
		return ResultadoRed{}, err
	}
	// Solo los hijos que absorbió este incidente; no se cierran solos.
	hijos, err := q.HijosDeIncidente(ctx, soportebd.HijosDeIncidenteParams{Incidente: inc.ID, GeneralID: g.ID})
	if err != nil {
		return ResultadoRed{}, err
	}
	for _, h := range hijos {
		if err := m.verificarHijo(ctx, t, q, h, inc, aj, ahora); err != nil {
			return ResultadoRed{}, err
		}
	}
	if abierto(g.Estado) {
		cal, err := cargarCalendario(ctx, q, g.CalendarioID)
		if err != nil {
			return ResultadoRed{}, err
		}
		despues := g
		salirDeEspera(&despues, cal, ahora)
		despues.Estado, despues.ResueltoEn = EstadoResuelto, &ahora
		f, err := guardar(ctx, t, q, g, despues, cal)
		if err != nil {
			return ResultadoRed{}, err
		}
		if _, err := mensajeSistema(ctx, q, f, false, nota+" Se resuelve y se cierra en 30 min si no vuelve."); err != nil {
			return ResultadoRed{}, err
		}
		if err := cambioDeEstado(ctx, t, q, g, f, ptr("Argos: falla general cerrada")); err != nil {
			return ResultadoRed{}, err
		}
		if err := m.encolarCerrarGeneral(ctx, t, f, inc.ID); err != nil {
			return ResultadoRed{}, err
		}
		g = f
	} else if _, err := mensajeSistema(ctx, q, g, false, nota); err != nil {
		return ResultadoRed{}, err
	}
	if err := m.encolarAvisoFalla(ctx, t, g, inc, AvisoFallaCerrada); err != nil {
		return ResultadoRed{}, err
	}
	return ResultadoRed{Resultado: RedFallaCerrada, TicketID: &g.ID}, nil
}

// resolverPosiblesFalla resuelve las posibles fallas generales abiertas que cuelgan del ticket general.
func (m *Modulo) resolverPosiblesFalla(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, g soportebd.SoporteTicket, nota string, ahora time.Time) error {
	posibles, err := q.PosiblesFallaHijasDe(ctx, g.ID)
	if err != nil {
		return err
	}
	for _, p := range posibles {
		cal, err := cargarCalendario(ctx, q, p.CalendarioID)
		if err != nil {
			return err
		}
		despues := p
		salirDeEspera(&despues, cal, ahora)
		despues.Estado, despues.ResueltoEn = EstadoResuelto, &ahora
		f, err := guardar(ctx, t, q, p, despues, cal)
		if err != nil {
			return err
		}
		if _, err := mensajeSistema(ctx, q, f, false, nota+" La posible falla general se resuelve con ella ("+g.Numero+")."); err != nil {
			return err
		}
		if err := cambioDeEstado(ctx, t, q, p, f, ptr("Argos: falla general cerrada")); err != nil {
			return err
		}
	}
	return nil
}

// encolarCerrarGeneral programa el cierre del ticket general a los 30 min de resuelto (uno por resolución).
func (m *Modulo) encolarCerrarGeneral(ctx context.Context, t *auditoria.Tx, g soportebd.SoporteTicket, incidente int64) error {
	if m.cfg.Encolar == nil || g.ResueltoEn == nil {
		return nil
	}
	return m.cfg.Encolar(ctx, t.Tx, ArgsCerrarFallaGeneral{TicketID: g.ID, Incidente: incidente, ResueltoEn: g.ResueltoEn.UTC()})
}

// reprogramarCierreGeneral: una persona resolvió el ticket general (lo había reabierto); si ninguna de
// sus fallas sigue abierta, se vuelve a programar su cierre a los 30 min.
func (m *Modulo) reprogramarCierreGeneral(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, g soportebd.SoporteTicket) error {
	incs, err := q.IncidentesDeTicket(ctx, g.ID)
	if err != nil || len(incs) == 0 {
		return err
	}
	for _, i := range incs {
		if i.Estado == "abierta" {
			return nil
		}
	}
	return m.encolarCerrarGeneral(ctx, t, g, incs[0].ID)
}

// verificarHijo: al cerrarse la falla, un hijo cuyo router Horus ya confirmó arriba se resuelve; uno
// abierto pasa a «en espera» para verificar con el cliente (vuelve a abierto si no responde en las
// horas de la empresa); uno nuevo se queda igual, con una nota.
func (m *Modulo) verificarHijo(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, h soportebd.SoporteTicket, inc soportebd.SoporteIncidentesRed,
	aj AjustesRed, ahora time.Time) error {
	if h.ServicioID != nil {
		arriba, err := q.HorusArribaDeServicio(ctx, soportebd.HorusArribaDeServicioParams{ServicioID: *h.ServicioID, Desde: inc.Desde})
		if err != nil {
			return err
		}
		if arriba {
			return m.resolverHijoConfirmado(ctx, t, q, h, ahora)
		}
	}
	const texto = "La falla general se cerró: verificar con el cliente que el servicio funcione."
	if h.Estado != EstadoAbierto {
		_, err := mensajeSistema(ctx, q, h, false, texto)
		return err
	}
	cal, err := cargarCalendario(ctx, q, h.CalendarioID)
	if err != nil {
		return err
	}
	despues := h
	despues.Estado, despues.EnEsperaDesde = EstadoEnEspera, &ahora
	f, err := guardar(ctx, t, q, h, despues, cal)
	if err != nil {
		return err
	}
	if _, err := mensajeSistema(ctx, q, f, false, texto); err != nil {
		return err
	}
	if err := cambioDeEstado(ctx, t, q, h, f, ptr("Verificar con el cliente: la falla general se cerró")); err != nil {
		return err
	}
	if m.cfg.Encolar == nil {
		return nil
	}
	return m.cfg.Encolar(ctx, t.Tx, ArgsVencerVerificacion{TicketID: f.ID, Desde: ahora, Vence: ahora.Add(time.Duration(aj.VerificacionHoras) * time.Hour)})
}

// resolverHijoConfirmado resuelve un hijo cuyo servicio volvió según Horus (su router arriba), con un
// mensaje al cliente para que confirme.
func (m *Modulo) resolverHijoConfirmado(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, h soportebd.SoporteTicket, ahora time.Time) error {
	cal, err := cargarCalendario(ctx, q, h.CalendarioID)
	if err != nil {
		return err
	}
	despues := h
	salirDeEspera(&despues, cal, ahora)
	despues.Estado, despues.ResueltoEn = EstadoResuelto, &ahora
	f, err := guardar(ctx, t, q, h, despues, cal)
	if err != nil {
		return err
	}
	msj, err := mensajeSistema(ctx, q, f, true, "Tu servicio volvió a funcionar según el monitoreo de la red. Si todavía tienes problemas, responde a este mensaje.")
	if err != nil {
		return err
	}
	if err := cambioDeEstado(ctx, t, q, h, f, ptr("Horus confirmó que el servicio volvió")); err != nil {
		return err
	}
	if err := m.generarEncuestaTicket(ctx, t, q, f); err != nil {
		return err
	}
	return t.Emitir("ticket.respondido", map[string]any{"ticket_id": f.ID, "mensaje_id": msj.ID})
}

// TandaConfirmados: hijos por consulta al resolver los que confirmó Horus.
const TandaConfirmados = 200

// resolverConfirmadosPorHorus: Horus avisó (en ocurrido) que un router volvió; se resuelven solo los
// hijos que esperan verificar con el cliente tras el cierre de su falla general: siguen colgando del
// ticket general de ese incidente, están en espera desde el cierre (nadie los movió ni volvieron a
// abierto), el cierre fue hace menos de las horas de verificación de la empresa y la falla empezó antes
// del aviso de Horus. Router de una empresa: solo esa; compartido (empresa nula): las empresas de los
// servicios del router. Por tandas, en la transacción del caso de Horus.
func (m *Modulo) resolverConfirmadosPorHorus(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, empresa *int64, router int64, ocurrido time.Time) (int, error) {
	ahora := m.ahora()
	resueltos := 0
	despues := int64(0)
	porEmpresa := map[int64]int{}
	for {
		hijos, err := q.HijosVerificandoDeRouter(ctx, soportebd.HijosVerificandoDeRouterParams{EmpresaID: empresa, RouterID: &router, Despues: despues,
			Ahora: ahora, Ocurrido: ocurrido.UTC(), Limite: TandaConfirmados})
		if err != nil {
			return resueltos, err
		}
		for _, h := range hijos {
			despues = h.ID
			f, err := q.TicketPorIDParaActualizar(ctx, h.ID)
			if err != nil {
				return resueltos, err
			}
			if f.Estado != EstadoEnEspera || f.TicketPadreID == nil || h.TicketPadreID == nil || *f.TicketPadreID != *h.TicketPadreID {
				continue
			}
			if empresa == nil {
				// Router compartido: los eventos del hijo salen con su empresa (y la auditoría la anota).
				t.FijarEmpresa(deref(f.EmpresaID))
				porEmpresa[deref(f.EmpresaID)]++
			}
			err = m.resolverHijoConfirmado(ctx, t, q, f, ahora)
			if empresa == nil {
				t.FijarEmpresa(0)
			}
			if err != nil {
				return resueltos, err
			}
			resueltos++
		}
		if len(hijos) < TandaConfirmados {
			if len(porEmpresa) > 0 {
				t.Anotar(map[string]any{"hijos_falla_por_empresa": porEmpresa})
			}
			return resueltos, nil
		}
	}
}

func claveAvisoFalla(aviso string, incidente int64) string {
	return fmt.Sprintf("%s:i%d", aviso, incidente)
}

func (m *Modulo) encolarAvisoFalla(ctx context.Context, t *auditoria.Tx, g soportebd.SoporteTicket, inc soportebd.SoporteIncidentesRed, aviso string) error {
	if g.Maestro == nil || *g.Maestro != MaestroFallaGeneral {
		// El general es el maestro de Horus: Horus ya avisa a los hijos (apertura y cierre).
		return nil
	}
	t.Anotar(map[string]any{"aviso_hijos": aviso})
	if m.cfg.Encolar == nil {
		return nil
	}
	return m.cfg.Encolar(ctx, t.Tx, ArgsAvisarHijos{MaestroID: g.ID, Aviso: aviso, Incidente: inc.ID})
}

// Ticket general cerrado a los 30 min ------------------------------------------------------------

// ArgsCerrarFallaGeneral cierra el ticket general resuelto si su falla no volvió en 30 min.
type ArgsCerrarFallaGeneral struct {
	TicketID  int64 `json:"ticket_id"`
	Incidente int64 `json:"incidente"`
	// ResueltoEn: la resolución que lo programó (una persona puede reabrirlo y volver a resolverlo:
	// otro job, que no choca con la unicidad del anterior).
	ResueltoEn time.Time `json:"resuelto_en,omitzero"`
}

// Kind implementa river.JobArgs.
func (ArgsCerrarFallaGeneral) Kind() string { return "soporte.cerrar_falla_general" }

// InsertOpts: a los 30 min, único por ticket e incidente.
func (ArgsCerrarFallaGeneral) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 10, UniqueOpts: unicoMientrasPendiente, ScheduledAt: time.Now().Add(VentanaReusoFalla)}
}

func (m *Modulo) nuevoCerrarFallaGeneral() *auditoria.Caso[ArgsCerrarFallaGeneral, bool] {
	return auditoria.Nuevo(auditoria.Caso[ArgsCerrarFallaGeneral, bool]{
		Accion: "tickets.cerrar_falla_general", Acceso: auditoria.Autenticado, AuditarExito: func(b bool) bool { return b },
		Logica: func(ctx context.Context, t *auditoria.Tx, in ArgsCerrarFallaGeneral) (bool, error) {
			if err := esSistema(ctx); err != nil {
				return false, err
			}
			q := soportebd.New(t)
			previo, err := q.TicketPorID(ctx, in.TicketID)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			// El mismo candado que la apertura: no compite con el reuso del ticket.
			if err := q.BloquearIncidente(ctx, fmt.Sprintf("soporte.red:empresa:%d", deref(previo.EmpresaID))); err != nil {
				return false, err
			}
			g, err := q.TicketPorIDParaActualizar(ctx, in.TicketID)
			if err != nil {
				return false, err
			}
			t.FijarEmpresa(deref(g.EmpresaID))
			t.FijarEntidad("ticket", g.ID)
			// Solo si sigue resuelto y ninguna falla abierta lo usa (no se reusó).
			incs, err := q.IncidentesDeTicket(ctx, g.ID)
			if err != nil {
				return false, err
			}
			for _, i := range incs {
				if i.Estado == "abierta" {
					return false, nil
				}
			}
			if g.Estado != EstadoResuelto || g.ResueltoEn == nil || m.ahora().Sub(*g.ResueltoEn) < VentanaReusoFalla {
				return false, nil
			}
			cal, err := cargarCalendario(ctx, q, g.CalendarioID)
			if err != nil {
				return false, err
			}
			f, err := guardar(ctx, t, q, g, cerrar(g, cal, m.ahora()), cal)
			if err != nil {
				return false, err
			}
			return true, cambioDeEstado(ctx, t, q, g, f, ptr("Argos: la falla general no volvió en 30 min"))
		},
	})
}

// TrabajadorCerrarFallaGeneral corre soporte.cerrar_falla_general.
type TrabajadorCerrarFallaGeneral struct {
	river.WorkerDefaults[ArgsCerrarFallaGeneral]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorCerrarFallaGeneral) Work(ctx context.Context, j *river.Job[ArgsCerrarFallaGeneral]) error {
	_, err := w.Modulo.CerrarFallaGeneral.Ejecutar(ctxWorker(ctx, w.BD), j.Args)
	return err
}

// Suscripción ------------------------------------------------------------------------------------

// SuscribirArgosRed: soporte se suscribe a los eventos de red que publica el conector (cmd/worker).
func (m *Modulo) SuscribirArgosRed(bus *integracion.Bus, bd nucleo.BD) {
	for _, ev := range []string{argos.EventoInternoCaidas, argos.EventoInternoFallaAbierta, argos.EventoInternoFallaCerrada, argos.EventoInternoFallaServicios} {
		bus.Suscribir("soporte.argos_red", ev, func(ctx context.Context, e integracion.Evento) error {
			return m.AlEventoArgosRed(nucleo.ConBD(ctx, bd), e)
		})
	}
}

// AlEventoArgosRed aplica un evento de red (actor sistema del relay). Un payload ilegible no se
// reintenta; un error de base sí (el relay).
func (m *Modulo) AlEventoArgosRed(ctx context.Context, e integracion.Evento) error {
	if e.Evento == argos.EventoInternoFallaServicios {
		var in EntradaFallaServicios
		if json.Unmarshal(e.Payload, &in) != nil || in.IncidenteID == "" {
			return nil
		}
		_, err := m.FallaServicios.Ejecutar(ctx, in)
		return reglaNoReintenta(err)
	}
	if e.Evento == argos.EventoInternoCaidas {
		var in EntradaCaidas
		if json.Unmarshal(e.Payload, &in) != nil || in.EntranteID == 0 {
			return nil
		}
		_, err := m.CaidasArgos.Ejecutar(ctx, in)
		return reglaNoReintenta(err)
	}
	var in EntradaFalla
	if json.Unmarshal(e.Payload, &in) != nil || in.EntranteID == 0 {
		return nil
	}
	in.Evento = e.Evento
	_, err := m.FallaArgos.Ejecutar(ctx, in)
	return reglaNoReintenta(err)
}

// reglaNoReintenta: una regla de negocio o un payload inválido no se arreglan reintentando.
func reglaNoReintenta(err error) error {
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Status >= 400 && p.Status < 500 {
		return nil
	}
	return err
}

// Vencimiento de la verificación con el cliente ------------------------------------------------------

// ArgsVencerVerificacion: si el cliente no respondió en las horas de la empresa, el hijo vuelve a
// abierto y se avisa a quien lo tiene asignado.
type ArgsVencerVerificacion struct {
	TicketID int64     `json:"ticket_id"`
	Desde    time.Time `json:"desde"` // en_espera_desde que dejó el cierre de la falla
	Vence    time.Time `json:"vence"`
}

// Kind implementa river.JobArgs.
func (ArgsVencerVerificacion) Kind() string { return "soporte.vencer_verificacion" }

// InsertOpts: programado al vencimiento, único por ticket y espera.
func (a ArgsVencerVerificacion) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 10, UniqueOpts: unicoMientrasPendiente, ScheduledAt: a.Vence}
}

func (m *Modulo) nuevoVencerVerificacion() *auditoria.Caso[ArgsVencerVerificacion, bool] {
	return auditoria.Nuevo(auditoria.Caso[ArgsVencerVerificacion, bool]{
		Accion: "tickets.vencer_verificacion", Acceso: auditoria.Autenticado, AuditarExito: func(b bool) bool { return b },
		Logica: func(ctx context.Context, t *auditoria.Tx, in ArgsVencerVerificacion) (bool, error) {
			if err := esSistema(ctx); err != nil {
				return false, err
			}
			q := soportebd.New(t)
			h, err := q.TicketPorIDParaActualizar(ctx, in.TicketID)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			t.FijarEmpresa(deref(h.EmpresaID))
			t.FijarEntidad("ticket", h.ID)
			// Solo si sigue en la misma espera (el cliente no respondió ni nadie lo movió).
			if h.Estado != EstadoEnEspera || h.EnEsperaDesde == nil || !h.EnEsperaDesde.Equal(in.Desde.UTC().Truncate(time.Microsecond)) {
				return false, nil
			}
			ahora := m.ahora()
			if ahora.Before(in.Vence) {
				return false, nil
			}
			cal, err := cargarCalendario(ctx, q, h.CalendarioID)
			if err != nil {
				return false, err
			}
			despues := h
			salirDeEspera(&despues, cal, ahora)
			despues.Estado = EstadoAbierto
			f, err := guardar(ctx, t, q, h, despues, cal)
			if err != nil {
				return false, err
			}
			if _, err := mensajeSistema(ctx, q, f, false, "El cliente no respondió a la verificación tras la falla general: el ticket vuelve a abierto."); err != nil {
				return false, err
			}
			if err := cambioDeEstado(ctx, t, q, h, f, ptr("Sin respuesta del cliente a la verificación")); err != nil {
				return false, err
			}
			return true, t.Emitir("soporte.verificacion_vencida", map[string]any{"ticket_id": f.ID, "numero": f.Numero, "empresa_id": f.EmpresaID,
				"asignado_a": f.AsignadoA})
		},
	})
}

// TrabajadorVencerVerificacion corre soporte.vencer_verificacion.
type TrabajadorVencerVerificacion struct {
	river.WorkerDefaults[ArgsVencerVerificacion]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorVencerVerificacion) Work(ctx context.Context, j *river.Job[ArgsVencerVerificacion]) error {
	_, err := w.Modulo.VencerVerificacion.Ejecutar(ctxWorker(ctx, w.BD), j.Args)
	return err
}

// Servicios verificados después de la apertura ---------------------------------------------------------

// EntradaFallaServicios es argos.falla_general_servicios: una tanda verificada por el conector.
type EntradaFallaServicios struct {
	EntranteID    int64   `json:"entrante_id"`
	IncidenteID   string  `json:"incidente_id"`
	EmpresaID     int64   `json:"empresa_id"`
	Servicios     []int64 `json:"servicios"`
	Descartados   int     `json:"descartados"`
	NoVerificados int     `json:"no_verificados"`
}

func (m *Modulo) nuevoFallaServicios() *auditoria.Caso[EntradaFallaServicios, ResultadoRed] {
	return auditoria.Nuevo(auditoria.Caso[EntradaFallaServicios, ResultadoRed]{
		Accion: "tickets.argos_falla_servicios", Acceso: auditoria.Autenticado, AuditarExito: auditarRed,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaFallaServicios) (ResultadoRed, error) {
			if err := esSistema(ctx); err != nil {
				return ResultadoRed{}, err
			}
			q := soportebd.New(t)
			t.FijarEmpresa(in.EmpresaID)
			if err := q.BloquearIncidente(ctx, fmt.Sprintf("soporte.red:empresa:%d", in.EmpresaID)); err != nil {
				return ResultadoRed{}, err
			}
			inc, err := q.IncidenteRed(ctx, soportebd.IncidenteRedParams{IncidenteID: in.IncidenteID, EmpresaID: in.EmpresaID})
			if errors.Is(err, pgx.ErrNoRows) {
				return ResultadoRed{Resultado: RedSinIncidente}, nil
			}
			if err != nil {
				return ResultadoRed{}, err
			}
			if inc.Estado != "abierta" {
				return ResultadoRed{Resultado: RedYaCerrada, TicketID: &inc.TicketID}, nil
			}
			g, err := q.TicketPorID(ctx, inc.TicketID)
			if err != nil {
				return ResultadoRed{}, err
			}
			t.FijarEntidad("ticket", g.ID)
			if in.Descartados > 0 || in.NoVerificados > 0 {
				if _, err := mensajeSistema(ctx, q, g, false, fmt.Sprintf("Argos: %d servicios más verificados; %d descartados (de otra empresa o inexistentes) y %d sin poder verificar.",
					len(in.Servicios), in.Descartados, in.NoVerificados)); err != nil {
					return ResultadoRed{}, err
				}
			}
			if _, err := m.sumarAfectados(ctx, t, q, inc, g, in.Servicios); err != nil {
				return ResultadoRed{}, err
			}
			t.Anotar(map[string]any{"servicios": len(in.Servicios), "descartados": in.Descartados, "no_verificados": in.NoVerificados})
			return ResultadoRed{Resultado: RedFallaActualizada, TicketID: &g.ID}, nil
		},
	})
}
