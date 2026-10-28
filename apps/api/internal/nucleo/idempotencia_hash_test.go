package nucleo

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestHashIdempotenciaEsHMACDerivado(t *testing.T) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	k, err := DerivarClaveIdempotencia(base64.StdEncoding.EncodeToString(b))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(k.clave, b) {
		t.Fatal("la clave HMAC no puede ser la de AES-GCM tal cual")
	}
	cuerpo := []byte(`{"contrasena":"x"}`)
	h := k.hashPeticion("POST", "/api/v1/yo/contrasena", cuerpo)
	m := hmac.New(sha256.New, k.clave)
	m.Write([]byte("POST\n/api/v1/yo/contrasena\n"))
	m.Write(cuerpo)
	if !hmac.Equal(h, m.Sum(nil)) || bytes.Equal(h, sha256Plano("POST", "/api/v1/yo/contrasena", cuerpo)) {
		t.Fatal("el hash debe ser el HMAC con la clave derivada, no el SHA-256 plano")
	}
	otra, _ := DerivarClaveIdempotencia(base64.StdEncoding.EncodeToString(b))
	if !bytes.Equal(otra.clave, k.clave) || !bytes.Equal(otra.id, k.id) {
		t.Fatal("la derivación es determinista")
	}
	if claves, err := ClavesIdempotencia("", ""); err != nil || claves != nil {
		t.Fatal("sin IRIS_CLAVE_CIFRADO no hay claves derivadas")
	}
}
