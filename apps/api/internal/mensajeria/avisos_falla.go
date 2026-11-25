package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Aviso por WhatsApp de una falla general (contrato con Argos 0.13.0 §3.11, B30). Soporte decide a
// quién (los clientes de los tickets que absorbió el incidente, uno por cliente, incidente y tipo, y
// como máximo uno de cada tipo cada 6 h) y emite soporte.aviso_falla; aquí se pide el aviso como
// cualquier otro (EnviarAviso), con las reglas de §6.5: permiso de contacto, baja, límite diario,
// horario de silencio y plantilla aprobada fuera de la ventana. Sin servicio ni dirección.

// Plantillas del aviso de falla.
const (
	PlantillaFallaGeneral         = "falla_general"
	PlantillaFallaGeneralResuelta = "falla_general_resuelta"
)

// EntradaAvisoFalla es el payload de soporte.aviso_falla.
type EntradaAvisoFalla struct {
	Incidente int64  `json:"incidente"`
	EmpresaID int64  `json:"empresa_id"`
	ClienteID int64  `json:"cliente_id"`
	Tipo      string `json:"tipo"` // apertura | cierre
}

// AvisarFalla pide el aviso (actor sistema). Una clave por cliente, incidente y tipo: un evento repetido
// no manda otro.
func (m *Modulo) AvisarFalla(ctx context.Context, in EntradaAvisoFalla) error {
	if in.Incidente <= 0 || in.EmpresaID <= 0 || in.ClienteID <= 0 {
		return nil
	}
	plantilla := PlantillaFallaGeneral
	if in.Tipo == "cierre" {
		plantilla = PlantillaFallaGeneralResuelta
	}
	clave := fmt.Sprintf("iris:falla:%d:%d:%s", in.Incidente, in.ClienteID, in.Tipo)
	ya, err := mensajeriabd.New(nucleo.BDDe(ctx)).AvisoSistemaExiste(ctx, clave)
	if err != nil || ya {
		return err
	}
	_, err = m.EnviarAviso.Ejecutar(ctx, EntradaAviso{EmpresaID: in.EmpresaID, ClienteID: in.ClienteID, Plantilla: plantilla,
		CanalPreferido: CanalWhatsApp, Prioridad: "normal", IdempotencyKey: clave})
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Status < 500 {
		// Un cliente fuera de la empresa o una plantilla mal pedida no se arreglan reintentando.
		return nil
	}
	return err
}

// SuscribirAvisosFalla: el worker pide los avisos de falla (soporte.aviso_falla).
func (m *Modulo) SuscribirAvisosFalla(bus *integracion.Bus, bd nucleo.BD) {
	bus.Suscribir("mensajeria.avisos_falla", "soporte.aviso_falla", func(ctx context.Context, e integracion.Evento) error {
		var in EntradaAvisoFalla
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return nil
		}
		return m.AvisarFalla(ctxSistema(ctx, bd), in)
	})
}
