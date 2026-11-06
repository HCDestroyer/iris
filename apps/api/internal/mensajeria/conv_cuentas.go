package mensajeria

import (
	"context"
	"encoding/json"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Cuentas bancarias de la empresa y bancos de origen (lecturas de Argos, contrato §7.2) para el
// formulario de la boleta. No se guardan en Iris.

// CuentaBancaria es una cuenta de la empresa que recibe depósitos.
type CuentaBancaria struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
	Banco  string `json:"banco"`
	Numero string `json:"numero"`
	Moneda string `json:"moneda"`
}

// BancoOrigen es un banco desde el que se deposita o transfiere.
type BancoOrigen struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
}

// ListaCuentasBancarias es { data, nextCursor }.
type ListaCuentasBancarias struct {
	Data       []CuentaBancaria `json:"data"`
	NextCursor *string          `json:"nextCursor" nullable:"true"`
}

// ListaBancos es { data, nextCursor }.
type ListaBancos struct {
	Data       []BancoOrigen `json:"data"`
	NextCursor *string       `json:"nextCursor" nullable:"true"`
}

func nuevoPermisoBoleta() *auditoria.Caso[struct{}, alcanceConv] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, alcanceConv]{
		Accion: "conversaciones.cuentas_bancarias", Permiso: nucleo.PermConversacionesRegistrarBoleta, AuditarExito: sinAuditar[alcanceConv],
		Logica: func(ctx context.Context, _ *auditoria.Tx, _ struct{}) (alcanceConv, error) { return alcanceConvDe(ctx) },
	})
}

func nombreBanco(crudo json.RawMessage) string {
	var s string
	if json.Unmarshal(crudo, &s) == nil {
		return s
	}
	var o struct {
		Nombre string `json:"nombre"`
	}
	_ = json.Unmarshal(crudo, &o)
	return o.Nombre
}

// CuentasBancarias lista las cuentas activas en GTQ de la empresa activa (Argos).
func (m *Modulo) CuentasBancarias(ctx context.Context) (ListaCuentasBancarias, error) {
	a, err := m.permisoBoleta.Ejecutar(ctx, struct{}{})
	if err != nil {
		return ListaCuentasBancarias{}, err
	}
	if m.cfg.Argos == nil {
		return ListaCuentasBancarias{}, noDisponible("Leer las cuentas de Argos")
	}
	cs, err := m.cfg.Argos.CuentasBancarias(ctx, argos.AtencionIris{Actor: "usuario:" + itoa(a.usuario), Conversacion: "iris:bandeja", EmpresaID: a.empresa})
	if err != nil {
		return ListaCuentasBancarias{}, argos.Problema(err)
	}
	out := ListaCuentasBancarias{Data: []CuentaBancaria{}}
	for _, c := range cs {
		if c.Moneda != "" && c.Moneda != "GTQ" {
			continue
		}
		if a.empresa != 0 && c.EmpresaID != nil && *c.EmpresaID != a.empresa {
			continue
		}
		out.Data = append(out.Data, CuentaBancaria{ID: c.ID, Nombre: c.Nombre, Banco: nombreBanco(c.Banco), Numero: c.Numero, Moneda: c.Moneda})
	}
	return out, nil
}

// Bancos lista los bancos de origen (Argos).
func (m *Modulo) Bancos(ctx context.Context) (ListaBancos, error) {
	a, err := m.permisoBoleta.Ejecutar(ctx, struct{}{})
	if err != nil {
		return ListaBancos{}, err
	}
	if m.cfg.Argos == nil {
		return ListaBancos{}, noDisponible("Leer los bancos de Argos")
	}
	bs, err := m.cfg.Argos.Bancos(ctx, argos.AtencionIris{Actor: "usuario:" + itoa(a.usuario), Conversacion: "iris:bandeja", EmpresaID: a.empresa})
	if err != nil {
		return ListaBancos{}, argos.Problema(err)
	}
	out := ListaBancos{Data: []BancoOrigen{}}
	for _, b := range bs {
		out.Data = append(out.Data, BancoOrigen{ID: b.ID, Nombre: b.Nombre})
	}
	return out, nil
}
