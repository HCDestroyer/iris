package campo

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Evidencias del trabajo (permiso ordenes.registrar): checklist, notas, materiales, equipos,
// firma y adjuntos. Cada una sube la versión de la orden (el teléfono y el panel se enteran por
// el feed). Checklist, materiales, equipos y firma solo con la orden en curso (programada, en
// camino o en sitio); notas y adjuntos en cualquier estado (también cancelada: evidencia).

// Acciones de equipo.
const (
	AccionInstalar = "instalar"
	AccionRetirar  = "retirar"
)

// EntradaMarcarChecklist es la entrada de ordenes.marcar_checklist.
type EntradaMarcarChecklist struct {
	ID           int64      `json:"-"`
	Version      *int32     `json:"version,omitempty" minimum:"1" doc:"Versión leída (opcional); si cambió, 409"`
	ItemID       *int64     `json:"itemId,omitempty" minimum:"1" doc:"Ítem de un formulario de la orden (identifica su formulario); o codigo"`
	Codigo       *string    `json:"codigo,omitempty" maxLength:"60" doc:"Código del ítem (o itemId); si se repite en varios formularios, con formularioId"`
	FormularioID *uuid.UUID `json:"formularioId,omitempty" doc:"Formulario de la orden (opcional con itemId)"`
	Valor        any        `json:"valor" doc:"si_no: boolean; texto: string; seleccion: una de sus opciones; numero y medicion: number; foto: uuid del adjunto"`
	Sync         *OpSync    `json:"-"`
}

// EntradaAgregarNota es la entrada de ordenes.agregar_nota.
type EntradaAgregarNota struct {
	ID      int64   `json:"-"`
	Version *int32  `json:"version,omitempty" minimum:"1" doc:"Versión leída (opcional); si cambió, 409"`
	Texto   string  `json:"texto" minLength:"1" maxLength:"4000"`
	Sync    *OpSync `json:"-"`
}

// EntradaUsarMaterial es la entrada de ordenes.usar_material.
type EntradaUsarMaterial struct {
	ID          int64   `json:"-"`
	Version     *int32  `json:"version,omitempty" minimum:"1" doc:"Versión leída (opcional); si cambió, 409"`
	ProductoID  int64   `json:"productoId" minimum:"1" doc:"Producto en Argos"`
	Cantidad    float64 `json:"cantidad" exclusiveMinimum:"0" maximum:"1000000" doc:"Hasta 3 decimales (metros de cable, unidades…)"`
	BodegaID    *int64  `json:"bodegaId,omitempty" minimum:"1" doc:"Otra bodega en Argos: solo despacho o supervisor (ordenes.asignar con alcance), validada contra Argos; nunca desde el lote. Por defecto, la de la orden"`
	Descripcion string  `json:"descripcion,omitempty" maxLength:"200"`
	Sync        *OpSync `json:"-"`

	pre *prelectura // lo leído de Argos antes de la transacción
}

// EntradaEquipo es la entrada de ordenes.instalar_equipo y ordenes.retirar_equipo.
type EntradaEquipo struct {
	ID        int64   `json:"-"`
	Version   *int32  `json:"version,omitempty" minimum:"1" doc:"Versión leída (opcional); si cambió, 409"`
	UnidadID  *int64  `json:"unidadId,omitempty" minimum:"1" doc:"Unidad en el inventario de Argos (o serie)"`
	Serie     *string `json:"serie,omitempty" maxLength:"100" doc:"Número de serie (o unidadId)"`
	MAC       *string `json:"mac,omitempty" maxLength:"30" doc:"Se normaliza a AA:BB:CC:DD:EE:FF"`
	Modelo    string  `json:"modelo,omitempty" maxLength:"100"`
	Condicion *string `json:"condicion,omitempty" enum:"buena,danada,perdido" doc:"Obligatoria al retirar; no va al instalar"`
	Sync      *OpSync `json:"-"`

	pre *prelectura // lo leído de Argos antes de la transacción
}

// EntradaCapturarFirma es la entrada de ordenes.capturar_firma.
type EntradaCapturarFirma struct {
	ID        int64      `json:"-"`
	Version   *int32     `json:"version,omitempty" minimum:"1" doc:"Versión leída (opcional); si cambió, 409"`
	Nombre    string     `json:"nombre" minLength:"2" maxLength:"120" doc:"Quien firma"`
	Documento string     `json:"documento,omitempty" maxLength:"40" doc:"DPI u otro documento de quien firma"`
	SVG       string     `json:"svg" minLength:"10" maxLength:"200000" doc:"Trazo de la firma en SVG" auditoria:"sensible"`
	AdjuntoID *uuid.UUID `json:"adjuntoId,omitempty" doc:"PNG de la firma (adjunto)"`
	Sync      *OpSync    `json:"-"`
}

// EntradaRegistrarAdjunto es la entrada de ordenes.registrar_adjunto.
type EntradaRegistrarAdjunto struct {
	ID          int64     `json:"-"`
	Version     *int32    `json:"version,omitempty" minimum:"1" doc:"Versión leída (opcional); si cambió, 409"`
	AdjuntoID   uuid.UUID `json:"adjuntoId" doc:"uuid del adjunto (POST /adjuntos, de I1); basta con que la subida esté creada"`
	Tipo        string    `json:"tipo" enum:"foto,firma,documento"`
	ItemID      *int64    `json:"itemId,omitempty" minimum:"1" doc:"Ítem foto del checklist al que corresponde"`
	Descripcion string    `json:"descripcion,omitempty" maxLength:"500"`
	Sync        *OpSync   `json:"-"`
}

func (e EntradaMarcarChecklist) ordenID() int64  { return e.ID }
func (e EntradaAgregarNota) ordenID() int64      { return e.ID }
func (e EntradaUsarMaterial) ordenID() int64     { return e.ID }
func (e EntradaEquipo) ordenID() int64           { return e.ID }
func (e EntradaCapturarFirma) ordenID() int64    { return e.ID }
func (e EntradaRegistrarAdjunto) ordenID() int64 { return e.ID }

// estadosEnCurso: la orden admite checklist, materiales, equipos y firma (también pausada:
// sigue abierta y el técnico puede dejar evidencia).
var estadosEnCurso = []string{EstadoProgramada, EstadoEnCamino, EstadoEnSitio, EstadoPausada}

// idDeEvidencia: el id de la operación del lote (el cliente ya lo conoce) o uno v7 nuevo.
func idDeEvidencia(s *OpSync) (uuid.UUID, error) {
	if s != nil {
		return s.ID, nil
	}
	return uuid.NewV7()
}

func operacionDe(s *OpSync) *uuid.UUID {
	if s == nil {
		return nil
	}
	return &s.ID
}

// tocar sube la versión de la orden tras registrar una evidencia (y alimenta el feed).
func tocar(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden) (campobd.CampoOrden, error) {
	return guardar(ctx, t, q, f, f)
}

func (m *Modulo) nuevoMarcarChecklist() *auditoria.Caso[EntradaMarcarChecklist, Orden] {
	return accionOrden("ordenes.marcar_checklist", nucleo.PermOrdenesRegistrar, func(e EntradaMarcarChecklist) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaMarcarChecklist) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrar, in.Version, in.Sync)
			if err != nil {
				return f, "", err
			}
			cuando := m.cuandoEn(in.Sync, f)
			if err := exigirEstado(f, "marcar el checklist de", estadosEnCurso...); err != nil {
				return f, "", err
			}
			item, fo, err := itemDeOrden(ctx, q, f, in.ItemID, in.Codigo, in.FormularioID)
			if err != nil {
				return f, "", err
			}
			valor, err := validarValor(item, in.Valor)
			if err != nil {
				return f, "", err
			}
			// Una foto del checklist es un adjunto de esta orden (en el módulo de adjuntos).
			if item.Tipo == ItemFoto {
				if err := m.cfg.Adjuntos.AdjuntoDeOrden(ctx, uuid.MustParse(in.Valor.(string)), f.ID); err != nil {
					return f, "", err
				}
			}
			// Gana la marca más reciente por ocurrido_en (empate: la que llega después); la otra
			// queda en el historial (contratos/sync.md §3).
			previa, err := q.MarcaChecklist(ctx, campobd.MarcaChecklistParams{FormularioID: fo.ID, ItemID: item.ID})
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return f, "", err
			}
			vigente := errors.Is(err, pgx.ErrNoRows) || !cuando.Before(previa.OcurridoEn)
			canal := string(contexto.CanalDe(ctx))
			if err := q.CrearHistorialChecklist(ctx, campobd.CrearHistorialChecklistParams{
				FormularioID: fo.ID, OrdenID: f.ID, ItemID: item.ID, Valor: valor, MarcadoPor: actorUsuario(t), Canal: canal,
				OcurridoEn: cuando, OperacionID: operacionDe(in.Sync), Vigente: vigente,
			}); err != nil {
				return f, "", err
			}
			aviso := AvisoChecklistNoVigente
			if vigente {
				aviso = ""
				if err := q.GuardarMarcaChecklist(ctx, campobd.GuardarMarcaChecklistParams{
					FormularioID: fo.ID, OrdenID: f.ID, ItemID: item.ID, Valor: valor, MarcadoPor: actorUsuario(t), Canal: canal,
					OcurridoEn: cuando, OperacionID: operacionDe(in.Sync),
				}); err != nil {
					return f, "", err
				}
				t.RegistrarCambios("orden_checklist", fmt.Sprintf("%d:%s:%s", f.ID, fo.ID, item.Codigo),
					map[string]any{"valor": string(previa.Valor)}, map[string]any{"valor": string(valor)})
			}
			t.Anotar(map[string]any{"item": item.Codigo, "formulario_id": fo.ID, "vigente": vigente})
			f, err = tocar(ctx, t, q, f)
			return f, aviso, err
		})
}

// validarValor comprueba el valor según el tipo del ítem y lo devuelve como JSON.
func validarValor(item campobd.CampoPlantillaItem, valor any) ([]byte, error) {
	invalido := func(msj string) error { return campoInvalido("valor", msj, item.Tipo) }
	switch item.Tipo {
	case ItemSiNo:
		if _, ok := valor.(bool); !ok {
			return nil, invalido("Debe ser verdadero o falso")
		}
	case ItemTexto:
		s, ok := valor.(string)
		if !ok {
			return nil, invalido("Debe ser texto")
		}
		v, err := texto("valor", s, 1, 2000)
		if err != nil {
			return nil, err
		}
		valor = v
	case ItemNumero, ItemMedicion:
		n, ok := valor.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, invalido("Debe ser un número")
		}
	case ItemSeleccion:
		s, ok := valor.(string)
		if !ok || !slices.Contains(opcionesDe(item.Opciones), s) {
			return nil, invalido("Debe ser una de las opciones del ítem")
		}
	case ItemFoto:
		s, ok := valor.(string)
		if !ok {
			return nil, invalido("Debe ser el uuid del adjunto")
		}
		u, err := uuid.Parse(s)
		if err != nil {
			return nil, invalido("Debe ser el uuid del adjunto")
		}
		valor = u.String()
	}
	return json.Marshal(valor)
}

func (m *Modulo) nuevoAgregarNota() *auditoria.Caso[EntradaAgregarNota, Orden] {
	return accionOrden("ordenes.agregar_nota", nucleo.PermOrdenesRegistrar, func(e EntradaAgregarNota) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaAgregarNota) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrar, in.Version, in.Sync)
			if err != nil {
				return f, "", err
			}
			cuerpo, err := texto("texto", in.Texto, 1, 4000)
			if err != nil {
				return f, "", err
			}
			id, err := idDeEvidencia(in.Sync)
			if err != nil {
				return f, "", err
			}
			n, err := q.CrearNota(ctx, campobd.CrearNotaParams{
				ID: id, OrdenID: f.ID, Texto: cuerpo, AutorID: actorUsuario(t), AutorNombre: t.Actor().Nombre,
				Canal: string(contexto.CanalDe(ctx)), OcurridoEn: m.cuandoEn(in.Sync, f), OperacionID: operacionDe(in.Sync),
			})
			if err != nil {
				return f, "", err
			}
			t.RegistrarCambios("orden_nota", n.ID, nil, n)
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}

func (m *Modulo) nuevoUsarMaterial() *auditoria.Caso[EntradaUsarMaterial, Orden] {
	return accionOrden("ordenes.usar_material", nucleo.PermOrdenesRegistrar, func(e EntradaUsarMaterial) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaUsarMaterial) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrar, in.Version, in.Sync)
			if err != nil {
				return f, "", err
			}
			if err := exigirEstado(f, "registrar materiales en", estadosEnCurso...); err != nil {
				return f, "", err
			}
			if in.ProductoID <= 0 {
				return f, "", campoObligatorio("productoId")
			}
			cantidad := math.Round(in.Cantidad*1000) / 1000
			if !(cantidad > 0) || cantidad > 1_000_000 {
				return f, "", campoInvalido("cantidad", "Debe ser mayor que 0 (hasta 3 decimales)", "rango")
			}
			// Siempre la bodega de la orden (la del técnico). Otra solo la indica despacho o
			// supervisor y debe existir en la empresa según Argos.
			bodega := f.BodegaID
			if in.BodegaID != nil && !mismoID(in.BodegaID, f.BodegaID) {
				switch {
				case in.Sync != nil:
					return f, "", campoInvalido("bodegaId", "Desde la app se usa la bodega de la orden", "bodega")
				case !cubre(ctx, nucleo.PermOrdenesAsignar, f):
					return f, "", nucleo.Prohibido("Solo despacho o supervisor (ordenes.asignar) indican otra bodega")
				case in.pre == nil || !in.pre.bodegaValida || in.pre.bodega != *in.BodegaID || in.pre.empresa != deref(f.EmpresaID):
					return f, "", nucleo.ReglaNegocio("bodega_invalida", "La bodega no existe en la empresa según Argos (o no hay conector para verificarla)")
				}
				bodega = in.BodegaID
			}
			// Más que la existencia de la bodega (según Argos, leída antes por el lote): se acepta
			// con aviso; el consumo en Argos queda pendiente con error (contratos/sync.md §3).
			excede := false
			if in.Sync != nil && in.Sync.Existencia != nil {
				usado, err := q.CantidadUsada(ctx, campobd.CantidadUsadaParams{OrdenID: f.ID, ProductoID: in.ProductoID})
				if err != nil {
					return f, "", err
				}
				excede = numero(usado)+cantidad > *in.Sync.Existencia+1e-9
			}
			id, err := idDeEvidencia(in.Sync)
			if err != nil {
				return f, "", err
			}
			mat, err := q.CrearMaterial(ctx, campobd.CrearMaterialParams{
				ID: id, OrdenID: f.ID, ProductoID: in.ProductoID, Descripcion: strings.TrimSpace(in.Descripcion),
				Cantidad: aNumeric(cantidad), BodegaID: bodega, ExcedeExistencia: excede, RegistradoPor: actorUsuario(t),
				Canal: string(contexto.CanalDe(ctx)), OcurridoEn: m.cuandoEn(in.Sync, f), OperacionID: operacionDe(in.Sync),
				Modalidad: modalidadDe(in.Sync),
			})
			if err != nil {
				return f, "", err
			}
			t.RegistrarCambios("orden_material", mat.ID, nil, map[string]any{
				"producto_id": mat.ProductoID, "cantidad": cantidad, "bodega_id": mat.BodegaID, "excede_existencia": excede,
			})
			f, err = tocar(ctx, t, q, f)
			if excede {
				return f, AvisoExistenciaInsuficiente, err
			}
			return f, "", err
		})
}

// normalizarMAC acepta AA:BB:…, AA-BB-…, AABB.CCDD.EEFF o 12 dígitos hexadecimales.
func normalizarMAC(crudo string) (string, bool) {
	s := strings.TrimSpace(crudo)
	if len(s) == 12 {
		if _, err := hex.DecodeString(s); err == nil {
			var b strings.Builder
			for i := 0; i < 12; i += 2 {
				if i > 0 {
					b.WriteByte(':')
				}
				b.WriteString(s[i : i+2])
			}
			s = b.String()
		}
	}
	hw, err := net.ParseMAC(s)
	if err != nil || len(hw) != 6 {
		return "", false
	}
	return strings.ToUpper(hw.String()), true
}

func (m *Modulo) nuevoEquipo(accionEquipo string) *auditoria.Caso[EntradaEquipo, Orden] {
	accion := "ordenes.instalar_equipo"
	if accionEquipo == AccionRetirar {
		accion = "ordenes.retirar_equipo"
	}
	return accionOrden(accion, nucleo.PermOrdenesRegistrar, func(e EntradaEquipo) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaEquipo) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrar, in.Version, in.Sync)
			if err != nil {
				return f, "", err
			}
			if err := exigirEstado(f, "registrar equipos en", estadosEnCurso...); err != nil {
				return f, "", err
			}
			serie, err := textoOpcional("serie", in.Serie, 100)
			if err != nil {
				return f, "", err
			}
			if serie != nil {
				s := strings.ToUpper(*serie)
				serie = &s
			}
			if serie == nil && in.UnidadID == nil {
				return f, "", campoInvalido("serie", "Indica la serie o la unidad de Argos", "required")
			}
			var mac *string
			if in.MAC != nil && strings.TrimSpace(*in.MAC) != "" {
				v, ok := normalizarMAC(*in.MAC)
				if !ok {
					return f, "", campoInvalido("mac", "MAC inválida", "mac")
				}
				mac = &v
			}
			condicion := in.Condicion
			switch {
			case accionEquipo == AccionRetirar && condicion == nil:
				return f, "", campoObligatorio("condicion")
			case accionEquipo == AccionInstalar && condicion != nil:
				return f, "", campoInvalido("condicion", "Solo al retirar", "condicion")
			case condicion != nil && *condicion != "buena" && *condicion != "danada" && *condicion != "perdido":
				return f, "", campoInvalido("condicion", "Condición desconocida", "enum")
			}
			// Coherente con el tipo: se instala en instalación y cambio de equipo; se retira en
			// retiro y cambio de equipo.
			tipo, err := tipoDe(ctx, q, f)
			if err != nil {
				return f, "", err
			}
			if (accionEquipo == AccionInstalar && !tipo.ExigeInstalar) || (accionEquipo == AccionRetirar && !tipo.ExigeRetirar) {
				return f, "", nucleo.ReglaNegocio("equipo_no_admitido",
					fmt.Sprintf("Una orden de %s no admite %s equipos", tipo.Nombre, accionEquipo))
			}
			// Con conector, lo retirado debe ser un equipo en comodato del servicio (por unidad o
			// serie). Sin conector (o Argos caído) se acepta como dato no verificado.
			validado := false
			if accionEquipo == AccionRetirar && in.pre != nil && in.pre.comodatoConocido && f.ServicioID != nil && in.pre.servicio == *f.ServicioID {
				coincide := false
				for _, eq := range in.pre.comodato {
					if (in.UnidadID != nil && eq.UnidadID == *in.UnidadID) || (serie != nil && strings.EqualFold(eq.Serie, *serie)) {
						coincide = true
					}
				}
				if !coincide {
					return f, "", nucleo.ReglaNegocio("equipo_no_en_comodato", "El equipo no está en comodato en el servicio de la orden según Argos")
				}
				validado = true
			}
			id, err := idDeEvidencia(in.Sync)
			if err != nil {
				return f, "", err
			}
			eq, err := q.CrearEquipo(ctx, campobd.CrearEquipoParams{
				ID: id, OrdenID: f.ID, Accion: accionEquipo, UnidadID: in.UnidadID, Serie: serie, Mac: mac,
				Modelo: strings.TrimSpace(in.Modelo), Condicion: condicion, Validado: validado, RegistradoPor: actorUsuario(t),
				Canal: string(contexto.CanalDe(ctx)), OcurridoEn: m.cuandoEn(in.Sync, f), OperacionID: operacionDe(in.Sync),
			})
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return f, "", nucleo.ReglaNegocio("equipo_duplicado", "Ese equipo ya está registrado en la orden")
			}
			if err != nil {
				return f, "", err
			}
			t.RegistrarCambios("orden_equipo", eq.ID, nil, eq)
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}

func (m *Modulo) nuevoCapturarFirma() *auditoria.Caso[EntradaCapturarFirma, Orden] {
	return accionOrden("ordenes.capturar_firma", nucleo.PermOrdenesRegistrar, func(e EntradaCapturarFirma) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaCapturarFirma) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrar, in.Version, in.Sync)
			if err != nil {
				return f, "", err
			}
			if err := exigirEstado(f, "capturar la firma de", estadosEnCurso...); err != nil {
				return f, "", err
			}
			nombre, err := texto("nombre", in.Nombre, 2, 120)
			if err != nil {
				return f, "", err
			}
			documento := strings.TrimSpace(in.Documento)
			svg := strings.TrimSpace(in.SVG)
			if !strings.HasPrefix(svg, "<svg") && !strings.HasPrefix(svg, "<?xml") {
				return f, "", campoInvalido("svg", "Debe ser un SVG", "svg")
			}
			if len(svg) > 200_000 {
				return f, "", campoInvalido("svg", "La firma es demasiado grande", "length")
			}
			if _, err := q.GuardarFirma(ctx, campobd.GuardarFirmaParams{
				OrdenID: f.ID, Nombre: nombre, Documento: documento, Svg: svg, AdjuntoID: in.AdjuntoID,
				CapturadaPor: actorUsuario(t), Canal: string(contexto.CanalDe(ctx)), OcurridoEn: m.cuandoEn(in.Sync, f),
				OperacionID: operacionDe(in.Sync),
			}); err != nil {
				return f, "", err
			}
			// El trazo nunca va a la auditoría.
			t.RegistrarCambios("firma", f.ID, nil, map[string]any{"nombre": nombre, "firma": "***", "adjunto_id": in.AdjuntoID})
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}

func (m *Modulo) nuevoRegistrarAdjunto() *auditoria.Caso[EntradaRegistrarAdjunto, Orden] {
	return accionOrden("ordenes.registrar_adjunto", nucleo.PermOrdenesRegistrar, func(e EntradaRegistrarAdjunto) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaRegistrarAdjunto) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrar, in.Version, in.Sync)
			if err != nil {
				return f, "", err
			}
			if in.AdjuntoID == uuid.Nil {
				return f, "", campoObligatorio("adjuntoId")
			}
			if in.Tipo != "foto" && in.Tipo != "firma" && in.Tipo != "documento" {
				return f, "", campoInvalido("tipo", "Tipo de adjunto desconocido", "enum")
			}
			descripcion, err := textoOpcional("descripcion", &in.Descripcion, 500)
			if err != nil {
				return f, "", err
			}
			if in.ItemID != nil {
				item, _, err := itemDeOrden(ctx, q, f, in.ItemID, nil, nil)
				if err != nil {
					return f, "", err
				}
				if item.Tipo != ItemFoto {
					return f, "", campoInvalido("itemId", "El ítem no es de tipo foto", "item")
				}
			}
			// El adjunto existe en el módulo de I1 y es de esta orden (basta con la subida creada).
			if err := m.cfg.Adjuntos.AdjuntoDeOrden(ctx, in.AdjuntoID, f.ID); err != nil {
				return f, "", err
			}
			previo, err := q.AdjuntoRegistrado(ctx, in.AdjuntoID)
			switch {
			case err == nil && previo.OrdenID != f.ID:
				return f, "", nucleo.ReglaNegocio("adjunto_de_otra_orden", "El adjunto ya está registrado en otra orden")
			case err == nil:
				t.Anotar(map[string]any{"ya_registrado": true})
				return f, "", nil // ya estaba: nada que cambiar
			case !errors.Is(err, pgx.ErrNoRows):
				return f, "", err
			}
			d := ""
			if descripcion != nil {
				d = *descripcion
			}
			a, err := q.RegistrarAdjunto(ctx, campobd.RegistrarAdjuntoParams{
				ID: in.AdjuntoID, OrdenID: f.ID, Tipo: in.Tipo, ItemID: in.ItemID, Descripcion: d,
				RegistradoPor: actorUsuario(t), Canal: string(contexto.CanalDe(ctx)), OcurridoEn: m.cuandoEn(in.Sync, f),
				OperacionID: operacionDe(in.Sync),
			})
			if err != nil {
				return f, "", err
			}
			t.RegistrarCambios("orden_adjunto", a.ID, nil, a)
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}

// prelectura: lo que una acción necesita de Argos, leído antes de abrir la transacción (sin
// llamadas HTTP dentro de ella). El caso de uso lo vuelve a cruzar con la orden ya tomada.
type prelectura struct {
	empresa          int64
	bodega           int64
	bodegaValida     bool
	servicio         int64
	comodato         []EquipoComodato
	comodatoConocido bool
}

// leerOrden lee la orden sin caso de uso ni transacción, solo para preparar lecturas de Argos
// (la acción vuelve a verificar todo con la orden tomada).
func leerOrden(ctx context.Context, id int64) (campobd.CampoOrden, bool) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return campobd.CampoOrden{}, false
	}
	f, err := campobd.New(bd).OrdenPorID(ctx, id)
	return f, err == nil
}

// comodatoDe lee de Argos los equipos en comodato del servicio de la orden.
func (m *Modulo) comodatoDe(ctx context.Context, empresa int64, servicio *int64) *prelectura {
	if m.cfg.Inventario == nil || servicio == nil {
		return nil
	}
	eq, err := m.cfg.Inventario.EquiposEnComodato(ctx, empresa, *servicio)
	if err != nil {
		return nil
	}
	return &prelectura{empresa: empresa, servicio: *servicio, comodato: eq, comodatoConocido: true}
}

// bodegaDe verifica en Argos que la bodega exista en la empresa.
func (m *Modulo) bodegaDe(ctx context.Context, empresa, bodega int64) *prelectura {
	p := &prelectura{empresa: empresa, bodega: bodega}
	if m.cfg.Inventario != nil {
		ok, err := m.cfg.Inventario.BodegaDeEmpresa(ctx, empresa, bodega)
		p.bodegaValida = ok && err == nil
	}
	return p
}

// UsarMaterialPanel: materiales.usar desde el panel (verifica en Argos otra bodega antes).
func (m *Modulo) UsarMaterialPanel(ctx context.Context, in EntradaUsarMaterial) (Orden, error) {
	if in.BodegaID != nil {
		if f, ok := leerOrden(ctx, in.ID); ok && !mismoID(in.BodegaID, f.BodegaID) {
			in.pre = m.bodegaDe(ctx, deref(f.EmpresaID), *in.BodegaID)
		}
	}
	return m.UsarMaterial.Ejecutar(ctx, in)
}

// RetirarEquipoPanel: equipo retirado desde el panel (lee el comodato antes).
func (m *Modulo) RetirarEquipoPanel(ctx context.Context, in EntradaEquipo) (Orden, error) {
	if f, ok := leerOrden(ctx, in.ID); ok {
		in.pre = m.comodatoDe(ctx, deref(f.EmpresaID), f.ServicioID)
	}
	return m.RetirarEquipo.Ejecutar(ctx, in)
}

// modalidadDe: la modalidad leída de Argos antes del lote (nil si no se sabe o no es una válida).
func modalidadDe(s *OpSync) *string {
	if s == nil {
		return nil
	}
	switch s.Modalidad {
	case "comodato", "consumible_gasto", "consumible_cobro":
		m := s.Modalidad
		return &m
	}
	return nil
}
