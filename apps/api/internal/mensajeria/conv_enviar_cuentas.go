package mensajeria

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// «Enviar cuentas» (contrato con Argos 0.8.0 §7.8, §10.3 fila 9): lo que se le dice al cliente sobre
// dónde depositar sale del catálogo de Argos con un texto fijo de Iris (banco, tipo, número, titular y
// moneda). La persona no lo escribe: el panel muestra el texto y lo manda con «cuentas: true» en
// POST /conversaciones/{id}/mensajes, y el servidor lo arma otra vez al enviar (nadie cambia un
// dígito). El asistente usa la marca {{cuentas}} (asistente_auto.go). Solo cuentas activas en
// quetzales de la empresa de la conversación y con titular (§10.2, punto 14).

// EncabezadoCuentas abre el texto fijo de las cuentas (y lo reconoce para taparlo ante el modelo).
const EncabezadoCuentas = "Cuentas para depositar o transferir"

// CierreCuentas cierra el texto fijo.
const CierreCuentas = "Después de pagar, envíenos por aquí la foto de la boleta. Deposite solo a estas cuentas."

// MaxCuentasTexto: cuántas cuentas lleva como mucho el texto fijo (las primeras, en el orden de Argos).
// Con los largos de cada dato acotados, el texto siempre cabe en un mensaje de WhatsApp.
const MaxCuentasTexto = 8

// MaxLargoTextoCuentas: el largo máximo del texto de las cuentas, con lugar para la presentación del
// asistente en el mismo mensaje.
const MaxLargoTextoCuentas = MaxTextoWhatsApp - 300

// TextoCuentas arma el texto fijo con las cuentas dadas (ya filtradas: activas, de la empresa, en
// quetzales y con titular), en el orden de Argos, hasta MaxCuentasTexto.
func TextoCuentas(cs []argos.CuentaBancariaArgos) string {
	if len(cs) > MaxCuentasTexto {
		cs = cs[:MaxCuentasTexto]
	}
	var b strings.Builder
	b.WriteString(EncabezadoCuentas + ":\n")
	for _, k := range cs {
		b.WriteString("\n• " + datoCuenta(k.Banco, 80))
		if k.Tipo != nil {
			b.WriteString(" · cuenta " + *k.Tipo)
		}
		b.WriteString("\n  No. " + datoCuenta(k.Numero, 60))
		if k.Titular != nil {
			b.WriteString("\n  A nombre de: " + datoCuenta(*k.Titular, 120))
		}
		b.WriteString("\n  Moneda: quetzales (GTQ)\n")
	}
	b.WriteString("\n" + CierreCuentas)
	return b.String()
}

// datoCuenta: un dato de Argos en una sola línea y con su largo máximo.
func datoCuenta(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}

// TextoCuentasTapado: lo que ve el modelo en el historial en lugar del texto de las cuentas.
const TextoCuentasTapado = "(el sistema le envió las cuentas bancarias de la empresa)"

// taparTextoCuentas: el texto fijo de las cuentas no vuelve al modelo (ni números ni titulares).
func taparTextoCuentas(s string) string {
	if strings.Contains(s, EncabezadoCuentas+":") {
		return TextoCuentasTapado
	}
	return s
}

// TextoCuentasConv es GET /conversaciones/{id}/texto-cuentas: lo que «Enviar cuentas» mandaría.
type TextoCuentasConv struct {
	Texto          *string    `json:"texto" nullable:"true" doc:"El texto fijo con las cuentas que se le pueden dar al cliente; null si no hay ninguna"`
	Cuentas        int        `json:"cuentas" doc:"Cuántas cuentas lleva el texto"`
	SinLugar       int        `json:"sinLugar" doc:"Cuentas que se podían dar pero no van porque el texto lleva como mucho 8"`
	Hash           *string    `json:"hash" nullable:"true" doc:"Hash del texto: se manda como cuentasHash al enviar (lo que se ve es lo que se envía)"`
	SinTitular     int        `json:"sinTitular" doc:"Cuentas activas en quetzales de la empresa que no van porque Argos no tiene su titular (§10.2, punto 14): el admin lo escribe en Argos"`
	SincronizadoEn *time.Time `json:"sincronizadoEn" nullable:"true" doc:"Cuándo se leyó de Argos la lista de cuentas de la empresa"`
	// empresa: la empresa con cuyas cuentas se armó el texto.
	empresa int64
}

// hashTextoCuentas: el hash que el panel devuelve al enviar (hex de SHA-256).
func hashTextoCuentas(texto string) string {
	h := sha256.Sum256([]byte(texto))
	return hex.EncodeToString(h[:])
}

// cuentasCambiaron: el texto que se mostró ya no es el que se mandaría (cambiaron las cuentas en
// Argos o la conversación pasó a otra empresa).
func cuentasCambiaron() error {
	return nucleo.NuevoProblema(http.StatusConflict, "cuentas_cambiaron", "Las cuentas cambiaron", "Las cuentas cambiaron, revisa de nuevo antes de enviarlas")
}

// EntradaTextoCuentas es GET /conversaciones/{id}/texto-cuentas.
type EntradaTextoCuentas struct {
	ID int64 `path:"id"`
}

// TextoCuentasDe arma el texto de «Enviar cuentas» para la conversación (permiso de responder).
func (m *Modulo) TextoCuentasDe(ctx context.Context, conv int64) (TextoCuentasConv, error) {
	a, err := m.permisoCuentasCliente.Ejecutar(ctx, conv)
	if err != nil {
		return TextoCuentasConv{}, err
	}
	if m.cfg.Argos == nil {
		return TextoCuentasConv{}, noDisponible("Leer las cuentas de Argos")
	}
	out := TextoCuentasConv{empresa: a.empresa}
	if a.empresa == 0 {
		return out, nil
	}
	ce, err := m.cuentasDe(ctx, atencionDe(a, conv))
	if err != nil {
		return TextoCuentasConv{}, err
	}
	out.SincronizadoEn = ce.LeidoEn
	var van []argos.CuentaBancariaArgos
	for _, k := range ce.Cuentas {
		switch {
		case cuentaParaOfrecer(k, a.empresa):
			van = append(van, k)
		case cuentaParaBoleta(k, a.empresa):
			out.SinTitular++
		}
	}
	if len(van) > MaxCuentasTexto {
		out.SinLugar = len(van) - MaxCuentasTexto
		van = van[:MaxCuentasTexto]
	}
	out.Cuentas = len(van)
	if len(van) > 0 {
		t := TextoCuentas(van)
		if len([]rune(t)) > MaxLargoTextoCuentas {
			// No pasa con los largos acotados de cada dato; si pasara, no se manda un texto cortado.
			return TextoCuentasConv{}, reglaConv("cuentas_demasiado_largas", "Las cuentas no caben en un mensaje: dalas por partes o revisa sus datos en Argos")
		}
		h := hashTextoCuentas(t)
		out.Texto, out.Hash = &t, &h
	}
	return out, nil
}

// sinCuentasParaEnviar: la empresa no tiene cuentas que se le puedan dar al cliente.
func sinCuentasParaEnviar() error {
	return reglaConv("sin_cuentas_para_enviar", "La empresa no tiene cuentas activas en quetzales con titular en Argos: el admin escribe el titular de la cuenta en Argos")
}

// prepararEnvioCuentas: con «cuentas: true», el texto del mensaje lo arma el servidor (y debe venir vacío).
func (m *Modulo) prepararEnvioCuentas(ctx context.Context, in *EntradaResponder) error {
	if !in.Cuentas {
		return nil
	}
	if (in.Texto != nil && strings.TrimSpace(*in.Texto) != "") || in.Plantilla != nil || in.AdjuntoID != nil || in.RespuestaRapidaID != nil || in.SugerenciaID != nil {
		return campoConv("cuentas", "El texto de las cuentas va solo: sin texto, plantilla ni adjunto", "exclusivo")
	}
	t, err := m.TextoCuentasDe(ctx, in.ConversacionID)
	if err != nil {
		return err
	}
	if t.Texto == nil {
		return sinCuentasParaEnviar()
	}
	// Lo que se ve es lo que se envía: el panel manda el hash del texto que mostró.
	if t.Hash == nil || in.CuentasHash != *t.Hash {
		return cuentasCambiaron()
	}
	in.Texto, in.cuentasEmpresa = t.Texto, t.empresa
	if m.trasPrepararCuentas != nil {
		m.trasPrepararCuentas(&in.cuentasEmpresa)
	}
	return nil
}
