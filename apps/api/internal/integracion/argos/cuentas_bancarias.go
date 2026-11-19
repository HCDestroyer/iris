package argos

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Cuentas bancarias (contrato 0.8.0 §7.8, pedido B5): Argos es la única fuente. Iris guarda una
// instantánea de lectura (integracion.argos_cuentas_bancarias) con los campos que usa y la refresca:
//
//   - con la lista completa (GET /cuentas-bancarias, sin `activa`: activas e inactivas), al activar
//     el conector y cada 6 h (ArgsSincronizarCuentas). Sin las cabeceras de §7.1, que son opcionales
//     desde la 0.8.0: con un Argos anterior (contrato < 0.8.0) la lista sin cabeceras responde 400,
//     así que el job no la pide y la instantánea se llena desde una conversación (CuentasDeEmpresa);
//   - con el webhook cuenta_bancaria.cambiada (solo a conectores ≥ 0.8.0): se relee la cuenta con
//     GET /cuentas-bancarias/:id y se guarda lo que diga Argos, nunca el payload;
//   - al usarla, si la última lista de la empresa tiene más de 24 h (CuentasDeEmpresa);
//   - antes de registrar una boleta, releyendo la cuenta elegida (ReleerCuentaBancaria).
//
// Nunca se edita en Iris. El número y el titular no van en eventos ni en la auditoría: solo el id.

// EventoCuentaBancariaCambiada es el webhook de Argos (§2.3).
const EventoCuentaBancariaCambiada = "cuenta_bancaria.cambiada"

// EventoCuentaBancariaCambiadaInterno es el evento interno (outbox): una cuenta de la instantánea
// cambió (por el webhook o porque una relectura trajo algo distinto).
const EventoCuentaBancariaCambiadaInterno = "argos.cuenta_bancaria_cambiada"

// ContratoCuentasSinCabeceras es el primer contrato con el que Argos lee la lista de cuentas y la de
// bancos sin X-Iris-Actor ni X-Iris-Conversacion (§2.5, A28).
const ContratoCuentasSinCabeceras = "0.8.0"

// VigenciaCuentas: edad de la lista de una empresa tras la cual se relee al usarla (§7.8, §4.1).
const VigenciaCuentas = 24 * time.Hour

// OmitidoArgosAnterior080: el job de cuentas no corre con un Argos anterior a 0.8.0 (§2.5).
const OmitidoArgosAnterior080 = "argos_anterior_080"

// Tipos de cuenta de Argos (0.8.0).
var tiposCuenta = []string{"monetaria", "ahorro"}

// normalizar recorta y deja nulos el titular y el tipo que no sirven (vacío, otro tipo).
func (c *CuentaBancariaArgos) normalizar() {
	if c.Titular != nil {
		if v := strings.TrimSpace(*c.Titular); v == "" {
			c.Titular = nil
		} else {
			c.Titular = &v
		}
	}
	if c.Tipo != nil {
		v := strings.ToLower(strings.TrimSpace(*c.Tipo))
		if slices.Contains(tiposCuenta, v) {
			c.Tipo = &v
		} else {
			c.Tipo = nil
		}
	}
	c.Numero, c.Moneda = strings.TrimSpace(c.Numero), strings.ToUpper(strings.TrimSpace(c.Moneda))
}

// CuentaDeInstantanea arma la cuenta con la fila de la instantánea.
func CuentaDeInstantanea(f argosbd.IntegracionArgosCuentasBancaria) CuentaBancariaArgos {
	e := f.EmpresaID
	c := CuentaBancariaArgos{ID: f.ID, EmpresaID: &e, BancoID: f.BancoID, Banco: f.BancoNombre, Numero: f.Numero, Alias: f.Alias,
		Titular: f.Titular, Tipo: f.Tipo, Moneda: f.Moneda, Activa: f.Activa, SincronizadoEn: f.SincronizadoEn}
	c.armarNombre()
	return c
}

// Lecturas en Argos ---------------------------------------------------------------------------------

// ListaCuentasBancarias: GET /cuentas-bancarias sin filtro (activas e inactivas), sin las cabeceras
// de §7.1 (Argos ≥ 0.8.0). Arreglo plano, sin paginar.
func (c *Cliente) ListaCuentasBancarias(ctx context.Context) ([]CuentaBancariaArgos, error) {
	p, err := conector.ListaDe[CuentaBancariaArgos](ctx, c.Cliente, "/cuentas-bancarias", nil)
	return p.Data, err
}

// ObtenerCuentaBancaria: GET /cuentas-bancarias/:id (no exige las cabeceras de §7.1). 404 = no
// existe (las cuentas nunca se borran); 403 empresa_no_permitida = fuera del alcance del token.
func (c *Cliente) ObtenerCuentaBancaria(ctx context.Context, id int64) (CuentaBancariaArgos, error) {
	var k CuentaBancariaArgos
	err := c.Pedir(ctx, http.MethodGet, "/cuentas-bancarias/"+strconv.FormatInt(id, 10), nil, nil, true, &conector.Recurso{Destino: &k})
	if err == nil && k.ID == 0 {
		k.ID = id
	}
	return k, err
}

// EsFueraDeAlcance: Argos respondió 403 (la cuenta es de una empresa fuera del alcance del token).
func EsFueraDeAlcance(err error) bool {
	var er *conector.ErrorRemoto
	return errors.As(err, &er) && er.Status == http.StatusForbidden && er.Codigo == "empresa_no_permitida"
}

// EsCuentaInactiva: POST /comprobantes respondió 422 validacion con rule «inactiva» en
// cuenta_bancaria_id (0.8.0): la cuenta se desactivó en Argos.
func EsCuentaInactiva(err error) bool {
	var er *conector.ErrorRemoto
	return errors.As(err, &er) && er.Status == http.StatusUnprocessableEntity && slices.Contains(er.Campos, "cuenta_bancaria_id") &&
		slices.Contains(er.Reglas, "inactiva")
}

// Guardar en la instantánea ---------------------------------------------------------------------------

// lecturaCuentas es lo que se leyó de Argos (fuera de transacción) para guardar.
type lecturaCuentas struct {
	cuentas []CuentaBancariaArgos
	// leidoEn: el reloj de la base antes de pedir a Argos (gana la lectura más reciente).
	leidoEn time.Time
	// completa: la lista trae todas las cuentas del alcance (sin filtro); empresa: la lista es de una
	// sola empresa (0 = de todas). Con completa, lo que la instantánea tenía y la lista no trajo se quita.
	completa bool
	empresa  int64
	// marcar: la lista cuenta como lectura de esa empresa (o de todas) para la vigencia de 24 h.
	marcar bool
	// fuera: la cuenta que Argos dejó fuera del alcance (403): se quita de la instantánea.
	fuera *int64
	// webhook: el entrante cuenta_bancaria.cambiada (sus campos[] y creada van en el evento).
	webhook *webhookCuenta
}

type webhookCuenta struct {
	entranteID int64
	campos     []string
	creada     bool
}

// payloadCuenta es el payload de cuenta_bancaria.cambiada (§2.3): solo nombres de campos.
type payloadCuenta struct {
	CuentaBancariaID int64    `json:"cuenta_bancaria_id"`
	EmpresaID        *int64   `json:"empresa_id"`
	Campos           []string `json:"campos"`
	Activa           *bool    `json:"activa"`
	Creada           bool     `json:"creada"`
}

// ResultadoCuentas dice qué cambió en la instantánea.
type ResultadoCuentas struct {
	Omitido    string `json:"omitido,omitempty"`
	Cuentas    int    `json:"cuentas"`
	Cambiadas  int    `json:"cambiadas"`
	Quitadas   int    `json:"quitadas"`
	Invalidas  int    `json:"invalidas"`
	Desechadas int    `json:"desechadas"`
}

// camposCambiados: los nombres de §2.3 que difieren entre la fila y la lectura (nunca valores).
func camposCambiados(f argosbd.IntegracionArgosCuentasBancaria, c CuentaBancariaArgos) []string {
	var out []string
	if !mismoPtr(f.BancoID, c.BancoID) || f.BancoNombre != c.Banco {
		out = append(out, "banco_id")
	}
	if f.Numero != c.Numero {
		out = append(out, "numero")
	}
	if f.Alias != c.Alias {
		out = append(out, "alias")
	}
	if !mismoPtr(f.Titular, c.Titular) {
		out = append(out, "titular")
	}
	if !mismoPtr(f.Tipo, c.Tipo) {
		out = append(out, "tipo")
	}
	if f.Moneda != c.Moneda {
		out = append(out, "moneda")
	}
	if f.Activa != c.Activa {
		out = append(out, "activa")
	}
	if f.EmpresaID != *c.EmpresaID {
		out = append(out, "empresa_id")
	}
	return out
}

func mismoPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// cuentaValida: lo mínimo para guardarla (Argos siempre manda id, empresa y número).
func cuentaValida(c CuentaBancariaArgos) bool {
	n := len([]rune(c.Numero))
	return c.ID > 0 && c.EmpresaID != nil && *c.EmpresaID > 0 && n >= 1 && n <= 60
}

// guardarCuentas escribe la lectura en la instantánea y emite argos.cuenta_bancaria_cambiada por cada
// cuenta que cambió (o la del webhook, siempre).
func guardarCuentas(ctx context.Context, t *auditoria.Tx, in lecturaCuentas) (ResultadoCuentas, error) {
	q := argosbd.New(t)
	res := ResultadoCuentas{Cuentas: len(in.cuentas)}
	ids := make([]int64, 0, len(in.cuentas))
	for _, c := range in.cuentas {
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	porID := make(map[int64]CuentaBancariaArgos, len(in.cuentas))
	for _, c := range in.cuentas {
		porID[c.ID] = c
	}
	// En orden de id: dos relecturas a la vez no se interbloquean.
	for _, id := range slices.Compact(ids) {
		c := porID[id]
		if !cuentaValida(c) {
			res.Invalidas++
			continue
		}
		previa, err := q.CuentaBancariaParaActualizar(ctx, c.ID)
		nueva := errors.Is(err, pgx.ErrNoRows)
		if err != nil && !nueva {
			return res, err
		}
		if !nueva && previa.SincronizadoEn.After(in.leidoEn) {
			// Una lectura más nueva ya la guardó.
			res.Desechadas++
			continue
		}
		campos := []string{}
		if nueva {
			campos = []string{"banco_id", "numero", "alias", "titular", "tipo", "moneda", "activa"}
		} else {
			campos = camposCambiados(previa, c)
		}
		if _, err := q.GuardarCuentaBancaria(ctx, argosbd.GuardarCuentaBancariaParams{ID: c.ID, EmpresaID: *c.EmpresaID, BancoID: c.BancoID,
			BancoNombre: c.Banco, Numero: c.Numero, Alias: c.Alias, Titular: c.Titular, Tipo: c.Tipo, Moneda: c.Moneda, Activa: c.Activa,
			SincronizadoEn: in.leidoEn}); err != nil {
			return res, err
		}
		var entrante *int64
		creada := nueva
		if in.webhook != nil {
			entrante, creada = &in.webhook.entranteID, in.webhook.creada
			if len(in.webhook.campos) > 0 {
				campos = in.webhook.campos
			}
		} else if len(campos) == 0 {
			continue
		}
		res.Cambiadas++
		empresa := *c.EmpresaID
		if err := t.EmitirEnEmpresa(EventoCuentaBancariaCambiadaInterno, &empresa, map[string]any{"cuenta_bancaria_id": c.ID, "empresa_id": empresa,
			"campos": campos, "activa": c.Activa, "creada": creada, "entrante_id": entrante}); err != nil {
			return res, err
		}
	}
	if in.fuera != nil {
		n, err := q.BorrarCuentaBancaria(ctx, argosbd.BorrarCuentaBancariaParams{ID: *in.fuera, LeidoEn: in.leidoEn})
		if err != nil {
			return res, err
		}
		res.Quitadas += int(n)
	}
	if in.completa {
		// Lo que la lista completa no trajo ya no está en el alcance del token: se quita (Argos nunca
		// borra cuentas; una desactivada sigue viniendo con activa: false).
		viejas, err := q.CuentasBancariasNoVistas(ctx, in.leidoEn)
		if err != nil {
			return res, err
		}
		for _, f := range viejas {
			if in.empresa != 0 && f.EmpresaID != in.empresa {
				continue
			}
			if _, ok := porID[f.ID]; ok {
				continue
			}
			if _, err := q.BorrarCuentaBancaria(ctx, argosbd.BorrarCuentaBancariaParams{ID: f.ID, LeidoEn: in.leidoEn}); err != nil {
				return res, err
			}
			res.Quitadas++
		}
	}
	if in.marcar {
		if err := q.MarcarLecturaCuentas(ctx, argosbd.MarcarLecturaCuentasParams{EmpresaID: in.empresa, LeidoEn: in.leidoEn}); err != nil {
			return res, err
		}
	}
	t.Anotar(map[string]any{"cuentas": res.Cuentas, "cambiadas": res.Cambiadas, "quitadas": res.Quitadas, "invalidas": res.Invalidas})
	return res, nil
}

func nuevoGuardarCuentas() *auditoria.Caso[lecturaCuentas, ResultadoCuentas] {
	return auditoria.Nuevo(auditoria.Caso[lecturaCuentas, ResultadoCuentas]{
		Accion: "integraciones.sincronizar_cuentas_bancarias", Acceso: auditoria.Autenticado,
		// Solo se audita lo que cambió algo (cada lectura de la bandeja no deja una fila).
		AuditarExito: func(r ResultadoCuentas) bool { return r.Cambiadas > 0 || r.Quitadas > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in lecturaCuentas) (ResultadoCuentas, error) {
			t.FijarEmpresa(in.empresa)
			return guardarCuentas(ctx, t, in)
		},
	})
}

// leerDeArgos prepara la lectura (reloj de la base antes de llamar) y normaliza lo que vino.
func relojAntes(ctx context.Context, bd nucleo.BD) (time.Time, error) {
	return argosbd.New(bd).RelojBD(ctx)
}

func normalizarCuentas(cs []CuentaBancariaArgos) []CuentaBancariaArgos {
	for i := range cs {
		cs[i].normalizar()
		cs[i].armarNombre()
	}
	return cs
}

// Carga completa (primera carga y periódico) ----------------------------------------------------------

// ArgsSincronizarCuentas relee la lista completa de cuentas bancarias (§7.8). Motivo separa la carga
// de la activación del periódico (igual que ArgsSincronizarCatalogos).
type ArgsSincronizarCuentas struct {
	Motivo string `json:"motivo,omitempty"`
}

// Kind implementa river.JobArgs.
func (ArgsSincronizarCuentas) Kind() string { return "integracion.argos_sincronizar_cuentas" }

// InsertOpts: único por minuto y por motivo.
func (ArgsSincronizarCuentas) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Minute}}
}

// CargarCuentasBancarias lee la lista completa en Argos y refresca la instantánea. Es el paso
// `cuentas_bancarias` de la primera sincronización (§2.5) y el job periódico. Sin conector activo, o
// con un Argos anterior a 0.8.0 (la lista exige cabeceras de una conversación), no hace nada y lo
// dice en Omitido.
func (m *Modulo) CargarCuentasBancarias(ctx context.Context) (ResultadoCuentas, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ResultadoCuentas{}, nucleo.ErrSinBD
	}
	c, err := m.conectorActivo(ctx, bd)
	if conectorApagado(err) {
		return ResultadoCuentas{Omitido: "conector_apagado"}, nil
	}
	if err != nil {
		return ResultadoCuentas{}, err
	}
	if !conector.VersionAlMenos(c.ContratoRemoto, ContratoCuentasSinCabeceras) {
		return ResultadoCuentas{Omitido: OmitidoArgosAnterior080}, nil
	}
	in := lecturaCuentas{completa: true, marcar: true}
	if in.leidoEn, err = relojAntes(ctx, bd); err != nil {
		return ResultadoCuentas{}, err
	}
	cs, err := m.clienteDe(c).ListaCuentasBancarias(ctx)
	if err != nil {
		return ResultadoCuentas{}, err
	}
	in.cuentas = normalizarCuentas(cs)
	return m.guardarCuentas.Ejecutar(ctx, in)
}

func conectorApagado(err error) bool {
	var p *nucleo.Problema
	return errors.As(err, &p) && p.Codigo() == "conector_apagado"
}

type trabajadorCuentas struct {
	river.WorkerDefaults[ArgsSincronizarCuentas]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorCuentas) Work(ctx context.Context, _ *river.Job[ArgsSincronizarCuentas]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	_, err := w.m.CargarCuentasBancarias(ctx)
	var er *conector.ErrorRemoto
	if errors.As(err, &er) && !er.Caido() {
		// Un 4xx de Argos (credencial, forma): reintentar no lo arregla; el periódico vuelve a probar.
		return river.JobCancel(err)
	}
	return err
}

func (w *trabajadorCuentas) Timeout(*river.Job[ArgsSincronizarCuentas]) time.Duration {
	return 2 * time.Minute
}

// Lecturas desde la bandeja ---------------------------------------------------------------------------

// CuentasEmpresa son las cuentas de la instantánea de una empresa (activas e inactivas, en el orden
// de Argos) y cuándo se leyó su lista.
type CuentasEmpresa struct {
	Cuentas []CuentaBancariaArgos
	LeidoEn *time.Time
}

// ErrCuentasViejas: la lista de la empresa tiene más de 24 h y Argos no respondió para releerla. Las
// cuentas no se ofrecen (§7.8: pasa a una persona).
var ErrCuentasViejas = errors.New("argos: las cuentas bancarias tienen más de 24 h y Argos no respondió")

// CuentasDeEmpresa devuelve las cuentas de la instantánea de la empresa de a. Si su lista tiene más
// de 24 h (o nunca se leyó), la relee primero: la lista completa sin cabeceras con Argos ≥ 0.8.0, o
// con las cabeceras de la conversación (§7.1) con uno anterior. Sin conector activo responde el 503
// conector_apagado de siempre; si Argos no responde y la lista es vieja, ErrCuentasViejas (envuelve
// el error de Argos).
func (m *Modulo) CuentasDeEmpresa(ctx context.Context, a AtencionIris) (CuentasEmpresa, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return CuentasEmpresa{}, nucleo.ErrSinBD
	}
	if a.EmpresaID == 0 {
		return CuentasEmpresa{Cuentas: []CuentaBancariaArgos{}}, nil
	}
	c, err := m.conectorActivo(ctx, bd)
	if err != nil {
		return CuentasEmpresa{}, err
	}
	q := argosbd.New(bd)
	leido, err := q.LecturaCuentasDe(ctx, a.EmpresaID)
	nunca := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !nunca {
		return CuentasEmpresa{}, err
	}
	ahora, err := relojAntes(ctx, bd)
	if err != nil {
		return CuentasEmpresa{}, err
	}
	if nunca || ahora.Sub(leido) > VigenciaCuentas {
		if errLectura := m.releerCuentasEmpresa(ctx, c, a, ahora); errLectura != nil {
			if conector.EsCaido(errLectura) && !conector.EsIlegible(errLectura) {
				return CuentasEmpresa{}, errors.Join(ErrCuentasViejas, errLectura)
			}
			return CuentasEmpresa{}, errLectura
		}
		leido = ahora
	}
	filas, err := q.CuentasBancariasDeEmpresa(ctx, a.EmpresaID)
	if err != nil {
		return CuentasEmpresa{}, err
	}
	out := CuentasEmpresa{Cuentas: make([]CuentaBancariaArgos, 0, len(filas)), LeidoEn: &leido}
	for _, f := range filas {
		out.Cuentas = append(out.Cuentas, CuentaDeInstantanea(f))
	}
	return out, nil
}

// releerCuentasEmpresa lee la lista en Argos y la guarda.
func (m *Modulo) releerCuentasEmpresa(ctx context.Context, c conector.Leido, a AtencionIris, ahora time.Time) error {
	in := lecturaCuentas{leidoEn: ahora, marcar: true}
	var cs []CuentaBancariaArgos
	var err error
	if conector.VersionAlMenos(c.ContratoRemoto, ContratoCuentasSinCabeceras) {
		// La lista completa del alcance (activas e inactivas), como la carga periódica.
		in.completa = true
		cs, err = m.clienteDe(c).ListaCuentasBancarias(ctx)
	} else {
		// Argos anterior a 0.8.0: con las cabeceras de la conversación y la empresa (X-Empresa-Id), sin
		// `activa` para enterarse también de las que se desactivaron. Cuenta solo para esa empresa.
		in.empresa = a.EmpresaID
		var cli *Cliente
		if cli, err = m.clienteAtencion(ctx, a); err == nil {
			var p conector.Pagina[CuentaBancariaArgos]
			p, err = conector.ListaDe[CuentaBancariaArgos](ctx, cli.Cliente, "/cuentas-bancarias", url.Values{})
			cs = p.Data
		}
	}
	if err != nil {
		return err
	}
	in.cuentas = normalizarCuentas(cs)
	if !in.completa {
		// Una lista con cabecera de empresa no autoriza a quitar las de otras.
		in.cuentas = slices.DeleteFunc(in.cuentas, func(k CuentaBancariaArgos) bool { return k.EmpresaID == nil || *k.EmpresaID != a.EmpresaID })
	}
	_, err = m.guardarCuentas.Ejecutar(ctx, in)
	return err
}

// ReleerCuentaBancaria relee una cuenta en Argos (GET /cuentas-bancarias/:id) y refresca la
// instantánea con lo que diga Argos (§7.8: antes de registrar una boleta; una instantánea vieja nunca
// decide a dónde se registra un pago). Un 403 la quita de la instantánea. Los errores de Argos
// (404, 403, caída) vuelven tal cual para que quien llama decida.
func (m *Modulo) ReleerCuentaBancaria(ctx context.Context, id int64) (CuentaBancariaArgos, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return CuentaBancariaArgos{}, nucleo.ErrSinBD
	}
	c, err := m.conectorActivo(ctx, bd)
	if err != nil {
		return CuentaBancariaArgos{}, err
	}
	ahora, err := relojAntes(ctx, bd)
	if err != nil {
		return CuentaBancariaArgos{}, err
	}
	k, errArgos := m.clienteDe(c).ObtenerCuentaBancaria(ctx, id)
	switch {
	case errArgos == nil:
		k.normalizar()
		k.armarNombre()
		k.SincronizadoEn = ahora
		if _, err := m.guardarCuentas.Ejecutar(ctx, lecturaCuentas{cuentas: []CuentaBancariaArgos{k}, leidoEn: ahora}); err != nil {
			return CuentaBancariaArgos{}, err
		}
		return k, nil
	case EsFueraDeAlcance(errArgos):
		if _, err := m.guardarCuentas.Ejecutar(ctx, lecturaCuentas{fuera: &id, leidoEn: ahora}); err != nil {
			return CuentaBancariaArgos{}, err
		}
	}
	return CuentaBancariaArgos{}, errArgos
}

// Webhook cuenta_bancaria.cambiada --------------------------------------------------------------------

// leerCuentaWebhook relee la cuenta del webhook (fuera de transacción). omitido dice por qué se
// ignora; un error de Argos (caído) reintenta el job.
func (p *ProcesarEntrante) leerCuentaWebhook(ctx context.Context, bd nucleo.BD, e conector.Entrante) (*lecturaCuentas, string, error) {
	var d payloadCuenta
	if err := conector.Decodificar(e.Payload, &d); err != nil || d.CuentaBancariaID <= 0 {
		return nil, "payload_invalido", nil
	}
	ahora, err := relojAntes(ctx, bd)
	if err != nil {
		return nil, "", err
	}
	cli, err := p.clienteArgos(ctx, bd, e.ConectorID)
	if err != nil {
		return nil, "", err
	}
	w := &webhookCuenta{entranteID: e.ID, campos: camposWebhook(d.Campos), creada: d.Creada}
	k, err := cli.ObtenerCuentaBancaria(ctx, d.CuentaBancariaID)
	switch {
	case err == nil:
		k.normalizar()
		return &lecturaCuentas{cuentas: []CuentaBancariaArgos{k}, leidoEn: ahora, webhook: w}, "", nil
	case EsFueraDeAlcance(err):
		id := d.CuentaBancariaID
		return &lecturaCuentas{fuera: &id, leidoEn: ahora}, "cuenta_fuera_de_alcance", nil
	case conector.EsNoEncontradoConfirmado(err):
		return nil, "cuenta_no_encontrada", nil
	}
	return nil, "", err
}

// camposWebhook: solo los nombres de campo del contrato (§2.3); lo demás se descarta.
func camposWebhook(cs []string) []string {
	validos := []string{"banco_id", "numero", "alias", "titular", "tipo", "moneda", "activa"}
	out := []string{}
	for _, c := range cs {
		if slices.Contains(validos, c) && !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}
