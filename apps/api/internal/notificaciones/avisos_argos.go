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
	bus.Suscribir("notificaciones.avisos", "integracion.pendiente_clave_reutilizada", func(ctx context.Context, e integracion.Evento) error {
		var in EntradaAvisarClaveReutilizada
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return fmt.Errorf("notificaciones: payload de %s: %w", e.Evento, err)
		}
		in.EventoID = e.ID
		_, err := m.AvisarClaveReutilizada.Ejecutar(nucleo.ConBD(ctx, bd), in)
		return err
	})
	bus.Suscribir("notificaciones.avisos", "argos.carga_vinculos_detenida", func(ctx context.Context, e integracion.Evento) error {
		var in EntradaAvisarCargaVinculos
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return fmt.Errorf("notificaciones: payload de %s: %w", e.Evento, err)
		}
		in.EventoID = e.ID
		_, err := m.AvisarCargaVinculos.Ejecutar(nucleo.ConBD(ctx, bd), in)
		return err
	})
}

// Aviso de clave reutilizada (contrato 0.14.0 §3.1 y §3.10): Argos respondió
// idempotency_key_reutilizada a una acción de Iris (tiene otro cuerpo con su clave). La acción quedó
// sin hacer para que decida una persona; en un cambio o un intento de recuperación es además una
// alerta de seguridad. Uno por pendiente: repetir el evento no avisa dos veces.

// TipoAvisoArgosClaveReutilizada: Argos tiene otro cuerpo con la clave de una acción de Iris.
const TipoAvisoArgosClaveReutilizada = "argos_clave_reutilizada"

// TituloArgosClaveReutilizada es el texto del aviso.
const TituloArgosClaveReutilizada = "Argos tiene otra acción con la misma clave"

// EntradaAvisarClaveReutilizada es el payload de integracion.pendiente_clave_reutilizada.
type EntradaAvisarClaveReutilizada struct {
	EventoID    uuid.UUID `json:"-"`
	PendienteID int64     `json:"pendiente_id"`
	Operacion   string    `json:"operacion"`
	Entidad     string    `json:"entidad"`
	EntidadID   string    `json:"entidad_id"`
	EmpresaID   *int64    `json:"empresa_id"`
	Seguridad   bool      `json:"seguridad"`
}

var reCodigo = regexp.MustCompile(`^[a-z_]{1,40}$`)

func (m *Modulo) nuevoAvisarClaveReutilizada() *auditoria.Caso[EntradaAvisarClaveReutilizada, ResultadoAvisarTope] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAvisarClaveReutilizada, ResultadoAvisarTope]{
		Accion: "avisos.argos_clave_reutilizada", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoAvisarTope) bool { return r.Avisos > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAvisarClaveReutilizada) (ResultadoAvisarTope, error) {
			r := ResultadoAvisarTope{}
			if err := soloSistema(ctx); err != nil {
				return r, err
			}
			if in.PendienteID <= 0 || in.EventoID == uuid.Nil || !reCodigo.MatchString(in.Operacion) || !reCodigo.MatchString(in.Entidad) {
				return r, nil
			}
			t.FijarEmpresa(0)
			t.FijarEntidad("pendiente", in.PendienteID)
			q := notificacionesbd.New(t)
			us, err := q.DestinatariosInstalacion(ctx, string(nucleo.PermIntegracionesConectar))
			if err != nil {
				return r, err
			}
			clave := fmt.Sprintf("%s:%d", TipoAvisoArgosClaveReutilizada, in.PendienteID)
			datos, err := json.Marshal(map[string]any{"pendiente_id": in.PendienteID, "operacion": in.Operacion, "entidad": in.Entidad,
				"entidad_id": in.EntidadID, "empresa_id": in.EmpresaID, "seguridad": in.Seguridad})
			if err != nil {
				return r, err
			}
			nivel := "alerta"
			if in.Seguridad {
				nivel = "peligro"
			}
			enlace := EnlaceEstadoArgos
			usuarios := []int64{}
			for _, u := range us {
				_, err := q.InsertarAviso(ctx, notificacionesbd.InsertarAvisoParams{UsuarioID: u.ID, Tipo: TipoAvisoArgosClaveReutilizada, Clave: clave,
					Nivel: nivel, Titulo: TituloArgosClaveReutilizada, Enlace: &enlace, Datos: datos, EventoID: pgtype.UUID{Bytes: in.EventoID, Valid: true}})
				if esSinFilas(err) {
					continue // ya avisado de este pendiente
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
			t.Anotar(map[string]any{"clave": clave, "avisos": r.Avisos, "seguridad": in.Seguridad})
			return r, t.EmitirEnEmpresa("notificaciones.avisos_creados", nil, map[string]any{"tipo": TipoAvisoArgosClaveReutilizada, "clave": clave,
				"empresa_id": nil, "usuarios": usuarios, "correo": "omitido"})
		},
	})
}

// Aviso de carga de vínculos detenida (contrato 0.14.0 §6.6): algún tecnico.argos_vinculado de la
// carga inicial agotó sus reintentos, así que la carga no se cierra y Argos sigue respondiendo
// vinculos_en_carga. Uno por episodio (clave con la carga y el momento del aviso).

// TipoAvisoArgosCargaVinculos: la carga inicial de vínculos está detenida.
const TipoAvisoArgosCargaVinculos = "argos_carga_vinculos"

// TituloArgosCargaVinculos es el texto del aviso.
const TituloArgosCargaVinculos = "La carga de vínculos con Argos está detenida"

// EntradaAvisarCargaVinculos es el payload de argos.carga_vinculos_detenida.
type EntradaAvisarCargaVinculos struct {
	EventoID   uuid.UUID `json:"-"`
	ConectorID int64     `json:"conector_id"`
	CargaID    uuid.UUID `json:"carga_id"`
	Agotados   int64     `json:"agotados"`
	Episodio   int64     `json:"episodio"`
}

func (m *Modulo) nuevoAvisarCargaVinculos() *auditoria.Caso[EntradaAvisarCargaVinculos, ResultadoAvisarTope] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAvisarCargaVinculos, ResultadoAvisarTope]{
		Accion: "avisos.argos_carga_vinculos", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoAvisarTope) bool { return r.Avisos > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAvisarCargaVinculos) (ResultadoAvisarTope, error) {
			r := ResultadoAvisarTope{}
			if err := soloSistema(ctx); err != nil {
				return r, err
			}
			if in.ConectorID <= 0 || in.EventoID == uuid.Nil || in.CargaID == uuid.Nil {
				return r, nil
			}
			t.FijarEmpresa(0)
			t.FijarEntidad("conector", in.ConectorID)
			q := notificacionesbd.New(t)
			us, err := q.DestinatariosInstalacion(ctx, string(nucleo.PermIntegracionesConectar))
			if err != nil {
				return r, err
			}
			clave := fmt.Sprintf("%s:%s:%d", TipoAvisoArgosCargaVinculos, in.CargaID, in.Episodio)
			datos, err := json.Marshal(map[string]any{"conector_id": in.ConectorID, "agotados": in.Agotados})
			if err != nil {
				return r, err
			}
			enlace := EnlaceEstadoArgos
			usuarios := []int64{}
			for _, u := range us {
				_, err := q.InsertarAviso(ctx, notificacionesbd.InsertarAvisoParams{UsuarioID: u.ID, Tipo: TipoAvisoArgosCargaVinculos, Clave: clave,
					Nivel: "peligro", Titulo: TituloArgosCargaVinculos, Enlace: &enlace, Datos: datos, EventoID: pgtype.UUID{Bytes: in.EventoID, Valid: true}})
				if esSinFilas(err) {
					continue
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
			return r, t.EmitirEnEmpresa("notificaciones.avisos_creados", nil, map[string]any{"tipo": TipoAvisoArgosCargaVinculos, "clave": clave,
				"empresa_id": nil, "usuarios": usuarios, "correo": "omitido"})
		},
	})
}
