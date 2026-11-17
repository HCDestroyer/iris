package mensajeria

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/voz"
)

// El servidor de IA como proveedor del asistente de chat (voz.md §4.11): el adaptador compatible
// con OpenAI de siempre, con otro transporte (mTLS + firma, sin Authorization) y con el reparto
// entre empresas. No hay cola: sin plaza, falla al instante como una falla del proveedor.

// Códigos propios de este proveedor.
const (
	// ErrIAOcupado: no hay plaza ahora (tope del servidor, de la empresa o plaza garantizada de otra).
	ErrIAOcupado = "servidor_ia_ocupado"
	// ErrIAServidorNoDisponible: el servidor de IA no está activo, compatible y contestando, o la
	// empresa no puede usarlo.
	ErrIAServidorNoDisponible = "servidor_ia_no_disponible"
)

func esIP(s string) bool {
	_, err := netip.ParseAddr(s)
	return err == nil
}

// cubetasLatencia: límites superiores (ms) del histograma de latencia; la última es «más».
var cubetasLatencia = []int64{250, 500, 1000, 2000, 4000, 8000, 15000, 30000, 60000, 90000, 120000}

func cubetaDe(ms int64) int32 {
	for i, tope := range cubetasLatencia {
		if ms <= tope {
			return int32(i + 1) //nolint:gosec // índice pequeño
		}
	}
	return int32(len(cubetasLatencia) + 1) //nolint:gosec // índice pequeño
}

type entradaPlaza struct {
	Empresa int64
	Modelo  string
	Plazo   time.Duration
}

// plazaIA es una plaza tomada, con las credenciales del canal para usarla.
type plazaIA struct {
	id       uuid.UUID
	conexion conexionMotor
}

// usoPeticionIA es lo que se anota al terminar una petición (nunca el texto).
type usoPeticionIA struct {
	Plaza     uuid.UUID
	MotorID   int64
	Empresa   int64
	Modelo    string
	Uso       UsoIA
	LatenciaM int64
	// Motivo del error ("" = bien); Rechazo: no hubo plaza.
	Motivo  string
	Rechazo bool
}

func nuevosCasosPlazas(m *Modulo, k *CasosServidorIA) {
	anotar := func(ctx context.Context, q *mensajeriabd.Queries, u usoPeticionIA) error {
		hora := m.ahora().Truncate(time.Hour)
		modelo := u.Modelo
		if !voz.ReNombreModelo.MatchString(modelo) {
			modelo = "?"
		}
		if err := q.AsegurarUsoIA(ctx, mensajeriabd.AsegurarUsoIAParams{MotorID: u.MotorID, Hora: hora, EmpresaID: u.Empresa, Modelo: modelo}); err != nil {
			return err
		}
		p := mensajeriabd.SumarUsoIAParams{MotorID: u.MotorID, Hora: hora, EmpresaID: u.Empresa, Modelo: modelo, Motivo: u.Motivo}
		switch {
		case u.Rechazo:
			p.Rechazos = 1
		default:
			p.Peticiones, p.TokensEntrada, p.TokensSalida, p.LatenciaMs, p.Cubeta = 1, u.Uso.Entrada, u.Uso.Salida, u.LatenciaM, cubetaDe(u.LatenciaM)
		}
		return q.SumarUsoIA(ctx, p)
	}

	// reservar: decide si se puede llamar y toma la plaza en la misma sentencia.
	k.reservar = auditoria.Nuevo(auditoria.Caso[entradaPlaza, plazaIA]{
		Accion: "servidor_ia.usar", Acceso: auditoria.Autenticado, SinEmpresa: true, AuditarExito: sinAuditar[plazaIA],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaPlaza) (plazaIA, error) {
			q := mensajeriabd.New(t)
			// El candado de la fila del servidor pone en fila a las reservas: se toma ANTES de contar las
			// plazas (en READ COMMITTED, una sentencia que espera un candado cuenta con la foto de antes
			// de esperar), y la cuenta y la reserva van después, en esta misma transacción.
			f, err := q.MotorIAUnicoParaActualizar(ctx)
			noDisponible := &ErrorIA{Codigo: ErrIAServidorNoDisponible, Detalle: "El servidor de IA no está disponible ahora"}
			if errors.Is(err, pgx.ErrNoRows) {
				return plazaIA{}, noDisponible
			}
			if err != nil {
				return plazaIA{}, err
			}
			// Caído, incompatible, desactivado o sin permiso para esta empresa: falla al instante, sin
			// llamar al servidor.
			if !motorUtilizable(f) || !empresaPermitida(f, in.Empresa) || !slices.Contains(modelosUtilizables(f), in.Modelo) {
				return plazaIA{}, noDisponible
			}
			c, err := m.conexionDeMotor(f)
			if err != nil || len(c.token) != voz.LargoToken {
				return plazaIA{}, noDisponible
			}
			id, err := uuid.NewV7()
			if err != nil {
				return plazaIA{}, err
			}
			_, err = q.ReservarPlazaIA(ctx, mensajeriabd.ReservarPlazaIAParams{PlazaID: id, MotorID: f.ID, EmpresaID: in.Empresa,
				Ahora: m.ahora(), VenceEn: m.ahora().Add(in.Plazo + 30*time.Second)})
			if errors.Is(err, pgx.ErrNoRows) {
				// Sin plaza: se anota el rechazo en otra transacción (esta termina en error).
				t.AlFallar(func(ctx context.Context, tx pgx.Tx) error {
					return anotar(ctx, mensajeriabd.New(tx), usoPeticionIA{MotorID: f.ID, Empresa: in.Empresa, Modelo: in.Modelo, Rechazo: true, Motivo: "ocupado"})
				})
				return plazaIA{}, &ErrorIA{Codigo: ErrIAOcupado, Detalle: "El servidor de IA está ocupado ahora; intenta de nuevo en un momento"}
			}
			if err != nil {
				return plazaIA{}, err
			}
			return plazaIA{id: id, conexion: c}, nil
		},
	})

	// terminar: suelta la plaza y anota el uso (por hora, empresa y modelo).
	k.terminar = auditoria.Nuevo(auditoria.Caso[usoPeticionIA, struct{}]{
		Accion: "servidor_ia.usar", Acceso: auditoria.Autenticado, SinEmpresa: true, AuditarExito: sinAuditar[struct{}],
		Logica: func(ctx context.Context, t *auditoria.Tx, in usoPeticionIA) (struct{}, error) {
			q := mensajeriabd.New(t)
			if err := q.SoltarPlazaIA(ctx, in.Plaza); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, anotar(ctx, q, in)
		},
	})
}

// transporteFirmado manda las peticiones del adaptador compatible con OpenAI por el canal
// autenticado del servidor de IA: la firma y el mTLS los pone voz.Cliente; aquí no hay Bearer.
type transporteFirmado struct{ cli *voz.Cliente }

func (t transporteFirmado) RoundTrip(r *http.Request) (*http.Response, error) {
	var cuerpo []byte
	if r.Body != nil {
		var err error
		if cuerpo, err = io.ReadAll(io.LimitReader(r.Body, voz.TopePeticionLLM+1)); err != nil {
			return nil, err
		}
		_ = r.Body.Close()
	}
	estado := http.StatusOK
	// Iris aplica su propio tope de lectura a la respuesta (no se fía del servidor).
	out, err := t.cli.Hacer(r.Context(), r.Method, r.URL.Path, cuerpo, voz.TopeRespuestaLLM)
	if err != nil {
		var ep *voz.ErrorPuerta
		if !errors.As(err, &ep) {
			return nil, err
		}
		estado, out = ep.Estado, nil
	}
	return &http.Response{StatusCode: estado, Status: http.StatusText(estado), Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(out)), Request: r}, nil
}

// proveedorServidorIA: toma una plaza, llama por el canal firmado, suelta la plaza y anota el uso.
type proveedorServidorIA struct {
	m       *Modulo
	empresa int64
	modelo  string
	plazo   time.Duration
}

func motivoUso(err error) string {
	if err == nil {
		return ""
	}
	var e *ErrorIA
	if !errors.As(err, &e) {
		return "otro"
	}
	switch {
	case e.Codigo == ErrIANoResponde && e.Status == 0:
		return "tiempo_agotado"
	case e.Codigo == ErrIANoResponde:
		return "no_disponible"
	case e.Codigo == ErrIARespInvalida:
		return "respuesta_invalida"
	}
	return "otro"
}

func (p *proveedorServidorIA) con(ctx context.Context, modelo string, f func(Proveedor) (UsoIA, error)) error {
	k := p.m.ServidorIA
	plaza, err := k.reservar.Ejecutar(ctx, entradaPlaza{Empresa: p.empresa, Modelo: modelo, Plazo: p.plazo})
	if err != nil {
		var e *ErrorIA
		if errors.As(err, &e) {
			return e
		}
		return &ErrorIA{Codigo: ErrIAServidorNoDisponible, Detalle: "El servidor de IA no está disponible ahora"}
	}
	inicio := time.Now()
	uso, err := func() (UsoIA, error) {
		cli, err := p.m.clienteMotor(plaza.conexion, plaza.conexion.token)
		if err != nil {
			return UsoIA{}, &ErrorIA{Codigo: ErrIAServidorNoDisponible, Detalle: "El servidor de IA no está disponible ahora"}
		}
		defer cli.Cerrar()
		base := plaza.conexion.fila.Url + voz.Base + "/llm"
		return f(&proveedorOpenAI{base: base, http: &http.Client{Transport: transporteFirmado{cli: cli}, Timeout: p.plazo,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})
	}()
	// La plaza se suelta aunque el pedido se haya cancelado.
	fin, cancelar := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelar()
	_, _ = k.terminar.Ejecutar(fin, usoPeticionIA{Plaza: plaza.id, MotorID: plaza.conexion.fila.ID, Empresa: p.empresa, Modelo: modelo,
		Uso: uso, LatenciaM: time.Since(inicio).Milliseconds(), Motivo: motivoUso(err)})
	return err
}

// Generar llama al modelo. Para el tope de tokens cuenta max(lo que declara el servidor, la
// estimación propia): un servidor que declare 0 no regala tokens.
func (p *proveedorServidorIA) Generar(ctx context.Context, in PedidoIA) (RespuestaIA, UsoIA, error) {
	var resp RespuestaIA
	var uso UsoIA
	modelo := in.Modelo
	if modelo == "" {
		modelo = p.modelo
	}
	err := p.con(ctx, modelo, func(prov Proveedor) (UsoIA, error) {
		var err error
		resp, uso, err = prov.Generar(ctx, in)
		if err != nil {
			return UsoIA{}, err
		}
		entradaMin := int64(len([]rune(in.Instrucciones))+len([]rune(in.Contenido)))/3 + 1
		salidaMin := int64(len([]rune(resp.Texto)))/3 + 1
		uso.Entrada, uso.Salida = max(uso.Entrada, entradaMin), max(uso.Salida, salidaMin)
		return uso, nil
	})
	return resp, uso, err
}

// Modelos lista los modelos que el servidor deja usar (los que Iris ya validó en el último latido;
// no se llama al servidor ni se gasta una plaza).
func (p *proveedorServidorIA) Modelos(ctx context.Context) ([]ModeloIA, error) {
	f, err := p.m.ServidorIA.motorActual.Ejecutar(ctx, struct{}{})
	if err != nil || f == nil || !motorUtilizable(*f) || !empresaPermitida(*f, p.empresa) {
		return nil, &ErrorIA{Codigo: ErrIAServidorNoDisponible, Detalle: "El servidor de IA no está disponible ahora"}
	}
	out := []ModeloIA{}
	for _, id := range modelosUtilizables(*f) {
		out = append(out, ModeloIA{ID: id, Nombre: id})
	}
	return out, nil
}

// --- Job del latido -------------------------------------------------------------------------------

// ArgsServidorIA: el latido del servidor de IA, cada 10 s.
type ArgsServidorIA struct{}

// Kind implementa river.JobArgs.
func (ArgsServidorIA) Kind() string { return "mensajeria.servidor_ia" }

// InsertOpts: uno cada 10 s aunque haya varias réplicas.
func (ArgsServidorIA) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Cola, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByPeriod: 10 * time.Second}}
}

type trabajadorServidorIA struct {
	river.WorkerDefaults[ArgsServidorIA]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorServidorIA) Work(ctx context.Context, _ *river.Job[ArgsServidorIA]) error {
	return w.m.MantenerServidorIA(ctx, w.bd)
}
