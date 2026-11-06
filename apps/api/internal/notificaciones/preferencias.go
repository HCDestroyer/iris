package notificaciones

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	notificacionesbd "github.com/HCDestroyer/iris/apps/api/internal/notificaciones/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Tipos de push (también son los ids de los canales de Android).
const (
	TipoChat    = "chat"
	TipoMencion = "mencion"
	TipoOrden   = "orden"
	TipoBandeja = "bandeja"
)

// Contenido del aviso.
const (
	ContenidoCompleto  = "completo"
	ContenidoRemitente = "remitente"
)

// TiposPush encendidos o no.
type TiposPush struct {
	Chat    bool `json:"chat" doc:"Mensajes del equipo (directos, de tu cuadrilla y de tus órdenes)"`
	Mencion bool `json:"mencion" doc:"Cuando alguien te menciona"`
	Orden   bool `json:"orden" doc:"Órdenes asignadas a ti o reprogramadas"`
	Bandeja bool `json:"bandeja" doc:"«¿Alguien la atiende?» de la bandeja de clientes"`
}

// NoMolestar es el horario sin avisos.
type NoMolestar struct {
	Activo       bool   `json:"activo"`
	Desde        string `json:"desde" pattern:"^([01][0-9]|2[0-3]):[0-5][0-9]$" doc:"HH:MM en zonaHoraria"`
	Hasta        string `json:"hasta" pattern:"^([01][0-9]|2[0-3]):[0-5][0-9]$" doc:"HH:MM; menor que desde cruza la medianoche"`
	UrgenteIgual bool   `json:"urgenteIgual" doc:"Avisar igual lo urgente: una orden asignada o reprogramada para hoy y las menciones"`
}

// EstadoPushVista es el estado del servidor (solo lectura).
type EstadoPushVista struct {
	Estado string `json:"estado" enum:"configurado,sin_configurar,error"`
	Texto  string `json:"texto" doc:"«Notificaciones push: sin configurar»"`
}

// Preferencias de la cuenta (valen en cualquier teléfono).
type Preferencias struct {
	Version           int32           `json:"version" doc:"0 = nunca guardadas (valores por defecto)"`
	Tipos             TiposPush       `json:"tipos"`
	Contenido         string          `json:"contenido" enum:"completo,remitente" doc:"completo: quién y qué dice; remitente: solo «Nuevo mensaje de X»"`
	NoMolestar        NoMolestar      `json:"noMolestar"`
	ZonaHoraria       string          `json:"zonaHoraria" doc:"Zona IANA (America/Guatemala)"`
	Push              EstadoPushVista `json:"push" readOnly:"true"`
	BandejaDisponible bool            `json:"bandejaDisponible" readOnly:"true" doc:"Tienes conversaciones.ver: muestra el interruptor de la bandeja"`
}

// EntradaPreferencias es POST /yo/notificaciones (reemplaza todo).
type EntradaPreferencias struct {
	Version     int32      `json:"version" minimum:"0" doc:"La versión que leíste (0 si nunca se guardaron)"`
	Tipos       TiposPush  `json:"tipos"`
	Contenido   string     `json:"contenido" enum:"completo,remitente"`
	NoMolestar  NoMolestar `json:"noMolestar"`
	ZonaHoraria string     `json:"zonaHoraria" minLength:"1" maxLength:"60"`
}

// porDefecto son las preferencias sin fila guardada.
func porDefecto(usuario int64) notificacionesbd.NotificacionesPreferencia {
	return notificacionesbd.NotificacionesPreferencia{UsuarioID: usuario, Chat: true, Mencion: true, Orden: true, Bandeja: true,
		Contenido: ContenidoCompleto, NoMolestarDesde: 21 * 60, NoMolestarHasta: 6 * 60, UrgenteIgual: true, ZonaHoraria: ZonaPorDefecto}
}

// leerPreferencias devuelve la fila o los valores por defecto (version 0).
func leerPreferencias(ctx context.Context, q *notificacionesbd.Queries, usuario int64) (notificacionesbd.NotificacionesPreferencia, error) {
	p, err := q.Preferencias(ctx, usuario)
	if errors.Is(err, pgx.ErrNoRows) {
		return porDefecto(usuario), nil
	}
	return p, err
}

func hhmm(min int32) string { return fmt.Sprintf("%02d:%02d", min/60, min%60) }

func minutosDe(campo, s string) (int32, error) {
	var h, mi int32
	if len(s) != 5 || s[2] != ':' {
		return 0, nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: "Usa HH:MM (24 h)", Rule: "pattern"})
	}
	if _, err := fmt.Sscanf(s, "%02d:%02d", &h, &mi); err != nil || h > 23 || mi > 59 || h < 0 || mi < 0 {
		return 0, nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: "Usa HH:MM (24 h)", Rule: "pattern"})
	}
	return h*60 + mi, nil
}

func (m *Modulo) aPreferencias(ctx context.Context, p notificacionesbd.NotificacionesPreferencia) Preferencias {
	return Preferencias{
		Version:           p.Version,
		Tipos:             TiposPush{Chat: p.Chat, Mencion: p.Mencion, Orden: p.Orden, Bandeja: p.Bandeja},
		Contenido:         p.Contenido,
		NoMolestar:        NoMolestar{Activo: p.NoMolestar, Desde: hhmm(p.NoMolestarDesde), Hasta: hhmm(p.NoMolestarHasta), UrgenteIgual: p.UrgenteIgual},
		ZonaHoraria:       p.ZonaHoraria,
		Push:              EstadoPushVista{Estado: m.cfg.Estado.Estado, Texto: m.cfg.Estado.Texto()},
		BandejaDisponible: contexto.PermisosDe(ctx).Tiene(string(nucleo.PermConversacionesVer)),
	}
}

// vistaAuditada: lo que cambia (para auditoria.cambios).
func vistaAuditada(p notificacionesbd.NotificacionesPreferencia) map[string]any {
	return map[string]any{"chat": p.Chat, "mencion": p.Mencion, "orden": p.Orden, "bandeja": p.Bandeja, "contenido": p.Contenido,
		"no_molestar": p.NoMolestar, "no_molestar_desde": hhmm(p.NoMolestarDesde), "no_molestar_hasta": hhmm(p.NoMolestarHasta),
		"urgente_igual": p.UrgenteIgual, "zona_horaria": p.ZonaHoraria}
}

func (m *Modulo) nuevoVerPreferencias() *auditoria.Caso[struct{}, Preferencias] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, Preferencias]{
		Accion:       "notificaciones.ver_preferencias",
		Acceso:       auditoria.Autenticado,
		AuditarExito: func(Preferencias) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (Preferencias, error) {
			a, err := persona(ctx)
			if err != nil {
				return Preferencias{}, err
			}
			p, err := leerPreferencias(ctx, notificacionesbd.New(t), a.ID)
			if err != nil {
				return Preferencias{}, err
			}
			return m.aPreferencias(ctx, p), nil
		},
	})
}

func (m *Modulo) nuevoGuardarPreferencias() *auditoria.Caso[EntradaPreferencias, Preferencias] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPreferencias, Preferencias]{
		Accion: "notificaciones.guardar_preferencias",
		Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPreferencias) (Preferencias, error) {
			a, err := persona(ctx)
			if err != nil {
				return Preferencias{}, err
			}
			if in.Contenido != ContenidoCompleto && in.Contenido != ContenidoRemitente {
				return Preferencias{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "contenido", Message: "completo o remitente", Rule: "enum"})
			}
			desde, err := minutosDe("noMolestar.desde", in.NoMolestar.Desde)
			if err != nil {
				return Preferencias{}, err
			}
			hasta, err := minutosDe("noMolestar.hasta", in.NoMolestar.Hasta)
			if err != nil {
				return Preferencias{}, err
			}
			if desde == hasta {
				return Preferencias{}, nucleo.ReglaNegocio("horario_invalido", "«Desde» y «Hasta» no pueden ser la misma hora")
			}
			in.ZonaHoraria = strings.TrimSpace(in.ZonaHoraria)
			if _, err := time.LoadLocation(in.ZonaHoraria); err != nil || in.ZonaHoraria == "" || strings.EqualFold(in.ZonaHoraria, "local") {
				return Preferencias{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "zonaHoraria", Message: "Zona horaria desconocida (p. ej. America/Guatemala)", Rule: "zona"})
			}
			q := notificacionesbd.New(t)
			antes, err := q.PreferenciasParaActualizar(ctx, a.ID)
			existe := err == nil
			if errors.Is(err, pgx.ErrNoRows) {
				antes = porDefecto(a.ID)
			} else if err != nil {
				return Preferencias{}, err
			}
			if err := nucleo.ExigirVersion(in.Version, antes.Version, "preferencias_notificaciones", a.ID); err != nil {
				return Preferencias{}, err
			}
			var f notificacionesbd.NotificacionesPreferencia
			if existe {
				f, err = q.ActualizarPreferencias(ctx, notificacionesbd.ActualizarPreferenciasParams{UsuarioID: a.ID, Version: in.Version,
					Chat: in.Tipos.Chat, Mencion: in.Tipos.Mencion, Orden: in.Tipos.Orden, Bandeja: in.Tipos.Bandeja, Contenido: in.Contenido,
					NoMolestar: in.NoMolestar.Activo, NoMolestarDesde: desde, NoMolestarHasta: hasta, UrgenteIgual: in.NoMolestar.UrgenteIgual,
					ZonaHoraria: in.ZonaHoraria})
				err = nucleo.VerificarVersion(err, "preferencias_notificaciones", a.ID)
			} else {
				f, err = q.InsertarPreferencias(ctx, notificacionesbd.InsertarPreferenciasParams{UsuarioID: a.ID,
					Chat: in.Tipos.Chat, Mencion: in.Tipos.Mencion, Orden: in.Tipos.Orden, Bandeja: in.Tipos.Bandeja, Contenido: in.Contenido,
					NoMolestar: in.NoMolestar.Activo, NoMolestarDesde: desde, NoMolestarHasta: hasta, UrgenteIgual: in.NoMolestar.UrgenteIgual,
					ZonaHoraria: in.ZonaHoraria})
				// Otra petición la creó a la vez: version 0 ya no es la actual.
				err = nucleo.VerificarVersion(err, "preferencias_notificaciones", a.ID)
			}
			if err != nil {
				return Preferencias{}, err
			}
			campos := t.RegistrarCambios("preferencias_notificaciones", a.ID, vistaAuditada(antes), vistaAuditada(f))
			if campos == nil {
				campos = []string{}
			}
			if err := t.EmitirEnEmpresa("notificaciones.preferencias_cambiadas", nil, map[string]any{"usuario_id": a.ID,
				"campos": campos, "version": f.Version}); err != nil {
				return Preferencias{}, err
			}
			return m.aPreferencias(ctx, f), nil
		},
	})
}

// enSilencio dice si ahora cae en el No molestar del usuario.
func enSilencio(p notificacionesbd.NotificacionesPreferencia, ahora time.Time) bool {
	if !p.NoMolestar {
		return false
	}
	local := ahora.In(zonaDe(p))
	min := int32(local.Hour()*60 + local.Minute()) //nolint:gosec // < 1440
	if p.NoMolestarDesde < p.NoMolestarHasta {
		return min >= p.NoMolestarDesde && min < p.NoMolestarHasta
	}
	return min >= p.NoMolestarDesde || min < p.NoMolestarHasta
}

func zonaDe(p notificacionesbd.NotificacionesPreferencia) *time.Location {
	if z, err := time.LoadLocation(p.ZonaHoraria); err == nil {
		return z
	}
	z, _ := time.LoadLocation(ZonaPorDefecto)
	return z
}

// tipoEncendido dice si el usuario quiere este tipo.
func tipoEncendido(p notificacionesbd.NotificacionesPreferencia, tipo string) bool {
	switch tipo {
	case TipoChat:
		return p.Chat
	case TipoMencion:
		return p.Mencion
	case TipoOrden:
		return p.Orden
	case TipoBandeja:
		return p.Bandeja
	}
	return false
}
