package sippuente

import (
	"crypto/md5" //nolint:gosec // RFC 2617/3261: Digest MD5 es lo que pide Asterisk; no es una elección de Iris
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strings"
)

// Desafio es un WWW-Authenticate o Proxy-Authenticate de tipo Digest (RFC 3261 §22.4, RFC 7616).
type Desafio struct {
	Realm     string
	Nonce     string
	Opaque    string
	Algoritmo string // MD5 (de fábrica) o SHA-256
	Qop       string // "auth" o vacío
	Stale     bool
}

var errDesafio = errors.New("sippuente: desafío no válido")

// LeerDesafio interpreta el valor de un WWW-Authenticate / Proxy-Authenticate.
func LeerDesafio(valor string) (Desafio, error) {
	v := strings.TrimSpace(valor)
	if len(v) < 7 || !strings.EqualFold(v[:6], "Digest") {
		return Desafio{}, errDesafio
	}
	ps := parametrosDigest(v[6:])
	d := Desafio{Realm: ps["realm"], Nonce: ps["nonce"], Opaque: ps["opaque"], Algoritmo: strings.ToUpper(ps["algorithm"]), Stale: strings.EqualFold(ps["stale"], "true")}
	if d.Algoritmo == "" {
		d.Algoritmo = "MD5"
	}
	if d.Algoritmo != "MD5" && d.Algoritmo != "SHA-256" {
		return Desafio{}, errDesafio
	}
	if d.Nonce == "" || strings.ContainsAny(d.Realm+d.Nonce+d.Opaque, "\"\r\n\\") {
		return Desafio{}, errDesafio
	}
	if q := ps["qop"]; q != "" {
		for _, x := range strings.Split(q, ",") {
			if strings.EqualFold(strings.TrimSpace(x), "auth") {
				d.Qop = "auth"
			}
		}
		if d.Qop == "" {
			return Desafio{}, errDesafio // solo auth (auth-int no se usa en SIP sobre WebRTC)
		}
	}
	return d, nil
}

// parametrosDigest separa «clave=valor» y «clave="valor, con comas"».
func parametrosDigest(s string) map[string]string {
	out := map[string]string{}
	for len(s) > 0 {
		s = strings.TrimLeft(s, " \t,")
		i := strings.IndexByte(s, '=')
		if i <= 0 {
			break
		}
		clave := strings.ToLower(strings.TrimSpace(s[:i]))
		s = strings.TrimLeft(s[i+1:], " \t")
		var valor string
		if strings.HasPrefix(s, `"`) {
			j := strings.IndexByte(s[1:], '"')
			if j < 0 {
				break
			}
			valor, s = s[1:1+j], s[2+j:]
		} else {
			j := strings.IndexByte(s, ',')
			if j < 0 {
				valor, s = strings.TrimSpace(s), ""
			} else {
				valor, s = strings.TrimSpace(s[:j]), s[j+1:]
			}
		}
		out[clave] = valor
	}
	return out
}

// Responder arma el valor de Authorization / Proxy-Authorization para metodo y uri con la
// credencial. nc es el contador del nonce (1 en el primer uso).
func Responder(d Desafio, usuario, secreto, metodo, uri string, nc int) (string, error) {
	cnonce, err := cnonceAleatorio()
	if err != nil {
		return "", err
	}
	return responderCon(d, usuario, secreto, metodo, uri, nc, cnonce), nil
}

func responderCon(d Desafio, usuario, secreto, metodo, uri string, nc int, cnonce string) string {
	h := func(s string) string {
		var f hash.Hash
		if d.Algoritmo == "SHA-256" {
			f = sha256.New()
		} else {
			f = md5.New() //nolint:gosec // ver el import
		}
		f.Write([]byte(s))
		return hex.EncodeToString(f.Sum(nil))
	}
	ha1 := h(usuario + ":" + d.Realm + ":" + secreto)
	ha2 := h(metodo + ":" + uri)
	var b strings.Builder
	fmt.Fprintf(&b, `Digest username="%s",realm="%s",nonce="%s",uri="%s"`, usuario, d.Realm, d.Nonce, uri)
	if d.Qop == "auth" {
		ncs := fmt.Sprintf("%08x", nc)
		resp := h(ha1 + ":" + d.Nonce + ":" + ncs + ":" + cnonce + ":auth:" + ha2)
		fmt.Fprintf(&b, `,response="%s",cnonce="%s",nc=%s,qop=auth`, resp, cnonce, ncs)
	} else {
		fmt.Fprintf(&b, `,response="%s"`, h(ha1+":"+d.Nonce+":"+ha2))
	}
	fmt.Fprintf(&b, ",algorithm=%s", d.Algoritmo)
	if d.Opaque != "" {
		fmt.Fprintf(&b, `,opaque="%s"`, d.Opaque)
	}
	return b.String()
}

// VerificarDigest comprueba un Authorization recibido (lo usa el Asterisk falso de las pruebas).
func VerificarDigest(autorizacion, secreto, metodo, nonce string) bool {
	v := strings.TrimSpace(autorizacion)
	if len(v) < 7 || !strings.EqualFold(v[:6], "Digest") {
		return false
	}
	ps := parametrosDigest(v[6:])
	if ps["nonce"] != nonce {
		return false
	}
	d := Desafio{Realm: ps["realm"], Nonce: nonce, Algoritmo: strings.ToUpper(ps["algorithm"]), Qop: ps["qop"]}
	if d.Algoritmo == "" {
		d.Algoritmo = "MD5"
	}
	nc := 0
	if d.Qop == "auth" {
		if _, err := fmt.Sscanf(ps["nc"], "%x", &nc); err != nil {
			return false
		}
	}
	esperado := parametrosDigest(responderCon(d, ps["username"], secreto, metodo, ps["uri"], nc, ps["cnonce"])[6:])
	return esperado["response"] != "" && esperado["response"] == ps["response"]
}

func cnonceAleatorio() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
