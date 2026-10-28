package agenda_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// La migración de las llaves foráneas sobre una base con zona_id y cuadrilla_id huérfanos (como
// la de main): se reconstruye el estado previo en una transacción (sin las llaves), se corre la
// sección Up y se revierte todo al final.
func TestMigracionLlavesConHuerfanos(t *testing.T) {
	crudo, err := os.ReadFile("../../migraciones/20261028175434_agenda_llaves_zonas_cuadrillas.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(strings.SplitN(string(crudo), "-- +goose Down", 2)[0], "-- +goose Up", 2)[1]

	ctx := context.Background()
	tx, err := pruebas.Pool(t).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	uno := func(sql string, args ...any) (s string) {
		t.Helper()
		if err := tx.QueryRow(ctx, sql, args...).Scan(&s); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return s
	}
	exec(`ALTER TABLE soporte.tickets DROP CONSTRAINT tickets_zona_fk, DROP CONSTRAINT tickets_cuadrilla_fk`)
	exec(`ALTER TABLE campo.ordenes DROP CONSTRAINT ordenes_zona_fk, DROP CONSTRAINT ordenes_cuadrilla_fk`)
	exec(`INSERT INTO integracion.argos_zonas (id, empresa_id, nombre, sincronizado_en) VALUES (880001, 31, 'Centro (Argos)', now())`)
	// 880001: zona de Argos usada por un ticket; 880002: huérfana de una sola empresa; 880003:
	// huérfana de dos empresas; 880004: solo en un alcance. Cuadrillas 880010 (una empresa) y
	// 880011 (sin empresa).
	exec(`INSERT INTO soporte.tickets (prioridad, origen, asunto, sla_inicio, empresa_id, zona_id, cuadrilla_id) VALUES
		('media', 'panel', 'A', now(), 31, 880001, 880010), ('media', 'panel', 'B', now(), 31, 880002, NULL),
		('media', 'panel', 'C', now(), 31, 880003, 880011)`)
	exec(`INSERT INTO campo.ordenes (tipo_id, direccion, empresa_id, zona_id, cuadrilla_id)
		SELECT id, 'x', 32, 880003, 880010 FROM campo.tipos_orden WHERE codigo = 'visita'`)
	tec := usuarioCon(t, rol("tecnico"))
	exec(`INSERT INTO nucleo.usuarios_roles (usuario_id, rol_id, zona_id, cuadrilla_id)
		SELECT $1, id, 880004, NULL FROM nucleo.roles WHERE codigo = 'lectura'`, tec)
	exec(`INSERT INTO nucleo.usuarios_roles (usuario_id, rol_id, zona_id, cuadrilla_id)
		SELECT $1, id, NULL, 880011 FROM nucleo.roles WHERE codigo = 'ventas'`, tec)

	exec(up)

	// 880003 la usan dos empresas: sin empresa e inactiva (un admin global le asigna empresa).
	for id, esperado := range map[int64]string{
		880001: "31|argos|Centro (Argos)|true", 880002: "31|migrada|Zona 880002|true", 880003: "-|migrada|Zona 880003|false",
		880004: "-|migrada|Zona 880004|true",
	} {
		if got := uno(`SELECT coalesce(empresa_id::text, '-') || '|' || origen || '|' || nombre || '|' || activa FROM agenda.zonas WHERE id = $1`, id); got != esperado {
			t.Fatalf("zona %d: %s (esperaba %s)", id, got, esperado)
		}
	}
	// 880010 la usan la empresa 31 (ticket) y la 32 (orden): sin empresa; 880011 solo la 31.
	if got := uno(`SELECT coalesce(empresa_id::text, '-') || '|' || nombre || '|' || activa FROM agenda.cuadrillas WHERE id = 880010`); got != "-|Cuadrilla 880010 (migrada)|false" {
		t.Fatalf("cuadrilla 880010: %s", got)
	}
	if got := uno(`SELECT coalesce(empresa_id::text, '-') || '|' || activa FROM agenda.cuadrillas WHERE id = 880011`); got != "31|true" {
		t.Fatalf("cuadrilla 880011: %s", got)
	}
	// La identidad sigue después de los ids conservados.
	if got := uno(`INSERT INTO agenda.cuadrillas (nombre) VALUES ('Nueva tras migrar') RETURNING (id > 880011)::text`); got != "true" {
		t.Fatalf("identidad de cuadrillas: %s", got)
	}
	// Las llaves están de vuelta: un zona_id inexistente ya no entra.
	if _, err := tx.Exec(ctx, `SAVEPOINT fk; INSERT INTO soporte.tickets (prioridad, origen, asunto, sla_inicio, zona_id) VALUES ('media', 'panel', 'X', now(), 999999999)`); err == nil {
		t.Fatal("la llave foránea de zona_id no quedó")
	}
}

// Con las llaves, tickets y órdenes validan zona y cuadrilla con 422 claro (no un error de FK).
func TestTicketsValidanZonaYCuadrilla(t *testing.T) {
	e := nuevoEntorno()
	sop := soporte.Nuevo(soporte.Config{})
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	_, err := sop.CrearTicket.Ejecutar(cs, soporte.EntradaCrearTicket{Asunto: "Sin zona real", ZonaID: ptr(int64(777777))})
	if p := problema(t, err, 422, "validacion"); p.Errors[0].Field != "zonaId" {
		t.Fatalf("zona inexistente: %+v", p.Errors)
	}
	z := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona tickets " + pruebas.Unico()}))(t)
	tk := deber(sop.CrearTicket.Ejecutar(cs, soporte.EntradaCrearTicket{Asunto: "Con zona", ZonaID: &z.ID}))(t)
	_, err = sop.AsignarTicket.Ejecutar(cs, soporte.EntradaAsignarTicket{ID: tk.ID, Version: tk.Version, CuadrillaID: ptr(int64(777777))})
	if p := problema(t, err, 422, "validacion"); p.Errors[0].Field != "cuadrillaId" {
		t.Fatalf("cuadrilla inexistente: %+v", p.Errors)
	}
	c := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Cuadrilla tickets " + pruebas.Unico()}))(t)
	deber(e.ag.DesactivarCuadrilla.Ejecutar(cs, agenda.EntradaVersion{ID: c.ID, Version: c.Version}))(t)
	_, err = sop.AsignarTicket.Ejecutar(cs, soporte.EntradaAsignarTicket{ID: tk.ID, Version: tk.Version, CuadrillaID: &c.ID})
	problema(t, err, 422, "validacion")
}
