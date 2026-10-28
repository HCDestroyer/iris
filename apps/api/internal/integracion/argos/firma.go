package argos

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// Firma de los webhooks (contrato Argos ↔ Horus §1.3, reutilizado en §1.3 del de Iris):
//
//	X-Webhook-Id:        id estable del envío (el mismo en cada reintento)
//	X-Webhook-Timestamp: segundos Unix del envío
//	X-Webhook-Signature: v1=<hex(HMAC-SHA256(secreto, "<timestamp>.<cuerpo crudo>"))>
//
// Supuesto (a confirmar con Argos): el mensaje firmado es "<timestamp>.<cuerpo>" y la firma va
// en hexadecimal en minúsculas. Se aceptan varias firmas separadas por coma (rotación del
// secreto); basta que una coincida. La marca de tiempo debe estar dentro de la ventana
// (IRIS_WEBHOOK_VENTANA, 5 min) para que un webhook capturado no se pueda reenviar después.

// FirmarWebhook calcula la cabecera X-Webhook-Signature (la usa el servidor falso de las pruebas
// y documenta el formato).
func FirmarWebhook(secreto, marca string, cuerpo []byte) string {
	return "v1=" + hex.EncodeToString(mac(secreto, marca, cuerpo))
}

func mac(secreto, marca string, cuerpo []byte) []byte {
	h := hmac.New(sha256.New, []byte(secreto))
	h.Write([]byte(marca))
	h.Write([]byte{'.'})
	h.Write(cuerpo)
	return h.Sum(nil)
}

// firmaValida compara en tiempo constante contra cada v1= de la cabecera.
func firmaValida(secreto, cabecera, marca string, cuerpo []byte) bool {
	esperada := mac(secreto, marca, cuerpo)
	for _, parte := range strings.Split(cabecera, ",") {
		version, valor, ok := strings.Cut(strings.TrimSpace(parte), "=")
		if !ok || version != "v1" {
			continue
		}
		recibida, err := hex.DecodeString(valor)
		if err == nil && hmac.Equal(recibida, esperada) {
			return true
		}
	}
	return false
}

// marcaEnVentana valida X-Webhook-Timestamp (segundos Unix).
func marcaEnVentana(marca string, ahora time.Time, ventana time.Duration) bool {
	s, err := strconv.ParseInt(strings.TrimSpace(marca), 10, 64)
	if err != nil {
		return false
	}
	d := ahora.Sub(time.Unix(s, 0))
	return d <= ventana && d >= -ventana
}

// nuevoSecreto devuelve 32 bytes aleatorios en base64url (secreto del webhook, códigos de
// enlace).
func nuevoSecreto() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hash256(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
