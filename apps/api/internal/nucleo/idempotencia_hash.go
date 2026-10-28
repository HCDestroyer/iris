package nucleo

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"hash"
)

// Hash de la petición en nucleo.idempotencia. El cuerpo puede llevar contraseñas en claro
// (POST /yo/contrasena, contrasenaTemporal del reinicio de MFA, contraseñas de buzones…): un
// SHA-256 plano permitiría probar contraseñas candidatas a gran velocidad a quien lea la tabla.
// Por eso es un HMAC-SHA256 con una clave del servidor, derivada de IRIS_CLAVE_CIFRADO con HKDF
// y una etiqueta propia (no se reutiliza la clave de AES-GCM tal cual).
//
// Formatos (columna hash_formato): 0 = SHA-256 plano (filas de antes; se comparan igual hasta
// que vencen), 1 = HMAC con la clave cuya huella está en hash_clave_id.
const (
	etiquetaIdempotencia = "iris:idempotencia"
	formatoSHA256        = 0
	formatoHMAC          = 1
)

// ClaveIdempotencia es una clave HMAC para el hash de las peticiones y su huella (8 bytes,
// HMAC de una etiqueta fija: identifica la clave sin revelarla).
type ClaveIdempotencia struct {
	clave []byte
	id    []byte
}

func nuevaClaveIdempotencia(clave []byte) ClaveIdempotencia {
	m := hmac.New(sha256.New, clave)
	m.Write([]byte(etiquetaIdempotencia + ":id"))
	return ClaveIdempotencia{clave: clave, id: m.Sum(nil)[:8]}
}

// DerivarClaveIdempotencia deriva la clave HMAC de una clave de cifrado (32 bytes en base64,
// como IRIS_CLAVE_CIFRADO) con HKDF-SHA256 y la etiqueta «iris:idempotencia».
func DerivarClaveIdempotencia(claveBase64 string) (ClaveIdempotencia, error) {
	bruta, _, err := abrirClave("IRIS_CLAVE_CIFRADO", claveBase64)
	if err != nil {
		return ClaveIdempotencia{}, err
	}
	k, err := hkdf.Key(sha256.New, bruta, nil, etiquetaIdempotencia, 32)
	if err != nil {
		return ClaveIdempotencia{}, err
	}
	return nuevaClaveIdempotencia(k), nil
}

// ClavesIdempotencia arma las claves del middleware desde IRIS_CLAVE_CIFRADO (con ella se
// escribe) e IRIS_CLAVE_CIFRADO_ANTERIOR (solo para comparar filas escritas antes de rotar).
// Sin IRIS_CLAVE_CIFRADO devuelve nil: el middleware usa una clave aleatoria del proceso.
func ClavesIdempotencia(actual, anterior string) ([]ClaveIdempotencia, error) {
	if actual == "" {
		return nil, nil
	}
	var claves []ClaveIdempotencia
	for _, c := range []string{actual, anterior} {
		if c == "" {
			continue
		}
		k, err := DerivarClaveIdempotencia(c)
		if err != nil {
			return nil, err
		}
		claves = append(claves, k)
	}
	return claves, nil
}

// ClaveIdempotenciaAleatoria: sin IRIS_CLAVE_CIFRADO (desarrollo). Vale mientras viva el
// proceso: tras reiniciar, una clave repetida responde 422 idempotencia_no_verificable.
func ClaveIdempotenciaAleatoria() ClaveIdempotencia {
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	return nuevaClaveIdempotencia(k)
}

func escribirPeticion(h hash.Hash, metodo, ruta string, cuerpo []byte) []byte {
	_, _ = fmt.Fprintf(h, "%s\n%s\n", metodo, ruta)
	h.Write(cuerpo)
	return h.Sum(nil)
}

// hashPeticion es el HMAC de método, ruta y cuerpo con esta clave.
func (c ClaveIdempotencia) hashPeticion(metodo, ruta string, cuerpo []byte) []byte {
	return escribirPeticion(hmac.New(sha256.New, c.clave), metodo, ruta, cuerpo)
}

// sha256Plano es el formato viejo (solo para comparar filas anteriores).
func sha256Plano(metodo, ruta string, cuerpo []byte) []byte {
	return escribirPeticion(sha256.New(), metodo, ruta, cuerpo)
}

// compararPeticion dice si la petición es la misma que la de la fila guardada. verificable es
// false si la fila se escribió con una clave que el servidor ya no conoce (rotación sin la
// anterior, o un proceso sin IRIS_CLAVE_CIFRADO que reinició): entonces no se puede saber.
func compararPeticion(claves []ClaveIdempotencia, formato int16, idClave, guardado []byte, metodo, ruta string, cuerpo []byte) (igual, verificable bool) {
	if formato == formatoSHA256 {
		return hmac.Equal(guardado, sha256Plano(metodo, ruta, cuerpo)), true
	}
	for _, c := range claves {
		if bytes.Equal(c.id, idClave) {
			return hmac.Equal(guardado, c.hashPeticion(metodo, ruta, cuerpo)), true
		}
	}
	return false, false
}
