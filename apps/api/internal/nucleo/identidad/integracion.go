package identidad

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
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
	// Con empresas registradas, una integración sin empresas no operaría en ninguna: obligatorias.
	if len(in.Empresas) == 0 {
		bd := nucleo.BDDe(ctx)
		if bd == nil {
			return IntegracionPreparada{}, nucleo.ErrSinBD
		}
		hay, err := identidadbd.New(bd).HayEmpresas(ctx)
		if err != nil {
			return IntegracionPreparada{}, err
		}
		if hay {
			return IntegracionPreparada{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "empresas",
				Message: "Hay empresas registradas: indica en cuáles opera la integración (--empresa)", Rule: "requerido"})
		}
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
		// Sus roles deben ser exactamente {rol del sistema}, sin alcance: no se emite un token
		// para un usuario con más permisos que los del contrato.
		soloSuRol := len(u.Roles) == 1 && u.Roles[0].Rol == rol && u.Roles[0].ZonaID == nil && u.Roles[0].CuadrillaID == nil
		if u.Tipo != "integracion" || !u.Activo || !soloSuRol {
			return Usuario{}, nucleo.ReglaNegocio("usuario_no_es_integracion",
				"El correo "+correo+" es de un usuario que no es una integración activa con solo el rol "+rol+
					" (tiene otro tipo, está inactivo o tiene otros roles); corrige sus roles o usa otro --correo")
		}
		return u, nil
	}
	return Usuario{}, nucleo.NoEncontrado("No se encontró el usuario " + correo)
}

func esCodigo(err error, codigo string) bool {
	if err == nil {
		return false
	}
	p, _ := nucleo.AProblema(err)
	return p != nil && p.Codigo() == codigo
}

// Token del canje de un enlace de conexión (contratos Argos/Horus §1.2) ---------------------
//
// Al conectar con un enlace del otro sistema, Iris le entrega un iris_pat_ de su usuario de
// integración. Nace con vencimiento (VigenciaTokenCanje): si el conector no se activa a tiempo
// —p. ej. el enlace era de un sitio falso que respondió 200— muere solo. Al activar el conector
// se confirma (sin vencimiento); al desactivarlo o reemplazarlo, se revoca.

// VigenciaTokenCanje es la vida del iris_pat_ entregado en un canje mientras no se activa el
// conector.
const VigenciaTokenCanje = 15 * time.Minute

// EmitirTokenCanje emite el iris_pat_ del canje con el actor de ctx (quien conecta, que el
// conector exige con empresas.todas global): queda en la auditoría con su usuario y request_id.
// Si el usuario de integración del sistema ya existe, sus empresas no se tocan; si no, se crea
// con todas las empresas registradas (en una instalación nueva, ninguna). Devuelve el id del
// token y su valor (se muestra una sola vez).
func (m *Modulo) EmitirTokenCanje(ctx context.Context, sistema string, expira time.Time) (int64, string, error) {
	rol, ok := RolesIntegracion[sistema]
	if !ok {
		return 0, "", nucleo.Validacion(nucleo.ErrorCampo{Field: "sistema", Message: "Debe ser argosisp u horus", Rule: "enum"})
	}
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return 0, "", nucleo.ErrSinBD
	}
	correo := "integracion." + sistema + "@iris.local"
	u, err := m.integracionExistente(ctx, correo, rol)
	switch {
	case err == nil:
	case esCodigo(err, "no_encontrado"):
		empresas, err := todasLasEmpresas(ctx, identidadbd.New(bd))
		if err != nil {
			return 0, "", err
		}
		if u, err = m.CrearUsuario.Ejecutar(ctx, EntradaCrearUsuario{
			Nombre: nombresIntegracion[sistema], Correo: correo, Tipo: "integracion", Roles: []AsignacionRol{{Rol: rol}}, Empresas: empresas,
		}); err != nil {
			return 0, "", err
		}
	default:
		return 0, "", err
	}
	t, err := m.CrearToken.Ejecutar(ctx, EntradaCrearToken{
		UsuarioID: u.ID, Nombre: sistema + " (canje " + time.Now().UTC().Format("2006-01-02 15:04") + ")", ExpiraEn: &expira,
	})
	if err != nil {
		return 0, "", err
	}
	return t.Token.ID, t.Valor, nil
}

func todasLasEmpresas(ctx context.Context, q *identidadbd.Queries) ([]int64, error) {
	var empresas []int64
	for despues := int64(0); ; {
		filas, err := q.ListarEmpresas(ctx, identidadbd.ListarEmpresasParams{DespuesDe: despues, Todas: true, Limite: 500})
		if err != nil {
			return nil, err
		}
		for _, f := range filas {
			empresas = append(empresas, f.ID)
		}
		if len(filas) < 500 {
			return empresas, nil
		}
		despues = filas[len(filas)-1].ID
	}
}

// ConfirmarTokenCanje quita el vencimiento del iris_pat_ de un canje al activar su conector. Corre
// en la transacción de la activación (ctx). 422 token_entregado_vencido si ya venció o se revocó:
// hay que volver a conectar con un enlace nuevo.
func (m *Modulo) ConfirmarTokenCanje(ctx context.Context, id int64) error {
	db := nucleo.DBDe(ctx)
	if db == nil {
		return nucleo.ErrSinBD
	}
	n, err := identidadbd.New(db).ConfirmarTokenEntregado(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return nucleo.ReglaNegocio("token_entregado_vencido",
			"El token que Iris entregó en el canje venció o se revocó: vuelve a conectar con un enlace nuevo")
	}
	return nil
}

// RevocarTokenIntegracion revoca un token entregado en un canje (canje fallido, conector
// desactivado o reemplazado). Con el actor de ctx (el worker usa el actor sistema). Revocar uno
// ya revocado no es un error.
func (m *Modulo) RevocarTokenIntegracion(ctx context.Context, id int64, motivo string) error {
	_, err := m.RevocarToken.Ejecutar(ctx, EntradaRevocarToken{ID: id, Motivo: motivo})
	if esCodigo(err, "token_revocado") {
		return nil
	}
	return err
}
