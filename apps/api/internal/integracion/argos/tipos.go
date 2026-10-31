package argos

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/formato"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Tipos de la API de Argos tal como Iris los lee.
//
// Supuesto (a confirmar con el equipo de Argos): los recursos son JSON con los nombres del
// contrato en español. Para no depender del estilo (snake_case o camelCase) ni de si el recurso
// viene envuelto en { data }, la respuesta se normaliza antes de leerla: todas las claves pasan
// a snake_case, un objeto { data: {...} } se desenvuelve y una lista puede ser un arreglo o
// { data: [...], nextCursor }. Los números pueden venir como número o como texto (numeric de
// Postgres) y las coordenadas como lat/lon, latitud/longitud, { coordenadas: { lat, lon } } o
// { coordenadas: "lat,lng" } (la forma de Argos 1.3.0 y del contrato §3.2).

// Ref es una referencia anidada ({ id, nombre }).
type Ref struct {
	ID     *int64 `json:"id"`
	Nombre string `json:"nombre"`
}

// Coordenadas anidadas.
type Coordenadas struct {
	Lat      *Numero `json:"lat"`
	Lon      *Numero `json:"lon"`
	Latitud  *Numero `json:"latitud"`
	Longitud *Numero `json:"longitud"`
}

// UnmarshalJSON acepta el objeto ({ lat, lon } o { latitud, longitud }), el texto "lat,lng" de
// Argos (contrato §3.2; lo que devuelven /clientes y /servicios en 1.3.0) y null o "". Un texto
// que no son dos números deja las coordenadas vacías: la ficha se lee igual, sin mapa.
func (c *Coordenadas) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*c = Coordenadas{}
		return nil
	}
	if b[0] == '"' {
		var texto string
		if err := json.Unmarshal(b, &texto); err != nil {
			return err
		}
		*c = Coordenadas{}
		lat, lon, ok := strings.Cut(texto, ",")
		if !ok {
			return nil
		}
		var nLat, nLon Numero
		if nLat.UnmarshalJSON([]byte(strings.TrimSpace(lat))) != nil || nLon.UnmarshalJSON([]byte(strings.TrimSpace(lon))) != nil {
			return nil
		}
		la, lo := nLat.Float(), nLon.Float()
		if la == nil || lo == nil || *la < -90 || *la > 90 || *lo < -180 || *lo > 180 {
			return nil
		}
		c.Lat, c.Lon = &nLat, &nLon
		return nil
	}
	type crudo Coordenadas
	var v crudo
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*c = Coordenadas(v)
	return nil
}

type conCoordenadas struct {
	Lat         *Numero      `json:"lat"`
	Lon         *Numero      `json:"lon"`
	Latitud     *Numero      `json:"latitud"`
	Longitud    *Numero      `json:"longitud"`
	Coordenadas *Coordenadas `json:"coordenadas"`
}

// LatLon devuelve las coordenadas, vengan como vengan.
func (c conCoordenadas) LatLon() (*float64, *float64) {
	lat, lon := c.Lat, c.Lon
	if lat == nil {
		lat = c.Latitud
	}
	if lon == nil {
		lon = c.Longitud
	}
	if c.Coordenadas != nil {
		if lat == nil {
			lat = primero(c.Coordenadas.Lat, c.Coordenadas.Latitud)
		}
		if lon == nil {
			lon = primero(c.Coordenadas.Lon, c.Coordenadas.Longitud)
		}
	}
	// Normalizadas: 6 decimales y el 0,0 (formulario sin llenar) como sin coordenadas.
	return formato.Coordenadas(lat.Float(), lon.Float())
}

func primero(a, b *Numero) *Numero {
	if a != nil {
		return a
	}
	return b
}

// ClienteArgos es un cliente de Argos (GET /clientes, /clientes/:id).
type ClienteArgos struct {
	conCoordenadas
	ID        int64 `json:"id"`
	EmpresaID int64 `json:"empresa_id"`
	// Empresas: Argos no le da empresa al cliente (la tienen sus servicios); si algún día manda
	// empresas[] (como en el webhook cliente.actualizado), se suman a las de sus servicios.
	Empresas  []int64 `json:"empresas"`
	Codigo    string  `json:"codigo"`
	Nombre    string  `json:"nombre"`
	Telefono  string  `json:"telefono"`
	Direccion string  `json:"direccion"`
	ZonaID    *int64  `json:"zona_id"`
	Zona      *Ref    `json:"zona"`
	// Estado: Argos 1.4.0 no tiene estado de cliente (lo tienen sus servicios); se lee por si
	// algún día lo manda. Iris muestra el resumen de los servicios (resumenEstado).
	Estado string `json:"estado"`
	// Datos de contacto y fiscales (Argos 1.4.0, GET /clientes/:id): texto libre, se guardan
	// tal cual y se formatean al mostrar (formato).
	Tipo                string `json:"tipo"`
	TipoDocumentoFiscal string `json:"tipo_documento_fiscal"`
	NitCui              string `json:"nit_cui"`
	Movil               string `json:"movil"`
	Email               string `json:"email"`
	Correo              string `json:"correo"`
}

// CorreoCliente: email (Argos) o correo.
func (c ClienteArgos) CorreoCliente() string {
	if c.Email != "" {
		return c.Email
	}
	return c.Correo
}

// IDZona devuelve zona_id o zona.id.
func (c ClienteArgos) IDZona() *int64 { return idDe(c.ZonaID, c.Zona) }

// ServicioArgos es un servicio de Argos (GET /servicios?cliente_id=).
type ServicioArgos struct {
	conCoordenadas
	ID           int64  `json:"id"`
	EmpresaID    int64  `json:"empresa_id"`
	ClienteID    int64  `json:"cliente_id"`
	Codigo       string `json:"codigo"`
	PlanID       *int64 `json:"plan_id"`
	Plan         *Ref   `json:"plan"`
	PlanNombre   string `json:"plan_nombre"`
	RouterID     *int64 `json:"router_id"`
	Router       *Ref   `json:"router"`
	RouterNombre string `json:"router_nombre"`
	ZonaID       *int64 `json:"zona_id"`
	Zona         *Ref   `json:"zona"`
	ZonaNombre   string `json:"zona_nombre"`
	// IP: Argos 1.4.0 la manda como ipv4 (la IPv4 vigente); ip queda por compatibilidad.
	IP   string `json:"ip"`
	IPv4 string `json:"ipv4"`
	// Direccion: dirección de instalación del servicio (puede faltar: entonces aplica la del
	// cliente).
	Direccion    string `json:"direccion"`
	Estado       string `json:"estado"`
	UserPPP      string `json:"user_ppp"`
	TipoConexion string `json:"tipo_conexion"`
}

// IPVigente: ipv4 (Argos 1.4.0) o ip.
func (s ServicioArgos) IPVigente() string {
	if s.IPv4 != "" {
		return s.IPv4
	}
	return s.IP
}

func idDe(id *int64, r *Ref) *int64 {
	if id != nil {
		return id
	}
	if r != nil {
		return r.ID
	}
	return nil
}

func nombreDe(nombre string, r *Ref) string {
	if nombre != "" || r == nil {
		return nombre
	}
	return r.Nombre
}

// EstadoCuentaArgos es el resumen de GET /clientes/:id/estado-cuenta. Iris solo usa el saldo y
// las facturas vencidas (§2.1); facturas_vencidas puede ser un número o la lista.
type EstadoCuentaArgos struct {
	Saldo            *Decimal        `json:"saldo"`
	Moneda           string          `json:"moneda"`
	FacturasVencidas json.RawMessage `json:"facturas_vencidas"`
	MontoVencido     *Decimal        `json:"monto_vencido"`
	TotalVencido     *Decimal        `json:"total_vencido"`
}

// CantidadVencidas cuenta las facturas vencidas (número o lista). nil si Argos no lo dijo.
func (e EstadoCuentaArgos) CantidadVencidas() *int {
	crudo := bytes.TrimSpace(e.FacturasVencidas)
	if len(crudo) == 0 || string(crudo) == "null" {
		return nil
	}
	var n Numero
	if json.Unmarshal(crudo, &n) == nil {
		if f := n.Float(); f != nil {
			v := int(*f)
			return &v
		}
	}
	var lista []json.RawMessage
	if json.Unmarshal(crudo, &lista) == nil {
		v := len(lista)
		return &v
	}
	return nil
}

// ResolucionArgos es la respuesta de resolver una IP (A8 o GET /ip/{ip}).
type ResolucionArgos struct {
	ClienteID  *int64 `json:"cliente_id"`
	ServicioID *int64 `json:"servicio_id"`
	EmpresaID  *int64 `json:"empresa_id"`
	Cliente    *Ref   `json:"cliente"`
	Servicio   *struct {
		ID        *int64 `json:"id"`
		ClienteID *int64 `json:"cliente_id"`
		EmpresaID *int64 `json:"empresa_id"`
	} `json:"servicio"`
}

// Ids devuelve cliente, servicio y empresa, vengan planos o anidados.
func (r ResolucionArgos) Ids() (cliente, servicio, empresa *int64) {
	cliente, servicio, empresa = r.ClienteID, r.ServicioID, r.EmpresaID
	if cliente == nil && r.Cliente != nil {
		cliente = r.Cliente.ID
	}
	if r.Servicio != nil {
		if servicio == nil {
			servicio = r.Servicio.ID
		}
		if cliente == nil {
			cliente = r.Servicio.ClienteID
		}
		if empresa == nil {
			empresa = r.Servicio.EmpresaID
		}
	}
	return cliente, servicio, empresa
}

// PlanArgos es un plan (GET /planes).
type PlanArgos struct {
	ID        int64    `json:"id"`
	EmpresaID int64    `json:"empresa_id"`
	Nombre    string   `json:"nombre"`
	Precio    *Decimal `json:"precio"`
	Estado    string   `json:"estado"`
}

// ZonaArgos es una zona (GET /zonas).
type ZonaArgos struct {
	ID                int64  `json:"id"`
	EmpresaID         int64  `json:"empresa_id"`
	EmpresaSugeridaID int64  `json:"empresa_sugerida_id"`
	Nombre            string `json:"nombre"`
}

// Empresa de la zona en la instantánea: empresa_id o, como devuelve Argos 1.3.0 (zonas y routers
// no son de una empresa; multiempresa.md), empresa_sugerida_id. nil = sin empresa: la usan todas.
func (z ZonaArgos) Empresa() *int64 { return primeraEmpresa(z.EmpresaID, z.EmpresaSugeridaID) }

// RouterArgos es un router (GET /routers).
type RouterArgos struct {
	conCoordenadas
	ID                int64  `json:"id"`
	EmpresaID         int64  `json:"empresa_id"`
	EmpresaSugeridaID int64  `json:"empresa_sugerida_id"`
	Nombre            string `json:"nombre"`
	ZonaID            *int64 `json:"zona_id"`
	Zona              *Ref   `json:"zona"`
}

// Empresa del router en la instantánea: empresa_id o empresa_sugerida_id (Argos 1.3.0). nil = sin
// empresa sugerida: en Argos lo usan todas las empresas.
func (r RouterArgos) Empresa() *int64 { return primeraEmpresa(r.EmpresaID, r.EmpresaSugeridaID) }

func primeraEmpresa(ids ...int64) *int64 {
	for _, id := range ids {
		if id > 0 {
			return &id
		}
	}
	return nil
}

// Numero acepta un número JSON o un texto numérico.
type Numero = conector.Numero

// Decimal es un monto: se guarda como texto para no perder precisión (numeric(14,2)).
type Decimal string

// UnmarshalJSON acepta número o texto numérico.
func (d *Decimal) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "null" {
		s = ""
	}
	if s != "" {
		if _, err := strconv.ParseFloat(s, 64); err != nil {
			return err
		}
	}
	*d = Decimal(s)
	return nil
}

// Texto devuelve el monto o nil.
func (d *Decimal) Texto() *string {
	if d == nil || *d == "" {
		return nil
	}
	s := string(*d)
	return &s
}

// EmpresasDelCliente: las empresas de un cliente de Argos = las de sus servicios (y empresa_id o
// empresas[] del cliente si vinieran), sin repetir y ordenadas. Vacío = sin servicios.
func EmpresasDelCliente(c ClienteArgos, servicios []ServicioArgos) []int64 {
	vistas := map[int64]bool{}
	salida := []int64{}
	agregar := func(e int64) {
		if e > 0 && !vistas[e] {
			vistas[e] = true
			salida = append(salida, e)
		}
	}
	agregar(c.EmpresaID)
	for _, e := range c.Empresas {
		agregar(e)
	}
	for _, s := range servicios {
		if s.ClienteID == 0 || s.ClienteID == c.ID {
			agregar(s.EmpresaID)
		}
	}
	slices.Sort(salida)
	return salida
}
