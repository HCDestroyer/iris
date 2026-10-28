package adjuntos

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// S3 guarda los adjuntos en un almacenamiento compatible con S3 (MinIO en dev, AWS S3 u otro).
// Solo usa PUT, GET y DELETE de objetos con firma AWS Signature V4, implementada aquí para no
// sumar el SDK de AWS ni minio-go (decenas de paquetes para tres llamadas).
type S3 struct {
	cfg  ConfigS3
	http *http.Client
	// ahora es el reloj de la firma (pruebas).
	ahora func() time.Time
}

// ConfigS3 sale de IRIS_S3_* (la misma configuración que crea el bucket en docker-compose).
type ConfigS3 struct {
	Endpoint   string // http://localhost:9000 o https://s3.us-east-1.amazonaws.com
	Region     string // us-east-1
	Bucket     string
	AccessKey  string
	SecretKey  string
	EstiloRuta bool // true: <endpoint>/<bucket>/<clave> (MinIO); false: <bucket>.<host>/<clave>
	// Timeout de cada llamada (2 min: un PUT de 25 MiB).
	Timeout time.Duration
}

// NuevoS3 valida la configuración.
func NuevoS3(cfg ConfigS3) (*S3, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("adjuntos: IRIS_S3_ENDPOINT inválido: %q", cfg.Endpoint)
	}
	if cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("adjuntos: S3 necesita IRIS_S3_BUCKET, IRIS_S3_ACCESS_KEY e IRIS_S3_SECRET_KEY")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Minute
	}
	cfg.Endpoint = strings.TrimRight(cfg.Endpoint, "/")
	return &S3{
		cfg: cfg, ahora: time.Now,
		http: &http.Client{Timeout: cfg.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

// Nombre implementa Almacen.
func (s *S3) Nombre() string { return "s3" }

func (s *S3) urlDe(clave string) (string, error) {
	if err := validarClave(clave); err != nil {
		return "", err
	}
	u, _ := url.Parse(s.cfg.Endpoint)
	if s.cfg.EstiloRuta {
		return s.cfg.Endpoint + "/" + url.PathEscape(s.cfg.Bucket) + "/" + clave, nil
	}
	return u.Scheme + "://" + s.cfg.Bucket + "." + u.Host + "/" + clave, nil
}

// sha256Vacio es el SHA-256 de un cuerpo vacío (GET y DELETE).
const sha256Vacio = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func (s *S3) pedir(ctx context.Context, metodo, clave string, cuerpo io.Reader, tamano int64, mime, sha256Hex string) (*http.Response, error) {
	u, err := s.urlDe(clave)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, metodo, u, cuerpo)
	if err != nil {
		return nil, err
	}
	if cuerpo != nil {
		req.ContentLength = tamano
		req.Header.Set("Content-Type", mime)
	}
	if sha256Hex == "" {
		sha256Hex = sha256Vacio
	}
	req.Header.Set("X-Amz-Content-Sha256", sha256Hex)
	FirmarV4(req, s.cfg.AccessKey, s.cfg.SecretKey, s.cfg.Region, "s3", sha256Hex, s.ahora())
	return s.http.Do(req)
}

// Guardar implementa Almacen (PUT de un solo objeto; el SHA-256 va firmado y S3 lo verifica).
func (s *S3) Guardar(ctx context.Context, clave string, r io.Reader, tamano int64, mime, sha256Hex string) error {
	resp, err := s.pedir(ctx, http.MethodPut, clave, io.LimitReader(r, tamano), tamano, mime, sha256Hex)
	if err != nil {
		return fmt.Errorf("adjuntos: S3 PUT: %w", err)
	}
	defer cerrar(resp)
	if resp.StatusCode/100 != 2 {
		return errorS3("PUT", resp)
	}
	return nil
}

// Abrir implementa Almacen.
func (s *S3) Abrir(ctx context.Context, clave string) (io.ReadCloser, error) {
	resp, err := s.pedir(ctx, http.MethodGet, clave, nil, 0, "", "")
	if err != nil {
		return nil, fmt.Errorf("adjuntos: S3 GET: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		cerrar(resp)
		return nil, ErrNoExiste
	}
	if resp.StatusCode/100 != 2 {
		defer cerrar(resp)
		return nil, errorS3("GET", resp)
	}
	return resp.Body, nil
}

// Borrar implementa Almacen.
func (s *S3) Borrar(ctx context.Context, clave string) error {
	resp, err := s.pedir(ctx, http.MethodDelete, clave, nil, 0, "", "")
	if err != nil {
		return fmt.Errorf("adjuntos: S3 DELETE: %w", err)
	}
	defer cerrar(resp)
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return errorS3("DELETE", resp)
	}
	return nil
}

func cerrar(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

// errorS3 resume la respuesta de error (<Error><Code>…</Code></Error>) sin el cuerpo completo.
func errorS3(op string, resp *http.Response) error {
	var e struct {
		Code string `xml:"Code"`
	}
	crudo, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	_ = xml.Unmarshal(crudo, &e)
	return fmt.Errorf("adjuntos: S3 %s respondió %d %s", op, resp.StatusCode, e.Code)
}

// FirmarV4 firma la petición con AWS Signature Version 4 (cabecera Authorization). Firma host y
// todas las cabeceras de la petición salvo User-Agent, Authorization y Content-Length; la
// petición debe llevar ya X-Amz-Content-Sha256 = payloadHash si el servicio lo exige (S3).
func FirmarV4(req *http.Request, acceso, secreto, region, servicio, payloadHash string, ahora time.Time) {
	t := ahora.UTC()
	fecha := t.Format("20060102")
	marca := t.Format("20060102T150405Z")
	req.Header.Set("X-Amz-Date", marca)

	cabeceras := map[string]string{"host": req.URL.Host}
	for nombre, valores := range req.Header {
		n := strings.ToLower(nombre)
		if n == "user-agent" || n == "authorization" || n == "content-length" {
			continue
		}
		vs := make([]string, len(valores))
		for i, v := range valores {
			vs[i] = strings.Join(strings.Fields(v), " ")
		}
		cabeceras[n] = strings.Join(vs, ",")
	}
	nombres := make([]string, 0, len(cabeceras))
	for n := range cabeceras {
		nombres = append(nombres, n)
	}
	sort.Strings(nombres)
	var canon strings.Builder
	for _, n := range nombres {
		canon.WriteString(n + ":" + cabeceras[n] + "\n")
	}
	firmadas := strings.Join(nombres, ";")

	ruta := req.URL.EscapedPath()
	if ruta == "" {
		ruta = "/"
	}
	peticion := strings.Join([]string{
		req.Method, ruta, consultaCanonica(req.URL.Query()), canon.String(), firmadas, payloadHash,
	}, "\n")
	alcance := fecha + "/" + region + "/" + servicio + "/aws4_request"
	h := sha256.Sum256([]byte(peticion))
	aFirmar := "AWS4-HMAC-SHA256\n" + marca + "\n" + alcance + "\n" + hex.EncodeToString(h[:])

	clave := hmacSHA256([]byte("AWS4"+secreto), fecha)
	clave = hmacSHA256(clave, region)
	clave = hmacSHA256(clave, servicio)
	clave = hmacSHA256(clave, "aws4_request")
	firma := hex.EncodeToString(hmacSHA256(clave, aFirmar))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+acceso+"/"+alcance+
		", SignedHeaders="+firmadas+", Signature="+firma)
}

func consultaCanonica(v url.Values) string {
	if len(v) == 0 {
		return ""
	}
	claves := make([]string, 0, len(v))
	for k := range v {
		claves = append(claves, k)
	}
	sort.Strings(claves)
	var partes []string
	for _, k := range claves {
		vs := append([]string(nil), v[k]...)
		sort.Strings(vs)
		for _, x := range vs {
			partes = append(partes, codificarAWS(k)+"="+codificarAWS(x))
		}
	}
	return strings.Join(partes, "&")
}

// codificarAWS: codificación URI de SigV4 (todo salvo A-Z a-z 0-9 - _ . ~).
func codificarAWS(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func hmacSHA256(clave []byte, dato string) []byte {
	m := hmac.New(sha256.New, clave)
	m.Write([]byte(dato))
	return m.Sum(nil)
}
