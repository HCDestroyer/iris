package identidad

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Contraseñas con argon2id en formato PHC: $argon2id$v=19$m=19456,t=2,p=1$<sal>$<hash>
// (parámetros mínimos de OWASP). Al verificar se leen los parámetros del hash guardado.
const (
	argonMemoria   = 19 * 1024
	argonIteracion = 2
	argonHilos     = 1
	argonLargo     = 32
	argonSal       = 16
)

// LongitudMinimaContrasena para usuarios persona.
const LongitudMinimaContrasena = 12

// HashContrasena devuelve el hash argon2id de la contraseña.
func HashContrasena(contrasena string) (string, error) {
	sal := make([]byte, argonSal)
	if _, err := rand.Read(sal); err != nil {
		return "", err
	}
	h := argon2.IDKey([]byte(contrasena), sal, argonIteracion, argonMemoria, argonHilos, argonLargo)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemoria, argonIteracion, argonHilos,
		base64.RawStdEncoding.EncodeToString(sal), base64.RawStdEncoding.EncodeToString(h)), nil
}

var errHashInvalido = errors.New("hash argon2id inválido")

// VerificarContrasena compara en tiempo constante.
func VerificarContrasena(contrasena, codificado string) (bool, error) {
	partes := strings.Split(codificado, "$")
	if len(partes) != 6 || partes[1] != "argon2id" {
		return false, errHashInvalido
	}
	var version int
	if _, err := fmt.Sscanf(partes[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errHashInvalido
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(partes[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, errHashInvalido
	}
	sal, err := base64.RawStdEncoding.DecodeString(partes[4])
	if err != nil {
		return false, errHashInvalido
	}
	esperado, err := base64.RawStdEncoding.DecodeString(partes[5])
	if err != nil {
		return false, errHashInvalido
	}
	h := argon2.IDKey([]byte(contrasena), sal, t, m, p, uint32(len(esperado))) //nolint:gosec // largo del hash guardado
	return subtle.ConstantTimeCompare(h, esperado) == 1, nil
}

// hashFicticio se verifica cuando el correo no existe, para que el tiempo de respuesta no
// revele qué correos tienen cuenta.
var hashFicticio, _ = HashContrasena("iris-contrasena-ficticia")

// PrefijoToken de los tokens de API (CLAUDE.md §7, contrato Argos §1.2).
const PrefijoToken = "iris_pat_"

const alfabeto62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// NuevoTokenAPI devuelve iris_pat_<32 bytes aleatorios en base62>.
func NuevoTokenAPI() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return PrefijoToken + base62(b), nil
}

func base62(b []byte) string {
	n := new(big.Int).SetBytes(b)
	base := big.NewInt(62)
	mod := new(big.Int)
	var salida []byte
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		salida = append(salida, alfabeto62[mod.Int64()])
	}
	// 32 bytes caben en 43 dígitos base62; se rellena para un largo fijo.
	for len(salida) < 43 {
		salida = append(salida, '0')
	}
	for i, j := 0, len(salida)-1; i < j; i, j = i+1, j-1 {
		salida[i], salida[j] = salida[j], salida[i]
	}
	return string(salida)
}

// NuevoSecreto devuelve 32 bytes aleatorios en base64url (cookie de sesión, CSRF).
func NuevoSecreto() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Hash256 es el SHA-256 con que se guardan sesiones, CSRF y tokens.
func Hash256(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// prefijoVisible es lo que se muestra de un token para reconocerlo (iris_pat_ + 4).
func prefijoVisible(token string) string {
	if len(token) < len(PrefijoToken)+4 {
		return token
	}
	return token[:len(PrefijoToken)+4]
}
