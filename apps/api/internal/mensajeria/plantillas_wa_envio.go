package mensajeria

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// Envío de una plantilla de la cuenta con su mapa de datos (contratos/plantillas-whatsapp.md §6): cada
// variable de Meta recibe el dato de Iris que eligió la empresa. Los datos de Argos salen de la
// instantánea (nunca se le pregunta a Argos al enviar).

// componentesEnvio arma los components del mensaje. faltante: el dato que no se pudo llenar (Meta
// rechaza un parámetro vacío: mejor no mandar).
func componentesEnvio(pm PlantillaMeta, datos map[string]string) ([]map[string]any, string) {
	var encabezado, cuerpo []map[string]any
	botones := map[int][]map[string]any{}
	tipoBoton := map[int]string{}
	orden := []int{}
	for _, p := range pm.Parametros {
		v := datos[p.Dato]
		if p.Dato == "" || v == "" {
			if p.Dato == "" {
				return nil, p.Nombre
			}
			return nil, p.Dato
		}
		texto := map[string]any{"type": "text", "text": v}
		if pm.Nombrados && p.Componente != "boton" {
			texto["parameter_name"] = p.Nombre
		}
		switch p.Componente {
		case "encabezado":
			encabezado = append(encabezado, texto)
		case "cuerpo":
			cuerpo = append(cuerpo, texto)
		case "boton":
			if p.Boton == nil {
				continue
			}
			i := *p.Boton
			if _, ya := botones[i]; !ya {
				orden = append(orden, i)
			}
			if p.Dato == "codigo_verificacion" && p.Nombre == "codigo" {
				botones[i] = append(botones[i], map[string]any{"type": "coupon_code", "coupon_code": v})
				tipoBoton[i] = "copy_code"
			} else {
				botones[i] = append(botones[i], map[string]any{"type": "text", "text": v})
				tipoBoton[i] = "url"
			}
		}
	}
	out := []map[string]any{}
	if len(encabezado) > 0 {
		out = append(out, map[string]any{"type": "header", "parameters": encabezado})
	}
	if len(cuerpo) > 0 {
		out = append(out, map[string]any{"type": "body", "parameters": cuerpo})
	}
	for _, i := range orden {
		out = append(out, map[string]any{"type": "button", "sub_type": tipoBoton[i], "index": strconv.Itoa(i), "parameters": botones[i]})
	}
	return out, ""
}

// datosParaEnvio: las variables del aviso, los derivados y los de la instantánea de Argos del cliente.
func datosParaEnvio(ctx context.Context, q *mensajeriabd.Queries, p Plantilla, vars map[string]string, empresa int64, cliente, servicio *int64) (map[string]string, error) {
	d := datosDerivados(p, vars)
	if nombre, err := q.NombreEmpresaParaPlantilla(ctx, empresa); err != nil {
		return nil, err
	} else if nombre != "" {
		d["empresa_nombre"] = nombre
	}
	if cliente == nil {
		return d, nil
	}
	c, err := q.DatosClienteParaPlantilla(ctx, mensajeriabd.DatosClienteParaPlantillaParams{ServicioID: servicio, ClienteID: *cliente, EmpresaID: empresa})
	if errors.Is(err, pgx.ErrNoRows) {
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	for k, v := range map[string]string{"cliente_nombre": c.Nombre, "cliente_codigo": c.Codigo, "plan": c.Plan} {
		if v != "" {
			d[k] = v
		}
	}
	return d, nil
}
