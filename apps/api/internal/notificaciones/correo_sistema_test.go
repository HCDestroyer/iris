package notificaciones_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/notificaciones"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte/correo"
)

// Correo del sistema configurable desde el panel (contratos/correo-sistema.md), contra Postgres real
// y un servidor SMTP de prueba en 127.0.0.1 (TLS implícito, STARTTLS o en claro).

const claveSMTP = "clave-smtp-muy-secreta"

// tlsCorreo genera una CA y un certificado de servidor para 127.0.0.1.
func tlsCorreo(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	clave, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	plantilla := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "iris-prueba"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, plantilla, plantilla, &clave.PublicKey, clave)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	raices := x509.NewCertPool()
	raices.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: clave}}, MinVersion: tls.VersionTLS12}, raices
}

// smtpPrueba es un servidor SMTP mínimo: guarda los mensajes, las credenciales y los comandos.
type smtpPrueba struct {
	puerto int
	mu     sync.Mutex
	// mensajes recibidos (cabeceras y cuerpo), sobres (MAIL FROM|RCPT TO), auth (usuario|contraseña) y comandos.
	mensajes []string
	sobres   []string
	auth     []string
	comandos []string
	// respuestas de rechazo
	rechazarAuth, rechazarRemitente, rechazarDestinatario bool
}

func nuevoSMTPPrueba(t *testing.T, cfg *tls.Config, modo string) *smtpPrueba {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if modo == correo.TLSImplicito {
		ln = tls.NewListener(ln, cfg)
	}
	s := &smtpPrueba{puerto: ln.Addr().(*net.TCPAddr).Port}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.atender(c, cfg, modo)
		}
	}()
	return s
}

func (s *smtpPrueba) atender(c net.Conn, cfg *tls.Config, modo string) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	enTLS := modo == correo.TLSImplicito
	r, w := bufio.NewReader(c), bufio.NewWriter(c)
	decir := func(l string) { _, _ = w.WriteString(l + "\r\n"); _ = w.Flush() }
	decir("220 smtp-prueba")
	var de string
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			return
		}
		l = strings.TrimRight(l, "\r\n")
		cmd := strings.ToUpper(strings.SplitN(l, " ", 2)[0])
		s.mu.Lock()
		s.comandos = append(s.comandos, cmd)
		rechazarAuth, rechazarRemitente, rechazarDestinatario := s.rechazarAuth, s.rechazarRemitente, s.rechazarDestinatario
		s.mu.Unlock()
		switch cmd {
		case "EHLO", "HELO":
			switch {
			case enTLS:
				decir("250-smtp-prueba")
				decir("250 AUTH PLAIN")
			case modo == correo.TLSStartTLS:
				decir("250-smtp-prueba")
				decir("250 STARTTLS")
			default:
				decir("250 smtp-prueba")
			}
		case "STARTTLS":
			if modo != correo.TLSStartTLS {
				decir("502 no")
				continue
			}
			decir("220 listo")
			t := tls.Server(c, cfg)
			if err := t.Handshake(); err != nil {
				return
			}
			c, enTLS = t, true
			r, w = bufio.NewReader(c), bufio.NewWriter(c)
		case "AUTH":
			if partes := strings.Fields(l); len(partes) == 3 {
				crudo, _ := base64.StdEncoding.DecodeString(partes[2])
				if campos := strings.Split(string(crudo), "\x00"); len(campos) == 3 {
					s.mu.Lock()
					s.auth = append(s.auth, campos[1]+"|"+campos[2])
					s.mu.Unlock()
				}
			}
			if rechazarAuth {
				decir("535 5.7.8 respuesta-cruda-del-servidor: credenciales malas")
				continue
			}
			decir("235 ok")
		case "MAIL":
			if rechazarRemitente {
				decir("550 5.7.1 respuesta-cruda-del-servidor: remitente no autorizado")
				continue
			}
			de = l
			decir("250 ok")
		case "RCPT":
			if rechazarDestinatario {
				decir("550 5.1.1 respuesta-cruda-del-servidor: buzón inexistente")
				continue
			}
			s.mu.Lock()
			s.sobres = append(s.sobres, de+"|"+l)
			s.mu.Unlock()
			decir("250 ok")
		case "RSET", "NOOP":
			decir("250 ok")
		case "DATA":
			decir("354 adelante")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			s.mu.Lock()
			s.mensajes = append(s.mensajes, b.String())
			s.mu.Unlock()
			decir("250 aceptado")
		case "QUIT":
			decir("221 adiós")
			return
		default:
			decir("502 no")
		}
	}
}

func (s *smtpPrueba) recibidos() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.mensajes...)
}

func (s *smtpPrueba) para(direccion string) int {
	n := 0
	for _, m := range s.recibidos() {
		if strings.Contains(m, "To: <"+direccion+">") {
			n++
		}
	}
	return n
}

func (s *smtpPrueba) credenciales() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auth...)
}

func (s *smtpPrueba) vio(cmd string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.comandos {
		if c == cmd {
			return true
		}
	}
	return false
}

func (s *smtpPrueba) rechazar(auth, remitente, destinatario bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rechazarAuth, s.rechazarRemitente, s.rechazarDestinatario = auth, remitente, destinatario
}

type entornoCorreo struct {
	*entorno
	tls, starttls, claro, otro *smtpPrueba
	cerrado                    int // un puerto permitido donde nadie escucha
	entornoSMTP                *correoFalso
	correos                    []int64
}

// nuevoEntornoCorreo arma el módulo con cifrado, la red local permitida, la CA de prueba y los
// puertos de los servidores de prueba como puertos permitidos. conEntorno: con IRIS_SMTP_* (falso).
func nuevoEntornoCorreo(t *testing.T, conEntorno bool) *entornoCorreo {
	t.Helper()
	base := nuevoEntorno(t, false)
	cfgTLS, raices := tlsCorreo(t)
	x := &entornoCorreo{entorno: base, tls: nuevoSMTPPrueba(t, cfgTLS, correo.TLSImplicito), starttls: nuevoSMTPPrueba(t, cfgTLS, correo.TLSStartTLS),
		claro: nuevoSMTPPrueba(t, cfgTLS, correo.TLSNinguna), otro: nuevoSMTPPrueba(t, cfgTLS, correo.TLSStartTLS)}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	x.cerrado = ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	clave := make([]byte, 32)
	if _, err := rand.Read(clave); err != nil {
		t.Fatal(err)
	}
	cif, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	x.reloj = time.Now().UTC()
	cfg := notificaciones.Config{BaseURL: "https://iris.prueba/", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Reloj: func() time.Time { return x.reloj }, Cifrador: cif,
		RedCorreo:     correo.Opciones{RedesPermitidas: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, Raices: raices, Timeout: 5 * time.Second},
		PuertosCorreo: []int{x.tls.puerto, x.starttls.puerto, x.claro.puerto, x.otro.puerto, x.cerrado, 587},
		Encolar: func(_ context.Context, _ pgx.Tx, a river.JobArgs) error {
			x.mu.Lock()
			defer x.mu.Unlock()
			x.correos = append(x.correos, a.(notificaciones.ArgsEnviarCorreo).CorreoID)
			return nil
		}}
	if conEntorno {
		x.entornoSMTP = &correoFalso{}
		cfg.Correo = x.entornoSMTP
	}
	x.notif = notificaciones.Nuevo(cfg)
	x.h, _ = servidor.Nuevo(servidor.Deps{BD: base.bd, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "prueba", Identidad: ident,
		Modulos: []func(huma.API){x.notif.Registrar}})
	// La base es compartida: los ajustes y las pruebas de otras pruebas no cuentan en esta, y los de
	// esta no quedan para las demás (un ajuste global activo cambiaría sus correos).
	limpiar := func() {
		for _, sql := range []string{`DELETE FROM notificaciones.correo_pruebas`, `DELETE FROM notificaciones.correo_ajustes`} {
			if _, err := base.bd.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	}
	limpiar()
	t.Cleanup(limpiar)
	return x
}

// conPermisos: una persona de esas empresas con un rol propio que solo da esos permisos (sin empresas.todas).
func (x *entornoCorreo) conPermisos(empresas []int64, permisos ...string) persona {
	x.t.Helper()
	p := x.persona(empresas)
	codigo := "correo_" + strings.ReplaceAll(pruebas.Unico(), "-", "_")
	var rol int64
	if err := x.bd.QueryRow(x.ctx, `INSERT INTO nucleo.roles (codigo, nombre) VALUES ($1, $1) RETURNING id`, codigo).Scan(&rol); err != nil {
		x.t.Fatal(err)
	}
	for _, permiso := range permisos {
		if _, err := x.bd.Exec(x.ctx, `INSERT INTO nucleo.roles_permisos (rol_id, permiso) VALUES ($1, $2)`, rol, permiso); err != nil {
			x.t.Fatal(err)
		}
	}
	if _, err := x.bd.Exec(x.ctx, `INSERT INTO nucleo.usuarios_roles (usuario_id, rol_id) VALUES ($1, $2)`, p.id, rol); err != nil {
		x.t.Fatal(err)
	}
	return p
}

// formulario: los campos para el servidor de prueba dado.
func formulario(s *smtpPrueba, seguridad string, extra map[string]any) map[string]any {
	f := map[string]any{"host": "127.0.0.1", "puerto": s.puerto, "seguridad": seguridad, "usuario": "avisos@isp.example",
		"contrasena": claveSMTP, "remitenteNombre": "Iris Avisos", "remitenteCorreo": "no-responder@isp.example"}
	for k, v := range extra {
		if v == nil {
			delete(f, k)
			continue
		}
		f[k] = v
	}
	return f
}

func (x *entornoCorreo) guardar(p persona, ambito string, version int, s *smtpPrueba, seguridad string, extra map[string]any) respuesta {
	x.t.Helper()
	f := formulario(s, seguridad, extra)
	f["version"] = version
	if _, ok := f["activo"]; !ok {
		f["activo"] = true
	}
	return x.api(p, http.MethodPost, "/api/v1/correo-sistema/"+ambito+"/guardar", f)
}

func (x *entornoCorreo) probar(p persona, ambito string, s *smtpPrueba, seguridad, para string, extra map[string]any) respuesta {
	x.t.Helper()
	f := formulario(s, seguridad, extra)
	f["para"] = para
	return x.api(p, http.MethodPost, "/api/v1/correo-sistema/"+ambito+"/probar", f)
}

// sinSecreto: la contraseña no quedó en claro en ninguna parte de la base.
func (x *entornoCorreo) sinSecreto(secreto string) {
	x.t.Helper()
	for _, sql := range []string{
		`SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%' OR error::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM auditoria.cambios WHERE antes::text LIKE '%' || $1 || '%' OR despues::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM notificaciones.correo_ajustes WHERE position(convert_to($1, 'UTF8') in contrasena_cifrada) > 0`,
		`SELECT count(*) FROM nucleo.idempotencia WHERE position(convert_to($1, 'UTF8') in respuesta) > 0`,
	} {
		if n := x.contar(sql, secreto); n != 0 {
			x.t.Fatalf("la contraseña quedó en claro (%d): %s", n, sql)
		}
	}
}

func TestCorreoSistemaGuardarVerYActivar(t *testing.T) {
	x := nuevoEntornoCorreo(t, false)
	admin := x.persona([]int64{1}, "admin")
	ruta := "/api/v1/correo-sistema/global"

	v := x.esperar(x.api(admin, http.MethodGet, ruta, nil), http.StatusOK).cuerpo
	if v["configurado"] != false || v["origen"] != "ninguno" || v["enUso"] != false || v["version"] != float64(0) || v["tieneContrasena"] != false ||
		v["seguridad"] != "starttls" || v["puerto"] != float64(587) || v["empresaId"] != nil {
		t.Fatalf("sin ajuste: %v", v)
	}

	// Crear: la contraseña no vuelve, queda cifrada y como *** en la auditoría.
	r := x.esperar(x.guardar(admin, "global", 0, x.starttls, "starttls", nil), http.StatusOK)
	v = r.cuerpo
	if v["configurado"] != true || v["tieneContrasena"] != true || v["origen"] != "global" || v["enUso"] != true || v["version"] != float64(1) ||
		v["host"] != "127.0.0.1" || v["remitenteCorreo"] != "no-responder@isp.example" || v["activo"] != true {
		t.Fatalf("guardado: %v", v)
	}
	if strings.Contains(r.crudo, claveSMTP) || strings.Contains(r.crudo, "contrasena\"") {
		t.Fatalf("la respuesta trae la contraseña: %s", r.crudo)
	}
	x.sinSecreto(claveSMTP)
	if n := x.contar(`SELECT count(*) FROM notificaciones.correo_ajustes WHERE empresa_id IS NULL AND contrasena_cifrada IS NOT NULL`); n != 1 {
		t.Fatalf("contraseña cifrada guardada: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.cambios c JOIN auditoria.acciones a ON a.id = c.accion_id
		WHERE a.accion = 'correo_sistema.guardar' AND a.actor_id = $1 AND c.campo = 'contrasena' AND c.despues = '"***"'`, admin.id); n != 1 {
		t.Fatalf("la contraseña como *** en cambios: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'correo_sistema.guardar' AND actor_id = $1 AND entrada->>'contrasena' = '***'`, admin.id); n != 1 {
		t.Fatalf("la contraseña como *** en la entrada: %d", n)
	}
	ev := x.ultimoEvento("correo_sistema.ajustes_cambiados")
	if ev["empresa_id"] != nil || ev["activo"] != true || !strings.Contains(fmt.Sprint(ev["campos"]), "contrasena") || !strings.Contains(fmt.Sprint(ev["campos"]), "host") {
		t.Fatalf("evento: %v", ev)
	}

	// Versión vieja: 409. Sin contraseña y el mismo servidor: se conserva.
	x.esperar(x.guardar(admin, "global", 0, x.starttls, "starttls", nil), http.StatusConflict)
	v = x.esperar(x.guardar(admin, "global", 1, x.starttls, "starttls", map[string]any{"contrasena": nil, "remitenteNombre": "Iris"}), http.StatusOK).cuerpo
	if v["tieneContrasena"] != true || v["remitenteNombre"] != "Iris" || v["version"] != float64(2) {
		t.Fatalf("conserva la contraseña: %v", v)
	}
	if ev := x.ultimoEvento("correo_sistema.ajustes_cambiados"); fmt.Sprint(ev["campos"]) != "[remitente_nombre]" {
		t.Fatalf("campos: %v", ev)
	}
	// Otro servidor sin volver a escribir la contraseña: 422 (la guardada no viaja a otro lado).
	r = x.esperar(x.guardar(admin, "global", 2, x.otro, "starttls", map[string]any{"contrasena": nil}), http.StatusUnprocessableEntity)
	if !strings.Contains(r.crudo, "contrasena_requerida") {
		t.Fatalf("otro servidor: %s", r.crudo)
	}
	// Sin usuario: la contraseña guardada se borra.
	v = x.esperar(x.guardar(admin, "global", 2, x.starttls, "starttls", map[string]any{"contrasena": nil, "usuario": ""}), http.StatusOK).cuerpo
	if v["tieneContrasena"] != false || v["usuario"] != "" {
		t.Fatalf("sin usuario: %v", v)
	}

	// Desactivar: el ajuste se conserva pero no se usa. Activar: vuelve.
	x.esperar(x.api(admin, http.MethodPost, ruta+"/desactivar", map[string]any{"version": 1}), http.StatusConflict)
	v = x.esperar(x.api(admin, http.MethodPost, ruta+"/desactivar", map[string]any{"version": 3}), http.StatusOK).cuerpo
	if v["activo"] != false || v["origen"] != "ninguno" || v["enUso"] != false || v["configurado"] != true {
		t.Fatalf("desactivado: %v", v)
	}
	if ev := x.ultimoEvento("correo_sistema.ajustes_cambiados"); ev["activo"] != false || fmt.Sprint(ev["campos"]) != "[activo]" {
		t.Fatalf("evento de desactivar: %v", ev)
	}
	v = x.esperar(x.api(admin, http.MethodPost, ruta+"/activar", map[string]any{"version": 4}), http.StatusOK).cuerpo
	if v["activo"] != true || v["origen"] != "global" || v["enUso"] != true {
		t.Fatalf("activado: %v", v)
	}
	// Activar el de una empresa que no tiene ajuste: 404.
	x.esperar(x.api(admin, http.MethodPost, "/api/v1/correo-sistema/1/activar", map[string]any{"version": 1}), http.StatusNotFound)
}

func TestCorreoSistemaValidaciones(t *testing.T) {
	x := nuevoEntornoCorreo(t, false)
	admin := x.persona([]int64{1}, "admin")
	casos := []struct {
		nombre string
		extra  map[string]any
		seg    string
		quiere string
	}{
		{"puerto fuera de la lista", map[string]any{"puerto": 8080}, "starttls", "puerto"},
		{"puerto de otro servicio", map[string]any{"puerto": 5432}, "starttls", "puerto"},
		{"host con salto de línea", map[string]any{"host": "127.0.0.1\r\nRCPT TO:<x@y.z>"}, "starttls", "host"},
		{"host con esquema", map[string]any{"host": "smtp://127.0.0.1"}, "starttls", "host"},
		{"host con puerto", map[string]any{"host": "localhost:25"}, "starttls", "host"},
		{"metadata de la nube", map[string]any{"host": "169.254.169.254"}, "starttls", "destino_no_permitido"},
		{"red privada no permitida", map[string]any{"host": "10.1.2.3"}, "starttls", "destino_no_permitido"},
		{"loopback IPv6 no permitido", map[string]any{"host": "::1"}, "starttls", "destino_no_permitido"},
		{"nombre con cabecera inyectada", map[string]any{"remitenteNombre": "Iris\r\nBcc: espia@malo.example"}, "starttls", "remitenteNombre"},
		{"remitente con cabecera inyectada", map[string]any{"remitenteCorreo": "a@isp.example\nBcc: espia@malo.example"}, "starttls", "remitenteCorreo"},
		{"dos remitentes", map[string]any{"remitenteCorreo": "a@isp.example, b@isp.example"}, "starttls", "remitenteCorreo"},
		{"remitente con nombre", map[string]any{"remitenteCorreo": "Ana <a@isp.example>"}, "starttls", "remitenteCorreo"},
		{"usuario con salto de línea", map[string]any{"usuario": "ana\r\nMAIL FROM:<x@y.z>"}, "starttls", "usuario"},
		{"usuario sin contraseña", map[string]any{"contrasena": ""}, "starttls", "contrasena_requerida"},
		{"contraseña sin usuario", map[string]any{"usuario": ""}, "starttls", "usuario"},
		{"sin cifrado con credenciales", nil, "ninguna", "sin_cifrado_sin_credenciales"},
		{"sin cifrado con contraseña", map[string]any{"usuario": ""}, "ninguna", "sin_cifrado_sin_credenciales"},
		{"sin cifrado fuera de la red interna", map[string]any{"host": "8.8.8.8", "usuario": "", "contrasena": nil}, "ninguna", "sin_cifrado_solo_red_interna"},
		{"seguridad desconocida", nil, "ssl", "seguridad"},
	}
	for _, c := range casos {
		r := x.guardar(admin, "global", 0, x.starttls, c.seg, c.extra)
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.crudo, c.quiere) {
			t.Errorf("%s: %d %s", c.nombre, r.status, r.crudo)
		}
		// La prueba valida lo mismo (y no llega a conectarse).
		r = x.probar(admin, "global", x.starttls, c.seg, "ana@isp.example", c.extra)
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.crudo, c.quiere) {
			t.Errorf("probar, %s: %d %s", c.nombre, r.status, r.crudo)
		}
	}
	for _, para := range []string{"ana@isp.example\r\nBcc: espia@malo.example", "ana@isp.example, otro@isp.example", "Ana <ana@isp.example>", "no-es-correo"} {
		if r := x.probar(admin, "global", x.starttls, "starttls", para, nil); r.status != http.StatusUnprocessableEntity || !strings.Contains(r.crudo, "para") {
			t.Errorf("para %q: %d %s", para, r.status, r.crudo)
		}
	}
	if n := x.contar(`SELECT count(*) FROM notificaciones.correo_ajustes`) + x.contar(`SELECT count(*) FROM notificaciones.correo_pruebas`); n != 0 {
		t.Fatalf("nada debió guardarse ni probarse: %d", n)
	}
	if len(x.starttls.recibidos()) != 0 || x.starttls.vio("EHLO") {
		t.Fatal("una entrada inválida no debe llegar al servidor")
	}
	x.sinSecreto(claveSMTP)
}

func TestCorreoSistemaProbarSinGuardar(t *testing.T) {
	x := nuevoEntornoCorreo(t, false)
	admin := x.persona([]int64{1}, "admin")

	// STARTTLS con usuario y contraseña: sale un correo de asunto y cuerpo fijos, a una sola dirección.
	r := x.esperar(x.probar(admin, "global", x.starttls, "starttls", "ana@isp.example", nil), http.StatusOK).cuerpo
	if r["estado"] != "ok" || r["causa"] != nil || !strings.Contains(fmt.Sprint(r["detalle"]), "ana@isp.example") {
		t.Fatalf("prueba: %v", r)
	}
	ms := x.starttls.recibidos()
	if len(ms) != 1 || !strings.Contains(ms[0], "Subject: Correo de prueba de Iris") || !strings.Contains(ms[0], "To: <ana@isp.example>") ||
		!strings.Contains(ms[0], `From: "Iris Avisos" <no-responder@isp.example>`) || !strings.Contains(ms[0], "correo de prueba de Iris") {
		t.Fatalf("mensaje: %q", ms)
	}
	if c := x.starttls.credenciales(); len(c) != 1 || c[0] != "avisos@isp.example|"+claveSMTP {
		t.Fatalf("credenciales: %v", c)
	}
	// No guarda nada, pero deja la última prueba, la auditoría y el evento (sin host, destino ni credenciales).
	v := x.esperar(x.api(admin, http.MethodGet, "/api/v1/correo-sistema/global", nil), http.StatusOK).cuerpo
	if v["configurado"] != false || v["ultimaPrueba"].(map[string]any)["estado"] != "ok" {
		t.Fatalf("tras probar: %v", v)
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'correo_sistema.probar' AND actor_id = $1 AND resultado = 'ok'
		AND entrada->>'estado' = 'ok'`, admin.id); n != 1 {
		t.Fatalf("auditoría de la prueba: %d", n)
	}
	ev := x.ultimoEvento("correo_sistema.probado")
	if ev["estado"] != "ok" || ev["causa"] != nil || ev["empresa_id"] != nil || ev["usuario_id"] != float64(admin.id) || len(ev) != 4 {
		t.Fatalf("evento: %v", ev)
	}
	x.sinSecreto(claveSMTP)

	// TLS implícito.
	if r := x.esperar(x.probar(admin, "global", x.tls, "tls", "ana@isp.example", nil), http.StatusOK).cuerpo; r["estado"] != "ok" || len(x.tls.recibidos()) != 1 {
		t.Fatalf("tls: %v", r)
	}
	// Las causas, de lista cerrada y sin la respuesta cruda del servidor.
	x.reloj = x.reloj.Add(2 * time.Hour)
	falla := func(nombre, causa string, r respuesta) {
		t.Helper()
		x.esperar(r, http.StatusOK)
		if r.cuerpo["estado"] != "error" || r.cuerpo["causa"] != causa || r.cuerpo["detalle"] == "" || strings.Contains(r.crudo, "respuesta-cruda") ||
			strings.Contains(r.crudo, "127.0.0.1") || strings.Contains(r.crudo, "535") || strings.Contains(r.crudo, "550") {
			t.Fatalf("%s: %s", nombre, r.crudo)
		}
		if ev := x.ultimoEvento("correo_sistema.probado"); ev["estado"] != "error" || ev["causa"] != causa {
			t.Fatalf("%s, evento: %v", nombre, ev)
		}
	}
	x.starttls.rechazar(true, false, false)
	falla("credenciales", "credenciales", x.probar(admin, "global", x.starttls, "starttls", "ana@isp.example", nil))
	x.starttls.rechazar(false, true, false)
	falla("remitente", "remitente_rechazado", x.probar(admin, "global", x.starttls, "starttls", "ana@isp.example", nil))
	x.starttls.rechazar(false, false, true)
	falla("destinatario", "destinatario_rechazado", x.probar(admin, "global", x.starttls, "starttls", "ana@isp.example", nil))
	x.starttls.rechazar(false, false, false)
	falla("puerto cerrado", "no_conecta", x.probar(admin, "global", x.starttls, "starttls", "ana@isp.example", map[string]any{"puerto": x.cerrado}))
	// STARTTLS contra un servidor que no lo ofrece: no se mandan credenciales en claro.
	falla("sin STARTTLS", "seguridad", x.probar(admin, "global", x.claro, "starttls", "ana@isp.example", nil))
	if len(x.claro.credenciales()) != 0 || len(x.claro.recibidos()) != 0 {
		t.Fatal("no debió autenticarse ni enviar en claro")
	}
	x.reloj = x.reloj.Add(2 * time.Hour)
	// TLS implícito contra un puerto que no habla TLS.
	falla("no habla TLS", "seguridad", x.probar(admin, "global", x.claro, "tls", "ana@isp.example", nil))
	if v := x.esperar(x.api(admin, http.MethodGet, "/api/v1/correo-sistema/global", nil), http.StatusOK).cuerpo["ultimaPrueba"].(map[string]any); v["estado"] != "error" || v["causa"] != "seguridad" {
		t.Fatalf("última prueba: %v", v)
	}
	x.sinSecreto(claveSMTP)
}

// Un certificado que no firma una autoridad conocida: causa «certificado», sin mandar credenciales.
func TestCorreoSistemaProbarCertificadoNoValido(t *testing.T) {
	x := nuevoEntornoCorreo(t, false)
	admin := x.persona([]int64{1}, "admin")
	otraCA, _ := tlsCorreo(t)
	ajeno := nuevoSMTPPrueba(t, otraCA, correo.TLSImplicito)
	r := x.esperar(x.probar(admin, "global", x.tls, "tls", "ana@isp.example", map[string]any{"puerto": ajeno.puerto}), http.StatusUnprocessableEntity)
	if !strings.Contains(r.crudo, "puerto") {
		t.Fatalf("puerto no permitido: %s", r.crudo)
	}
	// Mismo módulo con ese puerto permitido.
	x2 := nuevoEntornoCorreoCon(t, x, ajeno.puerto)
	r = x2.esperar(x2.probar(admin, "global", x.tls, "tls", "ana@isp.example", map[string]any{"puerto": ajeno.puerto}), http.StatusOK)
	if r.cuerpo["estado"] != "error" || r.cuerpo["causa"] != "certificado" || len(ajeno.credenciales()) != 0 || len(ajeno.recibidos()) != 0 {
		t.Fatalf("certificado: %s", r.crudo)
	}
}

// nuevoEntornoCorreoCon: otro módulo sobre el mismo entorno con un puerto permitido más.
func nuevoEntornoCorreoCon(t *testing.T, x *entornoCorreo, puerto int) *entornoCorreo {
	t.Helper()
	_, raices := tlsCorreo(t)
	y := *x
	y.notif = notificaciones.Nuevo(notificaciones.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Reloj: func() time.Time { return x.reloj },
		RedCorreo:     correo.Opciones{RedesPermitidas: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, Raices: raices, Timeout: 5 * time.Second},
		PuertosCorreo: []int{puerto}})
	y.h, _ = servidor.Nuevo(servidor.Deps{BD: x.bd, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "prueba", Identidad: ident,
		Modulos: []func(huma.API){y.notif.Registrar}})
	return &y
}

// Sin contraseña en el formulario se usa la guardada, pero solo hacia el servidor guardado.
func TestCorreoSistemaProbarConLaContrasenaGuardada(t *testing.T) {
	x := nuevoEntornoCorreo(t, false)
	admin := x.persona([]int64{1}, "admin")
	x.esperar(x.guardar(admin, "global", 0, x.starttls, "starttls", nil), http.StatusOK)

	r := x.esperar(x.probar(admin, "global", x.starttls, "starttls", "ana@isp.example", map[string]any{"contrasena": nil}), http.StatusOK).cuerpo
	if c := x.starttls.credenciales(); r["estado"] != "ok" || len(c) != 1 || c[0] != "avisos@isp.example|"+claveSMTP {
		t.Fatalf("con la guardada: %v %v", r, c)
	}
	// Otro servidor, otro puerto u otro usuario: no se manda la guardada (422), y el servidor no ve nada.
	for nombre, extra := range map[string]map[string]any{
		"otro servidor": {"contrasena": nil, "puerto": x.otro.puerto},
		"otro usuario":  {"contrasena": nil, "usuario": "otro@isp.example"},
	} {
		r := x.probar(admin, "global", x.starttls, "starttls", "ana@isp.example", extra)
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.crudo, "contrasena_requerida") {
			t.Fatalf("%s: %d %s", nombre, r.status, r.crudo)
		}
	}
	if len(x.otro.credenciales()) != 0 || x.otro.vio("EHLO") || len(x.starttls.credenciales()) != 1 {
		t.Fatal("la contraseña guardada no debe salir hacia otro servidor ni con otro usuario")
	}
	// La de otro ámbito tampoco: la empresa 1 no tiene guardada.
	r2 := x.probar(admin, "1", x.starttls, "starttls", "ana@isp.example", map[string]any{"contrasena": nil})
	if r2.status != http.StatusUnprocessableEntity || !strings.Contains(r2.crudo, "contrasena_requerida") {
		t.Fatalf("otro ámbito: %d %s", r2.status, r2.crudo)
	}
}

// La prueba no es un escáner ni un relay: 5 por usuario y 20 por instalación cada hora.
func TestCorreoSistemaProbarTopes(t *testing.T) {
	x := nuevoEntornoCorreo(t, false)
	admin := x.persona([]int64{1}, "admin")
	for i := range notificaciones.TopePruebasUsuario {
		// Las fallidas también cuentan.
		extra := map[string]any{}
		if i%2 == 1 {
			extra["puerto"] = x.cerrado
		}
		x.esperar(x.probar(admin, "global", x.starttls, "starttls", "ana@isp.example", extra), http.StatusOK)
		x.reloj = x.reloj.Add(time.Minute)
	}
	r := x.esperar(x.probar(admin, "global", x.starttls, "starttls", "ana@isp.example", nil), http.StatusTooManyRequests)
	if !strings.Contains(r.crudo, "demasiadas_pruebas") || len(x.starttls.recibidos()) != 3 {
		t.Fatalf("tope por usuario: %s (%d)", r.crudo, len(x.starttls.recibidos()))
	}
	// Otra persona sí puede; pasada la hora, la primera también.
	otra := x.persona([]int64{1}, "admin")
	x.esperar(x.probar(otra, "global", x.starttls, "starttls", "ana@isp.example", nil), http.StatusOK)
	x.reloj = x.reloj.Add(56 * time.Minute)
	x.esperar(x.probar(admin, "global", x.starttls, "starttls", "ana@isp.example", nil), http.StatusOK)

	// Tope de la instalación: 20 en la hora, de quien sean.
	if _, err := x.bd.Exec(x.ctx, `DELETE FROM notificaciones.correo_pruebas`); err != nil {
		t.Fatal(err)
	}
	for range notificaciones.TopePruebasInstalacion {
		u := x.persona([]int64{1})
		if _, err := x.bd.Exec(x.ctx, `INSERT INTO notificaciones.correo_pruebas (usuario_id, estado, creado_en) VALUES ($1, 'ok', $2)`,
			u.id, x.reloj.Add(-10*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	antes := len(x.starttls.recibidos())
	r = x.esperar(x.probar(admin, "global", x.starttls, "starttls", "ana@isp.example", nil), http.StatusTooManyRequests)
	if !strings.Contains(r.crudo, "demasiadas_pruebas") || len(x.starttls.recibidos()) != antes {
		t.Fatalf("tope de la instalación: %s", r.crudo)
	}
}

// Alcance: el global exige operar sin límite de empresa; el de una empresa, estar operando en ella.
func TestCorreoSistemaAlcance(t *testing.T) {
	x := nuevoEntornoCorreo(t, false)
	deEmpresa := x.conPermisos([]int64{1}, "correo_sistema.ver", "correo_sistema.configurar")
	soloVer := x.conPermisos([]int64{1}, "correo_sistema.ver")
	sinPermiso := x.persona([]int64{1}, "supervisor")

	x.esperar(x.api(deEmpresa, http.MethodGet, "/api/v1/correo-sistema/global", nil), http.StatusForbidden)
	x.esperar(x.guardar(deEmpresa, "global", 0, x.starttls, "starttls", nil), http.StatusForbidden)
	x.esperar(x.probar(deEmpresa, "global", x.starttls, "starttls", "ana@isp.example", nil), http.StatusForbidden)
	x.esperar(x.api(deEmpresa, http.MethodGet, "/api/v1/correo-sistema/2", nil), http.StatusNotFound)
	x.esperar(x.guardar(deEmpresa, "2", 0, x.starttls, "starttls", nil), http.StatusNotFound)
	x.esperar(x.api(deEmpresa, http.MethodGet, "/api/v1/correo-sistema/abc", nil), http.StatusUnprocessableEntity)

	v := x.esperar(x.guardar(deEmpresa, "1", 0, x.starttls, "starttls", nil), http.StatusOK).cuerpo
	if v["ambito"] != "1" || v["empresaId"] != float64(1) || v["origen"] != "empresa" || v["enUso"] != true {
		t.Fatalf("ajuste de la empresa: %v", v)
	}
	if ev := x.ultimoEvento("correo_sistema.ajustes_cambiados"); ev["empresa_id"] != float64(1) {
		t.Fatalf("evento: %v", ev)
	}
	// Solo ver: lee, pero no guarda, ni prueba, ni activa.
	if v := x.esperar(x.api(soloVer, http.MethodGet, "/api/v1/correo-sistema/1", nil), http.StatusOK).cuerpo; v["host"] != "127.0.0.1" || v["tieneContrasena"] != true {
		t.Fatalf("solo ver: %v", v)
	}
	x.esperar(x.guardar(soloVer, "1", 1, x.starttls, "starttls", nil), http.StatusForbidden)
	x.esperar(x.probar(soloVer, "1", x.starttls, "starttls", "ana@isp.example", nil), http.StatusForbidden)
	x.esperar(x.api(soloVer, http.MethodPost, "/api/v1/correo-sistema/1/desactivar", map[string]any{"version": 1}), http.StatusForbidden)
	// De fábrica solo admin: un supervisor no lo ve.
	x.esperar(x.api(sinPermiso, http.MethodGet, "/api/v1/correo-sistema/1", nil), http.StatusForbidden)
	if len(x.starttls.recibidos()) != 0 {
		t.Fatal("nadie sin permiso debió mandar correo")
	}
	for _, rol := range nucleo.RolesBase {
		tiene := false
		for _, p := range rol.Permisos {
			tiene = tiene || p == nucleo.PermCorreoSistemaVer || p == nucleo.PermCorreoSistemaConfigurar
		}
		if tiene {
			t.Fatalf("de fábrica solo admin tiene el correo del sistema: %s", rol.Codigo)
		}
	}
}

// Precedencia en cada envío: empresa → global → entorno → ninguno, sin reiniciar; un ajuste apagado se salta.
func TestCorreoSistemaPrecedenciaAlEnviar(t *testing.T) {
	x := nuevoEntornoCorreo(t, true)
	admin := x.persona([]int64{1}, "admin")
	// Quien recibe el aviso del tope del asistente de la empresa 1.
	p := x.conPermisos([]int64{1}, "mensajeria.asistente_configurar")
	mes := 0
	avisarYMandar := func() {
		t.Helper()
		mes++
		if _, err := x.notif.AvisarTope.Ejecutar(x.ctx, notificaciones.EntradaAvisarTope{EventoID: uuid.Must(uuid.NewV7()), EmpresaID: emp(1),
			Porcentaje: 80, Mes: fmt.Sprintf("2041-%02d", mes), Tokens: 1_600_000, Tope: 2_000_000}); err != nil {
			t.Fatal(err)
		}
		x.mu.Lock()
		ids := x.correos
		x.correos = nil
		x.mu.Unlock()
		for _, id := range ids {
			if err := x.notif.EnviarCorreo(x.ctx, id, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	cuenta := func() [3]int {
		return [3]int{x.tls.para(p.correo), x.starttls.para(p.correo), len(x.entornoSMTP.para(p.correo))}
	}

	// Sin ajustes: sale por el del entorno.
	if v := x.esperar(x.api(admin, http.MethodGet, "/api/v1/correo-sistema/1", nil), http.StatusOK).cuerpo; v["origen"] != "entorno" || v["enUso"] != false ||
		v["entorno"].(map[string]any)["configurado"] != true {
		t.Fatalf("origen sin ajustes: %v", v)
	}
	avisarYMandar()
	if c := cuenta(); c != [3]int{0, 0, 1} {
		t.Fatalf("por el entorno: %v", c)
	}
	// Global (STARTTLS) y de la empresa 1 (TLS): gana el de la empresa.
	x.esperar(x.guardar(admin, "global", 0, x.starttls, "starttls", nil), http.StatusOK)
	avisarYMandar()
	if c := cuenta(); c != [3]int{0, 1, 1} {
		t.Fatalf("por el global: %v", c)
	}
	x.esperar(x.guardar(admin, "1", 0, x.tls, "tls", map[string]any{"remitenteCorreo": "avisos@empresa1.example"}), http.StatusOK)
	avisarYMandar()
	if c := cuenta(); c != [3]int{1, 1, 1} {
		t.Fatalf("por el de la empresa: %v", c)
	}
	if m := x.tls.recibidos(); !strings.Contains(m[len(m)-1], "<avisos@empresa1.example>") || !strings.Contains(m[len(m)-1], "@empresa1.example>\r\nMIME-Version") {
		t.Fatalf("remitente de la empresa: %q", m[len(m)-1])
	}
	if c := x.tls.credenciales(); len(c) == 0 || c[len(c)-1] != "avisos@isp.example|"+claveSMTP {
		t.Fatalf("credenciales descifradas al enviar: %v", c)
	}
	// El global dice que el de la empresa no es el suyo; el de la empresa, que está en uso.
	if v := x.esperar(x.api(admin, http.MethodGet, "/api/v1/correo-sistema/1", nil), http.StatusOK).cuerpo; v["origen"] != "empresa" || v["enUso"] != true {
		t.Fatalf("origen de la empresa: %v", v)
	}
	if v := x.esperar(x.api(admin, http.MethodGet, "/api/v1/correo-sistema/global", nil), http.StatusOK).cuerpo; v["origen"] != "global" || v["enUso"] != true {
		t.Fatalf("origen global: %v", v)
	}
	// Apagar el de la empresa no bloquea: se usa el global. Apagar el global: el entorno.
	v := x.esperar(x.api(admin, http.MethodPost, "/api/v1/correo-sistema/1/desactivar", map[string]any{"version": 1}), http.StatusOK).cuerpo
	if v["origen"] != "global" || v["enUso"] != false {
		t.Fatalf("empresa apagada: %v", v)
	}
	avisarYMandar()
	if c := cuenta(); c != [3]int{1, 2, 1} {
		t.Fatalf("empresa apagada, por el global: %v", c)
	}
	v = x.esperar(x.api(admin, http.MethodPost, "/api/v1/correo-sistema/global/desactivar", map[string]any{"version": 1}), http.StatusOK).cuerpo
	if v["origen"] != "entorno" || v["enUso"] != false {
		t.Fatalf("global apagado: %v", v)
	}
	avisarYMandar()
	if c := cuenta(); c != [3]int{1, 2, 2} {
		t.Fatalf("todo apagado, por el entorno: %v", c)
	}
	if n := x.contar(`SELECT count(*) FROM notificaciones.correos WHERE usuario_id = $1 AND estado = 'enviado'`, p.id); n != 5 {
		t.Fatalf("correos enviados: %d", n)
	}
	x.sinSecreto(claveSMTP)
}

// Sin entorno ni ajuste activo el correo queda omitido; con un ajuste guardado desde el panel, sale.
func TestCorreoSistemaSinEntorno(t *testing.T) {
	x := nuevoEntornoCorreo(t, false)
	admin := x.persona([]int64{1}, "admin")
	p := x.conPermisos([]int64{1}, "mensajeria.asistente_configurar")
	avisar := func(mes string) notificaciones.ResultadoAvisarTope {
		t.Helper()
		r, err := x.notif.AvisarTope.Ejecutar(x.ctx, notificaciones.EntradaAvisarTope{EventoID: uuid.Must(uuid.NewV7()), EmpresaID: emp(1),
			Porcentaje: 80, Mes: mes, Tokens: 1_600_000, Tope: 2_000_000})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := avisar("2042-01"); r.CorreosPendientes != 0 || r.CorreosOmitidos == 0 {
		t.Fatalf("sin correo: %+v", r)
	}
	if n := x.contar(`SELECT count(*) FROM notificaciones.correos WHERE usuario_id = $1 AND estado = 'omitido' AND motivo = 'sin_correo_sistema'`, p.id); n != 1 {
		t.Fatalf("omitido: %d", n)
	}
	// Relay interno sin cifrado ni credenciales (dentro de la red permitida).
	v := x.esperar(x.guardar(admin, "global", 0, x.claro, "ninguna", map[string]any{"usuario": "", "contrasena": nil}), http.StatusOK).cuerpo
	if v["seguridad"] != "ninguna" || v["tieneContrasena"] != false || v["origen"] != "global" {
		t.Fatalf("relay interno: %v", v)
	}
	if r := avisar("2042-02"); r.CorreosPendientes == 0 || r.CorreosOmitidos != 0 {
		t.Fatalf("con correo: %+v", r)
	}
	for _, id := range x.correos {
		if err := x.notif.EnviarCorreo(x.ctx, id, true); err != nil {
			t.Fatal(err)
		}
	}
	if x.claro.para(p.correo) != 1 || x.claro.vio("AUTH") || x.claro.vio("STARTTLS") {
		t.Fatalf("por el relay interno: %d", x.claro.para(p.correo))
	}
	// El ajuste se quitó entre que se encoló y se envió: fallido, sin reintentos, con su evento.
	if r := avisar("2042-03"); r.CorreosPendientes == 0 {
		t.Fatalf("pendientes: %+v", r)
	}
	x.esperar(x.api(admin, http.MethodPost, "/api/v1/correo-sistema/global/desactivar", map[string]any{"version": 1}), http.StatusOK)
	x.mu.Lock()
	ids := x.correos
	x.mu.Unlock()
	for _, id := range ids[len(ids)-1:] {
		if err := x.notif.EnviarCorreo(x.ctx, id, false); err != nil {
			t.Fatal(err)
		}
		if n := x.contar(`SELECT count(*) FROM notificaciones.correos WHERE id = $1 AND estado = 'fallido'`, id); n != 1 {
			t.Fatalf("fallido sin correo del sistema: %d", n)
		}
	}
}

// El modo sin cifrado nunca lleva credenciales ni sale de la red permitida (soporte/correo).
func TestCorreoSinCifradoSoloRedInternaYSinCredenciales(t *testing.T) {
	cfgTLS, _ := tlsCorreo(t)
	s := nuevoSMTPPrueba(t, cfgTLS, correo.TLSNinguna)
	msj := correo.Saliente{De: mail.Address{Address: "a@isp.example"}, Para: mail.Address{Address: "b@isp.example"}, Asunto: "x", Texto: "x",
		MessageID: "x@isp.example", Fecha: time.Now()}
	local := correo.Opciones{RedesPermitidas: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, Timeout: 5 * time.Second}
	srv := correo.Servidor{Host: "127.0.0.1", Puerto: s.puerto, TLS: correo.TLSNinguna}
	if err := correo.Enviar(context.Background(), local, srv, msj); err != nil || len(s.recibidos()) != 1 {
		t.Fatalf("relay interno: %v", err)
	}
	conCredenciales := srv
	conCredenciales.Usuario, conCredenciales.Contrasena = "ana", claveSMTP
	err := correo.Enviar(context.Background(), local, conCredenciales, msj)
	if e := correo.Resumir(err); err == nil || e.Codigo != "claro_no_permitido" || notificaciones.CausaDeCorreo(err) != "seguridad" {
		t.Fatalf("con credenciales: %v", err)
	}
	// Fuera de las redes permitidas (aquí, sin ninguna): no conecta.
	err = correo.Enviar(context.Background(), correo.Opciones{Timeout: 5 * time.Second}, srv, msj)
	if err == nil || notificaciones.CausaDeCorreo(err) != "destino_no_permitido" {
		t.Fatalf("fuera de la red permitida: %v", err)
	}
	if len(s.recibidos()) != 1 || len(s.credenciales()) != 0 {
		t.Fatal("solo debió salir el primero y sin credenciales")
	}
}
