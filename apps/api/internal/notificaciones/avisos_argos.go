package notificaciones

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	notificacionesbd "github.com/HCDestroyer/iris/apps/api/internal/notificaciones/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Aviso del certificado TLS de la API de Argos (contrato Iris ↔ ArgosISP 0.8.0 §1.5, «registrar y
// avisar»; decisión del PM §10.2 punto 15): cuando cambia sin reconexión, la campana avisa a quienes
// pueden conectar integraciones con todas las empresas. Es solo informativo: Iris no detuvo nada.
// Uno por cambio (clave con la huella anterior y la nueva): repetir el evento no avisa dos veces.

// TipoAvisoArgosCertificado: el certificado de la API de Argos cambió.
const TipoAvisoArgosCertificado = "argos_certificado"

// EnlaceEstadoArgos es la pantalla de estado de la conexión con Argos.
const EnlaceEstadoArgos = "/administracion/integraciones/argos"

// TituloArgosCertificado es el texto del aviso.
const TituloArgosCertificado = "Cambió el certificado de Argos"

// EntradaAvisarCertificadoArgos es el payload de argos.certificado_cambiado.
type EntradaAvisarCertificadoArgos struct {
	EventoID       uuid.UUID `json:"-"`
	ConectorID     int64     `json:"conector_id"`
	CertificadoID  int64     `json:"certificado_id"`
	Huella         string    `json:"huella_sha256"`
	HuellaAnterior string    `json:"huella_anterior"`
}

var reHuella = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (m *Modulo) nuevoAvisarCertificadoArgos() *auditoria.Caso[EntradaAvisarCertificadoArgos, ResultadoAvisarTope] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAvisarCertificadoArgos, ResultadoAvisarTope]{
		Accion: "avisos.argos_certificado", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoAvisarTope) bool { return r.Avisos > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAvisarCertificadoArgos) (ResultadoAvisarTope, error) {
			r := ResultadoAvisarTope{}
			if err := soloSistema(ctx); err != nil {
				return r, err
			}
			if in.ConectorID <= 0 || in.EventoID == uuid.Nil || !reHuella.MatchString(in.Huella) {
				return r, nil
			}
			t.FijarEmpresa(0)
			t.FijarEntidad("conector", in.ConectorID)
			q := notificacionesbd.New(t)
			us, err := q.DestinatariosInstalacion(ctx, string(nucleo.PermIntegracionesConectar))
			if err != nil {
				return r, err
			}
			clave := fmt.Sprintf("%s:%d:%s:%s", TipoAvisoArgosCertificado, in.ConectorID, in.HuellaAnterior, in.Huella)
			datosMapa := map[string]any{"conector_id": in.ConectorID, "huella_sha256": in.Huella}
			if reHuella.MatchString(in.HuellaAnterior) {
				datosMapa["huella_anterior"] = in.HuellaAnterior
			}
			datos, err := json.Marshal(datosMapa)
			if err != nil {
				return r, err
			}
			enlace := EnlaceEstadoArgos
			usuarios := []int64{}
			for _, u := range us {
				_, err := q.InsertarAviso(ctx, notificacionesbd.InsertarAvisoParams{UsuarioID: u.ID, Tipo: TipoAvisoArgosCertificado, Clave: clave,
					Nivel: "alerta", Titulo: TituloArgosCertificado, Enlace: &enlace, Datos: datos, EventoID: pgtype.UUID{Bytes: in.EventoID, Valid: true}})
				if esSinFilas(err) {
					continue // ya avisado de este certificado
				}
				if err != nil {
					return r, err
				}
				r.Avisos++
				usuarios = append(usuarios, u.ID)
			}
			if r.Avisos == 0 {
				return r, nil
			}
			t.Anotar(map[string]any{"clave": clave, "avisos": r.Avisos})
			// Solo campana: el correo del sistema no se usa para este aviso informativo.
			return r, t.EmitirEnEmpresa("notificaciones.avisos_creados", nil, map[string]any{"tipo": TipoAvisoArgosCertificado, "clave": clave,
				"empresa_id": nil, "usuarios": usuarios, "correo": "omitido"})
		},
	})
}

func (m *Modulo) suscribirArgos(bus *integracion.Bus, bd nucleo.BD) {
	bus.Suscribir("notificaciones.avisos", "argos.certificado_cambiado", func(ctx context.Context, e integracion.Evento) error {
		var in EntradaAvisarCertificadoArgos
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return fmt.Errorf("notificaciones: payload de %s: %w", e.Evento, err)
		}
		in.EventoID = e.ID
		_, err := m.AvisarCertificadoArgos.Ejecutar(nucleo.ConBD(ctx, bd), in)
		return err
	})
}
