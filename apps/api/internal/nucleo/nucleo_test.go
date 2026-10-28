package nucleo_test

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

func TestCursor(t *testing.T) {
	c := nucleo.CodificarCursor(int64(42), "x")
	var id int64
	var s string
	if ok, err := nucleo.DecodificarCursor(c, &id, &s); !ok || err != nil || id != 42 || s != "x" {
		t.Fatalf("ida y vuelta: %v %v %d %s", ok, err, id, s)
	}
	if ok, err := nucleo.DecodificarCursor("", &id); ok || err != nil {
		t.Fatal("cursor vacío = primera página")
	}
	for _, malo := range []string{"!!", base64.RawURLEncoding.EncodeToString([]byte(`{"a":1}`)), nucleo.CodificarCursor(1, 2)} {
		_, err := nucleo.DecodificarCursor(malo, &id)
		var p *nucleo.Problema
		if !errors.As(err, &p) || p.Status != 422 || p.Errors[0].Field != "cursor" {
			t.Fatalf("%q: %v", malo, err)
		}
	}
}

func TestPaginar(t *testing.T) {
	filas := []int{1, 2, 3}
	p := nucleo.Paginar(filas, 2, func(i int) string { return fmt.Sprint(i) }, func(i int) []any { return []any{i} })
	if len(p.Data) != 2 || p.NextCursor == nil {
		t.Fatalf("con más filas que el límite hay cursor: %+v", p)
	}
	p = nucleo.Paginar(filas[:2], 2, func(i int) string { return fmt.Sprint(i) }, func(i int) []any { return []any{i} })
	if p.NextCursor != nil {
		t.Fatal("sin más filas, nextCursor null")
	}
	if l := (nucleo.ParamsPagina{Limit: 0}).Limite(); l != nucleo.LimitePorDefecto {
		t.Fatalf("límite por defecto: %d", l)
	}
}

func TestAProblema(t *testing.T) {
	casos := []struct {
		err    error
		status int
		tipo   string
	}{
		{nucleo.ConflictoVersion("usuario", 1), 409, "conflicto_version"},
		{fmt.Errorf("envuelto: %w", nucleo.Prohibido("")), 403, "prohibido"},
		{pgx.ErrNoRows, 404, "no_encontrado"},
		{&pgconn.PgError{Code: "23505", ConstraintName: "x"}, 409, "duplicado"},
		{&pgconn.PgError{Code: "23503"}, 422, "referencia_invalida"},
		{errors.New("algo interno"), 500, "interno"},
	}
	for _, c := range casos {
		p, _ := nucleo.AProblema(c.err)
		if p.Status != c.status || p.Codigo() != c.tipo {
			t.Errorf("%v → %d %s", c.err, p.Status, p.Codigo())
		}
	}
	if p, interno := nucleo.AProblema(errors.New("secreto de la base")); !interno || p.Detail == "secreto de la base" {
		t.Error("un error interno no filtra su mensaje")
	}
	if err := nucleo.VerificarVersion(pgx.ErrNoRows, "usuario", 3); err.(*nucleo.Problema).Status != 409 {
		t.Error("ErrNoRows de un UPDATE con versión = 409")
	}
}

func TestCifrador(t *testing.T) {
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	c, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	cifrado, _ := c.Cifrar([]byte("secreto"), []byte("usuario:1"))
	if plano, err := c.Descifrar(cifrado, []byte("usuario:1")); err != nil || string(plano) != "secreto" {
		t.Fatalf("descifrar: %s %v", plano, err)
	}
	if _, err := c.Descifrar(cifrado, []byte("usuario:2")); err == nil {
		t.Fatal("con otros datos asociados no descifra")
	}
	if _, err := nucleo.NuevoCifrador("corta"); err == nil {
		t.Fatal("clave inválida")
	}
}

func TestCatalogoOrdenadoYSinRepetidos(t *testing.T) {
	vistos := map[nucleo.Permiso]bool{}
	for i, p := range nucleo.Catalogo {
		if vistos[p.Codigo] || (i > 0 && nucleo.Catalogo[i-1].Codigo >= p.Codigo) {
			t.Fatalf("catálogo desordenado o repetido en %s", p.Codigo)
		}
		vistos[p.Codigo] = true
	}
	for _, r := range nucleo.RolesBase {
		for _, p := range r.PermisosDeRol() {
			if !nucleo.EsPermiso(string(p)) {
				t.Fatalf("el rol %s usa %s, que no está en el catálogo", r.Codigo, p)
			}
		}
	}
}

func TestRegistroDeTablasConEmpresa(t *testing.T) {
	for _, malo := range []string{"tickets", "soporte.tickets; DROP TABLE x", "Soporte.Tickets", "auditoria.acciones"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%q debía entrar en pánico", malo)
				}
			}()
			nucleo.RegistrarTablaConEmpresa(malo)
		}()
	}
	nucleo.RegistrarTablaConEmpresa("prueba.b")
	nucleo.RegistrarTablaConEmpresa("prueba.a")
	nucleo.RegistrarTablaConEmpresa("prueba.b")
	t.Cleanup(func() { nucleo.QuitarTablaConEmpresa("prueba.a"); nucleo.QuitarTablaConEmpresa("prueba.b") })
	if got := nucleo.TablasConEmpresa(); len(got) != 2 || got[0] != "prueba.a" || got[1] != "prueba.b" {
		t.Fatalf("registro: %v", got)
	}
}
