package correo

import (
	"strings"
)

// Verificación del remitente con Authentication-Results (RFC 8601).
//
// El From de un correo lo escribe quien lo envía: no prueba nada. Lo que sí sirve es lo que
// anota el servidor de correo de confianza (el que recibe para el buzón) tras revisar SPF, DKIM
// y DMARC. Reglas:
//
//   - Solo cuentan las cabeceras cuyo authserv-id es el configurado en el buzón (authserv_id).
//     Sin authserv_id configurado no se confía en ninguna: nada queda verificado.
//   - Se toma el primer bloque contiguo de esas cabeceras empezando desde arriba (las añade el
//     servidor propio, encima de lo que traía el correo); una cabecera con el mismo id más
//     abajo, separada por otra, la pudo escribir el remitente y se ignora. El servidor debe
//     quitar (o renombrar) las que lleguen con su id, como pide RFC 8601 §5.
//   - Verificado = dmarc=pass (con header.from igual al dominio del From, si viene), o
//     dkim=pass con header.d (o el dominio de header.i) alineado al dominio del From: igual, o
//     el From es un subdominio de d (alineación relajada; d debe tener al menos un punto).
//   - SPF solo no basta (autentica el sobre, no el From).

// Verificar dice si el remitente quedó autenticado según las cabeceras Authentication-Results
// (valores de arriba abajo) del servidor authservID. resumen describe el resultado usado (o
// por qué no se verificó), sin datos del correo más allá de métodos y dominios.
func Verificar(resultados []string, authservID, remitente string) (verificado bool, resumen string) {
	id := strings.ToLower(strings.TrimSpace(authservID))
	if id == "" {
		return false, "sin_authserv_id"
	}
	_, dominio, ok := strings.Cut(strings.ToLower(strings.TrimSpace(remitente)), "@")
	if !ok || dominio == "" {
		return false, "remitente_invalido"
	}
	var propios []string
	for _, r := range resultados {
		servidor, resto := partirAR(r)
		if servidor == id {
			propios = append(propios, resto)
			continue
		}
		if len(propios) > 0 {
			break // fin del bloque de arriba
		}
	}
	if len(propios) == 0 {
		return false, "sin_resultados_del_servidor"
	}
	for _, resto := range propios {
		for _, m := range metodos(resto) {
			if m.resultado != "pass" {
				continue
			}
			switch m.nombre {
			case "dmarc":
				if de := m.props["header.from"]; de == "" || de == dominio {
					return true, "dmarc=pass header.from=" + dominio
				}
			case "dkim":
				d := m.props["header.d"]
				if d == "" {
					if _, di, ok := strings.Cut(m.props["header.i"], "@"); ok {
						d = di
					}
				}
				if alineado(dominio, d) {
					return true, "dkim=pass header.d=" + d
				}
			}
		}
	}
	return false, "sin_pass_alineado"
}

// alineado: alineación relajada sin lista de sufijos públicos (d igual al dominio del From o
// un dominio padre con al menos un punto).
func alineado(dominio, d string) bool {
	d = strings.Trim(d, ".")
	if d == "" || !strings.Contains(d, ".") {
		return false
	}
	return dominio == d || strings.HasSuffix(dominio, "."+d)
}

// partirAR separa el authserv-id (en minúsculas, sin la versión) del resto de la cabecera.
func partirAR(valor string) (string, string) {
	v := sinComentarios(valor)
	cabeza, resto, _ := strings.Cut(v, ";")
	campos := strings.Fields(cabeza)
	if len(campos) == 0 {
		return "", resto
	}
	return strings.ToLower(campos[0]), resto
}

type metodo struct {
	nombre, resultado string
	props             map[string]string
}

// metodos interpreta «método=resultado prop=valor …; …». Los nombres y valores van en
// minúsculas y sin comillas.
func metodos(resto string) []metodo {
	var salida []metodo
	for _, parte := range strings.Split(resto, ";") {
		campos := strings.Fields(parte)
		if len(campos) == 0 {
			continue
		}
		nombre, resultado, ok := strings.Cut(strings.ToLower(campos[0]), "=")
		if !ok {
			continue // «none» u otra cosa sin resultado
		}
		nombre, _, _ = strings.Cut(nombre, "/") // método/versión
		m := metodo{nombre: nombre, resultado: strings.Trim(resultado, `"`), props: map[string]string{}}
		for _, c := range campos[1:] {
			if k, v, ok := strings.Cut(c, "="); ok {
				m.props[strings.ToLower(k)] = strings.ToLower(strings.Trim(v, `"`))
			}
		}
		salida = append(salida, m)
	}
	return salida
}

// sinComentarios quita los comentarios entre paréntesis (anidables) y desdobla líneas.
func sinComentarios(s string) string {
	var b strings.Builder
	nivel := 0
	escape := false
	for _, r := range s {
		switch {
		case escape:
			escape = false
			if nivel > 0 {
				continue
			}
		case r == '\\':
			escape = true
			if nivel > 0 {
				continue
			}
		case r == '(':
			nivel++
			continue
		case r == ')' && nivel > 0:
			nivel--
			b.WriteByte(' ')
			continue
		}
		if nivel > 0 {
			continue
		}
		if r == '\r' || r == '\n' || r == '\t' {
			r = ' '
		}
		b.WriteRune(r)
	}
	return b.String()
}
