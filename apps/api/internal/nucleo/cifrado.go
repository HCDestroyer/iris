package nucleo

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// Cifrador cifra secretos en reposo (TOTP, credenciales de conectores, contraseñas de buzones)
// con AES-256-GCM. La clave sale de IRIS_CLAVE_CIFRADO (32 bytes en base64) y nunca va al repo.
//
// Durante una rotación (docs/runbooks/rotar-clave-cifrado.md) también conoce la clave anterior
// (IRIS_CLAVE_CIFRADO_ANTERIOR): cifra siempre con la actual y, al descifrar, prueba la actual y
// luego la anterior. Así la API y el worker siguen leyendo lo que aún no se volvió a cifrar.
type Cifrador struct {
	aead       cipher.AEAD
	anteriores []cipher.AEAD
}

// ErrDescifrar: el valor no se pudo descifrar con ninguna de las claves conocidas (otra clave,
// valor alterado o datos asociados de otra fila).
var ErrDescifrar = errors.New("cifrado: no se pudo descifrar con ninguna clave conocida")

// NuevoCifrador recibe la clave actual en base64 (estándar o URL) de 32 bytes y, opcionalmente,
// claves anteriores (solo para descifrar) en el mismo formato. Una anterior vacía se ignora; una
// igual a la actual es un error de configuración.
func NuevoCifrador(claveBase64 string, anterioresBase64 ...string) (*Cifrador, error) {
	clave, aead, err := abrirClave("IRIS_CLAVE_CIFRADO", claveBase64)
	if err != nil {
		return nil, err
	}
	c := &Cifrador{aead: aead}
	for _, a := range anterioresBase64 {
		if a == "" {
			continue
		}
		anterior, aeadAnterior, err := abrirClave("IRIS_CLAVE_CIFRADO_ANTERIOR", a)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(anterior, clave) {
			return nil, errors.New("IRIS_CLAVE_CIFRADO_ANTERIOR es igual a IRIS_CLAVE_CIFRADO")
		}
		c.anteriores = append(c.anteriores, aeadAnterior)
	}
	return c, nil
}

func abrirClave(nombre, claveBase64 string) ([]byte, cipher.AEAD, error) {
	clave, err := base64.StdEncoding.DecodeString(claveBase64)
	if err != nil {
		clave, err = base64.RawURLEncoding.DecodeString(claveBase64)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%s no es base64: %w", nombre, err)
	}
	if len(clave) != 32 {
		return nil, nil, fmt.Errorf("%s debe tener 32 bytes (tiene %d)", nombre, len(clave))
	}
	bloque, err := aes.NewCipher(clave)
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(bloque)
	if err != nil {
		return nil, nil, err
	}
	return clave, aead, nil
}

// TieneAnterior indica si el cifrador conoce una clave anterior (rotación en curso).
func (c *Cifrador) TieneAnterior() bool { return len(c.anteriores) > 0 }

// Cifrar devuelve nonce || texto cifrado con la clave actual. datosAsociados ata el valor a su
// dueño (p. ej. "usuario:7:totp") para que no se pueda copiar a otra fila.
func (c *Cifrador) Cifrar(texto, datosAsociados []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, texto, datosAsociados), nil
}

// Descifrar invierte Cifrar: prueba la clave actual y, si falla, las anteriores.
func (c *Cifrador) Descifrar(cifrado, datosAsociados []byte) ([]byte, error) {
	plano, _, err := c.DescifrarConOrigen(cifrado, datosAsociados)
	return plano, err
}

// DescifrarConOrigen es Descifrar e indica además si hizo falta una clave anterior (el valor aún
// no se volvió a cifrar con la actual). ErrDescifrar si ninguna clave sirve.
func (c *Cifrador) DescifrarConOrigen(cifrado, datosAsociados []byte) (plano []byte, conAnterior bool, err error) {
	n := c.aead.NonceSize()
	if len(cifrado) < n {
		return nil, false, errors.New("cifrado: valor demasiado corto")
	}
	if plano, err := c.aead.Open(nil, cifrado[:n], cifrado[n:], datosAsociados); err == nil {
		return plano, false, nil
	}
	for _, a := range c.anteriores {
		if plano, err := a.Open(nil, cifrado[:n], cifrado[n:], datosAsociados); err == nil {
			return plano, true, nil
		}
	}
	return nil, false, ErrDescifrar
}
