package notificaciones_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// Menciones del chat del equipo de punta a punta (contratos/chat.md §3.2, push.md §3): el chat emite
// chat.mencion en el outbox y el emisor manda un solo push de tipo mencion al mencionado (no además
// el de chat), el resto de los miembros recibe el de chat y el autor nada.
func TestChatMencionDePuntaAPunta(t *testing.T) {
	e := nuevoEntorno(t, true)
	tec := e.persona([]int64{1}, "tecnico")
	despacho := e.persona([]int64{1}, "despacho")
	sup := e.persona([]int64{1}, "supervisor")
	tokTec, tokDespacho, tokSup := e.dispositivo(tec), e.dispositivo(despacho), e.dispositivo(sup)
	// La supervisión global ve todos los chats de la empresa 1: que no reciba los push de las demás
	// pruebas del paquete.
	t.Cleanup(func() {
		if _, err := e.bd.Exec(e.ctx, `UPDATE nucleo.usuarios SET activo = false WHERE id = $1`, sup.id); err != nil {
			t.Error(err)
		}
	})
	orden, numero := e.orden(1, tec.id, time.Date(2026, 11, 6, 20, 0, 0, 0, time.UTC), time.Date(2026, 11, 6, 22, 0, 0, 0, time.UTC))
	r := e.esperar(e.api(despacho, http.MethodPost, fmt.Sprintf("/api/v1/mensajeria/chat/ordenes/%d/abrir", orden), nil), http.StatusCreated)
	conv := int64(r.cuerpo["id"].(float64))

	msj := e.mensaje(despacho, conv, map[string]any{"texto": "@Tomás la bodega ya tiene conectores SC/APC", "menciones": []int64{tec.id}})
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'chat.mencion' AND payload->>'mensaje_id' = $1`, msj); n != 1 {
		t.Fatalf("chat.mencion en el outbox: %d", n)
	}
	e.procesar()

	if len(e.fcm.para(tokDespacho)) != 0 {
		t.Fatal("el autor recibe push de su propio mensaje")
	}
	ms := e.fcm.para(tokTec)
	if len(ms) != 1 {
		t.Fatalf("el mencionado recibe %d push (uno: la mención gana al chat)", len(ms))
	}
	d := ms[0].Data
	if d["tipo"] != "mencion" || d["canal"] != "mencion" || d["publicoTitulo"] != "Te mencionaron" || d["titulo"] != numero ||
		d["cuerpo"] != despacho.nombre+" te mencionó: @Tomás la bodega ya tiene conectores SC/APC" ||
		d["enlace"] != fmt.Sprintf("iris://chat/%d", conv) || d["grupo"] != fmt.Sprintf("mencion-%d", conv) {
		t.Fatalf("data: %v", d)
	}
	if dt := datos(t, ms[0]); dt["origen"] != "chat" || dt["mensajeId"] != msj {
		t.Fatalf("datos: %v", dt)
	}
	// La supervisión que cubre la orden no fue mencionada: push de chat.
	if s := e.fcm.para(tokSup); len(s) != 1 || s[0].Data["tipo"] != "chat" {
		t.Fatalf("supervisión: %v", s)
	}
}
