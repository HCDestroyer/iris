// Package legal guarda los datos legales del titular de la instalación (quién opera Iris: razón
// social, nombre comercial, contacto de privacidad, dirección y país) y los sirve, sin sesión, a
// las páginas públicas de privacidad, términos y eliminación de datos que Meta pide para revisar
// y publicar la app de WhatsApp (contratos/legal.md).
//
// Nada de esto va en el .env ni en el código: se configura en el panel (Administración › Datos
// legales). Todo lo que se guarda aquí es público por definición.
package legal

import (
	"context"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	legalbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/legal/bd"
)

// Campos obligatorios para que las páginas públicas estén listas para Meta, en el orden en que se
// le piden a la persona. El teléfono es opcional.
var obligatorios = []string{"razonSocial", "nombreComercial", "correoPrivacidad", "direccion", "pais"}

// VigenciaCachePublica: cuánto guarda el proceso la respuesta pública. La lectura es anónima y sin
// límite de peticiones: con la caché, un bucle de peticiones no llega a la base ni ocupa el pool.
// Guardar la invalida en este proceso; en otras réplicas vence sola.
const VigenciaCachePublica = 60 * time.Second

// Config del módulo.
type Config struct {
	// Reloj (pruebas).
	Reloj func() time.Time
}

// cachePublica guarda la última respuesta pública. `generacion` sube con cada Guardar: una lectura
// que empezó antes de un cambio no deja en la caché el dato viejo.
type cachePublica struct {
	mu         sync.Mutex
	datos      DatosPublicos
	hasta      time.Time
	generacion uint64
}

// Modulo reúne los casos de uso de los datos legales.
type Modulo struct {
	cfg     Config
	Ver     *auditoria.Caso[struct{}, DatosLegales]
	Guardar *auditoria.Caso[CuerpoGuardar, DatosLegales]
	Publico *auditoria.Caso[struct{}, DatosPublicos]

	cache    cachePublica
	lecturas atomic.Int64
}

// LeerPublico devuelve los datos públicos desde la caché del proceso y solo va a la base (caso
// Publico) cuando venció o Guardar la invalidó.
func (m *Modulo) LeerPublico(ctx context.Context) (DatosPublicos, error) {
	ahora := m.cfg.Reloj()
	m.cache.mu.Lock()
	if ahora.Before(m.cache.hasta) {
		d := m.cache.datos
		m.cache.mu.Unlock()
		return d, nil
	}
	generacion := m.cache.generacion
	m.cache.mu.Unlock()

	d, err := m.Publico.Ejecutar(ctx, struct{}{})
	if err != nil {
		return DatosPublicos{}, err
	}
	m.cache.mu.Lock()
	if m.cache.generacion == generacion {
		m.cache.datos, m.cache.hasta = d, ahora.Add(VigenciaCachePublica)
	}
	m.cache.mu.Unlock()
	return d, nil
}

func (m *Modulo) invalidarCache() {
	m.cache.mu.Lock()
	m.cache.generacion++
	m.cache.hasta = time.Time{}
	m.cache.mu.Unlock()
}

// LecturasPublicasEnBase cuenta las veces que la lectura pública consultó la base (pruebas).
func (m *Modulo) LecturasPublicasEnBase() int64 { return m.lecturas.Load() }

// Nuevo arma el módulo.
func Nuevo(cfg Config) *Modulo {
	if cfg.Reloj == nil {
		cfg.Reloj = time.Now
	}
	m := &Modulo{cfg: cfg}
	m.Ver = m.nuevoVer()
	m.Guardar = m.nuevoGuardar()
	m.Publico = m.nuevoPublico()
	return m
}

func (m *Modulo) ahora() time.Time { return m.cfg.Reloj().UTC().Truncate(time.Microsecond) }

// CamposLegales son los campos del formulario.
type CamposLegales struct {
	RazonSocial      string `json:"razonSocial" maxLength:"200" doc:"Nombre legal de quien opera la instalación (el responsable de los datos)"`
	NombreComercial  string `json:"nombreComercial" maxLength:"120" doc:"Nombre con que los clientes conocen a la empresa"`
	CorreoPrivacidad string `json:"correoPrivacidad" maxLength:"254" doc:"Correo al que se piden el acceso, la corrección y la eliminación de datos. Una sola dirección"`
	Telefono         string `json:"telefono" maxLength:"40" doc:"Opcional. Dígitos, espacios, +, guiones y paréntesis"`
	Direccion        string `json:"direccion" maxLength:"300" doc:"Dirección física del titular, en una línea"`
	Pais             string `json:"pais" maxLength:"80" doc:"País del titular: sus leyes son la ley aplicable de los términos"`
}

// DatosLegales es lo que ve el panel.
type DatosLegales struct {
	Configurado bool `json:"configurado" doc:"Ya se guardaron datos alguna vez"`
	CamposLegales
	Version       int32      `json:"version" doc:"0 si nunca se guardó"`
	ActualizadoEn *time.Time `json:"actualizadoEn" nullable:"true" doc:"Último cambio: es la fecha de «última actualización» de las páginas públicas"`
	Completo      bool       `json:"completo" doc:"Están todos los datos obligatorios: las páginas públicas están listas para Meta"`
	Faltan        []string   `json:"faltan" doc:"Campos obligatorios vacíos (razonSocial, nombreComercial, correoPrivacidad, direccion, pais), en el orden del formulario"`
}

// CuerpoGuardar es POST /datos-legales/guardar.
type CuerpoGuardar struct {
	Version int32 `json:"version" minimum:"0" doc:"La que leíste; 0 la primera vez"`
	CamposLegales
}

// DatosPublicos es lo que leen las páginas públicas (sin sesión). Solo lo que el titular publica.
type DatosPublicos struct {
	NombreComercial string     `json:"nombreComercial"`
	RazonSocial     string     `json:"razonSocial"`
	Correo          string     `json:"correo" doc:"Correo de contacto de privacidad"`
	Telefono        string     `json:"telefono"`
	Direccion       string     `json:"direccion"`
	Pais            string     `json:"pais"`
	ActualizadoEn   *time.Time `json:"actualizadoEn" nullable:"true"`
	Completo        bool       `json:"completo" doc:"false: faltan datos obligatorios; las páginas se muestran sin huecos, con lo que haya"`
}

func campo(nombre, mensaje, regla string) *nucleo.Problema {
	return nucleo.Validacion(nucleo.ErrorCampo{Field: nombre, Message: mensaje, Rule: regla})
}

// conControl: saltos de línea, caracteres de control, de formato (invisibles o que cambian el
// sentido del texto: U+200B–200F, U+202A–202E, U+2066–2069…) o separadores de línea y de párrafo.
// Los datos van en una línea y se pintan tal cual en la página pública y en su título.
func conControl(s string) bool {
	return !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
	})
}

var reTelefono = regexp.MustCompile(`^\+?[0-9][0-9 ().-]{5,38}$`)

// reCorreo: la parte local solo con letras, dígitos y . _ + -. Nada de ?, &, =, % ni comillas: el
// correo va en un enlace mailto: de la página pública y esos caracteres le agregarían campos (cc, body).
var reCorreo = regexp.MustCompile(`^[A-Za-z0-9._+-]+@[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$`)

// unaDireccion: una sola dirección de correo, sin nombre.
func unaDireccion(s string) bool {
	if !reCorreo.MatchString(s) {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && a.Name == ""
}

// validar normaliza (espacios) y valida el formato. Todos los campos pueden ir vacíos: se guarda lo
// que haya y `faltan` dice qué falta para estar listo.
func validar(c *CamposLegales) error {
	textos := []struct {
		nombre string
		valor  *string
		max    int
	}{
		{"razonSocial", &c.RazonSocial, 200}, {"nombreComercial", &c.NombreComercial, 120}, {"correoPrivacidad", &c.CorreoPrivacidad, 254},
		{"telefono", &c.Telefono, 40}, {"direccion", &c.Direccion, 300}, {"pais", &c.Pais, 80},
	}
	for _, t := range textos {
		*t.valor = strings.TrimSpace(*t.valor)
		if conControl(*t.valor) {
			return campo(t.nombre, "Escríbelo en una sola línea, sin saltos", "una_linea")
		}
		if utf8.RuneCountInString(*t.valor) > t.max {
			return campo(t.nombre, "Es demasiado largo", "maximo")
		}
	}
	if c.CorreoPrivacidad != "" && !unaDireccion(c.CorreoPrivacidad) {
		return campo("correoPrivacidad", "Escribe un solo correo, como privacidad@tuempresa.com", "correo")
	}
	if c.Telefono != "" && !reTelefono.MatchString(c.Telefono) {
		return campo("telefono", "Solo dígitos, espacios, +, guiones y paréntesis", "telefono")
	}
	return nil
}

func faltantes(c CamposLegales) []string {
	valores := map[string]string{"razonSocial": c.RazonSocial, "nombreComercial": c.NombreComercial, "correoPrivacidad": c.CorreoPrivacidad,
		"direccion": c.Direccion, "pais": c.Pais}
	faltan := []string{}
	for _, nombre := range obligatorios {
		if valores[nombre] == "" {
			faltan = append(faltan, nombre)
		}
	}
	return faltan
}

func aCampos(f legalbd.NucleoDatosLegale) CamposLegales {
	return CamposLegales{RazonSocial: f.RazonSocial, NombreComercial: f.NombreComercial, CorreoPrivacidad: f.CorreoPrivacidad,
		Telefono: f.Telefono, Direccion: f.Direccion, Pais: f.Pais}
}

func vista(f *legalbd.NucleoDatosLegale) DatosLegales {
	if f == nil {
		return DatosLegales{Faltan: faltantes(CamposLegales{})}
	}
	c := aCampos(*f)
	faltan := faltantes(c)
	return DatosLegales{Configurado: true, CamposLegales: c, Version: f.Version, ActualizadoEn: &f.UpdatedAt, Completo: len(faltan) == 0, Faltan: faltan}
}

// alcance: los datos legales son de la instalación. Los ve y los cambia una persona que opera sin
// límite de empresa (empresas.todas, o una instalación sin empresas), como el correo global.
func alcance(ctx context.Context) error {
	if a := contexto.ActorDe(ctx); a.Tipo != contexto.ActorUsuario || a.ID == 0 {
		return nucleo.NoEncontrado("Los datos legales no existen")
	}
	if e := contexto.EmpresasDe(ctx); e.Todas || e.Estado == contexto.EmpresaSinConector {
		return nil
	}
	return nucleo.Prohibido("Los datos legales de la instalación los configura quien opera en todas las empresas")
}

func sinFilas(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func (m *Modulo) nuevoVer() *auditoria.Caso[struct{}, DatosLegales] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, DatosLegales]{
		Accion: "datos_legales.ver", Permiso: nucleo.PermDatosLegalesVer, SinEmpresa: true,
		AuditarExito: func(DatosLegales) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (DatosLegales, error) {
			if err := alcance(ctx); err != nil {
				return DatosLegales{}, err
			}
			f, err := legalbd.New(t).DatosLegalesGlobales(ctx)
			if sinFilas(err) {
				return vista(nil), nil
			}
			if err != nil {
				return DatosLegales{}, err
			}
			return vista(&f), nil
		},
	})
}

func auditable(c CamposLegales) map[string]any {
	return map[string]any{"razon_social": c.RazonSocial, "nombre_comercial": c.NombreComercial, "correo_privacidad": c.CorreoPrivacidad,
		"telefono": c.Telefono, "direccion": c.Direccion, "pais": c.Pais}
}

func (m *Modulo) nuevoGuardar() *auditoria.Caso[CuerpoGuardar, DatosLegales] {
	return auditoria.Nuevo(auditoria.Caso[CuerpoGuardar, DatosLegales]{
		Accion: "datos_legales.guardar", Permiso: nucleo.PermDatosLegalesConfigurar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in CuerpoGuardar) (DatosLegales, error) {
			if err := alcance(ctx); err != nil {
				return DatosLegales{}, err
			}
			t.FijarEmpresa(0)
			q := legalbd.New(t)
			antes, err := q.DatosLegalesGlobalesParaActualizar(ctx)
			nuevo := sinFilas(err)
			if err != nil && !nuevo {
				return DatosLegales{}, err
			}
			if (nuevo && in.Version != 0) || (!nuevo && antes.Version != in.Version) {
				return DatosLegales{}, nucleo.ConflictoVersion("datos_legales", "global")
			}
			c := in.CamposLegales
			if err := validar(&c); err != nil {
				return DatosLegales{}, err
			}
			if !nuevo && aCampos(antes) == c {
				// Nada cambió: la fecha de «última actualización» de las páginas públicas no se mueve.
				t.FijarEntidad("datos_legales", antes.ID)
				return vista(&antes), nil
			}
			ahora := m.ahora()
			var f legalbd.NucleoDatosLegale
			if nuevo {
				f, err = q.InsertarDatosLegalesGlobales(ctx, legalbd.InsertarDatosLegalesGlobalesParams{RazonSocial: c.RazonSocial, NombreComercial: c.NombreComercial,
					CorreoPrivacidad: c.CorreoPrivacidad, Telefono: c.Telefono, Direccion: c.Direccion, Pais: c.Pais, Ahora: ahora})
				if sinFilas(err) {
					// Otra sesión los creó entre la lectura y la escritura.
					return DatosLegales{}, nucleo.ConflictoVersion("datos_legales", "global")
				}
			} else {
				f, err = q.ActualizarDatosLegales(ctx, legalbd.ActualizarDatosLegalesParams{ID: antes.ID, RazonSocial: c.RazonSocial, NombreComercial: c.NombreComercial,
					CorreoPrivacidad: c.CorreoPrivacidad, Telefono: c.Telefono, Direccion: c.Direccion, Pais: c.Pais, Ahora: ahora})
			}
			if err != nil {
				return DatosLegales{}, err
			}
			var previo any
			if !nuevo {
				previo = auditable(aCampos(antes))
			}
			campos := t.RegistrarCambios("datos_legales", f.ID, previo, auditable(c))
			if campos == nil {
				campos = []string{}
			}
			v := vista(&f)
			// Con el cambio confirmado, la próxima lectura pública de este proceso va a la base.
			t.AlConfirmar(func(context.Context) { m.invalidarCache() })
			if err := t.Emitir("datos_legales.cambiados", map[string]any{"campos": campos, "completo": v.Completo, "faltan": v.Faltan}); err != nil {
				return DatosLegales{}, err
			}
			return v, nil
		},
	})
}

func (m *Modulo) nuevoPublico() *auditoria.Caso[struct{}, DatosPublicos] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, DatosPublicos]{
		// Lectura pública y repetida (rastreadores): no deja fila de auditoría.
		Accion: "datos_legales.ver_publico", Acceso: auditoria.Publico, AuditarExito: func(DatosPublicos) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (DatosPublicos, error) {
			m.lecturas.Add(1)
			f, err := legalbd.New(t).DatosLegalesGlobales(ctx)
			if sinFilas(err) {
				return DatosPublicos{}, nil
			}
			if err != nil {
				return DatosPublicos{}, err
			}
			v := vista(&f)
			return DatosPublicos{NombreComercial: f.NombreComercial, RazonSocial: f.RazonSocial, Correo: f.CorreoPrivacidad, Telefono: f.Telefono,
				Direccion: f.Direccion, Pais: f.Pais, ActualizadoEn: &f.UpdatedAt, Completo: v.Completo}, nil
		},
	})
}
