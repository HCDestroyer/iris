package voz

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PrefijoCodigo es el prefijo del código de enlace.
const PrefijoCodigo = "voz-"

// VidaCodigo: cuánto vale un código de enlace y cuánto dura la ventana.
const VidaCodigo = 15 * time.Minute

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

var reCodigo = regexp.MustCompile(`^voz-[a-z2-7]{32}$`)

// NuevoCodigo genera un código de enlace de un solo uso: «voz-» + 160 bits aleatorios en base32.
func NuevoCodigo() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return PrefijoCodigo + strings.ToLower(b32.EncodeToString(b)), nil
}

// CodigoValido dice si el texto tiene la forma de un código de enlace.
func CodigoValido(c string) bool { return reCodigo.MatchString(c) }

// PruebaIris: lo que mensajería manda para demostrar que conoce el código, atado a los dos
// certificados del saludo TLS (con alguien en medio alguna huella no coincide).
func PruebaIris(codigo, nonceIris, huellaServidor, huellaCliente string) string {
	return mac([]byte(codigo), "iris", nonceIris, huellaServidor, huellaCliente)
}

// PruebaServidor: la respuesta de la puerta.
func PruebaServidor(codigo, nonceIris, nonceServidor, huellaServidor, huellaCliente string) string {
	return mac([]byte(codigo), "servidor", nonceIris, nonceServidor, huellaServidor, huellaCliente)
}

// TokenDeEnlace deriva en los dos lados el token inicial: nunca viaja y solo vive hasta activar.
func TokenDeEnlace(codigo, nonceIris, nonceServidor, huellaServidor, huellaCliente string) []byte {
	return derivar([]byte(codigo), strings.Join([]string{infoToken, nonceIris, nonceServidor, huellaServidor, huellaCliente}, "\n"))
}

// Huella de un certificado: «sha256:» + hex del DER.
func Huella(der []byte) string {
	s := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(s[:])
}

var reHuella = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// HuellaValida dice si el texto tiene la forma de una huella.
func HuellaValida(h string) bool { return reHuella.MatchString(h) }

// GenerarCertificado crea un par ECDSA P-256 autofirmado (no hay CA: se fijan huellas). Devuelve
// certificado y clave en PEM.
func GenerarCertificado(nombre string, vida time.Duration) (certPEM, clavePEM []byte, err error) {
	clave, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serie, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, err
	}
	ahora := time.Now().UTC()
	plantilla := &x509.Certificate{
		SerialNumber: serie, Subject: pkix.Name{CommonName: nombre},
		NotBefore: ahora.Add(-5 * time.Minute), NotAfter: ahora.Add(vida),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, plantilla, plantilla, &clave.PublicKey, clave)
	if err != nil {
		return nil, nil, err
	}
	k, err := x509.MarshalPKCS8PrivateKey(clave)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: k}), nil
}

// HuellaDePEM devuelve la huella y el vencimiento de un certificado en PEM.
func HuellaDePEM(certPEM []byte) (string, time.Time, error) {
	b, _ := pem.Decode(certPEM)
	if b == nil || b.Type != "CERTIFICATE" {
		return "", time.Time{}, errors.New("certificado inválido")
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		return "", time.Time{}, err
	}
	return Huella(c.Raw), c.NotAfter, nil
}

func huellaDeTLS(c *tls.Certificate) string {
	if c == nil || len(c.Certificate) == 0 {
		return ""
	}
	return Huella(c.Certificate[0])
}

// Manifiesto es la versión del paquete y el rango de Iris con el que es compatible (voz.md §4.7).
type Manifiesto struct {
	Version   string `json:"version"`
	Protocolo string `json:"protocolo"`
	IrisMin   string `json:"irisMin"`
	IrisMax   string `json:"irisMax"`
}

var reSemver = regexp.MustCompile(`^v?(\d{1,6})\.(\d{1,6})\.(\d{1,6})(?:[-+][0-9A-Za-z.+-]{1,64})?$`)

// VersionValida dice si el texto es MAYOR.MENOR.PARCHE (con sufijo opcional).
func VersionValida(v string) bool { return reSemver.MatchString(v) }

func partes(v string) ([3]int, bool) {
	m := reSemver.FindStringSubmatch(v)
	if m == nil {
		return [3]int{}, false
	}
	var p [3]int
	for i := range p {
		p[i], _ = strconv.Atoi(m[i+1])
	}
	return p, true
}

// CompararVersion devuelve -1, 0 o 1; el sufijo (-dev, +meta) no cuenta. ok es false si alguna
// no es una versión.
func CompararVersion(a, b string) (int, bool) {
	pa, ok1 := partes(a)
	pb, ok2 := partes(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// Validar comprueba la forma del manifiesto.
func (m Manifiesto) Validar() error {
	if !VersionValida(m.Version) || !VersionValida(m.IrisMin) || !VersionValida(m.IrisMax) {
		return errors.New("manifiesto: versiones inválidas")
	}
	if m.Protocolo != Protocolo {
		return fmt.Errorf("manifiesto: protocolo %q", m.Protocolo)
	}
	return nil
}

var reDesarrollo = regexp.MustCompile(`-\d+-g[0-9a-f]{4,40}(-dirty)?$|-dirty$`)

// VersionDesarrollo dice si esa versión de Iris es un binario de desarrollo y no una versión
// publicada: 0.0.0 (sin versión: `0.0.0-dev`) o lo que da `git describe` con commits después de la
// etiqueta (`v0.27.0-36-gabc1234`, o `-dirty`). De esas no se sabe a qué versión publicada
// equivalen: no se comparan con el rango del paquete.
func VersionDesarrollo(version string) bool {
	p, ok := partes(version)
	if !ok {
		return false
	}
	return p == [3]int{} || reDesarrollo.MatchString(version)
}

// AdmiteIris dice si esa versión de Iris entra en [irisMin, irisMax]. Un binario de desarrollo
// (VersionDesarrollo) se admite siempre: solo una versión publicada exacta fuera del rango es
// incompatible.
func (m Manifiesto) AdmiteIris(version string) bool {
	if VersionDesarrollo(version) {
		return true
	}
	a, ok1 := CompararVersion(version, m.IrisMin)
	b, ok2 := CompararVersion(version, m.IrisMax)
	return ok1 && ok2 && a >= 0 && b <= 0
}

// Compatible comprueba los dos sentidos desde Iris: protocolo, su versión dentro del rango del
// paquete y el paquete por encima de vozMin. Devuelve el motivo si no lo es.
func Compatible(m Manifiesto, versionIris, vozMin string) (bool, string) {
	if m.Protocolo != Protocolo {
		return false, "protocolo"
	}
	if !m.AdmiteIris(versionIris) {
		if c, ok := CompararVersion(versionIris, m.IrisMin); ok && c < 0 {
			return false, "iris_antiguo"
		}
		return false, "iris_nuevo"
	}
	if c, ok := CompararVersion(m.Version, vozMin); !ok || c < 0 {
		return false, "paquete_antiguo"
	}
	return true, ""
}
