package soporte

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// Umbrales de caídas repetidas y de posible falla general por empresa (contrato con Argos 0.13.0
// §3.11, B29): GET /tickets/ajustes-caidas (tickets.ver) y POST (tickets.configurar_caidas, con
// version; 0 = de fábrica). Solo con una empresa activa: son de la empresa.

func empresaDeAjustes(ctx context.Context) (int64, error) {
	if e := empresaActiva(ctx); e != nil {
		return *e, nil
	}
	return 0, nucleo.EmpresaRequerida()
}

func (m *Modulo) nuevoVerAjustesRed() *auditoria.Caso[struct{}, AjustesRed] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, AjustesRed]{
		Accion: "tickets.ver_ajustes_caidas", Permiso: nucleo.PermTicketsVer, AuditarExito: func(AjustesRed) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (AjustesRed, error) {
			empresa, err := empresaDeAjustes(ctx)
			if err != nil {
				return AjustesRed{}, err
			}
			return ajustesRed(ctx, soportebd.New(t), empresa)
		},
	})
}

func (m *Modulo) nuevoGuardarAjustesRed() *auditoria.Caso[AjustesRed, AjustesRed] {
	return auditoria.Nuevo(auditoria.Caso[AjustesRed, AjustesRed]{
		Accion: "tickets.configurar_caidas", Permiso: nucleo.PermTicketsConfigurarCaidas,
		Logica: func(ctx context.Context, t *auditoria.Tx, in AjustesRed) (AjustesRed, error) {
			if !contexto.PermisosDe(ctx).TieneGlobal(string(nucleo.PermTicketsConfigurarCaidas)) {
				return AjustesRed{}, nucleo.Prohibido("Cambiar los umbrales exige el permiso sin límite de zona ni cuadrilla")
			}
			if in.CaidasUmbral < 1 || in.CaidasUmbral > 100 || in.CaidasVentanaMin < 5 || in.CaidasVentanaMin > 1440 || in.FallaUmbral < 2 || in.FallaUmbral > 1000 ||
				in.VerificacionHoras < 1 || in.VerificacionHoras > 168 {
				return AjustesRed{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "caidasUmbral", Message: "Fuera de los límites", Rule: "rango"})
			}
			empresa, err := empresaDeAjustes(ctx)
			if err != nil {
				return AjustesRed{}, err
			}
			t.FijarEmpresa(empresa)
			q := soportebd.New(t)
			antes, err := ajustesRed(ctx, q, empresa)
			if err != nil {
				return AjustesRed{}, err
			}
			if antes.Version != in.Version {
				return AjustesRed{}, nucleo.ConflictoVersion("ajustes_caidas", empresa)
			}
			var usuario *int64
			if a := contexto.ActorDe(ctx); a.EsUsuario() {
				usuario = &a.ID
			}
			var f soportebd.SoporteRedAjuste
			if in.Version == 0 {
				f, err = q.GuardarAjustesRed(ctx, soportebd.GuardarAjustesRedParams{EmpresaID: empresa, CaidasUmbral: in.CaidasUmbral,
					CaidasVentanaMin: in.CaidasVentanaMin, FallaUmbral: in.FallaUmbral, VerificacionHoras: in.VerificacionHoras, ActualizadoPor: usuario, Version: 0})
			} else {
				f, err = q.ActualizarAjustesRed(ctx, soportebd.ActualizarAjustesRedParams{EmpresaID: empresa, CaidasUmbral: in.CaidasUmbral,
					CaidasVentanaMin: in.CaidasVentanaMin, FallaUmbral: in.FallaUmbral, VerificacionHoras: in.VerificacionHoras, ActualizadoPor: usuario, Version: in.Version})
			}
			if errors.Is(err, pgx.ErrNoRows) {
				return AjustesRed{}, nucleo.ConflictoVersion("ajustes_caidas", empresa)
			}
			if err != nil {
				return AjustesRed{}, err
			}
			despues := AjustesRed{CaidasUmbral: f.CaidasUmbral, CaidasVentanaMin: f.CaidasVentanaMin, FallaUmbral: f.FallaUmbral,
				VerificacionHoras: f.VerificacionHoras, Version: f.Version}
			t.RegistrarCambios("ajustes_caidas", empresa, antes, despues)
			campos := []string{}
			if antes.CaidasUmbral != despues.CaidasUmbral {
				campos = append(campos, "caidas_umbral")
			}
			if antes.CaidasVentanaMin != despues.CaidasVentanaMin {
				campos = append(campos, "caidas_ventana_min")
			}
			if antes.FallaUmbral != despues.FallaUmbral {
				campos = append(campos, "falla_umbral")
			}
			if antes.VerificacionHoras != despues.VerificacionHoras {
				campos = append(campos, "verificacion_horas")
			}
			return despues, t.Emitir("soporte.caidas_ajustes_cambiados", map[string]any{"empresa_id": empresa, "campos": campos,
				"caidas_umbral": despues.CaidasUmbral, "caidas_ventana_min": despues.CaidasVentanaMin, "falla_umbral": despues.FallaUmbral, "verificacion_horas": despues.VerificacionHoras})
		},
	})
}

func (m *Modulo) registrarAjustesRed(api huma.API) {
	ver := operacion("verAjustesCaidas", http.MethodGet, "/api/v1/tickets/ajustes-caidas", "Umbrales de caídas repetidas y de posible falla general",
		nucleo.PermTicketsVer, http.StatusOK)
	ver.Description += " De la empresa activa (contrato con Argos 0.13.0 §3.11). `version` 0 = los de fábrica: 3 caídas en 60 min, 20 tickets de caídas en 10 min y 24 h de espera de la respuesta del cliente tras una falla general."
	huma.Register(api, ver, func(ctx context.Context, _ *struct{}) (*cuerpo[AjustesRed], error) {
		return responder(m.VerAjustesRed.Ejecutar(ctx, struct{}{}))
	})
	guardar := operacion("guardarAjustesCaidas", http.MethodPost, "/api/v1/tickets/ajustes-caidas", "Cambiar los umbrales de caídas repetidas",
		nucleo.PermTicketsConfigurarCaidas, http.StatusOK, http.StatusConflict)
	guardar.Description += " Con el permiso sin límite de zona. `version` la leída (0 = de fábrica); si cambió, 409. Emite `soporte.caidas_ajustes_cambiados`."
	huma.Register(api, guardar, func(ctx context.Context, in *conClave[AjustesRed]) (*cuerpo[AjustesRed], error) {
		return responder(m.GuardarAjustesRed.Ejecutar(ctx, in.Body))
	})
}
