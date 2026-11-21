package inventario

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// vinculo es el usuario de Argos del técnico (su ficha de agenda).
type vinculo struct {
	IDExterno string
	Correo    string
}

// SinVincular: el técnico no tiene su ficha vinculada a un usuario de Argos.
func SinVincular() *nucleo.Problema {
	return nucleo.ReglaNegocio("tecnico_sin_vincular",
		"Tu ficha de técnico no está vinculada a tu usuario de Argos: pide a un administrador que la vincule en Agenda")
}

// vinculoDe lee el vínculo del usuario (422 tecnico_sin_vincular si no hay).
func vinculoDe(ctx context.Context, q argosbd.DBTX, usuarioID int64) (vinculo, error) {
	v, err := argosbd.New(q).VinculoDeUsuario(ctx, usuarioID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && v.IDExterno == "") {
		return vinculo{}, SinVincular()
	}
	if err != nil {
		return vinculo{}, err
	}
	return vinculo{IDExterno: v.IDExterno, Correo: v.Correo}, nil
}

func usuarioDe(ctx context.Context) (int64, error) {
	a := contexto.ActorDe(ctx)
	if !a.EsUsuario() {
		return 0, nucleo.Prohibido("Es la bodega de un técnico: entra con tu usuario")
	}
	return a.ID, nil
}

// Mi bodega ---------------------------------------------------------------------------------

// Bodega de Argos del técnico.
type Bodega struct {
	ID        int64  `json:"id"`
	Nombre    string `json:"nombre"`
	EmpresaID int64  `json:"empresaId"`
}

// ExistenciaBodega es una línea de existencias (o de una orden de traslado).
type ExistenciaBodega struct {
	ProductoID  int64   `json:"productoId"`
	Descripcion string  `json:"descripcion"`
	Cantidad    float64 `json:"cantidad"`
	Unidad      string  `json:"unidad"`
	Modalidad   string  `json:"modalidad" enum:"comodato,consumible_gasto,consumible_cobro," doc:"comodato (va como equipo, con serie), consumible_gasto o consumible_cobro (se cobra al cliente al consumirlo)"`
}

// UnidadBodega es una unidad con serie en la bodega (disponible o en reparación).
type UnidadBodega struct {
	ID          int64  `json:"id"`
	ProductoID  int64  `json:"productoId"`
	Descripcion string `json:"descripcion"`
	Serie       string `json:"serie"`
	MAC         string `json:"mac"`
	Estado      string `json:"estado" doc:"Como lo manda Argos (disponible, en_reparacion…)"`
}

// MovimientoBodega es un movimiento del kardex.
type MovimientoBodega struct {
	ID          int64      `json:"id"`
	Tipo        string     `json:"tipo" doc:"Como lo manda Argos (consumo, traslado_entrada, comodato…)"`
	ProductoID  int64      `json:"productoId"`
	Descripcion string     `json:"descripcion"`
	Cantidad    float64    `json:"cantidad"`
	OcurridoEn  *time.Time `json:"ocurridoEn,omitempty"`
	Referencia  string     `json:"referencia" doc:"Origen del movimiento (p. ej. la orden de Iris)"`
}

// JefeBodega es el jefe de una bodega de cuadrilla.
type JefeBodega struct {
	Nombre string `json:"nombre"`
	SoyYo  bool   `json:"soyYo" doc:"El jefe es el usuario que pregunta (recibe y firma los traslados)"`
}

// BodegaDeTecnico es una de las bodegas del técnico en la empresa: la de su cuadrilla (lo que va en
// el vehículo) o la suya (contrato 0.11.0 §3.9). «Mi bodega» tiene una pestaña por cada una.
type BodegaDeTecnico struct {
	Bodega              Bodega             `json:"bodega"`
	Cuadrilla           bool               `json:"cuadrilla" doc:"Es la bodega compartida de su cuadrilla"`
	CuadrillaID         *int64             `json:"cuadrillaId,omitempty" doc:"La cuadrilla en Iris"`
	Jefe                *JefeBodega        `json:"jefe,omitempty" doc:"Solo en la de cuadrilla; null = sin jefe (los traslados esperan)"`
	Existencias         []ExistenciaBodega `json:"existencias"`
	Unidades            []UnidadBodega     `json:"unidades"`
	TrasladosPendientes []int64            `json:"trasladosPendientes"`
	Kardex              []MovimientoBodega `json:"kardex" doc:"Últimos 50 movimientos"`
}

// MiBodegaRespuesta es la bodega del técnico en la empresa activa.
type MiBodegaRespuesta struct {
	Bodega              Bodega             `json:"bodega"`
	Existencias         []ExistenciaBodega `json:"existencias"`
	Unidades            []UnidadBodega     `json:"unidades"`
	TrasladosPendientes []int64            `json:"trasladosPendientes" doc:"Ids de las órdenes de traslado por recibir según Argos (detalle en GET /campo/inventario/traslados)"`
	Kardex              []MovimientoBodega `json:"kardex" doc:"Últimos 50 movimientos"`
	Bodegas             []BodegaDeTecnico  `json:"bodegas" doc:"Primero la de su cuadrilla (si Argos ≥ 0.11.0 la tiene) y después la suya; con un Argos anterior, solo la suya (la de arriba)"`
	LeidoEn             time.Time          `json:"leidoEn" doc:"Cuándo se leyó de Argos (caché de 1 min por técnico y empresa)"`
}

// lo que Iris lee de mi-bodega (tolerante: snake_case o camelCase, números como texto). Desde 0.11.0,
// con empresa, Argos manda arriba la bodega principal y en bodegas[] cada una con su contenido.
type miBodegaArgos struct {
	contenidoArgos
	Bodegas []json.RawMessage `json:"bodegas"`
}

// contenidoArgos es una bodega con lo suyo (arriba o en bodegas[]).
type contenidoArgos struct {
	ID        int64  `json:"id"`
	EmpresaID int64  `json:"empresa_id"`
	Nombre    string `json:"nombre"`
	Bodega    *struct {
		ID              int64           `json:"id"`
		Nombre          string          `json:"nombre"`
		EmpresaID       int64           `json:"empresa_id"`
		CuadrillaIDIris argos.IDExterno `json:"cuadrilla_id_iris"`
	} `json:"bodega"`
	Cuadrilla bool `json:"cuadrilla"`
	Jefe      *struct {
		IDExterno argos.IDExterno `json:"id_externo"`
		Nombre    string          `json:"nombre"`
	} `json:"jefe"`
	Existencias []argos.ExistenciaArgos `json:"existencias"`
	Unidades    []struct {
		ID          int64      `json:"id"`
		ProductoID  int64      `json:"producto_id"`
		Producto    *argos.Ref `json:"producto"`
		Descripcion string     `json:"descripcion"`
		Serie       string     `json:"serie"`
		MAC         string     `json:"mac"`
		Estado      string     `json:"estado"`
	} `json:"unidades"`
	TrasladosPendientes []struct {
		ID int64 `json:"id"`
	} `json:"traslados_pendientes"`
	Kardex []struct {
		ID          int64           `json:"id"`
		Tipo        string          `json:"tipo"`
		ProductoID  int64           `json:"producto_id"`
		Producto    *argos.Ref      `json:"producto"`
		Descripcion string          `json:"descripcion"`
		Cantidad    conector.Numero `json:"cantidad"`
		CreatedAt   *time.Time      `json:"created_at"`
		Fecha       *time.Time      `json:"fecha"`
		Referencia  string          `json:"referencia"`
		OrigenID    json.RawMessage `json:"origen_id"`
	} `json:"kardex"`
}

// cuadrillaIris: la cuadrilla de Iris de una bodega de cuadrilla (nil si no es o no se entiende).
func (b contenidoArgos) cuadrillaIris() *int64 {
	if !b.Cuadrilla || b.Bodega == nil {
		return nil
	}
	id, err := strconv.ParseInt(strings.TrimSpace(string(b.Bodega.CuadrillaIDIris)), 10, 64)
	if err != nil || id <= 0 {
		return nil
	}
	return &id
}

// bodegas: las del técnico; con un Argos anterior a 0.11.0 (sin bodegas[]), solo la de arriba.
func (b miBodegaArgos) bodegas(idExterno string) []BodegaDeTecnico {
	salida := []BodegaDeTecnico{}
	for _, crudo := range b.Bodegas {
		var c contenidoArgos
		if conector.Decodificar(crudo, &c) != nil || c.Bodega == nil {
			continue
		}
		salida = append(salida, c.aBodega(idExterno))
	}
	if len(salida) == 0 && b.Bodega != nil {
		salida = append(salida, b.aBodega(idExterno))
	}
	return salida
}

func (b contenidoArgos) aBodega(idExterno string) BodegaDeTecnico {
	r := b.aRespuesta(time.Time{})
	d := BodegaDeTecnico{Bodega: r.Bodega, Cuadrilla: b.Cuadrilla, CuadrillaID: b.cuadrillaIris(), Existencias: r.Existencias,
		Unidades: r.Unidades, TrasladosPendientes: r.TrasladosPendientes, Kardex: r.Kardex}
	if b.Cuadrilla && b.Jefe != nil {
		// Del jefe solo el nombre y si es quien pregunta (su id de Argos no sale de Iris).
		d.Jefe = &JefeBodega{Nombre: b.Jefe.Nombre, SoyYo: idExterno != "" && string(b.Jefe.IDExterno) == idExterno}
	}
	return d
}

func (b contenidoArgos) aRespuesta(leido time.Time) MiBodegaRespuesta {
	r := MiBodegaRespuesta{Existencias: []ExistenciaBodega{}, Unidades: []UnidadBodega{}, TrasladosPendientes: []int64{},
		Kardex: []MovimientoBodega{}, Bodegas: []BodegaDeTecnico{}, LeidoEn: leido}
	if b.Bodega != nil {
		r.Bodega = Bodega{ID: b.Bodega.ID, Nombre: b.Bodega.Nombre, EmpresaID: b.Bodega.EmpresaID}
	}
	for _, e := range b.Existencias {
		cant, _ := e.Cantidad.Float64()
		desc := e.Descripcion
		if desc == "" {
			desc = e.Nombre
		}
		if desc == "" && e.Producto != nil {
			desc = e.Producto.Nombre
		}
		r.Existencias = append(r.Existencias, ExistenciaBodega{ProductoID: e.IDProducto(), Descripcion: desc, Cantidad: cant,
			Unidad: e.Unidad, Modalidad: e.ModalidadValida()})
	}
	for _, u := range b.Unidades {
		desc := u.Descripcion
		if desc == "" && u.Producto != nil {
			desc = u.Producto.Nombre
		}
		producto := u.ProductoID
		if producto == 0 && u.Producto != nil && u.Producto.ID != nil {
			producto = *u.Producto.ID
		}
		r.Unidades = append(r.Unidades, UnidadBodega{ID: u.ID, ProductoID: producto, Descripcion: desc, Serie: u.Serie, MAC: u.MAC, Estado: u.Estado})
	}
	for _, tr := range b.TrasladosPendientes {
		if tr.ID > 0 {
			r.TrasladosPendientes = append(r.TrasladosPendientes, tr.ID)
		}
	}
	for _, k := range b.Kardex {
		desc := k.Descripcion
		if desc == "" && k.Producto != nil {
			desc = k.Producto.Nombre
		}
		producto := k.ProductoID
		if producto == 0 && k.Producto != nil && k.Producto.ID != nil {
			producto = *k.Producto.ID
		}
		var cant float64
		if f := k.Cantidad.Float(); f != nil {
			cant = *f
		}
		cuando := k.Fecha
		if cuando == nil {
			cuando = k.CreatedAt
		}
		r.Kardex = append(r.Kardex, MovimientoBodega{ID: k.ID, Tipo: k.Tipo, ProductoID: producto, Descripcion: desc,
			Cantidad: cant, OcurridoEn: cuando, Referencia: k.Referencia})
	}
	return r
}

type claveCache struct{ usuario, empresa int64 }

type bodegaEnCache struct {
	r      MiBodegaRespuesta
	expira time.Time
}

// MiBodega: permiso, vínculo y conector ≥ 1.6.0 en un caso corto; GET /inventario/mi-bodega fuera de
// transacción (con caché de CacheBodega por técnico y empresa); la lectura queda auditada.
type MiBodega struct {
	m      *Modulo
	previo *auditoria.Caso[struct{}, previoBodega]
	final  *auditoria.Caso[finalBodega, MiBodegaRespuesta]
}

type previoBodega struct {
	usuario int64
	vinculo vinculo
	cli     *argos.Cliente
}

type finalBodega struct {
	Cache bool `json:"cache"`
	r     MiBodegaRespuesta
	err   error
	// cuadrillas y personales: las bodegas que Argos mandó, para validar el bodega_id del lote.
	cuadrillas []argosbd.GuardarBodegaCuadrillaParams
	personales []argosbd.GuardarBodegaTecnicoParams
}

// bodegasPersonales: las bodegas personales del técnico en la respuesta (con un Argos anterior, la de
// arriba), con su empresa (M-268-1).
func (b miBodegaArgos) bodegasPersonales(usuario int64, ahora time.Time) []argosbd.GuardarBodegaTecnicoParams {
	var salida []argosbd.GuardarBodegaTecnicoParams
	agregar := func(c contenidoArgos) {
		if c.Bodega != nil && !c.Cuadrilla && c.Bodega.ID > 0 && c.Bodega.EmpresaID > 0 {
			salida = append(salida, argosbd.GuardarBodegaTecnicoParams{BodegaID: c.Bodega.ID, EmpresaID: c.Bodega.EmpresaID, UsuarioID: usuario, VistoEn: ahora})
		}
	}
	for _, crudo := range b.Bodegas {
		var c contenidoArgos
		if conector.Decodificar(crudo, &c) == nil {
			agregar(c)
		}
	}
	if len(b.Bodegas) == 0 {
		agregar(b.contenidoArgos)
	}
	return salida
}

// bodegasCuadrilla: las bodegas de cuadrilla de la respuesta, para guardarlas.
func (b miBodegaArgos) bodegasCuadrilla(ahora time.Time) []argosbd.GuardarBodegaCuadrillaParams {
	var salida []argosbd.GuardarBodegaCuadrillaParams
	for _, crudo := range b.Bodegas {
		var c contenidoArgos
		if conector.Decodificar(crudo, &c) != nil || c.Bodega == nil || c.Bodega.ID <= 0 || c.Bodega.EmpresaID <= 0 {
			continue
		}
		if id := c.cuadrillaIris(); id != nil {
			nombre := c.Bodega.Nombre
			salida = append(salida, argosbd.GuardarBodegaCuadrillaParams{BodegaID: c.Bodega.ID, EmpresaID: c.Bodega.EmpresaID,
				CuadrillaID: *id, Nombre: recortar(nombre, 200), VistoEn: ahora})
		}
	}
	return salida
}

func recortar(s string, n int) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return &s
}

func nuevoMiBodega(m *Modulo) *MiBodega {
	b := &MiBodega{m: m}
	b.previo = auditoria.Nuevo(auditoria.Caso[struct{}, previoBodega]{
		Accion: "campo.ver_bodega", Permiso: nucleo.PermCampoVerBodega, AuditarExito: soloLecturas[previoBodega],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (previoBodega, error) {
			usuario, err := usuarioDe(ctx)
			if err != nil {
				return previoBodega{}, err
			}
			v, err := vinculoDe(ctx, t, usuario)
			if err != nil {
				return previoBodega{}, err
			}
			cli, err := m.clienteTecnico(ctx, t)
			return previoBodega{usuario: usuario, vinculo: v, cli: cli}, err
		},
	})
	b.final = auditoria.Nuevo(auditoria.Caso[finalBodega, MiBodegaRespuesta]{
		Accion: "campo.ver_bodega", Permiso: nucleo.PermCampoVerBodega,
		AuditarExito: func(MiBodegaRespuesta) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in finalBodega) (MiBodegaRespuesta, error) {
			if in.err != nil {
				return MiBodegaRespuesta{}, in.err
			}
			for _, c := range in.cuadrillas {
				if err := argosbd.New(t).GuardarBodegaCuadrilla(ctx, c); err != nil {
					return MiBodegaRespuesta{}, err
				}
			}
			for _, p := range in.personales {
				if err := argosbd.New(t).GuardarBodegaTecnico(ctx, p); err != nil {
					return MiBodegaRespuesta{}, err
				}
			}
			return in.r, nil
		},
	})
	return b
}

// clienteTecnico: el cliente del conector Argos activo, solo con Argos ≥ 1.6.0.
func (m *Modulo) clienteTecnico(ctx context.Context, q conector.DBTX) (*argos.Cliente, error) {
	if m.cfg.Argos == nil {
		return nil, m.sinArgosFn()
	}
	cli, err := m.cfg.Argos.ClienteActivo(ctx, q)
	if err != nil {
		return nil, err
	}
	ok, err := m.cfg.Argos.InventarioPorTecnico(ctx, q)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, argos.SinInventarioTecnico()
	}
	return cli, nil
}

// Ejecutar lee la bodega del técnico en la empresa activa.
func (b *MiBodega) Ejecutar(ctx context.Context) (MiBodegaRespuesta, error) {
	p, err := b.previo.Ejecutar(ctx, struct{}{})
	if err != nil {
		return MiBodegaRespuesta{}, err
	}
	empresa := contexto.EmpresaDe(ctx)
	clave := claveCache{p.usuario, empresa}
	ahora := b.m.ahora()
	b.m.cacheMu.Lock()
	c, ok := b.m.cache[clave]
	b.m.cacheMu.Unlock()
	if ok && ahora.Before(c.expira) {
		return b.final.Ejecutar(ctx, finalBodega{Cache: true, r: c.r})
	}
	var crudo miBodegaArgos
	err = p.cli.MiBodega(ctx, argos.RefTecnico{IDExterno: p.vinculo.IDExterno}, empresa, &conector.Recurso{Destino: &crudo})
	f := finalBodega{}
	switch {
	case err == nil && crudo.Bodega == nil && len(crudo.Bodegas) > 0:
		f.err = nucleo.ReglaNegocio("empresa_requerida", "Tienes bodega en varias empresas de Argos: elige la empresa activa")
	case err == nil:
		f.r = crudo.aRespuesta(ahora)
		f.r.Bodegas = crudo.bodegas(p.vinculo.IDExterno)
		f.cuadrillas = crudo.bodegasCuadrilla(ahora)
		f.personales = crudo.bodegasPersonales(p.usuario, ahora)
		b.m.cacheMu.Lock()
		b.m.cache[clave] = bodegaEnCache{r: f.r, expira: ahora.Add(b.m.cfg.CacheBodega)}
		b.m.cacheMu.Unlock()
	default:
		f.err = problemaInventario(err)
	}
	return b.final.Ejecutar(ctx, f)
}

// problemaInventario traduce los códigos de §3.3 que ve el técnico (422 con el código de Argos);
// lo demás, como cualquier error de Argos.
func problemaInventario(err error) error {
	var er *argos.ErrorArgos
	if errors.As(err, &er) && er.Status >= 400 && er.Status < 500 {
		switch codigo := pendientes.TipoDe(er); codigo {
		case "tecnico_sin_bodega", "tecnico_no_encontrado", "tecnico_ambiguo", "empresa_requerida":
			return nucleo.ReglaNegocio(codigo, "Argos: "+er.Detalle)
		}
	}
	return argos.Problema(err)
}

// Traslados ---------------------------------------------------------------------------------

// EntradaTraslados filtra las órdenes de traslado del técnico.
type EntradaTraslados struct {
	Estado string `query:"estado" enum:"enviado,recibido,cancelado," doc:"Sin estado: todas (las más recientes primero)"`
	Cursor string `query:"cursor" maxLength:"100"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

// LineaTraslado es una línea de la orden de traslado.
type LineaTraslado struct {
	LineaID    int64    `json:"lineaId" doc:"Id de la línea en Argos (para lineas[] al recibir con faltantes); 0 si Argos no lo mandó"`
	ProductoID int64    `json:"productoId"`
	Cantidad   float64  `json:"cantidad"`
	Series     []string `json:"series"`
}

// EstadoRecepcion es cómo va la recepción que mandó el técnico (la operación traslado.recibir).
type EstadoRecepcion struct {
	TrasladoID  int64     `json:"trasladoId"`
	PendienteID int64     `json:"pendienteId"`
	Estado      string    `json:"estado" enum:"esperando_firma,enviando,recibida,rechazada,agotada" doc:"esperando_firma (la firma no termina de subir), enviando (en la cola hacia Argos), recibida (Argos la aceptó o ya estaba recibida), rechazada (error de negocio: codigo) o agotada (Argos no respondió; supervisor)"`
	Codigo      *string   `json:"codigo,omitempty" doc:"traslado_cancelado, traslado_de_otro_tecnico, lineas_invalidas, firma_invalida, tecnico_*…"`
	Detalle     *string   `json:"detalle,omitempty"`
	Actualizado time.Time `json:"actualizadoEn"`
}

// Traslado es una orden de traslado hacia la bodega del técnico.
type Traslado struct {
	ID              int64            `json:"id" doc:"Id en Argos"`
	EmpresaID       *int64           `json:"empresaId,omitempty"`
	BodegaDestinoID *int64           `json:"bodegaDestinoId,omitempty"`
	Estado          string           `json:"estado" enum:"enviado,recibido,cancelado" doc:"Según el último webhook de Argos"`
	Lineas          []LineaTraslado  `json:"lineas"`
	EnviadoEn       *time.Time       `json:"enviadoEn,omitempty"`
	RecibidoEn      *time.Time       `json:"recibidoEn,omitempty"`
	CanceladoEn     *time.Time       `json:"canceladoEn,omitempty"`
	Motivo          *string          `json:"motivo,omitempty" doc:"Motivo de la cancelación"`
	Recepcion       *EstadoRecepcion `json:"recepcion,omitempty" doc:"La recepción que mandó el técnico desde Iris, si la hay"`
	CuadrillaID     *int64           `json:"cuadrillaId,omitempty" doc:"Va a la bodega de esta cuadrilla (contrato 0.11.0 §3.9); lo recibe y firma su jefe"`
	PuedeRecibir    bool             `json:"puedeRecibir" doc:"Este usuario lo recibe y firma: es su traslado o es el jefe de la cuadrilla. Los demás miembros lo ven en camino; si mandan la recepción, 403 solo_jefe_recibe"`
}

// PaginaTraslados es la lista paginada.
type PaginaTraslados struct {
	Data       []Traslado `json:"data"`
	NextCursor *string    `json:"nextCursor" nullable:"true"`
}

func nuevoTraslados(m *Modulo) *auditoria.Caso[EntradaTraslados, PaginaTraslados] {
	return auditoria.Nuevo(auditoria.Caso[EntradaTraslados, PaginaTraslados]{
		Accion: "campo.ver_traslados", Permiso: nucleo.PermCampoVerBodega, AuditarExito: soloLecturas[PaginaTraslados],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTraslados) (PaginaTraslados, error) {
			usuario, err := usuarioDe(ctx)
			if err != nil {
				return PaginaTraslados{}, err
			}
			v, err := vinculoDe(ctx, t, usuario)
			if err != nil {
				return PaginaTraslados{}, err
			}
			var antes int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &antes); err != nil {
				return PaginaTraslados{}, err
			}
			limite := in.Limit
			if limite <= 0 {
				limite = 50
			}
			var estado *string
			if in.Estado != "" {
				estado = &in.Estado
			}
			// Los de su cuadrilla actual también (0.11.0 §3.9): el jefe los recibe; los demás los ven.
			var cuadrilla, jefe *int64
			c, err := argosbd.New(t).CuadrillaDelUsuario(ctx, usuario)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
			case err != nil:
				return PaginaTraslados{}, err
			default:
				cuadrilla, jefe = &c.CuadrillaID, c.JefeUsuarioID
			}
			filas, err := argosbd.New(t).TrasladosDeTecnico(ctx, argosbd.TrasladosDeTecnicoParams{
				IDExterno: &v.IDExterno, Correo: v.Correo, EmpresaID: contexto.EmpresaDe(ctx), Estado: estado, AntesDe: antes,
				Limite:      int32(limite + 1), //nolint:gosec // ≤ 101
				CuadrillaID: cuadrilla,
			})
			if err != nil {
				return PaginaTraslados{}, err
			}
			r := PaginaTraslados{Data: []Traslado{}}
			for i, f := range filas {
				if i == limite {
					c := nucleo.CodificarCursor(filas[i-1].ID)
					r.NextCursor = &c
					break
				}
				tr := aTraslado(f)
				tr.PuedeRecibir = f.CuadrillaID == nil || (jefe != nil && *jefe == usuario)
				if p, err := pendientes.PorClave(ctx, t, ClaveRecepcion(f.ID)); err == nil {
					e := estadoRecepcion(p)
					tr.Recepcion = &e
				} else if !errors.Is(err, pgx.ErrNoRows) {
					return PaginaTraslados{}, err
				}
				r.Data = append(r.Data, tr)
			}
			return r, nil
		},
	})
}

func aTraslado(f argosbd.IntegracionArgosTraslado) Traslado {
	t := Traslado{ID: f.ID, EmpresaID: f.EmpresaID, BodegaDestinoID: f.BodegaDestinoID, Estado: f.Estado, Lineas: []LineaTraslado{},
		EnviadoEn: f.EnviadoEn, RecibidoEn: f.RecibidoEn, CanceladoEn: f.CanceladoEn, Motivo: f.Motivo, CuadrillaID: f.CuadrillaID}
	var lineas []struct {
		ID         json.RawMessage `json:"id"`
		LineaID    json.RawMessage `json:"linea_id"`
		ProductoID json.RawMessage `json:"producto_id"`
		Cantidad   conector.Numero `json:"cantidad"`
		Series     []string        `json:"series"`
	}
	_ = conector.Decodificar(f.Lineas, &lineas)
	for _, l := range lineas {
		linea := entero(l.LineaID)
		if linea == 0 {
			linea = entero(l.ID)
		}
		var cant float64
		if c := l.Cantidad.Float(); c != nil {
			cant = *c
		}
		series := l.Series
		if series == nil {
			series = []string{}
		}
		t.Lineas = append(t.Lineas, LineaTraslado{LineaID: linea, ProductoID: entero(l.ProductoID), Cantidad: cant, Series: series})
	}
	return t
}

func entero(v json.RawMessage) int64 {
	s := string(v)
	if u, err := strconv.Unquote(s); err == nil {
		s = u
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// estadoRecepcion traduce el pendiente al estado que ve el técnico.
func estadoRecepcion(p pendientes.Registro) EstadoRecepcion {
	id, _ := strconv.ParseInt(p.EntidadID, 10, 64)
	e := EstadoRecepcion{TrasladoID: id, PendienteID: p.ID, Actualizado: p.ActualizadoEn}
	switch p.Estado {
	case pendientes.EstadoBloqueado:
		e.Estado = EsperandoFirma
	case pendientes.EstadoHecho, pendientes.EstadoResueltoManual:
		e.Estado = "recibida"
	case pendientes.EstadoFallidoNegocio:
		e.Estado, e.Codigo, e.Detalle = "rechazada", p.CodigoError, p.UltimoError
	case pendientes.EstadoAgotado:
		e.Estado, e.Detalle = "agotada", p.UltimoError
	default:
		e.Estado = "enviando"
	}
	return e
}

// Recepciones devuelve el estado de las recepciones del técnico de los últimos 7 días (para
// GET /campo/cambios: los rechazos de negocio llegan así al teléfono).
func (m *Modulo) Recepciones(ctx context.Context, q DBTX, usuarioID int64) ([]EstadoRecepcion, error) {
	ps, err := pendientes.DeTecnico(ctx, q, usuarioID, pendientes.OpRecepcion, m.ahora().Add(-7*24*time.Hour), 100)
	if err != nil {
		return nil, err
	}
	salida := make([]EstadoRecepcion, 0, len(ps))
	for _, p := range ps {
		salida = append(salida, estadoRecepcion(p))
	}
	return salida, nil
}

// ClaveRecepcion es la Idempotency-Key del contrato: iris:recepcion:<traslado_id>.
func ClaveRecepcion(trasladoID int64) string {
	return "iris:recepcion:" + strconv.FormatInt(trasladoID, 10)
}

// DBTX es una transacción o el pool.
type DBTX = pendientes.DBTX
