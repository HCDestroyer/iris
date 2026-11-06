package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Plantillas (GET /mensajeria/plantillas, mensajeria.ver) ----------------------------------------

// EntradaPlantillas filtra por empresa (opcional).
type EntradaPlantillas struct {
	EmpresaID int64 `json:"empresa_id,omitempty" query:"empresa_id" minimum:"0" doc:"Solo los canales de esta empresa (por defecto, todas las de la integración)"`
}

// PlantillaVigente es una plantilla del catálogo con dónde está aprobada.
type PlantillaVigente struct {
	Codigo    string     `json:"codigo"`
	Variables []Variable `json:"variables"`
	Texto     string     `json:"texto" doc:"Texto de referencia en español con {{variable}}"`
	Urgente   bool       `json:"admite_urgente" doc:"Admite prioridad urgente (solo la pide Iris)"`
	Pide      string     `json:"pide" enum:"argos,iris" doc:"Quién puede pedirla: argos (Argos e Iris) o iris (solo Iris; una integración recibe 422 plantilla_no_permitida)"`
	Canales   []string   `json:"canales" doc:"Medios en que está aprobada para las empresas consultadas (vacío = no aprobada: un aviso queda bloqueado con canal_no_disponible)"`
}

// ListaPlantillas es { data, nextCursor } (sin más páginas).
type ListaPlantillas struct {
	Data       []PlantillaVigente `json:"data"`
	NextCursor *string            `json:"nextCursor" nullable:"true"`
}

func nuevoListarPlantillas(m *Modulo) *auditoria.Caso[EntradaPlantillas, ListaPlantillas] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPlantillas, ListaPlantillas]{
		Accion: "mensajeria.ver", Permiso: nucleo.PermMensajeriaVer, SinEmpresa: true,
		AuditarExito: func(ListaPlantillas) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPlantillas) (ListaPlantillas, error) {
			todas, empresas, err := empresasDelActor(ctx, t)
			if err != nil {
				return ListaPlantillas{}, err
			}
			if in.EmpresaID > 0 {
				if !todas && !slices.Contains(empresas, in.EmpresaID) {
					return ListaPlantillas{}, nucleo.EmpresaNoPermitida()
				}
				todas, empresas = false, []int64{in.EmpresaID}
			}
			canales, err := mensajeriabd.New(t).CanalesActivosDeEmpresas(ctx, mensajeriabd.CanalesActivosDeEmpresasParams{Todas: todas, Empresas: empresas})
			if err != nil {
				return ListaPlantillas{}, err
			}
			aprobadas := map[string]map[string]bool{}
			for _, c := range canales {
				var p map[string]PlantillaMeta
				if json.Unmarshal(c.Plantillas, &p) != nil {
					continue
				}
				for codigo := range p {
					if aprobadas[codigo] == nil {
						aprobadas[codigo] = map[string]bool{}
					}
					aprobadas[codigo][c.Canal] = true
				}
			}
			out := ListaPlantillas{Data: make([]PlantillaVigente, 0, len(Catalogo))}
			for _, p := range Catalogo {
				medios := []string{}
				if m.cfg.Simulado {
					medios = append(medios, CanalWhatsApp)
				}
				for medio := range aprobadas[p.Codigo] {
					if !slices.Contains(medios, medio) {
						medios = append(medios, medio)
					}
				}
				sort.Strings(medios)
				out.Data = append(out.Data, PlantillaVigente{Codigo: p.Codigo, Variables: p.Variables, Texto: p.Texto, Urgente: p.Urgente, Pide: p.Pide, Canales: medios})
			}
			return out, nil
		},
	})
}

// empresasDelActor: todas = sin filtro (sistema o empresas.todas); si no, las del actor. Una
// empresa «0» representa el modo sin conector (canales con empresa_id null).
func empresasDelActor(ctx context.Context, q identidadbd.DBTX) (bool, []int64, error) {
	if !contexto.ActorDe(ctx).EsUsuario() {
		return true, nil, nil
	}
	e := contexto.EmpresasDe(ctx)
	if e.Estado == contexto.EmpresaNoResuelta {
		var err error
		if e, err = identidad.ResolverEmpresa(ctx, identidadbd.New(q), ""); err != nil {
			return false, nil, err
		}
	}
	if e.Estado == contexto.EmpresaSinConector {
		return false, []int64{0}, nil
	}
	if e.Todas {
		return true, nil, nil
	}
	return false, append([]int64{}, e.Disponibles...), nil
}

// Canales (mensajeria.configurar) ------------------------------------------------------------------

// Canal es un canal configurado (nunca sus credenciales).
type Canal struct {
	ID             int64                    `json:"id"`
	EmpresaID      *int64                   `json:"empresaId" nullable:"true"`
	Tipo           string                   `json:"tipo" enum:"whatsapp_cloud"`
	Canal          string                   `json:"canal" enum:"whatsapp"`
	Nombre         string                   `json:"nombre"`
	Numero         string                   `json:"numero" doc:"Número remitente en E.164"`
	PhoneNumberID  string                   `json:"phoneNumberId"`
	WabaID         string                   `json:"wabaId"`
	Plantillas     map[string]PlantillaMeta `json:"plantillas" doc:"Plantillas aprobadas en Meta por código de Iris"`
	Usos           []string                 `json:"usos" doc:"avisos, soporte, cobros, ventas (contratos/conversaciones.md §1)"`
	AvisosEmpresa  bool                     `json:"avisosEmpresa" doc:"Es el número por el que salen los avisos de la empresa (uno activo por empresa)"`
	ColaID         *int64                   `json:"colaId" nullable:"true" doc:"Cola de las conversaciones nuevas de este número"`
	HorarioID      *int64                   `json:"horarioId" nullable:"true" doc:"Horario del número"`
	FueraDeHorario FueraDeHorario           `json:"fueraDeHorario"`
	Activo         bool                     `json:"activo"`
	PausadoHasta   *time.Time               `json:"pausadoHasta" nullable:"true" doc:"Pausa automática por calidad o límite de Meta"`
	WebhookRuta    string                   `json:"webhookRuta" doc:"Ruta del webhook a configurar en la app de Meta (sobre IRIS_BASE_URL)"`
	Version        int32                    `json:"version"`
	CreadoEn       time.Time                `json:"creadoEn"`
}

// ListaCanales es { data, nextCursor }.
type ListaCanales struct {
	Data       []Canal `json:"data"`
	NextCursor *string `json:"nextCursor" nullable:"true"`
}

func aCanal(f mensajeriabd.MensajeriaCanal) Canal {
	p := map[string]PlantillaMeta{}
	_ = json.Unmarshal(f.Plantillas, &p)
	return Canal{ID: f.ID, EmpresaID: f.EmpresaID, Tipo: f.Tipo, Canal: f.Canal, Nombre: f.Nombre, Numero: f.Numero, PhoneNumberID: f.PhoneNumberID,
		WabaID: f.WabaID, Plantillas: p, Usos: f.Usos, AvisosEmpresa: f.AvisosEmpresa, ColaID: f.ColaID,
		HorarioID: f.HorarioID, FueraDeHorario: fueraDeHorarioDe(f.FueraHorario), Activo: f.Activo, PausadoHasta: f.PausadoHasta, WebhookRuta: rutaWebhookMeta(f.ID), Version: f.Version, CreadoEn: f.CreatedAt}
}

func rutaWebhookMeta(id int64) string {
	return "/api/v1/mensajeria/canales/" + itoa(id) + "/webhook"
}

// canalAuditable: lo que queda en auditoria.cambios (las credenciales, como ***).
type canalAuditable struct {
	EmpresaID     *int64                   `json:"empresa_id"`
	Nombre        string                   `json:"nombre"`
	Numero        string                   `json:"numero"`
	PhoneNumberID string                   `json:"phone_number_id"`
	WabaID        string                   `json:"waba_id"`
	Plantillas    map[string]PlantillaMeta `json:"plantillas"`
	Credenciales  string                   `json:"credenciales" auditoria:"sensible"`
	Activo        bool                     `json:"activo"`
}

func auditableCanal(c Canal, credenciales string) canalAuditable {
	return canalAuditable{EmpresaID: c.EmpresaID, Nombre: c.Nombre, Numero: c.Numero, PhoneNumberID: c.PhoneNumberID, WabaID: c.WabaID,
		Plantillas: c.Plantillas, Credenciales: credenciales, Activo: c.Activo}
}

// EntradaCrearCanal es POST /mensajeria/canales (WhatsApp Cloud API de la empresa).
type EntradaCrearCanal struct {
	EmpresaID     *int64                   `json:"empresaId,omitempty" minimum:"1" nullable:"true" doc:"Empresa de Argos (null solo en modo sin conector)"`
	Nombre        string                   `json:"nombre" minLength:"1" maxLength:"120"`
	Numero        string                   `json:"numero" pattern:"^\\+[1-9][0-9]{7,14}$" doc:"Número remitente en E.164"`
	PhoneNumberID string                   `json:"phoneNumberId" pattern:"^[0-9]{1,30}$" doc:"Phone number ID de Meta"`
	WabaID        string                   `json:"wabaId,omitempty" pattern:"^[0-9]{0,30}$" doc:"WhatsApp Business Account ID"`
	Token         string                   `json:"token" minLength:"20" maxLength:"1024" auditoria:"sensible" doc:"Token de acceso del usuario del sistema de Meta (solo escritura)"`
	AppSecret     string                   `json:"appSecret" minLength:"16" maxLength:"256" auditoria:"sensible" doc:"App secret de la app de Meta (verifica X-Hub-Signature-256; solo escritura)"`
	VerifyToken   string                   `json:"verifyToken" minLength:"16" maxLength:"256" auditoria:"sensible" doc:"Token de verificación del webhook que se escribe en Meta (solo escritura)"`
	Plantillas    map[string]PlantillaMeta `json:"plantillas,omitempty" doc:"Plantillas aprobadas en Meta por código de Iris (§6.3)"`
	Usos          []string                 `json:"usos,omitempty" maxItems:"4" uniqueItems:"true" doc:"avisos, soporte, cobros, ventas (por defecto avisos y soporte)"`
	AvisosEmpresa *bool                    `json:"avisosEmpresa,omitempty" doc:"Número de avisos de la empresa. Por defecto, sí cuando la empresa no tiene otro y usos incluye avisos"`
	ColaID        *int64                   `json:"colaId,omitempty" minimum:"1" nullable:"true" doc:"Cola de las conversaciones nuevas"`
}

// EntradaActualizarCanal es POST /mensajeria/canales/{id}/actualizar (las credenciales vacías no cambian).
type EntradaActualizarCanal struct {
	ID            int64                    `json:"-"`
	Version       int32                    `json:"version" minimum:"1"`
	Nombre        string                   `json:"nombre" minLength:"1" maxLength:"120"`
	Numero        string                   `json:"numero" pattern:"^\\+[1-9][0-9]{7,14}$"`
	PhoneNumberID string                   `json:"phoneNumberId" pattern:"^[0-9]{1,30}$"`
	WabaID        string                   `json:"wabaId,omitempty" pattern:"^[0-9]{0,30}$"`
	Token         string                   `json:"token,omitempty" maxLength:"1024" auditoria:"sensible" doc:"Vacío = no cambia"`
	AppSecret     string                   `json:"appSecret,omitempty" maxLength:"256" auditoria:"sensible" doc:"Vacío = no cambia"`
	VerifyToken   string                   `json:"verifyToken,omitempty" maxLength:"256" auditoria:"sensible" doc:"Vacío = no cambia"`
	Plantillas    map[string]PlantillaMeta `json:"plantillas"`
}

// EntradaCambioCanal activa o desactiva un canal.
type EntradaCambioCanal struct {
	ID int64 `json:"id"`
}

func validarPlantillasMeta(p map[string]PlantillaMeta) error {
	for codigo := range p {
		if _, ok := PlantillaPorCodigo(codigo); !ok {
			return nucleo.Validacion(nucleo.ErrorCampo{Field: "plantillas." + recortar(codigo, 64), Message: "No es una plantilla de Iris", Rule: "plantilla"})
		}
	}
	return nil
}

func (m *Modulo) cifrarCredenciales(id int64, c credenciales) ([]byte, error) {
	if m.cfg.Cifrador == nil {
		return nil, errSinCifrado()
	}
	plano, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	defer clear(plano)
	return m.cfg.Cifrador.Cifrar(plano, DatosAsociadosCanal(id))
}

func nuevoCrearCanal(m *Modulo) *auditoria.Caso[EntradaCrearCanal, Canal] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearCanal, Canal]{
		Accion: "mensajeria.configurar", Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearCanal) (Canal, error) {
			if err := validarPlantillasMeta(in.Plantillas); err != nil {
				return Canal{}, err
			}
			if err := exigirEmpresaCanal(ctx, t, in.EmpresaID); err != nil {
				return Canal{}, err
			}
			q := mensajeriabd.New(t)
			if len(in.Usos) == 0 {
				in.Usos = []string{"avisos", "soporte"}
			}
			empresa := int64(0)
			if in.EmpresaID != nil {
				empresa = *in.EmpresaID
			}
			if err := m.colaDeEmpresa(ctx, q, empresa, in.ColaID); err != nil {
				return Canal{}, err
			}
			avisos := false
			if in.AvisosEmpresa != nil {
				avisos = *in.AvisosEmpresa
			} else if slices.Contains(in.Usos, "avisos") {
				hay, err := q.HayCanalAvisos(ctx, mensajeriabd.HayCanalAvisosParams{EmpresaID: empresa, Excepto: 0})
				if err != nil {
					return Canal{}, err
				}
				avisos = !hay
			}
			if err := validarUsos(in.Usos, avisos); err != nil {
				return Canal{}, err
			}
			id, err := q.SiguienteIDCanal(ctx)
			if err != nil {
				return Canal{}, err
			}
			cifradas, err := m.cifrarCredenciales(id, credenciales{Token: in.Token, AppSecret: in.AppSecret, VerifyToken: in.VerifyToken})
			if err != nil {
				return Canal{}, err
			}
			if in.Plantillas == nil {
				in.Plantillas = map[string]PlantillaMeta{}
			}
			plantillas, _ := json.Marshal(in.Plantillas)
			f, err := q.InsertarCanal(ctx, mensajeriabd.InsertarCanalParams{ID: id, EmpresaID: in.EmpresaID, Tipo: TipoWhatsAppCloud, Canal: CanalWhatsApp,
				Nombre: in.Nombre, Numero: in.Numero, PhoneNumberID: in.PhoneNumberID, WabaID: in.WabaID, CredencialesCifradas: cifradas,
				Plantillas: plantillas, CreadoPor: usuarioDe(ctx), Usos: in.Usos, AvisosEmpresa: avisos, ColaID: in.ColaID})
			if err != nil {
				return Canal{}, conflictoCanal(err)
			}
			c := aCanal(f)
			fijarEmpresaCanal(t, f.EmpresaID)
			if avisos {
				if err := fijarAvisos(ctx, t, q, f.EmpresaID, f.ID); err != nil {
					return Canal{}, err
				}
			}
			t.RegistrarCambios("canal_mensajeria", f.ID, nil, auditableCanal(c, "***"))
			return c, t.Emitir("canal_mensajeria.creado", map[string]any{"canal_id": f.ID, "empresa_id": f.EmpresaID, "tipo": f.Tipo, "canal": f.Canal})
		},
	})
}

func fijarEmpresaCanal(t *auditoria.Tx, empresa *int64) {
	if empresa == nil {
		t.FijarEmpresa(0)
		return
	}
	t.FijarEmpresa(*empresa)
}

// exigirEmpresaCanal: la empresa del canal debe ser del administrador (null solo sin empresas registradas).
func exigirEmpresaCanal(ctx context.Context, q identidadbd.DBTX, empresa *int64) error {
	todas, empresas, err := empresasDelActor(ctx, q)
	if err != nil {
		return err
	}
	if empresa == nil {
		if todas || slices.Contains(empresas, 0) {
			hay, err := identidadbd.New(q).HayEmpresas(ctx)
			if err != nil || !hay {
				return err
			}
		}
		return nucleo.Validacion(nucleo.ErrorCampo{Field: "empresaId", Message: "Obligatoria con empresas registradas", Rule: "required"})
	}
	if todas || slices.Contains(empresas, *empresa) {
		return nil
	}
	return nucleo.EmpresaNoPermitida()
}

func (m *Modulo) canalParaActualizar(ctx context.Context, t *auditoria.Tx, id int64) (mensajeriabd.MensajeriaCanal, error) {
	f, err := mensajeriabd.New(t).CanalPorIDParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, nucleo.NoEncontrado("El canal no existe")
	}
	if err != nil {
		return f, err
	}
	if err := exigirEmpresaCanal(ctx, t, f.EmpresaID); err != nil {
		var p *nucleo.Problema
		if errors.As(err, &p) && p.Status == 403 {
			return f, nucleo.NoEncontrado("El canal no existe")
		}
		return f, err
	}
	return f, nil
}

func nuevoActualizarCanal(m *Modulo) *auditoria.Caso[EntradaActualizarCanal, Canal] {
	return auditoria.Nuevo(auditoria.Caso[EntradaActualizarCanal, Canal]{
		Accion: "mensajeria.configurar", Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaActualizarCanal) (Canal, error) {
			if err := validarPlantillasMeta(in.Plantillas); err != nil {
				return Canal{}, err
			}
			antes, err := m.canalParaActualizar(ctx, t, in.ID)
			if err != nil {
				return Canal{}, err
			}
			if antes.Version != in.Version {
				return Canal{}, nucleo.ConflictoVersion("canal", in.ID)
			}
			cr, err := m.credencialesDe(antes)
			if err != nil {
				return Canal{}, err
			}
			cambioCred := "sin_cambio" //nolint:gosec // marcador de auditoría, no una credencial
			for _, par := range []struct {
				nuevo   string
				destino *string
				minimo  int
			}{{in.Token, &cr.Token, 20}, {in.AppSecret, &cr.AppSecret, 16}, {in.VerifyToken, &cr.VerifyToken, 16}} {
				if par.nuevo == "" {
					continue
				}
				if len(par.nuevo) < par.minimo {
					return Canal{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "credenciales", Message: "Demasiado corta", Rule: "minLength"})
				}
				*par.destino, cambioCred = par.nuevo, "***"
			}
			cifradas, err := m.cifrarCredenciales(in.ID, cr)
			if err != nil {
				return Canal{}, err
			}
			if in.Plantillas == nil {
				in.Plantillas = map[string]PlantillaMeta{}
			}
			plantillas, _ := json.Marshal(in.Plantillas)
			f, err := mensajeriabd.New(t).ActualizarCanal(ctx, mensajeriabd.ActualizarCanalParams{ID: in.ID, Nombre: in.Nombre, Numero: in.Numero,
				PhoneNumberID: in.PhoneNumberID, WabaID: in.WabaID, CredencialesCifradas: cifradas, Plantillas: plantillas})
			if err != nil {
				return Canal{}, conflictoCanal(err)
			}
			c := aCanal(f)
			fijarEmpresaCanal(t, f.EmpresaID)
			campos := t.RegistrarCambios("canal_mensajeria", f.ID, auditableCanal(aCanal(antes), "sin_cambio"), auditableCanal(c, cambioCred))
			if campos == nil {
				campos = []string{}
			}
			return c, t.Emitir("canal_mensajeria.actualizado", map[string]any{"canal_id": f.ID, "empresa_id": f.EmpresaID, "campos": campos})
		},
	})
}

func nuevoCambioCanal(m *Modulo, accion string, activo bool) *auditoria.Caso[EntradaCambioCanal, Canal] {
	evento := "canal_mensajeria.desactivado"
	if activo {
		evento = "canal_mensajeria.activado"
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaCambioCanal, Canal]{
		Accion: accion, Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambioCanal) (Canal, error) {
			antes, err := m.canalParaActualizar(ctx, t, in.ID)
			if err != nil {
				return Canal{}, err
			}
			fijarEmpresaCanal(t, antes.EmpresaID)
			if antes.Activo == activo {
				return aCanal(antes), nil
			}
			f, err := mensajeriabd.New(t).FijarCanalActivo(ctx, mensajeriabd.FijarCanalActivoParams{ID: in.ID, Activo: activo})
			if err != nil {
				return Canal{}, conflictoCanal(err)
			}
			t.RegistrarCambios("canal_mensajeria", f.ID, map[string]any{"activo": antes.Activo}, map[string]any{"activo": f.Activo})
			return aCanal(f), t.Emitir(evento, map[string]any{"canal_id": f.ID, "empresa_id": f.EmpresaID})
		},
	})
}

func nuevoListarCanales(_ *Modulo) *auditoria.Caso[struct{}, ListaCanales] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ListaCanales]{
		Accion: "mensajeria.ver_canales", Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true,
		AuditarExito: func(ListaCanales) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ListaCanales, error) {
			todas, empresas, err := empresasDelActor(ctx, t)
			if err != nil {
				return ListaCanales{}, err
			}
			fs, err := mensajeriabd.New(t).ListarCanales(ctx, mensajeriabd.ListarCanalesParams{Todas: todas, Empresas: empresas})
			if err != nil {
				return ListaCanales{}, err
			}
			out := ListaCanales{Data: make([]Canal, 0, len(fs))}
			for _, f := range fs {
				out.Data = append(out.Data, aCanal(f))
			}
			return out, nil
		},
	})
}
