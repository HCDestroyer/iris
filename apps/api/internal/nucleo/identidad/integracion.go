package identidad

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Usuarios de integración (Argos, Horus): un usuario de tipo integracion con el rol base de su
// sistema (integracion_argos, integracion_horus; nucleo.RolesBase), sus empresas y un token
// iris_pat_ que se entrega al otro sistema. Lo usa la consola (iris integracion crear) con el
// actor sistema, por los mismos casos de uso (auditados), sin pasar por el rol admin.

// RolesIntegracion: rol base de cada sistema.
var RolesIntegracion = map[string]string{"argosisp": "integracion_argos", "horus": "integracion_horus"}

var nombresIntegracion = map[string]string{"argosisp": "ArgosISP", "horus": "Horus Flow"}

// EntradaPrepararIntegracion: sistema (argosisp | horus), empresas en que opera (ids de
// Argos; obligatorias si hay empresas registradas) y, opcionales, correo (por defecto
// integracion.<sistema>@iris.local), nombre y vencimiento del token.
type EntradaPrepararIntegracion struct {
	Sistema  string
	Empresas []int64
	Correo   string
	Nombre   string
	ExpiraEn *time.Time
}

// IntegracionPreparada es el usuario (creado ahora o ya existente) y su token nuevo.
type IntegracionPreparada struct {
	Usuario Usuario
	Creado  bool
	Token   TokenCreado
}

// PrepararIntegracion crea (o reutiliza, por correo) el usuario de integración del sistema con
// su rol y sus empresas y le emite un token nuevo. Reutilizar exige que sea de tipo integracion
// y tenga el rol del sistema; las empresas que falten se le asignan.
func (m *Modulo) PrepararIntegracion(ctx context.Context, in EntradaPrepararIntegracion) (IntegracionPreparada, error) {
	rol, ok := RolesIntegracion[in.Sistema]
	if !ok {
		return IntegracionPreparada{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "sistema", Message: "Debe ser argosisp u horus", Rule: "enum"})
	}
	correo := strings.ToLower(strings.TrimSpace(in.Correo))
	if correo == "" {
		correo = "integracion." + in.Sistema + "@iris.local"
	}
	nombre := in.Nombre
	if nombre == "" {
		nombre = nombresIntegracion[in.Sistema]
	}
	salida := IntegracionPreparada{}
	u, err := m.CrearUsuario.Ejecutar(ctx, EntradaCrearUsuario{
		Nombre: nombre, Correo: correo, Tipo: "integracion", Roles: []AsignacionRol{{Rol: rol}}, Empresas: in.Empresas,
	})
	switch {
	case err == nil:
		salida.Creado = true
	case esCodigo(err, "correo_en_uso"):
		if u, err = m.integracionExistente(ctx, correo, rol); err != nil {
			return salida, err
		}
		for _, e := range in.Empresas {
			if slices.Contains(u.Empresas, e) {
				continue
			}
			if u, err = m.AsignarEmpresaUsuario.Ejecutar(ctx, EntradaEmpresaUsuario{UsuarioID: u.ID, EmpresaID: e}); err != nil {
				return salida, err
			}
		}
	default:
		return salida, err
	}
	salida.Usuario = u
	salida.Token, err = m.CrearToken.Ejecutar(ctx, EntradaCrearToken{
		UsuarioID: u.ID, Nombre: in.Sistema + " " + time.Now().UTC().Format("2006-01-02 15:04"), ExpiraEn: in.ExpiraEn,
	})
	return salida, err
}

func (m *Modulo) integracionExistente(ctx context.Context, correo, rol string) (Usuario, error) {
	p, err := m.ListarUsuarios.Ejecutar(ctx, EntradaListarUsuarios{Q: correo})
	if err != nil {
		return Usuario{}, err
	}
	for _, u := range p.Data {
		if u.Correo != correo {
			continue
		}
		tieneRol := slices.ContainsFunc(u.Roles, func(a AsignacionRol) bool { return a.Rol == rol })
		if u.Tipo != "integracion" || !u.Activo || !tieneRol {
			return Usuario{}, nucleo.ReglaNegocio("usuario_no_es_integracion",
				"El correo "+correo+" es de un usuario que no es la integración activa con el rol "+rol+"; usa otro --correo")
		}
		return u, nil
	}
	return Usuario{}, nucleo.NoEncontrado("No se encontró el usuario " + correo)
}

func esCodigo(err error, codigo string) bool {
	p, _ := nucleo.AProblema(err)
	return p.Codigo() == codigo
}
