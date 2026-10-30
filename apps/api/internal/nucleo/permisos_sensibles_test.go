package nucleo_test

import (
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Todo permiso del catálogo que no es de ver ni operativo es de administración y debe estar en
// PermisosSensibles (así un permiso nuevo no queda fuera del MFA ni de la re-autenticación del
// autologin).
func TestPermisosSensiblesCubrenLaAdministracion(t *testing.T) {
	operativos := []string{"tickets.", "ordenes.", "prospectos.", "campo.", "agenda.", "rutas.", "cotizaciones.",
		"contratos.", "encuestas.", "clientes.", "adjuntos."}
	operativosSueltos := map[nucleo.Permiso]bool{nucleo.PermFactibilidadEvaluar: true, nucleo.PermFactibilidadRegistrarManual: true,
		nucleo.PermEmpresasTodas: true} // empresas.todas es un alcance: esPrivilegiado lo cuenta aparte
	for _, d := range nucleo.Catalogo {
		c := string(d.Codigo)
		_, accion, _ := strings.Cut(c, ".")
		if accion == "ver" || strings.HasPrefix(accion, "ver_") || operativosSueltos[d.Codigo] {
			if nucleo.EsSensible(d.Codigo) {
				t.Errorf("%s es de ver u operativo: no va en PermisosSensibles", c)
			}
			continue
		}
		operativo := false
		for _, p := range operativos {
			operativo = operativo || strings.HasPrefix(c, p)
		}
		if !operativo && !nucleo.EsSensible(d.Codigo) {
			t.Errorf("%s es de administración y falta en nucleo.PermisosSensibles", c)
		}
	}
	enCatalogo := map[nucleo.Permiso]bool{}
	for _, d := range nucleo.Catalogo {
		enCatalogo[d.Codigo] = true
	}
	for p := range nucleo.PermisosSensibles {
		if !enCatalogo[p] {
			t.Errorf("%s no está en el catálogo", p)
		}
	}
}
