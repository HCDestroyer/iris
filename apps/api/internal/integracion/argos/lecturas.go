package argos

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
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
// que fija identidad.MiddlewareEmpresa con X-Iris-Empresa). En Argos el cliente no tiene empresa:
// la tienen sus servicios. Un cliente es de la empresa si alguno de sus servicios lo es, o si no
// tiene servicios (lo ven todas, como las listas de Argos). Lo de otra empresa se descarta
// (búsqueda) o es 404 (ficha, resolver IP), y nunca se pide el estado de cuenta de un cliente ajeno.

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

// MarshalJSON es lo que queda en auditoria.acciones.entrada: una búsqueda por teléfono, NIT o CUI
// (4 dígitos o más) se guarda con los dígitos ocultos salvo los dos últimos (5512-3344 →
// ****-**44); por nombre, tal cual.
func (e EntradaBuscarClientes) MarshalJSON() ([]byte, error) {
	type plano EntradaBuscarClientes
	p := plano(e)
	p.Q = enmascararDigitos(p.Q)
	return json.Marshal(p)
}

func enmascararDigitos(q string) string {
	total := 0
	for _, r := range q {
		if r >= '0' && r <= '9' {
			total++
		}
	}
	if total < 4 {
		return q
	}
	var b strings.Builder
	visto := 0
	for _, r := range q {
		if r >= '0' && r <= '9' {
			visto++
			if visto <= total-2 {
				r = '*'
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

type entradaBusquedaFinal struct {
	EntradaBuscarClientes
	pagina   Pagina[ClienteArgos]
	errArgos error
	// verificados: lo que se leyó en Argos de los clientes que Iris no conocía (los que no están,
	// por el tope o un error, quedan fuera).
	verificados map[int64]infoEmpresas
	conectorID  int64
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
				q := argosbd.New(t)
				for _, c := range in.pagina.Data {
					if empresa != 0 {
						info, conocido, err := m.conocido(ctx, q, in.conectorID, c)
						if err != nil {
							return r, err
						}
						if !conocido {
							info, conocido = in.verificados[c.ID]
						}
						if !conocido || !enEmpresa(info.empresas, info.sinServicios, empresa) {
							continue
						}
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
	cli := b.m.clienteDe(c)
	empresa := contexto.EmpresaDe(ctx)
	f.pagina, f.errArgos = cli.BuscarClientes(ctx, in.Q, in.limite(), in.Cursor, empresa)
	f.conectorID = c.ID
	if f.errArgos == nil && empresa != 0 {
		// Los que Iris no conoce se verifican en Argos (sus servicios) fuera de la transacción.
		desconocidos, err := b.desconocidos(ctx, c.ID, f.pagina.Data)
		if err != nil {
			return ResultadoBusqueda{}, err
		}
		f.verificados = b.m.verificarEnArgos(ctx, c.ID, cli, desconocidos)
	}
	return b.final.Ejecutar(ctx, f)
}

// desconocidos: los clientes del resultado de los que Iris no sabe sus empresas.
func (b *BuscarClientes) desconocidos(ctx context.Context, conectorID int64, clientes []ClienteArgos) ([]ClienteArgos, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return nil, nucleo.ErrSinBD
	}
	q := argosbd.New(bd)
	var salida []ClienteArgos
	for _, c := range clientes {
		_, conocido, err := b.m.conocido(ctx, q, conectorID, c)
		if err != nil {
			return nil, err
		}
		if !conocido {
			salida = append(salida, c)
		}
	}
	return salida, nil
}

// infoEmpresas: lo que se sabe de las empresas de un cliente de Argos (las de sus servicios) y si
// Argos dijo, con la lista de todas las empresas, que no tiene servicios.
type infoEmpresas struct {
	empresas     []int64
	sinServicios bool
}

// enEmpresa: el cliente es de la empresa si alguno de sus servicios lo es, o si no tiene servicios
// (lo ven todas, como en Argos; multiempresa.md). Una lista vacía por el alcance del token o
// servicios sin empresa no lo hacen «de todas».
func enEmpresa(empresas []int64, sinServicios bool, empresa int64) bool {
	return slices.Contains(empresas, empresa) || sinServicios
}

// conocido: lo que Iris ya sabe de un cliente que devolvió Argos: lo que venga en el resultado, la
// instantánea o una verificación reciente (caché). ok = false si no sabe nada.
func (m *Modulo) conocido(ctx context.Context, q *argosbd.Queries, conectorID int64, c ClienteArgos) (infoEmpresas, bool, error) {
	if conocidas := EmpresasDelCliente(c, nil); len(conocidas) > 0 {
		return infoEmpresas{empresas: conocidas}, true, nil
	}
	f, err := q.ClientePorID(ctx, c.ID)
	switch {
	case err == nil:
		return infoEmpresas{empresas: empresasDeFila(f), sinServicios: f.SinServicios}, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return infoEmpresas{}, false, err
	}
	if i, ok := m.verificacion.leer(conectorID, c.ID); ok {
		return i, true, nil
	}
	return infoEmpresas{}, false, nil
}

// empresasDeFila: las empresas de la instantánea; una fila sin ellas pero con empresa_id (anterior
// a la migración o escrita a mano) cuenta como [empresa_id].
func empresasDeFila(f argosbd.IntegracionArgosCliente) []int64 {
	if len(f.Empresas) == 0 && f.EmpresaID != 0 {
		return []int64{f.EmpresaID}
	}
	return f.Empresas
}

func escaparLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Ficha del cliente -------------------------------------------------------------------------

// EntradaFicha: id del cliente en Argos.
type EntradaFicha struct {
	ID int64 `json:"id" path:"id" minimum:"1" doc:"Id del cliente en Argos"`
	// Refrescar: «Refrescar» del panel. Relee de Argos aunque la instantánea sea reciente, salvo
	// que se haya leído hace menos de RefrescoMinimo (no se martilla a Argos con clics).
	Refrescar bool `json:"refrescar,omitempty" query:"refrescar" doc:"true: relee de Argos aunque la instantánea tenga menos de 24 h (si se leyó hace menos de 30 s, devuelve la que hay)"`
}

// RefrescoMinimo: con ?refrescar=true, una instantánea más nueva que esto no se vuelve a pedir.
const RefrescoMinimo = 30 * time.Second

// Por qué «Refrescar» no llamó a Argos (Ficha.RefrescoOmitido).
const (
	RefrescoCuota    = "cuota"
	RefrescoReciente = "reciente"
	RefrescoEnEspera = "en_espera"
)

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
	// enEspera: no se llamó a Argos por un fallo reciente (no se vuelve a marcar el fallo).
	enEspera bool
	// omitido: por qué «Refrescar» no llamó a Argos (RefrescoCuota, RefrescoReciente…).
	omitido   *string
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
					q := argosbd.New(tx)
					if err := q.AnonimizarCopiasDeCliente(ctx, in.ClienteID); err != nil {
						return err
					}
					return q.BorrarCliente(ctx, in.ClienteID)
				})
				return Ficha{}, nucleo.NoEncontrado("El cliente no existe en Argos")
			}
			stale := false
			switch {
			case in.leida != nil:
				if err := guardarFicha(ctx, t, q, *in.leida); err != nil {
					return Ficha{}, err
				}
			case in.errArgos != nil && EsCaido(in.errArgos) && in.previa.cliente != nil:
				stale = true
				if !in.enEspera {
					ahora := time.Now()
					if err := q.MarcarFalloCliente(ctx, argosbd.MarcarFalloClienteParams{ID: in.ClienteID, FalloEn: &ahora}); err != nil {
						return Ficha{}, err
					}
				}
			case in.errArgos != nil:
				return Ficha{}, Problema(in.errArgos)
			}
			cliente, err := q.ClientePorID(ctx, in.ClienteID)
			if err != nil {
				return Ficha{}, err
			}
			servicios, err := q.ServiciosDeCliente(ctx, in.ClienteID)
			if err != nil {
				return Ficha{}, err
			}
			e := contexto.EmpresaDe(ctx)
			if e != 0 && !enEmpresa(empresasDeFila(cliente), cliente.SinServicios, e) {
				// Mismo 404 que un cliente inexistente: no revela clientes de otras empresas.
				t.Anotar(map[string]any{"empresas_cliente": cliente.Empresas})
				return Ficha{}, nucleo.NoEncontrado("El cliente no existe")
			}
			// Los servicios de otras empresas no se muestran (salvo con empresas.todas global).
			filtro := int64(0)
			if e != 0 && !todasGlobal(ctx) {
				filtro = e
				servicios = slices.DeleteFunc(servicios, func(s argosbd.IntegracionArgosServicio) bool { return s.EmpresaID != e })
			}
			eliminados, err := q.ServiciosEliminadosDeCliente(ctx, argosbd.ServiciosEliminadosDeClienteParams{ClienteID: &in.ClienteID, EmpresaID: filtro})
			if err != nil {
				return Ficha{}, err
			}
			if e == 0 {
				e = cliente.EmpresaID
			}
			t.FijarEmpresa(e)
			t.Anotar(map[string]any{"stale": stale, "estado_cuenta": in.estado != nil})
			resumen := aClienteResumen(cliente)
			if resumen.Estado == "" {
				estados := make([]string, 0, len(servicios))
				for _, s := range servicios {
					estados = append(estados, s.Estado)
				}
				resumen.Estado = ResumenEstado(estados)
			}
			return Ficha{
				Cliente: resumen, Servicios: aServicios(servicios), ServiciosEliminados: aEliminados(eliminados), EstadoCuenta: in.estado,
				EstadoCuentaError: in.errEstado, Stale: stale, SincronizadoEn: cliente.SincronizadoEn, RefrescoOmitido: in.omitido,
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
	ahora := time.Now()
	e := contexto.EmpresaDe(ctx)
	// Un cliente que la instantánea pone en otra empresa dará 404: «Refrescar» no llama a Argos.
	deEmpresa := p.cliente == nil || e == 0 || enEmpresa(empresasDeFila(*p.cliente), p.cliente.SinServicios, e)
	vencida := debeRefrescar(p.cliente, false, o.m.cfg.VigenciaInstantanea)
	// «Refrescar» de una instantánea vigente: lectura a pedido, con cuota (solo si se llama).
	aPedido := in.Refrescar && !vencida && deEmpresa && ahora.Sub(p.cliente.SincronizadoEn) > RefrescoMinimo
	omitir := func(motivo string) {
		if in.Refrescar && deEmpresa {
			f.omitido = &motivo
		}
	}
	neg, hayNeg := o.m.refrescos.negativoDe(p.conector.ID, in.ID, ahora)
	switch {
	case p.cliente == nil && hayNeg && neg.noExiste:
		// Argos dijo 404 hace menos de NegativoTTL: el mismo 404 sin volver a preguntar.
		f.noExiste = true
	case p.cliente == nil && hayNeg:
		// Argos no respondió hace menos de NegativoTTL y no hay instantánea: 503 sin esperar otra vez.
		f.errArgos, f.enEspera = errEnEspera(), true
	case !vencida && !aPedido:
		omitir(RefrescoReciente)
	case enEspera(p.cliente, ahora):
		// Argos se cayó hace poco al leer este cliente: no se insiste; la instantánea, stale.
		f.errArgos, f.enEspera = errEnEspera(), true
		omitir(RefrescoEnEspera)
	default:
		// Toda lectura cobra la cuota del usuario; «Refrescar», además, la suya del conector y de la
		// empresa.
		c := cobro{usuario: contexto.ActorDe(ctx).ID, empresa: e}
		if aPedido {
			c.uso = usoRefrescar
		}
		leida, err := o.m.refrescos.leerUnaVez(ctx, p.conector.ID, in.ID, c, p.cliente == nil, func(cx context.Context) (fichaArgos, error) {
			return leerFichaArgos(cx, nucleo.BDDe(ctx), cli, in.ID)
		})
		switch {
		case err == nil:
			f.leida = &leida
		case errors.Is(err, errCuota) && aPedido:
			// Sin llamar a Argos: la instantánea vigente, y el panel avisa que no se refrescó.
			omitir(RefrescoCuota)
		case errors.Is(err, errCuota) && p.cliente != nil:
			// Vencida y sin cuota: la última instantánea, marcada stale (sin marcar un fallo).
			f.errArgos, f.enEspera = errEnEspera(), true
		case errors.Is(err, errCuota):
			f.errArgos = errCuotaArgos()
		case EsNoEncontrado(err):
			f.noExiste = true
		default:
			f.errArgos = err
		}
	}
	switch {
	case f.noExiste || (f.errArgos != nil && p.cliente == nil):
		// Sin ficha que mostrar: la respuesta será un error.
	case !estadoCuentaPermitido(ctx, f.leida, p.cliente):
		// De otra empresa (la respuesta será 404) o sin servicios sin empresas.todas: no se pide
		// su estado de cuenta.
		if p.estadoCta {
			motivo := "sin_permiso"
			f.errEstado = &motivo
		}
	case f.errArgos != nil && EsCaido(f.errArgos) && p.estadoCta:
		// Argos no responde: no se insiste con el estado de cuenta.
		motivo := "argos_no_disponible"
		f.errEstado = &motivo
	default:
		f.estado, f.errEstado = o.estadoCuenta(ctx, cli, p, in.ID)
	}
	return o.final.Ejecutar(ctx, f)
}

// debeRefrescar: sin instantánea, vencida (VigenciaInstantanea), de una versión anterior de los
// datos (VersionDatos) o pedida con ?refrescar=true y de hace más de RefrescoMinimo.
func debeRefrescar(f *argosbd.IntegracionArgosCliente, refrescar bool, vigencia time.Duration) bool {
	if f == nil || f.VersionDatos < VersionDatos {
		return true
	}
	edad := time.Since(f.SincronizadoEn)
	return edad > vigencia || (refrescar && edad > RefrescoMinimo)
}

// enEspera: la última lectura de este cliente falló por caída hace menos de EsperaTrasFallo.
// Solo con instantánea (sin ella no hay qué servir y se intenta).
func enEspera(f *argosbd.IntegracionArgosCliente, ahora time.Time) bool {
	return f != nil && f.FalloEn != nil && ahora.Sub(*f.FalloEn) < EsperaTrasFallo
}

// errCuotaArgos: 429 por la cuota de lecturas de Argos (sin instantánea que servir).
func errCuotaArgos() error {
	return nucleo.DemasiadosIntentos("cuota_argos", "Demasiadas consultas a Argos en el último minuto; intenta en un momento", time.Minute)
}

// errEnEspera: Argos no se consultó por un fallo reciente; cuenta como caído (stale).
func errEnEspera() error {
	return conector.NuevoErrorRemoto(Remoto, 0, "Argos falló hace poco; se reintenta en un momento", nil)
}

// estadoCuentaPermitido: el estado de cuenta se pide si el cliente tiene un servicio en la empresa
// activa. Un cliente sin servicios (o recién creado por un alta, aún sin servicio) solo para quien
// tiene empresas.todas global; uno de otra empresa, nunca (la ficha será 404).
func estadoCuentaPermitido(ctx context.Context, leida *fichaArgos, previa *argosbd.IntegracionArgosCliente) bool {
	e := contexto.EmpresaDe(ctx)
	switch {
	case e == 0:
		return true
	case leida != nil:
		if slices.Contains(EmpresasDelCliente(leida.cliente, leida.servicios), e) {
			return true
		}
		return leida.completa && len(leida.servicios) == 0 && todasGlobal(ctx)
	case previa != nil:
		if slices.Contains(empresasDeFila(*previa), e) {
			return true
		}
		return previa.SinServicios && todasGlobal(ctx)
	}
	return false
}

// todasGlobal: el actor tiene empresas.todas sin límite de alcance.
func todasGlobal(ctx context.Context) bool {
	return contexto.PermisosDe(ctx).TieneGlobal(string(nucleo.PermEmpresasTodas))
}

func (o *ObtenerFicha) estadoCuenta(ctx context.Context, cli *Cliente, p fichaPrevia, id int64) (*EstadoCuenta, *string) {
	motivo := func(s string) *string { return &s }
	if !p.estadoCta {
		return nil, motivo("sin_permiso")
	}
	// Solo el de la empresa activa (Argos lo separa por empresa); con empresas.todas, el consolidado.
	empresa := contexto.EmpresaDe(ctx)
	if todasGlobal(ctx) {
		empresa = 0
	}
	e, err := cli.EstadoCuenta(ctx, id, empresa)
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
	vivos    map[int64]time.Time
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
			ids := make([]int64, 0, len(in.routers))
			for _, r := range in.routers {
				ids = append(ids, r.ID)
			}
			// Candados en orden de id antes de guardar ninguno (sin interbloqueos).
			if err := bloquear(ctx, q, eliminadoRouter, ids); err != nil {
				return ResultadoCatalogos{}, err
			}
			for _, r := range in.routers {
				lat, lon := r.LatLon()
				if err := guardarRouter(ctx, t, q, argosbd.GuardarRouterParams{
					ID: r.ID, EmpresaID: r.Empresa(), Nombre: r.Nombre, ZonaID: idDe(r.ZonaID, r.Zona), Lat: lat, Lon: lon, SincronizadoEn: in.inicio,
				}, vivoEn(in.vivos, r.ID)); err != nil {
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
	in := entradaCatalogos{}
	if in.inicio, err = argosbd.New(bd).RelojBD(ctx); err != nil {
		return ResultadoCatalogos{}, err
	}
	cli := s.m.clienteDe(c)
	if in.zonas, in.errArgos = cli.Zonas(ctx); in.errArgos == nil {
		in.routers, in.errArgos = cli.Routers(ctx)
	}
	if in.errArgos == nil {
		ids := make([]int64, 0, len(in.routers))
		for _, r := range in.routers {
			ids = append(ids, r.ID)
		}
		// Routers de la lista con lápida: solo resucitan con un GET /routers/:id con 200.
		if in.vivos, err = confirmarVivos(ctx, bd, eliminadoRouter, ids, func(ctx context.Context, id int64) error {
			_, err := cli.obtenerRouterUnaVez(ctx, id)
			return err
		}); err != nil {
			return ResultadoCatalogos{}, err
		}
	}
	return s.guardar.Ejecutar(ctx, in)
}
