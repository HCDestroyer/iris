package argos

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Lecturas de Argos para el panel e I2 (§2.1). Cada una es un caso de uso en dos fases sin
// transacción abierta durante la llamada HTTP:
//
//  1. un caso corto verifica el permiso y que haya conector activo (503 conector_apagado si no)
//     y lee la instantánea;
//  2. se llama a Argos;
//  3. otro caso guarda la instantánea y escribe la fila de auditoría de la lectura (§0: Iris
//     audita sus lecturas de datos personales), con lo que se leyó en entrada.auditoria.
//
// Si Argos no responde, se sirve la última instantánea con stale = true; solo sin instantánea
// se responde 503 argos_no_disponible.

// Multiempresa: toda lectura de clientes se filtra por la empresa activa (contexto.EmpresaDe,
// que fija identidad.MiddlewareEmpresa con X-Iris-Empresa). Lo que Argos devuelva de otra
// empresa, o sin empresa, se descarta (búsqueda) o es 404 (ficha, resolver IP), y nunca se pide
// el estado de cuenta de un cliente ajeno.

// empresaDeLectura devuelve la empresa por la que se filtra. Con un usuario detrás siempre es
// ≠ 0: auditoria.Caso ya cortó «por elegir» (422) y «sin empresa» (403); queda el modo sin
// conector con el conector activo (no hay empresas registradas), que aquí es 422
// empresas_no_registradas porque sin empresa no hay cómo filtrar lo que devuelve Argos. Solo el
// actor sistema lee sin filtro (0).
func empresaDeLectura(ctx context.Context) (int64, error) {
	e := contexto.EmpresaDe(ctx)
	if e == 0 && contexto.ActorDe(ctx).EsUsuario() {
		return 0, nucleo.ReglaNegocio("empresas_no_registradas",
			"Iris no tiene empresas registradas para filtrar los datos de Argos: regístralas (POST /api/v1/empresas) y asígnalas a los usuarios")
	}
	return e, nil
}

// nuevoPrevio arma la fase 1 genérica: permiso + conector activo. No deja fila al terminar bien.
func nuevoPrevio[In any](m *Modulo, accion string) *auditoria.Caso[In, conector.Leido] {
	return auditoria.Nuevo(auditoria.Caso[In, conector.Leido]{
		Accion: accion, Permiso: nucleo.PermClientesVer, AuditarExito: soloLecturas[conector.Leido],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ In) (conector.Leido, error) {
			c, err := m.conectorActivo(ctx, t)
			if err != nil {
				return c, err
			}
			_, err = empresaDeLectura(ctx)
			return c, err
		},
	})
}

// Buscar clientes ---------------------------------------------------------------------------

// EntradaBuscarClientes: ?q=&limit=&cursor=.
type EntradaBuscarClientes struct {
	Q      string `json:"q" query:"q" minLength:"2" maxLength:"100" required:"true" doc:"Nombre, código, teléfono, NIT o dirección (lo que Argos admita en su búsqueda)"`
	Limit  int    `json:"limit" query:"limit" minimum:"1" maximum:"50" default:"20" doc:"Tamaño de página"`
	Cursor string `json:"cursor,omitempty" query:"cursor" maxLength:"500" doc:"nextCursor de la página anterior (de Argos)"`
}

type entradaBusquedaFinal struct {
	EntradaBuscarClientes
	pagina   Pagina[ClienteArgos]
	errArgos error
}

// BuscarClientes busca en Argos (GET /clientes?q=). Si Argos no responde, busca en las
// instantáneas locales (solo clientes que ya se abrieron) y marca stale. No guarda los
// resultados: la instantánea es solo de lo que se muestra en una ficha.
type BuscarClientes struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaBuscarClientes, conector.Leido]
	final  *auditoria.Caso[entradaBusquedaFinal, ResultadoBusqueda]
}

func nuevoBuscarClientes(m *Modulo) *BuscarClientes {
	b := &BuscarClientes{m: m, previo: nuevoPrevio[EntradaBuscarClientes](m, "clientes.buscar")}
	b.final = auditoria.Nuevo(auditoria.Caso[entradaBusquedaFinal, ResultadoBusqueda]{
		Accion: "clientes.buscar", Permiso: nucleo.PermClientesVer,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaBusquedaFinal) (ResultadoBusqueda, error) {
			empresa := contexto.EmpresaDe(ctx)
			r := ResultadoBusqueda{Data: []ClienteResumen{}}
			switch {
			case in.errArgos == nil:
				for _, c := range in.pagina.Data {
					if empresa != 0 && c.EmpresaID != empresa { // también los que llegan sin empresa
						continue
					}
					r.Data = append(r.Data, clienteArgosAResumen(c))
				}
				if in.pagina.NextCursor != "" {
					r.NextCursor = &in.pagina.NextCursor
				}
			case EsCaido(in.errArgos):
				filas, err := argosbd.New(t).BuscarClientesLocal(ctx, argosbd.BuscarClientesLocalParams{
					Q: escaparLike(in.Q), EmpresaID: empresa, Limite: int32(in.limite()), //nolint:gosec // ≤ 50
				})
				if err != nil {
					return r, err
				}
				for _, f := range filas {
					r.Data = append(r.Data, aClienteResumen(f))
				}
				r.Stale = true
			default:
				return r, Problema(in.errArgos)
			}
			ids := make([]int64, 0, len(r.Data))
			for _, c := range r.Data {
				ids = append(ids, c.ID)
			}
			t.Anotar(map[string]any{"clientes": ids, "stale": r.Stale})
			return r, nil
		},
	})
	return b
}

func (e EntradaBuscarClientes) limite() int {
	if e.Limit <= 0 || e.Limit > 50 {
		return 20
	}
	return e.Limit
}

// Ejecutar busca.
func (b *BuscarClientes) Ejecutar(ctx context.Context, in EntradaBuscarClientes) (ResultadoBusqueda, error) {
	in.Q = strings.TrimSpace(in.Q)
	c, err := b.previo.Ejecutar(ctx, in)
	if err != nil {
		return ResultadoBusqueda{}, err
	}
	f := entradaBusquedaFinal{EntradaBuscarClientes: in}
	f.pagina, f.errArgos = b.m.clienteDe(c).BuscarClientes(ctx, in.Q, in.limite(), in.Cursor, contexto.EmpresaDe(ctx))
	return b.final.Ejecutar(ctx, f)
}

func escaparLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Ficha del cliente -------------------------------------------------------------------------

// EntradaFicha: id del cliente en Argos.
type EntradaFicha struct {
	ID int64 `json:"id" path:"id" minimum:"1" doc:"Id del cliente en Argos"`
}

type fichaPrevia struct {
	conector  conector.Leido
	cliente   *argosbd.IntegracionArgosCliente
	estadoCta bool
}

type entradaFichaFinal struct {
	ClienteID int64 `json:"cliente_id"`
	previa    fichaPrevia
	leida     *fichaArgos
	noExiste  bool
	errArgos  error
	estado    *EstadoCuenta
	errEstado *string
}

// ObtenerFicha devuelve el cliente con sus servicios y el estado de cuenta resumido. La
// instantánea se refresca si tiene más de VigenciaInstantanea (24 h) o no existe; si Argos no
// responde, se devuelve la que hay con stale = true. El estado de cuenta se lee siempre en vivo
// (si el actor tiene clientes.estado_cuenta) y nunca se guarda.
type ObtenerFicha struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaFicha, fichaPrevia]
	final  *auditoria.Caso[entradaFichaFinal, Ficha]
}

func nuevoObtenerFicha(m *Modulo) *ObtenerFicha {
	o := &ObtenerFicha{m: m}
	o.previo = auditoria.Nuevo(auditoria.Caso[EntradaFicha, fichaPrevia]{
		Accion: "clientes.ver", Permiso: nucleo.PermClientesVer, AuditarExito: soloLecturas[fichaPrevia],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaFicha) (fichaPrevia, error) {
			q := argosbd.New(t)
			c, err := m.conectorActivo(ctx, t)
			if err != nil {
				return fichaPrevia{}, err
			}
			if _, err := empresaDeLectura(ctx); err != nil {
				return fichaPrevia{}, err
			}
			p := fichaPrevia{conector: c, estadoCta: contexto.PermisosDe(ctx).Tiene(string(nucleo.PermClientesEstadoCuenta))}
			f, err := q.ClientePorID(ctx, in.ID)
			if err == nil {
				p.cliente = &f
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return p, err
			}
			return p, nil
		},
	})
	o.final = auditoria.Nuevo(auditoria.Caso[entradaFichaFinal, Ficha]{
		Accion: "clientes.ver", Permiso: nucleo.PermClientesVer,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaFichaFinal) (Ficha, error) {
			q := argosbd.New(t)
			t.FijarEntidad("cliente_argos", in.ClienteID)
			if in.noExiste {
				// Argos ya no lo tiene: se borra la instantánea aunque la acción termine en 404.
				t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
					return argosbd.New(tx).BorrarCliente(ctx, in.ClienteID)
				})
				return Ficha{}, nucleo.NoEncontrado("El cliente no existe en Argos")
			}
			stale := false
			switch {
			case in.leida != nil:
				if err := guardarFicha(ctx, q, *in.leida); err != nil {
					return Ficha{}, err
				}
			case in.errArgos != nil && EsCaido(in.errArgos) && in.previa.cliente != nil:
				stale = true
			case in.errArgos != nil:
				return Ficha{}, Problema(in.errArgos)
			}
			cliente, err := q.ClientePorID(ctx, in.ClienteID)
			if err != nil {
				return Ficha{}, err
			}
			if e := contexto.EmpresaDe(ctx); e != 0 && cliente.EmpresaID != e {
				// Mismo 404 que un cliente inexistente: no revela clientes de otras empresas.
				t.Anotar(map[string]any{"empresa_cliente": cliente.EmpresaID})
				return Ficha{}, nucleo.NoEncontrado("El cliente no existe")
			}
			servicios, err := q.ServiciosDeCliente(ctx, in.ClienteID)
			if err != nil {
				return Ficha{}, err
			}
			t.FijarEmpresa(cliente.EmpresaID)
			t.Anotar(map[string]any{"stale": stale, "estado_cuenta": in.estado != nil})
			return Ficha{
				Cliente: aClienteResumen(cliente), Servicios: aServicios(servicios), EstadoCuenta: in.estado,
				EstadoCuentaError: in.errEstado, Stale: stale, SincronizadoEn: cliente.SincronizadoEn,
			}, nil
		},
	})
	return o
}

// Ejecutar arma la ficha.
func (o *ObtenerFicha) Ejecutar(ctx context.Context, in EntradaFicha) (Ficha, error) {
	p, err := o.previo.Ejecutar(ctx, in)
	if err != nil {
		return Ficha{}, err
	}
	f := entradaFichaFinal{ClienteID: in.ID, previa: p}
	cli := o.m.clienteDe(p.conector)
	if p.cliente == nil || time.Since(p.cliente.SincronizadoEn) > o.m.cfg.VigenciaInstantanea {
		leida, err := leerFichaArgos(ctx, cli, in.ID)
		switch {
		case err == nil:
			f.leida = &leida
		case EsNoEncontrado(err):
			f.noExiste = true
		default:
			f.errArgos = err
		}
	}
	switch {
	case f.noExiste || (f.errArgos != nil && p.cliente == nil):
		// Sin ficha que mostrar: la respuesta será un error.
	case deOtraEmpresa(ctx, f.leida, p.cliente):
		// La respuesta será 404 (final): no se pide el estado de cuenta de un cliente ajeno.
	case f.errArgos != nil && EsCaido(f.errArgos) && p.estadoCta:
		// Argos no responde: no se insiste con el estado de cuenta.
		motivo := "argos_no_disponible"
		f.errEstado = &motivo
	default:
		f.estado, f.errEstado = o.estadoCuenta(ctx, cli, p, in.ID)
	}
	return o.final.Ejecutar(ctx, f)
}

// deOtraEmpresa indica si el cliente (el recién leído o, si no, la instantánea) no es de la
// empresa activa.
func deOtraEmpresa(ctx context.Context, leida *fichaArgos, previa *argosbd.IntegracionArgosCliente) bool {
	e := contexto.EmpresaDe(ctx)
	switch {
	case e == 0:
		return false
	case leida != nil:
		return leida.cliente.EmpresaID != e
	case previa != nil:
		return previa.EmpresaID != e
	}
	return false
}

func (o *ObtenerFicha) estadoCuenta(ctx context.Context, cli *Cliente, p fichaPrevia, id int64) (*EstadoCuenta, *string) {
	motivo := func(s string) *string { return &s }
	if !p.estadoCta {
		return nil, motivo("sin_permiso")
	}
	e, err := cli.EstadoCuenta(ctx, id)
	switch {
	case err == nil:
	case EsCaido(err):
		return nil, motivo("argos_no_disponible")
	default:
		return nil, motivo("argos_rechazo")
	}
	monto := e.MontoVencido.Texto()
	if monto == nil {
		monto = e.TotalVencido.Texto()
	}
	return &EstadoCuenta{Saldo: e.Saldo.Texto(), Moneda: e.Moneda, FacturasVencidas: e.CantidadVencidas(), MontoVencido: monto}, nil
}

// Resolver IP --------------------------------------------------------------------------------

// EntradaResolverIP: ?ip=.
type EntradaResolverIP struct {
	IP string `json:"ip" query:"ip" required:"true" minLength:"2" maxLength:"64" doc:"IPv4 o IPv6 del cliente"`
}

type entradaIPFinal struct {
	IP       string `json:"ip"`
	res      ResolucionArgos
	errArgos error
}

// ResolverIP pregunta a Argos de quién es una IP (A8, con respaldo a GET /ip/{ip}). Si Argos no
// responde, busca la IP en la instantánea de servicios (stale).
type ResolverIP struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaResolverIP, conector.Leido]
	final  *auditoria.Caso[entradaIPFinal, IPResuelta]
}

func nuevoResolverIP(m *Modulo) *ResolverIP {
	r := &ResolverIP{m: m, previo: nuevoPrevio[EntradaResolverIP](m, "clientes.resolver_ip")}
	r.final = auditoria.Nuevo(auditoria.Caso[entradaIPFinal, IPResuelta]{
		Accion: "clientes.resolver_ip", Permiso: nucleo.PermClientesVer,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaIPFinal) (IPResuelta, error) {
			salida := IPResuelta{IP: in.IP, Fuente: "argos"}
			switch {
			case in.errArgos == nil:
				salida.ClienteID, salida.ServicioID, salida.EmpresaID = in.res.Ids()
				if salida.ClienteID == nil && salida.ServicioID == nil {
					return salida, nucleo.NoEncontrado("Argos no tiene un servicio con esa IP")
				}
			case EsCaido(in.errArgos):
				s, err := argosbd.New(t).ServicioPorIP(ctx, argosbd.ServicioPorIPParams{Ip: in.IP, EmpresaID: contexto.EmpresaDe(ctx)})
				if errors.Is(err, pgx.ErrNoRows) {
					return salida, Problema(in.errArgos)
				}
				if err != nil {
					return salida, err
				}
				salida.ClienteID, salida.ServicioID, salida.EmpresaID = &s.ClienteID, &s.ID, &s.EmpresaID
				salida.Fuente, salida.Stale = "instantanea", true
			case EsNoEncontrado(in.errArgos):
				return salida, nucleo.NoEncontrado("Argos no tiene un servicio con esa IP")
			default:
				return salida, Problema(in.errArgos)
			}
			if e := contexto.EmpresaDe(ctx); e != 0 && (salida.EmpresaID == nil || *salida.EmpresaID != e) {
				// De otra empresa o sin empresa: igual que una IP sin cliente.
				t.Anotar(map[string]any{"descartado": "otra_empresa"})
				return IPResuelta{IP: in.IP}, nucleo.NoEncontrado("Argos no tiene un servicio con esa IP")
			}
			if salida.EmpresaID != nil {
				t.FijarEmpresa(*salida.EmpresaID)
			}
			if salida.ClienteID != nil {
				t.FijarEntidad("cliente_argos", *salida.ClienteID)
			}
			t.Anotar(map[string]any{"cliente_id": salida.ClienteID, "servicio_id": salida.ServicioID, "stale": salida.Stale})
			return salida, nil
		},
	})
	return r
}

// Ejecutar resuelve la IP.
func (r *ResolverIP) Ejecutar(ctx context.Context, in EntradaResolverIP) (IPResuelta, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(in.IP))
	if err != nil {
		return IPResuelta{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "ip", Message: "No es una IP válida", Rule: "ip"})
	}
	in.IP = ip.Unmap().String()
	c, err := r.previo.Ejecutar(ctx, in)
	if err != nil {
		return IPResuelta{}, err
	}
	f := entradaIPFinal{IP: in.IP}
	f.res, f.errArgos = r.m.clienteDe(c).ResolverIP(ctx, in.IP, contexto.EmpresaDe(ctx))
	return r.final.Ejecutar(ctx, f)
}

// Catálogos: zonas y routers ------------------------------------------------------------------

// ResultadoCatalogos resume una sincronización.
type ResultadoCatalogos struct {
	Omitido bool `json:"omitido"` // sin conector activo
	Zonas   int  `json:"zonas"`
	Routers int  `json:"routers"`
}

type entradaCatalogos struct {
	zonas    []ZonaArgos
	routers  []RouterArgos
	inicio   time.Time
	errArgos error
}

// SincronizarCatalogos refresca argos_zonas y argos_routers desde Argos (job periódico y al
// activar el conector). Lo que Argos ya no devuelve se borra de la instantánea.
type SincronizarCatalogos struct {
	m       *Modulo
	guardar *auditoria.Caso[entradaCatalogos, ResultadoCatalogos]
}

func nuevoSincronizarCatalogos(m *Modulo) *SincronizarCatalogos {
	s := &SincronizarCatalogos{m: m}
	s.guardar = auditoria.Nuevo(auditoria.Caso[entradaCatalogos, ResultadoCatalogos]{
		Accion: "integraciones.sincronizar_catalogos", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaCatalogos) (ResultadoCatalogos, error) {
			t.FijarEmpresa(0)
			if in.errArgos != nil {
				return ResultadoCatalogos{}, in.errArgos
			}
			q := argosbd.New(t)
			for _, z := range in.zonas {
				if err := q.GuardarZona(ctx, argosbd.GuardarZonaParams{ID: z.ID, EmpresaID: z.Empresa(), Nombre: z.Nombre, SincronizadoEn: in.inicio}); err != nil {
					return ResultadoCatalogos{}, err
				}
			}
			for _, r := range in.routers {
				lat, lon := r.LatLon()
				if err := q.GuardarRouter(ctx, argosbd.GuardarRouterParams{
					ID: r.ID, EmpresaID: r.Empresa(), Nombre: r.Nombre, ZonaID: idDe(r.ZonaID, r.Zona), Lat: lat, Lon: lon, SincronizadoEn: in.inicio,
				}); err != nil {
					return ResultadoCatalogos{}, err
				}
			}
			if _, err := q.BorrarZonasAnteriores(ctx, in.inicio); err != nil {
				return ResultadoCatalogos{}, err
			}
			if _, err := q.BorrarRoutersAnteriores(ctx, in.inicio); err != nil {
				return ResultadoCatalogos{}, err
			}
			res := ResultadoCatalogos{Zonas: len(in.zonas), Routers: len(in.routers)}
			t.Anotar(map[string]any{"zonas": res.Zonas, "routers": res.Routers})
			return res, nil
		},
	})
	return s
}

// Ejecutar sincroniza. Sin conector activo no hace nada.
func (s *SincronizarCatalogos) Ejecutar(ctx context.Context, _ struct{}) (ResultadoCatalogos, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ResultadoCatalogos{}, nucleo.ErrSinBD
	}
	c, err := s.m.conectorActivo(ctx, bd)
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Codigo() == "conector_apagado" {
		return ResultadoCatalogos{Omitido: true}, nil
	}
	if err != nil {
		return ResultadoCatalogos{}, err
	}
	in := entradaCatalogos{inicio: time.Now().UTC()}
	cli := s.m.clienteDe(c)
	if in.zonas, in.errArgos = cli.Zonas(ctx); in.errArgos == nil {
		in.routers, in.errArgos = cli.Routers(ctx)
	}
	return s.guardar.Ejecutar(ctx, in)
}
