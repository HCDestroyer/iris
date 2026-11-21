package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/voz"
)

// Lo que corren los workers, para las pruebas (sin River).

func (m *Modulo) ProcesarEnvio(ctx context.Context, id uuid.UUID, ultimo bool) (time.Duration, error) {
	return m.procesarEnvio(ctx, id, ultimo)
}

// PlantillasDeCanal: con qué plantilla sale cada código por el número (uso "" = cualquiera).
func (m *Modulo) PlantillasDeCanal(ctx context.Context, bd nucleo.BD, canal int64, uso string) (map[string]string, error) {
	q := mensajeriabd.New(bd)
	c, err := q.CanalPorID(ctx, canal)
	if err != nil {
		return nil, err
	}
	p, err := m.plantillasDeCanal(ctx, q, c, uso)
	out := map[string]string{}
	for k, v := range p {
		out[k] = v.Nombre
	}
	return out, err
}

// ProgramarSincronizaciones es lo que hace el mantenimiento cada minuto con las plantillas.
func (m *Modulo) ProgramarSincronizaciones(ctx context.Context, bd nucleo.BD) error {
	return m.programarSincronizaciones(ctx, bd)
}

func (m *Modulo) DespacharWebhooks(ctx context.Context, bd nucleo.BD) error {
	return m.despacharWebhooks(ctx, bd)
}

func (m *Modulo) ProcesarEntrante(ctx context.Context, id int64) error {
	_, err := m.procesarMeta.Ejecutar(ctx, id)
	return err
}

func (m *Modulo) VencerAvisos(ctx context.Context) (int, error) {
	return m.vencerAvisos.Ejecutar(ctx, struct{}{})
}

var ErrReintentar = errReintentar

func (m *Modulo) PurgarFeedChat(ctx context.Context, bd nucleo.BD) error {
	return m.purgarFeedChat(ctx, bd)
}

func (m *Modulo) FijarTrasLeerCabeza(f func()) { m.trasLeerCabeza = f }

// FijarAntesDeLeerFlujo: se llama antes de abrir los flujos SSE (no se cambia con ellos abiertos).
func (m *Modulo) FijarAntesDeLeerFlujo(f func(usuario int64)) { m.antesDeLeerFlujo = f }

// FijarAlRevalidarFlujo: como FijarAntesDeLeerFlujo; donde es "pagina", "latido" o "toque".
func (m *Modulo) FijarAlRevalidarFlujo(f func(usuario int64, donde string)) { m.alRevalidarFlujo = f }

func (m *Modulo) ProcesarMensaje(ctx context.Context, id uuid.UUID, ultimo bool) (time.Duration, error) {
	return m.procesarMensaje(ctx, id, ultimo)
}

func (m *Modulo) DescargarMedio(ctx context.Context, id uuid.UUID, ultimo bool) error {
	return m.descargarMedio(ctx, id, ultimo)
}

func (m *Modulo) VencerSLA(ctx context.Context) (int, error) {
	return m.vencerSLA.Ejecutar(ctx, struct{}{})
}

func (m *Modulo) DespacharBoletas(ctx context.Context, bd nucleo.BD) error {
	return m.despacharBoletas(ctx, bd)
}

// DespacharResoluciones aplica las resoluciones de boletas que dejó el webhook de Argos.
func (m *Modulo) DespacharResoluciones(ctx context.Context, bd nucleo.BD) error {
	return m.despacharResoluciones(ctxSistema(ctx, bd), bd)
}

// MotivoParaCliente es lo que el cliente lee del rechazo: texto fijo por causa y, si pasa el filtro, el motivo libre.
func MotivoParaCliente(causa string, libre *string) string { return motivoParaCliente(&causa, libre) }

// DecidirBoletas da los casos de aprobar y rechazar (para probar actores que no entran por HTTP).
func (m *Modulo) DecidirBoletas() *DecidirBoleta { return m.boletas }

func (m *Modulo) VencerDisponibilidad(ctx context.Context, bd nucleo.BD) int {
	n, _ := m.vencerDisponibilidades(ctx, bd)
	return n
}

func (m *Modulo) DispararBots(ctx context.Context, bd nucleo.BD) int {
	n, _ := m.dispararBots(ctx, bd)
	return n
}

// VencerDisponibilidadConError y DispararBotsConError devuelven además el primer error de una fila
// (el barrido sigue con las demás).
func (m *Modulo) VencerDisponibilidadConError(ctx context.Context, bd nucleo.BD) (int, error) {
	return m.vencerDisponibilidades(ctx, bd)
}

func (m *Modulo) DispararBotsConError(ctx context.Context, bd nucleo.BD) (int, error) {
	return m.dispararBots(ctx, bd)
}

// FijarFallarFila hace fallar las filas del barrido para las que f devuelve un error.
func (m *Modulo) FijarFallarFila(f func(id int64) error) { m.fallarFila = f }

func (m *Modulo) PurgarFotosSinEnviar(ctx context.Context, bd nucleo.BD) error {
	return m.purgarFotosSinEnviar(ctx, bd)
}

// ConsultasResumen: cuántas veces GET /chat/resumen calculó (sin el tope de frecuencia).
func (m *Modulo) ConsultasResumen() int64 { return m.resumenes.consultas.Load() }

// VaciarResumenes olvida los resúmenes guardados (pruebas que comparan contadores al instante).
func (m *Modulo) VaciarResumenes() { m.resumenes.vaciar() }

// FijarRelojResumen cambia el reloj del tope de frecuencia de GET /chat/resumen.
func (m *Modulo) FijarRelojResumen(f func() time.Time) { m.resumenes.ahora = f }

func (m *Modulo) PurgarRegistrosQR(ctx context.Context, bd nucleo.BD) error {
	return m.purgarRegistrosQR(ctx, bd)
}

// Asistente de IA (§16).

func ValidarURLIA(u string, loopback, conClave bool) error {
	return validarURLIA(u, loopback, conClave)
}

func IPPermitidaIA(ip string, loopback bool) bool {
	return ipPermitidaIA(netip.MustParseAddr(ip), loopback)
}

// CtxAsistenteIA: el contexto del actor asistente_ia de una empresa (pruebas de permisos por actor).
func CtxAsistenteIA(ctx context.Context, empresa int64) context.Context {
	return ctxAsistente(ctx, empresa)
}

// Reglas del texto del asistente autónomo (pruebas de tabla).

// TextoAutonomoValido: el texto pasaría las reglas de envío con esos enlaces y datos vistos, y con
// esas familias de resultado devueltas por herramientas en el turno.
func TextoAutonomoValido(texto string, enlaces, vistos, resultados []string) bool {
	t := &turnoIA{enlaces: enlaces, vistos: vistos, hechas: map[string]bool{}}
	t.permitir(resultados...)
	_, ok := validarMensajeAutonomo(t, texto)
	return ok
}

func AfirmaSerPersona(texto string) bool { return afirmaSerPersona(texto) }
func PidePersona(texto string) bool      { return pidePersona(texto) }
func PreguntaSiEsPersona(texto string) bool {
	return rePreguntaPersona.MatchString(texto)
}
func TextoReglaPromesa(codigo string) string { return textoReglaPromesa(codigo) }
func CamposSeguros(campos []string) []string { return camposSeguros(campos) }
func PerfilPorDefecto() PerfilAsistente      { return perfilPorDefecto() }
func AccionesDeDinero() []string             { return accionesDeDinero }

// InicioEntrantes: desde cuándo cuentan los mensajes del cliente de un periodo reabierto.
func InicioEntrantes(resuelta, reabierta *time.Time) time.Time {
	return inicioEntrantes(mensajeriabd.MensajeriaConversacion{ResueltaEn: resuelta, ReabiertaEn: reabierta})
}

func (m *Modulo) ArriendoTurno() time.Duration { return m.arriendoTurno() }

// DocumentoCandidato: el dato que verificar_identidad mandaría a Argos ("" = no hay uno claro).
func DocumentoCandidato(texto, telefono string) string {
	v, _ := documentoCandidato(texto, telefono)
	return v
}

func DocumentoNombradoInvalido(texto, telefono string) bool {
	_, inv := documentoCandidato(texto, telefono)
	return inv
}

func PayloadTapado(payload []byte, wamid string) []byte {
	b, _, _ := payloadTapado(payload, wamid)
	return b
}

// CrearOrdenDeVisita crea la orden de una visita como lo hace agendar_visita (el contexto de las
// herramientas de visita y los topes dentro de la transacción), para probar la concurrencia.
func (m *Modulo) CrearOrdenDeVisita(ctx context.Context, empresa, conv, servicio int64, clave string, maxAbiertas, porDiaEmpresa int32) error {
	_, err := m.Conv.CrearOrden.Ejecutar(ctxVisitas(ctx, empresa), EntradaOrdenConv{ID: conv, Tipo: "visita", ServicioID: &servicio, Clave: clave,
		visita: &topesVisita{maxAbiertas: maxAbiertas, porDiaEmpresa: porDiaEmpresa}})
	return err
}

// ReservarVerificacion reserva un intento del desafío de identidad del cliente (0 = agotados).
func (m *Modulo) ReservarVerificacion(ctx context.Context, empresa, cliente, conv int64, numeroVerificado bool) (int64, error) {
	id, _, err := m.reservarVerificacion(ctx, &turnoIA{empresa: empresa, clienteID: cliente, conv: conv, numeroVerificado: numeroVerificado},
		m.ahora().Add(-24*time.Hour))
	return id, err
}

// ProveedorPara arma el proveedor del asistente de una configuración (el mismo que usa el módulo).
func (m *Modulo) ProveedorPara(cfg ConfigProveedor, plazo time.Duration) (Proveedor, error) {
	return m.proveedorDe(cfg, plazo)
}

// RotacionAMedias deja una rotación de credenciales como queda si la respuesta del servidor se
// pierde: lo «siguiente» guardado en Iris y ya aplicado en el servidor, sin promover.
func (m *Modulo) RotacionAMedias(ctx context.Context, bd nucleo.BD, id int64, revocar bool) error {
	ctx = ctxSistema(ctx, bd)
	c, err := m.ServidorIA.leer.Ejecutar(ctx, id)
	if err != nil {
		return err
	}
	convivencia := convivenciaRotacion
	if revocar {
		convivencia = 0
	}
	return m.entregarCredencial(ctx, m.ServidorIA, c, canalVivo{token: c.token, cert: c.cert}, convivencia, revocar)
}

// EstimarEntradaIA y UsoContado: la cuenta propia de tokens del servidor de IA.
func EstimarEntradaIA(in PedidoIA) int64 { return estimarEntradaIA(in) }

func UsoContado(in PedidoIA, declarado UsoIA, salida string) UsoIA {
	return usoContado(in, declarado, salida)
}

// PlazoRotacionIA: lo que dura apartada una rotación.
const PlazoRotacionIA = plazoRotacionIA

// MetricasValidas: la revalidación de lo que devuelve /metricas.
func MetricasValidas(mt voz.Metricas) bool {
	_, _, ok := muestraDe(mt)
	return ok
}

// ModelosDeMuestra: cuántos modelos se guardan de una lectura y si la lista quedó cortada.
func ModelosDeMuestra(mt voz.Metricas) (int, bool, bool) {
	g, _, ok := muestraDe(mt)
	return len(g.Servidor.Modelos), g.ModelosTruncados, ok
}

// FijarSondeoSiguienteIA hace fallar (o no) el sondeo de lo «siguiente» en el latido.
func (m *Modulo) FijarSondeoSiguienteIA(f func() error) { m.sondeoSiguienteIA = f }

// SiguienteSinEntregar deja guardado lo «siguiente» de una rotación cuya entrega nunca llegó al
// servidor (Iris lo guardó y la petición se perdió antes de aplicarse).
func (m *Modulo) SiguienteSinEntregar(ctx context.Context, bd nucleo.BD, id int64, conCertificado bool) error {
	ctx = ctxSistema(ctx, bd)
	c, err := m.ServidorIA.leer.Ejecutar(ctx, id)
	if err != nil {
		return err
	}
	nuevo, err := voz.NuevoToken()
	if err != nil {
		return err
	}
	fijar := entradaFijarSiguiente{ID: id, Version: c.fila.Version, Token: nuevo}
	if conCertificado {
		// Como «Revocar ahora»: también un certificado de cliente nuevo, que el servidor no conoce.
		cert, clave, err := voz.GenerarCertificado("iris", vidaCertCliente)
		if err != nil {
			return err
		}
		cifrada, err := m.cifrarMotor(id, "cert_cliente_clave", clave)
		if err != nil {
			return err
		}
		pem, vence := string(cert), m.ahora().Add(vidaCertCliente)
		fijar.Cert, fijar.CertClave, fijar.CertVence = &pem, cifrada, &vence
	}
	_, err = m.ServidorIA.fijarSiguiente.Ejecutar(ctx, fijar)
	return err
}

// TokenDeMotor: el token vigente que Iris tiene guardado, descifrado (para compararlo con el del servidor).
func (m *Modulo) TokenDeMotor(ctx context.Context, bd nucleo.BD, id int64) ([]byte, error) {
	c, err := m.ServidorIA.leer.Ejecutar(ctxSistema(ctx, bd), id)
	return c.token, err
}

// CredencialConLoVigente manda a /credencial una petición de solo confirmar firmada con el token que
// Iris tiene como vigente (durante la convivencia de una rotación a medias, el anterior del servidor).
func (m *Modulo) CredencialConLoVigente(ctx context.Context, bd nucleo.BD, id int64) error {
	c, err := m.ServidorIA.leer.Ejecutar(ctxSistema(ctx, bd), id)
	if err != nil {
		return err
	}
	cli, err := m.clienteCon(c, c.cert, c.token)
	if err != nil {
		return err
	}
	defer cli.Cerrar()
	err = cli.Credencial(ctx, voz.PeticionCredencial{ConfirmaHuellaServidor: *c.fila.HuellaTls})
	// 409 sin_huella_siguiente: la puerta autenticó la petición y la atendió (no hay nada que
	// confirmar). Lo que se comprueba es que no la rechaza con 401.
	var ep *voz.ErrorPuerta
	if errors.As(err, &ep) && ep.Estado == 409 {
		return nil
	}
	return err
}

// DecidirCuenta: la decisión sobre la cuenta de una boleta con lo que respondió Argos al releerla
// (la cuenta o el error).
func DecidirCuenta(empresa int64, k argos.CuentaBancariaArgos, err error, cuenta int64) (string, error) {
	return decidirCuenta(empresa, cuentaLeida{cuenta: k, err: err}, cuenta)
}

// TaparTextoCuentas: el historial del modelo sin el texto de las cuentas (§7.8).
var TaparTextoCuentas = taparTextoCuentas

// EnviarBoleta: el despacho de una boleta (lo que hace cada corrida con cada id vencido).
func (m *Modulo) EnviarBoleta(ctx context.Context, id uuid.UUID) error {
	return m.enviarBoleta(ctx, id)
}

// DescribirServiciosIA: cómo ve el asistente los servicios del cliente (contrato argosisp.md 0.8.0 §2.4).
var DescribirServiciosIA = describirServiciosIA

// TextoAutonomoTrasReconexion: el texto final del asistente en un turno en que se aceptó una
// reconexión temporal con ese `hasta` (marca {{reconexion}}, §7.3). ok = false si no se enviaría.
func TextoAutonomoTrasReconexion(texto string, hasta time.Time, zona *time.Location) (string, bool) {
	t := &turnoIA{hechas: map[string]bool{}, fraseReconexion: FraseReconexion(hasta, zona)}
	t.permitir(ResReconectar, ResActivo, ResAceptar, ResConfirmar)
	return validarMensajeAutonomo(t, texto)
}

// AplicarPromesaArgos corre lo que hace el suscriptor de argos.promesa_cambiada (sin el bus).
func (m *Modulo) AplicarPromesaArgos(ctx context.Context, bd nucleo.BD, payload []byte) (bool, error) {
	var in entradaPromesaArgos
	if err := json.Unmarshal(payload, &in); err != nil {
		return false, err
	}
	r, err := m.Asistente.reconexionArgos.Ejecutar(ctxSistema(ctx, bd), in)
	return r.Aplicada, err
}

// RenderPlantilla: el texto que lee el cliente con esas variables.
func RenderPlantilla(codigo string, v map[string]string) string {
	p, _ := PlantillaPorCodigo(codigo)
	return p.Render(v)
}

// ValidarVariablesPlantilla: la validación de §6.2 de una plantilla del catálogo.
func ValidarVariablesPlantilla(codigo string, v map[string]string, ahora time.Time) error {
	p, _ := PlantillaPorCodigo(codigo)
	return p.validarVariables(v, ahora)
}

// FijarPlazoMedidas cambia el plazo para leer la cabecera de una foto al enviarla (pruebas).
func FijarPlazoMedidas(d time.Duration) func() {
	antes := plazoMedidas
	plazoMedidas = d
	return func() { plazoMedidas = antes }
}

// FijarTrasPrepararCuentas: corre entre armar el texto de «Enviar cuentas» y la transacción (la
// empresa con que se armó, para simular que la conversación cambió de empresa en medio).
func (m *Modulo) FijarTrasPrepararCuentas(f func(empresa *int64)) { m.trasPrepararCuentas = f }

// TextoAutonomoConCuentas: las reglas de envío con el texto de las cuentas del turno (marca {{cuentas}}).
func TextoAutonomoConCuentas(texto string, textoCuentas *string, verificado bool) (string, bool) {
	t := &turnoIA{hechas: map[string]bool{}, textoCuentas: textoCuentas, verificado: verificado}
	return validarMensajeAutonomo(t, texto)
}

// NombreCuentaModelo: cómo ve el modelo una cuenta.
var NombreCuentaModelo = nombreCuentaModelo

// TextoAutonomoConSaldo: las reglas de envío con el saldo leído en el turno (marca {{saldo}}).
func TextoAutonomoConSaldo(texto, saldo string) (string, bool) {
	t := &turnoIA{hechas: map[string]bool{}, saldo: &saldo, verificado: true}
	return validarMensajeAutonomo(t, texto)
}

// SugerenciaValida: la salida del modelo pasaría como sugerencia (JSON { texto, fuentes }); si no, el motivo.
func SugerenciaValida(json string) (bool, string) {
	_, err := validarSalidaModelo(RespuestaIA{Texto: json}, contextoIA{catalogo: map[string]FuenteSugerencia{}}, nil, nil)
	if err != nil {
		return false, err.Error()
	}
	return true, ""
}
