package argos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Inventario del técnico (contrato Iris ↔ ArgosISP 0.5.0 §3.3): técnicos de Argos con bodega,
// para vincular la ficha de agenda de cada técnico de Iris con su usuario de Argos (id_externo).

// IDExterno es el id de un usuario de Argos: texto ("u-12") o número en el JSON; Iris lo guarda y lo
// manda como texto.
type IDExterno string

// UnmarshalJSON acepta "u-12" o 12.
func (i *IDExterno) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if string(b) == "null" {
		*i = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*i = IDExterno(strings.TrimSpace(s))
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*i = IDExterno(n.String())
	return nil
}

// BodegaTecnicoArgos es una bodega de tipo técnico de un usuario de Argos.
type BodegaTecnicoArgos struct {
	ID        int64 `json:"id"`
	EmpresaID int64 `json:"empresa_id"`
	Activa    *bool `json:"activa"`
}

// EsActiva: activa = true o no informado (Argos solo lista las del alcance).
func (b BodegaTecnicoArgos) EsActiva() bool { return b.Activa == nil || *b.Activa }

// TecnicoArgos es un usuario de Argos con bodega técnico (GET /inventario/tecnicos).
type TecnicoArgos struct {
	IDExterno IDExterno            `json:"id_externo"`
	Correo    string               `json:"correo"`
	Nombre    string               `json:"nombre"`
	Bodegas   []BodegaTecnicoArgos `json:"bodegas"`
}

// BodegaActivaEn devuelve la bodega activa del técnico en la empresa (0 = cualquiera).
func (t TecnicoArgos) BodegaActivaEn(empresaID int64) (BodegaTecnicoArgos, bool) {
	for _, b := range t.Bodegas {
		if b.EsActiva() && (empresaID == 0 || b.EmpresaID == empresaID) {
			return b, true
		}
	}
	return BodegaTecnicoArgos{}, false
}

// TecnicosInventario: GET /inventario/tecnicos?cursor=&limit= (permiso inventario.ver en Argos).
func (c *Cliente) TecnicosInventario(ctx context.Context, cursor string, limite int) (Pagina[TecnicoArgos], error) {
	v := url.Values{"limit": {strconv.Itoa(limite)}}
	if cursor != "" {
		v.Set("cursor", cursor)
	}
	return conector.ListaDe[TecnicoArgos](ctx, c.Cliente, "/inventario/tecnicos", v)
}

// Errores del inventario por técnico (contrato 0.5.0 §3.3).
var (
	// ErrSinInventarioTecnico: Argos anterior a 1.6.0 (o versión aún desconocida).
	ErrSinInventarioTecnico = errors.New("argos: inventario por técnico no disponible (Argos < 1.6.0)")
	// ErrTecnicoNoEncontrado: el id_externo no está entre los técnicos con bodega de Argos.
	ErrTecnicoNoEncontrado = errors.New("argos: técnico no encontrado")
)

// SinInventarioTecnico es el 422 que ve quien pide algo del inventario por técnico con Argos < 1.6.0.
func SinInventarioTecnico() *nucleo.Problema {
	return nucleo.ReglaNegocio("argos_sin_inventario_tecnico",
		"Argos < "+VersionInventarioTecnico+": inventario por técnico no disponible (actualiza Argos; mientras tanto se usa la bodega de la ficha)")
}

// clienteTecnicos: el cliente del conector activo, solo si Argos es ≥ 1.6.0.
func (m *Modulo) clienteTecnicos(ctx context.Context, q conector.DBTX) (*Cliente, error) {
	c, err := m.conectorActivo(ctx, q)
	if err != nil {
		return nil, err
	}
	if !SoportaInventarioTecnico(c.VersionRemota) {
		return nil, ErrSinInventarioTecnico
	}
	return m.clienteDe(c), nil
}

// maxPaginasTecnicos acota la búsqueda de un técnico por id (100 por página).
const maxPaginasTecnicos = 50

// BuscarTecnico busca un técnico de Argos por id_externo recorriendo GET /inventario/tecnicos (sin
// transacción). ErrSinInventarioTecnico, ErrTecnicoNoEncontrado, 503 conector_apagado o el error de
// Argos.
func (m *Modulo) BuscarTecnico(ctx context.Context, idExterno string) (TecnicoArgos, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return TecnicoArgos{}, nucleo.ErrSinBD
	}
	cli, err := m.clienteTecnicos(ctx, bd)
	if err != nil {
		return TecnicoArgos{}, err
	}
	cursor := ""
	for range maxPaginasTecnicos {
		p, err := cli.TecnicosInventario(ctx, cursor, 100)
		if err != nil {
			return TecnicoArgos{}, err
		}
		for _, t := range p.Data {
			if string(t.IDExterno) == idExterno {
				return t, nil
			}
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	return TecnicoArgos{}, ErrTecnicoNoEncontrado
}

// Selector de técnicos de Argos (GET /integraciones/argosisp/tecnicos) -----------------------

// EntradaTecnicosArgos: página de los técnicos de Argos.
type EntradaTecnicosArgos struct {
	Cursor string `query:"cursor" maxLength:"500" doc:"nextCursor de la página anterior (de Argos)"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

// BodegaTecnico es una bodega técnico de un usuario de Argos.
type BodegaTecnico struct {
	ID        int64 `json:"id" doc:"Id de la bodega en Argos"`
	EmpresaID int64 `json:"empresaId"`
	Activa    bool  `json:"activa"`
}

// VinculoIris: el técnico de Iris ya vinculado a ese usuario de Argos.
type VinculoIris struct {
	TecnicoID int64 `json:"tecnicoId" doc:"Ficha de agenda"`
	UsuarioID int64 `json:"usuarioId"`
}

// TecnicoDeArgos es un usuario de Argos con bodega técnico, para el selector del vínculo.
type TecnicoDeArgos struct {
	IDExterno string          `json:"idExterno" doc:"Id del usuario en Argos (lo que se manda como tecnico.id_externo)"`
	Correo    string          `json:"correo"`
	Nombre    string          `json:"nombre"`
	Bodegas   []BodegaTecnico `json:"bodegas" doc:"Sus bodegas técnico en las empresas que ves (la activa; todas con empresas.todas). Un usuario sin bodega en ellas no aparece"`
	// EnEmpresa: tiene bodega activa en la empresa activa (sin empresa activa: en alguna).
	EnEmpresa  bool         `json:"enEmpresa" doc:"Tiene bodega técnico activa en la empresa activa: solo así se puede vincular"`
	VinculadoA *VinculoIris `json:"vinculadoA,omitempty" doc:"Ficha de Iris que ya lo tiene vinculado (ausente = libre)"`
}

// PaginaTecnicosArgos es la página del selector.
type PaginaTecnicosArgos struct {
	Data       []TecnicoDeArgos `json:"data"`
	NextCursor *string          `json:"nextCursor" nullable:"true"`
}

type entradaTecnicosFinal struct {
	EntradaTecnicosArgos
	pagina Pagina[TecnicoArgos]
	err    error
}

// ListarTecnicosArgos: fase 1 (permiso y conector ≥ 1.6.0), llamada a Argos fuera de transacción y
// fase 2 (marca los ya vinculados, audita la lectura de datos personales).
type ListarTecnicosArgos struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaTecnicosArgos, conector.Leido]
	final  *auditoria.Caso[entradaTecnicosFinal, PaginaTecnicosArgos]
}

func nuevoListarTecnicosArgos(m *Modulo) *ListarTecnicosArgos {
	l := &ListarTecnicosArgos{m: m}
	l.previo = auditoria.Nuevo(auditoria.Caso[EntradaTecnicosArgos, conector.Leido]{
		Accion: "agenda.ver_tecnicos_argos", Permiso: nucleo.PermAgendaVincularArgos, AuditarExito: soloLecturas[conector.Leido],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ EntradaTecnicosArgos) (conector.Leido, error) {
			c, err := m.conectorActivo(ctx, t)
			if err != nil {
				return c, err
			}
			if !SoportaInventarioTecnico(c.VersionRemota) {
				return c, SinInventarioTecnico()
			}
			return c, nil
		},
	})
	l.final = auditoria.Nuevo(auditoria.Caso[entradaTecnicosFinal, PaginaTecnicosArgos]{
		Accion: "agenda.ver_tecnicos_argos", Permiso: nucleo.PermAgendaVincularArgos,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaTecnicosFinal) (PaginaTecnicosArgos, error) {
			if in.err != nil {
				return PaginaTecnicosArgos{}, Problema(in.err)
			}
			// Empresas visibles: la activa o, con empresas.todas global, todas. Un usuario de Argos sin
			// bodega en ellas no se muestra (nombre, correo y bodegas de otra empresa), ni un vínculo de
			// una ficha de otra empresa.
			empresa := contexto.EmpresaDe(ctx)
			todas := contexto.PermisosDe(ctx).TieneGlobal(string(nucleo.PermEmpresasTodas))
			visible := func(e int64) bool { return todas || (empresa != 0 && e == empresa) }
			ids := make([]string, 0, len(in.pagina.Data))
			for _, x := range in.pagina.Data {
				ids = append(ids, string(x.IDExterno))
			}
			vinculados, err := argosbd.New(t).TecnicosVinculadosArgos(ctx, argosbd.TecnicosVinculadosArgosParams{Ids: ids, Todas: todas, EmpresaID: empresa})
			if err != nil {
				return PaginaTecnicosArgos{}, err
			}
			r := PaginaTecnicosArgos{Data: make([]TecnicoDeArgos, 0, len(in.pagina.Data))}
			for _, x := range in.pagina.Data {
				d := TecnicoDeArgos{IDExterno: string(x.IDExterno), Correo: x.Correo, Nombre: x.Nombre, Bodegas: []BodegaTecnico{}}
				if d.IDExterno == "" {
					continue
				}
				for _, b := range x.Bodegas {
					if visible(b.EmpresaID) {
						d.Bodegas = append(d.Bodegas, BodegaTecnico{ID: b.ID, EmpresaID: b.EmpresaID, Activa: b.EsActiva()})
					}
				}
				if len(d.Bodegas) == 0 {
					continue
				}
				_, d.EnEmpresa = x.BodegaActivaEn(empresa)
				for _, v := range vinculados {
					if v.IDExterno == d.IDExterno {
						d.VinculadoA = &VinculoIris{TecnicoID: v.ID, UsuarioID: v.UsuarioID}
					}
				}
				r.Data = append(r.Data, d)
			}
			if in.pagina.NextCursor != "" {
				r.NextCursor = &in.pagina.NextCursor
			}
			t.Anotar(map[string]any{"leidos": len(r.Data)})
			return r, nil
		},
	})
	return l
}

// Ejecutar lee una página de técnicos de Argos.
func (l *ListarTecnicosArgos) Ejecutar(ctx context.Context, in EntradaTecnicosArgos) (PaginaTecnicosArgos, error) {
	c, err := l.previo.Ejecutar(ctx, in)
	if err != nil {
		return PaginaTecnicosArgos{}, err
	}
	limite := in.Limit
	if limite <= 0 {
		limite = 50
	}
	f := entradaTecnicosFinal{EntradaTecnicosArgos: in}
	f.pagina, f.err = l.m.clienteDe(c).TecnicosInventario(ctx, in.Cursor, limite)
	return l.final.Ejecutar(ctx, f)
}

// Leer la bodega de un técnico (para la app de campo, I4) -------------------------------------

// RefTecnico es `tecnico` en el contrato: { id_externo } o { correo }.
type RefTecnico struct {
	IDExterno string `json:"id_externo,omitempty"`
	Correo    string `json:"correo,omitempty"`
}

func (r RefTecnico) consulta() url.Values {
	if r.IDExterno != "" {
		return url.Values{"id_externo": {r.IDExterno}}
	}
	return url.Values{"correo": {r.Correo}}
}

// MiBodega: GET /inventario/mi-bodega?id_externo=|correo= con X-Empresa-Id (≠ 0). Se lee crudo (lo
// interpreta quien llama). 404 tecnico_sin_bodega si no tiene bodega activa.
func (c *Cliente) MiBodega(ctx context.Context, t RefTecnico, empresaID int64, destino any) error {
	cli := c.Cliente
	if empresaID != 0 {
		cli = cli.ConCabecera(CabeceraEmpresaArgos, strconv.FormatInt(empresaID, 10))
	}
	return cli.Pedir(ctx, http.MethodGet, "/inventario/mi-bodega", t.consulta(), nil, true, destino)
}
