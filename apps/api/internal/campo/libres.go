package campo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Órdenes libres: si la empresa lo permite (ajuste tecnicos_toman_libres, apagado por defecto), el
// técnico ve las órdenes pendientes y sin técnico de su cuadrilla o de las zonas que atiende
// (GET /campo/ordenes-libres). Una orden con cuadrilla es libre solo para los técnicos de ESA
// cuadrilla (despacho ya la encaminó); sin cuadrilla, vale la zona, y al tomarla queda con la
// cuadrilla del técnico. Fuera de eso, y en cualquier estado que no sea abierto (pendiente,
// programada, en camino), tomar responde el mismo 404 que una orden inexistente: sin número ni
// nombre (no se enumeran órdenes ni técnicos con ids secuenciales) y toma una (POST /ordenes/{id}/tomar). Tomar reutiliza la lógica de
// ordenes.asignar (asignar) con el técnico = el actor, bajo el candado de la orden: si otro la tomó
// primero, 409 orden_ya_tomada con tomadaPor. Requiere conexión: no va por el lote (dos técnicos
// sin señal no pueden saber quién llegó primero).

// AjustesCampo son los ajustes de la app de campo de la empresa activa.
type AjustesCampo struct {
	EmpresaID           *int64 `json:"empresaId" nullable:"true"`
	TecnicosTomanLibres bool   `json:"tecnicosTomanLibres" doc:"Los técnicos pueden tomar órdenes libres (sin técnico) de su cuadrilla o de las zonas que atiende"`
	PorDefecto          bool   `json:"porDefecto" doc:"La empresa no los ha fijado: son los de Iris (todo apagado)"`
	Version             int32  `json:"version" doc:"Versión de los de la empresa (0 si son los por defecto)"`
}

// EntradaFijarAjustes fija los ajustes de la empresa activa.
type EntradaFijarAjustes struct {
	Version             int32 `json:"version" minimum:"0" doc:"Versión leída (0 si eran los por defecto); si cambió, 409"`
	TecnicosTomanLibres bool  `json:"tecnicosTomanLibres"`
}

func aAjustes(f campobd.CampoAjuste, empresa *int64) AjustesCampo {
	return AjustesCampo{EmpresaID: empresa, TecnicosTomanLibres: f.TecnicosTomanLibres, Version: f.Version}
}

func porDefecto(empresa *int64) AjustesCampo {
	return AjustesCampo{EmpresaID: empresa, PorDefecto: true}
}

// empresaDeAjustes: la empresa activa (null sin conector). El actor sistema no tiene una.
func empresaDeAjustes(ctx context.Context) (*int64, error) {
	todas, empresa := filtroEmpresa(ctx)
	if todas || (empresa != nil && *empresa == 0) {
		return nil, nucleo.EmpresaRequerida()
	}
	return empresa, nil
}

// ajustesDe lee los ajustes de la empresa activa (los por defecto si no hay fila).
func ajustesDe(ctx context.Context, q *campobd.Queries) (AjustesCampo, error) {
	empresa, err := empresaDeAjustes(ctx)
	if err != nil {
		return AjustesCampo{}, err
	}
	f, err := q.AjustesDeEmpresa(ctx, empresa)
	if errors.Is(err, pgx.ErrNoRows) {
		return porDefecto(empresa), nil
	}
	if err != nil {
		return AjustesCampo{}, err
	}
	return aAjustes(f, empresa), nil
}

// puedeTomarLibres: el ajuste de la empresa está encendido y el actor tiene ordenes.tomar.
func puedeTomarLibres(ctx context.Context, q *campobd.Queries) (bool, error) {
	if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermOrdenesTomar)) {
		return false, nil
	}
	if todas, empresa := filtroEmpresa(ctx); todas || (empresa != nil && *empresa == 0) {
		return false, nil
	}
	a, err := ajustesDe(ctx, q)
	return a.TecnicosTomanLibres, err
}

func libresApagado() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "libres_apagado", "Tomar órdenes libres está apagado",
		"La empresa no permite que los técnicos tomen órdenes libres (Administración → App de campo)")
}

func (m *Modulo) nuevoVerAjustes() *auditoria.Caso[struct{}, AjustesCampo] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, AjustesCampo]{
		Accion: "campo.configurar", Permiso: nucleo.PermCampoConfigurar, AuditarExito: soloLecturas[AjustesCampo],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (AjustesCampo, error) {
			return ajustesDe(ctx, campobd.New(t))
		},
	})
}

func (m *Modulo) nuevoFijarAjustes() *auditoria.Caso[EntradaFijarAjustes, AjustesCampo] {
	return auditoria.Nuevo(auditoria.Caso[EntradaFijarAjustes, AjustesCampo]{
		Accion: "campo.configurar", Permiso: nucleo.PermCampoConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaFijarAjustes) (AjustesCampo, error) {
			if !contexto.PermisosDe(ctx).TieneGlobal(string(nucleo.PermCampoConfigurar)) {
				return AjustesCampo{}, nucleo.Prohibido("Los ajustes de la empresa exigen campo.configurar sin límite de zona o cuadrilla")
			}
			empresa, err := empresaDeAjustes(ctx)
			if err != nil {
				return AjustesCampo{}, err
			}
			t.FijarEntidad("ajustes_campo", deref(empresa))
			if empresa != nil {
				t.FijarEmpresa(*empresa)
			}
			q := campobd.New(t)
			antes, err := q.AjustesDeEmpresaParaActualizar(ctx, empresa)
			var f campobd.CampoAjuste
			campos := []string{}
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				if in.Version != 0 {
					return AjustesCampo{}, nucleo.ConflictoVersion("ajustes_campo", deref(empresa))
				}
				f, err = q.CrearAjustes(ctx, campobd.CrearAjustesParams{EmpresaID: empresa, TecnicosTomanLibres: in.TecnicosTomanLibres})
				if err != nil {
					return AjustesCampo{}, err
				}
				t.RegistrarCambios("ajustes_campo", f.ID, nil, f)
				if in.TecnicosTomanLibres {
					campos = append(campos, "tecnicos_toman_libres")
				}
			case err != nil:
				return AjustesCampo{}, err
			default:
				if err := nucleo.ExigirVersion(in.Version, antes.Version, "ajustes_campo", antes.ID); err != nil {
					return AjustesCampo{}, err
				}
				if antes.TecnicosTomanLibres == in.TecnicosTomanLibres {
					return AjustesCampo{}, nucleo.ReglaNegocio("sin_cambios", "Los ajustes ya están así")
				}
				f, err = q.GuardarAjustes(ctx, campobd.GuardarAjustesParams{ID: antes.ID, Version: antes.Version, TecnicosTomanLibres: in.TecnicosTomanLibres})
				if err != nil {
					return AjustesCampo{}, nucleo.VerificarVersion(err, "ajustes_campo", antes.ID)
				}
				t.RegistrarCambios("ajustes_campo", f.ID, antes, f)
				campos = append(campos, "tecnicos_toman_libres")
			}
			return aAjustes(f, empresa), t.Emitir("campo.ajustes_cambiados", map[string]any{
				"empresa_id": empresa, "campos": campos, "tecnicos_toman_libres": f.TecnicosTomanLibres,
			})
		},
	})
}

// --- Órdenes libres ---------------------------------------------------------------------

// ZonaLibre: una zona de donde salen las órdenes libres del técnico.
type ZonaLibre struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
}

// OrdenLibre es una orden que el técnico puede tomar.
type OrdenLibre struct {
	Orden
	ClienteNombre *string `json:"clienteNombre" nullable:"true" doc:"De la instantánea de Argos (null si Iris no la tiene)"`
	DistanciaM    *int64  `json:"distanciaM" nullable:"true" doc:"Metros en línea recta desde tu última posición conocida (null sin posición o sin coordenadas)"`
}

// OrdenesLibres es la página de órdenes libres.
type OrdenesLibres struct {
	Data       []OrdenLibre `json:"data"`
	NextCursor *string      `json:"nextCursor" nullable:"true" doc:"Cursor de la página siguiente; null si no hay más"`
	Total      int          `json:"total" doc:"Cuántas hay en total (hasta 1000), para el contador de la hoja del «+»"`
	Orden      string       `json:"orden" enum:"distancia,fecha" doc:"distancia: desde tu última posición conocida (las sin coordenadas al final, por fecha); fecha: sin posición conocida, por fecha programada y antigüedad"`
	Zonas      []ZonaLibre  `json:"zonas" doc:"Zonas que atiende tu cuadrilla (de donde salen)"`
}

// EntradaOrdenesLibres pagina las órdenes libres.
type EntradaOrdenesLibres struct {
	nucleo.ParamsPagina
}

// EntradaTomarOrden: el técnico toma la orden libre.
type EntradaTomarOrden struct {
	ID int64 `json:"-"`
}

func (e EntradaTomarOrden) ordenID() int64 { return e.ID }

// tomables: la cuadrilla y las zonas de donde el técnico puede tomar órdenes libres: las de su
// ficha de agenda activa (su cuadrilla y las zonas que atiende) y, si ordenes.tomar le llega con
// alcance por zona o cuadrilla, esas. Sin ficha ni alcance, ninguna (falla cerrado). `propia` es
// la cuadrilla de su ficha (la que recibe una orden sin cuadrilla al tomarla).
func tomables(ctx context.Context, q *campobd.Queries, usuario int64) (zonas, cuadrillas []int64, propia *int64, err error) {
	zonas, cuadrillas = []int64{}, []int64{}
	f, err := q.ZonasDeTecnico(ctx, usuario)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, nil, nil, err
	default:
		if f.CuadrillaID != nil {
			cuadrillas = append(cuadrillas, *f.CuadrillaID)
			propia = f.CuadrillaID
		}
		zonas = append(zonas, f.Zonas...)
	}
	if global, z, c := contexto.PermisosDe(ctx).Alcances(string(nucleo.PermOrdenesTomar)); !global {
		zonas, cuadrillas = append(zonas, z...), append(cuadrillas, c...)
	}
	slices.Sort(zonas)
	slices.Sort(cuadrillas)
	return slices.Compact(zonas), slices.Compact(cuadrillas), propia, nil
}

// esTomable: con cuadrilla, solo para los de esa cuadrilla; sin cuadrilla, por la zona.
func esTomable(f campobd.CampoOrden, zonas, cuadrillas []int64) bool {
	if f.CuadrillaID != nil {
		return slices.Contains(cuadrillas, *f.CuadrillaID)
	}
	return f.ZonaID != nil && slices.Contains(zonas, *f.ZonaID)
}

// abiertaParaTomar: estados en que una orden puede estar libre o tomada por otro. En cualquier
// otro (en sitio, pausada, cerrada, fallida, cancelada) tomar responde 404 sin datos.
func abiertaParaTomar(f campobd.CampoOrden) bool {
	return f.Estado == EstadoPendiente || f.Estado == EstadoProgramada || f.Estado == EstadoEnCamino
}

// claveLibre ordena la lista: grupo 0 (con distancia) por metros; grupo 1 por fecha programada
// (o de creación) en microsegundos; desempate por id.
type claveLibre struct {
	grupo int
	valor int64
	id    int64
}

func (a claveLibre) comparar(b claveLibre) int {
	switch {
	case a.grupo != b.grupo:
		return a.grupo - b.grupo
	case a.valor < b.valor:
		return -1
	case a.valor > b.valor:
		return 1
	case a.id < b.id:
		return -1
	case a.id > b.id:
		return 1
	}
	return 0
}

// metros entre dos puntos (haversine, radio medio de la Tierra).
func metros(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000.0
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func (m *Modulo) nuevoOrdenesLibres() *auditoria.Caso[EntradaOrdenesLibres, OrdenesLibres] {
	return auditoria.Nuevo(auditoria.Caso[EntradaOrdenesLibres, OrdenesLibres]{
		Accion: "ordenes.tomar", Permiso: nucleo.PermOrdenesTomar, AuditarExito: soloLecturas[OrdenesLibres],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaOrdenesLibres) (OrdenesLibres, error) {
			salida := OrdenesLibres{Data: []OrdenLibre{}, Zonas: []ZonaLibre{}, Orden: "fecha"}
			actor := contexto.ActorDe(ctx)
			if !actor.EsUsuario() {
				return salida, nucleo.Prohibido("Las órdenes libres son de un técnico")
			}
			q := campobd.New(t)
			ok, err := puedeTomarLibres(ctx, q)
			if err != nil {
				return salida, err
			}
			if !ok {
				return salida, libresApagado()
			}
			var despues claveLibre
			conCursor, err := nucleo.DecodificarCursor(in.Cursor, &despues.grupo, &despues.valor, &despues.id)
			if err != nil {
				return salida, err
			}
			zonas, cuadrillas, _, err := tomables(ctx, q, actor.ID)
			if err != nil {
				return salida, err
			}
			if nz, err := q.NombresDeZonas(ctx, zonas); err == nil {
				for _, z := range nz {
					salida.Zonas = append(salida.Zonas, ZonaLibre{ID: z.ID, Nombre: z.Nombre})
				}
			} else {
				return salida, err
			}
			todas, empresa := filtroEmpresa(ctx)
			filas, err := q.OrdenesLibres(ctx, campobd.OrdenesLibresParams{TodasEmpresas: todas, EmpresaID: empresa, Zonas: zonas, Cuadrillas: cuadrillas})
			if err != nil {
				return salida, err
			}
			salida.Total = len(filas)
			pos, err := q.UltimaPosicionDeTecnico(ctx, campobd.UltimaPosicionDeTecnicoParams{TecnicoID: actor.ID, EmpresaID: empresa})
			conPosicion := err == nil
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return salida, err
			}
			if conPosicion {
				salida.Orden = "distancia"
			}
			type candidata struct {
				fila      campobd.CampoOrden
				clave     claveLibre
				distancia *int64
			}
			cands := make([]candidata, 0, len(filas))
			for _, f := range filas {
				c := candidata{fila: f, clave: claveLibre{grupo: 1, id: f.ID}}
				fecha := f.CreatedAt
				if f.ProgramadaInicio != nil {
					fecha = *f.ProgramadaInicio
				}
				c.clave.valor = fecha.UnixMicro()
				if conPosicion && f.Lat != nil && f.Lon != nil {
					d := int64(math.Round(metros(pos.Lat, pos.Lon, *f.Lat, *f.Lon)))
					c.distancia, c.clave = &d, claveLibre{grupo: 0, valor: d, id: f.ID}
				}
				cands = append(cands, c)
			}
			slices.SortFunc(cands, func(a, b candidata) int { return a.clave.comparar(b.clave) })
			if conCursor {
				i := 0
				for i < len(cands) && cands[i].clave.comparar(despues) <= 0 {
					i++
				}
				cands = cands[i:]
			}
			limite := in.Limite()
			if len(cands) > limite {
				ultima := cands[limite-1].clave
				salida.NextCursor = ptr(nucleo.CodificarCursor(ultima.grupo, ultima.valor, ultima.id))
				cands = cands[:limite]
			}
			pagina := make([]campobd.CampoOrden, 0, len(cands))
			clientes := []int64{}
			for _, c := range cands {
				pagina = append(pagina, c.fila)
				if c.fila.ClienteID != nil {
					clientes = append(clientes, *c.fila.ClienteID)
				}
			}
			cs, err := q.ClientesArgos(ctx, clientes)
			if err != nil {
				return salida, err
			}
			tipos := map[int64]string{}
			for i, c := range cands {
				if _, ok := tipos[c.fila.TipoID]; !ok {
					tipo, err := tipoDe(ctx, q, c.fila)
					if err != nil {
						return salida, err
					}
					tipos[c.fila.TipoID] = tipo.Codigo
				}
				l := OrdenLibre{Orden: aOrden(pagina[i], tipos[c.fila.TipoID]), DistanciaM: c.distancia}
				for _, cl := range cs {
					if c.fila.ClienteID != nil && cl.ID == *c.fila.ClienteID {
						l.ClienteNombre = ptr(cl.Nombre)
					}
				}
				salida.Data = append(salida.Data, l)
			}
			return salida, nil
		},
	})
}

// ordenYaTomada: otro técnico la tomó primero (o despacho la asignó): 409 con quién.
func ordenYaTomada(numero string, id int64, nombre string) *nucleo.Problema {
	p := nucleo.Conflicto("orden_ya_tomada", fmt.Sprintf("%s ya la tomó %s", numero, nombre))
	p.TomadaPor = &nucleo.PersonaProblema{ID: id, Nombre: nombre}
	return p
}

func (m *Modulo) nuevoTomarOrden() *auditoria.Caso[EntradaTomarOrden, Orden] {
	return accionOrden("ordenes.tomar", nucleo.PermOrdenesTomar, sinLote[EntradaTomarOrden],
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaTomarOrden) (campobd.CampoOrden, string, error) {
			ahora := m.ahora()
			actor := contexto.ActorDe(ctx)
			t.FijarEntidad("orden", in.ID)
			if !actor.EsUsuario() {
				return campobd.CampoOrden{}, "", nucleo.Prohibido("Tomar una orden es de un técnico")
			}
			ok, err := puedeTomarLibres(ctx, q)
			if err != nil {
				return campobd.CampoOrden{}, "", err
			}
			if !ok {
				return campobd.CampoOrden{}, "", libresApagado()
			}
			// El candado de la orden ordena a los que llegan a la vez: el segundo ve el técnico del primero.
			antes, err := q.OrdenPorIDParaActualizar(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return antes, "", noEncontrada()
			}
			if err != nil {
				return antes, "", err
			}
			if !enEmpresa(ctx, antes) {
				return antes, "", noEncontrada()
			}
			if propia(ctx, antes) {
				return antes, "", nil // ya es suya (reintento): sin cambios
			}
			// Primero el estado y la zona, luego el técnico: fuera de eso, el mismo 404 que una
			// orden inexistente (ni número ni nombre de quién la tiene).
			if !abiertaParaTomar(antes) {
				return antes, "", noEncontrada()
			}
			zonas, cuadrillas, cuadrillaPropia, err := tomables(ctx, q, actor.ID)
			if err != nil {
				return antes, "", err
			}
			if !esTomable(antes, zonas, cuadrillas) {
				return antes, "", noEncontrada() // fuera de su zona o de su cuadrilla: no revela que existe
			}
			t.FijarEmpresa(deref(antes.EmpresaID))
			if antes.TecnicoID != nil {
				nombre, err := q.NombreDeUsuario(ctx, *antes.TecnicoID)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return antes, "", err
				}
				return antes, "", ordenYaTomada(antes.Numero, *antes.TecnicoID, nombre)
			}
			if err := exigirEstado(antes, "tomar", EstadoPendiente); err != nil {
				return antes, "", err
			}
			bodega := antes.BodegaID
			if b, err := q.BodegaDeTecnico(ctx, actor.ID); err == nil && b != nil {
				bodega = b
			} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return antes, "", err
			}
			// Sin cuadrilla, queda con la del técnico (si tiene).
			cuadrilla := antes.CuadrillaID
			if cuadrilla == nil {
				cuadrilla = cuadrillaPropia
			}
			f, err := m.asignar(ctx, t, q, antes, EntradaAsignarOrden{ID: antes.ID, Version: antes.Version, TecnicoID: &actor.ID,
				CuadrillaID: cuadrilla, BodegaID: bodega}, OrigenTomar, ptr("tomada"), ahora)
			return f, "", err
		})
}

// tomarLibresDe: el paquete y los cambios lo dicen a la app (la hoja del «+» muestra o no la opción).
func tomarLibresDe(ctx context.Context, q *campobd.Queries) bool {
	ok, err := puedeTomarLibres(ctx, q)
	return err == nil && ok
}
