package horus

import "github.com/danielgtaylor/huma/v2"

// Registrar publica las rutas de conexión y webhook de Horus bajo
// /api/v1/integraciones/horus/ (conectar, conexion/enlaces, conexion/canjear,
// webhook/{conector_id}). Las comunes (listar, activar, desactivar, URL pública, enlaces) las
// publica el conector Argos, dueño del núcleo.
func (m *Modulo) Registrar(api huma.API) {
	m.conexion.Registrar(api)
}
