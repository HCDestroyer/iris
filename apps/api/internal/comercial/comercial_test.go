package comercial_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

var ident = identidad.Nuevo(identidad.Config{})

// Firma de prueba (como la de un pad de firma: <path d> con M y L).
const firmaSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="80"><path d="M 10 40 L 60 20 L 120 60 L 190 30"/></svg>`

// --- Argos falso (contrato §2.1 y §3) ---------------------------------------------------------

type argosFalso struct {
	srv       *httptest.Server
	mu        sync.Mutex
	altas     map[string]int64          // Idempotency-Key → id creado
	cuerpos   map[string]map[string]any // Idempotency-Key → cuerpo de la primera llamada
	llamadas  map[string]int            // Idempotency-Key → llamadas recibidas
	siguiente int64
	caido     bool
}

func nuevoArgos(t *testing.T) *argosFalso {
	a := &argosFalso{altas: map[string]int64{}, cuerpos: map[string]map[string]any{}, llamadas: map[string]int{}, siguiente: 9000}
	json200 := func(w http.ResponseWriter, v string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/integraciones/iris/conexion/canjear", func(w http.ResponseWriter, _ *http.Request) {
		json200(w, `{"token":"atk_prueba_comercial","empresas":[1,2]}`)
	})
	mux.HandleFunc("GET /api/v1/zonas", func(w http.ResponseWriter, _ *http.Request) {
		json200(w, `{"data":[{"id":7001,"empresa_id":1,"nombre":"Centro"},{"id":7002,"empresa_id":2,"nombre":"Otra"}],"nextCursor":null}`)
	})
	mux.HandleFunc("GET /api/v1/routers", func(w http.ResponseWriter, _ *http.Request) {
		json200(w, `{"data":[{"id":51,"empresa_id":1,"nombre":"RT-Centro","zona_id":7001,"lat":14.6000,"lon":-90.5000},
			{"id":53,"empresa_id":1,"nombre":"RT-Sin-Coordenadas","zona_id":7001},
			{"id":52,"empresa_id":2,"nombre":"RT-Otra","zona_id":7002,"lat":14.6000,"lon":-90.5000}],"nextCursor":null}`)
	})
	mux.HandleFunc("GET /api/v1/planes", func(w http.ResponseWriter, _ *http.Request) {
		json200(w, `{"data":[{"id":300,"empresa_id":1,"nombre":"Fibra 100","precio":"250.00","estado":"activo"},
			{"id":301,"empresa_id":1,"nombre":"Fibra 200 (sin precio)","precio":null,"estado":"activo"},
			{"id":400,"empresa_id":2,"nombre":"Plan de otra empresa","precio":"99.00","estado":"activo"}],"nextCursor":null}`)
	})
	alta := func(w http.ResponseWriter, r *http.Request) {
		clave := r.Header.Get("Idempotency-Key")
		var c map[string]any
		_ = json.NewDecoder(r.Body).Decode(&c)
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.caido {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		a.llamadas[clave]++
		id, ok := a.altas[clave]
		if !ok {
			a.siguiente++
			id = a.siguiente
			a.altas[clave], a.cuerpos[clave] = id, c
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"data":{"id":%d}}`, id)
	}
	mux.HandleFunc("POST /api/v1/clientes", alta)
	mux.HandleFunc("POST /api/v1/servicios", alta)
	a.srv = httptest.NewServer(mux)
	t.Cleanup(a.srv.Close)
	return a
}

func (a *argosFalso) cuerpo(clave string) map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cuerpos[clave]
}

func (a *argosFalso) altasDistintas() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.altas)
}

// --- Entorno -----------------------------------------------------------------------------------

type entorno struct {
	t     *testing.T
	am    *argos.Modulo
	pend  *pendientes.Modulo
	cmp   *campo.Modulo
	com   *comercial.Modulo
	bus   *integracion.Bus
	falso *argosFalso
	conID int64
	ver   int32
}

func nuevoEntorno(t *testing.T, reloj func() time.Time) *entorno {
	t.Helper()
	comercial.RegistrarAdjuntos()
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	cif, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	e := &entorno{t: t, falso: nuevoArgos(t), bus: integracion.NuevoBus()}
	e.am = argos.Nuevo(argos.Config{
		Cifrador: cif, BaseURL: "https://iris.prueba", PermitirHTTP: true, Timeout: 2 * time.Second,
		RedesPermitidas: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
	})
	e.pend = pendientes.Nuevo(pendientes.Config{Argos: e.am})
	e.cmp = campo.Nuevo(campo.Config{Adjuntos: campo.SinAdjuntosObligatorios{}})
	almacen, err := adjuntos.NuevoDisco(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.com = comercial.Nuevo(comercial.Config{
		Reloj: reloj, Planes: comercial.PlanesDeArgos(e.am), Altas: comercial.AltasDeArgos(e.am),
		Documentos: comercial.DocumentosDeAdjuntos(adjuntos.Nuevo(adjuntos.Config{Almacen: almacen})), Campo: e.cmp,
	})
	e.com.SuscribirAltas(e.bus, pruebas.Pool(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return e
}

// limpiarConector deja la base sin conector ni empresas (las demás pruebas corren sin conector).
func limpiarConector(t *testing.T) {
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM integracion.pendientes", "DELETE FROM integracion.conectores",
			"DELETE FROM integracion.argos_routers", "DELETE FROM integracion.argos_zonas",
			"DELETE FROM nucleo.usuarios_empresas", "DELETE FROM nucleo.empresas"} {
			if _, err := pruebas.Pool(t).Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
}

func sistema(t *testing.T) context.Context {
	return nucleo.ConBD(contexto.Sistema(context.Background(), contexto.CanalWorker), pruebas.Pool(t))
}

// conectar canjea el enlace (registra las empresas 1 y 2), activa el conector y trae zonas y routers.
func (e *entorno) conectar() {
	e.t.Helper()
	c, err := e.am.Conectar.Ejecutar(sistema(e.t), argos.EntradaConectar{Enlace: e.falso.srv.URL + "/conectar#codigo-de-prueba-comercial"})
	if err != nil {
		e.t.Fatal(err)
	}
	a, err := e.am.ActivarConector.Ejecutar(sistema(e.t), argos.EntradaCambioConector{ID: c.ID, Version: c.Version})
	if err != nil {
		e.t.Fatal(err)
	}
	e.conID, e.ver = a.ID, a.Version
	if _, err := e.am.SincronizarCatalogos.Ejecutar(sistema(e.t), struct{}{}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *entorno) apagar() {
	e.t.Helper()
	a, err := e.am.DesactivarConector.Ejecutar(sistema(e.t), argos.EntradaCambioConector{ID: e.conID, Version: e.ver, Motivo: "prueba"})
	if err != nil {
		e.t.Fatal(err)
	}
	e.ver = a.Version
}

func (e *entorno) encender() {
	e.t.Helper()
	a, err := e.am.ActivarConector.Ejecutar(sistema(e.t), argos.EntradaCambioConector{ID: e.conID, Version: e.ver})
	if err != nil {
		e.t.Fatal(err)
	}
	e.ver = a.Version
}

// procesar corre la cola de pendientes y luego el relay (suscriptor de altas).
func (e *entorno) procesar() {
	e.t.Helper()
	if _, err := e.pend.Procesar(sistema(e.t)); err != nil {
		e.t.Fatal(err)
	}
	r := &integracion.Relay{BD: pruebas.Pool(e.t), Bus: e.bus, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := r.Ejecutar(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

// --- Ayudantes ---------------------------------------------------------------------------------

func usuarioCon(t *testing.T, roles ...identidad.AsignacionRol) int64 {
	t.Helper()
	u, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Vendedor de prueba", Correo: pruebas.Correo("ventas"), Contrasena: "contrasena-larga-1", Roles: roles,
	})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func rol(r string) identidad.AsignacionRol { return identidad.AsignacionRol{Rol: r} }

func permisosDe(r string) []nucleo.Permiso {
	for _, b := range nucleo.RolesBase {
		if b.Codigo == r {
			return b.PermisosDeRol()
		}
	}
	panic(r)
}

// como: un usuario con los permisos globales del rol, en modo sin conector.
func como(t *testing.T, id int64, r string) context.Context {
	return pruebas.ContextoUsuario(pruebas.Pool(t), id, permisosDe(r)...)
}

// enEmpresa: como, con la empresa activa dada.
func enEmpresa(t *testing.T, id, empresa int64, r string) context.Context {
	return pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), id, empresa, permisosDe(r)...)
}

// conZona: los permisos del rol limitados a una zona.
func conZona(ctx context.Context, r string, zona int64) context.Context {
	mapa := map[string][]contexto.Alcance{}
	for _, p := range permisosDe(r) {
		mapa[string(p)] = []contexto.Alcance{{ZonaID: zona}}
	}
	return contexto.ConActor(ctx, contexto.ActorDe(ctx), contexto.NuevosPermisos(mapa))
}

func asignarEmpresa(t *testing.T, usuario, empresa int64) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"INSERT INTO nucleo.usuarios_empresas (usuario_id, empresa_id) VALUES ($1, $2)", usuario, empresa); err != nil {
		t.Fatal(err)
	}
}

func deber[T any](v T, err error) func(*testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func problema(t *testing.T, err error, status int, codigo string) {
	t.Helper()
	var p *nucleo.Problema
	if !errors.As(err, &p) || p.Status != status || p.Codigo() != codigo {
		t.Fatalf("esperaba %d %s, llegó %v", status, codigo, err)
	}
}

func ptr[T any](v T) *T { return &v }

func contar(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pruebas.Pool(t).QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func zonaLocal(t *testing.T, nombre string) int64 {
	t.Helper()
	z, err := agenda.Nuevo(agenda.Config{}).CrearZona.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), agenda.EntradaCrearZona{Nombre: nombre + " " + pruebas.Unico()})
	if err != nil {
		t.Fatal(err)
	}
	return z.ID
}

// eventos del outbox de una entidad (por prospecto_id en el payload).
func eventosDe(t *testing.T, prospecto int64) []string {
	t.Helper()
	filas, err := pruebas.Pool(t).Query(context.Background(),
		"SELECT evento FROM integracion.outbox WHERE payload->>'prospecto_id' = $1 ORDER BY id", fmt.Sprint(prospecto))
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	var salida []string
	for filas.Next() {
		var e string
		if err := filas.Scan(&e); err != nil {
			t.Fatal(err)
		}
		salida = append(salida, e)
	}
	return salida
}

func contiene(lista []string, buscados ...string) bool {
	for _, b := range buscados {
		hallado := false
		for _, l := range lista {
			if l == b {
				hallado = true
			}
		}
		if !hallado {
			return false
		}
	}
	return true
}

// lineaPlanLocal: plan sin conector (descripción y precio a mano).
func lineaPlanLocal() comercial.EntradaLinea {
	return comercial.EntradaLinea{Tipo: "plan", Descripcion: ptr("Fibra 50 local"), PrecioUnitario: ptr("199.99")}
}

// hastaContrato lleva el prospecto de nuevo a contrato firmado (factibilidad manual si no
// es viable por distancia).
func hastaContrato(t *testing.T, com *comercial.Modulo, ctx context.Context, id int64, lineas []comercial.EntradaLinea, manual string) comercial.ProspectoDetalle {
	t.Helper()
	p := deber(com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: id}))(t)
	if p.Estado == comercial.EstadoNuevo {
		deber(com.Contactar.Ejecutar(ctx, comercial.EntradaTransicion{ID: id, Version: p.Version}))(t)
		p = deber(com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: id}))(t)
	}
	entrada := comercial.EntradaFactibilidad{ProspectoID: id, Version: p.Version}
	if manual != "" {
		entrada.Manual, entrada.Notas = &comercial.EntradaManual{Resultado: manual, Caja: ptr("CJ-01")}, "Visita técnica: hay poste"
	}
	deber(com.EvaluarFactibilidad(ctx, entrada))(t)
	p = deber(com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: id}))(t)
	c := deber(com.CrearCotizacion(ctx, comercial.EntradaCrearCotizacion{ProspectoID: id, Version: p.Version, Lineas: lineas}))(t)
	deber(com.AceptarCotizacion.Ejecutar(ctx, comercial.EntradaAceptarCotizacion{ID: c.ID, Version: c.Version}))(t)
	p = deber(com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: id}))(t)
	ctr := deber(com.CrearContrato(ctx, comercial.EntradaCrearContrato{ProspectoID: id, Version: p.Version}))(t)
	deber(com.FirmarContrato(ctx, comercial.EntradaFirmar{ContratoID: ctr.ID, Version: ctr.Version, FirmanteNombre: "Ana López",
		FirmanteDocumento: ptr("1234567890101"), SVG: firmaSVG}))(t)
	return deber(com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: id}))(t)
}

// --- Pruebas -----------------------------------------------------------------------------------

// Sin conector (modo local): el embudo completo; ganar crea la orden con los datos capturados.
func TestEmbudoSinConector(t *testing.T) {
	e := nuevoEntorno(t, nil)
	v := usuarioCon(t, rol("ventas"))
	ctx := como(t, v, "ventas")
	zona := zonaLocal(t, "Zona local")

	p := deber(e.com.CrearProspecto.Ejecutar(ctx, comercial.EntradaCrearProspecto{Nombre: "Ana López", Direccion: "4a calle 1-23, zona 1",
		Telefono: ptr("5555-1234"), Lat: ptr(14.61), Lon: ptr(-90.51), ZonaID: &zona, Origen: "referido", OrigenDetalle: ptr("Juan")}))(t)
	if p.Estado != comercial.EstadoNuevo || p.EmpresaID != nil || !strings.HasPrefix(p.Numero, "PR-") {
		t.Fatalf("prospecto: %+v", p)
	}
	deber(e.com.RegistrarActividad.Ejecutar(ctx, comercial.EntradaActividad{ProspectoID: p.ID, Tipo: "llamada", Descripcion: "Le interesa fibra",
		ProximaAccion: ptr("Visita"), ProximaFecha: ptr(time.Now().Add(24 * time.Hour))}))(t)
	p = deber(e.com.Contactar.Ejecutar(ctx, comercial.EntradaTransicion{ID: p.ID, Version: p.Version}))(t)

	// Factibilidad por distancia sin conector: indeterminada (no se inventa).
	fa := deber(e.com.EvaluarFactibilidad(ctx, comercial.EntradaFactibilidad{ProspectoID: p.ID, Version: p.Version}))(t)
	if fa.Resultado != comercial.FactibleIndeterminado || fa.Motivo == nil || *fa.Motivo != comercial.MotivoSinConector {
		t.Fatalf("factibilidad sin conector: %+v", fa)
	}
	d := deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: p.ID}))(t)
	_, err := e.com.CrearCotizacion(ctx, comercial.EntradaCrearCotizacion{ProspectoID: p.ID, Version: d.Version, Lineas: []comercial.EntradaLinea{lineaPlanLocal()}})
	problema(t, err, 422, "factibilidad_no_viable")
	// Manual sin notas: 422; con la visita, viable.
	_, err = e.com.EvaluarFactibilidad(ctx, comercial.EntradaFactibilidad{ProspectoID: p.ID, Version: d.Version, Manual: &comercial.EntradaManual{Resultado: "viable"}})
	problema(t, err, 422, "validacion")

	// Sin conector, el plan va a mano (planId → 422).
	d2 := hastaContratoParcial(t, e.com, ctx, p.ID)
	_, err = e.com.CrearCotizacion(ctx, comercial.EntradaCrearCotizacion{ProspectoID: p.ID, Version: d2.Version,
		Lineas: []comercial.EntradaLinea{{Tipo: "plan", PlanID: ptr(int64(300))}}})
	problema(t, err, 422, "validacion")
	d = hastaContrato(t, e.com, ctx, p.ID, []comercial.EntradaLinea{lineaPlanLocal(),
		{Tipo: "instalacion", Descripcion: ptr("Instalación"), PrecioUnitario: ptr("100")},
		{Tipo: "equipo", Descripcion: ptr("ONU"), Cantidad: "2", PrecioUnitario: ptr("150.50")},
		{Tipo: "descuento", Descripcion: ptr("Promoción"), PrecioUnitario: ptr("50.25")},
	}, "viable")
	if d.Estado != comercial.EstadoContrato || d.Contrato == nil || d.Contrato.Estado != "firmado" || d.Cotizacion.TotalMensual != "199.99" ||
		d.Cotizacion.TotalUnico != "350.75" || d.Cotizacion.Estado != "aceptada" {
		t.Fatalf("antes de ganar: %+v %+v %+v", d.Prospecto, d.Contrato, d.Cotizacion)
	}
	// PDF de la cotización, borrador y firmado: adjuntos completos; el SHA-256 en la auditoría.
	if d.Cotizacion.PDF == nil || d.Cotizacion.PDF.Estado != "completo" || d.Contrato.PDF.Estado != "completo" ||
		d.Contrato.PDFFirmado == nil || d.Contrato.PDFFirmado.Estado != "completo" || d.Contrato.PDFFirmado.SHA256 == nil {
		t.Fatalf("PDF: %+v %+v %+v", d.Cotizacion.PDF, d.Contrato.PDF, d.Contrato.PDFFirmado)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'contratos.firmar' AND resultado = 'ok' AND entrada->'auditoria'->>'pdf_firmado_sha256' = $1",
		*d.Contrato.PDFFirmado.SHA256); n != 1 {
		t.Fatalf("hash en la auditoría: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.cambios WHERE entidad = 'contrato' AND campo = 'firma_svg' AND despues = '\"***\"'::jsonb"); n == 0 {
		t.Fatal("la firma debe quedar enmascarada en auditoria.cambios")
	}

	// Ganar sin conector: orden de instalación con los datos capturados, sin ids de Argos.
	g := deber(e.com.Ganar.Ejecutar(ctx, comercial.EntradaGanar{ID: p.ID, Version: d.Version}))(t)
	if g.Estado != comercial.EstadoGanado || g.Alta == nil || g.Alta.Modo != comercial.AltaLocal || g.Alta.Estado != comercial.AltaCompleta ||
		g.Alta.OrdenID == nil || g.Alta.ClienteArgosID != nil {
		t.Fatalf("ganado: %+v %+v", g, g.Alta)
	}
	o := deber(e.cmp.VerOrden.Ejecutar(sistema(t), campo.EntradaVerOrden{ID: *g.Alta.OrdenID}))(t)
	if o.Tipo != "instalacion" || o.Origen != "sistema" || o.ProspectoID == nil || *o.ProspectoID != p.ID || o.ClienteID != nil ||
		o.Direccion != "4a calle 1-23, zona 1" || o.ZonaID == nil || *o.ZonaID != zona {
		t.Fatalf("orden: %+v", o)
	}
	if !contiene(eventosDe(t, p.ID), "prospecto.creado", "prospecto.estado_cambiado", "factibilidad.evaluada", "cotizacion.creada",
		"cotizacion.aceptada", "contrato.creado", "contrato.firmado", "prospecto.ganado") {
		t.Fatalf("eventos: %v", eventosDe(t, p.ID))
	}
	// Ganado es final: ni perder ni ganar otra vez.
	_, err = e.com.Perder.Ejecutar(ctx, comercial.EntradaPerder{ID: p.ID, Version: g.Version, Motivo: "x"})
	problema(t, err, 422, "transicion_invalida")
	if n := contar(t, "SELECT count(*) FROM campo.ordenes WHERE prospecto_id = $1", p.ID); n != 1 {
		t.Fatalf("órdenes del prospecto: %d", n)
	}
}

// hastaContratoParcial: deja el prospecto con factibilidad manual viable (para probar cotizar).
func hastaContratoParcial(t *testing.T, com *comercial.Modulo, ctx context.Context, id int64) comercial.ProspectoDetalle {
	t.Helper()
	p := deber(com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: id}))(t)
	deber(com.EvaluarFactibilidad(ctx, comercial.EntradaFactibilidad{ProspectoID: id, Version: p.Version,
		Manual: &comercial.EntradaManual{Resultado: "viable"}, Notas: "Visita técnica"}))(t)
	return deber(com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: id}))(t)
}

// Con conector: factibilidad por distancia, planes de Argos, ganar con los dos pendientes y la
// orden de instalación con el cliente y el servicio de Argos, sin duplicar.
func TestEmbudoConArgos(t *testing.T) {
	limpiarConector(t)
	e := nuevoEntorno(t, nil)
	e.conectar()
	v := usuarioCon(t, rol("ventas"))
	asignarEmpresa(t, v, 1)
	ctx := enEmpresa(t, v, 1, "ventas")

	// A 100 m del router RT-Centro (14.6000, -90.5000): viable.
	cerca := 14.6000 + 100.0/111_195
	p := deber(e.com.CrearProspecto.Ejecutar(ctx, comercial.EntradaCrearProspecto{Nombre: "Comercial Las Flores", Direccion: "Calzada 5-10",
		Documento: ptr("1234567-8"), Lat: &cerca, Lon: ptr(-90.5), ZonaID: ptr(int64(7001))}))(t)
	if p.EmpresaID == nil || *p.EmpresaID != 1 {
		t.Fatalf("empresa: %+v", p)
	}
	fa := deber(e.com.EvaluarFactibilidad(ctx, comercial.EntradaFactibilidad{ProspectoID: p.ID, Version: p.Version}))(t)
	if fa.Resultado != comercial.FactibleViable || fa.RouterID == nil || *fa.RouterID != 51 || fa.DistanciaM == nil ||
		*fa.DistanciaM < 99 || *fa.DistanciaM > 101 || fa.Motor != "distancia" {
		t.Fatalf("factibilidad: %+v", fa)
	}

	// Planes: solo los de la empresa activa.
	pl := deber(e.com.Planes(ctx))(t)
	if !pl.Disponible || len(pl.Data) != 2 {
		t.Fatalf("planes: %+v", pl)
	}
	d := deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: p.ID}))(t)
	// El plan de otra empresa no existe para esta; sin planId con conector, 422; el precio de Argos no se pisa.
	for _, l := range []comercial.EntradaLinea{{Tipo: "plan", PlanID: ptr(int64(400))}, lineaPlanLocal(),
		{Tipo: "plan", PlanID: ptr(int64(300)), PrecioUnitario: ptr("1.00")}, {Tipo: "plan", PlanID: ptr(int64(301))}} {
		_, err := e.com.CrearCotizacion(ctx, comercial.EntradaCrearCotizacion{ProspectoID: p.ID, Version: d.Version, Lineas: []comercial.EntradaLinea{l}})
		problema(t, err, 422, "validacion")
	}
	// Dos planes: 422 plan_requerido; descuento mayor que el total: 422.
	_, err := e.com.CrearCotizacion(ctx, comercial.EntradaCrearCotizacion{ProspectoID: p.ID, Version: d.Version,
		Lineas: []comercial.EntradaLinea{{Tipo: "plan", PlanID: ptr(int64(300))}, {Tipo: "plan", PlanID: ptr(int64(301)), PrecioUnitario: ptr("10")}}})
	problema(t, err, 422, "plan_requerido")
	_, err = e.com.CrearCotizacion(ctx, comercial.EntradaCrearCotizacion{ProspectoID: p.ID, Version: d.Version,
		Lineas: []comercial.EntradaLinea{{Tipo: "plan", PlanID: ptr(int64(300))}, {Tipo: "descuento", Descripcion: ptr("x"), PrecioUnitario: ptr("300"), Recurrente: ptr(true)}}})
	problema(t, err, 422, "descuento_excesivo")

	lineas := []comercial.EntradaLinea{{Tipo: "plan", PlanID: ptr(int64(300))}, {Tipo: "instalacion", Descripcion: ptr("Instalación fibra"), PrecioUnitario: ptr("0")}}
	d = hastaContrato(t, e.com, ctx, p.ID, lineas, "")
	if d.Cotizacion.PlanID == nil || *d.Cotizacion.PlanID != 300 || d.Cotizacion.TotalMensual != "250.00" || d.Cotizacion.Lineas[0].Descripcion != "Fibra 100" {
		t.Fatalf("cotización con Argos: %+v", d.Cotizacion)
	}

	// Ganar: encola POST /clientes (iris:cliente:<id>); todavía sin orden.
	g := deber(e.com.Ganar.Ejecutar(ctx, comercial.EntradaGanar{ID: p.ID, Version: d.Version}))(t)
	if g.Alta == nil || g.Alta.Modo != comercial.AltaArgos || g.Alta.Estado != comercial.AltaEsperandoCliente || g.Alta.OrdenID != nil {
		t.Fatalf("ganado con Argos: %+v", g.Alta)
	}
	d = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: p.ID}))(t)
	if len(d.Pendientes) != 1 || d.Pendientes[0].IdempotencyKey != comercial.ClaveCliente(p.ID) || d.Pendientes[0].Estado != "pendiente" {
		t.Fatalf("pendientes al ganar: %+v", d.Pendientes)
	}

	// Argos caído: el pendiente se reintenta (sigue pendiente) y nada avanza.
	e.falso.mu.Lock()
	e.falso.caido = true
	e.falso.mu.Unlock()
	e.procesar()
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE idempotency_key = $1 AND estado = 'pendiente' AND intentos = 1", comercial.ClaveCliente(p.ID)); n != 1 {
		t.Fatal("con Argos caído el pendiente queda para reintentar")
	}
	e.falso.mu.Lock()
	e.falso.caido = false
	e.falso.mu.Unlock()
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.pendientes SET proximo_intento = now() WHERE estado = 'pendiente'"); err != nil {
		t.Fatal(err)
	}

	// POST /clientes hecho → comercial encola POST /servicios (pendiente_instalacion), que depende del primero.
	e.procesar()
	cli := e.falso.cuerpo(comercial.ClaveCliente(p.ID))
	if cli == nil || cli["nombre"] != "Comercial Las Flores" || cli["origen_tipo"] != "iris_prospecto" || cli["empresa_id"] != float64(1) {
		t.Fatalf("POST /clientes: %+v", cli)
	}
	d = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: p.ID}))(t)
	if d.Alta.Estado != comercial.AltaEsperandoServicio || d.Alta.ClienteArgosID == nil || len(d.Pendientes) != 2 {
		t.Fatalf("tras el cliente: %+v %+v", d.Alta, d.Pendientes)
	}
	if n := contar(t, `SELECT count(*) FROM integracion.pendientes s JOIN integracion.pendientes c ON c.id = ANY (s.depende_de)
		WHERE s.idempotency_key = $1 AND c.idempotency_key = $2`, comercial.ClaveServicio(p.ID), comercial.ClaveCliente(p.ID)); n != 1 {
		t.Fatal("el servicio depende del cliente")
	}

	// POST /servicios hecho → orden de instalación con el cliente y el servicio de Argos.
	e.procesar()
	srv := e.falso.cuerpo(comercial.ClaveServicio(p.ID))
	if srv == nil || srv["estado"] != "pendiente_instalacion" || srv["plan_id"] != float64(300) || srv["cliente_id"] != float64(*d.Alta.ClienteArgosID) ||
		srv["router_id"] != float64(51) {
		t.Fatalf("POST /servicios: %+v", srv)
	}
	d = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: p.ID}))(t)
	if d.Alta.Estado != comercial.AltaCompleta || d.Alta.ServicioArgosID == nil || d.Alta.OrdenID == nil || d.Alta.OrdenNumero == nil {
		t.Fatalf("alta completa: %+v", d.Alta)
	}
	o := deber(e.cmp.VerOrden.Ejecutar(sistema(t), campo.EntradaVerOrden{ID: *d.Alta.OrdenID}))(t)
	if o.Origen != "sistema" || o.ClienteID == nil || *o.ClienteID != *d.Alta.ClienteArgosID || o.ServicioID == nil ||
		*o.ServicioID != *d.Alta.ServicioArgosID || o.EmpresaID == nil || *o.EmpresaID != 1 {
		t.Fatalf("orden con Argos: %+v", o)
	}

	// Sin duplicar: eventos repetidos y el barrido no crean otra orden ni otra alta en Argos.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"UPDATE integracion.outbox SET publicado_en = NULL WHERE evento = 'integracion.pendiente_hecho'"); err != nil {
		t.Fatal(err)
	}
	e.procesar()
	if _, err := e.com.BarrerAltas(sistema(t)); err != nil {
		t.Fatal(err)
	}
	if n := contar(t, "SELECT count(*) FROM campo.ordenes WHERE prospecto_id = $1", p.ID); n != 1 {
		t.Fatalf("órdenes: %d", n)
	}
	if e.falso.altasDistintas() != 2 {
		t.Fatalf("altas distintas en Argos: %d", e.falso.altasDistintas())
	}
	if !contiene(eventosDe(t, p.ID), "prospecto.ganado", "prospecto.alta_actualizada") {
		t.Fatalf("eventos: %v", eventosDe(t, p.ID))
	}

	// Requiere obra: a 600 m. Ganar sin confirmarObra → 422; con la confirmación, ganado.
	lejos := 14.6000 + 600.0/111_195
	p2 := deber(e.com.CrearProspecto.Ejecutar(ctx, comercial.EntradaCrearProspecto{Nombre: "Obra", Direccion: "Lejos", Lat: &lejos, Lon: ptr(-90.5)}))(t)
	d2 := hastaContrato(t, e.com, ctx, p2.ID, lineas, "")
	if d2.Factibilidad.Resultado != comercial.FactibleRequiereObra || !d2.Acciones.ConfirmaObra {
		t.Fatalf("requiere obra: %+v %+v", d2.Factibilidad, d2.Acciones)
	}
	_, err = e.com.Ganar.Ejecutar(ctx, comercial.EntradaGanar{ID: p2.ID, Version: d2.Version})
	problema(t, err, 422, "validacion")
	g2 := deber(e.com.Ganar.Ejecutar(ctx, comercial.EntradaGanar{ID: p2.ID, Version: d2.Version, ConfirmarObra: true}))(t)
	if !g2.ObraConfirmada {
		t.Fatal("obra confirmada")
	}

	// Coordenadas fuera de umbral (no viable) y umbrales por empresa: a 2 km no es viable; con
	// obra hasta 3 km, requiere obra.
	muyLejos := 14.6000 + 2000.0/111_195
	p3 := deber(e.com.CrearProspecto.Ejecutar(ctx, comercial.EntradaCrearProspecto{Nombre: "Muy lejos", Direccion: "Lejos", Lat: &muyLejos, Lon: ptr(-90.5)}))(t)
	f3 := deber(e.com.EvaluarFactibilidad(ctx, comercial.EntradaFactibilidad{ProspectoID: p3.ID, Version: p3.Version}))(t)
	if f3.Resultado != comercial.FactibleNoViable {
		t.Fatalf("a 2 km: %+v", f3)
	}
	sup := usuarioCon(t, rol("supervisor"))
	asignarEmpresa(t, sup, 1)
	_, err = e.com.FijarUmbrales.Ejecutar(ctx, comercial.EntradaUmbrales{ViableM: 300, ObraM: 3000})
	problema(t, err, 403, "prohibido")
	u := deber(e.com.FijarUmbrales.Ejecutar(enEmpresa(t, sup, 1, "supervisor"), comercial.EntradaUmbrales{ViableM: 300, ObraM: 3000}))(t)
	if u.PorDefecto || u.ObraM != 3000 {
		t.Fatalf("umbrales: %+v", u)
	}
	_, err = e.com.FijarUmbrales.Ejecutar(enEmpresa(t, sup, 1, "supervisor"), comercial.EntradaUmbrales{Version: 0, ViableM: 300, ObraM: 2000})
	problema(t, err, 409, "conflicto_version")
	p3d := deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: p3.ID}))(t)
	f3 = deber(e.com.EvaluarFactibilidad(ctx, comercial.EntradaFactibilidad{ProspectoID: p3.ID, Version: p3d.Version}))(t)
	if f3.Resultado != comercial.FactibleRequiereObra || f3.ObraM == nil || *f3.ObraM != 3000 {
		t.Fatalf("con umbrales de la empresa: %+v", f3)
	}
	// La empresa 2 sigue con los por defecto.
	if u2 := deber(e.com.VerUmbrales.Ejecutar(enEmpresa(t, sup, 2, "supervisor"), struct{}{}))(t); !u2.PorDefecto || u2.ObraM != 1000 {
		t.Fatalf("umbrales de la empresa 2: %+v", u2)
	}

	// Cobertura para el mapa: los routers con coordenadas de la empresa.
	cob := deber(e.com.Cobertura.Ejecutar(ctx, struct{}{}))(t)
	if !cob.Disponible || len(cob.Routers) != 1 || cob.Routers[0].ID != 51 {
		t.Fatalf("cobertura: %+v", cob)
	}

	// Conector apagado: factibilidad indeterminada (conector_apagado); planes no disponibles.
	e.apagar()
	p3d = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: p3.ID}))(t)
	f3 = deber(e.com.EvaluarFactibilidad(ctx, comercial.EntradaFactibilidad{ProspectoID: p3.ID, Version: p3d.Version}))(t)
	if f3.Resultado != comercial.FactibleIndeterminado || *f3.Motivo != comercial.MotivoConectorApagado {
		t.Fatalf("conector apagado: %+v", f3)
	}
	if pl := deber(e.com.Planes(ctx))(t); pl.Disponible || pl.Motivo == nil {
		t.Fatalf("planes con el conector apagado: %+v", pl)
	}
	if cob := deber(e.com.Cobertura.Ejecutar(ctx, struct{}{}))(t); cob.Disponible || *cob.Motivo != comercial.MotivoConectorApagado {
		t.Fatalf("cobertura apagada: %+v", cob)
	}
	e.encender()
}

// Revisiones de la cotización, vencimiento, contrato anulado y perder/reabrir.
func TestCotizacionesYContrato(t *testing.T) {
	ahora := time.Now().UTC()
	var mu sync.Mutex
	reloj := func() time.Time { mu.Lock(); defer mu.Unlock(); return ahora }
	e := nuevoEntorno(t, reloj)
	v := usuarioCon(t, rol("ventas"))
	ctx := como(t, v, "ventas")
	p := deber(e.com.CrearProspecto.Ejecutar(ctx, comercial.EntradaCrearProspecto{Nombre: "Revisiones", Direccion: "Zona 4"}))(t)
	d := hastaContratoParcial(t, e.com, ctx, p.ID)
	c1 := deber(e.com.CrearCotizacion(ctx, comercial.EntradaCrearCotizacion{ProspectoID: p.ID, Version: d.Version,
		Lineas: []comercial.EntradaLinea{lineaPlanLocal()}, VigenteHasta: ptr(ahora.Add(48 * time.Hour))}))(t)
	if c1.Revision != 1 || c1.Estado != "vigente" {
		t.Fatalf("c1: %+v", c1)
	}
	// Vencida: no se acepta.
	mu.Lock()
	ahora = ahora.Add(72 * time.Hour)
	mu.Unlock()
	_, err := e.com.AceptarCotizacion.Ejecutar(ctx, comercial.EntradaAceptarCotizacion{ID: c1.ID, Version: c1.Version})
	problema(t, err, 422, "cotizacion_vencida")
	// Revisión 2 reemplaza a la 1 sin borrarla; se acepta y se contrata.
	d = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: p.ID}))(t)
	c2 := deber(e.com.CrearCotizacion(ctx, comercial.EntradaCrearCotizacion{ProspectoID: p.ID, Version: d.Version,
		Lineas: []comercial.EntradaLinea{lineaPlanLocal(), {Tipo: "equipo", Descripcion: ptr("Router wifi"), PrecioUnitario: ptr("15"), Recurrente: ptr(true)}}}))(t)
	if c2.Revision != 2 || c2.TotalMensual != "214.99" {
		t.Fatalf("c2: %+v", c2)
	}
	c1 = deber(e.com.VerCotizacion.Ejecutar(ctx, comercial.EntradaVerCotizacion{ID: c1.ID}))(t)
	if c1.Estado != "reemplazada" || c1.ReemplazadaPor == nil || *c1.ReemplazadaPor != c2.ID {
		t.Fatalf("c1 reemplazada: %+v", c1)
	}
	_, err = e.com.AceptarCotizacion.Ejecutar(ctx, comercial.EntradaAceptarCotizacion{ID: c1.ID, Version: c1.Version})
	problema(t, err, 422, "transicion_invalida")
	// Contrato sin cotización aceptada: 422.
	d = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: p.ID}))(t)
	_, err = e.com.CrearContrato(ctx, comercial.EntradaCrearContrato{ProspectoID: p.ID, Version: d.Version})
	problema(t, err, 422, "cotizacion_no_aceptada")
	c2 = deber(e.com.AceptarCotizacion.Ejecutar(ctx, comercial.EntradaAceptarCotizacion{ID: c2.ID, Version: c2.Version}))(t)
	d = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: p.ID}))(t)
	ctr := deber(e.com.CrearContrato(ctx, comercial.EntradaCrearContrato{ProspectoID: p.ID, Version: d.Version}))(t)
	// Ganar con el contrato en borrador: 422.
	d = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: p.ID}))(t)
	_, err = e.com.Ganar.Ejecutar(ctx, comercial.EntradaGanar{ID: p.ID, Version: d.Version})
	problema(t, err, 422, "contrato_no_firmado")
	// Una firma ilegible: 422; con versión vieja: 409.
	_, err = e.com.FirmarContrato(ctx, comercial.EntradaFirmar{ContratoID: ctr.ID, Version: ctr.Version, FirmanteNombre: "Ana", SVG: "<svg><circle r='1'/></svg>"})
	problema(t, err, 422, "validacion")
	_, err = e.com.FirmarContrato(ctx, comercial.EntradaFirmar{ContratoID: ctr.ID, Version: ctr.Version + 1, FirmanteNombre: "Ana", SVG: firmaSVG})
	problema(t, err, 409, "conflicto_version")
	// Revisión 3 con el contrato en borrador: lo anula y vuelve a cotizado.
	c3 := deber(e.com.CrearCotizacion(ctx, comercial.EntradaCrearCotizacion{ProspectoID: p.ID, Version: d.Version, Lineas: []comercial.EntradaLinea{lineaPlanLocal()}}))(t)
	ctr = deber(e.com.VerContrato.Ejecutar(ctx, comercial.EntradaVerContrato{ID: ctr.ID}))(t)
	d = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: p.ID}))(t)
	if ctr.Estado != "anulado" || d.Estado != comercial.EstadoCotizado || d.Cotizacion.ID != c3.ID || d.Contrato != nil {
		t.Fatalf("revisión con contrato en borrador: %+v %+v", ctr, d.Prospecto)
	}
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE prospecto_id = $1", p.ID); n != 3 {
		t.Fatalf("ninguna versión se borra: %d", n)
	}
	// Perder (con motivo) y reabrir.
	_, err = e.com.Perder.Ejecutar(ctx, comercial.EntradaPerder{ID: p.ID, Version: d.Version, Motivo: " "})
	problema(t, err, 422, "validacion")
	pe := deber(e.com.Perder.Ejecutar(ctx, comercial.EntradaPerder{ID: p.ID, Version: d.Version, Motivo: "Eligió a la competencia"}))(t)
	if pe.Estado != comercial.EstadoPerdido || pe.MotivoPerdida == nil {
		t.Fatalf("perdido: %+v", pe)
	}
	re := deber(e.com.Reabrir.Ejecutar(ctx, comercial.EntradaTransicion{ID: p.ID, Version: pe.Version}))(t)
	if re.Estado != comercial.EstadoContactado || re.MotivoPerdida != nil {
		t.Fatalf("reabierto: %+v", re)
	}
	if !contiene(eventosDe(t, p.ID), "prospecto.perdido", "cotizacion.creada", "contrato.anulado") {
		t.Fatalf("eventos: %v", eventosDe(t, p.ID))
	}
	// El PDF de una cotización se vuelve a generar igual (idempotente).
	r := deber(e.com.GenerarPDFCotizacion(ctx, comercial.EntradaVerCotizacion{ID: c3.ID}))(t)
	if r.PDF == nil || r.PDF.Estado != "completo" {
		t.Fatalf("pdf: %+v", r.PDF)
	}
}

// Permisos, alcance por zona (falla cerrado) y multiempresa.
func TestPermisosAlcanceYEmpresa(t *testing.T) {
	e := nuevoEntorno(t, nil)
	zonaA, zonaB := zonaLocal(t, "A"), zonaLocal(t, "B")
	admin := usuarioCon(t, rol("admin"))
	ca := como(t, admin, "admin")
	vA := usuarioCon(t, identidad.AsignacionRol{Rol: "ventas", ZonaID: &zonaA})
	ctxA := conZona(como(t, vA, "ventas"), "ventas", zonaA)

	enB := deber(e.com.CrearProspecto.Ejecutar(ca, comercial.EntradaCrearProspecto{Nombre: "En zona B", Direccion: "B", ZonaID: &zonaB}))(t)
	sinZona := deber(e.com.CrearProspecto.Ejecutar(ca, comercial.EntradaCrearProspecto{Nombre: "Sin zona", Direccion: "?"}))(t)

	// Con alcance por zona: crear exige una zona propia.
	_, err := e.com.CrearProspecto.Ejecutar(ctxA, comercial.EntradaCrearProspecto{Nombre: "x", Direccion: "x"})
	problema(t, err, 403, "fuera_de_alcance")
	_, err = e.com.CrearProspecto.Ejecutar(ctxA, comercial.EntradaCrearProspecto{Nombre: "x", Direccion: "x", ZonaID: &zonaB})
	problema(t, err, 403, "fuera_de_alcance")
	enA := deber(e.com.CrearProspecto.Ejecutar(ctxA, comercial.EntradaCrearProspecto{Nombre: "En zona A", Direccion: "A", ZonaID: &zonaA}))(t)

	// Ve los de su zona; los de otra zona y los sin zona son 404 (no 403) y no salen en la lista.
	for _, id := range []int64{enB.ID, sinZona.ID} {
		_, err := e.com.VerProspecto.Ejecutar(ctxA, comercial.EntradaVerProspecto{ID: id})
		problema(t, err, 404, "no_encontrado")
		_, err = e.com.Contactar.Ejecutar(ctxA, comercial.EntradaTransicion{ID: id, Version: 1})
		problema(t, err, 404, "no_encontrado")
	}
	lista := deber(e.com.ListarProspectos.Ejecutar(ctxA, comercial.EntradaListarProspectos{}))(t)
	for _, x := range lista.Data {
		if x.ZonaID == nil || *x.ZonaID != zonaA {
			t.Fatalf("fuera de su zona: %+v", x)
		}
	}
	// Asignar un vendedor que no ve la zona: 422; al de la zona A, sí. El vendedor ve los suyos.
	_, err = e.com.AsignarProspecto.Ejecutar(ca, comercial.EntradaAsignarProspecto{ID: enB.ID, Version: enB.Version, VendedorID: &vA})
	problema(t, err, 422, "validacion")
	a := deber(e.com.AsignarProspecto.Ejecutar(ca, comercial.EntradaAsignarProspecto{ID: enA.ID, Version: enA.Version, VendedorID: &vA}))(t)
	_, err = e.com.AsignarProspecto.Ejecutar(ca, comercial.EntradaAsignarProspecto{ID: enA.ID, Version: a.Version, VendedorID: &vA})
	problema(t, err, 422, "sin_cambios")
	// Un vendedor con alcance global, asignado a un prospecto sin zona, lo ve por ser suyo aunque
	// después le limiten el alcance a otra zona.
	vG := usuarioCon(t, rol("ventas"))
	sz := deber(e.com.AsignarProspecto.Ejecutar(ca, comercial.EntradaAsignarProspecto{ID: sinZona.ID, Version: sinZona.Version, VendedorID: &vG}))(t)
	deber(e.com.VerProspecto.Ejecutar(conZona(como(t, vG, "ventas"), "ventas", zonaB), comercial.EntradaVerProspecto{ID: sz.ID}))(t)

	// lectura: ve, no actúa (403).
	lec := usuarioCon(t, rol("lectura"))
	cl := como(t, lec, "lectura")
	deber(e.com.VerProspecto.Ejecutar(cl, comercial.EntradaVerProspecto{ID: enA.ID}))(t)
	_, err = e.com.CrearProspecto.Ejecutar(cl, comercial.EntradaCrearProspecto{Nombre: "x", Direccion: "x"})
	problema(t, err, 403, "prohibido")
	_, err = e.com.Contactar.Ejecutar(cl, comercial.EntradaTransicion{ID: enA.ID, Version: a.Version})
	problema(t, err, 403, "prohibido")
	// Un técnico no tiene prospectos.ver.
	tec := usuarioCon(t, rol("tecnico"))
	_, err = e.com.ListarProspectos.Ejecutar(como(t, tec, "tecnico"), comercial.EntradaListarProspectos{})
	problema(t, err, 403, "prohibido")

	// Idempotencia: la misma clave devuelve el mismo prospecto; 409 con versión vieja.
	p1 := deber(e.com.CrearProspecto.Ejecutar(ca, comercial.EntradaCrearProspecto{Nombre: "Idem", Direccion: "x", ClaveIdempotencia: "clave-1"}))(t)
	p2 := deber(e.com.CrearProspecto.Ejecutar(ca, comercial.EntradaCrearProspecto{Nombre: "Idem", Direccion: "x", ClaveIdempotencia: "clave-1"}))(t)
	if p1.ID != p2.ID {
		t.Fatalf("idempotencia: %d %d", p1.ID, p2.ID)
	}
	ed := deber(e.com.EditarProspecto.Ejecutar(ca, comercial.EntradaEditarProspecto{ID: p1.ID, Version: p1.Version, Telefono: ptr("5555")}))(t)
	_, err = e.com.EditarProspecto.Ejecutar(ca, comercial.EntradaEditarProspecto{ID: p1.ID, Version: p1.Version, Telefono: ptr("6666")})
	problema(t, err, 409, "conflicto_version")
	_, err = e.com.EditarProspecto.Ejecutar(ca, comercial.EntradaEditarProspecto{ID: p1.ID, Version: ed.Version, Telefono: ptr("5555")})
	problema(t, err, 422, "sin_cambios")
	a1 := deber(e.com.RegistrarActividad.Ejecutar(ca, comercial.EntradaActividad{ProspectoID: p1.ID, Tipo: "nota", Descripcion: "n", ClaveIdempotencia: "act-1"}))(t)
	a2 := deber(e.com.RegistrarActividad.Ejecutar(ca, comercial.EntradaActividad{ProspectoID: p1.ID, Tipo: "nota", Descripcion: "n", ClaveIdempotencia: "act-1"}))(t)
	if a1.ID != a2.ID {
		t.Fatal("actividad repetida")
	}
	acts := deber(e.com.ListarActividades.Ejecutar(ca, comercial.EntradaListarActividades{ID: p1.ID}))(t)
	if len(acts.Data) != 1 {
		t.Fatalf("actividades: %+v", acts)
	}
	// El origen argos solo lo usa la integración.
	_, err = e.com.CrearProspecto.Ejecutar(ca, comercial.EntradaCrearProspecto{Nombre: "x", Direccion: "x", Origen: "argos"})
	problema(t, err, 422, "origen_no_permitido")
}

// Multiempresa: con empresas registradas, cada empresa ve solo lo suyo.
func TestMultiempresa(t *testing.T) {
	limpiarConector(t)
	e := nuevoEntorno(t, nil)
	e.conectar()
	v := usuarioCon(t, rol("ventas"))
	asignarEmpresa(t, v, 1)
	asignarEmpresa(t, v, 2)
	c1, c2 := enEmpresa(t, v, 1, "ventas"), enEmpresa(t, v, 2, "ventas")
	p := deber(e.com.CrearProspecto.Ejecutar(c1, comercial.EntradaCrearProspecto{Nombre: "De la 1", Direccion: "x", ZonaID: ptr(int64(7001))}))(t)
	// Zona de la otra empresa: 422; empresa distinta a la activa: 422.
	_, err := e.com.CrearProspecto.Ejecutar(c2, comercial.EntradaCrearProspecto{Nombre: "x", Direccion: "x", ZonaID: ptr(int64(7001))})
	problema(t, err, 422, "validacion")
	_, err = e.com.CrearProspecto.Ejecutar(c2, comercial.EntradaCrearProspecto{Nombre: "x", Direccion: "x", EmpresaID: ptr(int64(1))})
	problema(t, err, 422, "empresa_no_coincide")
	_, err = e.com.VerProspecto.Ejecutar(c2, comercial.EntradaVerProspecto{ID: p.ID})
	problema(t, err, 404, "no_encontrado")
	if l := deber(e.com.ListarProspectos.Ejecutar(c2, comercial.EntradaListarProspectos{}))(t); len(l.Data) != 0 {
		t.Fatalf("la empresa 2 no ve los de la 1: %+v", l.Data)
	}
	// Un vendedor que no opera en la empresa 1 no se asigna.
	otro := usuarioCon(t, rol("ventas"))
	asignarEmpresa(t, otro, 2)
	_, err = e.com.AsignarProspecto.Ejecutar(c1, comercial.EntradaAsignarProspecto{ID: p.ID, Version: p.Version, VendedorID: &otro})
	problema(t, err, 422, "vendedor_fuera_de_empresa")
	// Datos guardados sin empresa (antes del conector) no se ven desde una empresa ni se ganan.
	if n := contar(t, "SELECT count(*) FROM comercial.prospectos WHERE empresa_id IS NULL"); n == 0 {
		t.Skip("sin datos sin empresa")
	}
	l := deber(e.com.ListarProspectos.Ejecutar(c1, comercial.EntradaListarProspectos{ParamsPagina: nucleo.ParamsPagina{Limit: 200}}))(t)
	for _, x := range l.Data {
		if x.EmpresaID == nil || *x.EmpresaID != 1 {
			t.Fatalf("empresa 1 ve un prospecto ajeno: %+v", x)
		}
	}
}
