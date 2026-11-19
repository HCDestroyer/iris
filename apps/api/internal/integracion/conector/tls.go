package conector

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// Certificado TLS que presentó el otro sistema en una llamada (contrato Iris ↔ ArgosISP 0.8.0 §1.5,
// «registrar y avisar»): solo informativo. Nunca se fija ni se compara para cortar una llamada; la
// cadena ya la validó TLS contra las CA públicas (una que no valida ni llega aquí).
type CertificadoVisto struct {
	// HuellaSHA256: SHA-256 del certificado hoja (DER), en hexadecimal minúscula.
	HuellaSHA256 string
	Emisor       string
	Sujeto       string
	// Host al que se llamó (el de la base del conector).
	Host    string
	VenceEn time.Time
}

type claveCapturaTLS struct{}

// capturaTLS guarda el último certificado visto en las llamadas hechas con un contexto.
type capturaTLS struct {
	mu    sync.Mutex
	visto *CertificadoVisto
}

// ConCapturaTLS devuelve un contexto con el que las llamadas del cliente anotan el certificado TLS
// del otro sistema, y la función que lo lee (nil si ninguna llamada fue por HTTPS).
func ConCapturaTLS(ctx context.Context) (context.Context, func() *CertificadoVisto) {
	c := &capturaTLS{}
	return context.WithValue(ctx, claveCapturaTLS{}, c), func() *CertificadoVisto {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.visto == nil {
			return nil
		}
		v := *c.visto
		return &v
	}
}

func anotarTLS(ctx context.Context, host string, resp *http.Response) {
	c, ok := ctx.Value(claveCapturaTLS{}).(*capturaTLS)
	if !ok || resp == nil || resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		return
	}
	v := DescribirCertificado(resp.TLS.PeerCertificates[0], host)
	c.mu.Lock()
	c.visto = &v
	c.mu.Unlock()
}

// DescribirCertificado: huella, emisor, sujeto y vencimiento del certificado hoja.
func DescribirCertificado(cert *x509.Certificate, host string) CertificadoVisto {
	suma := sha256.Sum256(cert.Raw)
	return CertificadoVisto{
		HuellaSHA256: hex.EncodeToString(suma[:]), Emisor: recortar(cert.Issuer.String(), 300),
		Sujeto: recortar(cert.Subject.String(), 300), Host: recortar(host, 255), VenceEn: cert.NotAfter.UTC(),
	}
}
