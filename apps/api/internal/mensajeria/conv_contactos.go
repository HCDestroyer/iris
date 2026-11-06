package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Contactos de una conversación (contratos/conversaciones.md §5): asociar un cliente de Argos (solo
// su id; los datos salen de la instantánea de lectura), sugerir coincidencias por teléfono y los
// contactos propios de Iris. Nada de copias editables de datos de Argos (CLAUDE.md regla 4).

// ArgosAtencion son las lecturas y escrituras en Argos que usa la bandeja (las implementa
// argos.Modulo). nil = sin Argos: solo la instantánea local.
type ArgosAtencion interface {
	BuscarPorTelefono(ctx context.Context, a argos.AtencionIris, telefono string) ([]argos.ClienteTelefono, error)
	AsegurarInstantanea(ctx context.Context, clienteID int64, servicioID *int64) error
	RegistrarComprobante(ctx context.Context, a argos.AtencionIris, in argos.EntradaComprobante) (argos.ComprobanteArgos, error)
	SimularPromesa(ctx context.Context, a argos.AtencionIris, servicioID int64, venceEn *time.Time) (argos.SimulacionPromesa, error)
	CrearPromesa(ctx context.Context, a argos.AtencionIris, servicioID int64, venceEn time.Time, clave string) (argos.PromesaArgos, error)
	CuentasBancarias(ctx context.Context, a argos.AtencionIris) ([]argos.CuentaBancariaArgos, error)
	Bancos(ctx context.Context, a argos.AtencionIris) ([]argos.BancoArgos, error)
	EquiposEnComodato(ctx context.Context, empresaID, servicioID int64) ([]argos.EquipoComodatoArgos, error)
}

func atencionDe(a alcanceConv, conv int64) argos.AtencionIris {
	return argos.AtencionIris{Actor: "usuario:" + strconv.FormatInt(a.usuario, 10), Conversacion: "iris:conversacion:" + strconv.FormatInt(conv, 10), EmpresaID: a.empresa}
}

// ContactoIris es un contacto propio de Iris.
type ContactoIris struct {
	ID        int64      `json:"id"`
	Nombre    string     `json:"nombre"`
	Tipo      string     `json:"tipo" enum:"proveedor,personal,prospecto,otro"`
	Telefono  string     `json:"telefono"`
	Nota      string     `json:"nota"`
	ClienteID *int64     `json:"clienteId" nullable:"true" doc:"Cliente de Argos al que se unió"`
	UnidoEn   *time.Time `json:"unidoEn" nullable:"true"`
	Version   int32      `json:"version"`
}

func aContactoIris(f mensajeriabd.MensajeriaContactoIris) ContactoIris {
	return ContactoIris{ID: f.ID, Nombre: f.Nombre, Tipo: f.Tipo, Telefono: f.Telefono, Nota: f.Nota, ClienteID: f.ClienteID, UnidoEn: f.UnidoEn, Version: f.Version}
}

func contactoAuditable(c ContactoIris) map[string]any {
	return map[string]any{"nombre": c.Nombre, "tipo": c.Tipo, "telefono": c.Telefono, "largo_nota": len([]rune(c.Nota)), "cliente_id": c.ClienteID}
}

// Coincidencias ----------------------------------------------------------------------------------

// CoincidenciaCliente es un cliente de Argos con el mismo número.
type CoincidenciaCliente struct {
	ClienteID  int64    `json:"clienteId"`
	Nombre     string   `json:"nombre"`
	Codigo     string   `json:"codigo"`
	CoincideEn []string `json:"coincideEn" doc:"telefono, movil, contacto_servicio (contacto en sitio de un servicio: no es el titular)"`
	Origen     string   `json:"origen" enum:"argos,instantanea" doc:"instantanea: Argos no respondió y se buscó en la copia local"`
}

// CoincidenciasConv son las sugerencias para asociar la conversación.
type CoincidenciasConv struct {
	Clientes        []CoincidenciaCliente `json:"clientes"`
	ContactosIris   []ContactoIris        `json:"contactosIris"`
	ArgosConsultado bool                  `json:"argosConsultado" doc:"false: Argos no respondió (o no hay conector) y se usó la instantánea"`
}

type convLeida struct {
	ID      int64 `json:"conversacion_id"`
	alcance alcanceConv
	conv    mensajeriabd.MensajeriaConversacion
}

// BuscarCoincidencias es GET /conversaciones/{id}/coincidencias: permiso y conversación en una
// transacción, Argos fuera de ella y la instantánea y los contactos de Iris en otra.
type BuscarCoincidencias struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaConversacion, convLeida]
	final  *auditoria.Caso[entradaCoincidencias, CoincidenciasConv]
}

type entradaCoincidencias struct {
	ID    int64 `json:"conversacion_id"`
	argos []argos.ClienteTelefono
	ok    bool
}

func nuevoLeerConvPara(accion string, permiso nucleo.Permiso) *auditoria.Caso[EntradaConversacion, convLeida] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConversacion, convLeida]{
		Accion: accion, Permiso: permiso, AuditarExito: sinAuditar[convLeida],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConversacion) (convLeida, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return convLeida{}, err
			}
			c, err := convVisible(ctx, mensajeriabd.New(t), a, in.ID, false)
			return convLeida{ID: c.ID, alcance: a, conv: c}, err
		},
	})
}

func nuevoBuscarCoincidencias(m *Modulo) *BuscarCoincidencias {
	b := &BuscarCoincidencias{m: m, previo: nuevoLeerConvPara("conversaciones.coincidencias", nucleo.PermConversacionesVer)}
	b.final = auditoria.Nuevo(auditoria.Caso[entradaCoincidencias, CoincidenciasConv]{
		Accion: "conversaciones.coincidencias", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[CoincidenciasConv],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaCoincidencias) (CoincidenciasConv, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return CoincidenciasConv{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return CoincidenciasConv{}, err
			}
			out := CoincidenciasConv{Clientes: []CoincidenciaCliente{}, ContactosIris: []ContactoIris{}, ArgosConsultado: in.ok}
			if in.ok {
				for _, x := range in.argos {
					en := x.CoincideEn
					if en == nil {
						en = []string{}
					}
					out.Clientes = append(out.Clientes, CoincidenciaCliente{ClienteID: x.ID, Nombre: x.Nombre, Codigo: x.Codigo, CoincideEn: en, Origen: "argos"})
				}
			} else {
				cs, err := q.ClientesPorTelefonoInstantanea(ctx, mensajeriabd.ClientesPorTelefonoInstantaneaParams{Digitos: soloDigitos(c.Numero), Empresa: a.empresa})
				if err != nil {
					return out, err
				}
				for _, x := range cs {
					en := []string{}
					if x.PorTelefono {
						en = append(en, "telefono")
					}
					if x.PorMovil {
						en = append(en, "movil")
					}
					if x.PorServicio {
						en = append(en, "contacto_servicio")
					}
					out.Clientes = append(out.Clientes, CoincidenciaCliente{ClienteID: x.ID, Nombre: x.Nombre, Codigo: x.Codigo, CoincideEn: en, Origen: "instantanea"})
				}
			}
			ks, err := q.ContactosIrisPorTelefono(ctx, mensajeriabd.ContactosIrisPorTelefonoParams{Empresa: a.empresa, Telefono: c.Numero})
			if err != nil {
				return out, err
			}
			for _, k := range ks {
				out.ContactosIris = append(out.ContactosIris, aContactoIris(k))
			}
			return out, nil
		},
	})
	return b
}

// Ejecutar busca las coincidencias.
func (b *BuscarCoincidencias) Ejecutar(ctx context.Context, in EntradaConversacion) (CoincidenciasConv, error) {
	l, err := b.previo.Ejecutar(ctx, in)
	if err != nil {
		return CoincidenciasConv{}, err
	}
	e := entradaCoincidencias{ID: l.ID}
	if b.m.cfg.Argos != nil {
		cs, err := b.m.cfg.Argos.BuscarPorTelefono(ctx, atencionDe(l.alcance, l.ID), l.conv.Numero)
		if err == nil {
			e.argos, e.ok = cs, true
		}
	}
	return b.final.Ejecutar(ctx, e)
}

// Asociar un cliente --------------------------------------------------------------------------------

// EntradaAsociarCliente es POST /conversaciones/{id}/asociar-cliente.
type EntradaAsociarCliente struct {
	ID         int64  `json:"-"`
	Version    int32  `json:"version" minimum:"1"`
	ClienteID  int64  `json:"clienteId" minimum:"1"`
	ServicioID *int64 `json:"servicioId,omitempty" minimum:"1" nullable:"true"`
}

// AsociarCliente asegura la instantánea del cliente en Argos (fuera de la transacción) y lo asocia.
type AsociarCliente struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaConversacion, convLeida]
	final  *auditoria.Caso[EntradaAsociarCliente, ConversacionClienteDetalle]
}

// clienteEnEmpresa valida el cliente (y el servicio) contra la instantánea de la empresa.
func clienteEnEmpresa(ctx context.Context, q *mensajeriabd.Queries, empresa, cliente int64, servicio *int64) error {
	c, err := q.ClienteInstantanea(ctx, cliente)
	if errors.Is(err, pgx.ErrNoRows) {
		return clienteFueraDeEmpresaConv()
	}
	if err != nil {
		return err
	}
	if empresa != 0 && c.EmpresaID != empresa && !slices.Contains(c.Empresas, empresa) {
		return clienteFueraDeEmpresaConv()
	}
	if servicio == nil {
		return nil
	}
	ss, err := q.ServiciosInstantanea(ctx, cliente)
	if err != nil {
		return err
	}
	for _, s := range ss {
		if s.ID == *servicio && (empresa == 0 || s.EmpresaID == empresa) {
			return nil
		}
	}
	return nucleo.NuevoProblema(422, "servicio_fuera_de_empresa", "Servicio fuera de la empresa", "El servicio no es del cliente o no es de la empresa",
		nucleo.ErrorCampo{Field: "servicioId", Message: "No es un servicio del cliente en la empresa", Rule: "empresa"})
}

func clienteFueraDeEmpresaConv() *nucleo.Problema {
	return nucleo.NuevoProblema(422, "cliente_fuera_de_empresa", "Cliente fuera de la empresa", "El cliente no existe en Argos o no es de la empresa activa",
		nucleo.ErrorCampo{Field: "clienteId", Message: "No es un cliente de la empresa", Rule: "empresa"})
}

func nuevoAsociarCliente(m *Modulo) *AsociarCliente {
	k := &AsociarCliente{m: m, previo: nuevoLeerConvPara("conversaciones.asociar", nucleo.PermConversacionesAsociar)}
	k.final = auditoria.Nuevo(auditoria.Caso[EntradaAsociarCliente, ConversacionClienteDetalle]{
		Accion: "conversaciones.asociar", Permiso: nucleo.PermConversacionesAsociar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAsociarCliente) (ConversacionClienteDetalle, error) {
			return m.accionConv(ctx, t, in.ID, in.Version, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				if err := clienteEnEmpresa(ctx, q, empresaDeConv(*c), in.ClienteID, in.ServicioID); err != nil {
					return err
				}
				c.ClienteID, c.ServicioID, c.Asociacion = &in.ClienteID, in.ServicioID, nombrePtr("manual")
				// Verificada solo si el número es un teléfono del cliente en la instantánea; si no,
				// un supervisor la confirma (POST …/confirmar-cliente).
				coincide, err := numeroDelCliente(ctx, q, empresaDeConv(*c), c.Numero, in.ClienteID)
				if err != nil {
					return err
				}
				c.ClienteVerificado, c.VerificadoPor = coincide, nil
				if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "cliente_asociado", ActorNombre: actorNombre(ctx, q, a.usuario),
					EntidadID: nombrePtr(strconv.FormatInt(in.ClienteID, 10))}); err != nil {
					return err
				}
				return t.Emitir("conversacion.cliente_asociado", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "cliente_id": in.ClienteID,
					"servicio_id": in.ServicioID, "asociacion": "manual", "actor_id": a.usuario})
			})
		},
	})
	return k
}

// numeroDelCliente: el número es el teléfono, el móvil o el contacto de un servicio del cliente en
// la instantánea de la empresa.
func numeroDelCliente(ctx context.Context, q *mensajeriabd.Queries, empresa int64, numero string, cliente int64) (bool, error) {
	cs, err := q.ClientesPorTelefonoInstantanea(ctx, mensajeriabd.ClientesPorTelefonoInstantaneaParams{Digitos: soloDigitos(numero), Empresa: empresa})
	if err != nil {
		return false, err
	}
	for _, x := range cs {
		if x.ID == cliente {
			return true, nil
		}
	}
	return false, nil
}

func nuevoConfirmarCliente(m *Modulo) *auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle]{
		Accion: "conversaciones.confirmar_cliente", Permiso: nucleo.PermConversacionesSupervisar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAccionConv) (ConversacionClienteDetalle, error) {
			return m.accionConv(ctx, t, in.ID, in.Version, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				if c.ClienteID == nil {
					return reglaConv("sin_cliente", "La conversación no tiene cliente asociado")
				}
				if c.ClienteVerificado {
					return nil
				}
				c.ClienteVerificado, c.VerificadoPor = true, &a.usuario
				if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "cliente_confirmado", ActorNombre: actorNombre(ctx, q, a.usuario),
					EntidadID: nombrePtr(strconv.FormatInt(*c.ClienteID, 10))}); err != nil {
					return err
				}
				return t.Emitir("conversacion.cliente_confirmado", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID,
					"cliente_id": c.ClienteID, "actor_id": a.usuario})
			})
		},
	})
}

// EntradaConsentimientoNumero es POST /contactos-iris/{id}/consentimiento.
type EntradaConsentimientoNumero struct {
	ID     int64   `json:"-"`
	Estado string  `json:"estado" enum:"acepta,baja" doc:"El contacto aceptó (o pidió no recibir) mensajes por WhatsApp. acepta exige conversaciones.supervisar"`
	Motivo *string `json:"motivo,omitempty" minLength:"5" maxLength:"300" doc:"Obligatorio para registrar acepta sobre una baja que pidió el propio cliente (queda auditado)"`
}

// ConsentimientoNumero es el permiso vigente de un número.
type ConsentimientoNumero struct {
	Telefono   string    `json:"telefono"`
	Estado     string    `json:"estado" enum:"acepta,baja"`
	OcurridoEn time.Time `json:"ocurridoEn"`
}

func nuevoConsentimientoNumero(m *Modulo) *auditoria.Caso[EntradaConsentimientoNumero, ConsentimientoNumero] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConsentimientoNumero, ConsentimientoNumero]{
		Accion: "conversaciones.consentimiento_numero", Permiso: nucleo.PermConversacionesAsociar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConsentimientoNumero) (ConsentimientoNumero, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ConsentimientoNumero{}, err
			}
			q := mensajeriabd.New(t)
			k, err := q.ContactoIrisParaActualizar(ctx, mensajeriabd.ContactoIrisParaActualizarParams{ID: in.ID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return ConsentimientoNumero{}, nucleo.NoEncontrado("El contacto no existe")
			}
			if err != nil {
				return ConsentimientoNumero{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("contacto_iris", k.ID)
			var motivo *string
			if in.Estado == "acepta" {
				// Habilitar el contacto es de supervisión (permiso sensible), y nunca anula en silencio la
				// baja que pidió el propio cliente: para eso hace falta un motivo, que queda auditado.
				if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermConversacionesSupervisar)) {
					return ConsentimientoNumero{}, nucleo.Prohibido("Registrar el permiso de contacto es de supervisión (conversaciones.supervisar)")
				}
				// Cualquier baja del propio cliente sin un acepta posterior (aunque luego haya bajas de agentes).
				baja, err := q.BajaDelClientePendiente(ctx, mensajeriabd.BajaDelClientePendienteParams{Empresa: a.empresa, Telefono: k.Telefono})
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return ConsentimientoNumero{}, err
				}
				if err == nil {
					if in.Motivo == nil || utf8.RuneCountInString(strings.TrimSpace(*in.Motivo)) < 5 {
						return ConsentimientoNumero{}, reglaConv("baja_del_cliente", "El propio cliente pidió la baja: para anularla escribe el motivo")
					}
					motivo = nombrePtr(strings.TrimSpace(*in.Motivo))
					t.Anotar(map[string]any{"anula_baja_del_cliente": baja, "motivo": *motivo})
				}
			}
			f, err := q.InsertarConsentimientoNumero(ctx, mensajeriabd.InsertarConsentimientoNumeroParams{EmpresaID: k.EmpresaID, Telefono: k.Telefono,
				Estado: in.Estado, Origen: "agente", OcurridoEn: m.ahora(), RegistradoPor: &a.usuario, Motivo: motivo})
			if err != nil {
				return ConsentimientoNumero{}, err
			}
			t.RegistrarCambios("contacto_iris", k.ID, nil, map[string]any{"consentimiento": f.Estado})
			return ConsentimientoNumero{Telefono: f.Telefono, Estado: f.Estado, OcurridoEn: f.OcurridoEn},
				t.Emitir("contacto_iris.consentimiento", map[string]any{"contacto_iris_id": k.ID, "empresa_id": k.EmpresaID, "estado": f.Estado, "origen": "agente"})
		},
	})
}

// Ejecutar asocia el cliente.
func (k *AsociarCliente) Ejecutar(ctx context.Context, in EntradaAsociarCliente) (ConversacionClienteDetalle, error) {
	if _, err := k.previo.Ejecutar(ctx, EntradaConversacion{ID: in.ID}); err != nil {
		return ConversacionClienteDetalle{}, err
	}
	if k.m.cfg.Argos != nil {
		if err := k.m.cfg.Argos.AsegurarInstantanea(ctx, in.ClienteID, in.ServicioID); err != nil {
			return ConversacionClienteDetalle{}, err
		}
	}
	return k.final.Ejecutar(ctx, in)
}

func nuevoDesasociar(m *Modulo) *auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle]{
		Accion: "conversaciones.desasociar", Permiso: nucleo.PermConversacionesAsociar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAccionConv) (ConversacionClienteDetalle, error) {
			return m.accionConv(ctx, t, in.ID, in.Version, func(a alcanceConv, _ *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				c.ClienteID, c.ServicioID, c.ContactoIrisID, c.ProspectoID, c.Asociacion = nil, nil, nil, nil, nil
				c.ClienteVerificado, c.VerificadoPor = false, nil
				return t.Emitir("conversacion.cliente_asociado", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "cliente_id": nil,
					"servicio_id": nil, "asociacion": nil, "actor_id": a.usuario})
			})
		},
	})
}

// Contactos de Iris -----------------------------------------------------------------------------------

// EntradaContactoIrisConv es POST /conversaciones/{id}/contacto-iris.
type EntradaContactoIrisConv struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1"`
	Nombre  string `json:"nombre" minLength:"1" maxLength:"120"`
	Tipo    string `json:"tipo" enum:"proveedor,personal,prospecto,otro"`
	Nota    string `json:"nota,omitempty" maxLength:"500"`
}

func nuevoContactoIrisConv(m *Modulo) *auditoria.Caso[EntradaContactoIrisConv, ConversacionClienteDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaContactoIrisConv, ConversacionClienteDetalle]{
		Accion: "conversaciones.crear_contacto_iris", Permiso: nucleo.PermConversacionesAsociar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaContactoIrisConv) (ConversacionClienteDetalle, error) {
			return m.accionConv(ctx, t, in.ID, in.Version, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				f, err := q.InsertarContactoIris(ctx, mensajeriabd.InsertarContactoIrisParams{EmpresaID: c.EmpresaID, Nombre: strings.TrimSpace(in.Nombre),
					Tipo: in.Tipo, Telefono: c.Numero, Nota: in.Nota, CreadoPor: &a.usuario})
				if err != nil {
					return err
				}
				t.RegistrarCambios("contacto_iris", f.ID, nil, contactoAuditable(aContactoIris(f)))
				c.ContactoIrisID, c.ClienteID, c.ServicioID, c.Asociacion = &f.ID, nil, nil, nombrePtr("manual")
				if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "contacto_iris_creado", ActorNombre: actorNombre(ctx, q, a.usuario),
					EntidadID: nombrePtr(strconv.FormatInt(f.ID, 10))}); err != nil {
					return err
				}
				return t.Emitir("contacto_iris.creado", map[string]any{"contacto_iris_id": f.ID, "empresa_id": f.EmpresaID, "tipo": f.Tipo,
					"conversacion_id": c.ID})
			})
		},
	})
}

// MarshalJSON: sin la nota.
func (e EntradaContactoIrisConv) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": e.ID, "version": e.Version, "nombre": e.Nombre, "tipo": e.Tipo, "largo_nota": len([]rune(e.Nota))})
}

// MarshalJSON: sin la nota.
func (e EntradaActualizarContactoIris) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": e.ID, "version": e.Version, "nombre": e.Nombre, "tipo": e.Tipo, "largo_nota": len([]rune(e.Nota))})
}

// EntradaActualizarContactoIris es POST /contactos-iris/{id}/actualizar.
type EntradaActualizarContactoIris struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1"`
	Nombre  string `json:"nombre" minLength:"1" maxLength:"120"`
	Tipo    string `json:"tipo" enum:"proveedor,personal,prospecto,otro"`
	Nota    string `json:"nota,omitempty" maxLength:"500"`
}

func (m *Modulo) contactoParaActualizar(ctx context.Context, t *auditoria.Tx, a alcanceConv, id int64, version int32) (mensajeriabd.MensajeriaContactoIris, error) {
	f, err := mensajeriabd.New(t).ContactoIrisParaActualizar(ctx, mensajeriabd.ContactoIrisParaActualizarParams{ID: id, Empresa: a.empresa})
	if errors.Is(err, pgx.ErrNoRows) {
		return f, nucleo.NoEncontrado("El contacto no existe")
	}
	if err != nil {
		return f, err
	}
	if f.Version != version {
		return f, nucleo.ConflictoVersion("contacto_iris", id)
	}
	t.FijarEmpresa(a.empresa)
	t.FijarEntidad("contacto_iris", id)
	return f, nil
}

func nuevoActualizarContactoIris(m *Modulo) *auditoria.Caso[EntradaActualizarContactoIris, ContactoIris] {
	return auditoria.Nuevo(auditoria.Caso[EntradaActualizarContactoIris, ContactoIris]{
		Accion: "conversaciones.actualizar_contacto_iris", Permiso: nucleo.PermConversacionesAsociar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaActualizarContactoIris) (ContactoIris, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ContactoIris{}, err
			}
			antes, err := m.contactoParaActualizar(ctx, t, a, in.ID, in.Version)
			if err != nil {
				return ContactoIris{}, err
			}
			f, err := mensajeriabd.New(t).ActualizarContactoIris(ctx, mensajeriabd.ActualizarContactoIrisParams{ID: in.ID, Nombre: strings.TrimSpace(in.Nombre),
				Tipo: in.Tipo, Nota: in.Nota})
			if err != nil {
				return ContactoIris{}, err
			}
			t.RegistrarCambios("contacto_iris", f.ID, contactoAuditable(aContactoIris(antes)), contactoAuditable(aContactoIris(f)))
			return aContactoIris(f), nil
		},
	})
}

// EntradaUnirContacto es POST /contactos-iris/{id}/unir-cliente.
type EntradaUnirContacto struct {
	ID        int64 `json:"-"`
	Version   int32 `json:"version" minimum:"1"`
	ClienteID int64 `json:"clienteId" minimum:"1"`
}

// UnirContacto asegura la instantánea del cliente y une el contacto (y sus conversaciones).
type UnirContacto struct {
	m     *Modulo
	final *auditoria.Caso[EntradaUnirContacto, ContactoIris]
}

func nuevoUnirContacto(m *Modulo) *UnirContacto {
	k := &UnirContacto{m: m}
	k.final = auditoria.Nuevo(auditoria.Caso[EntradaUnirContacto, ContactoIris]{
		Accion: "conversaciones.unir_contacto_iris", Permiso: nucleo.PermConversacionesAsociar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaUnirContacto) (ContactoIris, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ContactoIris{}, err
			}
			antes, err := m.contactoParaActualizar(ctx, t, a, in.ID, in.Version)
			if err != nil {
				return ContactoIris{}, err
			}
			q := mensajeriabd.New(t)
			if err := clienteEnEmpresa(ctx, q, a.empresa, in.ClienteID, nil); err != nil {
				return ContactoIris{}, err
			}
			ahora := m.ahora()
			f, err := q.UnirContactoIris(ctx, mensajeriabd.UnirContactoIrisParams{ID: in.ID, ClienteID: &in.ClienteID, UnidoEn: &ahora})
			if err != nil {
				return ContactoIris{}, err
			}
			convs, err := q.PasarConversacionesACliente(ctx, mensajeriabd.PasarConversacionesAClienteParams{ClienteID: &in.ClienteID, ContactoIrisID: &in.ID})
			if err != nil {
				return ContactoIris{}, err
			}
			for _, id := range convs {
				c, err := q.ConversacionClienteParaActualizar(ctx, id)
				if err != nil {
					return ContactoIris{}, err
				}
				if c.ClienteVerificado, err = numeroDelCliente(ctx, q, empresaDeConv(c), c.Numero, in.ClienteID); err != nil {
					return ContactoIris{}, err
				}
				if err := m.sistema(ctx, t, &c, EventoSistema{Tipo: "cliente_asociado", ActorNombre: actorNombre(ctx, q, a.usuario),
					EntidadID: nombrePtr(strconv.FormatInt(in.ClienteID, 10))}); err != nil {
					return ContactoIris{}, err
				}
				if _, err := guardarConv(ctx, q, c, false); err != nil {
					return ContactoIris{}, err
				}
				if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
					return ContactoIris{}, err
				}
			}
			t.RegistrarCambios("contacto_iris", f.ID, map[string]any{"cliente_id": antes.ClienteID}, map[string]any{"cliente_id": f.ClienteID})
			return aContactoIris(f), t.Emitir("contacto_iris.unido", map[string]any{"contacto_iris_id": f.ID, "empresa_id": f.EmpresaID,
				"cliente_id": in.ClienteID, "conversaciones": convs})
		},
	})
	return k
}

// Ejecutar une el contacto.
func (k *UnirContacto) Ejecutar(ctx context.Context, in EntradaUnirContacto) (ContactoIris, error) {
	if k.m.cfg.Argos != nil {
		if err := k.m.cfg.Argos.AsegurarInstantanea(ctx, in.ClienteID, nil); err != nil {
			return ContactoIris{}, err
		}
	}
	return k.final.Ejecutar(ctx, in)
}

// EntradaBuscarContactos es GET /contactos-iris.
type EntradaBuscarContactos struct {
	nucleo.ParamsPagina
	Q string `query:"q" maxLength:"80"`
}

func nuevoBuscarContactos(*Modulo) *auditoria.Caso[EntradaBuscarContactos, nucleo.Pagina[ContactoIris]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaBuscarContactos, nucleo.Pagina[ContactoIris]]{
		Accion: "conversaciones.buscar_contactos", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[nucleo.Pagina[ContactoIris]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaBuscarContactos) (nucleo.Pagina[ContactoIris], error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return nucleo.Pagina[ContactoIris]{}, err
			}
			var cursor int64
			con, err := nucleo.DecodificarCursor(in.Cursor, &cursor)
			if err != nil {
				return nucleo.Pagina[ContactoIris]{}, err
			}
			limite := in.Limite()
			fs, err := mensajeriabd.New(t).BuscarContactosIris(ctx, mensajeriabd.BuscarContactosIrisParams{Empresa: a.empresa,
				Q: escaparLike(strings.TrimSpace(in.Q)), ConCursor: con, CursorID: cursor, Limite: int32(limite + 1)}) //nolint:gosec // limite ≤ 200
			if err != nil {
				return nucleo.Pagina[ContactoIris]{}, err
			}
			return nucleo.Paginar(fs, limite, aContactoIris, func(f mensajeriabd.MensajeriaContactoIris) []any { return []any{f.ID} }), nil
		},
	})
}
