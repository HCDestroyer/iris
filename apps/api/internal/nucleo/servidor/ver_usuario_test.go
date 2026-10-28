package servidor_test

import (
	"fmt"
	"net/http"
	"testing"
)

// GET /usuarios/{id}: mismo alcance que la lista; fuera de él, 404 como un id inexistente.
func TestVerUsuario(t *testing.T) {
	e := entornoEmpresas(t)
	nuevo := empresasAB(t, e)
	idAdmin, correoAdmin := nuevo("admin")
	idA, correoA := nuevo("lectura", 101)
	idA2, _ := nuevo("lectura", 101, 102)
	idB, _ := nuevo("lectura", 102)

	a := e.cliente()
	a.entrar(correoA)
	ver := func(c *cliente, id int64) respuesta {
		return c.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d", id), nil)
	}
	for _, id := range []int64{idA, idA2} {
		if r := ver(a, id); r.status != 200 || int64(r.cuerpo["id"].(float64)) != id || r.cuerpo["version"] == nil {
			t.Fatalf("ver %d: %d %s", id, r.status, r.crudo)
		}
	}
	ver(a, idB).problema(t, 404, "no_encontrado")
	ver(a, idAdmin).problema(t, 404, "no_encontrado")
	ver(a, 999999999).problema(t, 404, "no_encontrado")

	admin := e.cliente()
	admin.entrar(correoAdmin)
	if r := ver(admin, idB); r.status != 200 {
		t.Fatalf("admin global: %d %s", r.status, r.crudo)
	}
	// Sin usuarios.ver: 403.
	_, correoT := nuevo("tecnico", 101)
	tec := e.cliente()
	tec.entrar(correoT)
	ver(tec, idA).problema(t, 403, "prohibido")
}
