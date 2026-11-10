package argos

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Revisar la versión ya (diagnóstico del conector): el administrador ve «Argos < 1.6.0» o un
// versionEstado que no es ok, corrige la URL o el token en Argos y pide releerla sin esperar al
// periódico de cada hora. Encola integraciones.informar_contrato en la misma transacción.

// EntradaRevisarVersion es la entrada de integraciones.revisar_version.
type EntradaRevisarVersion struct {
	ID int64 `json:"id"`
}

func nuevoRevisarVersion(m *Modulo) *auditoria.Caso[EntradaRevisarVersion, Conector] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRevisarVersion, Conector]{
		Accion: "integraciones.revisar_version", Permiso: nucleo.PermIntegracionesRevisarVersion, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRevisarVersion) (Conector, error) {
			t.FijarEntidad("conector", in.ID)
			t.FijarEmpresa(0)
			f, err := conectorbd.New(t).ConectorPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return Conector{}, nucleo.NoEncontrado("No existe el conector")
			}
			if err != nil {
				return Conector{}, err
			}
			if f.Sistema != Sistema {
				return Conector{}, nucleo.ReglaNegocio("sistema_sin_version", "Solo los conectores con ArgosISP informan su versión")
			}
			if f.Estado != "activo" {
				return Conector{}, nucleo.ReglaNegocio("conector_apagado", "Activa el conector para revisar la versión de Argos")
			}
			if m.cfg.Encolar == nil {
				return Conector{}, nucleo.NuevoProblema(http.StatusServiceUnavailable, "cola_no_disponible", "Cola no disponible",
					"No se pudo encolar la revisión; vuelve a intentar en unos minutos")
			}
			if err := m.encolarInformarContrato(ctx, t.Tx, f.ID); err != nil {
				return Conector{}, err
			}
			return m.nucleo.AConector(f), nil
		},
	})
}

func (m *Modulo) registrarRevisarVersion(api huma.API) {
	op := conector.Operacion(conector.EtiquetaIntegraciones, "revisarVersionConector", http.MethodPost, "/api/v1/integraciones/conectores/{id}/revisar-version",
		"Releer ya la versión de Argos (diagnóstico del conector)", nucleo.PermIntegracionesRevisarVersion, http.StatusAccepted,
		http.StatusNotFound, http.StatusServiceUnavailable)
	op.Description += " Encola enseguida el job que informa el contrato (`PUT /integraciones/iris/contrato`) y lee la versión (`GET /integraciones/iris/estado`); " +
		"responde **202** con el conector como está ahora. El resultado llega en `versionEstado`, `versionDetalle` y `versionConsultadaEn` (el panel vuelve a leer el conector). " +
		"Acepta `Idempotency-Key`. 422 `conector_apagado` si no está activo y `sistema_sin_version` si no es de ArgosISP; 404 si no existe."
	huma.Register(api, op, func(ctx context.Context, in *struct {
		nucleo.ConIdempotencia
		ID int64 `path:"id" minimum:"1"`
	}) (*conector.Cuerpo[Conector], error) {
		return conector.Responder(m.RevisarVersion.Ejecutar(ctx, EntradaRevisarVersion{ID: in.ID}))
	})
}
