package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Llenar la caché de contactos al atender (contrato argosisp.md §3.5, B22) --------------------------
//
// La caché de contactos (integracion.argos_contactos) la llena Argos por cliente. Cuando una persona
// abre una conversación (o el asistente la atiende), Iris se asegura de tener los contactos:
//
//   - Con cliente asociado: relee los contactos de ese cliente si tienen más de 24 h.
//   - Sin cliente: busca el número en Argos (GET /contactos/buscar, una vez al día por número y
//     empresa), trae a la instantánea los clientes que respondió y lee sus contactos. Si el número
//     queda de una sola persona, la conversación se asocia sola, igual que al llegar el mensaje.
//
// buscar exige X-Iris-Actor de quien atiende (nunca «sistema», 0.14.2): por eso no se hace al llegar
// el mensaje, que procesa el sistema. Todo es de mejor esfuerzo: si Argos no responde, se sigue con lo
// que haya en la caché.

// ArgosContactos son las lecturas de contactos de Argos (las implementa argos.Modulo). Opcional.
type ArgosContactos interface {
	BuscarContactos(ctx context.Context, actor, telefono string) ([]argos.ContactoBuscado, bool, error)
	RefrescarContactos(ctx context.Context, in argos.EntradaRefrescarContactos) (bool, error)
}

// VigenciaBusquedaNumero: un número sin cliente se busca en Argos como mucho una vez en este tiempo.
const VigenciaBusquedaNumero = 24 * time.Hour

// maxClientesBuscados: cuántos clientes de una búsqueda se traen a la instantánea.
const maxClientesBuscados = 5

// tiempoContactos: lo que se espera a Argos al abrir una conversación.
const tiempoContactos = 4 * time.Second

// actorAtencion: X-Iris-Actor de quien atiende («usuario:<id>»); vacío si no es una persona.
func actorAtencion(ctx context.Context) string {
	a := contexto.ActorDe(ctx)
	if a.Tipo != contexto.ActorUsuario || a.ID == 0 {
		return ""
	}
	return "usuario:" + strconv.FormatInt(a.ID, 10)
}

// PrepararContactosConv deja en la caché los contactos del número de la conversación y, si con eso
// queda una sola persona, asocia la conversación. actor: «usuario:<id>» o «asistente_ia». Devuelve
// si cambió algo que se ve en la conversación. Nunca falla: los errores solo van al log.
func (m *Modulo) PrepararContactosConv(ctx context.Context, convID int64, actor string) bool {
	ac, ok := m.cfg.Argos.(ArgosContactos)
	if m.cfg.Argos == nil || !ok || actor == "" {
		return false
	}
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return false
	}
	cctx, cancelar := context.WithTimeout(ctx, tiempoContactos)
	defer cancelar()
	q := mensajeriabd.New(bd)
	c, err := q.ConversacionClientePorID(cctx, convID)
	if err != nil {
		return false
	}
	if c.ClienteID != nil {
		leido, err := ac.RefrescarContactos(cctx, argos.EntradaRefrescarContactos{ClienteID: *c.ClienteID, Actor: actor})
		if err != nil {
			slog.WarnContext(ctx, "mensajeria: no se pudieron leer los contactos del cliente", "conversacion", convID, "error", err)
		}
		return leido
	}
	empresa := empresaDeConv(c)
	digitos := argos.DigitosTelefono(c.Numero)
	if _, err := q.NumeroBuscadoEn(cctx, mensajeriabd.NumeroBuscadoEnParams{Empresa: empresa, Digitos: digitos,
		Desde: m.ahora().Add(-VigenciaBusquedaNumero)}); err == nil {
		return false
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	l, soporta, err := ac.BuscarContactos(cctx, actor, "+"+digitos)
	if err != nil || !soporta {
		if err != nil {
			slog.WarnContext(ctx, "mensajeria: no se pudo buscar el número en los contactos de Argos", "conversacion", convID, "error", err)
		}
		return false
	}
	clientes := []int64{}
	for _, x := range l {
		if x.EmpresaID != nil && empresa != 0 && *x.EmpresaID != empresa {
			continue
		}
		if x.ClienteID > 0 && !containsID(clientes, x.ClienteID) && len(clientes) < maxClientesBuscados {
			clientes = append(clientes, x.ClienteID)
		}
	}
	for _, cl := range clientes {
		if err := m.cfg.Argos.AsegurarInstantanea(cctx, cl, nil); err != nil {
			slog.WarnContext(ctx, "mensajeria: no se pudo traer el cliente de Argos", "conversacion", convID, "error", err)
			continue
		}
		if _, err := ac.RefrescarContactos(cctx, argos.EntradaRefrescarContactos{ClienteID: cl, Actor: actor}); err != nil {
			slog.WarnContext(ctx, "mensajeria: no se pudieron leer los contactos del cliente", "conversacion", convID, "error", err)
		}
	}
	r, err := m.asociarPorContactos.Ejecutar(ctx, entradaAsociarPorContactos{ConversacionID: convID, Empresa: empresa, Digitos: digitos, Encontrados: len(clientes)})
	if err != nil {
		slog.WarnContext(ctx, "mensajeria: no se pudo asociar la conversación por sus contactos", "conversacion", convID, "error", err)
		return false
	}
	return r || len(clientes) > 0
}

func containsID(l []int64, id int64) bool {
	for _, x := range l {
		if x == id {
			return true
		}
	}
	return false
}

// entradaAsociarPorContactos: la conversación, el número buscado y cuántos clientes respondió Argos.
type entradaAsociarPorContactos struct {
	ConversacionID int64  `json:"conversacion_id"`
	Empresa        int64  `json:"-"`
	Digitos        string `json:"-"`
	Encontrados    int    `json:"encontrados"`
}

// nuevoAsociarPorContactos anota la búsqueda del número y, si el número quedó de una sola persona
// de la empresa, asocia la conversación sola («automatica», como al llegar el mensaje): identifica,
// no verifica más que el número (§3.5, M6).
func nuevoAsociarPorContactos(m *Modulo) *auditoria.Caso[entradaAsociarPorContactos, bool] {
	return auditoria.Nuevo(auditoria.Caso[entradaAsociarPorContactos, bool]{
		Accion: "conversaciones.asociar_por_contacto", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaAsociarPorContactos) (bool, error) {
			q := mensajeriabd.New(t)
			if err := q.MarcarNumeroBuscado(ctx, mensajeriabd.MarcarNumeroBuscadoParams{Empresa: in.Empresa, Digitos: in.Digitos, BuscadoEn: m.ahora()}); err != nil {
				return false, err
			}
			c, err := q.ConversacionClienteParaActualizar(ctx, in.ConversacionID)
			if err != nil {
				return false, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("conversacion", c.ID)
			if c.ClienteID != nil {
				return false, nil
			}
			cliente, asociacion, contacto, err := clienteUnicoDelNumero(ctx, q, empresaDeConv(c), c.Numero)
			if err != nil || cliente == nil {
				return false, err
			}
			if err := fijarContactoConv(ctx, q, &c, contacto); err != nil {
				return false, err
			}
			antes := c
			c.ClienteID, c.ServicioID, c.Asociacion = cliente, nil, asociacion
			c.ClienteVerificado, c.VerificadoPor = true, nil
			if err := m.sistema(ctx, t, &c, EventoSistema{Tipo: "cliente_asociado", EntidadID: nombrePtr(strconv.FormatInt(*cliente, 10))}); err != nil {
				return false, err
			}
			if c, err = guardarConv(ctx, q, c, true); err != nil {
				return false, err
			}
			t.RegistrarCambios("conversacion", c.ID, auditableConv(antes), auditableConv(c))
			if err := t.Emitir("conversacion.cliente_asociado", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "cliente_id": *cliente,
				"servicio_id": nil, "asociacion": "automatica", "origen": "contacto", "actor_id": nil}); err != nil {
				return false, err
			}
			return true, m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil)
		},
	})
}

// Contactos que Argos quitó (§3.5, B22) --------------------------------------------------------------

// entradaContactosArgos es el payload de argos.contactos_cambiados (sin números ni nombres).
type entradaContactosArgos struct {
	ClienteID  int64  `json:"cliente_id"`
	ContactoID *int64 `json:"contacto_id"`
	Accion     string `json:"accion"`
}

// nuevoContactosArgosCambiados: las conversaciones asociadas por un contacto que ya no está en la caché
// (Argos lo quitó, dejó de devolverlo o la caché del cliente se olvidó) pierden la verificación del
// número: hasta que un supervisor confirme, no hay saldo ni pagos (y el rol ya falla cerrado).
func nuevoContactosArgosCambiados(m *Modulo) *auditoria.Caso[entradaContactosArgos, int] {
	return auditoria.Nuevo(auditoria.Caso[entradaContactosArgos, int]{
		Accion: "conversaciones.contacto_argos_quitado", Acceso: auditoria.Autenticado,
		AuditarExito: func(n int) bool { return n > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaContactosArgos) (int, error) {
			if err := soloSistema(ctx); err != nil {
				return 0, err
			}
			if in.ClienteID <= 0 {
				return 0, nil
			}
			q := mensajeriabd.New(t)
			cs, err := q.DesverificarConvsSinContacto(ctx, mensajeriabd.DesverificarConvsSinContactoParams{ClienteID: &in.ClienteID, ContactoID: in.ContactoID})
			if err != nil {
				return 0, err
			}
			for _, c := range cs {
				t.RegistrarCambios("conversacion", c.ID, map[string]any{"cliente_verificado": true}, map[string]any{"cliente_verificado": false})
				if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
					return 0, err
				}
			}
			t.Anotar(map[string]any{"cliente_id": in.ClienteID, "conversaciones": len(cs)})
			return len(cs), nil
		},
	})
}

// SuscribirContactos registra en el bus del relay (cmd/worker) el suscriptor de
// argos.contactos_cambiados. Idempotente.
func (m *Modulo) SuscribirContactos(bus *integracion.Bus, bd nucleo.BD) {
	bus.Suscribir("mensajeria.contactos_argos", argos.EventoContactosArgosCambiados, func(ctx context.Context, e integracion.Evento) error {
		var in entradaContactosArgos
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return nil // un payload ilegible no se arregla reintentando
		}
		_, err := m.contactosArgosCambiados.Ejecutar(ctxSistema(ctx, bd), in)
		return err
	})
}
