package mensajeria

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Iniciar una conversación, buscar a quién escribir y «Pasar a…» (contratos/conversaciones.md
// §14.3 y §14.4).

// categoriaPlantilla: la categoría de Meta. Todo el catálogo de Iris son avisos de servicio
// (utility); marketing y autenticación quedan para plantillas futuras.
func categoriaPlantilla(Plantilla) string { return "servicio" }

// plantillasDisponibles: las plantillas del catálogo aprobadas en el número.
func plantillasDisponibles(canal mensajeriabd.MensajeriaCanal) ListaPlantillasConv {
	aprobadas := plantillasDeCanal(canal)
	out := ListaPlantillasConv{Data: []PlantillaDisponible{}}
	for _, p := range Catalogo {
		if pm, ok := aprobadas[p.Codigo]; ok {
			out.Data = append(out.Data, PlantillaDisponible{Codigo: p.Codigo, Nombre: pm.Nombre, Categoria: categoriaPlantilla(p), Variables: p.Variables,
				VistaPrevia: p.Texto})
		}
	}
	return out
}

// puedeIniciarDesde: el número es activo, de la empresa y de una de las colas del usuario (miembro
// o supervisor de la cola); un supervisor global, cualquier número activo de la empresa.
func puedeIniciarDesde(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, canal mensajeriabd.MensajeriaCanal) (bool, error) {
	e := int64(0)
	if canal.EmpresaID != nil {
		e = *canal.EmpresaID
	}
	if !canal.Activo || e != a.empresa {
		return false, nil
	}
	if a.supervisa {
		return true, nil
	}
	if canal.ColaID == nil {
		return false, nil
	}
	cola, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: *canal.ColaID, Empresa: a.empresa})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil || !cola.Activa {
		return false, err
	}
	return atiendeCola(ctx, q, cola.ID, a.usuario)
}

func numeroNoPermitido() *nucleo.Problema {
	return reglaConv("numero_no_permitido", "Solo puedes iniciar conversaciones desde un número activo de tus colas")
}

// Plantillas de un número ----------------------------------------------------------------------

// EntradaCanalConv es un número por id (GET /conversaciones/canales/{id}/plantillas).
type EntradaCanalConv struct {
	ID int64 `path:"id" minimum:"1"`
}

func nuevoPlantillasCanal(*Modulo) *auditoria.Caso[EntradaCanalConv, ListaPlantillasConv] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCanalConv, ListaPlantillasConv]{
		Accion: "conversaciones.plantillas_numero", Permiso: nucleo.PermConversacionesIniciar, AuditarExito: sinAuditar[ListaPlantillasConv],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCanalConv) (ListaPlantillasConv, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaPlantillasConv{}, err
			}
			q := mensajeriabd.New(t)
			canal, err := q.CanalPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ListaPlantillasConv{}, nucleo.NoEncontrado("El número no existe o no es de tus colas")
			}
			if err != nil {
				return ListaPlantillasConv{}, err
			}
			ok, err := puedeIniciarDesde(ctx, q, a, canal)
			if err != nil {
				return ListaPlantillasConv{}, err
			}
			if !ok {
				return ListaPlantillasConv{}, nucleo.NoEncontrado("El número no existe o no es de tus colas")
			}
			return plantillasDisponibles(canal), nil
		},
	})
}

// Destinatarios ---------------------------------------------------------------------------------

// Destinatario es a quién se le puede escribir primero.
type Destinatario struct {
	Tipo           string  `json:"tipo" enum:"cliente,contacto_iris"`
	ID             int64   `json:"id" doc:"Cliente en Argos o contacto de Iris"`
	Nombre         string  `json:"nombre"`
	Codigo         *string `json:"codigo" nullable:"true" doc:"Código del cliente en Argos (null en un contacto de Iris)"`
	Detalle        *string `json:"detalle" nullable:"true" doc:"Contacto de Iris: su tipo (proveedor, personal, prospecto, otro)"`
	Numero         *string `json:"numero" nullable:"true" doc:"El número al que saldría (E.164); null si no tiene"`
	Puede          bool    `json:"puede"`
	Motivo         *string `json:"motivo" nullable:"true" enum:"sin_contacto,sin_permiso,baja,cliente_fuera_de_empresa,conversacion_de_otro,conversacion_de_otra_cola,numero_no_permitido" doc:"Por qué no (los códigos de POST /conversaciones/iniciar)"`
	ConversacionID *int64  `json:"conversacionId" nullable:"true" doc:"Con canalId: la conversación que ya existe de ese número en ese canal"`
}

// ListaDestinatarios es { data, nextCursor }.
type ListaDestinatarios struct {
	Data       []Destinatario `json:"data"`
	NextCursor *string        `json:"nextCursor" nullable:"true"`
}

// EntradaDestinatarios es GET /conversaciones/destinatarios.
type EntradaDestinatarios struct {
	Q       string `query:"q" minLength:"2" maxLength:"80" doc:"Nombre, código o dígitos del teléfono"`
	CanalID int64  `query:"canalId" minimum:"0" doc:"Número de salida: suma conversacion_de_otro, numero_no_permitido y conversacionId"`
	Limit   int    `query:"limit" minimum:"1" maximum:"20" default:"10"`
}

// EntradaDestinatario es GET /conversaciones/destinatario.
type EntradaDestinatario struct {
	CanalID        int64 `query:"canalId" minimum:"0"`
	ClienteID      int64 `query:"clienteId" minimum:"0"`
	ContactoIrisID int64 `query:"contactoIrisId" minimum:"0"`
}

func telefonoDe(movil, telefono string) string {
	return destinoDe(argos.ContactoArgos{Movil: movil, Telefono: telefono}, CanalWhatsApp)
}

// evaluarDestino llena numero, puede, motivo y conversacionId con las reglas de iniciar.
func (m *Modulo) evaluarDestino(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, canal *mensajeriabd.MensajeriaCanal, d *Destinatario, numero string) error {
	motivo := func(s string) error {
		d.Puede, d.Motivo = false, &s
		return nil
	}
	d.Puede = true
	if numero == "" {
		return motivo(MotivoSinContacto)
	}
	d.Numero = &numero
	prueba := mensajeriabd.MensajeriaConversacion{EmpresaID: a.empresaPtr(), Numero: numero}
	if d.Tipo == "cliente" {
		prueba.ClienteID, prueba.ClienteVerificado = &d.ID, true
	} else {
		prueba.ContactoIrisID = &d.ID
	}
	if canal != nil {
		// Primero el número de salida: sin él no se mira la conversación del número.
		ok, err := puedeIniciarDesde(ctx, q, a, *canal)
		if err != nil {
			return err
		}
		if !ok {
			return motivo("numero_no_permitido")
		}
		c, err := q.ConversacionDeContacto(ctx, mensajeriabd.ConversacionDeContactoParams{Empresa: a.empresa, CanalID: canal.ID, Numero: numero})
		switch {
		case err == nil:
			if err := reutilizable(ctx, q, a, c); err != nil {
				var p *nucleo.Problema
				if errors.As(err, &p) {
					if p.Codigo() == "conversacion_de_otro" {
						// La ve: puede abrirla o pedir que se la pasen.
						d.ConversacionID = &c.ID
					}
					return motivo(p.Codigo())
				}
				return err
			}
			d.ConversacionID = &c.ID
			prueba.UltimoEntranteEn = c.UltimoEntranteEn
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
	}
	if err := permisoPlantilla(ctx, q, prueba, !ventanaDe(prueba.UltimoEntranteEn, m.ahora()).Abierta); err != nil {
		var p *nucleo.Problema
		if errors.As(err, &p) {
			return motivo(p.Codigo())
		}
		return err
	}
	return nil
}

// canalDeEntrada: el número de la empresa activa; uno que no existe y uno de otra empresa dan el
// mismo 422 (no se revela que existe).
func canalDeEntrada(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, id int64) (*mensajeriabd.MensajeriaCanal, error) {
	if id <= 0 {
		return nil, nil
	}
	c, err := canalDeEmpresa(ctx, q, a, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, campoConv("canalId", "No es un número de la empresa", "canal")
	}
	return &c, err
}

// canalDeEmpresa: pgx.ErrNoRows si no existe o es de otra empresa.
func canalDeEmpresa(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, id int64) (mensajeriabd.MensajeriaCanal, error) {
	c, err := q.CanalPorID(ctx, id)
	if err != nil {
		return c, err
	}
	e := int64(0)
	if c.EmpresaID != nil {
		e = *c.EmpresaID
	}
	if e != a.empresa {
		return mensajeriabd.MensajeriaCanal{}, pgx.ErrNoRows
	}
	return c, nil
}

// reutilizable: iniciar puede usar la conversación que ya existe del número si la ve y es de una
// cola que atiende (o sin cola), salvo supervisor global; si no, 409 conversacion_de_otra_cola. Una
// activa de otro agente: 409 conversacion_de_otro.
func reutilizable(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, c mensajeriabd.MensajeriaConversacion) error {
	if !a.supervisa {
		visible, err := q.ConversacionVisible(ctx, mensajeriabd.ConversacionVisibleParams{ID: c.ID, Empresa: a.empresa, Usuario: a.usuario, Supervisa: false})
		if err != nil {
			return err
		}
		colaOK := c.ColaID == nil
		if !colaOK {
			if colaOK, err = atiendeCola(ctx, q, *c.ColaID, a.usuario); err != nil {
				return err
			}
		}
		if !visible || !colaOK {
			return nucleo.NuevoProblema(http.StatusConflict, "conversacion_de_otra_cola", "Conversación de otra cola",
				"Ese número ya tiene una conversación que no es de tus colas: pide a un supervisor que te la asigne")
		}
	}
	if c.Estado != EstadoConvResuelta && c.AsignadoA != nil && *c.AsignadoA != a.usuario {
		quien := "otro agente"
		if n := actorNombre(ctx, q, *c.AsignadoA); n != nil {
			quien = *n
		}
		return nucleo.NuevoProblema(http.StatusConflict, "conversacion_de_otro", "Conversación de otro agente",
			"Ese número ya tiene una conversación activa con "+quien+": ábrela o pide que te la pase")
	}
	return nil
}

func nuevoBuscarDestinatarios(m *Modulo) *auditoria.Caso[EntradaDestinatarios, ListaDestinatarios] {
	return auditoria.Nuevo(auditoria.Caso[EntradaDestinatarios, ListaDestinatarios]{
		Accion: "conversaciones.destinatarios", Permiso: nucleo.PermConversacionesIniciar, AuditarExito: sinAuditar[ListaDestinatarios],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDestinatarios) (ListaDestinatarios, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaDestinatarios{}, err
			}
			texto := strings.TrimSpace(in.Q)
			if len([]rune(texto)) < 2 {
				return ListaDestinatarios{}, campoConv("q", "Escribe al menos 2 letras", "minLength")
			}
			limite := in.Limit
			if limite <= 0 || limite > 20 {
				limite = 10
			}
			digitos := soloDigitos(texto)
			if len(digitos) < 3 {
				digitos = ""
			}
			q := mensajeriabd.New(t)
			canal, err := canalDeEntrada(ctx, q, a, in.CanalID)
			if err != nil {
				return ListaDestinatarios{}, err
			}
			out := ListaDestinatarios{Data: []Destinatario{}}
			cs, err := q.BuscarClientesInstantanea(ctx, mensajeriabd.BuscarClientesInstantaneaParams{Empresa: a.empresa, Q: escaparLike(texto),
				Digitos: digitos, Limite: int32(limite)}) //nolint:gosec // limite ≤ 20
			if err != nil {
				return ListaDestinatarios{}, err
			}
			for _, c := range cs {
				codigo := c.Codigo
				d := Destinatario{Tipo: "cliente", ID: c.ID, Nombre: c.Nombre, Codigo: &codigo}
				if err := m.evaluarDestino(ctx, q, a, canal, &d, telefonoDe(c.Movil, c.Telefono)); err != nil {
					return ListaDestinatarios{}, err
				}
				out.Data = append(out.Data, d)
			}
			if resto := limite - len(out.Data); resto > 0 {
				ks, err := q.BuscarContactosIrisDestino(ctx, mensajeriabd.BuscarContactosIrisDestinoParams{Empresa: a.empresa, Q: escaparLike(texto),
					Digitos: digitos, Limite: int32(resto)}) //nolint:gosec // resto ≤ 20
				if err != nil {
					return ListaDestinatarios{}, err
				}
				for _, k := range ks {
					tipo := k.Tipo
					d := Destinatario{Tipo: "contacto_iris", ID: k.ID, Nombre: k.Nombre, Detalle: &tipo}
					if err := m.evaluarDestino(ctx, q, a, canal, &d, k.Telefono); err != nil {
						return ListaDestinatarios{}, err
					}
					out.Data = append(out.Data, d)
				}
			}
			return out, nil
		},
	})
}

func nuevoVerDestinatario(m *Modulo) *auditoria.Caso[EntradaDestinatario, Destinatario] {
	return auditoria.Nuevo(auditoria.Caso[EntradaDestinatario, Destinatario]{
		Accion: "conversaciones.destinatario", Permiso: nucleo.PermConversacionesIniciar, AuditarExito: sinAuditar[Destinatario],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDestinatario) (Destinatario, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return Destinatario{}, err
			}
			if (in.ClienteID > 0) == (in.ContactoIrisID > 0) {
				return Destinatario{}, campoConv("clienteId", "Indica clienteId o contactoIrisId (uno)", "exclusivo")
			}
			q := mensajeriabd.New(t)
			canal, err := canalDeEntrada(ctx, q, a, in.CanalID)
			if err != nil {
				return Destinatario{}, err
			}
			if in.ClienteID > 0 {
				c, err := q.ClienteInstantaneaEnEmpresa(ctx, mensajeriabd.ClienteInstantaneaEnEmpresaParams{ID: in.ClienteID, Empresa: a.empresa})
				if errors.Is(err, pgx.ErrNoRows) {
					return Destinatario{}, nucleo.NoEncontrado("El cliente no está en la instantánea de Argos de la empresa")
				}
				if err != nil {
					return Destinatario{}, err
				}
				codigo := c.Codigo
				d := Destinatario{Tipo: "cliente", ID: c.ID, Nombre: c.Nombre, Codigo: &codigo}
				return d, m.evaluarDestino(ctx, q, a, canal, &d, telefonoDe(c.Movil, c.Telefono))
			}
			k, err := q.ContactoIrisDeEmpresa(ctx, mensajeriabd.ContactoIrisDeEmpresaParams{ID: in.ContactoIrisID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return Destinatario{}, nucleo.NoEncontrado("El contacto de Iris no existe")
			}
			if err != nil {
				return Destinatario{}, err
			}
			tipo := k.Tipo
			d := Destinatario{Tipo: "contacto_iris", ID: k.ID, Nombre: k.Nombre, Detalle: &tipo}
			return d, m.evaluarDestino(ctx, q, a, canal, &d, k.Telefono)
		},
	})
}

// Iniciar ------------------------------------------------------------------------------------------

// EntradaIniciarConv es POST /conversaciones/iniciar.
type EntradaIniciarConv struct {
	ID             uuid.UUID        `json:"id" doc:"uuid v7 generado en el cliente (id del mensaje): el servidor es idempotente por él"`
	CanalID        int64            `json:"canalId" minimum:"1" doc:"Número de salida (de una de tus colas)"`
	ClienteID      *int64           `json:"clienteId,omitempty" minimum:"1" doc:"Cliente de Argos (o contactoIrisId)"`
	ServicioID     *int64           `json:"servicioId,omitempty" minimum:"1" doc:"Servicio del cliente (opcional)"`
	ContactoIrisID *int64           `json:"contactoIrisId,omitempty" minimum:"1" doc:"Contacto de Iris (o clienteId)"`
	Plantilla      PlantillaMensaje `json:"plantilla" doc:"Plantilla aprobada en el número"`
	IdempotencyKey string           `json:"-"`
}

// MarshalJSON: a la auditoría van los ids, la plantilla y el hash; nunca las variables.
func (e EntradaIniciarConv) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": e.ID, "canal_id": e.CanalID, "cliente_id": e.ClienteID, "servicio_id": e.ServicioID,
		"contacto_iris_id": e.ContactoIrisID, "plantilla": e.Plantilla.Codigo, "hash": e.hash()})
}

func (e EntradaIniciarConv) hash() []byte {
	return hashDe(map[string]any{"canal": e.CanalID, "cliente": e.ClienteID, "servicio": e.ServicioID, "contacto": e.ContactoIrisID, "plantilla": e.Plantilla})
}

// ConversacionIniciada es la respuesta de POST /conversaciones/iniciar.
type ConversacionIniciada struct {
	Conversacion ConversacionClienteDetalle `json:"conversacion"`
	Mensaje      MensajeConversacion        `json:"mensaje"`
	Nueva        bool                       `json:"nueva" doc:"Se creó la conversación (si no, se reutilizó la del número)"`
	Repetido     bool                       `json:"-"`
}

type entradaIniciarFinal struct {
	EntradaIniciarConv
	contacto *argos.ContactoArgos
}

// MarshalJSON: lo mismo que la entrada (sin el contacto de Argos).
func (e entradaIniciarFinal) MarshalJSON() ([]byte, error) { return e.EntradaIniciarConv.MarshalJSON() }

// IniciarConv lee el contacto del cliente en Argos (instantánea refrescada) fuera de la transacción
// y luego corre el caso.
type IniciarConv struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaIniciarConv, struct{}]
	final  *auditoria.Caso[entradaIniciarFinal, ConversacionIniciada]
}

// Ejecutar inicia la conversación. Antes de consultar Argos, el caso previo revisa el permiso, la
// re-autenticación (sesión de autologin), el número de salida y que el id no sea un reenvío.
func (k *IniciarConv) Ejecutar(ctx context.Context, in EntradaIniciarConv) (ConversacionIniciada, error) {
	f := entradaIniciarFinal{EntradaIniciarConv: in}
	if in.ClienteID != nil && in.ContactoIrisID == nil && k.m.cfg.Contactos != nil {
		if _, err := k.previo.Ejecutar(ctx, in); err != nil {
			if errors.Is(err, errReenvioIniciar) {
				return k.final.Ejecutar(ctx, f)
			}
			return ConversacionIniciada{}, err
		}
		c, err := k.m.cfg.Contactos.ContactoCliente(ctx, *in.ClienteID)
		if err != nil {
			// El detalle de Argos no sale al panel (queda en el log).
			slog.WarnContext(ctx, "mensajeria: iniciar sin contacto de Argos", "cliente_id", *in.ClienteID, "err", err)
			return ConversacionIniciada{}, clienteFueraDeEmpresaConv()
		}
		f.contacto = &c
	}
	return k.final.Ejecutar(ctx, f)
}

// errReenvioIniciar: el id ya se recibió (el caso final responde el reenvío sin consultar Argos).
var errReenvioIniciar = errors.New("reenvío de iniciar")

func nuevoIniciarConv(m *Modulo) *IniciarConv {
	previo := auditoria.Nuevo(auditoria.Caso[EntradaIniciarConv, struct{}]{
		Accion: "conversaciones.iniciar", Permiso: nucleo.PermConversacionesIniciar, AuditarExito: sinAuditar[struct{}],
		// El reenvío no es un error: no se audita (lo responde el caso final).
		AuditarError: func(err error) bool { return !errors.Is(err, errReenvioIniciar) },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaIniciarConv) (struct{}, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return struct{}{}, err
			}
			q := mensajeriabd.New(t)
			if in.ID != uuid.Nil {
				if _, err := q.MensajeConversacionPorID(ctx, in.ID); err == nil {
					return struct{}{}, errReenvioIniciar
				}
			}
			canal, err := canalDeEmpresa(ctx, q, a, in.CanalID)
			if errors.Is(err, pgx.ErrNoRows) {
				return struct{}{}, numeroNoPermitido()
			}
			if err != nil {
				return struct{}{}, err
			}
			ok, err := puedeIniciarDesde(ctx, q, a, canal)
			if err != nil {
				return struct{}{}, err
			}
			if !ok {
				return struct{}{}, numeroNoPermitido()
			}
			return struct{}{}, nil
		},
	})
	return &IniciarConv{m: m, previo: previo, final: auditoria.Nuevo(auditoria.Caso[entradaIniciarFinal, ConversacionIniciada]{
		Accion: "conversaciones.iniciar", Permiso: nucleo.PermConversacionesIniciar,
		AuditarExito: func(r ConversacionIniciada) bool { return !r.Repetido },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaIniciarFinal) (ConversacionIniciada, error) {
			return m.iniciar(ctx, t, in)
		},
	})}
}

func (m *Modulo) iniciar(ctx context.Context, t *auditoria.Tx, in entradaIniciarFinal) (ConversacionIniciada, error) {
	a, err := alcanceConvDe(ctx)
	if err != nil {
		return ConversacionIniciada{}, err
	}
	if in.ID == uuid.Nil {
		return ConversacionIniciada{}, campoConv("id", "Falta el id del mensaje", "required")
	}
	if (in.ClienteID != nil) == (in.ContactoIrisID != nil) {
		return ConversacionIniciada{}, campoConv("clienteId", "Indica clienteId o contactoIrisId (uno)", "exclusivo")
	}
	if in.ServicioID != nil && in.ClienteID == nil {
		return ConversacionIniciada{}, campoConv("servicioId", "El servicio va con clienteId", "exclusivo")
	}
	t.FijarEmpresa(a.empresa)
	q := mensajeriabd.New(t)
	hash := in.hash()
	// El mismo id: la misma respuesta (o 409 con otro contenido); la misma clave con otro id, 422.
	if f, err := q.MensajeConversacionPorID(ctx, in.ID); err == nil {
		if f.AutorID == nil || *f.AutorID != a.usuario || !bytesIguales(f.HashContenido, hash) {
			return ConversacionIniciada{}, nucleo.Conflicto("mensaje_id_reutilizado", "Ese id de mensaje ya se usó con otro contenido")
		}
		c, err := convVisible(ctx, q, a, f.ConversacionID, false)
		if err != nil {
			return ConversacionIniciada{}, err
		}
		return m.respuestaIniciada(ctx, q, a, c, f, false, true)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ConversacionIniciada{}, err
	}
	if in.IdempotencyKey != "" {
		previo, err := q.MensajeConversacionPorClave(ctx, mensajeriabd.MensajeConversacionPorClaveParams{AutorID: &a.usuario, IdempotencyKey: &in.IdempotencyKey})
		if err == nil && previo.ID != in.ID {
			return ConversacionIniciada{}, idempotenciaReutilizada()
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ConversacionIniciada{}, err
		}
	}
	// Número de salida.
	canal, err := canalDeEmpresa(ctx, q, a, in.CanalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ConversacionIniciada{}, numeroNoPermitido()
	}
	if err != nil {
		return ConversacionIniciada{}, err
	}
	ok, err := puedeIniciarDesde(ctx, q, a, canal)
	if err != nil {
		return ConversacionIniciada{}, err
	}
	if !ok {
		return ConversacionIniciada{}, numeroNoPermitido()
	}
	// Plantilla aprobada en el número.
	p, ok := PlantillaPorCodigo(in.Plantilla.Codigo)
	if !ok {
		return ConversacionIniciada{}, campoConv("plantilla.codigo", "No es una plantilla de Iris", "plantilla")
	}
	if _, ok := plantillasDeCanal(canal)[p.Codigo]; !ok {
		return ConversacionIniciada{}, reglaConv("plantilla_no_aprobada", "La plantilla no está aprobada en ese número")
	}
	if in.Plantilla.Variables == nil {
		in.Plantilla.Variables = map[string]string{}
	}
	if err := p.validarVariables(in.Plantilla.Variables); err != nil {
		return ConversacionIniciada{}, err
	}
	// Destinatario: el número lo pone Iris.
	numero, err := m.numeroDestino(ctx, q, a, in)
	if err != nil {
		return ConversacionIniciada{}, err
	}
	ahora := m.ahora()
	c, err := q.ConversacionPorContacto(ctx, mensajeriabd.ConversacionPorContactoParams{Empresa: a.empresa, CanalID: canal.ID, Numero: numero})
	nueva := false
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		var cola *int64
		if canal.ColaID != nil {
			if co, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: *canal.ColaID, Empresa: a.empresa}); err == nil && co.Activa {
				cola = canal.ColaID
			}
		}
		c, err = q.InsertarConversacionCliente(ctx, mensajeriabd.InsertarConversacionClienteParams{EmpresaID: canal.EmpresaID, CanalID: canal.ID,
			Numero: numero, ColaID: cola, UltimaActividad: ahora})
		if errors.Is(err, pgx.ErrNoRows) {
			// Llegó a la vez un mensaje del cliente: se usa esa.
			c, err = q.ConversacionPorContacto(ctx, mensajeriabd.ConversacionPorContactoParams{Empresa: a.empresa, CanalID: canal.ID, Numero: numero})
		} else {
			nueva = true
		}
		if err != nil {
			return ConversacionIniciada{}, err
		}
	case err != nil:
		return ConversacionIniciada{}, err
	}
	t.FijarEntidad("conversacion", c.ID)
	if !nueva {
		if err := reutilizable(ctx, q, a, c); err != nil {
			return ConversacionIniciada{}, err
		}
	}
	antes := c
	// El permiso de contacto es el del destinatario que eligió el agente (verificado: el número sale
	// de Argos o del contacto de Iris).
	prueba := c
	prueba.ClienteID, prueba.ContactoIrisID, prueba.ClienteVerificado = in.ClienteID, in.ContactoIrisID, in.ClienteID != nil
	if err := permisoPlantilla(ctx, q, prueba, !ventanaDe(c.UltimoEntranteEn, ahora).Abierta); err != nil {
		return ConversacionIniciada{}, err
	}
	// La asociación: se llena si estaba vacía; cambiarla exige conversaciones.asociar. El prospecto
	// se conserva.
	mismo := (in.ClienteID != nil && c.ClienteID != nil && *c.ClienteID == *in.ClienteID) ||
		(in.ContactoIrisID != nil && c.ContactoIrisID != nil && *c.ContactoIrisID == *in.ContactoIrisID)
	vacia := c.ClienteID == nil && c.ContactoIrisID == nil
	switch {
	case mismo:
		if in.ClienteID != nil {
			c.ClienteVerificado = true
			if c.ServicioID == nil {
				c.ServicioID = in.ServicioID
			}
		}
	case !vacia && !nucleoTiene(ctx, nucleo.PermConversacionesAsociar):
		return ConversacionIniciada{}, nucleo.Prohibido("La conversación de ese número está asociada a otro contacto: cambiarla exige conversaciones.asociar")
	default:
		c.ClienteID, c.ServicioID, c.ContactoIrisID = in.ClienteID, in.ServicioID, in.ContactoIrisID
		c.ClienteVerificado, c.VerificadoPor, c.Asociacion = in.ClienteID != nil, nil, nil
		if in.ClienteID != nil {
			c.Asociacion = nombrePtr("manual")
		}
	}
	if c.Estado == EstadoConvResuelta {
		c.ReabiertaEn, c.ResueltaEn = &ahora, nil
		if canal.ColaID != nil {
			if co, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: *canal.ColaID, Empresa: a.empresa}); err == nil && co.Activa {
				c.ColaID = canal.ColaID
			}
		}
	}
	c.Estado = EstadoConvEsperando
	asignada := c.AsignadoA == nil || *c.AsignadoA != a.usuario
	if asignada {
		c.AsignadoA, c.AsignadoEn = &a.usuario, &ahora
	}
	c.EsperaDesde, c.SlaVenceEn, c.SlaAvisado = nil, nil, false
	texto := p.Render(in.Plantilla.Variables)
	vars, err := json.Marshal(in.Plantilla.Variables)
	if err != nil {
		return ConversacionIniciada{}, err
	}
	n := nuevoMensaje{id: in.ID, direccion: DirSaliente, tipo: "plantilla", texto: &texto, plantilla: &p.Codigo, variables: vars, autor: &a.usuario,
		hash: hash, estadoEntrega: nombrePtr(EntregaEnCola)}
	if in.IdempotencyKey != "" {
		n.clave = &in.IdempotencyKey
	}
	if err := m.sistema(ctx, t, &c, EventoSistema{Tipo: "iniciada", ActorNombre: actorNombre(ctx, q, a.usuario), Plantilla: &p.Codigo}); err != nil {
		return ConversacionIniciada{}, err
	}
	f, err := m.agregarMensaje(ctx, t, &c, n)
	if err != nil {
		return ConversacionIniciada{}, err
	}
	if c, err = guardarConv(ctx, q, c, true); err != nil {
		return ConversacionIniciada{}, err
	}
	if err := limpiarEspera(ctx, q, &c); err != nil {
		return ConversacionIniciada{}, err
	}
	if err := m.tocarActividad(ctx, q, a); err != nil {
		return ConversacionIniciada{}, err
	}
	t.RegistrarCambios("conversacion", c.ID, auditableConv(antes), auditableConv(c))
	if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
		return ConversacionIniciada{}, err
	}
	if err := m.encolar(ctx, t.Tx, ArgsEnviarMensaje{MensajeID: f.ID}); err != nil {
		return ConversacionIniciada{}, err
	}
	t.Anotar(map[string]any{"mensaje_id": f.ID, "nueva": nueva})
	if nueva {
		if err := t.Emitir("conversacion.creada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "canal_id": c.CanalID,
			"cola_id": c.ColaID, "cliente_id": c.ClienteID, "asociacion": c.Asociacion, "origen": "agente"}); err != nil {
			return ConversacionIniciada{}, err
		}
	}
	if asignada {
		if err := t.Emitir("conversacion.asignada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "usuario_id": a.usuario,
			"anterior_id": antes.AsignadoA, "origen": "iniciar", "actor_id": a.usuario}); err != nil {
			return ConversacionIniciada{}, err
		}
	}
	if err := t.Emitir("conversacion.iniciada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "canal_id": c.CanalID,
		"cola_id": c.ColaID, "mensaje_id": f.ID, "plantilla": p.Codigo, "autor_id": a.usuario, "nueva": nueva, "cliente_id": c.ClienteID,
		"contacto_iris_id": c.ContactoIrisID}); err != nil {
		return ConversacionIniciada{}, err
	}
	if err := t.Emitir("conversacion.mensaje_enviado", map[string]any{"conversacion_id": c.ID, "mensaje_id": f.ID, "empresa_id": c.EmpresaID,
		"autor_id": a.usuario, "tipo": f.Tipo, "plantilla": f.Plantilla, "respuesta_rapida_id": nil}); err != nil {
		return ConversacionIniciada{}, err
	}
	return m.respuestaIniciada(ctx, q, a, c, f, nueva, false)
}

// numeroDestino: el teléfono del cliente (instantánea de Argos) o del contacto de Iris.
func (m *Modulo) numeroDestino(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, in entradaIniciarFinal) (string, error) {
	if in.ContactoIrisID != nil {
		k, err := q.ContactoIrisDeEmpresa(ctx, mensajeriabd.ContactoIrisDeEmpresaParams{ID: *in.ContactoIrisID, Empresa: a.empresa})
		if errors.Is(err, pgx.ErrNoRows) {
			return "", campoConv("contactoIrisId", "No es un contacto de Iris de la empresa", "contacto")
		}
		return k.Telefono, err
	}
	var c argos.ContactoArgos
	if in.contacto != nil {
		c = *in.contacto
	} else {
		// Sin Argos configurado: la instantánea local.
		f, err := q.ClienteInstantaneaEnEmpresa(ctx, mensajeriabd.ClienteInstantaneaEnEmpresaParams{ID: *in.ClienteID, Empresa: a.empresa})
		if errors.Is(err, pgx.ErrNoRows) {
			return "", clienteFueraDeEmpresaConv()
		}
		if err != nil {
			return "", err
		}
		c = argos.ContactoArgos{Existe: true, Empresas: []int64{a.empresa}, Movil: f.Movil, Telefono: f.Telefono}
		if in.ServicioID != nil {
			ss, err := q.ServiciosInstantanea(ctx, f.ID)
			if err != nil {
				return "", err
			}
			c.Servicios = map[int64]int64{}
			for _, s := range ss {
				c.Servicios[s.ID] = s.EmpresaID
			}
		}
	}
	if !c.Existe || (a.empresa != 0 && !c.EnEmpresa(a.empresa)) {
		return "", clienteFueraDeEmpresaConv()
	}
	if in.ServicioID != nil {
		if e, ok := c.Servicios[*in.ServicioID]; !ok || (a.empresa != 0 && e != a.empresa) {
			return "", nucleo.NuevoProblema(http.StatusUnprocessableEntity, "servicio_fuera_de_empresa", "Servicio fuera de la empresa",
				"El servicio no es del cliente o no es de la empresa", nucleo.ErrorCampo{Field: "servicioId", Message: "No es un servicio del cliente en la empresa", Rule: "empresa"})
		}
	}
	numero := destinoDe(c, CanalWhatsApp)
	if numero == "" {
		return "", reglaConv(MotivoSinContacto, "El cliente no tiene un número de WhatsApp en Argos")
	}
	return numero, nil
}

func (m *Modulo) respuestaIniciada(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, c mensajeriabd.MensajeriaConversacion,
	f mensajeriabd.MensajeriaConversacionMensaje, nueva, repetido bool) (ConversacionIniciada, error) {
	d, err := m.detalleConv(ctx, q, a.usuario, c)
	if err != nil {
		return ConversacionIniciada{}, err
	}
	ms, err := mensajesConv(ctx, q, []mensajeriabd.MensajeriaConversacionMensaje{f})
	if err != nil {
		return ConversacionIniciada{}, err
	}
	return ConversacionIniciada{Conversacion: d, Mensaje: ms[0], Nueva: nueva, Repetido: repetido}, nil
}

// Pasar a… ---------------------------------------------------------------------------------------

// EntradaPasarConv es POST /conversaciones/{id}/pasar.
type EntradaPasarConv struct {
	ID        int64   `json:"-"`
	Version   int32   `json:"version" minimum:"1"`
	UsuarioID int64   `json:"usuarioId" minimum:"1" doc:"Compañero de la cola (miembro o supervisor de la cola)"`
	Nota      *string `json:"nota,omitempty" maxLength:"1000" doc:"Nota interna para quien la recibe"`
}

// MarshalJSON: sin la nota (puede tener datos del cliente).
func (e EntradaPasarConv) MarshalJSON() ([]byte, error) {
	largo := 0
	if e.Nota != nil {
		largo = len([]rune(*e.Nota))
	}
	return json.Marshal(map[string]any{"id": e.ID, "version": e.Version, "usuario_id": e.UsuarioID, "largo_nota": largo})
}

// PaseConversacion es la respuesta de «Pasar a…».
type PaseConversacion struct {
	Conversacion      ConversacionClienteDetalle `json:"conversacion"`
	Destino           UsuarioRef                 `json:"destino"`
	DestinoDisponible bool                       `json:"destinoDisponible" doc:"false: se pasó a alguien no disponible (el panel avisa)"`
}

func nuevoPasarConv(m *Modulo) *auditoria.Caso[EntradaPasarConv, PaseConversacion] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPasarConv, PaseConversacion]{
		Accion: "conversaciones.pasar", Permiso: nucleo.PermConversacionesPasar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPasarConv) (PaseConversacion, error) {
			var destino UsuarioRef
			disponible := false
			d, err := m.accionConv(ctx, t, in.ID, in.Version, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				mia := c.AsignadoA != nil && *c.AsignadoA == a.usuario
				if !mia && !a.supervisa {
					sup, err := supervisaColaDe(ctx, q, *c, a.usuario)
					if err != nil {
						return err
					}
					if !sup {
						return nucleo.NuevoProblema(http.StatusForbidden, "no_es_tuya", "No es tuya",
							"Solo quien tiene la conversación o un supervisor (global o de su cola) la puede pasar")
					}
				}
				if c.AsignadoA != nil && *c.AsignadoA == in.UsuarioID {
					return reglaConv("mismo_agente", "La conversación ya la tiene esa persona")
				}
				u, err := puedeAtender(ctx, t, *c, in.UsuarioID)
				if err != nil {
					return err
				}
				destino = u
				disp, err := disponibilidades(ctx, q, empresaDeConv(*c), []int64{u.ID})
				if err != nil {
					return err
				}
				disponible = dispDe(disp, u.ID).Estado == Disponible
				var notaID any
				if in.Nota != nil && strings.TrimSpace(*in.Nota) != "" {
					nota := strings.TrimSpace(*in.Nota)
					f, err := m.agregarMensaje(ctx, t, c, nuevoMensaje{direccion: DirNota, tipo: "texto", texto: &nota, autor: &a.usuario})
					if err != nil {
						return err
					}
					notaID = f.ID
				}
				anterior := c.AsignadoA
				ahora := m.ahora()
				c.AsignadoA, c.AsignadoEn = &u.ID, &ahora
				ev := EventoSistema{Tipo: "pasada", ActorNombre: actorNombre(ctx, q, a.usuario), AsignadoNombre: &u.Nombre}
				if !disponible {
					ev.Detalle = nombrePtr("no_disponible")
				}
				if err := m.sistema(ctx, t, c, ev); err != nil {
					return err
				}
				return t.Emitir("conversacion.asignada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "usuario_id": u.ID,
					"anterior_id": anterior, "origen": "pasar", "actor_id": a.usuario, "disponible": disponible, "nota_id": notaID})
			})
			if err != nil {
				return PaseConversacion{}, err
			}
			return PaseConversacion{Conversacion: d, Destino: destino, DestinoDisponible: disponible}, nil
		},
	})
}

func nucleoTiene(ctx context.Context, p nucleo.Permiso) bool {
	return contexto.PermisosDe(ctx).Tiene(string(p))
}

func bytesIguales(a, b []byte) bool { return bytes.Equal(a, b) }
