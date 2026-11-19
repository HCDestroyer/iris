package mensajeria

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/ami"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Centralita Issabel, etapa E4a (contratos/voz.md §2.10 y §3.12): la conexión de solo lectura con
// el AMI y las extensiones de cada usuario. La conexión en sí la abre el motor (centralita_motor.go).

// PuertoAMI es el AMI sobre TLS. El 5038 (sin cifrar) no se admite.
const PuertoAMI = 5039

// Estados de la centralita.
const (
	CentralitaConectada    = "conectada"
	CentralitaDesconectada = "desconectada"
	CentralitaError        = "error"
	CentralitaDesactivada  = "desactivada"
)

// Resultados de «Probar».
const (
	PruebaOK             = "ok"
	PruebaPermisosDeMas  = "permisos_de_mas"
	PruebaHuellaDistinta = "huella_distinta"
	PruebaNoConecta      = "no_conecta"
)

// DatosAsociadosCentralita ata el secreto cifrado a su fila (rotación de IRIS_CLAVE_CIFRADO).
func DatosAsociadosCentralita(id int64) []byte {
	return fmt.Appendf(nil, "mensajeria.centralita:%d:secreto", id)
}

// DatosAsociadosExtensionSIP ata la credencial SIP del navegador de una extensión (E4c, prevista y
// sin usar en E4a) a su fila.
func DatosAsociadosExtensionSIP(id int64) []byte {
	return fmt.Appendf(nil, "mensajeria.centralita_extension:%d:sip", id)
}

// Centralita es la conexión como la ve el panel. Nunca trae el secreto.
type Centralita struct {
	ID                int64      `json:"id"`
	Nombre            string     `json:"nombre"`
	Tipo              string     `json:"tipo" enum:"issabel"`
	Host              string     `json:"host"`
	Puerto            int32      `json:"puerto"`
	Usuario           string     `json:"usuario"`
	SecretoGuardadoEn *time.Time `json:"secretoGuardadoEn" nullable:"true" doc:"Cuándo se guardó la contraseña del AMI (nunca se devuelve)"`
	HuellaTLS         *string    `json:"huellaTls" nullable:"true" doc:"SHA-256 del certificado del AMI que confirmó un administrador"`
	HuellaVista       *string    `json:"huellaVista" nullable:"true" doc:"Con error huella_cambiada: la huella distinta que presentó la centralita"`
	Modo              string     `json:"modo" enum:"lectura"`
	Estado            string     `json:"estado" enum:"conectada,desconectada,error,desactivada"`
	ErrorCodigo       *string    `json:"errorCodigo" nullable:"true" enum:"huella_cambiada,permisos_de_mas,credenciales,sin_conexion"`
	ProbadaEn         *time.Time `json:"probadaEn" nullable:"true" doc:"La última prueba que pasó con los datos actuales (encender la exige)"`
	ConectadaEn       *time.Time `json:"conectadaEn" nullable:"true"`
	DesconectadaDesde *time.Time `json:"desconectadaDesde" nullable:"true"`
	UltimoEventoEn    *time.Time `json:"ultimoEventoEn" nullable:"true" doc:"Última señal del AMI (se actualiza como mucho cada minuto)"`
	Activa            bool       `json:"activa"`
	Version           int32      `json:"version"`
}

func aCentralita(f mensajeriabd.MensajeriaCentralita) Centralita {
	c := Centralita{ID: f.ID, Nombre: f.Nombre, Tipo: f.Tipo, Host: f.Host, Puerto: f.Puerto, Usuario: f.Usuario, SecretoGuardadoEn: f.SecretoGuardadoEn,
		HuellaTLS: f.HuellaTls, Modo: f.Modo, Estado: f.Estado, ErrorCodigo: f.ErrorCodigo, ProbadaEn: f.ProbadaEn, ConectadaEn: f.ConectadaEn,
		DesconectadaDesde: f.DesconectadaDesde, UltimoEventoEn: f.UltimoEventoEn, Activa: f.Activa, Version: f.Version}
	if f.ErrorCodigo != nil && *f.ErrorCodigo == "huella_cambiada" {
		c.HuellaVista = f.HuellaVista
	}
	return c
}

// centralitaAuditable: lo que va a auditoria.cambios (sin el secreto: solo si está guardado).
func centralitaAuditable(f mensajeriabd.MensajeriaCentralita) map[string]any {
	return map[string]any{"nombre": f.Nombre, "host": f.Host, "puerto": f.Puerto, "usuario": f.Usuario, "secreto_guardado": f.SecretoCifrado != nil,
		"huella_tls": f.HuellaTls, "estado": f.Estado, "activa": f.Activa}
}

// Validación de los datos de conexión ----------------------------------------------------------------

var (
	hostCentralita    = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,62}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,62}[A-Za-z0-9])?)*$`)
	usuarioCentralita = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	huellaCentralita  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	extensionValida   = regexp.MustCompile(`^[0-9]{2,8}$`)
)

func campoCentralita(campo, detalle string) error {
	return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: detalle})
}

// validarHostCentralita aplica las reglas de URL de §16.1 del chat a la dirección del AMI: se
// permiten IP privadas; nunca link-local, metadata ni loopback (salvo configuración de pruebas). El
// DNS se vuelve a revisar en cada conexión (controlIA).
func (m *Modulo) validarHostCentralita(host string) error {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if ip, err := netip.ParseAddr(h); err == nil {
		if !ipPermitidaIA(ip, m.cfg.CentralitaLoopback) {
			return nucleo.ReglaNegocio("url_no_permitida", "Esa dirección no se permite (loopback, link-local o metadata de la nube)")
		}
		return nil
	}
	if !hostCentralita.MatchString(h) || len(h) > 253 {
		return campoCentralita("host", "Escribe una IP o un nombre de servidor, sin http:// ni puerto")
	}
	if (esLocalhost(h) && !m.cfg.CentralitaLoopback) || h == "metadata" || strings.HasSuffix(h, "metadata.google.internal") {
		return nucleo.ReglaNegocio("url_no_permitida", "Esa dirección no se permite (loopback o metadata de la nube)")
	}
	return nil
}

func (m *Modulo) validarPuertoCentralita(p int32) error {
	if p == PuertoAMI || slices.Contains(m.cfg.CentralitaPuertos, int(p)) {
		return nil
	}
	return nucleo.ReglaNegocio("puerto_no_permitido", "Solo el AMI cifrado (TLS) en el puerto 5039. El 5038 sin cifrar no se admite")
}

func validarSecretoCentralita(s string) error {
	if len(s) < 8 || len(s) > 128 || !ami.ValorSeguro(s) {
		return campoCentralita("secreto", "La contraseña del AMI debe tener entre 8 y 128 caracteres, sin saltos de línea")
	}
	return nil
}

func normalizarHuella(h string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(h), ":", ""))
}

func (m *Modulo) cifrarCentralita(id int64, plano string) ([]byte, error) {
	if m.cfg.Cifrador == nil {
		return nil, nucleo.ReglaNegocio("cifrado_no_configurado", "Falta IRIS_CLAVE_CIFRADO: no se puede guardar la contraseña del AMI")
	}
	return m.cfg.Cifrador.Cifrar([]byte(plano), DatosAsociadosCentralita(id))
}

func (m *Modulo) descifrarCentralita(f mensajeriabd.MensajeriaCentralita) (string, error) {
	if f.SecretoCifrado == nil || m.cfg.Cifrador == nil {
		return "", errors.New("centralita sin secreto o sin cifrador")
	}
	b, err := m.cfg.Cifrador.Descifrar(f.SecretoCifrado, DatosAsociadosCentralita(f.ID))
	return string(b), err
}

func centralitaNoEncontrada() *nucleo.Problema {
	return nucleo.NoEncontrado("La centralita no existe")
}

func empresaDeCtx(ctx context.Context) int64 {
	if contexto.EmpresasDe(ctx).Estado == contexto.EmpresaActiva {
		return contexto.EmpresaDe(ctx)
	}
	return 0
}

func ptrEmpresa(e int64) *int64 {
	if e == 0 {
		return nil
	}
	return &e
}

func esUnica(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

// notificarMotor despierta al motor (vuelve a leer centralitas y extensiones) al confirmar.
func notificarMotor(ctx context.Context, q *mensajeriabd.Queries, id int64) error {
	return q.NotificarCentralitas(ctx, strconv.FormatInt(id, 10))
}

// Casos de uso de la conexión --------------------------------------------------------------------------

// EntradaCrearCentralita es POST /centralitas.
type EntradaCrearCentralita struct {
	Nombre    string `json:"nombre" minLength:"1" maxLength:"80"`
	Host      string `json:"host" minLength:"1" maxLength:"253" doc:"IP o nombre del servidor Issabel dentro de la red o la VPN"`
	Puerto    int32  `json:"puerto,omitempty" minimum:"1" maximum:"65535" doc:"5039 (AMI con TLS). Otro solo si la instalación lo permite"`
	Usuario   string `json:"usuario" minLength:"1" maxLength:"64" pattern:"^[A-Za-z0-9_.-]{1,64}$"`
	Secreto   string `json:"secreto" minLength:"8" maxLength:"128" doc:"Contraseña del usuario AMI de solo lectura (se guarda cifrada y no se devuelve)"`
	HuellaTLS string `json:"huellaTls" minLength:"64" maxLength:"95" doc:"La huella SHA-256 que mostró «Probar» y que el administrador comparó con la de Issabel"`
}

// EntradaActualizarCentralita es POST /centralitas/{id}/actualizar.
type EntradaActualizarCentralita struct {
	ID        int64   `json:"-"`
	Version   int32   `json:"version" minimum:"1"`
	Nombre    string  `json:"nombre" minLength:"1" maxLength:"80"`
	Host      string  `json:"host" minLength:"1" maxLength:"253"`
	Puerto    int32   `json:"puerto,omitempty" minimum:"1" maximum:"65535"`
	Usuario   string  `json:"usuario" minLength:"1" maxLength:"64" pattern:"^[A-Za-z0-9_.-]{1,64}$"`
	Secreto   *string `json:"secreto,omitempty" minLength:"8" maxLength:"128" doc:"Solo para cambiarla. Obligatoria si cambian host, puerto o usuario (422 clave_requerida)"`
	HuellaTLS string  `json:"huellaTls" minLength:"64" maxLength:"95"`
}

// EntradaCambioCentralita: activar, desactivar.
type EntradaCambioCentralita struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1"`
}

// EntradaConfirmarHuella es POST /centralitas/{id}/confirmar-huella.
type EntradaConfirmarHuella struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1"`
	Huella  string `json:"huella" minLength:"64" maxLength:"95" doc:"La huella nueva tal como la muestra Iris (huellaVista), comparada con la de Issabel"`
}

// EntradaVerCentralita es GET /centralitas/{id}.
type EntradaVerCentralita struct {
	ID int64 `json:"-"`
}

// CentralitaDeEmpresa es GET /centralita: la de la empresa activa (o ninguna).
type CentralitaDeEmpresa struct {
	Centralita *Centralita `json:"centralita,omitempty" doc:"Ausente si la empresa no tiene centralita"`
}

type casosCentralita struct {
	Ver             *auditoria.Caso[struct{}, CentralitaDeEmpresa]
	Crear           *auditoria.Caso[EntradaCrearCentralita, Centralita]
	Actualizar      *auditoria.Caso[EntradaActualizarCentralita, Centralita]
	Activar         *auditoria.Caso[EntradaCambioCentralita, Centralita]
	Desactivar      *auditoria.Caso[EntradaCambioCentralita, Centralita]
	ConfirmarHuella *auditoria.Caso[EntradaConfirmarHuella, Centralita]
	Probar          *ProbarCentralita
	Extensiones     *auditoria.Caso[EntradaVerCentralita, ListaExtensiones]
	Asignar         *auditoria.Caso[EntradaAsignarExtension, Extension]
	Quitar          *auditoria.Caso[EntradaQuitarExtension, Extension]

	Llamadas     *auditoria.Caso[EntradaListarLlamadasCentralita, ListaLlamadasCentralita]
	VerLlamada   *auditoria.Caso[EntradaLlamadaCentralita, Llamada]
	Anotar       *auditoria.Caso[EntradaAnotarCentralita, Llamada]
	Asociar      *AsociarClienteCentralita
	Vincular     *auditoria.Caso[EntradaVincularTicket, Llamada]
	procesar     *auditoria.Caso[entradaHechoAMI, bool]
	fijarEstado  *auditoria.Caso[entradaEstadoCentralita, struct{}]
	probada      *auditoria.Caso[entradaProbada, Centralita]
	cerrarViejas *auditoria.Caso[int64, int]
}

func nuevosCasosCentralita(m *Modulo) casosCentralita {
	k := casosCentralita{
		Ver: auditoria.Nuevo(auditoria.Caso[struct{}, CentralitaDeEmpresa]{
			Accion: "centralita.ver", Permiso: nucleo.PermCentralitaAsignarExtension, AuditarExito: sinAuditar[CentralitaDeEmpresa],
			Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (CentralitaDeEmpresa, error) {
				f, err := mensajeriabd.New(t).CentralitaDeEmpresa(ctx, empresaDeCtx(ctx))
				if errors.Is(err, pgx.ErrNoRows) {
					return CentralitaDeEmpresa{}, nil
				}
				if err != nil {
					return CentralitaDeEmpresa{}, err
				}
				c := aCentralita(f)
				return CentralitaDeEmpresa{Centralita: &c}, nil
			},
		}),
		Crear: auditoria.Nuevo(auditoria.Caso[EntradaCrearCentralita, Centralita]{
			Accion: "centralita.crear", Permiso: nucleo.PermCentralitaConfigurar,
			Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearCentralita) (Centralita, error) {
				if in.Puerto == 0 {
					in.Puerto = PuertoAMI
				}
				huella := normalizarHuella(in.HuellaTLS)
				if err := m.validarConexion(in.Host, in.Puerto, in.Usuario, &in.Secreto, huella); err != nil {
					return Centralita{}, err
				}
				q := mensajeriabd.New(t)
				empresa := empresaDeCtx(ctx)
				f, err := q.InsertarCentralita(ctx, mensajeriabd.InsertarCentralitaParams{EmpresaID: ptrEmpresa(empresa), Nombre: strings.TrimSpace(in.Nombre),
					Host: strings.ToLower(strings.TrimSpace(in.Host)), Puerto: in.Puerto, Usuario: in.Usuario})
				if esUnica(err) {
					return Centralita{}, nucleo.Conflicto("centralita_existente", "La empresa ya tiene una centralita: edítala")
				}
				if err != nil {
					return Centralita{}, err
				}
				cif, err := m.cifrarCentralita(f.ID, in.Secreto)
				if err != nil {
					return Centralita{}, err
				}
				ahora := m.ahora()
				f.SecretoCifrado, f.SecretoGuardadoEn, f.HuellaTls = cif, &ahora, &huella
				if f, err = guardarCentralita(ctx, q, f); err != nil {
					return Centralita{}, err
				}
				t.FijarEntidad("centralita", f.ID)
				t.RegistrarCambios("centralita", f.ID, nil, centralitaAuditable(f))
				return aCentralita(f), t.Emitir("centralita.creada", map[string]any{"centralita_id": f.ID, "empresa_id": f.EmpresaID, "tipo": f.Tipo, "modo": f.Modo})
			},
		}),
		Actualizar: auditoria.Nuevo(auditoria.Caso[EntradaActualizarCentralita, Centralita]{
			Accion: "centralita.actualizar", Permiso: nucleo.PermCentralitaConfigurar,
			Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaActualizarCentralita) (Centralita, error) {
				q := mensajeriabd.New(t)
				f, err := m.centralitaParaCambiar(ctx, q, in.ID, in.Version)
				if err != nil {
					return Centralita{}, err
				}
				if in.Puerto == 0 {
					in.Puerto = PuertoAMI
				}
				huella := normalizarHuella(in.HuellaTLS)
				host := strings.ToLower(strings.TrimSpace(in.Host))
				if err := m.validarConexion(host, in.Puerto, in.Usuario, in.Secreto, huella); err != nil {
					return Centralita{}, err
				}
				// La contraseña guardada nunca viaja a otro servidor ni con otro usuario.
				cambiaDestino := host != f.Host || in.Puerto != f.Puerto || in.Usuario != f.Usuario
				if cambiaDestino && in.Secreto == nil {
					return Centralita{}, nucleo.ReglaNegocio("clave_requerida", "Cambiaste la dirección, el puerto o el usuario: escribe otra vez la contraseña del AMI")
				}
				antes := f
				f.Nombre, f.Host, f.Puerto, f.Usuario = strings.TrimSpace(in.Nombre), host, in.Puerto, in.Usuario
				conexion := cambiaDestino || in.Secreto != nil || f.HuellaTls == nil || *f.HuellaTls != huella
				if in.Secreto != nil {
					cif, err := m.cifrarCentralita(f.ID, *in.Secreto)
					if err != nil {
						return Centralita{}, err
					}
					ahora := m.ahora()
					f.SecretoCifrado, f.SecretoGuardadoEn = cif, &ahora
				}
				f.HuellaTls = &huella
				if conexion {
					// Otros datos de conexión: hay que volver a probar y encender.
					f.Activa, f.Estado, f.ErrorCodigo, f.HuellaVista, f.ProbadaEn, f.ConectadaEn, f.DesconectadaDesde = false, CentralitaDesactivada, nil, nil, nil, nil, nil
				}
				if f, err = guardarCentralita(ctx, q, f); err != nil {
					return Centralita{}, err
				}
				campos := t.RegistrarCambios("centralita", f.ID, centralitaAuditable(antes), centralitaAuditable(f))
				if in.Secreto != nil {
					campos = append(campos, "secreto")
				}
				if err := notificarMotor(ctx, q, f.ID); err != nil {
					return Centralita{}, err
				}
				return aCentralita(f), t.Emitir("centralita.actualizada", map[string]any{"centralita_id": f.ID, "empresa_id": f.EmpresaID, "campos": campos, "activa": f.Activa})
			},
		}),
		Activar:         nuevoCambioCentralita(m, "centralita.activar", true),
		Desactivar:      nuevoCambioCentralita(m, "centralita.desactivar", false),
		ConfirmarHuella: nuevoConfirmarHuella(m),
		Probar:          nuevoProbarCentralita(m),
	}
	k.Extensiones, k.Asignar, k.Quitar = nuevosCasosExtensiones(m)
	k.Llamadas, k.VerLlamada, k.Anotar, k.Asociar, k.Vincular = nuevosCasosLlamadasCentralita(m)
	k.procesar = nuevoProcesarHechoAMI(m)
	k.fijarEstado = nuevoFijarEstadoCentralita(m)
	k.probada = nuevoMarcarProbada(m)
	k.cerrarViejas = nuevoCerrarViejasCentralita(m)
	return k
}

func (m *Modulo) validarConexion(host string, puerto int32, usuario string, secreto *string, huella string) error {
	if err := m.validarHostCentralita(host); err != nil {
		return err
	}
	if err := m.validarPuertoCentralita(puerto); err != nil {
		return err
	}
	if !usuarioCentralita.MatchString(usuario) {
		return campoCentralita("usuario", "Solo letras, números, punto, guion y guion bajo")
	}
	if secreto != nil {
		if err := validarSecretoCentralita(*secreto); err != nil {
			return err
		}
	}
	if !huellaCentralita.MatchString(huella) {
		return campoCentralita("huellaTls", "La huella debe ser el SHA-256 del certificado (64 caracteres hexadecimales)")
	}
	return nil
}

func guardarCentralita(ctx context.Context, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaCentralita) (mensajeriabd.MensajeriaCentralita, error) {
	return q.GuardarCentralita(ctx, mensajeriabd.GuardarCentralitaParams{ID: f.ID, Nombre: f.Nombre, Host: f.Host, Puerto: f.Puerto, Usuario: f.Usuario,
		SecretoCifrado: f.SecretoCifrado, SecretoGuardadoEn: f.SecretoGuardadoEn, HuellaTls: f.HuellaTls, HuellaVista: f.HuellaVista, Estado: f.Estado,
		ErrorCodigo: f.ErrorCodigo, ProbadaEn: f.ProbadaEn, ConectadaEn: f.ConectadaEn, DesconectadaDesde: f.DesconectadaDesde, Activa: f.Activa})
}

func (m *Modulo) centralitaParaCambiar(ctx context.Context, q *mensajeriabd.Queries, id int64, version int32) (mensajeriabd.MensajeriaCentralita, error) {
	f, err := q.CentralitaParaActualizar(ctx, mensajeriabd.CentralitaParaActualizarParams{ID: id, Empresa: empresaDeCtx(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		return f, centralitaNoEncontrada()
	}
	if err != nil {
		return f, err
	}
	if f.Version != version {
		return f, nucleo.ConflictoVersion("centralita", id)
	}
	return f, nil
}

func nuevoCambioCentralita(m *Modulo, accion string, activar bool) *auditoria.Caso[EntradaCambioCentralita, Centralita] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCambioCentralita, Centralita]{
		Accion: accion, Permiso: nucleo.PermCentralitaConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambioCentralita) (Centralita, error) {
			q := mensajeriabd.New(t)
			f, err := m.centralitaParaCambiar(ctx, q, in.ID, in.Version)
			if err != nil {
				return Centralita{}, err
			}
			t.FijarEntidad("centralita", f.ID)
			if f.Activa == activar {
				return aCentralita(f), nil
			}
			antes := f
			if activar {
				// Encender exige una prueba que pasó con los datos actuales (solo lectura comprobada).
				if f.ProbadaEn == nil || f.SecretoCifrado == nil || f.HuellaTls == nil {
					return Centralita{}, nucleo.ReglaNegocio("sin_probar", "Prueba la conexión antes de encenderla: Iris comprueba que el usuario solo pueda leer")
				}
				f.Activa, f.Estado, f.ErrorCodigo, f.HuellaVista = true, CentralitaDesconectada, nil, nil
				ahora := m.ahora()
				f.DesconectadaDesde = &ahora
			} else {
				f.Activa, f.Estado, f.ErrorCodigo, f.HuellaVista, f.ConectadaEn, f.DesconectadaDesde = false, CentralitaDesactivada, nil, nil, nil, nil
			}
			if f, err = guardarCentralita(ctx, q, f); err != nil {
				return Centralita{}, err
			}
			campos := t.RegistrarCambios("centralita", f.ID, centralitaAuditable(antes), centralitaAuditable(f))
			if err := notificarMotor(ctx, q, f.ID); err != nil {
				return Centralita{}, err
			}
			if err := t.Emitir("centralita.actualizada", map[string]any{"centralita_id": f.ID, "empresa_id": f.EmpresaID, "campos": campos, "activa": f.Activa}); err != nil {
				return Centralita{}, err
			}
			return aCentralita(f), t.Emitir("centralita.estado_cambiado", map[string]any{"centralita_id": f.ID, "empresa_id": f.EmpresaID, "de": antes.Estado, "a": f.Estado})
		},
	})
}

func nuevoConfirmarHuella(m *Modulo) *auditoria.Caso[EntradaConfirmarHuella, Centralita] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConfirmarHuella, Centralita]{
		Accion: "centralita.confirmar_huella", Permiso: nucleo.PermCentralitaConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConfirmarHuella) (Centralita, error) {
			q := mensajeriabd.New(t)
			f, err := m.centralitaParaCambiar(ctx, q, in.ID, in.Version)
			if err != nil {
				return Centralita{}, err
			}
			t.FijarEntidad("centralita", f.ID)
			h := normalizarHuella(in.Huella)
			if f.HuellaVista == nil || f.ErrorCodigo == nil || *f.ErrorCodigo != "huella_cambiada" {
				return Centralita{}, nucleo.ReglaNegocio("sin_huella_nueva", "La centralita no tiene una huella nueva por confirmar")
			}
			if h != *f.HuellaVista {
				return Centralita{}, nucleo.ReglaNegocio("huella_no_coincide", "La huella no es la que presentó la centralita: vuelve a compararla")
			}
			antes := f
			f.HuellaTls, f.HuellaVista, f.ErrorCodigo, f.Estado = &h, nil, nil, CentralitaDesconectada
			if !f.Activa {
				f.Estado = CentralitaDesactivada
			}
			if f, err = guardarCentralita(ctx, q, f); err != nil {
				return Centralita{}, err
			}
			campos := t.RegistrarCambios("centralita", f.ID, centralitaAuditable(antes), centralitaAuditable(f))
			if err := notificarMotor(ctx, q, f.ID); err != nil {
				return Centralita{}, err
			}
			return aCentralita(f), t.Emitir("centralita.actualizada", map[string]any{"centralita_id": f.ID, "empresa_id": f.EmpresaID, "campos": campos, "activa": f.Activa})
		},
	})
}

// Probar -------------------------------------------------------------------------------------------

// EntradaProbarCentralita es POST /centralitas/probar: prueba datos sin guardarlos (alta o
// edición). Sin secreto, usa el guardado de centralitaId, pero solo si no cambian host, puerto ni
// usuario (la contraseña guardada nunca viaja a otro servidor).
type EntradaProbarCentralita struct {
	CentralitaID *int64  `json:"centralitaId,omitempty" minimum:"1"`
	Host         string  `json:"host" minLength:"1" maxLength:"253"`
	Puerto       int32   `json:"puerto,omitempty" minimum:"1" maximum:"65535"`
	Usuario      string  `json:"usuario" minLength:"1" maxLength:"64" pattern:"^[A-Za-z0-9_.-]{1,64}$"`
	Secreto      *string `json:"secreto,omitempty" minLength:"8" maxLength:"128"`
}

// PruebaCentralita es la respuesta de probar. Un solo «no_conecta» para todo lo que no sea huella o
// permisos: así la prueba no sirve para escanear la red.
type PruebaCentralita struct {
	Estado string `json:"estado" enum:"ok,permisos_de_mas,huella_distinta,no_conecta"`
	// Huella del certificado que presentó la centralita (para compararla con la de Issabel).
	Huella *string `json:"huella" nullable:"true"`
	// Version del AMI (p. ej. «Asterisk Call Manager/7.0.3»).
	Version *string `json:"version" nullable:"true"`
	// AccionesDeMas: las acciones de escritura que el usuario puede ejecutar (con permisos_de_mas).
	AccionesDeMas []string `json:"accionesDeMas"`
	// Centralita al día (solo al probar una guardada).
	Centralita *Centralita `json:"centralita,omitempty"`
}

// ProbarCentralita conecta al AMI fuera de la transacción y deja el resultado auditado.
type ProbarCentralita struct {
	m        *Modulo
	previo   *auditoria.Caso[EntradaProbarCentralita, destinoPrueba]
	guardada *auditoria.Caso[EntradaVerCentralita, destinoPrueba]
	auditar  *auditoria.Caso[pruebaAuditada, struct{}]
}

type destinoPrueba struct {
	opciones ami.Opciones
	id       int64
	version  int32
}

type pruebaAuditada struct {
	CentralitaID *int64 `json:"centralita_id"`
	Host         string `json:"host"`
	Puerto       int32  `json:"puerto"`
	Estado       string `json:"estado"`
}

func nuevoProbarCentralita(m *Modulo) *ProbarCentralita {
	k := &ProbarCentralita{m: m}
	k.previo = auditoria.Nuevo(auditoria.Caso[EntradaProbarCentralita, destinoPrueba]{
		Accion: "centralita.probar", Permiso: nucleo.PermCentralitaConfigurar, AuditarExito: sinAuditar[destinoPrueba],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaProbarCentralita) (destinoPrueba, error) {
			if in.Puerto == 0 {
				in.Puerto = PuertoAMI
			}
			host := strings.ToLower(strings.TrimSpace(in.Host))
			if err := m.validarHostCentralita(host); err != nil {
				return destinoPrueba{}, err
			}
			if err := m.validarPuertoCentralita(in.Puerto); err != nil {
				return destinoPrueba{}, err
			}
			if in.Secreto != nil {
				if err := validarSecretoCentralita(*in.Secreto); err != nil {
					return destinoPrueba{}, err
				}
				return destinoPrueba{opciones: m.opcionesAMI(host, in.Puerto, in.Usuario, *in.Secreto, "")}, nil
			}
			if in.CentralitaID == nil {
				return destinoPrueba{}, nucleo.ReglaNegocio("clave_requerida", "Escribe la contraseña del AMI para probar")
			}
			f, err := mensajeriabd.New(t).CentralitaPorID(ctx, mensajeriabd.CentralitaPorIDParams{ID: *in.CentralitaID, Empresa: empresaDeCtx(ctx)})
			if errors.Is(err, pgx.ErrNoRows) {
				return destinoPrueba{}, centralitaNoEncontrada()
			}
			if err != nil {
				return destinoPrueba{}, err
			}
			if host != f.Host || in.Puerto != f.Puerto || in.Usuario != f.Usuario {
				return destinoPrueba{}, nucleo.ReglaNegocio("clave_requerida", "Cambiaste la dirección, el puerto o el usuario: escribe otra vez la contraseña del AMI")
			}
			s, err := m.descifrarCentralita(f)
			if err != nil {
				return destinoPrueba{}, nucleo.ReglaNegocio("clave_requerida", "Escribe otra vez la contraseña del AMI")
			}
			return destinoPrueba{opciones: m.opcionesAMI(host, in.Puerto, in.Usuario, s, "")}, nil
		},
	})
	k.guardada = auditoria.Nuevo(auditoria.Caso[EntradaVerCentralita, destinoPrueba]{
		Accion: "centralita.probar", Permiso: nucleo.PermCentralitaConfigurar, AuditarExito: sinAuditar[destinoPrueba],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerCentralita) (destinoPrueba, error) {
			f, err := mensajeriabd.New(t).CentralitaPorID(ctx, mensajeriabd.CentralitaPorIDParams{ID: in.ID, Empresa: empresaDeCtx(ctx)})
			if errors.Is(err, pgx.ErrNoRows) {
				return destinoPrueba{}, centralitaNoEncontrada()
			}
			if err != nil {
				return destinoPrueba{}, err
			}
			s, err := m.descifrarCentralita(f)
			if err != nil || f.HuellaTls == nil {
				return destinoPrueba{}, nucleo.ReglaNegocio("clave_requerida", "Escribe otra vez la contraseña del AMI")
			}
			return destinoPrueba{opciones: m.opcionesAMI(f.Host, f.Puerto, f.Usuario, s, *f.HuellaTls), id: f.ID, version: f.Version}, nil
		},
	})
	k.auditar = auditoria.Nuevo(auditoria.Caso[pruebaAuditada, struct{}]{
		Accion: "centralita.probar", Permiso: nucleo.PermCentralitaConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in pruebaAuditada) (struct{}, error) {
			if in.CentralitaID != nil {
				t.FijarEntidad("centralita", *in.CentralitaID)
			}
			t.Anotar(map[string]any{"estado": in.Estado})
			return struct{}{}, nil
		},
	})
	return k
}

// opcionesAMI arma la conexión con las reglas de red de la instalación.
func (m *Modulo) opcionesAMI(host string, puerto int32, usuario, secreto, huella string) ami.Opciones {
	return ami.Opciones{Host: host, Puerto: int(puerto), Usuario: usuario, Secreto: secreto, Huella: huella,
		Plazo: m.cfg.CentralitaPlazo, Control: controlIA(m.cfg.CentralitaLoopback)}
}

// probarAMI conecta, entra, hace Ping y ListCommands. Nunca devuelve el detalle del error.
func probarAMI(ctx context.Context, o ami.Opciones) PruebaCentralita {
	r := PruebaCentralita{AccionesDeMas: []string{}}
	o.Eventos = "off"
	c, err := ami.Conectar(ctx, o)
	if err != nil {
		var eh *ami.ErrHuella
		if errors.As(err, &eh) {
			r.Estado, r.Huella = PruebaHuellaDistinta, &eh.Vista
			return r
		}
		r.Estado = PruebaNoConecta
		return r
	}
	defer c.Cerrar()
	h, v := c.Huella(), c.Version()
	r.Huella, r.Version = &h, &v
	if _, err := c.Accion(ctx, "Ping"); err != nil {
		r.Estado = PruebaNoConecta
		return r
	}
	p, err := c.Accion(ctx, "ListCommands")
	if err != nil {
		r.Estado = PruebaNoConecta
		return r
	}
	if de := ami.PermisosDeMas(p); len(de) > 0 {
		r.Estado, r.AccionesDeMas = PruebaPermisosDeMas, de
		return r
	}
	r.Estado = PruebaOK
	return r
}

// Datos prueba datos sin guardar.
func (k *ProbarCentralita) Datos(ctx context.Context, in EntradaProbarCentralita) (PruebaCentralita, error) {
	d, err := k.previo.Ejecutar(ctx, in)
	if err != nil {
		return PruebaCentralita{}, err
	}
	r := probarAMI(ctx, d.opciones)
	if _, err := k.auditar.Ejecutar(ctx, pruebaAuditada{CentralitaID: in.CentralitaID, Host: d.opciones.Host, Puerto: int32(d.opciones.Puerto), Estado: r.Estado}); err != nil { //nolint:gosec // puerto validado
		return PruebaCentralita{}, err
	}
	return r, nil
}

// Guardada prueba la conexión guardada (con su huella fijada) y, si pasa, deja la fecha de la
// prueba (encender la exige).
func (k *ProbarCentralita) Guardada(ctx context.Context, id int64) (PruebaCentralita, error) {
	d, err := k.guardada.Ejecutar(ctx, EntradaVerCentralita{ID: id})
	if err != nil {
		return PruebaCentralita{}, err
	}
	r := probarAMI(ctx, d.opciones)
	c, err := k.m.Centralita.probada.Ejecutar(ctx, entradaProbada{ID: d.id, Version: d.version, Estado: r.Estado})
	if err != nil {
		return PruebaCentralita{}, err
	}
	r.Centralita = &c
	return r, nil
}

type entradaProbada struct {
	ID      int64  `json:"id"`
	Version int32  `json:"version"`
	Estado  string `json:"estado"`
}

func nuevoMarcarProbada(m *Modulo) *auditoria.Caso[entradaProbada, Centralita] {
	return auditoria.Nuevo(auditoria.Caso[entradaProbada, Centralita]{
		Accion: "centralita.probar", Permiso: nucleo.PermCentralitaConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaProbada) (Centralita, error) {
			q := mensajeriabd.New(t)
			f, err := m.centralitaParaCambiar(ctx, q, in.ID, in.Version)
			if err != nil {
				return Centralita{}, err
			}
			t.FijarEntidad("centralita", f.ID)
			t.Anotar(map[string]any{"estado": in.Estado})
			ahora := m.ahora()
			switch in.Estado {
			case PruebaOK:
				f.ProbadaEn = &ahora
			case PruebaPermisosDeMas:
				// Con permisos de más no se enciende: se apaga si estaba encendida.
				f.ProbadaEn = nil
				if f.Activa {
					f.Activa, f.Estado, f.ErrorCodigo = false, CentralitaError, texto("permisos_de_mas")
				}
			default:
				return aCentralita(f), nil
			}
			if f, err = guardarCentralita(ctx, q, f); err != nil {
				return Centralita{}, err
			}
			if !f.Activa {
				if err := notificarMotor(ctx, q, f.ID); err != nil {
					return Centralita{}, err
				}
			}
			return aCentralita(f), nil
		},
	})
}

// Estado que deja el motor --------------------------------------------------------------------------

type entradaEstadoCentralita struct {
	ID      int64   `json:"id"`
	Version int32   `json:"version"`
	Estado  string  `json:"estado"`
	Codigo  *string `json:"codigo"`
	Huella  *string `json:"huella"`
	Desde   *time.Time
}

func nuevoFijarEstadoCentralita(m *Modulo) *auditoria.Caso[entradaEstadoCentralita, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[entradaEstadoCentralita, struct{}]{
		Accion: "centralita.estado", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaEstadoCentralita) (struct{}, error) {
			if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
				return struct{}{}, centralitaNoEncontrada()
			}
			q := mensajeriabd.New(t)
			f, err := q.CentralitaDelMotorParaActualizar(ctx, in.ID)
			if err != nil {
				return struct{}{}, err
			}
			if f.Version != in.Version || !f.Activa {
				return struct{}{}, nil // cambió la configuración: el motor se reinicia
			}
			if f.Estado == in.Estado && derefTexto(f.ErrorCodigo) == derefTexto(in.Codigo) {
				return struct{}{}, nil
			}
			ahora := m.ahora()
			p := mensajeriabd.FijarEstadoCentralitaParams{ID: f.ID, Version: f.Version, Estado: in.Estado, ErrorCodigo: in.Codigo, ConectadaEn: f.ConectadaEn,
				DesconectadaDesde: f.DesconectadaDesde, HuellaVista: f.HuellaVista}
			switch in.Estado {
			case CentralitaConectada:
				p.ConectadaEn, p.DesconectadaDesde, p.HuellaVista, p.ErrorCodigo = &ahora, nil, nil, nil
			default:
				if p.DesconectadaDesde == nil {
					p.DesconectadaDesde = &ahora
					if in.Desde != nil {
						p.DesconectadaDesde = in.Desde
					}
				}
				p.HuellaVista = in.Huella
			}
			if _, err := q.FijarEstadoCentralita(ctx, p); err != nil {
				return struct{}{}, err
			}
			t.FijarEmpresa(derefEmpresa(f.EmpresaID))
			t.FijarEntidad("centralita", f.ID)
			t.Anotar(map[string]any{"de": f.Estado, "a": in.Estado, "codigo": in.Codigo})
			return struct{}{}, t.Emitir("centralita.estado_cambiado", map[string]any{"centralita_id": f.ID, "empresa_id": f.EmpresaID, "de": f.Estado, "a": in.Estado})
		},
	})
}
