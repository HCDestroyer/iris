package mensajeria

import (
	"context"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
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

// TextoCuentas arma el texto fijo con las cuentas dadas (ya filtradas: activas, de la empresa, en
// quetzales y con titular), en el orden de Argos.
func TextoCuentas(cs []argos.CuentaBancariaArgos) string {
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
	SinTitular     int        `json:"sinTitular" doc:"Cuentas activas en quetzales de la empresa que no van porque Argos no tiene su titular (§10.2, punto 14): el admin lo escribe en Argos"`
	SincronizadoEn *time.Time `json:"sincronizadoEn" nullable:"true" doc:"Cuándo se leyó de Argos la lista de cuentas de la empresa"`
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
	out := TextoCuentasConv{}
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
	out.Cuentas = len(van)
	if len(van) > 0 {
		t := TextoCuentas(van)
		out.Texto = &t
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
	in.Texto = t.Texto
	return nil
}
