package campo_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Paso «Confirmar titular y plan» (contrato argosisp.md 0.10.0 §3.4, B18 y B19) de punta a punta:
// lote del teléfono → casos de uso → depósito de tránsito cifrado → cola de pendientes → Argos falso.

// Datos sensibles de prueba: nunca deben aparecer fuera del depósito de tránsito cifrado.
const (
	dpiPrueba      = "1234567890101" // CUI válido (verificador 9, departamento 01, municipio 01)
	telefonoPrueba = "5512-3344"
	correoPrueba   = "ana.titular@correo.gt"
	nitPrueba      = "12345679" // NIT válido (verificador 9)
)

// llamadaArgos es una escritura que recibió el Argos falso.
type llamadaArgos struct {
	Ruta, Clave, Actor string
	// Partes de texto del multipart (A41) o las claves del JSON (A43, A40…).
	Partes map[string]string
	// Archivos del multipart: campo → tamaño y tipo.
	Archivos map[string]string
	// JSON: el cuerpo JSON o la parte «datos» de A39.
	JSON map[string]any
}

// argosTitular: Argos falso con el canje y las rutas de §3.4. rechazos: sufijo de ruta → códigos
// (422) que responde en orden antes de aceptar.
type argosTitular struct {
	srv      *httptest.Server
	mu       sync.Mutex
	llamadas []llamadaArgos
	rechazos map[string][]string
}

func nuevoArgosTitular(t *testing.T) *argosTitular {
	a := &argosTitular{rechazos: map[string][]string{}}
	json200 := func(w http.ResponseWriter, status int, v string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(v))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/integraciones/iris/conexion/canjear", func(w http.ResponseWriter, r *http.Request) {
		json200(w, 200, `{"token":"atk_prueba_titular","webhook":{"url":"http://`+r.Host+`/api/v1/integraciones/iris/webhook","secreto":"secreto-de-argos-para-iris"},"empresas":[1]}`)
	})
	mux.HandleFunc("POST /api/v1/", func(w http.ResponseWriter, r *http.Request) {
		ruta := strings.TrimPrefix(r.URL.Path, "/api/v1")
		l := llamadaArgos{Ruta: ruta, Clave: r.Header.Get("Idempotency-Key"), Actor: r.Header.Get("X-Iris-Actor"),
			Partes: map[string]string{}, Archivos: map[string]string{}}
		tipo, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if tipo == "multipart/form-data" {
			if err := r.ParseMultipartForm(8 << 20); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			for k, v := range r.MultipartForm.Value {
				l.Partes[k] = v[0]
			}
			for k, fs := range r.MultipartForm.File {
				l.Archivos[k] = fs[0].Header.Get("Content-Type") + ":" + strconv.FormatInt(fs[0].Size, 10)
			}
			if d, ok := l.Partes["datos"]; ok {
				_ = json.Unmarshal([]byte(d), &l.JSON)
			}
		} else {
			_ = json.NewDecoder(r.Body).Decode(&l.JSON)
		}
		a.mu.Lock()
		a.llamadas = append(a.llamadas, l)
		var rechazo string
		for sufijo, codigos := range a.rechazos {
			if strings.HasSuffix(ruta, sufijo) && len(codigos) > 0 {
				rechazo, a.rechazos[sufijo] = codigos[0], codigos[1:]
			}
		}
		a.mu.Unlock()
		if rechazo != "" {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"type":"urn:argos:` + rechazo + `","title":"Rechazado","status":422,"detail":"El documento no cumple la regla"}`))
			return
		}
		switch {
		case strings.HasSuffix(ruta, "/identidad"):
			json200(w, 201, `{"identidad_id":501,"version":1}`)
		case strings.HasSuffix(ruta, "/cambiar-titular"):
			json200(w, 200, `{"servicio_id":9901,"titular":{"cliente_id":777,"existente":false},"posible_duplicado":false,"identidad_id":502,"de_cliente_id":990,"de_cliente_eliminado":true}`)
		default:
			json200(w, 201, `{"ok":true}`)
		}
	})
	a.srv = httptest.NewServer(mux)
	t.Cleanup(a.srv.Close)
	return a
}

func (a *argosTitular) rechazar(sufijo string, codigos ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rechazos[sufijo] = append(a.rechazos[sufijo], codigos...)
}

func (a *argosTitular) recibidas() []llamadaArgos {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]llamadaArgos(nil), a.llamadas...)
}

func (a *argosTitular) porClave(clave string) *llamadaArgos {
	var ultima *llamadaArgos
	for _, l := range a.recibidas() {
		if l.Clave == clave {
			ultima = &l
		}
	}
	return ultima
}

type entornoTitular struct {
	t     *testing.T
	reloj *reloj
	am    *argos.Modulo
	falso *argosTitular
	m     *campo.Modulo
	pend  *pendientes.Modulo
	bus   *integracion.Bus
	sup   int64
	cs    context.Context // supervisor en la empresa 1
}

// nuevoEntornoTitular: conector Argos conectado (empresa 1), pendientes con el mismo cifrador y
// reloj, y campo con el paso del titular. Al terminar borra lo que deja (las demás pruebas del
// paquete corren sin conector).
func nuevoEntornoTitular(t *testing.T) *entornoTitular {
	t.Helper()
	pool := pruebas.Pool(t)
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	cif, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	r := &reloj{t: time.Now().UTC().Truncate(time.Microsecond)}
	e := &entornoTitular{t: t, reloj: r, falso: nuevoArgosTitular(t), bus: integracion.NuevoBus()}
	e.am = argos.Nuevo(argos.Config{
		Cifrador: cif, BaseURL: "https://iris.prueba", PermitirHTTP: true, Timeout: 2 * time.Second, Origenes: []string{e.falso.srv.URL},
		RedesPermitidas: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
	})
	e.am.Nucleo().FijarTokens(pruebas.TokensFalsos{})
	e.pend = pendientes.Nuevo(pendientes.Config{Argos: e.am, Cifrador: cif, Reloj: r.ahora})
	e.pend.Suscribir(e.bus)
	e.m = campo.Nuevo(campo.Config{Reloj: r.ahora, Adjuntos: campo.SinAdjuntosObligatorios{},
		Titular: campo.ConfigTitular{Cifrador: cif, Conector: campo.ConectorDeArgos(e.am)}})
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM campo.orden_titular", "DELETE FROM integracion.pendientes", "DELETE FROM integracion.datos_transito",
			"DELETE FROM integracion.conectores", "DELETE FROM nucleo.usuarios_empresas", "DELETE FROM nucleo.empresas"} {
			if _, err := pool.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
	c, err := e.am.Conectar.Ejecutar(sistema(t), argos.EntradaConectar{Enlace: e.falso.srv.URL + "/conectar#codigo-de-prueba-titular"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.am.ActivarConector.Ejecutar(sistema(t), argos.EntradaActivarConector{ID: c.ID, Version: c.Version}); err != nil {
		t.Fatal(err)
	}
	e.sup = usuarioCon(t, identidad.AsignacionRol{Rol: "supervisor"})
	asignarEmpresa(t, e.sup, 1)
	e.cs = pruebas.ContextoUsuarioEnEmpresa(pool, e.sup, 1, permisosDe("supervisor")...)
	return e
}

// tecnicoEnEmpresa: un técnico de la empresa 1 y su contexto.
func (e *entornoTitular) tecnicoEnEmpresa() (int64, context.Context) {
	tec := tecnico(e.t)
	asignarEmpresa(e.t, tec, 1)
	return tec, pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(e.t), tec, 1, permisosDe("tecnico")...)
}

// instalacion: orden de instalación de una venta (prospecto) programada al técnico, en sitio.
func (e *entornoTitular) instalacion(tec int64, ct context.Context, conProspecto bool) campo.Orden {
	e.t.Helper()
	in := campo.EntradaCrearOrden{Tipo: "instalacion", Direccion: ptr("Zona 10"), ClienteID: ptr(int64(990)), ServicioID: ptr(int64(9901))}
	if conProspecto {
		var p int64
		if err := pruebas.Pool(e.t).QueryRow(context.Background(),
			"INSERT INTO comercial.prospectos (empresa_id, origen, nombre, direccion) VALUES (1, 'panel', 'Prospecto titular', 'Zona 10') RETURNING id").Scan(&p); err != nil {
			e.t.Fatal(err)
		}
		in.ProspectoID = &p
	}
	o := deber(e.m.CrearOrden.Ejecutar(e.cs, in))(e.t)
	o = deber(e.m.ProgramarOrden.Ejecutar(e.cs, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &tec,
		BodegaID: ptr(int64(9)), Inicio: hoyEnGuatemala(10)}))(e.t)
	e.lote(ct, op(campo.OpLlegar, o.ID, nil))
	return o
}

func (e *entornoTitular) lote(ctx context.Context, ops ...campo.OperacionLote) campo.RespuestaLote {
	e.t.Helper()
	r, err := e.m.Lote(ctx, campo.EntradaLote{DispositivoID: "tel-titular-" + pruebas.Unico(), Plataforma: "pwa", Operaciones: ops})
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *entornoTitular) relay() {
	e.t.Helper()
	r := &integracion.Relay{BD: pruebas.Pool(e.t), Bus: e.bus, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := r.Ejecutar(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func (e *entornoTitular) procesar() {
	e.t.Helper()
	for range 4 {
		if _, err := e.pend.Procesar(sistema(e.t)); err != nil {
			e.t.Fatal(err)
		}
	}
}

type filaPendiente struct {
	ID        int64
	Estado    string
	DependeDe []int64
	Actor     *string
	Codigo    *string
}

func (e *entornoTitular) pendiente(clave string) filaPendiente {
	e.t.Helper()
	var f filaPendiente
	if err := pruebas.Pool(e.t).QueryRow(context.Background(),
		"SELECT id, estado, depende_de, actor, codigo_error FROM integracion.pendientes WHERE idempotency_key = $1", clave).
		Scan(&f.ID, &f.Estado, &f.DependeDe, &f.Actor, &f.Codigo); err != nil {
		e.t.Fatalf("pendiente %s: %v", clave, err)
	}
	return f
}

// jpegFalso: unos bytes que empiezan como JPEG.
func jpegFalso(n int) string {
	b := make([]byte, n)
	copy(b, []byte{0xFF, 0xD8, 0xFF, 0xE0})
	return base64.StdEncoding.EncodeToString(b)
}

func datosIdentidad() map[string]any {
	return map[string]any{"documentoTipo": "dpi", "documentoNumero": dpiPrueba, "nombre": "Ana  María Pérez", "mayorDeEdad": true,
		"coincide": true, "frente": map[string]any{"mime": "image/jpeg", "base64": jpegFalso(2048)},
		"reverso": map[string]any{"mime": "image/jpeg", "base64": jpegFalso(1024)}}
}

func datosCambioTitular() map[string]any {
	return map[string]any{
		"titular": map[string]any{"nombre": "Ana Pérez", "documentoTipo": "dpi", "documentoNumero": dpiPrueba, "mayorDeEdad": true,
			"telefono": "+502 " + telefonoPrueba, "whatsapp": true, "email": correoPrueba, "tipoDocumentoFiscal": "nit", "nitCui": nitPrueba, "relacion": "familiar"},
		"coincide": true, "frente": map[string]any{"mime": "image/jpeg", "base64": jpegFalso(2048)},
		"motivoCodigo": "menor_de_edad", "motivo": "El servicio lo pidió un menor; queda de titular su madre",
	}
}

// sinSensibles: el documento, el teléfono, el correo, el NIT y las fotos no están en ninguna tabla de
// Iris fuera del depósito cifrado (auditoría, eventos, pendientes, operaciones del lote, la orden).
func sinSensibles(t *testing.T) {
	t.Helper()
	foto := jpegFalso(2048)[:40]
	for _, consulta := range []string{
		"SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE $1 OR coalesce(resultado::text, '') LIKE $1 OR coalesce(error::text, '') LIKE $1",
		"SELECT count(*) FROM auditoria.cambios WHERE coalesce(antes::text, '') LIKE $1 OR coalesce(despues::text, '') LIKE $1",
		"SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE $1",
		"SELECT count(*) FROM integracion.pendientes WHERE payload::text LIKE $1 OR coalesce(plantilla::text, '') LIKE $1 OR coalesce(respuesta::text, '') LIKE $1 OR coalesce(ultimo_error, '') LIKE $1",
		"SELECT count(*) FROM campo.operaciones_sync WHERE coalesce(resultado::text, '') LIKE $1",
		"SELECT count(*) FROM campo.orden_titular WHERE row_to_json(orden_titular)::text LIKE $1",
		"SELECT count(*) FROM integracion.datos_transito WHERE position(convert_to($2, 'UTF8') in coalesce(cifrado, ''::bytea)) > 0 AND $1 <> ''",
	} {
		for _, valor := range []string{dpiPrueba, telefonoPrueba, "55123344", correoPrueba, nitPrueba, foto} {
			args := []any{"%" + valor + "%"}
			if strings.Contains(consulta, "$2") {
				args = append(args, valor)
			}
			if n := contar(t, consulta, args...); n != 0 {
				t.Fatalf("dato sensible fuera del depósito (%s): %d filas en %q", valor[:4], n, consulta[:60])
			}
		}
	}
}

func TestTitularSinSenalHastaArgos(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	orden := strconv.FormatInt(o.ID, 10)

	// El teléfono manda el paso completo sin señal: identidad (A41), datos (A43) y plan (A40).
	ident := op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())
	datos := op(campo.OpCompletarDatos, o.ID, map[string]any{
		"cliente":  map[string]any{"nombre": "Ana María Pérez López", "telefono": telefonoPrueba, "whatsapp": true, "email": correoPrueba, "tipoDocumentoFiscal": "nit", "nitCui": nitPrueba},
		"servicio": map[string]any{"direccion": "4a calle 5-20 zona 10", "referencia": "Portón negro", "lat": 14.6, "lon": -90.51, "precisionM": 8},
	})
	plan := op(campo.OpCambiarPlan, o.ID, map[string]any{"planId": 300, "motivo": "mas_velocidad"})
	r := e.lote(ct, ident, datos, plan, ident)
	esperar(t, r, "aplicada", "aplicada", "aplicada", "duplicada")

	// Pendientes con las claves del contrato, el actor y la cadena titular → datos → plan.
	pi, pd, pp := e.pendiente("iris:identidad:"+orden+":1"), e.pendiente("iris:datos:"+orden+":1"), e.pendiente("iris:plan:"+orden+":1")
	if len(pi.DependeDe) != 0 || len(pd.DependeDe) != 1 || pd.DependeDe[0] != pi.ID || len(pp.DependeDe) != 1 || pp.DependeDe[0] != pd.ID {
		t.Fatalf("dependencias: %+v %+v %+v", pi, pd, pp)
	}
	quiere := "usuario:" + strconv.FormatInt(tec, 10)
	for _, p := range []filaPendiente{pi, pd, pp} {
		if p.Actor == nil || *p.Actor != quiere {
			t.Fatalf("X-Iris-Actor del pendiente: %+v", p)
		}
	}
	if n := contar(t, "SELECT count(*) FROM integracion.datos_transito WHERE orden_id = $1 AND cifrado IS NOT NULL", o.ID); n != 2 {
		t.Fatalf("depósitos de tránsito (identidad y datos): %d", n)
	}
	// La auditoría guarda la entrada con lo sensible como ***.
	var doc, fotoAud string
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT entrada->>'documentoNumero', entrada->'frente'->>'base64' FROM auditoria.acciones WHERE accion = 'ordenes.registrar_identidad' AND entidad_id = $1",
		orden).Scan(&doc, &fotoAud); err != nil {
		t.Fatal(err)
	}
	if doc != "***" || fotoAud != "***" {
		t.Fatalf("auditoría sin enmascarar: %q %q", doc, fotoAud)
	}
	// Estado del paso: registrada, sin el número.
	est := deber(e.m.Titular.Ver.Ejecutar(ct, campo.EntradaVerOrden{ID: o.ID}))(t)
	if est.Identidad != "registrada" || est.QuienFirma == nil || *est.QuienFirma != "misma" || est.Nombre == nil || *est.Nombre != "Ana María Pérez" ||
		len(est.Pasos) != 3 || est.Pasos[0].Estado != "enviando" || est.PlanID == nil || *est.PlanID != 300 {
		t.Fatalf("estado del paso: %+v", est)
	}

	// Cierre: la activación espera a los tres pasos.
	cierre := trabajoDeInstalacion(o.ID)[2:]
	esperar(t, e.lote(ct, cierre...), repetir(len(cierre), "aplicada")...)
	e.relay()
	act := e.pendiente("iris:instalacion:" + orden)
	if len(act.DependeDe) != 3 || act.DependeDe[0] != pi.ID || act.DependeDe[2] != pp.ID {
		t.Fatalf("la activación depende de los pasos del titular: %+v", act)
	}

	e.procesar()
	// Llegan a Argos en orden, con X-Iris-Actor, y lo sensible solo en el cuerpo de la llamada.
	orden2 := []string{}
	for _, l := range e.falso.recibidas() {
		switch {
		case strings.HasSuffix(l.Ruta, "/identidad"), strings.HasSuffix(l.Ruta, "/completar-datos"),
			strings.HasSuffix(l.Ruta, "/cambiar-plan"), strings.HasSuffix(l.Ruta, "/activar-instalacion"):
			orden2 = append(orden2, l.Ruta[strings.LastIndex(l.Ruta, "/")+1:])
		}
	}
	if strings.Join(orden2, ",") != "identidad,completar-datos,cambiar-plan,activar-instalacion" {
		t.Fatalf("orden de las llamadas: %v", orden2)
	}
	li := e.falso.porClave("iris:identidad:" + orden + ":1")
	if li.Ruta != "/clientes/990/identidad" || li.Actor != quiere || li.Partes["documento_numero"] != dpiPrueba || li.Partes["documento_tipo"] != "dpi" ||
		li.Partes["nombre"] != "Ana María Pérez" || li.Partes["mayor_de_edad"] != "true" || li.Partes["coincide"] != "true" ||
		li.Partes["metodo"] != "foto_documento" || li.Partes["origen_tipo"] != "iris_orden" || li.Partes["origen_id"] != orden ||
		li.Archivos["frente"] != "image/jpeg:2048" || li.Archivos["reverso"] != "image/jpeg:1024" {
		t.Fatalf("A41 multipart: %+v", li)
	}
	ld := e.falso.porClave("iris:datos:" + orden + ":1")
	cli, _ := ld.JSON["cliente"].(map[string]any)
	srv, _ := ld.JSON["servicio"].(map[string]any)
	if ld.Ruta != "/servicios/9901/completar-datos" || ld.Actor != quiere || cli["telefono"] != "+502 "+telefonoPrueba || cli["email"] != correoPrueba ||
		cli["nit_cui"] != nitPrueba || cli["tipo_documento_fiscal"] != "nit" || cli["nombre"] != "Ana María Pérez López" ||
		ld.JSON["identidad_id"] != 501.0 || srv["coordenadas"] != "14.600000,-90.510000" || srv["precision_m"] != 8.0 {
		t.Fatalf("A43: %+v", ld)
	}
	lp := e.falso.porClave("iris:plan:" + orden + ":1")
	if lp.Ruta != "/servicios/9901/cambiar-plan" || lp.JSON["plan_id"] != 300.0 || lp.JSON["motivo"] != "mas_velocidad" || lp.Actor != quiere {
		t.Fatalf("A40: %+v", lp)
	}
	// 2xx: el depósito se borra.
	if n := contar(t, "SELECT count(*) FROM integracion.datos_transito WHERE orden_id = $1 AND cifrado IS NULL AND motivo_borrado = 'entregado'", o.ID); n != 2 {
		t.Fatalf("depósitos borrados al 2xx: %d", n)
	}
	sinSensibles(t)
	// El panel de pendientes no ve lo sensible (tampoco el cuerpo con marcadores ya resuelto).
	lista := deber(e.pend.Listar.Ejecutar(e.cs, pendientes.EntradaListar{Entidad: "orden", EntidadID: orden}))(t)
	for _, p := range lista.Data {
		crudo, _ := json.Marshal(p)
		if strings.Contains(string(crudo), dpiPrueba) || strings.Contains(string(crudo), correoPrueba) {
			t.Fatalf("el panel ve un dato sensible: %s", crudo)
		}
	}
}

func TestTitularRechazoYOtraToma(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	orden := strconv.FormatInt(o.ID, 10)
	e.falso.rechazar("/identidad", "validacion")

	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad()),
		op(campo.OpCompletarDatos, o.ID, map[string]any{"servicio": map[string]any{"referencia": "Casa verde"}})), "aplicada", "aplicada")
	e.procesar()
	p1, pd := e.pendiente("iris:identidad:"+orden+":1"), e.pendiente("iris:datos:"+orden+":1")
	if p1.Estado != pendientes.EstadoFallidoNegocio || p1.Codigo == nil || *p1.Codigo != "validacion" || pd.Estado != pendientes.EstadoBloqueado {
		t.Fatalf("A41 rechazado bloquea A43: %+v %+v", p1, pd)
	}
	est := deber(e.m.Titular.Ver.Ejecutar(ct, campo.EntradaVerOrden{ID: o.ID}))(t)
	if !est.Pendiente || est.EnRevision || est.Pasos[0].Estado != "rechazado" || *est.Pasos[0].Codigo != "validacion" || est.Pasos[1].Estado != "bloqueado" {
		t.Fatalf("estado tras el rechazo: %+v", est)
	}
	// Un cambio de plan nace bloqueado detrás del rechazo.
	esperar(t, e.lote(ct, op(campo.OpCambiarPlan, o.ID, map[string]any{"planId": 301, "motivo": "otro", "motivoTexto": "El cliente prefiere el plan básico"})), "aplicada")
	if pp := e.pendiente("iris:plan:" + orden + ":1"); pp.Estado != pendientes.EstadoBloqueado {
		t.Fatalf("plan detrás del rechazo: %+v", pp)
	}

	// Otra toma reemplaza a la rechazada: lo que la esperaba pasa a esperar a la nueva.
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())), "aplicada")
	p1, p2 := e.pendiente("iris:identidad:"+orden+":1"), e.pendiente("iris:identidad:"+orden+":2")
	pd = e.pendiente("iris:datos:" + orden + ":1")
	if p1.Estado != pendientes.EstadoResueltoManual || len(pd.DependeDe) != 1 || pd.DependeDe[0] != p2.ID {
		t.Fatalf("la toma 2 reemplaza a la 1: %+v %+v %+v", p1, p2, pd)
	}
	e.procesar()
	for _, k := range []string{"iris:identidad:" + orden + ":2", "iris:datos:" + orden + ":1", "iris:plan:" + orden + ":1"} {
		if p := e.pendiente(k); p.Estado != pendientes.EstadoHecho {
			t.Fatalf("%s: %+v", k, p)
		}
	}
	// El depósito de la toma rechazada sigue hasta los 7 días; entonces se purga.
	if n := contar(t, "SELECT count(*) FROM integracion.datos_transito WHERE orden_id = $1 AND cifrado IS NOT NULL", o.ID); n != 1 {
		t.Fatalf("queda el depósito de la toma rechazada: %d", n)
	}
	e.reloj.avanzar(8 * 24 * time.Hour)
	e.procesar()
	if n := contar(t, "SELECT count(*) FROM integracion.datos_transito WHERE orden_id = $1 AND cifrado IS NOT NULL", o.ID); n != 0 {
		t.Fatalf("a los 7 días se purga: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.datos_transito WHERE orden_id = $1 AND motivo_borrado = 'vencido'", o.ID); n != 1 {
		t.Fatalf("motivo vencido: %d", n)
	}
	sinSensibles(t)
}

func TestTitularFotoVencida(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, false)
	orden := strconv.FormatInt(o.ID, 10)
	// El depósito vence antes de entregarse: foto_vencida (de negocio, sin reintento) y no sale nada.
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())), "aplicada")
	e.reloj.avanzar(8 * 24 * time.Hour)
	e.procesar()
	if p := e.pendiente("iris:identidad:" + orden + ":1"); p.Estado != pendientes.EstadoFallidoNegocio || p.Codigo == nil || *p.Codigo != pendientes.CodigoFotoVencida {
		t.Fatalf("foto vencida: %+v", p)
	}
	if l := e.falso.porClave("iris:identidad:" + orden + ":1"); l != nil {
		t.Fatalf("no se manda nada sin el depósito: %+v", l)
	}
}

func TestTitularNoCoincideBloqueaLaActivacion(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	orden := strconv.FormatInt(o.ID, 10)
	e.falso.rechazar("/cambiar-titular", "titular_no_coincide")

	cierre := trabajoDeInstalacion(o.ID)[2:]
	ops := append([]campo.OperacionLote{op(campo.OpCambiarTitular, o.ID, datosCambioTitular())}, cierre...)
	esperar(t, e.lote(ct, ops...), repetir(len(ops), "aplicada")...)
	e.relay()
	e.procesar()
	pt, act := e.pendiente("iris:titular:"+orden+":1"), e.pendiente("iris:instalacion:"+orden)
	if pt.Estado != pendientes.EstadoFallidoNegocio || pt.Codigo == nil || *pt.Codigo != "titular_no_coincide" || act.Estado != pendientes.EstadoBloqueado {
		t.Fatalf("titular_no_coincide bloquea la activación: %+v %+v", pt, act)
	}
	if l := e.falso.porClave("iris:instalacion:" + orden); l != nil {
		t.Fatalf("nunca se activa con el titular equivocado: %+v", l)
	}
	// El cobro de la instalación (si lo hay) también espera; aquí el estado del paso.
	est := deber(e.m.Titular.Ver.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	if !est.EnRevision || !est.Pendiente || *est.QuienFirma != "otra" {
		t.Fatalf("en revisión: %+v", est)
	}
	// Validar la identidad desde el panel nunca cambia el titular.
	f := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	_, err := e.m.Titular.ValidarIdentidad.Ejecutar(e.cs, campo.EntradaValidarIdentidad{ID: o.ID, Version: f.Version, Motivo: "Revisé la foto con el técnico"})
	problema(t, err, 422, "titular_en_revision")
	// El cuerpo de A39 llevó los datos del titular nuevo (del depósito) y el técnico como actor.
	l := e.falso.porClave("iris:titular:" + orden + ":1")
	cli, _ := l.JSON["cliente"].(map[string]any)
	if l.Ruta != "/servicios/9901/cambiar-titular" || cli["documento_numero"] != dpiPrueba || cli["telefono"] != "+502 "+telefonoPrueba ||
		cli["nit_cui"] != nitPrueba || l.JSON["motivo_codigo"] != "menor_de_edad" || l.Archivos["frente"] != "image/jpeg:2048" ||
		l.Actor != "usuario:"+strconv.FormatInt(tec, 10) {
		t.Fatalf("A39: %+v", l)
	}
	sinSensibles(t)
}

func TestTitularCobroAlTitularFinal(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	orden := strconv.FormatInt(o.ID, 10)
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosCambioTitular())), "aplicada")
	// El cobro de la instalación lo pide la orden cerrada (cargo); va al titular que quedó (B21).
	if _, err := pruebas.Pool(t).Exec(context.Background(), "INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES (gen_random_uuid(), 'orden.cerrada', 1, $1)",
		`{"orden_id":"`+orden+`","tipo":"instalacion","resultado":"exitosa","cliente_id":990,"servicio_id":9901,"cargo":{"monto":"150.00","descripcion":"Instalación"}}`); err != nil {
		t.Fatal(err)
	}
	e.relay()
	cargo := e.pendiente("iris:cargo:" + orden)
	if len(cargo.DependeDe) != 1 {
		t.Fatalf("el cobro espera al titular: %+v", cargo)
	}
	e.procesar()
	l := e.falso.porClave("iris:cargo:" + orden)
	if l == nil || l.JSON["cliente_id"] != 777.0 {
		t.Fatalf("cobro al titular final: %+v", l)
	}
}

func TestTitularCierreExigeIdentidadYValidarDesdeElPanel(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()

	// 1. Instalación de una venta sin identidad: no cierra.
	o := e.instalacion(tec, ct, true)
	orden := strconv.FormatInt(o.ID, 10)
	cierre := trabajoDeInstalacion(o.ID)[2:]
	r := e.lote(ct, cierre...)
	if fin := r.Resultados[len(r.Resultados)-1]; fin.Error == nil || fin.Error.Codigo() != "identidad_requerida" {
		t.Fatalf("cierre sin identidad: %+v", fin)
	}
	// El técnico no puede validarla; el supervisor sí (sin identidad: solo sirve para activar).
	f := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	_, err := e.m.Titular.ValidarIdentidad.Ejecutar(ct, campo.EntradaValidarIdentidad{ID: o.ID, Version: f.Version, Motivo: "Lo revisé por videollamada"})
	problema(t, err, 403, "prohibido")
	_, err = e.m.Titular.ValidarIdentidad.Ejecutar(e.cs, campo.EntradaValidarIdentidad{ID: o.ID, Version: f.Version, Motivo: "corto"})
	problema(t, err, 422, "validacion")
	deber(e.m.Titular.ValidarIdentidad.Ejecutar(e.cs, campo.EntradaValidarIdentidad{ID: o.ID, Version: f.Version, Motivo: "Lo revisé por videollamada con el titular"}))(t)
	_, err = e.m.Titular.ValidarIdentidad.Ejecutar(e.cs, campo.EntradaValidarIdentidad{ID: o.ID, Version: f.Version + 1, Motivo: "Otra vez la misma validación"})
	problema(t, err, 409, "identidad_ya_validada")
	esperar(t, e.lote(ct, op(campo.OpCerrar, o.ID, map[string]any{"resultado": "exitosa"})), "aplicada")
	e.relay()
	if act := e.pendiente("iris:instalacion:" + orden); len(act.DependeDe) != 0 {
		t.Fatalf("activación sin pasos del titular: %+v", act)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.identidad_validada' AND payload->>'metodo' = 'sin_identidad' AND payload->>'orden_id' = $1", orden); n != 1 {
		t.Fatalf("evento orden.identidad_validada: %d", n)
	}

	// 2. Identidad rechazada con la foto aún en el depósito: el supervisor la manda revisada.
	o2 := e.instalacion(tec, ct, true)
	orden2 := strconv.FormatInt(o2.ID, 10)
	e.falso.rechazar("/identidad", "cliente_sin_instalacion")
	cierre2 := trabajoDeInstalacion(o2.ID)[2:]
	ops := append([]campo.OperacionLote{op(campo.OpRegistrarIdentidad, o2.ID, datosIdentidad())}, cierre2...)
	esperar(t, e.lote(ct, ops...), repetir(len(ops), "aplicada")...) // registrada: cierra
	e.relay()
	e.procesar()
	if act := e.pendiente("iris:instalacion:" + orden2); act.Estado != pendientes.EstadoBloqueado {
		t.Fatalf("activación bloqueada por la identidad rechazada: %+v", act)
	}
	f2 := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o2.ID}))(t)
	deber(e.m.Titular.ValidarIdentidad.Ejecutar(e.cs, campo.EntradaValidarIdentidad{ID: o2.ID, Version: f2.Version, Motivo: "Revisé la foto tomada en sitio"}))(t)
	p2 := e.pendiente("iris:identidad:" + orden2 + ":2")
	if p2.Actor == nil || *p2.Actor != "usuario:"+strconv.FormatInt(e.sup, 10) {
		t.Fatalf("la revisión va con el supervisor como actor: %+v", p2)
	}
	e.procesar()
	l := e.falso.porClave("iris:identidad:" + orden2 + ":2")
	if l == nil || l.Partes["metodo"] != "revision_panel" || l.Partes["documento_numero"] != dpiPrueba || l.Archivos["frente"] != "image/jpeg:2048" {
		t.Fatalf("A41 revisión desde el panel con la foto tomada en sitio: %+v", l)
	}
	if act := e.pendiente("iris:instalacion:" + orden2); act.Estado != pendientes.EstadoHecho {
		t.Fatalf("activación tras la revisión: %+v", act)
	}

	// 3. Identidad rechazada y la foto ya borrada: el paso queda resuelto y la activación sale.
	o3 := e.instalacion(tec, ct, true)
	orden3 := strconv.FormatInt(o3.ID, 10)
	e.falso.rechazar("/identidad", "validacion")
	cierre3 := trabajoDeInstalacion(o3.ID)[2:]
	ops = append([]campo.OperacionLote{op(campo.OpRegistrarIdentidad, o3.ID, datosIdentidad())}, cierre3...)
	esperar(t, e.lote(ct, ops...), repetir(len(ops), "aplicada")...)
	e.relay()
	e.procesar()
	e.reloj.avanzar(8 * 24 * time.Hour)
	e.procesar() // purga el depósito
	f3 := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o3.ID}))(t)
	deber(e.m.Titular.ValidarIdentidad.Ejecutar(e.cs, campo.EntradaValidarIdentidad{ID: o3.ID, Version: f3.Version, Motivo: "Foto vencida; titular confirmado en persona"}))(t)
	if p := e.pendiente("iris:identidad:" + orden3 + ":1"); p.Estado != pendientes.EstadoResueltoManual {
		t.Fatalf("identidad resuelta: %+v", p)
	}
	e.procesar()
	if act := e.pendiente("iris:instalacion:" + orden3); act.Estado != pendientes.EstadoHecho {
		t.Fatalf("activación sin la identidad: %+v", act)
	}
	sinSensibles(t)
}

func TestTitularReglasDelLote(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, false)

	malo := datosIdentidad()
	malo["documentoNumero"] = "1234567890199" // municipio 99 no existe
	r := e.lote(ct,
		op(campo.OpRegistrarIdentidad, o.ID, malo),
		op(campo.OpCambiarPlan, o.ID, map[string]any{"planId": 300, "motivo": "mas_velocidad"}),
	)
	esperar(t, r, "rechazada:validacion", "rechazada:bloqueada_por_rechazo")
	crudo, _ := json.Marshal(r.Resultados[0].Error)
	if strings.Contains(string(crudo), "1234567890199") {
		t.Fatalf("el rechazo repite el documento: %s", crudo)
	}
	// Sin identidad, ni datos ni plan (titular → datos → plan).
	esperar(t, e.lote(ct, op(campo.OpCambiarPlan, o.ID, map[string]any{"planId": 300, "motivo": "mas_velocidad"})), "rechazada:falta_identidad")
	// Las marcas del técnico son obligatorias; la foto debe ser lo que dice.
	sinMarca := datosIdentidad()
	sinMarca["coincide"] = false
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, sinMarca)), "rechazada:validacion")
	png := datosIdentidad()
	png["frente"] = map[string]any{"mime": "image/png", "base64": jpegFalso(100)}
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, png)), "rechazada:mime_distinto")
	// Otro técnico no ve la orden.
	_, ctB := e.tecnicoEnEmpresa()
	esperar(t, e.lote(ctB, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())), "rechazada:no_encontrado")
	// Fuera de sitio (pausada), no.
	esperar(t, e.lote(ct, op(campo.OpPausar, o.ID, map[string]any{"motivo": "clima"}),
		op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())), "aplicada", "rechazada:orden_pausada")
	esperar(t, e.lote(ct, op(campo.OpReanudar, o.ID, nil), op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad()),
		op(campo.OpCambiarTitular, o.ID, datosCambioTitular()),
		op(campo.OpCompletarDatos, o.ID, map[string]any{"cliente": map[string]any{"tipoDocumentoFiscal": "cf", "nitCui": "123"}})),
		"aplicada", "aplicada", "aplicada", "rechazada:validacion")
	// Ya con datos o plan, otro titular no (lo corrige un supervisor).
	esperar(t, e.lote(ct, op(campo.OpCambiarPlan, o.ID, map[string]any{"planId": 300, "motivo": "mas_velocidad"}),
		op(campo.OpCambiarTitular, o.ID, datosCambioTitular())), "aplicada", "rechazada:titular_despues_de_datos")
	sinSensibles(t)

	// Sin conector (modo sin Argos): el paso no aplica.
	sinArgos := campo.Nuevo(campo.Config{Adjuntos: campo.SinAdjuntosObligatorios{}})
	r, err := sinArgos.Lote(ct, campo.EntradaLote{DispositivoID: "tel-sin-" + pruebas.Unico(), Operaciones: []campo.OperacionLote{
		op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())}})
	if err != nil {
		t.Fatal(err)
	}
	esperar(t, r, "rechazada:argos_sin_conector")
}

func TestDocumentosDeGuatemala(t *testing.T) {
	for _, c := range []struct {
		n  string
		ok bool
	}{{dpiPrueba, true}, {"1234567880101", false}, {"1234567892301", false}, {"1234567890100", false}, {"123456789010", false}} {
		if campo.CUIValido(c.n) != c.ok {
			t.Fatalf("CUI %s: %v", c.n, !c.ok)
		}
	}
	for _, c := range []struct {
		n  string
		ok bool
	}{{nitPrueba, true}, {"12345678", false}, {"576937K", true}, {"5769370", false}, {"K", false}} {
		if campo.NITValido(c.n) != c.ok {
			t.Fatalf("NIT %s: %v", c.n, !c.ok)
		}
	}
}
