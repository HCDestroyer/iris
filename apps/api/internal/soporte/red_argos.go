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
	CaidasUmbralFabrica     = 3
	CaidasVentanaMinFabrica = 60
	FallaUmbralFabrica      = 20
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
	EntranteID  int64      `json:"entrante_id"`
	Evento      string     `json:"-"`
	IncidenteID string     `json:"incidente_id"`
	EmpresaID   int64      `json:"empresa_id"`
	Alcance     string     `json:"alcance"`
	AlcanceID   string     `json:"alcance_id"`
	Servicios   []int64    `json:"servicios"`
	Descartados int        `json:"descartados"`
	Desde       *time.Time `json:"desde"`
	Hasta       *time.Time `json:"hasta"`
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
	CaidasUmbral     int32 `json:"caidasUmbral" minimum:"1" maximum:"100" doc:"Caídas que abren un ticket (de fábrica 3)"`
	CaidasVentanaMin int32 `json:"caidasVentanaMin" minimum:"5" maximum:"1440" doc:"En cuántos minutos (de fábrica 60)"`
	FallaUmbral      int32 `json:"fallaUmbral" minimum:"2" maximum:"1000" doc:"Tickets de caídas en 10 min a partir de los cuales Iris deja de abrirlos y avisa una posible falla general (de fábrica 20)"`
	Version          int32 `json:"version" doc:"0 = de fábrica"`
}

func ajustesRed(ctx context.Context, q *soportebd.Queries, empresa int64) (AjustesRed, error) {
	a, err := q.AjustesRed(ctx, empresa)
	if errors.Is(err, pgx.ErrNoRows) {
		return AjustesRed{CaidasUmbral: CaidasUmbralFabrica, CaidasVentanaMin: CaidasVentanaMinFabrica, FallaUmbral: FallaUmbralFabrica}, nil
	}
	if err != nil {
		return AjustesRed{}, err
	}
	return AjustesRed{CaidasUmbral: a.CaidasUmbral, CaidasVentanaMin: a.CaidasVentanaMin, FallaUmbral: a.FallaUmbral, Version: a.Version}, nil
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
			if int32(in.Sesiones) < aj.CaidasUmbral || int32(in.VentanaMinutos) > aj.CaidasVentanaMin {
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
	// 4. Posible falla general abierta: nota en ella (no se abren más).
	posible, err := q.PosibleFallaAbierta(ctx, &empresa)
	switch {
	case err == nil:
		if _, err := mensajeSistema(ctx, q, posible, false, nota); err != nil {
			return ResultadoRed{}, err
		}
		return ResultadoRed{Resultado: RedNotaPosible, TicketID: &posible.ID}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return ResultadoRed{}, err
	}
	// 5. Más de N tickets de caídas en 10 min: posible falla general.
	desde := m.ahora().Add(-VentanaPosibleFalla)
	n, err := q.ContarCaidasRecientes(ctx, soportebd.ContarCaidasRecientesParams{EmpresaID: &empresa, CategoriaID: cat, Desde: desde})
	if err != nil {
		return ResultadoRed{}, err
	}
	if n >= aj.FallaUmbral {
		return m.abrirPosibleFalla(ctx, t, q, empresa, *cat, desde, n, nota)
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
	nota := fmt.Sprintf("Argos: falla general %s en %s desde %s. Servicios afectados: %d.", in.IncidenteID, in.que(),
		in.Desde.UTC().Format(time.RFC3339), len(in.Servicios))
	if in.Descartados > 0 {
		nota += fmt.Sprintf(" Se descartaron %d ids que no son de esta empresa o no existen en Argos.", in.Descartados)
	}
	resultado := RedFallaCreada
	var general soportebd.SoporteTicket
	var reusa *int64
	previo, err := q.IncidenteReusable(ctx, soportebd.IncidenteReusableParams{EmpresaID: empresa, Alcance: in.Alcance, AlcanceID: in.AlcanceID,
		Desde: ptr(ahora.Add(-VentanaReusoFalla))})
	switch {
	case err == nil:
		// El mismo alcance reabre antes de 30 min: se reusa el ticket general.
		g, err := q.TicketPorIDParaActualizar(ctx, previo.TicketID)
		if err != nil {
			return ResultadoRed{}, err
		}
		if general, err = m.reabrirGeneral(ctx, t, q, g, nota+" Se reusa el ticket de la falla "+previo.IncidenteID+" (cerrada hace menos de 30 min)."); err != nil {
			return ResultadoRed{}, err
		}
		reusa, resultado = &previo.ID, RedFallaReusada
	case !errors.Is(err, pgx.ErrNoRows):
		return ResultadoRed{}, err
	default:
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
	}
	t.Anotar(map[string]any{"absorbidos": len(hijos)})
	return len(hijos), m.encolarAvisoFalla(ctx, t, general, inc, AvisoFallaAbierta)
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
		return ResultadoRed{Resultado: RedSinIncidente}, nil
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
	// Los hijos no se cierran solos: pasan a «en espera» para verificar con el cliente.
	hijos, err := q.HijosAbiertos(ctx, &g.ID)
	if err != nil {
		return ResultadoRed{}, err
	}
	for _, h := range hijos {
		if h.Maestro != nil {
			continue
		}
		if err := m.esperarVerificacion(ctx, t, q, h, ahora); err != nil {
			return ResultadoRed{}, err
		}
	}
	if g.Maestro != nil && *g.Maestro == MaestroFallaGeneral && abierto(g.Estado) {
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
		if m.cfg.Encolar != nil {
			if err := m.cfg.Encolar(ctx, t.Tx, ArgsCerrarFallaGeneral{TicketID: f.ID, Incidente: inc.ID}); err != nil {
				return ResultadoRed{}, err
			}
		}
		g = f
	} else if _, err := mensajeSistema(ctx, q, g, false, nota); err != nil {
		// Un maestro de Horus lo resuelve una persona (Horus propone cuando el router vuelve).
		return ResultadoRed{}, err
	}
	if err := m.encolarAvisoFalla(ctx, t, g, inc, AvisoFallaCerrada); err != nil {
		return ResultadoRed{}, err
	}
	return ResultadoRed{Resultado: RedFallaCerrada, TicketID: &g.ID}, nil
}

// esperarVerificacion: el hijo pasa a «en espera» (verificar con el cliente); un mensaje del cliente
// lo reanuda como siempre.
func (m *Modulo) esperarVerificacion(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, h soportebd.SoporteTicket, ahora time.Time) error {
	if h.Estado == EstadoEnEspera {
		_, err := mensajeSistema(ctx, q, h, false, "La falla general se cerró: verificar con el cliente que el servicio funcione.")
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
	if _, err := mensajeSistema(ctx, q, f, false, "La falla general se cerró: verificar con el cliente que el servicio funcione."); err != nil {
		return err
	}
	return cambioDeEstado(ctx, t, q, h, f, ptr("Verificar con el cliente: la falla general se cerró"))
}

func (m *Modulo) encolarAvisoFalla(ctx context.Context, t *auditoria.Tx, g soportebd.SoporteTicket, inc soportebd.SoporteIncidentesRed, aviso string) error {
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
			g, err := q.TicketPorIDParaActualizar(ctx, in.TicketID)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
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
	for _, ev := range []string{argos.EventoInternoCaidas, argos.EventoInternoFallaAbierta, argos.EventoInternoFallaCerrada} {
		bus.Suscribir("soporte.argos_red", ev, func(ctx context.Context, e integracion.Evento) error {
			return m.AlEventoArgosRed(nucleo.ConBD(ctx, bd), e)
		})
	}
}

// AlEventoArgosRed aplica un evento de red (actor sistema del relay). Un payload ilegible no se
// reintenta; un error de base sí (el relay).
func (m *Modulo) AlEventoArgosRed(ctx context.Context, e integracion.Evento) error {
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
