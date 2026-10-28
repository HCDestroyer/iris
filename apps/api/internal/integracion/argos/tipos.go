package argos

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
)

// Tipos de la API de Argos tal como Iris los lee.
//
// Supuesto (a confirmar con el equipo de Argos): los recursos son JSON con los nombres del
// contrato en español. Para no depender del estilo (snake_case o camelCase) ni de si el recurso
// viene envuelto en { data }, la respuesta se normaliza antes de leerla: todas las claves pasan
// a snake_case, un objeto { data: {...} } se desenvuelve y una lista puede ser un arreglo o
// { data: [...], nextCursor }. Los números pueden venir como número o como texto (numeric de
// Postgres) y las coordenadas como lat/lon, latitud/longitud o { coordenadas: { lat, lon } }.

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
	return lat.Float(), lon.Float()
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
	ID        int64  `json:"id"`
	EmpresaID int64  `json:"empresa_id"`
	Codigo    string `json:"codigo"`
	Nombre    string `json:"nombre"`
	Telefono  string `json:"telefono"`
	Direccion string `json:"direccion"`
	ZonaID    *int64 `json:"zona_id"`
	Zona      *Ref   `json:"zona"`
	Estado    string `json:"estado"`
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
	IP           string `json:"ip"`
	Direccion    string `json:"direccion"`
	Estado       string `json:"estado"`
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
	ID        int64  `json:"id"`
	EmpresaID int64  `json:"empresa_id"`
	Nombre    string `json:"nombre"`
}

// RouterArgos es un router (GET /routers).
type RouterArgos struct {
	conCoordenadas
	ID        int64  `json:"id"`
	EmpresaID int64  `json:"empresa_id"`
	Nombre    string `json:"nombre"`
	ZonaID    *int64 `json:"zona_id"`
	Zona      *Ref   `json:"zona"`
}

// PeticionCanje es el cuerpo del canje en Argos (§1.3). Supuesto: mismos campos que el canje
// Argos ↔ Horus, en snake_case; el receptor de los webhooks (Iris) genera el secreto.
type PeticionCanje struct {
	Codigo         string `json:"codigo"`
	Sistema        string `json:"sistema"`
	URL            string `json:"url"`
	WebhookURL     string `json:"webhook_url"`
	WebhookSecreto string `json:"webhook_secreto"`
}

// RespuestaCanje es lo que Argos devuelve al canjear: el token atk_… del usuario técnico
// «Iris», y opcionalmente el id de la conexión y las empresas que cubre el token.
type RespuestaCanje struct {
	Token      string            `json:"token"`
	ConexionID json.RawMessage   `json:"conexion_id"`
	Empresas   []json.RawMessage `json:"empresas"`
}

// IDConexion devuelve el id de la conexión como texto (número o texto en Argos).
func (r RespuestaCanje) IDConexion() *string {
	crudo := strings.Trim(strings.TrimSpace(string(r.ConexionID)), `"`)
	if crudo == "" || crudo == "null" {
		return nil
	}
	return &crudo
}

// IDsEmpresas acepta [1, 2] o [{ id: 1 }, …].
func (r RespuestaCanje) IDsEmpresas() []int64 {
	return idsEmpresas(r.Empresas)
}

func idsEmpresas(crudos []json.RawMessage) []int64 {
	salida := []int64{}
	for _, c := range crudos {
		var n int64
		if json.Unmarshal(c, &n) == nil {
			salida = append(salida, n)
			continue
		}
		var r Ref
		if json.Unmarshal(c, &r) == nil && r.ID != nil {
			salida = append(salida, *r.ID)
		}
	}
	return salida
}

// Numero acepta un número JSON o un texto numérico.
type Numero struct {
	valor *float64
}

// UnmarshalJSON implementa json.Unmarshaler.
func (n *Numero) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		n.valor = nil
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	n.valor = &f
	return nil
}

// Float devuelve el valor (nil si no vino).
func (n *Numero) Float() *float64 {
	if n == nil {
		return nil
	}
	return n.valor
}

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

// Decodificación tolerante ---------------------------------------------------------------

// decodificar normaliza el JSON (claves a snake_case) y lo lee en destino.
func decodificar(crudo []byte, destino any) error {
	dec := json.NewDecoder(bytes.NewReader(crudo))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return err
	}
	b, err := json.Marshal(normalizar(v))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, destino)
}

func normalizar(v any) any {
	switch x := v.(type) {
	case map[string]any:
		salida := make(map[string]any, len(x))
		for k, interno := range x {
			salida[auditoria.SnakeCase(k)] = normalizar(interno)
		}
		return salida
	case []any:
		for i := range x {
			x[i] = normalizar(x[i])
		}
		return x
	}
	return v
}

// envoltura lee un recurso que puede venir solo o como { data: {...} }.
type envoltura struct {
	destino any
}

// UnmarshalJSON implementa json.Unmarshaler.
func (e *envoltura) UnmarshalJSON(b []byte) error {
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) == nil {
		if data, ok := obj["data"]; ok && len(obj) <= 3 && bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
			return json.Unmarshal(data, e.destino)
		}
	}
	return json.Unmarshal(b, e.destino)
}

// listaArgos lee una lista: un arreglo o { data: [...], next_cursor }.
type listaArgos[T any] struct {
	data []T
	next string
}

// UnmarshalJSON implementa json.Unmarshaler.
func (l *listaArgos[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.HasPrefix(b, []byte("[")) {
		return json.Unmarshal(b, &l.data)
	}
	var obj struct {
		Data       []T     `json:"data"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	l.data = obj.Data
	if obj.NextCursor != nil {
		l.next = *obj.NextCursor
	}
	return nil
}
