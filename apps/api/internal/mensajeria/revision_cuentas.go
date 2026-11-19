package mensajeria

import (
	"context"
	"regexp"
	"strings"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Textos con números de cuenta escritos a mano (contrato con Argos 0.8.0 §10.3, filas 12 a 14;
// decisión del PM §10.2 punto 11): los manuales, las respuestas rápidas y la respuesta de fuera de
// horario son texto libre y pueden traer «nuestras cuentas son…». Iris solo avisa al admin para que
// los borre a mano y use «Enviar cuentas»; no bloquea guardar. Nunca devuelve ni audita el número
// encontrado: solo dónde está.

// TextoConCuenta es un texto que parece llevar un número de cuenta bancaria.
type TextoConCuenta struct {
	Tipo   string `json:"tipo" enum:"manual,respuesta_rapida,fuera_de_horario" doc:"Dónde está: un manual, una respuesta rápida o la respuesta de fuera de horario de un número"`
	ID     int64  `json:"id" doc:"Id del manual, de la respuesta rápida o del número de WhatsApp"`
	Titulo string `json:"titulo" doc:"El título del manual, el atajo de la respuesta o el nombre del número"`
	Motivo string `json:"motivo" enum:"cuenta_de_argos,parece_cuenta" doc:"cuenta_de_argos: trae el número de una cuenta de Argos; parece_cuenta: un número largo junto a «cuenta», «depósito» o un banco"`
}

// RevisionCuentas es GET /revision-cuentas.
type RevisionCuentas struct {
	Data []TextoConCuenta `json:"data"`
}

var (
	// reNumeroLargo: 7 o más dígitos, con guiones o espacios entre grupos (como se escriben las cuentas).
	reNumeroLargo = regexp.MustCompile(`\d(?:[\d\- ]{5,}\d)`)
	// rePalabraCuenta: lo que acompaña a un número de cuenta escrito a mano.
	rePalabraCuenta = regexp.MustCompile(`(?i)cuenta|dep[oó]sit|transfer|monetaria|ahorro|banco|banrural|\bbi\b|industrial|bam\b|g&t|promerica|bantrab|interbanco|ficohsa`)
)

// MotivoNumeroCuenta dice si el texto parece llevar un número de cuenta: el de una cuenta de Argos
// (comparando solo dígitos, de 6 o más) o un número de 7 o más dígitos cerca (40 caracteres) de
// «cuenta», «depósito» o el nombre de un banco. "" si no.
func MotivoNumeroCuenta(texto string, numeros []string) string {
	digitos := soloDigitos(texto)
	for _, n := range numeros {
		if d := soloDigitos(n); len(d) >= 6 && strings.Contains(digitos, d) {
			return "cuenta_de_argos"
		}
	}
	palabras := rePalabraCuenta.FindAllStringIndex(texto, -1)
	if len(palabras) == 0 {
		return ""
	}
	for _, n := range reNumeroLargo.FindAllStringIndex(texto, -1) {
		if len(soloDigitos(texto[n[0]:n[1]])) < 7 {
			continue
		}
		for _, p := range palabras {
			if (n[0] >= p[1] && n[0]-p[1] <= CercaniaSecreto) || (p[0] >= n[1] && p[0]-n[1] <= CercaniaSecreto) {
				return "parece_cuenta"
			}
		}
	}
	return ""
}

func nuevaRevisionCuentas() *auditoria.Caso[struct{}, RevisionCuentas] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, RevisionCuentas]{
		Accion: "canales.revision_cuentas", Permiso: nucleo.PermCanalesConfigurar, AuditarExito: sinAuditar[RevisionCuentas],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (RevisionCuentas, error) {
			// La empresa activa y lo global (sin empresa); sin empresa activa, todo lo que ve el admin.
			empresa := int64(0)
			if contexto.EmpresasDe(ctx).Estado == contexto.EmpresaActiva {
				empresa = contexto.EmpresaDe(ctx)
			}
			numeros := []string{}
			cs, err := argosbd.New(t).CuentasBancariasTodas(ctx)
			if err != nil {
				return RevisionCuentas{}, err
			}
			for _, k := range cs {
				numeros = append(numeros, k.Numero)
			}
			out := RevisionCuentas{Data: []TextoConCuenta{}}
			consultas := []struct{ tipo, sql string }{
				{"manual", `SELECT id, titulo, cuerpo FROM mensajeria.manuales WHERE activo AND ($1::bigint = 0 OR empresa_id IS NULL OR empresa_id = $1) ORDER BY id`},
				{"respuesta_rapida", `SELECT id, atajo, texto FROM mensajeria.respuestas_rapidas WHERE activa AND ($1::bigint = 0 OR empresa_id IS NULL OR empresa_id = $1) ORDER BY id`},
				{"fuera_de_horario", `SELECT id, nombre, coalesce(fuera_horario->>'mensaje', '') FROM mensajeria.canales WHERE ($1::bigint = 0 OR empresa_id IS NULL OR empresa_id = $1) ORDER BY id`},
			}
			for _, c := range consultas {
				filas, err := t.Query(ctx, c.sql, empresa)
				if err != nil {
					return RevisionCuentas{}, err
				}
				for filas.Next() {
					var id int64
					var titulo, texto string
					if err := filas.Scan(&id, &titulo, &texto); err != nil {
						filas.Close()
						return RevisionCuentas{}, err
					}
					if motivo := MotivoNumeroCuenta(texto, numeros); motivo != "" {
						out.Data = append(out.Data, TextoConCuenta{Tipo: c.tipo, ID: id, Titulo: titulo, Motivo: motivo})
					}
				}
				filas.Close()
				if err := filas.Err(); err != nil {
					return RevisionCuentas{}, err
				}
			}
			return out, nil
		},
	})
}
