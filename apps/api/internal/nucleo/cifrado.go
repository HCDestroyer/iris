package nucleo

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// Cifrador cifra secretos en reposo (TOTP, y más adelante tokens de conectores) con
// AES-256-GCM. La clave sale de IRIS_CLAVE_CIFRADO (32 bytes en base64) y nunca va al repo.
type Cifrador struct {
	aead cipher.AEAD
}

// NuevoCifrador recibe la clave en base64 (estándar o URL) de 32 bytes.
func NuevoCifrador(claveBase64 string) (*Cifrador, error) {
	clave, err := base64.StdEncoding.DecodeString(claveBase64)
	if err != nil {
		clave, err = base64.RawURLEncoding.DecodeString(claveBase64)
	}
	if err != nil {
		return nil, fmt.Errorf("IRIS_CLAVE_CIFRADO no es base64: %w", err)
	}
	if len(clave) != 32 {
		return nil, fmt.Errorf("IRIS_CLAVE_CIFRADO debe tener 32 bytes (tiene %d)", len(clave))
	}
	bloque, err := aes.NewCipher(clave)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(bloque)
	if err != nil {
		return nil, err
	}
	return &Cifrador{aead: aead}, nil
}

// Cifrar devuelve nonce || texto cifrado. datosAsociados ata el valor a su dueño (p. ej.
// "usuario:7:totp") para que no se pueda copiar a otra fila.
func (c *Cifrador) Cifrar(texto, datosAsociados []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, texto, datosAsociados), nil
}

// Descifrar invierte Cifrar.
func (c *Cifrador) Descifrar(cifrado, datosAsociados []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(cifrado) < n {
		return nil, errors.New("cifrado: valor demasiado corto")
	}
	return c.aead.Open(nil, cifrado[:n], cifrado[n:], datosAsociados)
}
