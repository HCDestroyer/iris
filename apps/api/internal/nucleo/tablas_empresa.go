package nucleo

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Tablas operativas con empresa_id (multiempresa). Cada módulo registra las suyas para que la
// acción administrativa POST /empresas/{id}/adoptar-sin-empresa pueda asignar una empresa a los
// datos que se guardaron con empresa_id null en modo «sin conector»:
//
//	func init() { nucleo.RegistrarTablaConEmpresa("soporte.tickets") }
//
// La tabla debe tener una columna empresa_id (bigint, null permitido). Solo datos operativos
// propios de Iris: nunca auditoria.* (no se modifica), integracion.outbox ni las instantáneas
// de Argos (su empresa la dice Argos).

var (
	tablasConEmpresaMu sync.RWMutex
	tablasConEmpresa   []string
	reTabla            = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
)

// RegistrarTablaConEmpresa agrega "esquema.tabla" al registro. Repetirla no hace nada. Entra en
// pánico si el nombre no es esquema.tabla en minúsculas (se descubre al arrancar), o si es de
// auditoria.
func RegistrarTablaConEmpresa(tabla string) {
	if !reTabla.MatchString(tabla) {
		panic(fmt.Sprintf("nucleo.RegistrarTablaConEmpresa: %q no es esquema.tabla", tabla))
	}
	if strings.HasPrefix(tabla, "auditoria.") {
		panic("nucleo.RegistrarTablaConEmpresa: auditoria.* no se modifica")
	}
	tablasConEmpresaMu.Lock()
	defer tablasConEmpresaMu.Unlock()
	if !slices.Contains(tablasConEmpresa, tabla) {
		tablasConEmpresa = append(tablasConEmpresa, tabla)
		slices.Sort(tablasConEmpresa)
	}
}

// QuitarTablaConEmpresa saca una tabla del registro (para pruebas).
func QuitarTablaConEmpresa(tabla string) {
	tablasConEmpresaMu.Lock()
	defer tablasConEmpresaMu.Unlock()
	tablasConEmpresa = slices.DeleteFunc(tablasConEmpresa, func(t string) bool { return t == tabla })
}

// TablasConEmpresa devuelve las tablas registradas, ordenadas.
func TablasConEmpresa() []string {
	tablasConEmpresaMu.RLock()
	defer tablasConEmpresaMu.RUnlock()
	return slices.Clone(tablasConEmpresa)
}
