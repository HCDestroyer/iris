// Package clave rota IRIS_CLAVE_CIFRADO: vuelve a cifrar con la clave nueva todo lo que Iris
// guarda cifrado con la anterior (IRIS_CLAVE_CIFRADO_ANTERIOR). Lo usa `iris clave rotar`;
// el procedimiento está en docs/runbooks/rotar-clave-cifrado.md.
package clave

import (
	"context"
	"fmt"
	"strings"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	clavebd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/clave/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// Columnas cifradas con IRIS_CLAVE_CIFRADO (esquema.tabla.columna). Toda columna nueva que
// guarde algo cifrado con nucleo.Cifrador debe sumarse aquí y en Rotar (la prueba
// TestTodasLasColumnasCifradasSeRotan lo exige).
const (
	ColumnaTOTP           = "nucleo.usuarios.totp_secreto"
	ColumnaTokenConector  = "integracion.conectores.token_cifrado"
	ColumnaSecretoWebhook = "integracion.conectores.secreto_webhook_cifrado"
	ColumnaIMAP           = "soporte.buzones.imap_contrasena"
	ColumnaSMTP           = "soporte.buzones.smtp_contrasena"
	// ColumnaVerificador: code_verifier de las autorizaciones «Conectar con un botón» sin usar.
	ColumnaVerificador = "integracion.autorizaciones.verificador_cifrado"
)

// Columnas en el orden en que se rotan y se informan.
var Columnas = []string{ColumnaTOTP, ColumnaTokenConector, ColumnaSecretoWebhook, ColumnaIMAP, ColumnaSMTP, ColumnaVerificador}

// datoAsociadoBuzon es el de internal/soporte (buzones.go, de I2): "soporte.buzon:<id>:imap|smtp".
// La prueba de la rotación lo verifica leyendo el buzón con el módulo de soporte.
func datoAsociadoBuzon(id int64, servidor string) []byte {
	return fmt.Appendf(nil, "soporte.buzon:%d:%s", id, servidor)
}

// EntradaRotar: Simular cuenta sin escribir; IgnorarIlegibles rota lo legible y deja como está
// lo que no se descifra con ninguna de las dos claves (si no, la rotación falla sin escribir).
type EntradaRotar struct {
	Simular          bool `json:"simular"`
	IgnorarIlegibles bool `json:"ignorarIlegibles"`
}

// Conteo de una columna: valores no nulos (Total), los que estaban con la clave anterior
// (Rotadas: re-cifrados, o por re-cifrar si es simulación), los que ya estaban con la nueva
// (YaRotadas) y los que no se descifran con ninguna (Ilegibles).
type Conteo struct {
	Columna   string `json:"columna"`
	Total     int    `json:"total"`
	Rotadas   int    `json:"rotadas"`
	YaRotadas int    `json:"yaRotadas"`
	Ilegibles int    `json:"ilegibles"`
}

// Ilegible identifica un valor que no se descifra con ninguna clave (sin el valor).
type Ilegible struct {
	Columna string `json:"columna"`
	ID      int64  `json:"id"`
}

// Resultado de la rotación.
type Resultado struct {
	Simulada  bool       `json:"simulada"`
	Columnas  []Conteo   `json:"columnas"`
	Ilegibles []Ilegible `json:"ilegibles"`
}

// Rotadas suma los valores re-cifrados (o por re-cifrar) de todas las columnas.
func (r Resultado) Rotadas() int {
	n := 0
	for _, c := range r.Columnas {
		n += c.Rotadas
	}
	return n
}

// Modulo arma el caso de uso con el cifrador de transición (clave nueva + anterior).
type Modulo struct {
	Rotar *auditoria.Caso[EntradaRotar, Resultado]
}

// Nuevo arma el módulo. c debe conocer la clave anterior (nucleo.NuevoCifrador(nueva, anterior)).
func Nuevo(c *nucleo.Cifrador) *Modulo {
	return &Modulo{Rotar: nuevoRotar(c)}
}

func nuevoRotar(c *nucleo.Cifrador) *auditoria.Caso[EntradaRotar, Resultado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRotar, Resultado]{
		Accion: "clave.rotar",
		// Acción de sistema: no es un permiso del catálogo (nadie la tiene por un rol); solo la
		// corre la consola con acceso a la base y a las dos claves.
		Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRotar) (Resultado, error) {
			if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
				return Resultado{}, nucleo.Prohibido("La rotación de la clave solo se corre desde la consola (iris clave rotar)")
			}
			if c == nil {
				return Resultado{}, nucleo.ReglaNegocio("cifrado_no_configurado", "Falta IRIS_CLAVE_CIFRADO (la clave nueva)")
			}
			if !c.TieneAnterior() {
				return Resultado{}, nucleo.ReglaNegocio("sin_clave_anterior", "Falta IRIS_CLAVE_CIFRADO_ANTERIOR (la clave que se rota)")
			}
			r := &rotador{c: c, res: Resultado{Simulada: in.Simular}, conteos: map[string]*Conteo{}}
			for _, col := range Columnas {
				r.conteos[col] = &Conteo{Columna: col}
			}
			q := clavebd.New(t)
			if err := r.totp(ctx, q, in.Simular); err != nil {
				return Resultado{}, err
			}
			if err := r.conectores(ctx, q, in.Simular); err != nil {
				return Resultado{}, err
			}
			if err := r.buzones(ctx, q, in.Simular); err != nil {
				return Resultado{}, err
			}
			if err := r.autorizaciones(ctx, q, in.Simular); err != nil {
				return Resultado{}, err
			}
			for _, col := range Columnas {
				r.res.Columnas = append(r.res.Columnas, *r.conteos[col])
			}
			if r.res.Ilegibles == nil {
				r.res.Ilegibles = []Ilegible{}
			}
			// Solo conteos e ids: nunca valores, ni cifrados ni en claro.
			t.Anotar(map[string]any{"columnas": r.res.Columnas, "ilegibles": r.res.Ilegibles, "rotadas": r.res.Rotadas()})
			if len(r.res.Ilegibles) > 0 && !in.Simular && !in.IgnorarIlegibles {
				return Resultado{}, nucleo.ReglaNegocio("cifrados_ilegibles", fmt.Sprintf(
					"%d valores no se descifran ni con la clave nueva ni con la anterior (%s); no se escribió nada. "+
						"Revisa las claves o corre con --ignorar-ilegibles (quedan como están y hay que reescribirlos a mano)",
					len(r.res.Ilegibles), resumirIlegibles(r.res.Ilegibles)))
			}
			return r.res, nil
		},
	})
}

func resumirIlegibles(ils []Ilegible) string {
	const maximo = 20
	partes := make([]string, 0, min(len(ils), maximo))
	for i, il := range ils {
		if i == maximo {
			partes = append(partes, fmt.Sprintf("y %d más", len(ils)-maximo))
			break
		}
		partes = append(partes, fmt.Sprintf("%s id %d", il.Columna, il.ID))
	}
	return strings.Join(partes, ", ")
}

type rotador struct {
	c       *nucleo.Cifrador
	res     Resultado
	conteos map[string]*Conteo
}

// valor decide qué hacer con un valor cifrado: lo deja si ya está con la clave nueva o no se
// descifra (ilegible), y lo re-cifra con la nueva si estaba con la anterior. cambio indica si
// hay que escribirlo.
func (r *rotador) valor(col string, id int64, actual, ad []byte) (nuevo []byte, cambio bool, err error) {
	if actual == nil {
		return nil, false, nil
	}
	cnt := r.conteos[col]
	cnt.Total++
	plano, conAnterior, err := r.c.DescifrarConOrigen(actual, ad)
	switch {
	case err != nil:
		cnt.Ilegibles++
		r.res.Ilegibles = append(r.res.Ilegibles, Ilegible{Columna: col, ID: id})
		return actual, false, nil
	case !conAnterior:
		cnt.YaRotadas++
		return actual, false, nil
	}
	defer clear(plano)
	cnt.Rotadas++
	nuevo, err = r.c.Cifrar(plano, ad)
	if err != nil {
		return nil, false, err
	}
	return nuevo, true, nil
}

func (r *rotador) totp(ctx context.Context, q *clavebd.Queries, simular bool) error {
	filas, err := q.TOTPCifrados(ctx)
	if err != nil {
		return err
	}
	for _, f := range filas {
		nuevo, cambio, err := r.valor(ColumnaTOTP, f.ID, f.TotpSecreto, identidad.DatosAsociadosTOTP(f.ID))
		if err != nil {
			return err
		}
		if cambio && !simular {
			if err := q.RecifrarTOTP(ctx, clavebd.RecifrarTOTPParams{ID: f.ID, TotpSecreto: nuevo}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *rotador) conectores(ctx context.Context, q *clavebd.Queries, simular bool) error {
	filas, err := q.ConectoresCifrados(ctx)
	if err != nil {
		return err
	}
	for _, f := range filas {
		token, c1, err := r.valor(ColumnaTokenConector, f.ID, f.TokenCifrado, conector.DatosAsociados(f.ID, "token"))
		if err != nil {
			return err
		}
		secreto, c2, err := r.valor(ColumnaSecretoWebhook, f.ID, f.SecretoWebhookCifrado, conector.DatosAsociados(f.ID, "secreto_webhook"))
		if err != nil {
			return err
		}
		if (c1 || c2) && !simular {
			if err := q.RecifrarConector(ctx, clavebd.RecifrarConectorParams{ID: f.ID, TokenCifrado: token, SecretoWebhookCifrado: secreto}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *rotador) buzones(ctx context.Context, q *clavebd.Queries, simular bool) error {
	filas, err := q.BuzonesCifrados(ctx)
	if err != nil {
		return err
	}
	for _, f := range filas {
		imap, c1, err := r.valor(ColumnaIMAP, f.ID, f.ImapContrasena, datoAsociadoBuzon(f.ID, "imap"))
		if err != nil {
			return err
		}
		smtp, c2, err := r.valor(ColumnaSMTP, f.ID, f.SmtpContrasena, datoAsociadoBuzon(f.ID, "smtp"))
		if err != nil {
			return err
		}
		if (c1 || c2) && !simular {
			if err := q.RecifrarBuzon(ctx, clavebd.RecifrarBuzonParams{ID: f.ID, ImapContrasena: imap, SmtpContrasena: smtp}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *rotador) autorizaciones(ctx context.Context, q *clavebd.Queries, simular bool) error {
	filas, err := q.AutorizacionesCifradas(ctx)
	if err != nil {
		return err
	}
	for _, f := range filas {
		nuevo, cambio, err := r.valor(ColumnaVerificador, f.ID, f.VerificadorCifrado, conector.DatosAsociadosAutorizacion(f.ID))
		if err != nil {
			return err
		}
		if cambio && !simular {
			if err := q.RecifrarAutorizacion(ctx, clavebd.RecifrarAutorizacionParams{ID: f.ID, VerificadorCifrado: nuevo}); err != nil {
				return err
			}
		}
	}
	return nil
}
