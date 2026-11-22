package mensajeria

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Plantillas de WhatsApp para Argos (contrato Iris ↔ ArgosISP 0.13.0 §6.7, pedido B32). Las plantillas
// se editan y se aprueban solo en Iris; Argos las lee para saber qué avisos puede pedir. La ruta vive
// fuera de /api/v1/mensajeria (es del conector de Argos): la API la reenvía a este proceso como un
// delegado más (RutaPlantillasArgos).

// RutaPlantillasArgos es la ruta de §6.7. La API la reenvía tal cual al proceso de mensajería.
const RutaPlantillasArgos = "/api/v1/integraciones/argosisp/plantillas-whatsapp"

// EntradaPlantillasArgos: la empresa de la que Argos pregunta (debe estar en el alcance del conector).
type EntradaPlantillasArgos struct {
	EmpresaID int64 `query:"empresa_id" required:"true" minimum:"1" doc:"Empresa de Argos (del alcance del conector; si no, 403 empresa_no_permitida)"`
	nucleo.ParamsPagina
}

// PlantillaArgos es lo que ve Argos de una plantilla: nunca el id de la cuenta (WABA), el id de Meta ni el
// motivo de un rechazo; solo su estado.
type PlantillaArgos struct {
	Nombre       string   `json:"nombre" doc:"Nombre de la plantilla en Meta"`
	Idioma       string   `json:"idioma" doc:"Idioma en Meta (es, es_MX…)"`
	Categoria    string   `json:"categoria" enum:"UTILITY,MARKETING,AUTHENTICATION"`
	Estado       string   `json:"estado" doc:"Estado en Meta (APPROVED, PENDING, REJECTED, PAUSED, DISABLED, FLAGGED…). Un aviso sale solo con APPROVED o FLAGGED y sin pausa"`
	CodigosAviso []string `json:"codigos_aviso" doc:"Códigos de §6.3 que atiende (factura_por_vencer…)"`
	VistaPrevia  string   `json:"vista_previa" doc:"Texto del mensaje con las variables como {{nombre}}"`
}

// ListaPlantillasArgos es { data, nextCursor }.
type ListaPlantillasArgos = nucleo.Pagina[PlantillaArgos]

type salidaPlantillasArgos struct {
	CacheControl string `header:"Cache-Control"`
	Body         ListaPlantillasArgos
}

func (m *Modulo) nuevoPlantillasArgos() *auditoria.Caso[EntradaPlantillasArgos, ListaPlantillasArgos] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPlantillasArgos, ListaPlantillasArgos]{
		Accion: "plantillas_whatsapp.leer_integracion", Permiso: nucleo.PermPlantillasWhatsAppLeerIntegracion, SinEmpresa: true,
		AuditarExito: func(ListaPlantillasArgos) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPlantillasArgos) (ListaPlantillasArgos, error) {
			actor := contexto.ActorDe(ctx)
			soloArgos := nucleo.NuevoProblema(http.StatusForbidden, "solo_integracion_argos", "Solo la integración de Argos",
				"Esta ruta la usa solo el token de integración de Argos")
			if actor.Tipo != contexto.ActorIntegracion {
				return ListaPlantillasArgos{}, soloArgos
			}
			// Otra integración (Horus) no la usa aunque tenga el permiso.
			if ok, err := mensajeriabd.New(t).EsIntegracionArgos(ctx, actor.ID); err != nil || !ok {
				if err == nil {
					err = soloArgos
				}
				return ListaPlantillasArgos{}, err
			}
			todas, empresas, err := empresasDelActor(ctx, t)
			if err != nil {
				return ListaPlantillasArgos{}, err
			}
			if !todas && !slices.Contains(empresas, in.EmpresaID) {
				return ListaPlantillasArgos{}, nucleo.EmpresaNoPermitida()
			}
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return ListaPlantillasArgos{}, err
			}
			limite := in.Limite()
			fs, err := mensajeriabd.New(t).PlantillasWAParaArgos(ctx, mensajeriabd.PlantillasWAParaArgosParams{Empresa: in.EmpresaID, Despues: despues,
				Limite: int32(limite + 1)}) //nolint:gosec // limite ≤ 200
			if err != nil {
				return ListaPlantillasArgos{}, err
			}
			return nucleo.Paginar(fs, limite, plantillaArgos, func(f mensajeriabd.PlantillasWAParaArgosRow) []any { return []any{f.ID} }), nil
		},
	})
}

// plantillaArgos arma lo que ve Argos: solo los códigos del catálogo de §6.3 y el cuerpo del mensaje
// (sin encabezado, botones ni ejemplos).
func plantillaArgos(f mensajeriabd.PlantillasWAParaArgosRow) PlantillaArgos {
	var d DefinicionPWA
	_ = json.Unmarshal(f.Componentes, &d)
	codigos := []string{}
	for _, c := range f.Codigos {
		if _, ok := PlantillaPorCodigo(c); ok && !slices.Contains(codigos, c) {
			codigos = append(codigos, c)
		}
	}
	return PlantillaArgos{Nombre: f.Nombre, Idioma: f.Idioma, Categoria: f.Categoria, Estado: f.Estado, CodigosAviso: codigos, VistaPrevia: d.Cuerpo}
}

// registrarPlantillasArgos publica GET RutaPlantillasArgos (§6.7).
func (m *Modulo) registrarPlantillasArgos(api huma.API) {
	op := conector.Operacion(Etiqueta, "listarPlantillasWhatsAppArgos", http.MethodGet, RutaPlantillasArgos,
		"Plantillas de WhatsApp que puede usar una empresa (contrato Iris ↔ ArgosISP 0.13.0 §6.7)",
		nucleo.PermPlantillasWhatsAppLeerIntegracion, http.StatusOK)
	op.Description += " Solo el token de integración de Argos (otro actor: 403 `solo_integracion_argos`) y solo empresas de su alcance " +
		"(403 `empresa_no_permitida`). Paginada por cursor (`cursor`, `limit`; `{ data, nextCursor }`) y con `Cache-Control: no-store`. Devuelve las plantillas con el uso «avisos» de las cuentas de WhatsApp de la empresa (con un número suyo activo y conectado en esa cuenta) y, de un número " +
		"prestado, solo si el préstamo la autoriza a mandar avisos. Sin borradores ni borradas; las de marketing solo con marketing activado. " +
		"Nunca el id de la cuenta (WABA), el id de Meta ni el motivo de un rechazo. Las plantillas se editan y se aprueban solo en Iris."
	huma.Register(api, op, func(ctx context.Context, in *EntradaPlantillasArgos) (*salidaPlantillasArgos, error) {
		l, err := m.PlantillasArgos.Ejecutar(ctx, *in)
		if err != nil {
			return nil, err
		}
		return &salidaPlantillasArgos{CacheControl: "no-store", Body: l}, nil
	})
}
