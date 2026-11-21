package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Dueña de la cuenta (WABA) y el formulario de «Números de WhatsApp».
//
// Una WABA es de una sola empresa: la de sus números (mensajeria.plantillas_whatsapp_cuentas). Un
// préstamo de número no la cambia (el número sigue siendo de la dueña). Al crear o actualizar un
// número con WABA, otra empresa no puede usar una WABA que ya tiene números de una empresa distinta
// (409 waba_de_otra_empresa), y la WABA de un número manual se comprueba con su token en Meta
// (GET /{waba}/phone_numbers debe traer su phone_number_id).

func wabaDeOtraEmpresa() *nucleo.Problema {
	return nucleo.Conflicto("waba_de_otra_empresa", "Esa cuenta de WhatsApp (WABA) ya la usan números de otra empresa. Cada cuenta es de una sola empresa; para compartir un número, préstalo")
}

// reclamarWaba deja a la empresa como dueña de la cuenta (o falla si es de otra que aún tiene números).
func reclamarWaba(ctx context.Context, q *mensajeriabd.Queries, waba string, empresa *int64) error {
	if waba == "" {
		return nil
	}
	emp := derefEmpresa(empresa)
	duena, err := q.ReclamarCuentaWA(ctx, mensajeriabd.ReclamarCuentaWAParams{WabaID: waba, Empresa: emp})
	if err != nil {
		return err
	}
	if duena == emp {
		return nil
	}
	otra, err := q.OtraEmpresaUsaWaba(ctx, mensajeriabd.OtraEmpresaUsaWabaParams{WabaID: waba, Empresa: emp})
	if err != nil {
		return err
	}
	if otra {
		return wabaDeOtraEmpresa()
	}
	// La dueña anterior ya no tiene números en la cuenta: pasa a esta empresa (sus plantillas quedan
	// con la empresa anterior y esta no las ve).
	return q.ReasignarCuentaWA(ctx, mensajeriabd.ReasignarCuentaWAParams{WabaID: waba, Empresa: emp})
}

// verificarWabaNumero comprueba en Meta, con el token del número, que el phone_number_id es de la WABA.
func (m *Modulo) verificarWabaNumero(ctx context.Context, waba, pnid, token string) error {
	if waba == "" {
		return nil
	}
	if token == "" {
		return campoConv("token", "Para guardar la cuenta de WhatsApp (WABA ID), Iris la comprueba con el token del número", "required")
	}
	ok, err := m.graphPlantillas().VerificarNumero(ctx, CuentaGraph{WabaID: waba, Token: token}, pnid)
	if err != nil {
		return problemaMetaPW(err)
	}
	if !ok {
		return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "waba_no_coincide", "La cuenta no coincide",
			"Meta dice que ese phone number ID no es de la cuenta de WhatsApp (WABA ID) indicada. Revisa los dos datos en Meta.",
			nucleo.ErrorCampo{Field: "wabaId", Message: "No es la cuenta de este número", Rule: "waba_no_coincide"})
	}
	return nil
}

// El formulario todavía manda plantillas (código → nombre e idioma). En un número con cuenta, lo que
// cambia ahí se refleja en las plantillas de la cuenta: solo los códigos que cambiaron (así guardar el
// número no deshace lo que se eligió en la pantalla de plantillas), con plantillas_whatsapp.editar. Una
// plantilla que Iris no conocía entra «en revisión» (nunca aprobada sin Meta) y se pide la relectura
// de la cuenta, que dice su estado real.
func (m *Modulo) reflejarPlantillasCanal(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaCanal, antesCrudo []byte) error {
	if c.WabaID == "" || c.Tipo != TipoWhatsAppCloud {
		return nil
	}
	antes, despues := map[string]PlantillaMeta{}, map[string]PlantillaMeta{}
	_ = json.Unmarshal(antesCrudo, &antes)
	_ = json.Unmarshal(c.Plantillas, &despues)
	cambia := len(antes) != len(despues)
	for k, v := range despues {
		if a, ok := antes[k]; !ok || a.Nombre != v.Nombre || a.Idioma != v.Idioma {
			cambia = true
		}
	}
	if !cambia {
		return nil
	}
	if a := contexto.ActorDe(ctx); a.EsUsuario() && !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermPlantillasWhatsAppEditar)) {
		return nucleo.Prohibido("Cambiar las plantillas de un número con cuenta de WhatsApp exige el permiso " + string(nucleo.PermPlantillasWhatsAppEditar))
	}
	empresa := derefEmpresa(c.EmpresaID)
	ahora := m.ahora()
	if err := q.BloquearCuentaPlantillas(ctx, c.WabaID); err != nil {
		return err
	}
	nuevas := false
	for codigo, pm := range despues {
		if a, ok := antes[codigo]; ok && a.Nombre == pm.Nombre && a.Idioma == pm.Idioma {
			continue
		}
		f, err := q.PlantillaWAPorNombre(ctx, mensajeriabd.PlantillaWAPorNombreParams{WabaID: c.WabaID, Empresa: empresa, Nombre: pm.Nombre, Idioma: pm.Idioma})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if !reNombrePW.MatchString(pm.Nombre) || !reIdiomaPW.MatchString(pm.Idioma) {
				continue
			}
			f, err = q.InsertarPlantillaWA(ctx, mensajeriabd.InsertarPlantillaWAParams{EmpresaID: c.EmpresaID, WabaID: c.WabaID, Nombre: pm.Nombre,
				Idioma: pm.Idioma, Etiqueta: recortar(pm.Nombre, 80), Categoria: CategoriaUtility, Estado: EstadoPWPendiente, Calidad: "UNKNOWN",
				Formato: "POSITIONAL", Componentes: []byte(`{"cuerpo":"","botones":[],"variables":[]}`), Codigos: []string{}, Parametros: []byte("[]"),
				Usos: []string{UsoPWAvisos, UsoPWConversaciones}, Origen: OrigenPWCanal, CreadaPor: actorPW(ctx)})
			if err != nil {
				return conflictoNombrePW(err)
			}
			nuevas = true
			if err := historialPW(ctx, q, f.ID, "creada", nil, &f.Estado, map[string]any{"origen": OrigenPWCanal, "canal_id": c.ID}, "usuario", actorPW(ctx), ahora); err != nil {
				return err
			}
			if err := t.Emitir("plantilla_whatsapp.creada", conPW(basePW(f), map[string]any{"nombre": f.Nombre, "idioma": f.Idioma,
				"categoria": f.Categoria, "origen": OrigenPWCanal})); err != nil {
				return err
			}
		case err != nil:
			return err
		}
		if slices.Contains(f.Codigos, codigo) {
			continue
		}
		if err := tomarCodigos(ctx, t, q, empresa, c.WabaID, f.ID, []string{codigo}, ahora); err != nil {
			return err
		}
		f.Codigos = normalizarCodigos(append(slices.Clone(f.Codigos), codigo))
		g, err := guardarPW(ctx, q, f)
		if err != nil {
			return conflictoNombrePW(err)
		}
		if err := historialPW(ctx, q, g.ID, "usos", nil, &codigo, map[string]any{"canal_id": c.ID}, "usuario", actorPW(ctx), ahora); err != nil {
			return err
		}
		if err := t.Emitir("plantilla_whatsapp.actualizada", conPW(basePW(g), map[string]any{"campos": []string{"codigos"}})); err != nil {
			return err
		}
	}
	for codigo, pm := range antes {
		if _, sigue := despues[codigo]; sigue {
			continue
		}
		fs, err := q.PlantillasWACodigoDe(ctx, mensajeriabd.PlantillasWACodigoDeParams{WabaID: c.WabaID, Empresa: empresa, Codigo: codigo, Excepto: 0})
		if err != nil {
			return err
		}
		for _, f := range fs {
			if f.Nombre != pm.Nombre || f.Idioma != pm.Idioma || f.Estado == EstadoPWBorrada {
				continue
			}
			f.Codigos = slices.DeleteFunc(slices.Clone(f.Codigos), func(x string) bool { return x == codigo })
			g, err := guardarPW(ctx, q, f)
			if err != nil {
				return err
			}
			if err := historialPW(ctx, q, g.ID, "usos", &codigo, nil, map[string]any{"canal_id": c.ID}, "usuario", actorPW(ctx), ahora); err != nil {
				return err
			}
			if err := t.Emitir("plantilla_whatsapp.actualizada", conPW(basePW(g), map[string]any{"campos": []string{"codigos"}})); err != nil {
				return err
			}
		}
	}
	if nuevas {
		return m.encolar(ctx, t.Tx, ArgsSincronizarPlantillas{WabaID: c.WabaID, Origen: SyncCanal})
	}
	return nil
}
