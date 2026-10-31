package inventario

import "context"

// GuardarArmadoConVersion guarda un cuerpo armado como lo haría un ArmarFirma que leyó el pendiente
// en esa versión (prueba de un armado viejo). true si quedó guardado.
func (m *Modulo) GuardarArmadoConVersion(ctx context.Context, pendienteID int64, version int32) (bool, error) {
	r, err := m.guardar.Ejecutar(ctx, entradaArmar{PendienteID: pendienteID, Version: version, cuerpo: []byte(`{"armado":"viejo"}`)})
	return r.Resultado == "lista", err
}
