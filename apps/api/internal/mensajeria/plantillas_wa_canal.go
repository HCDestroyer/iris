package mensajeria

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// El formulario de «Números de WhatsApp» todavía manda plantillas (código → nombre e idioma). En un
// número con cuenta (WABA), lo que cambia ahí se refleja en las plantillas de la cuenta: solo los
// códigos que cambiaron (así guardar el número no deshace lo que se eligió en la pantalla de
// plantillas). Una plantilla que Iris no conocía entra «aprobada», como hasta hoy; la siguiente
// sincronización dice su estado real.

func (m *Modulo) reflejarPlantillasCanal(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaCanal, antesCrudo []byte) error {
	if c.WabaID == "" || c.Tipo != TipoWhatsAppCloud {
		return nil
	}
	antes, despues := map[string]PlantillaMeta{}, map[string]PlantillaMeta{}
	_ = json.Unmarshal(antesCrudo, &antes)
	_ = json.Unmarshal(c.Plantillas, &despues)
	ahora := m.ahora()
	if err := q.BloquearCuentaPlantillas(ctx, c.WabaID); err != nil {
		return err
	}
	for codigo, pm := range despues {
		if a, ok := antes[codigo]; ok && a.Nombre == pm.Nombre && a.Idioma == pm.Idioma {
			continue
		}
		f, err := q.PlantillaWAPorNombre(ctx, mensajeriabd.PlantillaWAPorNombreParams{WabaID: c.WabaID, Nombre: pm.Nombre, Idioma: pm.Idioma})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if !reNombrePW.MatchString(pm.Nombre) || !reIdiomaPW.MatchString(pm.Idioma) {
				continue
			}
			f, err = q.InsertarPlantillaWA(ctx, mensajeriabd.InsertarPlantillaWAParams{EmpresaID: c.EmpresaID, WabaID: c.WabaID, Nombre: pm.Nombre,
				Idioma: pm.Idioma, Etiqueta: recortar(codigo, 80), Categoria: CategoriaUtility, Estado: EstadoPWAprobada, Calidad: "UNKNOWN",
				Formato: "POSITIONAL", Componentes: []byte(`{"cuerpo":"","botones":[],"variables":[]}`), Parametros: []byte("[]"),
				Usos: []string{UsoPWAvisos, UsoPWConversaciones}, Origen: OrigenPWCanal, CreadaPor: actorPW(ctx)})
			if err != nil {
				return conflictoNombrePW(err)
			}
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
		if f.Codigo != nil && *f.Codigo == codigo {
			continue
		}
		if err := tomarCodigo(ctx, t, q, f, codigo, ahora); err != nil {
			return err
		}
		f.Codigo = &codigo
		if len(f.Parametros) == 0 || string(f.Parametros) == "null" {
			f.Parametros = []byte("[]")
		}
		g, err := guardarPW(ctx, q, f)
		if err != nil {
			return conflictoNombrePW(err)
		}
		if err := historialPW(ctx, q, g.ID, "usos", nil, &codigo, map[string]any{"canal_id": c.ID}, "usuario", actorPW(ctx), ahora); err != nil {
			return err
		}
		if err := t.Emitir("plantilla_whatsapp.actualizada", conPW(basePW(g), map[string]any{"campos": []string{"codigo"}})); err != nil {
			return err
		}
	}
	for codigo, pm := range antes {
		if _, sigue := despues[codigo]; sigue {
			continue
		}
		fs, err := q.PlantillasWACodigoDe(ctx, mensajeriabd.PlantillasWACodigoDeParams{WabaID: c.WabaID, Codigo: &codigo, Excepto: 0})
		if err != nil {
			return err
		}
		for _, f := range fs {
			if f.Nombre != pm.Nombre || f.Idioma != pm.Idioma || f.Estado == EstadoPWBorrada {
				continue
			}
			f.Codigo = nil
			g, err := guardarPW(ctx, q, f)
			if err != nil {
				return err
			}
			if err := historialPW(ctx, q, g.ID, "usos", &codigo, nil, map[string]any{"canal_id": c.ID}, "usuario", actorPW(ctx), ahora); err != nil {
				return err
			}
			if err := t.Emitir("plantilla_whatsapp.actualizada", conPW(basePW(g), map[string]any{"campos": []string{"codigo"}})); err != nil {
				return err
			}
		}
	}
	return nil
}
