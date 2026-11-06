package mensajeria_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// integracion crea un usuario de tipo integración con su token.
func (b *bandeja) integracion(rol string) persona {
	b.t.Helper()
	u, err := identChat.CrearUsuario.Ejecutar(b.ctx, identidad.EntradaCrearUsuario{Nombre: "Integración " + pruebas.Unico(), Correo: pruebas.Correo("integ"),
		Tipo: "integracion", Roles: []identidad.AsignacionRol{rolBase(rol)}, Empresas: []int64{1}})
	if err != nil {
		b.t.Fatal(err)
	}
	tk, err := identChat.CrearToken.Ejecutar(pruebas.ContextoUsuario(b.bd, u.ID, nucleo.PermTokensCrear), identidad.EntradaCrearToken{Nombre: "integración"})
	if err != nil {
		b.t.Fatal(err)
	}
	return persona{id: u.ID, token: tk.Valor}
}

// Toda lectura y acción sobre conversaciones es de personas con sesión y conversaciones.ver: ni un
// token (aunque sea de un supervisor), ni una integración, ni un rol con tickets.crear sin
// conversaciones.ver ven que la conversación existe.
func TestConvSoloPersonasConConversacionesVer(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.s1", "Hola, mi número es secreto")
	c := b.conv(num)
	ruta := "/conversaciones/" + cid(c)
	v := ver(c)

	tokenSup := b.entorno.persona([]int64{1}, rolBase("supervisor"))
	horus := b.integracion("integracion_horus")
	codigo := fmt.Sprintf("solo_tickets_%d", time.Now().UnixNano()%1_000_000_000)
	if _, err := identChat.CrearRol.Ejecutar(b.ctx, identidad.EntradaCrearRol{Codigo: codigo, Nombre: "Solo tickets " + pruebas.Unico(),
		Permisos: []nucleo.Permiso{nucleo.PermTicketsCrear, nucleo.PermTicketsVer}}); err != nil {
		t.Fatal(err)
	}
	medida := b.persona([]int64{1}, rolBase(codigo))

	type peticion struct {
		metodo, ruta string
		cuerpo       any
	}
	todas := []peticion{
		{http.MethodGet, ruta, nil},
		{http.MethodGet, ruta + "/mensajes", nil},
		{http.MethodGet, ruta + "/plantillas", nil},
		{http.MethodGet, ruta + "/coincidencias", nil},
		{http.MethodPost, ruta + "/mensajes", map[string]any{"id": nuevoID(), "texto": "hola"}},
		{http.MethodPost, ruta + "/notas", map[string]any{"id": nuevoID(), "texto": "nota"}},
		{http.MethodPost, ruta + "/tomar", map[string]any{"version": v}},
		{http.MethodPost, ruta + "/crear-ticket", map[string]any{"asunto": "Sin internet en casa"}},
		{http.MethodPost, ruta + "/promesas/simular", map[string]any{"servicioId": 10, "venceEl": "2026-11-20"}},
		{http.MethodPost, ruta + "/boletas", map[string]any{"adjuntoId": nuevoID(), "metodo": "deposito", "cuentaBancariaId": 3, "referencia": "1",
			"monto": "1.00", "fechaDeposito": "2026-11-04"}},
	}
	crearTicket := todas[7]
	revisar := func(quien string, p persona, ps []peticion, permitidos ...int) {
		t.Helper()
		for _, x := range ps {
			r := b.api(p, x.metodo, x.ruta, x.cuerpo)
			ok := false
			for _, s := range permitidos {
				ok = ok || r.status == s
			}
			if !ok {
				t.Errorf("%s %s %s: %d %v", quien, x.metodo, x.ruta, r.status, r.cuerpo)
			}
			if strings.Contains(fmt.Sprint(r.cuerpo), num) || strings.Contains(fmt.Sprint(r.cuerpo), "secreto") {
				t.Errorf("%s %s %s: revela la conversación: %v", quien, x.metodo, x.ruta, r.cuerpo)
			}
		}
	}
	// Token personal (con todos los permisos de supervisor): 404 en todo.
	revisar("token", tokenSup, todas, http.StatusNotFound)
	// Lo que piden un permiso que la integración o el rol a medida sí tienen (tickets.crear): 404.
	revisar("horus", horus, []peticion{crearTicket}, http.StatusNotFound)
	revisar("a medida", medida, []peticion{crearTicket}, http.StatusNotFound)
	// Lo demás: el ACL los frena antes (403) o 404; nunca la conversación.
	revisar("horus", horus, todas, http.StatusNotFound, http.StatusForbidden)
	revisar("a medida", medida, todas, http.StatusNotFound, http.StatusForbidden)
	// La lista y los contadores tampoco.
	for _, p := range []persona{tokenSup, horus, medida} {
		if r := b.api(p, http.MethodGet, "/conversaciones?limit=200", nil); r.status == http.StatusOK {
			t.Errorf("lista para %d: %v", p.id, r.cuerpo)
		}
	}
	if b.contar(`SELECT count(*) FROM soporte.tickets WHERE asunto = 'Sin internet en casa'`) != 0 {
		t.Fatal("se creó un ticket desde una conversación que no se ve")
	}
}

// Responder una conversación que nadie tiene la asigna a quien responde (si puede atenderla).
func TestConvResponderAsigna(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.r1", "Hola")
	c := b.conv(num)
	if _, ok := c["asignado"]; ok {
		t.Fatalf("nueva y sin asignar: %v", c["asignado"])
	}
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/mensajes", map[string]any{"id": nuevoID(), "texto": "Con gusto"}), http.StatusCreated)
	c = b.conv(num)
	if a, ok := c["asignado"].(map[string]any); !ok || a["id"].(float64) != float64(b.agente.id) {
		t.Fatalf("responder asigna: %v", c["asignado"])
	}
	if b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.asignada' AND payload->>'conversacion_id' = $1 AND payload->>'origen' = 'responder'`, cid(c)) != 1 {
		t.Fatal("conversacion.asignada origen responder")
	}
	// Si ya la tiene alguien, responder (un supervisor) no la cambia.
	b.exigir(b.api(b.supervisor, http.MethodPost, "/conversaciones/"+cid(c)+"/mensajes", map[string]any{"id": nuevoID(), "texto": "Aquí estoy"}), http.StatusCreated)
	if a := b.conv(num)["asignado"].(map[string]any); a["id"].(float64) != float64(b.agente.id) {
		t.Fatalf("responder no reasigna: %v", a)
	}
}

// Un contacto de Iris con permiso registrado por número recibe plantillas fuera de la ventana; una
// baja por chat (palabra BAJA) lo impide aunque no sea cliente.
func TestConvConsentimientoPorNumero(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.k1", "Hola")
	c := b.conv(num)
	r := b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/contacto-iris", map[string]any{"version": ver(c), "nombre": "Proveedor Y", "tipo": "proveedor"})
	b.exigir(r, http.StatusOK)
	contacto := int64(r.cuerpo["contacto"].(map[string]any)["contactoIrisId"].(float64))
	*b.reloj = b.reloj.Add(25 * time.Hour)
	p, _ := mensajeria.PlantillaPorCodigo("bienvenida")
	vars := map[string]string{}
	for _, x := range p.Variables {
		vars[x.Nombre] = ejemploVariable(x.Formato)
	}
	ruta := "/conversaciones/" + cid(c) + "/mensajes"
	pl := func() map[string]any {
		return map[string]any{"id": nuevoID(), "plantilla": map[string]any{"codigo": "bienvenida", "variables": vars}}
	}
	b.exigir(b.api(b.agente, http.MethodPost, ruta, pl()), http.StatusUnprocessableEntity, "type", "urn:iris:problema:sin_permiso")
	consent := fmt.Sprintf("/contactos-iris/%d/consentimiento", contacto)
	b.exigir(b.api(b.lectura, http.MethodPost, consent, map[string]any{"estado": "acepta"}), http.StatusForbidden)
	b.exigir(b.api(b.agente, http.MethodPost, consent, map[string]any{"estado": "acepta"}), http.StatusOK, "estado", "acepta")
	b.exigir(b.api(b.agente, http.MethodPost, ruta, pl()), http.StatusCreated, "tipo", "plantilla")
	// Otra queda en cola; si antes de salir el número pide baja, al enviarla se revisa y falla «baja».
	r = b.api(b.agente, http.MethodPost, ruta, pl())
	b.exigir(r, http.StatusCreated)
	b.exigir(b.api(b.agente, http.MethodPost, consent, map[string]any{"estado": "baja"}), http.StatusOK, "estado", "baja")
	if _, err := b.m.ProcesarMensaje(b.ctx, uuid.MustParse(r.cuerpo["id"].(string)), false); err != nil {
		t.Fatal(err)
	}
	var estado, motivo string
	if err := b.bd.QueryRow(b.ctx, `SELECT estado_entrega, coalesce(motivo_falla, '') FROM mensajeria.conversacion_mensajes WHERE id = $1`, r.cuerpo["id"]).Scan(&estado, &motivo); err != nil {
		t.Fatal(err)
	}
	if estado != "fallido" || motivo != "baja" {
		t.Fatalf("plantilla retenida con baja: %s %s", estado, motivo)
	}
	b.exigir(b.api(b.agente, http.MethodPost, consent, map[string]any{"estado": "acepta"}), http.StatusOK)
	// Escribe BAJA: queda la baja del número (origen cliente) y ya no sale.
	*b.reloj = b.reloj.Add(time.Minute)
	b.texto(num, "wamid.k2", "BAJA")
	if b.contar(`SELECT count(*) FROM mensajeria.consentimientos_numero WHERE telefono = $1 AND estado = 'baja' AND origen = 'cliente'`, "+"+num) != 1 {
		t.Fatal("baja por número")
	}
	*b.reloj = b.reloj.Add(25 * time.Hour)
	b.exigir(b.api(b.agente, http.MethodPost, ruta, pl()), http.StatusUnprocessableEntity, "type", "urn:iris:problema:baja")
}

// Varios números en la misma app de Meta: un cambio de otro phone_number_id va a su canal solo si es
// de la misma empresa y comparte el app_secret que verificó la firma.
func TestConvOtroNumeroDeLaMismaApp(t *testing.T) {
	b := nuevaBandeja(t, nil)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO nucleo.empresas (id, nombre, origen) VALUES (4, 'Empresa 4', 'argos') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	crear := func(empresa int64, secreto string) mensajeria.Canal {
		pnid := fmt.Sprint(time.Now().UnixNano() % 1e12)
		c, err := b.m.CrearCanal.Ejecutar(b.ctx, mensajeria.EntradaCrearCanal{EmpresaID: &empresa, Nombre: "Otro " + pruebas.Unico(), Numero: "+50222000030",
			PhoneNumberID: pnid, Token: "token-de-meta-0123456789abcdef", AppSecret: secreto, VerifyToken: "verify-token-0123456789", Usos: []string{"soporte"}})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	hermano := crear(1, secretoWA)
	otraEmpresa := crear(4, secretoWA)
	otroSecreto := crear(1, "otro-app-secret-0123456789abcdef")
	pnidDe := func(c mensajeria.Canal) string {
		var p string
		if err := b.bd.QueryRow(b.ctx, `SELECT phone_number_id FROM mensajeria.canales WHERE id = $1`, c.ID).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cuantas := func(canal int64, num string) int {
		return b.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2`, canal, "+"+num)
	}
	for _, caso := range []struct {
		canal mensajeria.Canal
		entra bool
	}{{hermano, true}, {otraEmpresa, false}, {otroSecreto, false}} {
		num := numeroCliente()
		cuerpo := strings.Replace(b.mensajeCliente(num, "wamid.o"+num, `"type":"text","text":{"body":"Hola"}`),
			fmt.Sprintf(`"phone_number_id":%q`, b.pnid), fmt.Sprintf(`"phone_number_id":%q`, pnidDe(caso.canal)), 1)
		b.webhook(cuerpo)
		if got := cuantas(caso.canal.ID, num) == 1; got != caso.entra {
			t.Errorf("canal %d: entra=%v (esperaba %v)", caso.canal.ID, got, caso.entra)
		}
		if cuantas(b.canal.ID, num) != 0 {
			t.Errorf("canal %d: quedó en el canal que recibió el webhook", caso.canal.ID)
		}
	}
}
