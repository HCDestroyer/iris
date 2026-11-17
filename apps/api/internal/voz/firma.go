// Package voz es el protocolo entre mensajería y el Servidor de IA («Iris Voz», contrato voz.md §4):
// la puerta (cmd/voz-puerta), que termina TLS con mTLS de huellas fijadas y firma por petición, y
// el cliente con que Iris la llama. No importa internal/mensajeria ni nada con acceso a la base
// (lo comprueba la prueba de dependencias de este paquete).
package voz

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/hkdf"
)

// Cabeceras del canal (voz.md §4.2).
const (
	CabFirma          = "X-Iris-Firma"
	CabMarca          = "X-Iris-Marca"
	CabNonce          = "X-Iris-Nonce"
	CabVersion        = "X-Iris-Version"
	CabFirmaRespuesta = "X-Iris-Firma-Respuesta"
)

// Protocolo es la versión del protocolo (la base de las rutas).
const Protocolo = "v1"

// Base es el prefijo de todas las rutas de la puerta.
const Base = "/voz/" + Protocolo

const (
	infoFirma     = "iris-voz/firma/v1"
	infoRespuesta = "iris-voz/respuesta/v1"
	infoToken     = "iris-voz/token/v1" //nolint:gosec // etiqueta de derivación, no un secreto

	// VentanaMarca: desfase máximo de reloj aceptado.
	VentanaMarca = 30 * time.Second
	// VidaNonce: cuánto se recuerda un nonce (más que la ventana).
	VidaNonce = 90 * time.Second
	// LargoToken: bytes del token de servicio.
	LargoToken = 32
)

func derivar(secreto []byte, info string) []byte {
	k := make([]byte, 32)
	// HKDF sobre SHA-256 no falla al pedir 32 bytes.
	_, _ = io.ReadFull(hkdf.New(sha256.New, secreto, nil, []byte(info)), k)
	return k
}

// NuevoToken genera un token de servicio aleatorio.
func NuevoToken() ([]byte, error) {
	t := make([]byte, LargoToken)
	_, err := rand.Read(t)
	return t, err
}

// NuevoNonce genera un nonce de 128 bits en hexadecimal.
func NuevoNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func nonceValido(n string) bool {
	if len(n) != 32 {
		return false
	}
	_, err := hex.DecodeString(n)
	return err == nil
}

func mac(clave []byte, partes ...string) string {
	h := hmac.New(sha256.New, clave)
	for i, p := range partes {
		if i > 0 {
			h.Write([]byte{'\n'})
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sumaHex(cuerpo []byte) string {
	s := sha256.Sum256(cuerpo)
	return hex.EncodeToString(s[:])
}

// FirmaPeticion: HMAC sobre método, ruta con query, marca, nonce, versión de Iris y sha256 del cuerpo.
func FirmaPeticion(token []byte, metodo, ruta, marca, nonce, version string, cuerpo []byte) string {
	return mac(derivar(token, infoFirma), metodo, ruta, marca, nonce, version, sumaHex(cuerpo))
}

// FirmaRespuesta: HMAC sobre el nonce de la petición, el estado y sha256 del cuerpo.
func FirmaRespuesta(token []byte, nonce string, estado int, cuerpo []byte) string {
	return mac(derivar(token, infoRespuesta), nonce, strconv.Itoa(estado), sumaHex(cuerpo))
}

// Iguales compara dos firmas en tiempo constante.
func Iguales(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// nonces recuerda los nonces vistos durante VidaNonce.
type nonces struct {
	mu     sync.Mutex
	vistos map[string]time.Time
	tope   int
}

func nuevosNonces() *nonces { return &nonces{vistos: map[string]time.Time{}, tope: 200_000} }

// usar devuelve false si el nonce ya se vio (o si la memoria está llena: se rechaza, no se olvida).
func (n *nonces) usar(nonce string, ahora time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.vistos) >= n.tope/2 {
		for k, v := range n.vistos {
			if ahora.After(v) {
				delete(n.vistos, k)
			}
		}
	}
	if v, ok := n.vistos[nonce]; ok && !ahora.After(v) {
		return false
	}
	if len(n.vistos) >= n.tope {
		return false
	}
	n.vistos[nonce] = ahora.Add(VidaNonce)
	return true
}

// cubo es un límite de frecuencia simple (cubo de fichas).
type cubo struct {
	mu      sync.Mutex
	fichas  float64
	tope    float64
	porSeg  float64
	ultimaV time.Time
}

func nuevoCubo(porSeg float64, rafaga int) *cubo {
	return &cubo{fichas: float64(rafaga), tope: float64(rafaga), porSeg: porSeg}
}

func (c *cubo) permitir(ahora time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ultimaV.IsZero() {
		c.fichas += ahora.Sub(c.ultimaV).Seconds() * c.porSeg
		if c.fichas > c.tope {
			c.fichas = c.tope
		}
	}
	c.ultimaV = ahora
	if c.fichas < 1 {
		return false
	}
	c.fichas--
	return true
}
