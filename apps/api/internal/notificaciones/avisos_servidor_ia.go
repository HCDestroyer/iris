package notificaciones

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	notificacionesbd "github.com/HCDestroyer/iris/apps/api/internal/notificaciones/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Avisos del servidor de IA de la instalación (contratos/voz.md §4.12): campana y correo a quienes
// tienen servidor_ia.ver y todas las empresas. Como mucho uno por tipo, servidor y hora. Son de la
// instalación: se ven con cualquier empresa activa.

// EnlaceServidorIA es la pantalla del servidor de IA en el panel.
const EnlaceServidorIA = "/administracion/servidor-ia"

// TiposAvisoServidorIA: los avisos de la instalación (no dependen de la empresa activa).
var TiposAvisoServidorIA = []string{TipoAvisoServidorIACaido, TipoAvisoServidorIAIncompatible, TipoAvisoServidorIAUmbral}

var umbralesServidorIA = []string{"vram", "disco", "temperatura", "saturacion"}

// EntradaAvisarServidorIA es un evento voz.motor_estado_cambiado o servidor_ia.umbral_superado.
type EntradaAvisarServidorIA struct {
	EventoID uuid.UUID `json:"-"`
	MotorID  int64     `json:"motor_id"`
	// A: el estado nuevo (voz.motor_estado_cambiado).
	A string `json:"a"`
	// Tipo, Valor y Umbral: servidor_ia.umbral_superado.
	Tipo   string   `json:"tipo"`
	Valor  *float64 `json:"valor"`
	Umbral *float64 `json:"umbral"`
}

// TituloServidorIA es el texto del aviso y el asunto del correo.
func TituloServidorIA(tipo, umbral string) string {
	switch tipo {
	case TipoAvisoServidorIACaido:
		return "El servidor de IA no responde"
	case TipoAvisoServidorIAIncompatible:
		return "El servidor de IA no es compatible con esta versión de Iris"
	}
	switch umbral {
	case "vram":
		return "El servidor de IA tiene la memoria de video casi llena"
	case "disco":
		return "El servidor de IA tiene el disco de modelos casi lleno"
	case "temperatura":
		return "La GPU del servidor de IA está muy caliente"
	}
	return "El servidor de IA está rechazando peticiones por saturación"
}

func (m *Modulo) nuevoAvisarServidorIA() *auditoria.Caso[EntradaAvisarServidorIA, ResultadoAvisarTope] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAvisarServidorIA, ResultadoAvisarTope]{
		Accion: "avisos.servidor_ia", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoAvisarTope) bool { return r.Avisos > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAvisarServidorIA) (ResultadoAvisarTope, error) {
			r := ResultadoAvisarTope{}
			if err := soloSistema(ctx); err != nil {
				return r, err
			}
			tipo, nivel := "", "peligro"
			switch {
			case in.A == "no_disponible":
				tipo = TipoAvisoServidorIACaido
			case in.A == "incompatible":
				tipo = TipoAvisoServidorIAIncompatible
			case slices.Contains(umbralesServidorIA, in.Tipo):
				tipo, nivel = TipoAvisoServidorIAUmbral, "alerta"
			default:
				return r, nil // otro cambio de estado (vuelve a listo, degradado…): no avisa
			}
			if in.MotorID <= 0 || in.EventoID == uuid.Nil {
				return r, nil
			}
			t.FijarEmpresa(0)
			t.FijarEntidad("voz_motor", in.MotorID)
			q := notificacionesbd.New(t)
			us, err := q.DestinatariosInstalacion(ctx, string(nucleo.PermServidorIAVer))
			if err != nil {
				return r, err
			}
			// Enfriamiento: como mucho un aviso (y un correo) por tipo, servidor y hora, por más
			// episodios que se abran (un servidor inestable u hostil no inunda la campana ni el
			// correo). La clave lleva la hora, no el evento; el episodio sigue en el tablero.
			clave := fmt.Sprintf("%s:%d:%s", tipo, in.MotorID, m.ahora().Format("2006-01-02T15"))
			if tipo == TipoAvisoServidorIAUmbral {
				clave = fmt.Sprintf("%s:%d:%s:%s", tipo, in.MotorID, in.Tipo, m.ahora().Format("2006-01-02T15"))
			}
			titulo := TituloServidorIA(tipo, in.Tipo)
			datosMapa := map[string]any{"motor_id": in.MotorID}
			if tipo == TipoAvisoServidorIAUmbral {
				// Solo números y el tipo de una lista cerrada: nada de lo que mande el servidor llega como texto.
				datosMapa["tipo"], datosMapa["valor"], datosMapa["umbral"] = in.Tipo, in.Valor, in.Umbral
			}
			datos, err := json.Marshal(datosMapa)
			if err != nil {
				return r, err
			}
			origen, _, err := m.origenCorreo(ctx, q, 0)
			if err != nil {
				return r, err
			}
			hayCorreo := origen != OrigenNinguno
			enlace := EnlaceServidorIA
			usuarios := []int64{}
			for _, u := range us {
				av, err := q.InsertarAviso(ctx, notificacionesbd.InsertarAvisoParams{UsuarioID: u.ID, Tipo: tipo, Clave: clave, Nivel: nivel,
					Titulo: titulo, Enlace: &enlace, Datos: datos, EventoID: pgtype.UUID{Bytes: in.EventoID, Valid: true}})
				if esSinFilas(err) {
					continue
				}
				if err != nil {
					return r, err
				}
				r.Avisos++
				usuarios = append(usuarios, u.ID)
				estado, motivo := "pendiente", (*string)(nil)
				switch {
				case !hayCorreo:
					estado, motivo = "omitido", ptr(CorreoSinSistema)
				case u.Correo == "":
					estado, motivo = "omitido", ptr(CorreoSinCorreo)
				}
				c, err := q.InsertarCorreo(ctx, notificacionesbd.InsertarCorreoParams{UsuarioID: u.ID, AvisoID: &av.ID, Tipo: tipo, Clave: clave,
					Para: u.Correo, Asunto: titulo, Datos: datos, Estado: estado, Motivo: motivo})
				if esSinFilas(err) {
					continue
				}
				if err != nil {
					return r, err
				}
				if estado == "omitido" {
					r.CorreosOmitidos++
					continue
				}
				r.CorreosPendientes++
				if m.cfg.Encolar != nil {
					if err := m.cfg.Encolar(ctx, t.Tx, ArgsEnviarCorreo{CorreoID: c.ID}); err != nil {
						return r, err
					}
				}
			}
			if r.Avisos == 0 {
				return r, nil
			}
			t.Anotar(map[string]any{"clave": clave, "avisos": r.Avisos, "correos_pendientes": r.CorreosPendientes, "correos_omitidos": r.CorreosOmitidos})
			correo := "enviar"
			if !hayCorreo {
				correo = "omitido"
			}
			return r, t.EmitirEnEmpresa("notificaciones.avisos_creados", nil, map[string]any{"tipo": tipo, "clave": clave,
				"empresa_id": nil, "usuarios": usuarios, "correo": correo})
		},
	})
}

// textoCorreoServidorIA arma el cuerpo del correo de un aviso del servidor de IA. Los valores son
// números ya validados por Iris; nunca texto que venga del servidor.
func (m *Modulo) textoCorreoServidorIA(c notificacionesbd.NotificacionesCorreo) string {
	var d struct {
		Tipo   string   `json:"tipo"`
		Valor  *float64 `json:"valor"`
		Umbral *float64 `json:"umbral"`
	}
	_ = json.Unmarshal(c.Datos, &d)
	texto := c.Asunto + ".\n\n"
	switch c.Tipo {
	case TipoAvisoServidorIACaido:
		texto += "Iris dejó de recibir respuesta del servidor de IA. Mientras esté caído, las sugerencias del asistente fallan con «Reintentar» y el modo autónomo pasa las conversaciones a una persona. Cuando vuelva se usa de nuevo sin tocar nada.\n"
	case TipoAvisoServidorIAIncompatible:
		texto += "La versión del servidor de IA y la de Iris no son compatibles, así que Iris no lo usa. La pantalla del servidor dice cuál de los dos hay que actualizar.\n"
	default:
		if d.Valor != nil && d.Umbral != nil && slices.Contains(umbralesServidorIA, d.Tipo) {
			texto += fmt.Sprintf("Valor medido: %.0f. Umbral: %.0f.\n", *d.Valor, *d.Umbral)
		}
	}
	texto += fmt.Sprintf("\nRevisa el servidor en %s%s\n", m.baseURL(), EnlaceServidorIA)
	texto += "\nEste aviso lo envía Iris a quienes administran el servidor de IA de la instalación.\n"
	return texto
}

func (m *Modulo) suscribirServidorIA(bus *integracion.Bus, bd nucleo.BD) {
	for _, evento := range []string{"voz.motor_estado_cambiado", "servidor_ia.umbral_superado"} {
		bus.Suscribir("notificaciones.avisos", evento, func(ctx context.Context, e integracion.Evento) error {
			var in EntradaAvisarServidorIA
			if err := json.Unmarshal(e.Payload, &in); err != nil {
				return fmt.Errorf("notificaciones: payload de %s: %w", e.Evento, err)
			}
			in.EventoID = e.ID
			if e.Evento == "voz.motor_estado_cambiado" {
				in.Tipo = "" // el estado manda; «tipo» es de los umbrales
			} else {
				in.A = ""
			}
			_, err := m.AvisarServidorIA.Ejecutar(nucleo.ConBD(ctx, bd), in)
			return err
		})
	}
}
