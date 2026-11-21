package pendientes_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
)

// aplicar corre EncolarAlAplicar en un caso de uso de prueba, como lo hace campo al aplicar la
// operación del lote.
func (e *entorno) aplicar(orden map[string]any) bool {
	e.t.Helper()
	cuerpo, err := json.Marshal(orden)
	if err != nil {
		e.t.Fatal(err)
	}
	caso := auditoria.Nuevo(auditoria.Caso[[]byte, bool]{
		Accion: "prueba.aplicar_equipo", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, c []byte) (bool, error) {
			return e.m.EncolarAlAplicar(ctx, t, c)
		},
	})
	ok, err := caso.Ejecutar(e.ctx(), cuerpo)
	if err != nil {
		e.t.Fatal(err)
	}
	return ok
}

// foto registra una foto de la orden en ese estado.
func (e *entorno) foto(orden, estado string) string {
	e.t.Helper()
	id := uuid.Must(uuid.NewV7())
	// completado_en: la hora del reloj de las pruebas (dentro de la ventana de 72 h desde la fecha).
	var completado *time.Time
	if estado == "completo" {
		completado = &e.reloj
	}
	e.exec(`INSERT INTO nucleo.adjuntos (id, entidad, entidad_id, tipo, mime, tamano, sha256, almacenamiento, clave, estado, completado_en)
		VALUES ($1, 'orden', $2, 'foto', 'image/jpeg', 10, repeat('a', 64), 'local', $3, $4, $5)`, id, orden, "orden/"+id.String(), estado, completado)
	return id.String()
}

func intentoAplicado(orden, intento string, unidad, tec int64, foto string, fecha time.Time) map[string]any {
	return map[string]any{
		"orden_id": orden, "tipo": "retiro", "cliente_id": 10, "servicio_id": 59, "tecnico_id": tec,
		"equipos": []any{map[string]any{"id": intento, "accion": "no_recuperado", "unidad_id": unidad, "motivo": "se_nego",
			"foto_ref": foto, "ubicacion": map[string]any{"lat": 14.6, "lng": -90.5, "precision_m": 10}, "usuario_id": tec,
			"fecha": fecha.UTC().Format(time.RFC3339), "recibido_en_iris": fecha.UTC().Format(time.RFC3339)}},
	}
}

// Contrato 0.14.0 §3.10: con un Argos ≥ 0.14.0 el intento sale al aplicarse, con la clave del
// intento y el X-Iris-Actor de quien lo registró, y solo cuando su foto está completo; con un Argos
// anterior no se encola al aplicar (lo encola el cierre).
func TestIntentoSaleAlAplicarseConSuFoto014(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.13.0")
	tec := e.tecnico("u-44")
	orden := nuevaOrden()
	intento := uuid.Must(uuid.NewV7()).String()
	foto := e.foto(orden, "pendiente")
	if e.aplicar(intentoAplicado(orden, intento, 99, tec, foto, e.reloj)) {
		t.Fatal("con un Argos < 0.14.0 no sale al aplicarse")
	}
	e.contrato("0.14.0")
	if !e.aplicar(intentoAplicado(orden, intento, 99, tec, foto, e.reloj)) {
		t.Fatal("con 0.14.0 sale al aplicarse")
	}
	clave := "iris:no_recuperado:" + orden + ":99:" + intento
	if p := e.pendientes(orden)[clave]; p.Estado != "bloqueado" || p.Error != pendientes.EsperandoFoto {
		t.Fatalf("espera la foto: %+v", p)
	}
	e.procesar()
	for _, l := range e.argos.copia() {
		if l.Clave == clave {
			t.Fatal("no sale sin la foto completo")
		}
	}
	e.exec(`UPDATE nucleo.adjuntos SET estado = 'completo' WHERE id = $1`, foto)
	e.procesar() // el barrido de fotos (red de seguridad del evento adjunto.completado)
	l := e.llamadaA(clave)
	if l.Actor != "usuario:"+strconv.FormatInt(tec, 10) || l.Cuerpo["foto_ref"] != foto || l.Ruta != "/servicios/59/equipos/99/no-recuperado" {
		t.Fatalf("intento enviado: %s actor=%q", l.Crudo, l.Actor)
	}
}

// Contrato 0.14.0 §3.10: una foto rechazada deja el intento en foto_invalida; una que no queda
// completo en 72 h desde la fecha, en foto_fuera_de_ventana. Los dos, para una persona.
func TestIntentoConFotoRechazadaOFueraDeVentana014(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.14.0")
	tec := e.tecnico("u-45")
	orden := nuevaOrden()
	mala, lenta := e.foto(orden, "pendiente"), e.foto(orden, "pendiente")
	i1, i2 := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	e.aplicar(intentoAplicado(orden, i1, 100, tec, mala, e.reloj))
	e.aplicar(intentoAplicado(orden, i2, 101, tec, lenta, e.reloj.Add(-73*time.Hour)))
	e.exec(`UPDATE nucleo.adjuntos SET estado = 'rechazado' WHERE id = $1`, mala)
	e.procesar()
	ps := e.pendientes(orden)
	if p := ps["iris:no_recuperado:"+orden+":100:"+i1]; p.Estado != "fallido_negocio" {
		t.Fatalf("foto rechazada: %+v", p)
	}
	if p := ps["iris:no_recuperado:"+orden+":101:"+i2]; p.Estado != "fallido_negocio" {
		t.Fatalf("foto fuera de la ventana: %+v", p)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND codigo_error = ANY($2)`, orden,
		[]string{pendientes.CodigoFotoInvalida, pendientes.CodigoFotoFueraDeVentana}); n != 2 {
		t.Fatalf("códigos: %d", n)
	}
	if len(e.argos.copia()) != 0 {
		t.Fatal("ninguno sale")
	}
}
