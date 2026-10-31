package comercial_test

import (
	"errors"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Contrato Iris ↔ ArgosISP 0.5.0 §3.2: el teléfono del prospecto se valida y normaliza como lo hace
// Argos (Guatemala con +502 opcional o internacional con +), así el alta no falla con 422 validacion.
func TestTelefonoDelProspectoComoArgos(t *testing.T) {
	e := nuevoEntorno(t, nil)
	ca := como(t, usuarioCon(t, rol("admin")), "admin")
	p := deber(e.com.CrearProspecto.Ejecutar(ca, comercial.EntradaCrearProspecto{Nombre: "Tel", Direccion: "x", Telefono: ptr("00502 5512 3344")}))(t)
	if p.Telefono == nil || *p.Telefono != "+502 5512-3344" {
		t.Fatalf("normalizado: %v", p.Telefono)
	}
	_, err := e.com.CrearProspecto.Ejecutar(ca, comercial.EntradaCrearProspecto{Nombre: "Tel", Direccion: "x", Telefono: ptr("1234")})
	problema(t, err, 422, "validacion")
	var pr *nucleo.Problema
	if !errors.As(err, &pr) || len(pr.Errors) != 1 || pr.Errors[0].Field != "telefono" || pr.Errors[0].Rule != "telefono" {
		t.Fatalf("inválido: %+v", err)
	}
	p = deber(e.com.EditarProspecto.Ejecutar(ca, comercial.EntradaEditarProspecto{ID: p.ID, Version: p.Version, Telefono: ptr("+1 415 555 0100")}))(t)
	if *p.Telefono != "+14155550100" {
		t.Fatalf("internacional: %v", *p.Telefono)
	}
	_, err = e.com.EditarProspecto.Ejecutar(ca, comercial.EntradaEditarProspecto{ID: p.ID, Version: p.Version, Telefono: ptr("8512-3344")})
	problema(t, err, 422, "validacion")
}
