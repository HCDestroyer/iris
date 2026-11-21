package mensajeria

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
// Si la cuenta cambia de dueña, las plantillas de la anterior quedan archivadas (borradas por el
// sistema): dejan de contar en los índices de la cuenta y la nueva dueña no ve sus nombres.
func (m *Modulo) reclamarWaba(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, waba string, empresa *int64) error {
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
	// La dueña anterior ya no tiene números en la cuenta: pasa a esta empresa.
	if err := q.ReasignarCuentaWA(ctx, mensajeriabd.ReasignarCuentaWAParams{WabaID: waba, Empresa: emp}); err != nil {
		return err
	}
	ahora := m.ahora()
	archivadas, err := q.ArchivarPlantillasDeOtraDuena(ctx, mensajeriabd.ArchivarPlantillasDeOtraDuenaParams{WabaID: waba, Empresa: emp, Ahora: ahora})
	if err != nil {
		return err
	}
	borrada := EstadoPWBorrada
	for _, f := range archivadas {
		if err := historialPW(ctx, q, f.ID, "borrada", nil, &borrada, map[string]any{"motivo": "cuenta_de_otra_empresa"}, "sistema", nil, ahora); err != nil {
			return err
		}
		if err := t.Emitir("plantilla_whatsapp.borrada", conPW(basePW(f), map[string]any{"nombre": f.Nombre, "idioma": f.Idioma,
			"origen": "sistema", "motivo": "cuenta_de_otra_empresa", "estaba_en_uso": false, "codigos": []string{}, "nombre_libre_en": nil})); err != nil {
			return err
		}
	}
	if len(archivadas) > 0 {
		t.Anotar(map[string]any{"plantillas_archivadas": len(archivadas), "duena_anterior": duena})
	}
	return nil
}

// cuentaEsDelNumero: el número tiene WABA y su empresa es la dueña de la cuenta. Si no (sin WABA, sin
// dueña registrada o de otra empresa), el número sigue con su mapa propio (canales.plantillas).
func cuentaEsDelNumero(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaCanal) (bool, error) {
	if c.WabaID == "" || c.Tipo != TipoWhatsAppCloud {
		return false, nil
	}
	k, err := q.CuentaWA(ctx, c.WabaID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return k.EmpresaID == derefEmpresa(c.EmpresaID), nil
}

// huellaWaba: lo que se comprobó en Meta (cuenta, número y token), sin guardar el token.
func huellaWaba(waba, pnid, token string) string {
	h := sha256.Sum256([]byte(waba + "\x00" + pnid + "\x00" + token))
	return hex.EncodeToString(h[:])
}

// wabaSinComprobar: lo que llega al caso no es lo que se comprobó en Meta antes de la transacción
// (alguien cambió el token o el número entretanto).
func wabaSinComprobar() *nucleo.Problema {
	return nucleo.Conflicto("waba_sin_comprobar", "El número cambió mientras Iris comprobaba la cuenta en Meta. Vuelve a guardar")
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
// de la cuenta, que dice su estado real. Un número que no es de la dueña de la cuenta no toca la cuenta
// (sigue con su mapa propio). reactivado: el número vuelve a estar activo; su mapa entera se refleja,
// pero solo en los códigos que ninguna otra plantilla de la cuenta atiende (no pisa lo elegido en la
// pantalla de plantillas) y sin exigir plantillas_whatsapp.editar (no cambia nada del mapa).
func (m *Modulo) reflejarPlantillasCanal(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaCanal, antesCrudo []byte, reactivado bool) error {
	deLaDuena, err := cuentaEsDelNumero(ctx, q, c)
	if err != nil || !deLaDuena {
		return err
	}
	antes, despues := map[string]PlantillaMeta{}, map[string]PlantillaMeta{}
	if !reactivado {
		_ = json.Unmarshal(antesCrudo, &antes)
	}
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
	if a := contexto.ActorDe(ctx); !reactivado && a.EsUsuario() && !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermPlantillasWhatsAppEditar)) {
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
		if reactivado {
			otras, err := q.PlantillasWACodigoDe(ctx, mensajeriabd.PlantillasWACodigoDeParams{WabaID: c.WabaID, Empresa: empresa, Codigo: codigo, Excepto: 0})
			if err != nil {
				return err
			}
			if slices.ContainsFunc(otras, func(o mensajeriabd.MensajeriaPlantillaWA) bool { return o.Estado != EstadoPWBorrada }) {
				continue
			}
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

func esProblemaConv(err error, codigo string) bool {
	var p *nucleo.Problema
	return errors.As(err, &p) && p.Codigo() == codigo
}

// requiereComprobarWaba: al actualizar un número de la API, la cuenta se comprueba en Meta si cambia
// la cuenta, el número o el token.
func requiereComprobarWaba(antes mensajeriabd.MensajeriaCanal, in EntradaActualizarCanal) bool {
	return in.WabaID != "" && antes.TipoConexion != ConexionQR && (in.WabaID != antes.WabaID || in.PhoneNumberID != antes.PhoneNumberID || in.Token != "")
}

// CasoCanal: crear o actualizar un número, con la comprobación de la cuenta en Meta antes de abrir la
// transacción (una llamada de red no retiene filas bloqueadas). El caso exige la huella de lo comprobado.
type CasoCanal[E any] struct {
	caso  *auditoria.Caso[E, Canal]
	antes func(ctx context.Context, in *E) error
}

func (c *CasoCanal[E]) Ejecutar(ctx context.Context, in E) (Canal, error) {
	if err := c.antes(ctx, &in); err != nil {
		return Canal{}, err
	}
	return c.caso.Ejecutar(ctx, in)
}

// puedeConfigurar: la comprobación previa solo se hace para quien podría guardar (el caso responde
// 403 o 404 a los demás sin que Iris llame a Meta).
func puedeConfigurar(ctx context.Context) bool {
	a := contexto.ActorDe(ctx)
	return !a.EsUsuario() || contexto.PermisosDe(ctx).Tiene(string(nucleo.PermMensajeriaConfigurar))
}

func nuevoCasoCrearCanal(m *Modulo) *CasoCanal[EntradaCrearCanal] {
	return &CasoCanal[EntradaCrearCanal]{caso: nuevoCrearCanal(m), antes: func(ctx context.Context, in *EntradaCrearCanal) error {
		if in.WabaID == "" || !puedeConfigurar(ctx) {
			return nil
		}
		if err := exigirEmpresaCanal(ctx, nucleo.DBDe(ctx), in.EmpresaID); err != nil {
			return nil //nolint:nilerr // el caso responde el error de la empresa
		}
		if err := m.verificarWabaNumero(ctx, in.WabaID, in.PhoneNumberID, in.Token); err != nil {
			return err
		}
		in.wabaComprobada = huellaWaba(in.WabaID, in.PhoneNumberID, in.Token)
		return nil
	}}
}

func nuevoCasoActualizarCanal(m *Modulo) *CasoCanal[EntradaActualizarCanal] {
	return &CasoCanal[EntradaActualizarCanal]{caso: nuevoActualizarCanal(m), antes: func(ctx context.Context, in *EntradaActualizarCanal) error {
		if in.WabaID == "" || !puedeConfigurar(ctx) {
			return nil
		}
		db := nucleo.DBDe(ctx)
		f, err := mensajeriabd.New(db).CanalPorID(ctx, in.ID)
		if err != nil || empresaDeCanalExistente(ctx, db, f.EmpresaID, "") != nil || !requiereComprobarWaba(f, *in) {
			return nil //nolint:nilerr // el caso responde 404 o no hace falta comprobar
		}
		token := in.Token
		if token == "" {
			cr, err := m.credencialesDe(f)
			if err != nil {
				return err
			}
			token = cr.Token
		}
		if err := m.verificarWabaNumero(ctx, in.WabaID, in.PhoneNumberID, token); err != nil {
			return err
		}
		in.wabaComprobada = huellaWaba(in.WabaID, in.PhoneNumberID, token)
		return nil
	}}
}
