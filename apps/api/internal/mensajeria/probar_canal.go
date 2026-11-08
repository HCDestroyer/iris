package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Probar la conexión de un número (contratos/conversaciones.md §1.1): POST /canales/{id}/probar
// llama a la Graph API GET /{phone_number_id}?fields=display_phone_number,verified_name,quality_rating
// con el token guardado. La llamada a Meta va fuera de la transacción: un caso lee el número y sus
// credenciales, se consulta a Meta y otro caso guarda el resultado, audita y emite
// canal_mensajeria.probado. El token nunca sale en la respuesta, el detalle, la auditoría ni el log.

// EntradaProbarCanal es POST /canales/{id}/probar (sin cuerpo).
type EntradaProbarCanal struct {
	ID int64 `json:"canal_id"`
}

// PruebaCanal es el resultado de una prueba.
type PruebaCanal struct {
	CanalID             int64      `json:"canalId"`
	Estado              string     `json:"estado" enum:"conectado,token_invalido,numero_no_coincide,sin_permiso,error_meta"`
	Detalle             *string    `json:"detalle" nullable:"true" doc:"Qué pasó, en español (null si conectado). Nunca lleva credenciales"`
	ProbadoEn           time.Time  `json:"probadoEn"`
	NumeroMeta          *string    `json:"numeroMeta" nullable:"true" doc:"display_phone_number de Meta en E.164, cuando se leyó"`
	NombreVerificado    *string    `json:"nombreVerificado" nullable:"true" doc:"verified_name de Meta"`
	Calidad             *string    `json:"calidad" nullable:"true" enum:"alta,media,baja,desconocida" doc:"quality_rating de Meta: GREEN alta, YELLOW media, RED baja"`
	WebhookVerificado   bool       `json:"webhookVerificado" doc:"Meta ya verificó el webhook (webhookVerificadoEn no es null)"`
	WebhookVerificadoEn *time.Time `json:"webhookVerificadoEn" nullable:"true"`
	UltimoMensajeEn     *time.Time `json:"ultimoMensajeEn" nullable:"true"`
	Conexion            string     `json:"conexion" enum:"desactivado,sin_probar,conectado,webhook_sin_verificar,token_invalido,numero_no_coincide,sin_permiso,error_meta" doc:"Lo mismo que Canal.conexion después de la prueba"`
}

// ProbarCanal es el caso de uso completo (dos transacciones y la llamada a Meta en medio).
type ProbarCanal struct {
	m       *Modulo
	leer    *auditoria.Caso[EntradaProbarCanal, canalAProbar]
	guardar *auditoria.Caso[resultadoPrueba, PruebaCanal]
}

// canalAProbar: lo que hace falta para llamar a Meta (nunca se audita la salida).
type canalAProbar struct {
	id            int64
	version       int32
	phoneNumberID string
	numero        string
	token         string
}

// resultadoPrueba: lo que se guarda (y se audita: sin credenciales).
type resultadoPrueba struct {
	CanalID          int64   `json:"canal_id"`
	Version          int32   `json:"version"`
	Estado           string  `json:"estado"`
	Detalle          *string `json:"detalle"`
	NumeroMeta       *string `json:"numero_meta"`
	NombreVerificado *string `json:"-"`
	Calidad          *string `json:"calidad"`
}

func nuevoProbarCanal(m *Modulo) *ProbarCanal {
	p := &ProbarCanal{m: m}
	p.leer = auditoria.Nuevo(auditoria.Caso[EntradaProbarCanal, canalAProbar]{
		Accion: "mensajeria.probar_canal", Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true,
		AuditarExito: func(canalAProbar) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaProbarCanal) (canalAProbar, error) {
			f, err := m.canalPersona(ctx, t, in.ID)
			if err != nil {
				return canalAProbar{}, err
			}
			cr, err := m.credencialesDe(f)
			if err != nil {
				return canalAProbar{}, err
			}
			return canalAProbar{id: f.ID, version: f.Version, phoneNumberID: f.PhoneNumberID, numero: f.Numero, token: cr.Token}, nil
		},
	})
	p.guardar = auditoria.Nuevo(auditoria.Caso[resultadoPrueba, PruebaCanal]{
		Accion: "mensajeria.probar_canal", Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in resultadoPrueba) (PruebaCanal, error) {
			if err := soloPersonas(ctx); err != nil {
				return PruebaCanal{}, err
			}
			antes, err := m.canalParaActualizar(ctx, t, in.CanalID)
			if err != nil {
				return PruebaCanal{}, err
			}
			// Si el número se editó mientras se consultaba a Meta, el resultado es de otros datos.
			if antes.Version != in.Version {
				return PruebaCanal{}, nucleo.Conflicto("conflicto_version", "El número cambió mientras se probaba; vuelve a probar")
			}
			ahora := m.ahora()
			f, err := mensajeriabd.New(t).RegistrarPruebaCanal(ctx, mensajeriabd.RegistrarPruebaCanalParams{ID: in.CanalID, ProbadoEn: &ahora,
				Estado: &in.Estado, Detalle: in.Detalle})
			if err != nil {
				return PruebaCanal{}, err
			}
			t.FijarEntidad("canal_mensajeria", f.ID)
			fijarEmpresaCanal(t, f.EmpresaID)
			out := PruebaCanal{CanalID: f.ID, Estado: in.Estado, Detalle: in.Detalle, ProbadoEn: ahora, NumeroMeta: in.NumeroMeta,
				NombreVerificado: in.NombreVerificado, Calidad: in.Calidad, WebhookVerificado: f.WebhookVerificadoEn != nil,
				WebhookVerificadoEn: f.WebhookVerificadoEn, UltimoMensajeEn: f.UltimoMensajeEn, Conexion: conexionDe(f)}
			return out, t.Emitir("canal_mensajeria.probado", map[string]any{"canal_id": f.ID, "empresa_id": f.EmpresaID, "estado": in.Estado})
		},
	})
	return p
}

// Ejecutar prueba el número: lee sus credenciales, consulta a Meta y guarda el resultado.
func (p *ProbarCanal) Ejecutar(ctx context.Context, in EntradaProbarCanal) (PruebaCanal, error) {
	c, err := p.leer.Ejecutar(ctx, in)
	if err != nil {
		return PruebaCanal{}, err
	}
	r := p.m.consultarNumeroMeta(ctx, c)
	r.CanalID, r.Version = c.id, c.version
	return p.guardar.Ejecutar(ctx, r)
}

// soloPersonas: como la bandeja y «configurar», solo una persona con sesión del panel (un token de
// API o una integración reciben 404).
func soloPersonas(ctx context.Context) error {
	if actor := contexto.ActorDe(ctx); actor.Tipo != contexto.ActorUsuario || actor.ID == 0 {
		return nucleo.NoEncontrado("El número no existe")
	}
	return nil
}

// canalPersona: el número (sin bloquear la fila) si quien pide es una persona y es de su empresa.
func (m *Modulo) canalPersona(ctx context.Context, t *auditoria.Tx, id int64) (mensajeriabd.MensajeriaCanal, error) {
	if err := soloPersonas(ctx); err != nil {
		return mensajeriabd.MensajeriaCanal{}, err
	}
	f, err := mensajeriabd.New(t).CanalPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, nucleo.NoEncontrado("El número no existe")
	}
	if err != nil {
		return f, err
	}
	if err := exigirEmpresaCanal(ctx, t, f.EmpresaID); err != nil {
		var pr *nucleo.Problema
		if errors.As(err, &pr) && pr.Status == http.StatusForbidden {
			return f, nucleo.NoEncontrado("El número no existe")
		}
		return f, err
	}
	return f, nil
}

// numeroMeta es la respuesta de GET /{phone_number_id}.
type numeroMeta struct {
	ID                 string     `json:"id"`
	DisplayPhoneNumber string     `json:"display_phone_number"`
	VerifiedName       string     `json:"verified_name"`
	QualityRating      string     `json:"quality_rating"`
	Error              *metaError `json:"error"`
}

// consultarNumeroMeta llama a la Graph API con el mismo host, cliente y tiempo que los envíos.
func (m *Modulo) consultarNumeroMeta(ctx context.Context, c canalAProbar) resultadoPrueba {
	ctx, cancelar := context.WithTimeout(ctx, 15*time.Second)
	defer cancelar()
	destino := m.cfg.MetaURL + "/" + url.PathEscape(c.phoneNumberID) + "?fields=display_phone_number,verified_name,quality_rating"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, destino, nil)
	if err != nil {
		return resultadoError(PruebaErrorMeta, "No se pudo armar la consulta a Meta")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "Iris/"+m.cfg.Version)
	resp, err := m.cfg.HTTPMeta.Do(req)
	if err != nil {
		// Sin el texto del error: podría llevar la URL.
		return resultadoError(PruebaErrorMeta, "Meta no respondió (tiempo agotado o sin conexión); vuelve a probar en unos minutos")
	}
	defer func() { _ = resp.Body.Close() }()
	leido, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	r := clasificarPrueba(resp.StatusCode, leido, c.phoneNumberID, c.numero)
	if r.Detalle != nil && c.token != "" {
		// Defensa extra: nada de lo que se arma aquí copia texto de Meta, pero el detalle nunca
		// debe llevar el token.
		d := strings.ReplaceAll(*r.Detalle, c.token, "***")
		r.Detalle = &d
	}
	return r
}

func resultadoError(estado, detalle string) resultadoPrueba {
	d := recortar(detalle, 300)
	return resultadoPrueba{Estado: estado, Detalle: &d}
}

// clasificarPrueba traduce la respuesta de Meta a un estado. El detalle se arma solo con el status
// y los códigos numéricos: el `message` de Meta puede repetir el token (p. ej. «Malformed access
// token EAAB…») y nunca se copia.
func clasificarPrueba(status int, cuerpo []byte, phoneNumberID, numero string) resultadoPrueba {
	var r numeroMeta
	legible := json.Unmarshal(cuerpo, &r) == nil
	if status/100 == 2 {
		if !legible {
			return resultadoError(PruebaErrorMeta, fmt.Sprintf("Meta respondió %d con un cuerpo que Iris no entiende", status))
		}
		out := resultadoPrueba{Calidad: calidadMeta(r.QualityRating)}
		if r.VerifiedName != "" {
			v := recortar(r.VerifiedName, 120)
			out.NombreVerificado = &v
		}
		meta := soloDigitos(r.DisplayPhoneNumber)
		if meta != "" {
			n := "+" + meta
			out.NumeroMeta = &n
		}
		switch {
		case r.ID != "" && r.ID != phoneNumberID:
			d := "Meta devolvió otro phone number ID para esta consulta"
			out.Estado, out.Detalle = PruebaNumeroNoCoincide, &d
		case meta == "":
			d := "Meta no devolvió el número de este phone number ID"
			out.Estado, out.Detalle = PruebaErrorMeta, &d
		case meta != soloDigitos(numero):
			d := fmt.Sprintf("Este phone number ID es del número +%s en Meta, no de %s", meta, numero)
			out.Estado, out.Detalle = PruebaNumeroNoCoincide, &d
		default:
			out.Estado = PruebaConectado
		}
		return out
	}
	codigo, sub := 0, 0
	if legible && r.Error != nil {
		codigo, sub = r.Error.Code, r.Error.Subcode
	}
	switch {
	case status == http.StatusUnauthorized || codigo == 190:
		return resultadoError(PruebaTokenInvalido, fmt.Sprintf("Meta rechazó el token: vencido, revocado o mal copiado (HTTP %d, código %d)", status, codigo))
	case status == http.StatusForbidden || codigo == 10 || (codigo >= 200 && codigo <= 299):
		return resultadoError(PruebaSinPermiso, fmt.Sprintf("El token no tiene permiso sobre este número o su cuenta de WhatsApp Business (HTTP %d, código %d)", status, codigo))
	case codigo == 100 && sub == 33:
		return resultadoError(PruebaNumeroNoCoincide, fmt.Sprintf("Meta no encuentra el phone number ID %s con este token", phoneNumberID))
	case status == http.StatusTooManyRequests || status >= 500:
		return resultadoError(PruebaErrorMeta, fmt.Sprintf("Meta no está disponible ahora (HTTP %d, código %d); vuelve a probar en unos minutos", status, codigo))
	}
	return resultadoError(PruebaErrorMeta, fmt.Sprintf("Meta respondió con un error (HTTP %d, código %d, subcódigo %d)", status, codigo, sub))
}

func calidadMeta(q string) *string {
	if q == "" {
		return nil
	}
	c := map[string]string{"GREEN": "alta", "YELLOW": "media", "RED": "baja"}[strings.ToUpper(q)]
	if c == "" {
		c = "desconocida"
	}
	return &c
}

// EntradaVerCanal es GET /canales/{id}.
type EntradaVerCanal struct {
	ID int64 `json:"canal_id"`
}

func nuevoVerCanal(m *Modulo) *auditoria.Caso[EntradaVerCanal, Canal] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerCanal, Canal]{
		Accion: "mensajeria.ver_canales", Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true,
		AuditarExito: func(Canal) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerCanal) (Canal, error) {
			f, err := mensajeriabd.New(t).CanalPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return Canal{}, nucleo.NoEncontrado("El canal no existe")
			}
			if err != nil {
				return Canal{}, err
			}
			if err := exigirEmpresaCanal(ctx, t, f.EmpresaID); err != nil {
				var pr *nucleo.Problema
				if errors.As(err, &pr) && pr.Status == http.StatusForbidden {
					return Canal{}, nucleo.NoEncontrado("El canal no existe")
				}
				return Canal{}, err
			}
			return m.aCanal(f), nil
		},
	})
}
