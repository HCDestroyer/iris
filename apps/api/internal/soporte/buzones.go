package soporte

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte/correo"
)

// Buzones de correo: cuentas IMAP (y opcionalmente SMTP) de las que entran tickets. Las
// contraseñas van cifradas con IRIS_CLAVE_CIFRADO (atadas al buzón) y nunca salen en respuestas,
// logs ni auditoría (en la auditoría quedan como «***» y solo se anota si cambiaron).

// ServidorIMAP del buzón (sin contraseña).
type ServidorIMAP struct {
	Host              string  `json:"host"`
	Puerto            int32   `json:"puerto"`
	TLS               string  `json:"tls" enum:"tls,starttls" doc:"tls = IMAPS (993); starttls = STARTTLS obligatorio (143). Nunca en claro"`
	Usuario           string  `json:"usuario"`
	Carpeta           string  `json:"carpeta"`
	CarpetaProcesados *string `json:"carpetaProcesados" nullable:"true" doc:"Carpeta a la que se mueve cada correo ya guardado; null = solo se marca leído"`
}

// ServidorSMTP del buzón (sin contraseña).
type ServidorSMTP struct {
	Host    string  `json:"host"`
	Puerto  int32   `json:"puerto"`
	TLS     string  `json:"tls" enum:"tls,starttls" doc:"tls = SMTPS (465); starttls = STARTTLS obligatorio (587). Nunca en claro"`
	Usuario *string `json:"usuario" nullable:"true" doc:"null = sin autenticación"`
}

// Buzon es un buzón de correo de soporte. Nunca trae contraseñas.
type Buzon struct {
	ID              int64         `json:"id"`
	EmpresaID       *int64        `json:"empresaId" nullable:"true" doc:"Empresa en Argos; null sin conector"`
	Nombre          string        `json:"nombre"`
	Correo          string        `json:"correo" doc:"Dirección del buzón (remitente de las respuestas)"`
	IMAP            ServidorIMAP  `json:"imap"`
	SMTP            *ServidorSMTP `json:"smtp,omitempty" doc:"Salida de respuestas por correo; ausente = sin salida"`
	CategoriaID     *int64        `json:"categoriaId" nullable:"true" doc:"Categoría de los tickets nuevos"`
	Prioridad       string        `json:"prioridad" enum:"baja,media,alta,critica" doc:"Prioridad de los tickets nuevos"`
	AuthservID      *string       `json:"authservId" nullable:"true" doc:"authserv-id de Authentication-Results del servidor de correo de confianza; null = ningún remitente queda verificado"`
	Activo          bool          `json:"activo"`
	UltimaLecturaEn *time.Time    `json:"ultimaLecturaEn" nullable:"true"`
	UltimoError     *string       `json:"ultimoError" nullable:"true" doc:"Último error de lectura (resumido, sin credenciales)"`
	Version         int32         `json:"version"`
	CreadoEn        time.Time     `json:"creadoEn"`
	ActualizadoEn   time.Time     `json:"actualizadoEn"`
}

func aBuzon(f soportebd.SoporteBuzon) Buzon {
	b := Buzon{
		ID: f.ID, EmpresaID: f.EmpresaID, Nombre: f.Nombre, Correo: f.Correo,
		IMAP: ServidorIMAP{
			Host: f.ImapHost, Puerto: f.ImapPuerto, TLS: f.ImapTls, Usuario: f.ImapUsuario,
			Carpeta: f.ImapCarpeta, CarpetaProcesados: f.ImapCarpetaProcesados,
		},
		CategoriaID: f.CategoriaID, Prioridad: f.Prioridad, AuthservID: f.AuthservID, Activo: f.Activo,
		UltimaLecturaEn: f.UltimaLecturaEn, UltimoError: f.UltimoError, Version: f.Version,
		CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt,
	}
	if f.SmtpHost != nil && f.SmtpPuerto != nil && f.SmtpTls != nil {
		b.SMTP = &ServidorSMTP{Host: *f.SmtpHost, Puerto: *f.SmtpPuerto, TLS: *f.SmtpTls, Usuario: f.SmtpUsuario}
	}
	return b
}

// EntradaIMAP es el servidor IMAP al crear o editar.
type EntradaIMAP struct {
	Host              string  `json:"host" minLength:"1" maxLength:"253"`
	Puerto            int32   `json:"puerto" minimum:"1" maximum:"65535"`
	TLS               string  `json:"tls" enum:"tls,starttls"`
	Usuario           string  `json:"usuario" minLength:"1" maxLength:"254"`
	Contrasena        *string `json:"contrasena,omitempty" maxLength:"512" doc:"Obligatoria al crear y al cambiar host, puerto o usuario; al editar, omitida = se conserva"`
	Carpeta           string  `json:"carpeta,omitempty" maxLength:"200" doc:"INBOX por defecto"`
	CarpetaProcesados *string `json:"carpetaProcesados,omitempty" maxLength:"200"`
}

// EntradaSMTP es el servidor SMTP al crear o editar.
type EntradaSMTP struct {
	Host       string  `json:"host" minLength:"1" maxLength:"253"`
	Puerto     int32   `json:"puerto" minimum:"1" maximum:"65535"`
	TLS        string  `json:"tls" enum:"tls,starttls"`
	Usuario    *string `json:"usuario,omitempty" maxLength:"254" doc:"Sin usuario no se autentica"`
	Contrasena *string `json:"contrasena,omitempty" maxLength:"512" doc:"Obligatoria con usuario al crear o al cambiar host, puerto o usuario; al editar, omitida = se conserva"`
}

// DatosBuzon son los campos editables de un buzón.
type DatosBuzon struct {
	Nombre      string       `json:"nombre" minLength:"1" maxLength:"120"`
	Correo      string       `json:"correo" format:"email" maxLength:"254"`
	IMAP        EntradaIMAP  `json:"imap"`
	SMTP        *EntradaSMTP `json:"smtp,omitempty" doc:"Sin smtp, las respuestas de los agentes no salen por correo"`
	CategoriaID *int64       `json:"categoriaId,omitempty" minimum:"1"`
	Prioridad   string       `json:"prioridad,omitempty" enum:"baja,media,alta,critica" doc:"media por defecto"`
	AuthservID  *string      `json:"authservId,omitempty" maxLength:"253" doc:"authserv-id que pone en Authentication-Results el servidor que recibe para este buzón (RFC 8601; p. ej. mx.isp.gt). Solo con él se verifica el remitente (dmarc=pass o dkim=pass alineado): sin verificar, la respuesta a un ticket queda como nota interna. Omitido o vacío = no se confía en ninguna cabecera"`
}

// EntradaCrearBuzon es la entrada de buzones.crear. El buzón nace inactivo.
type EntradaCrearBuzon struct {
	DatosBuzon
}

// EntradaEditarBuzon es la entrada de buzones.editar.
type EntradaEditarBuzon struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1"`
	DatosBuzon
}

// EntradaCambiarBuzon es la entrada de buzones.activar y buzones.desactivar.
type EntradaCambiarBuzon struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1"`
}

// EntradaVerBuzon es la entrada de GET /buzones/{id} y de buzones.probar.
type EntradaVerBuzon struct {
	ID int64 `path:"id" minimum:"1"`
}

// EntradaListarBuzones es la entrada de GET /buzones.
type EntradaListarBuzones struct {
	nucleo.ParamsPagina
}

// ResultadoPrueba de la conexión de un buzón.
type ResultadoPrueba struct {
	IMAP PruebaServidor  `json:"imap"`
	SMTP *PruebaServidor `json:"smtp,omitempty" doc:"Ausente si el buzón no tiene SMTP"`
}

// PruebaServidor: ok o el error resumido (sin credenciales ni respuesta cruda del servidor).
type PruebaServidor struct {
	OK      bool    `json:"ok"`
	Codigo  *string `json:"codigo" nullable:"true" enum:"destino_no_permitido,dns,conexion,tls,sin_tls,autenticacion,carpeta,protocolo,cifrado"`
	Detalle *string `json:"detalle" nullable:"true"`
}

// datosValidos es DatosBuzon ya validado y normalizado.
type datosValidos struct {
	nombre, correo, prioridad string
	imap                      EntradaIMAP
	smtp                      *EntradaSMTP
	categoriaID               *int64
	authservID                *string
}

func campo(nombre, mensaje, regla string) *nucleo.Problema {
	return nucleo.Validacion(nucleo.ErrorCampo{Field: nombre, Message: mensaje, Rule: regla})
}

// validarHost: nombre o IP; una IP interna escrita se rechaza aquí (los nombres se revisan al
// conectar, tras resolver el DNS).
func (m *Modulo) validarHost(nombre, host string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" || strings.ContainsAny(h, " /\\@?#") {
		return "", campo(nombre, "Host inválido", "host")
	}
	if !correo.HostPermitido(h, m.cfg.Correo.Red.RedesPermitidas) {
		return "", nucleo.ReglaNegocio("destino_no_permitido", "El servidor está en una red interna no permitida")
	}
	return h, nil
}

func (m *Modulo) validarBuzon(ctx context.Context, q *soportebd.Queries, d DatosBuzon, empresa *int64) (datosValidos, error) {
	var v datosValidos
	var err error
	if v.nombre, err = texto("nombre", d.Nombre, 1, 120); err != nil {
		return v, err
	}
	dir, err := mail.ParseAddress(strings.TrimSpace(d.Correo))
	if err != nil || dir.Name != "" {
		return v, campo("correo", "Correo inválido", "email")
	}
	v.correo = strings.ToLower(dir.Address)
	v.prioridad = d.Prioridad
	if v.prioridad == "" {
		v.prioridad = "media"
	}
	if !slices.Contains(Prioridades, v.prioridad) {
		return v, campo("prioridad", "Prioridad desconocida", "enum")
	}
	v.imap = d.IMAP
	if v.imap.Host, err = m.validarHost("imap.host", d.IMAP.Host); err != nil {
		return v, err
	}
	if v.imap.TLS != correo.TLSImplicito && v.imap.TLS != correo.TLSStartTLS {
		return v, campo("imap.tls", "Solo tls o starttls: nunca en claro", "enum")
	}
	if v.imap.Puerto < 1 || v.imap.Puerto > 65535 {
		return v, campo("imap.puerto", "Puerto inválido", "range")
	}
	if v.imap.Usuario, err = texto("imap.usuario", d.IMAP.Usuario, 1, 254); err != nil {
		return v, err
	}
	if v.imap.Carpeta = strings.TrimSpace(d.IMAP.Carpeta); v.imap.Carpeta == "" {
		v.imap.Carpeta = "INBOX"
	}
	if v.imap.CarpetaProcesados, err = textoOpcional("imap.carpetaProcesados", d.IMAP.CarpetaProcesados, 200); err != nil {
		return v, err
	}
	if d.SMTP != nil {
		s := *d.SMTP
		if s.Host, err = m.validarHost("smtp.host", d.SMTP.Host); err != nil {
			return v, err
		}
		if s.TLS != correo.TLSImplicito && s.TLS != correo.TLSStartTLS {
			return v, campo("smtp.tls", "Solo tls o starttls: nunca en claro", "enum")
		}
		if s.Puerto < 1 || s.Puerto > 65535 {
			return v, campo("smtp.puerto", "Puerto inválido", "range")
		}
		if s.Usuario, err = textoOpcional("smtp.usuario", d.SMTP.Usuario, 254); err != nil {
			return v, err
		}
		if s.Usuario == nil {
			s.Contrasena = nil
		}
		v.smtp = &s
	}
	if d.AuthservID != nil {
		a := strings.ToLower(strings.TrimSpace(*d.AuthservID))
		if strings.ContainsAny(a, " \t\r\n;()\"") || len(a) > 253 {
			return v, campo("authservId", "authserv-id inválido (un nombre, sin espacios ni «;»)", "authserv_id")
		}
		if a != "" {
			v.authservID = &a
		}
	}
	if d.CategoriaID != nil {
		c, err := q.CategoriaPorID(ctx, *d.CategoriaID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!c.Activa || (c.EmpresaID != nil && deref(c.EmpresaID) != deref(empresa)))) {
			return v, campo("categoriaId", "La categoría no existe o está inactiva", "categoria")
		}
		if err != nil {
			return v, err
		}
		v.categoriaID = d.CategoriaID
	}
	return v, nil
}

// datoAsociado ata la contraseña cifrada a su buzón y servidor (no se puede copiar a otra fila).
func datoAsociado(id int64, servidor string) []byte {
	return []byte(fmt.Sprintf("soporte.buzon:%d:%s", id, servidor))
}

func (m *Modulo) cifrar(id int64, servidor, valor string) ([]byte, error) {
	if m.cfg.Cifrador == nil {
		return nil, sinCifrado()
	}
	return m.cfg.Cifrador.Cifrar([]byte(valor), datoAsociado(id, servidor))
}

func (m *Modulo) descifrar(id int64, servidor string, valor []byte) (string, error) {
	if len(valor) == 0 {
		return "", nil
	}
	if m.cfg.Cifrador == nil {
		return "", sinCifrado()
	}
	b, err := m.cfg.Cifrador.Descifrar(valor, datoAsociado(id, servidor))
	if err != nil {
		return "", nucleo.ReglaNegocio("contrasena_ilegible", "La contraseña guardada no se puede descifrar (¿cambió IRIS_CLAVE_CIFRADO?); vuelve a escribirla")
	}
	return string(b), nil
}

func sinCifrado() *nucleo.Problema {
	return nucleo.ReglaNegocio("cifrado_no_configurado", "Falta IRIS_CLAVE_CIFRADO: no se pueden guardar ni usar contraseñas de buzones")
}

func contrasenaRequerida(nombre string) *nucleo.Problema {
	return nucleo.Validacion(nucleo.ErrorCampo{Field: nombre, Message: "Escribe la contraseña (es nueva o cambió el servidor o el usuario)", Rule: "required"})
}

// empresaDelBuzon: el buzón es de la empresa activa (null sin conector), como un ticket.
func empresaDelBuzon(ctx context.Context) (*int64, error) {
	todas, id := filtroEmpresa(ctx)
	if todas || (id != nil && *id == 0) {
		return nil, nucleo.EmpresaRequerida()
	}
	return id, nil
}

// tomarBuzon lee el buzón con FOR UPDATE; de otra empresa es 404.
func tomarBuzon(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, id int64, version *int32) (soportebd.SoporteBuzon, error) {
	t.FijarEntidad("buzon", id)
	f, err := q.BuzonPorIDParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, nucleo.NoEncontrado("El buzón no existe")
	}
	if err != nil {
		return f, err
	}
	if todas, emp := filtroEmpresa(ctx); !todas && !mismoID(f.EmpresaID, emp) {
		return f, nucleo.NoEncontrado("El buzón no existe")
	}
	t.FijarEmpresa(deref(f.EmpresaID))
	if version != nil {
		if err := nucleo.ExigirVersion(*version, f.Version, "buzon", id); err != nil {
			return f, err
		}
	}
	return f, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (m *Modulo) nuevoCrearBuzon() *auditoria.Caso[EntradaCrearBuzon, Buzon] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearBuzon, Buzon]{
		Accion:  "buzones.crear",
		Permiso: nucleo.PermBuzonesCrear,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearBuzon) (Buzon, error) {
			if err := exigirGlobal(ctx, nucleo.PermBuzonesCrear); err != nil {
				return Buzon{}, err
			}
			q := soportebd.New(t)
			if m.cfg.Cifrador == nil {
				return Buzon{}, sinCifrado()
			}
			empresa, err := empresaDelBuzon(ctx)
			if err != nil {
				return Buzon{}, err
			}
			t.FijarEmpresa(deref(empresa))
			v, err := m.validarBuzon(ctx, q, in.DatosBuzon, empresa)
			if err != nil {
				return Buzon{}, err
			}
			if v.imap.Contrasena == nil || *v.imap.Contrasena == "" {
				return Buzon{}, contrasenaRequerida("imap.contrasena")
			}
			if v.smtp != nil && v.smtp.Usuario != nil && (v.smtp.Contrasena == nil || *v.smtp.Contrasena == "") {
				return Buzon{}, contrasenaRequerida("smtp.contrasena")
			}
			p := soportebd.CrearBuzonParams{
				EmpresaID: empresa, Nombre: v.nombre, Correo: v.correo, ImapHost: v.imap.Host, ImapPuerto: v.imap.Puerto,
				ImapTls: v.imap.TLS, ImapUsuario: v.imap.Usuario, ImapCarpeta: v.imap.Carpeta,
				ImapCarpetaProcesados: v.imap.CarpetaProcesados, CategoriaID: v.categoriaID, Prioridad: v.prioridad,
				AuthservID: v.authservID,
			}
			if v.smtp != nil {
				p.SmtpHost, p.SmtpPuerto, p.SmtpTls, p.SmtpUsuario = &v.smtp.Host, &v.smtp.Puerto, &v.smtp.TLS, v.smtp.Usuario
			}
			f, err := q.CrearBuzon(ctx, p)
			if err != nil {
				return Buzon{}, err
			}
			t.FijarEntidad("buzon", f.ID)
			cifImap, err := m.cifrar(f.ID, "imap", *v.imap.Contrasena)
			if err != nil {
				return Buzon{}, err
			}
			var cifSMTP []byte
			if v.smtp != nil && v.smtp.Contrasena != nil {
				if cifSMTP, err = m.cifrar(f.ID, "smtp", *v.smtp.Contrasena); err != nil {
					return Buzon{}, err
				}
			}
			if f, err = q.FijarContrasenasBuzon(ctx, soportebd.FijarContrasenasBuzonParams{ID: f.ID, ImapContrasena: cifImap, SmtpContrasena: cifSMTP}); err != nil {
				return Buzon{}, err
			}
			b := aBuzon(f)
			t.RegistrarCambios("buzon", f.ID, nil, b)
			return b, t.Emitir("buzon.creado", map[string]any{"buzon_id": f.ID, "correo": f.Correo})
		},
	})
}

func (m *Modulo) nuevoEditarBuzon() *auditoria.Caso[EntradaEditarBuzon, Buzon] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEditarBuzon, Buzon]{
		Accion:  "buzones.editar",
		Permiso: nucleo.PermBuzonesEditar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEditarBuzon) (Buzon, error) {
			if err := exigirGlobal(ctx, nucleo.PermBuzonesEditar); err != nil {
				return Buzon{}, err
			}
			q := soportebd.New(t)
			antes, err := tomarBuzon(ctx, t, q, in.ID, &in.Version)
			if err != nil {
				return Buzon{}, err
			}
			v, err := m.validarBuzon(ctx, q, in.DatosBuzon, antes.EmpresaID)
			if err != nil {
				return Buzon{}, err
			}
			p := soportebd.GuardarBuzonParams{
				ID: antes.ID, Version: antes.Version, Nombre: v.nombre, Correo: v.correo, ImapHost: v.imap.Host,
				ImapPuerto: v.imap.Puerto, ImapTls: v.imap.TLS, ImapUsuario: v.imap.Usuario, ImapContrasena: antes.ImapContrasena,
				ImapCarpeta: v.imap.Carpeta, ImapCarpetaProcesados: v.imap.CarpetaProcesados,
				CategoriaID: v.categoriaID, Prioridad: v.prioridad, AuthservID: v.authservID,
				ReiniciarCursor: antes.ImapHost != v.imap.Host || antes.ImapUsuario != v.imap.Usuario ||
					antes.ImapCarpeta != v.imap.Carpeta || antes.ImapPuerto != v.imap.Puerto,
			}
			cambiaron := []string{}
			// Si cambia el destino, la contraseña guardada no viaja a otro servidor: hay que
			// escribirla de nuevo (si no, un editor podría mandarla a un host suyo).
			if v.imap.Contrasena != nil && *v.imap.Contrasena != "" {
				if p.ImapContrasena, err = m.cifrar(antes.ID, "imap", *v.imap.Contrasena); err != nil {
					return Buzon{}, err
				}
				cambiaron = append(cambiaron, "imap")
			} else if antes.ImapHost != v.imap.Host || antes.ImapPuerto != v.imap.Puerto || antes.ImapUsuario != v.imap.Usuario || antes.ImapTls != v.imap.TLS {
				return Buzon{}, contrasenaRequerida("imap.contrasena")
			}
			if v.smtp != nil {
				p.SmtpHost, p.SmtpPuerto, p.SmtpTls, p.SmtpUsuario = &v.smtp.Host, &v.smtp.Puerto, &v.smtp.TLS, v.smtp.Usuario
				mismo := antes.SmtpHost != nil && *antes.SmtpHost == v.smtp.Host && deref32(antes.SmtpPuerto) == v.smtp.Puerto &&
					derefStr(antes.SmtpTls) == v.smtp.TLS && derefStr(antes.SmtpUsuario) == derefStr(v.smtp.Usuario)
				switch {
				case v.smtp.Usuario == nil:
				case v.smtp.Contrasena != nil && *v.smtp.Contrasena != "":
					if p.SmtpContrasena, err = m.cifrar(antes.ID, "smtp", *v.smtp.Contrasena); err != nil {
						return Buzon{}, err
					}
					cambiaron = append(cambiaron, "smtp")
				case mismo && len(antes.SmtpContrasena) > 0:
					p.SmtpContrasena = antes.SmtpContrasena
				default:
					return Buzon{}, contrasenaRequerida("smtp.contrasena")
				}
			}
			f, err := q.GuardarBuzon(ctx, p)
			if err != nil {
				return Buzon{}, nucleo.VerificarVersion(err, "buzon", antes.ID)
			}
			b := aBuzon(f)
			t.RegistrarCambios("buzon", f.ID, aBuzon(antes), b)
			if len(cambiaron) > 0 {
				t.Anotar(map[string]any{"acceso_renovado": cambiaron})
			}
			return b, t.Emitir("buzon.actualizado", map[string]any{"buzon_id": f.ID})
		},
	})
}

func deref32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (m *Modulo) nuevoCambiarBuzon(activo bool) *auditoria.Caso[EntradaCambiarBuzon, Buzon] {
	accion, permiso, evento := "buzones.desactivar", nucleo.PermBuzonesDesactivar, "buzon.desactivado"
	if activo {
		accion, permiso, evento = "buzones.activar", nucleo.PermBuzonesActivar, "buzon.activado"
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaCambiarBuzon, Buzon]{
		Accion:  accion,
		Permiso: permiso,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambiarBuzon) (Buzon, error) {
			if err := exigirGlobal(ctx, permiso); err != nil {
				return Buzon{}, err
			}
			q := soportebd.New(t)
			antes, err := tomarBuzon(ctx, t, q, in.ID, &in.Version)
			if err != nil {
				return Buzon{}, err
			}
			if antes.Activo == activo {
				return Buzon{}, nucleo.ReglaNegocio("sin_cambios", "El buzón ya está así")
			}
			if activo && m.cfg.Cifrador == nil {
				return Buzon{}, sinCifrado()
			}
			f, err := q.CambiarActivoBuzon(ctx, soportebd.CambiarActivoBuzonParams{ID: antes.ID, Version: antes.Version, Activo: activo})
			if err != nil {
				return Buzon{}, nucleo.VerificarVersion(err, "buzon", antes.ID)
			}
			b := aBuzon(f)
			t.RegistrarCambios("buzon", f.ID, aBuzon(antes), b)
			return b, t.Emitir(evento, map[string]any{"buzon_id": f.ID})
		},
	})
}

func (m *Modulo) nuevoVerBuzon() *auditoria.Caso[EntradaVerBuzon, Buzon] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerBuzon, Buzon]{
		Accion:       "buzones.ver",
		Permiso:      nucleo.PermBuzonesVer,
		AuditarExito: soloLecturas[Buzon],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerBuzon) (Buzon, error) {
			f, err := soportebd.New(t).BuzonPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return Buzon{}, nucleo.NoEncontrado("El buzón no existe")
			}
			if err != nil {
				return Buzon{}, err
			}
			if todas, emp := filtroEmpresa(ctx); !todas && !mismoID(f.EmpresaID, emp) {
				return Buzon{}, nucleo.NoEncontrado("El buzón no existe")
			}
			return aBuzon(f), nil
		},
	})
}

func (m *Modulo) nuevoListarBuzones() *auditoria.Caso[EntradaListarBuzones, nucleo.Pagina[Buzon]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarBuzones, nucleo.Pagina[Buzon]]{
		Accion:       "buzones.ver",
		Permiso:      nucleo.PermBuzonesVer,
		AuditarExito: soloLecturas[nucleo.Pagina[Buzon]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarBuzones) (nucleo.Pagina[Buzon], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Buzon]{}, err
			}
			empresa, err := empresaDelBuzon(ctx)
			if err != nil {
				return nucleo.Pagina[Buzon]{}, err
			}
			filas, err := soportebd.New(t).ListarBuzones(ctx, soportebd.ListarBuzonesParams{
				DespuesDe: despues, EmpresaID: empresa, Limite: int32(in.Limite() + 1), //nolint:gosec // ≤ 201
			})
			if err != nil {
				return nucleo.Pagina[Buzon]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(), aBuzon, func(f soportebd.SoporteBuzon) []any { return []any{f.ID} }), nil
		},
	})
}

// servidores descifra las credenciales del buzón (solo dentro del proceso; nunca se devuelven).
func (m *Modulo) servidores(f soportebd.SoporteBuzon) (imap correo.Servidor, smtp *correo.Servidor, err error) {
	clave, err := m.descifrar(f.ID, "imap", f.ImapContrasena)
	if err != nil {
		return imap, nil, err
	}
	imap = correo.Servidor{Host: f.ImapHost, Puerto: int(f.ImapPuerto), TLS: f.ImapTls, Usuario: f.ImapUsuario, Contrasena: clave}
	if f.SmtpHost != nil && f.SmtpPuerto != nil && f.SmtpTls != nil {
		claveSMTP, err := m.descifrar(f.ID, "smtp", f.SmtpContrasena)
		if err != nil {
			return imap, nil, err
		}
		smtp = &correo.Servidor{Host: *f.SmtpHost, Puerto: int(*f.SmtpPuerto), TLS: *f.SmtpTls, Usuario: derefStr(f.SmtpUsuario), Contrasena: claveSMTP}
	}
	return imap, smtp, nil
}

func prueba(err error) PruebaServidor {
	if err == nil {
		return PruebaServidor{OK: true}
	}
	var p *nucleo.Problema
	if errors.As(err, &p) {
		return PruebaServidor{Codigo: ptr("cifrado"), Detalle: ptr(p.Detail)}
	}
	r := correo.Resumir(err)
	return PruebaServidor{Codigo: &r.Codigo, Detalle: &r.Detalle}
}

// Probar un buzón son tres pasos para no tener una transacción abierta durante la red:
//
//  1. buzones.probar (preparar): transacción corta que verifica permiso (global), empresa y lee
//     y descifra las credenciales. No deja fila de auditoría por sí sola.
//  2. La prueba de red, fuera de toda transacción, con un plazo total (PlazoPrueba, 20 s) que no se
//     renueva por paso: IMAP y SMTP comparten el mismo plazo.
//  3. buzones.probar (registrar): transacción corta que audita el resultado.

// pruebaPreparada: credenciales descifradas (solo en memoria; nunca se serializan).
type pruebaPreparada struct {
	buzon   soportebd.SoporteBuzon
	imap    correo.Servidor
	smtp    *correo.Servidor
	cifrado error
}

// EntradaRegistrarPrueba es lo que se audita de una prueba (sin credenciales).
type EntradaRegistrarPrueba struct {
	ID        int64           `json:"id"`
	Resultado ResultadoPrueba `json:"resultado"`
}

// ProbadorBuzon ejecuta buzones.probar (ver arriba).
type ProbadorBuzon struct {
	m         *Modulo
	preparar  *auditoria.Caso[EntradaVerBuzon, pruebaPreparada]
	registrar *auditoria.Caso[EntradaRegistrarPrueba, struct{}]
}

func (m *Modulo) nuevoProbarBuzon() *ProbadorBuzon {
	p := &ProbadorBuzon{m: m}
	p.preparar = auditoria.Nuevo(auditoria.Caso[EntradaVerBuzon, pruebaPreparada]{
		Accion:  "buzones.probar",
		Permiso: nucleo.PermBuzonesEditar,
		// La fila la deja registrar, con el resultado (los errores sí se auditan aquí).
		AuditarExito: func(pruebaPreparada) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerBuzon) (pruebaPreparada, error) {
			if err := exigirGlobal(ctx, nucleo.PermBuzonesEditar); err != nil {
				return pruebaPreparada{}, err
			}
			// Sin FOR UPDATE: la prueba no cambia el buzón y no debe bloquear la lectura.
			t.FijarEntidad("buzon", in.ID)
			f, err := soportebd.New(t).BuzonPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return pruebaPreparada{}, nucleo.NoEncontrado("El buzón no existe")
			}
			if err != nil {
				return pruebaPreparada{}, err
			}
			if todas, emp := filtroEmpresa(ctx); !todas && !mismoID(f.EmpresaID, emp) {
				return pruebaPreparada{}, nucleo.NoEncontrado("El buzón no existe")
			}
			t.FijarEmpresa(deref(f.EmpresaID))
			r := pruebaPreparada{buzon: f}
			r.imap, r.smtp, r.cifrado = m.servidores(f)
			return r, nil
		},
	})
	p.registrar = auditoria.Nuevo(auditoria.Caso[EntradaRegistrarPrueba, struct{}]{
		Accion:  "buzones.probar",
		Permiso: nucleo.PermBuzonesEditar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRegistrarPrueba) (struct{}, error) {
			t.FijarEntidad("buzon", in.ID)
			f, err := soportebd.New(t).BuzonPorID(ctx, in.ID)
			if err != nil {
				return struct{}{}, err
			}
			t.FijarEmpresa(deref(f.EmpresaID))
			t.Anotar(map[string]any{"imap_ok": in.Resultado.IMAP.OK, "imap_codigo": in.Resultado.IMAP.Codigo, "smtp": in.Resultado.SMTP})
			return struct{}{}, nil
		},
	})
	return p
}

// Ejecutar prueba la conexión IMAP (y SMTP) del buzón: preparar, red con plazo total y
// registrar (ver arriba).
func (p *ProbadorBuzon) Ejecutar(ctx context.Context, in EntradaVerBuzon) (ResultadoPrueba, error) {
	prep, err := p.preparar.Ejecutar(ctx, in)
	if err != nil {
		return ResultadoPrueba{}, err
	}
	var r ResultadoPrueba
	if prep.cifrado != nil {
		r.IMAP = prueba(prep.cifrado)
	} else {
		r = p.m.probarRed(ctx, prep)
	}
	if _, err := p.registrar.Ejecutar(ctx, EntradaRegistrarPrueba{ID: prep.buzon.ID, Resultado: r}); err != nil {
		return ResultadoPrueba{}, err
	}
	return r, nil
}

// probarRed corre la prueba de red fuera de la transacción, con PlazoPrueba en total.
func (m *Modulo) probarRed(ctx context.Context, prep pruebaPreparada) ResultadoPrueba {
	ctx, cancelar := context.WithTimeout(ctx, m.cfg.Correo.plazoPrueba())
	defer cancelar()
	var r ResultadoPrueba
	ses, _, err := correo.AbrirIMAP(ctx, m.cfg.Correo.Red, prep.imap, prep.buzon.ImapCarpeta)
	ses.Cerrar()
	r.IMAP = prueba(err)
	if prep.smtp != nil {
		s := prueba(correo.ProbarSMTP(ctx, m.cfg.Correo.Red, *prep.smtp))
		r.SMTP = &s
	}
	return r
}

// exigirGlobal: administrar buzones (y probarlos, que conecta con las credenciales) exige el
// permiso sin límite de zona ni cuadrilla: un buzón es de toda la empresa.
func exigirGlobal(ctx context.Context, permiso nucleo.Permiso) error {
	if !contexto.PermisosDe(ctx).TieneGlobal(string(permiso)) {
		return nucleo.Prohibido(fmt.Sprintf("El permiso %s debe tener alcance global (sin zona ni cuadrilla)", permiso))
	}
	return nil
}

// esSistema: las acciones internas del correo (lectura, proceso y envío) solo las corre el
// worker como actor sistema.
func esSistema(ctx context.Context) error {
	if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
		return nucleo.Prohibido("Solo el sistema")
	}
	return nil
}
