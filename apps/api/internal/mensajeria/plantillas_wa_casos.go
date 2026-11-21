package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Casos de uso de las plantillas de WhatsApp que no hablan con Meta: leer, borradores, usos,
// ajustes, sugeridas y la revisión de reglas. Los que hablan con Meta están en plantillas_wa_acciones.go.

// casosPlantillasWA reúne los casos (Modulo.PlantillasWA).
type casosPlantillasWA struct {
	Datos          *auditoria.Caso[struct{}, ListaDatosPWA]
	Cuentas        *auditoria.Caso[struct{}, ListaCuentasPWA]
	Listar         *auditoria.Caso[EntradaListarPWA, ListaPlantillasWA]
	Ver            *auditoria.Caso[EntradaPWID, PlantillaWADetalle]
	Validar        *auditoria.Caso[EntradaValidarPWA, ResultadoReglasPWA]
	Crear          *auditoria.Caso[EntradaCrearPWA, PlantillaWA]
	Editar         *auditoria.Caso[EntradaEditarPWA, PlantillaWA]
	Usos           *auditoria.Caso[EntradaUsosPWA, PlantillaWA]
	VerAjustes     *auditoria.Caso[struct{}, AjustesPWA]
	GuardarAjustes *auditoria.Caso[EntradaAjustesPWA, AjustesPWA]
	Sugeridas      *auditoria.Caso[EntradaSugeridasPWA, ListaSugeridasPWA]
	CrearSugerida  *auditoria.Caso[EntradaCrearSugeridaPWA, PlantillaWA]

	Enviar      *AccionMetaPWA[EntradaVersionPWA]
	EditarMeta  *AccionMetaPWA[EntradaEditarMetaPWA]
	Borrar      *AccionMetaPWA[EntradaVersionPWA]
	Reanudar    *AccionMetaPWA[EntradaVersionPWA]
	Sincronizar *SincronizarPWA
}

func nuevosCasosPlantillasWA(m *Modulo) casosPlantillasWA {
	return casosPlantillasWA{
		Datos: nuevoDatosPWA(), Cuentas: nuevoCuentasPWA(m), Listar: nuevoListarPWA(m), Ver: nuevoVerPWA(m), Validar: nuevoValidarPWA(m),
		Crear: nuevoCrearPWA(m), Editar: nuevoEditarPWA(m), Usos: nuevoUsosPWA(m), VerAjustes: nuevoVerAjustesPWA(), GuardarAjustes: nuevoGuardarAjustesPWA(),
		Sugeridas: nuevoSugeridasPWA(m), CrearSugerida: nuevoCrearSugeridaPWA(m),
		Enviar: nuevoEnviarPWA(m), EditarMeta: nuevoEditarMetaPWA(m), Borrar: nuevoBorrarPWA(m), Reanudar: nuevoReanudarPWA(m),
		Sincronizar: nuevoSincronizarPWA(m),
	}
}

// Errores ---------------------------------------------------------------------------------------

func plantillaWANoEncontrada() *nucleo.Problema {
	return nucleo.NoEncontrado("La plantilla no existe")
}

func cuentaWANoEncontrada() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusNotFound, "cuenta_no_encontrada", "Cuenta no encontrada",
		"Ningún número activo de tu empresa es de esa cuenta de WhatsApp")
}

func reglaPW(codigo, detalle string, errores ...nucleo.ErrorCampo) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, codigo, "No se puede", detalle, errores...)
}

func marketingNoPermitido() *nucleo.Problema {
	return reglaPW("marketing_no_permitido", "Tu empresa no tiene activadas las plantillas de marketing. Actívalas en los ajustes de plantillas (Meta cobra más por ellas).")
}

// soloPersonasPW: las plantillas las administra una persona con sesión (un token de API o una
// integración reciben 404).
func soloPersonasPW(ctx context.Context) error {
	if a := contexto.ActorDe(ctx); a.Tipo != contexto.ActorUsuario || a.ID == 0 {
		return plantillaWANoEncontrada()
	}
	return nil
}

func actorPW(ctx context.Context) *int64 {
	if a := contexto.ActorDe(ctx); a.Tipo == contexto.ActorUsuario && a.ID != 0 {
		id := a.ID
		return &id
	}
	return nil
}

// Ayudantes --------------------------------------------------------------------------------------

// canalesDeCuentaEmpresa: los números activos de la cuenta que son de la empresa (404 si ninguno).
func canalesDeCuentaEmpresa(ctx context.Context, q *mensajeriabd.Queries, empresa int64, waba string) ([]mensajeriabd.MensajeriaCanal, error) {
	// La cuenta debe ser de la empresa (su dueña) y tener un número activo de ella.
	k, err := q.CuentaWA(ctx, waba)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && k.EmpresaID != empresa) {
		return nil, cuentaWANoEncontrada()
	}
	if err != nil {
		return nil, err
	}
	out, err := q.CanalesDeCuentaWA(ctx, mensajeriabd.CanalesDeCuentaWAParams{WabaID: waba, Empresa: empresa})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, cuentaWANoEncontrada()
	}
	return out, nil
}

// plantillaConCandado: la plantilla de la empresa bloqueada en el orden de todos los caminos (el
// candado de la cuenta, después la fila de la cuenta y después la plantilla): sin interbloqueos 40P01.
func (m *Modulo) plantillaConCandado(ctx context.Context, q *mensajeriabd.Queries, id int64) (mensajeriabd.MensajeriaPlantillaWA, error) {
	sinBloqueo, err := m.plantillaDeEmpresa(ctx, q, id, false)
	if err != nil {
		return sinBloqueo, err
	}
	if err := q.BloquearCuentaPlantillas(ctx, sinBloqueo.WabaID); err != nil {
		return sinBloqueo, err
	}
	return m.plantillaDeEmpresa(ctx, q, id, true)
}

func (m *Modulo) plantillaDeEmpresa(ctx context.Context, q *mensajeriabd.Queries, id int64, bloquear bool) (mensajeriabd.MensajeriaPlantillaWA, error) {
	p := mensajeriabd.PlantillaWAPorIDParams{ID: id, Empresa: contexto.EmpresaDe(ctx)}
	var f mensajeriabd.MensajeriaPlantillaWA
	var err error
	if bloquear {
		f, err = q.PlantillaWAParaActualizar(ctx, mensajeriabd.PlantillaWAParaActualizarParams(p))
	} else {
		f, err = q.PlantillaWAPorID(ctx, p)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return f, plantillaWANoEncontrada()
	}
	return f, err
}

func historialPW(ctx context.Context, q *mensajeriabd.Queries, id int64, tipo string, de, a *string, detalle map[string]any, origen string,
	actor *int64, en time.Time) error {
	if detalle == nil {
		detalle = map[string]any{}
	}
	crudo, _ := json.Marshal(detalle)
	return q.InsertarHistorialPlantillaWA(ctx, mensajeriabd.InsertarHistorialPlantillaWAParams{PlantillaID: id, Tipo: tipo, De: de, A: a,
		Detalle: crudo, Origen: origen, ActorID: actor, OcurridoEn: en})
}

func guardarPW(ctx context.Context, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaPlantillaWA) (mensajeriabd.MensajeriaPlantillaWA, error) {
	if f.Parametros == nil {
		f.Parametros = []byte("[]")
	}
	if f.Usos == nil {
		f.Usos = []string{}
	}
	if f.Ediciones == nil {
		f.Ediciones = []time.Time{}
	}
	return q.GuardarPlantillaWA(ctx, mensajeriabd.GuardarPlantillaWAParams{ID: f.ID, MetaID: f.MetaID, Nombre: f.Nombre, Idioma: f.Idioma,
		Etiqueta: f.Etiqueta, Categoria: f.Categoria, CategoriaAnterior: f.CategoriaAnterior, CategoriaCambiadaEn: f.CategoriaCambiadaEn,
		CategoriaProxima: f.CategoriaProxima, CategoriaProximaEn: f.CategoriaProximaEn, Estado: f.Estado, EstadoEn: f.EstadoEn, Motivo: f.Motivo,
		Rechazo: f.Rechazo, Calidad: f.Calidad, PausadaHasta: f.PausadaHasta, Formato: f.Formato, Componentes: f.Componentes,
		ComponentesMeta: f.ComponentesMeta, Codigos: lista(f.Codigos), Parametros: f.Parametros, Usos: f.Usos, Ediciones: f.Ediciones, EnviadaEn: f.EnviadaEn,
		BorradaEn: f.BorradaEn, BorradaPor: f.BorradaPor, NombreLibreEn: f.NombreLibreEn, SincronizadaEn: f.SincronizadaEn, MetaEventoEn: f.MetaEventoEn})
}

// auditablePW: lo que se compara en auditoria.cambios.
func auditablePW(f mensajeriabd.MensajeriaPlantillaWA) map[string]any {
	return map[string]any{"nombre": f.Nombre, "idioma": f.Idioma, "etiqueta": f.Etiqueta, "categoria": f.Categoria, "estado": f.Estado,
		"definicion": json.RawMessage(f.Componentes), "codigos": lista(f.Codigos), "usos": lista(f.Usos), "meta_id": f.MetaID, "calidad": f.Calidad}
}

func basePW(f mensajeriabd.MensajeriaPlantillaWA) map[string]any {
	return map[string]any{"plantilla_id": f.ID, "empresa_id": f.EmpresaID, "waba_id": f.WabaID}
}

func conPW(base map[string]any, extra map[string]any) map[string]any {
	for k, v := range extra {
		base[k] = v
	}
	return base
}

var acentosPW = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n", "Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u", "Ñ", "n")

// NombreMetaDe propone el nombre en Meta a partir del nombre para personas.
func NombreMetaDe(etiqueta string) string {
	s := strings.ToLower(acentosPW.Replace(strings.TrimSpace(etiqueta)))
	var b strings.Builder
	guion := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			guion = false
		} else if !guion && b.Len() > 0 {
			b.WriteByte('_')
			guion = true
		}
	}
	n := strings.Trim(b.String(), "_")
	if n == "" {
		n = "plantilla"
	}
	return recortar(n, 500)
}

// nombreLibre propone base, base_2, base_3… sin usar ni bloqueado en la cuenta.
func nombreLibre(ctx context.Context, q *mensajeriabd.Queries, empresa int64, waba, base string, ahora time.Time) (string, error) {
	usados, err := q.NombresWAParecidos(ctx, mensajeriabd.NombresWAParecidosParams{WabaID: waba, Empresa: empresa, Nombre: base, Ahora: &ahora})
	if err != nil {
		return "", err
	}
	if !slices.Contains(usados, base) {
		return base, nil
	}
	for i := 2; ; i++ {
		n := base + "_" + strconv.Itoa(i)
		if !slices.Contains(usados, n) {
			return n, nil
		}
	}
}

// revisarNombre: 409 nombre_en_uso (mismo nombre e idioma en la cuenta) o nombre_bloqueado (una
// aprobada con ese nombre se borró hace menos de 30 días), con un nombre libre sugerido.
func revisarNombre(ctx context.Context, q *mensajeriabd.Queries, empresa int64, waba, nombre, idioma string, excepto int64, ahora time.Time) error {
	enUso, err := q.NombreWAEnUso(ctx, mensajeriabd.NombreWAEnUsoParams{WabaID: waba, Empresa: empresa, Nombre: nombre, Idioma: idioma, Excepto: excepto})
	if err != nil {
		return err
	}
	libre := func() *string {
		base := nombre
		if i := strings.LastIndex(nombre, "_"); i > 0 {
			if _, err := strconv.Atoi(nombre[i+1:]); err == nil {
				base = nombre[:i]
			}
		}
		s, err := nombreLibre(ctx, q, empresa, waba, base, ahora)
		if err != nil || s == nombre {
			return nil
		}
		return &s
	}
	if enUso {
		p := nucleo.NuevoProblema(http.StatusConflict, "nombre_en_uso", "Nombre en uso",
			fmt.Sprintf("Ya hay una plantilla «%s» en este idioma. Meta no deja dos con el mismo nombre en el mismo idioma.", nombre),
			nucleo.ErrorCampo{Field: "nombre", Message: "Ese nombre ya está en uso en este idioma", Rule: "nombre_en_uso"})
		p.Sugerencia = libre()
		return p
	}
	hasta, err := q.NombreWABloqueado(ctx, mensajeriabd.NombreWABloqueadoParams{WabaID: waba, Empresa: empresa, Nombre: nombre, Ahora: &ahora})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	p := nucleo.NuevoProblema(http.StatusConflict, "nombre_bloqueado", "Nombre bloqueado por Meta",
		fmt.Sprintf("Una plantilla aprobada con ese nombre se borró hace poco, y Meta no deja reusarlo hasta el %s. Usa otro nombre.",
			hasta.In(zonaPlantillas).Format("02/01/2006")),
		nucleo.ErrorCampo{Field: "nombre", Message: "Meta no deja reusar este nombre todavía", Rule: "nombre_bloqueado"})
	p.Sugerencia, p.DisponibleEn = libre(), hasta
	return p
}

func marketingPermitido(ctx context.Context, q *mensajeriabd.Queries, empresa int64) (bool, error) {
	f, err := q.AjustesPlantillasWA(ctx, empresa)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return f.MarketingPermitido, err
}

func validarUsosPW(usos []string) error {
	for i, u := range usos {
		if !slices.Contains([]string{UsoPWAvisos, UsoPWConversaciones, UsoPWAsistente}, u) {
			return campoConv(fmt.Sprintf("usos[%d]", i), "Usos: avisos, conversaciones o asistente", "enum")
		}
	}
	return nil
}

// revisarCodigo: el código existe y la plantilla lo puede atender (cada variable con un dato que el
// envío de ese código trae).
func revisarCodigo(codigo string, categoria string, d DefinicionPWA, formato string, parametros []ParametroPWA) error {
	p, ok := PlantillaPorCodigo(codigo)
	if !ok {
		return campoConv("codigo", "No es un código del catálogo de Iris (GET /mensajeria/plantillas)", "codigo")
	}
	if categoria == CategoriaAutenticacion {
		return reglaPW("codigo_no_admitido", "Una plantilla de verificación no atiende avisos")
	}
	if d.Encabezado != nil && d.Encabezado.Tipo != "texto" {
		return reglaPW("encabezado_no_admitido", "Todavía no se mandan avisos con un archivo en el encabezado: usa un encabezado de texto o ninguno")
	}
	disponibles := datosDeCodigo(p)
	if len(parametros) == 0 && formato == "POSITIONAL" {
		// Migrada sin mapa: las variables del catálogo por posición (como se mandaba hasta hoy).
		if n := len(variablesDeTexto(d.Cuerpo)); d.Cuerpo != "" && n != len(p.Variables) {
			return reglaPW("datos_no_disponibles", fmt.Sprintf("La plantilla tiene %d datos y el aviso %s trae %d: elige qué dato va en cada uno", n, codigo, len(p.Variables)))
		}
		return nil
	}
	faltan := []string{}
	for _, x := range parametros {
		if x.Dato == "" || !slices.Contains(disponibles, x.Dato) {
			et := x.Nombre
			if dd, ok := DatoPorClave(x.Dato); ok {
				et = dd.Etiqueta
			}
			faltan = append(faltan, et)
		}
	}
	if len(faltan) > 0 {
		return reglaPW("datos_no_disponibles", fmt.Sprintf("El aviso %s no trae: %s. Cambia esos datos o elige otro aviso.", codigo, strings.Join(faltan, ", ")))
	}
	return nil
}

// tomarCodigos: los códigos pasan a esta plantilla (id 0 = una que se está creando); las demás de la
// cuenta que los tenían los sueltan. Con el candado de la cuenta: un código, una sola plantilla.
func tomarCodigos(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, empresa int64, waba string, id int64, codigos []string, ahora time.Time) error {
	if len(codigos) == 0 {
		return nil
	}
	if err := q.BloquearCuentaPlantillas(ctx, waba); err != nil {
		return err
	}
	for _, codigo := range codigos {
		otras, err := q.PlantillasWACodigoDe(ctx, mensajeriabd.PlantillasWACodigoDeParams{WabaID: waba, Empresa: empresa, Codigo: codigo, Excepto: id})
		if err != nil {
			return err
		}
		for _, o := range otras {
			o.Codigos = slices.DeleteFunc(slices.Clone(o.Codigos), func(c string) bool { return c == codigo })
			if _, err := guardarPW(ctx, q, o); err != nil {
				return err
			}
			if err := historialPW(ctx, q, o.ID, "usos", &codigo, nil, map[string]any{"codigo_paso_a": id}, "usuario", actorPW(ctx), ahora); err != nil {
				return err
			}
			if err := t.Emitir("plantilla_whatsapp.actualizada", conPW(basePW(o), map[string]any{"campos": []string{"codigos"}})); err != nil {
				return err
			}
		}
	}
	return nil
}

// normalizarCodigos: sin repetidos y en orden.
func normalizarCodigos(c []string) []string {
	out := slices.Clone(c)
	slices.Sort(out)
	return slices.Compact(lista(out))
}

// Datos -------------------------------------------------------------------------------------------

// ListaDatosPWA es { data }.
type ListaDatosPWA struct {
	Data []DatoPWA `json:"data"`
}

func nuevoDatosPWA() *auditoria.Caso[struct{}, ListaDatosPWA] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ListaDatosPWA]{
		Accion: "plantillas_whatsapp.ver", Permiso: nucleo.PermPlantillasWhatsAppVer, AuditarExito: sinAuditar[ListaDatosPWA],
		Logica: func(ctx context.Context, _ *auditoria.Tx, _ struct{}) (ListaDatosPWA, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return ListaDatosPWA{}, err
			}
			return ListaDatosPWA{Data: DiccionarioDatos}, nil
		},
	})
}

// Cuentas -----------------------------------------------------------------------------------------

// ErrorCuentaPWA es el último error al hablar con Meta.
type ErrorCuentaPWA struct {
	Codigo string    `json:"codigo" doc:"meta_sin_permiso, meta_token_invalido, meta_no_responde, meta_limite, meta_rechazo"`
	En     time.Time `json:"en"`
}

// ResumenSincronizacionPWA es lo que cambió en la última sincronización.
type ResumenSincronizacionPWA struct {
	Total     int               `json:"total" doc:"Plantillas que devolvió Meta"`
	Nuevas    []CambioCuentaPWA `json:"nuevas" doc:"Creadas fuera de Iris (en Meta)"`
	Cambiadas []CambioCuentaPWA `json:"cambiadas"`
	Borradas  []CambioCuentaPWA `json:"borradas" doc:"Ya no están en Meta"`
	Origen    string            `json:"origen" enum:"manual,nocturna,webhook,canal"`
}

// CambioCuentaPWA es una plantilla en el resumen.
type CambioCuentaPWA struct {
	ID       int64    `json:"id"`
	Nombre   string   `json:"nombre"`
	Etiqueta string   `json:"etiqueta"`
	Estado   string   `json:"estado"`
	Codigos  []string `json:"codigos"`
	Usos     []string `json:"usos"`
}

// CuentaPWA es una cuenta de WhatsApp (WABA) de la empresa.
type CuentaPWA struct {
	WabaID         string                    `json:"wabaId"`
	Numeros        []string                  `json:"numeros"`
	Nombres        []string                  `json:"nombres" doc:"Nombre de cada número en Iris"`
	SincronizadaEn *time.Time                `json:"sincronizadaEn" nullable:"true"`
	Resumen        *ResumenSincronizacionPWA `json:"resumen"`
	Error          *ErrorCuentaPWA           `json:"error" doc:"Último error al hablar con Meta (null si la última vez salió bien)"`
}

// ListaCuentasPWA es { data }.
type ListaCuentasPWA struct {
	Data []CuentaPWA `json:"data"`
}

func resumenDe(crudo []byte) *ResumenSincronizacionPWA {
	if len(crudo) == 0 {
		return nil
	}
	var r ResumenSincronizacionPWA
	if json.Unmarshal(crudo, &r) != nil {
		return nil
	}
	r.Nuevas, r.Cambiadas, r.Borradas = lista(r.Nuevas), lista(r.Cambiadas), lista(r.Borradas)
	for _, l := range [][]CambioCuentaPWA{r.Nuevas, r.Cambiadas, r.Borradas} {
		for i := range l {
			l[i].Usos = lista(l[i].Usos)
		}
	}
	return &r
}

func nuevoCuentasPWA(*Modulo) *auditoria.Caso[struct{}, ListaCuentasPWA] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ListaCuentasPWA]{
		Accion: "plantillas_whatsapp.ver", Permiso: nucleo.PermPlantillasWhatsAppVer, AuditarExito: sinAuditar[ListaCuentasPWA],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ListaCuentasPWA, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return ListaCuentasPWA{}, err
			}
			fs, err := mensajeriabd.New(t).CuentasWADeEmpresa(ctx, contexto.EmpresaDe(ctx))
			out := ListaCuentasPWA{Data: make([]CuentaPWA, 0, len(fs))}
			for _, f := range fs {
				c := CuentaPWA{WabaID: f.WabaID, Numeros: lista(f.Numeros), Nombres: lista(f.Nombres), SincronizadaEn: f.SincronizadaEn, Resumen: resumenDe(f.Resumen)}
				if f.Error != nil && f.ErrorEn != nil {
					c.Error = &ErrorCuentaPWA{Codigo: *f.Error, En: *f.ErrorEn}
				}
				out.Data = append(out.Data, c)
			}
			return out, err
		},
	})
}

// Listar ------------------------------------------------------------------------------------------

// EntradaListarPWA filtra la lista.
type EntradaListarPWA struct {
	WabaID    string `query:"wabaId" pattern:"^[0-9]{0,30}$" doc:"Solo esta cuenta"`
	Grupo     string `query:"grupo" enum:"activas,revision,atender" doc:"activas (aprobadas), revision (borrador o en revisión), atender (rechazadas, pausadas, borradas en uso, reclasificadas)"`
	Categoria string `query:"categoria" enum:"UTILITY,MARKETING,AUTHENTICATION"`
	Idioma    string `query:"idioma" maxLength:"10"`
	Buscar    string `query:"buscar" maxLength:"100" doc:"Por nombre o etiqueta"`
	Borradas  bool   `query:"borradas" doc:"Incluir todas las borradas (por defecto solo las que Meta borró y se usaban)"`
	Cursor    string `query:"cursor" maxLength:"30"`
	Limit     int32  `query:"limit" minimum:"1" maximum:"200" default:"50"`
}

// ConteosPWA son los contadores de la lista (sin los filtros de grupo, categoría e idioma).
type ConteosPWA struct {
	Todas    int64 `json:"todas"`
	Activas  int64 `json:"activas"`
	Revision int64 `json:"revision"`
	Atender  int64 `json:"atender"`
}

// ListaPlantillasWA es { data, nextCursor, conteos }.
type ListaPlantillasWA struct {
	Data       []PlantillaWA `json:"data"`
	NextCursor *string       `json:"nextCursor" nullable:"true"`
	Conteos    ConteosPWA    `json:"conteos"`
}

func nuevoListarPWA(m *Modulo) *auditoria.Caso[EntradaListarPWA, ListaPlantillasWA] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarPWA, ListaPlantillasWA]{
		Accion: "plantillas_whatsapp.ver", Permiso: nucleo.PermPlantillasWhatsAppVer, AuditarExito: sinAuditar[ListaPlantillasWA],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarPWA) (ListaPlantillasWA, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return ListaPlantillasWA{}, err
			}
			despues := int64(0)
			if in.Cursor != "" {
				n, err := strconv.ParseInt(in.Cursor, 10, 64)
				if err != nil || n < 0 {
					return ListaPlantillasWA{}, nucleo.SolicitudInvalida("cursor inválido")
				}
				despues = n
			}
			if in.Limit <= 0 {
				in.Limit = 50
			}
			q := mensajeriabd.New(t)
			empresa := contexto.EmpresaDe(ctx)
			fs, err := q.ListarPlantillasWA(ctx, mensajeriabd.ListarPlantillasWAParams{Empresa: empresa, WabaID: in.WabaID, Categoria: in.Categoria,
				Idioma: in.Idioma, Buscar: strings.TrimSpace(in.Buscar), Grupo: in.Grupo, IncluirBorradas: in.Borradas, Despues: despues, Limite: in.Limit + 1})
			if err != nil {
				return ListaPlantillasWA{}, err
			}
			c, err := q.ContarPlantillasWA(ctx, mensajeriabd.ContarPlantillasWAParams{Empresa: empresa, WabaID: in.WabaID})
			if err != nil {
				return ListaPlantillasWA{}, err
			}
			out := ListaPlantillasWA{Data: []PlantillaWA{}, Conteos: ConteosPWA(c)}
			ahora := m.ahora()
			for i, f := range fs {
				if i == int(in.Limit) {
					s := strconv.FormatInt(fs[i-1].ID, 10)
					out.NextCursor = &s
					break
				}
				out.Data = append(out.Data, aPlantillaWA(f, ahora))
			}
			return out, nil
		},
	})
}

// Ver ---------------------------------------------------------------------------------------------

// EntradaPWID es una plantilla por id.
type EntradaPWID struct {
	ID int64 `path:"id" minimum:"1"`
}

// HistorialPWA es una línea del historial.
type HistorialPWA struct {
	Tipo        string          `json:"tipo" enum:"creada,editada,enviada,estado,categoria,calidad,usos,borrada,reanudada"`
	De          *string         `json:"de" nullable:"true"`
	A           *string         `json:"a" nullable:"true"`
	Detalle     json.RawMessage `json:"detalle"`
	Origen      string          `json:"origen" enum:"usuario,webhook,sincronizacion,sistema"`
	ActorID     *int64          `json:"actorId" nullable:"true"`
	ActorNombre *string         `json:"actorNombre" nullable:"true"`
	OcurridoEn  time.Time       `json:"ocurridoEn"`
}

// PrestatariaPWA es una empresa a la que se presta un número de la cuenta (usa sus plantillas).
type PrestatariaPWA struct {
	EmpresaID int64    `json:"empresaId"`
	Nombre    string   `json:"nombre"`
	Usos      []string `json:"usos"`
}

// PlantillaWADetalle es la ficha.
type PlantillaWADetalle struct {
	PlantillaWA
	Historial    []HistorialPWA   `json:"historial"`
	Prestatarias []PrestatariaPWA `json:"prestatarias" doc:"Empresas a las que prestas un número de esta cuenta: mandan con estas mismas plantillas"`
}

func nuevoVerPWA(m *Modulo) *auditoria.Caso[EntradaPWID, PlantillaWADetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPWID, PlantillaWADetalle]{
		Accion: "plantillas_whatsapp.ver", Permiso: nucleo.PermPlantillasWhatsAppVer, AuditarExito: sinAuditar[PlantillaWADetalle],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPWID) (PlantillaWADetalle, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return PlantillaWADetalle{}, err
			}
			q := mensajeriabd.New(t)
			f, err := m.plantillaDeEmpresa(ctx, q, in.ID, false)
			if err != nil {
				return PlantillaWADetalle{}, err
			}
			return m.detallePW(ctx, q, f)
		},
	})
}

func (m *Modulo) detallePW(ctx context.Context, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaPlantillaWA) (PlantillaWADetalle, error) {
	d := PlantillaWADetalle{PlantillaWA: aPlantillaWA(f, m.ahora()), Historial: []HistorialPWA{}, Prestatarias: []PrestatariaPWA{}}
	hs, err := q.HistorialPlantillaWA(ctx, f.ID)
	if err != nil {
		return d, err
	}
	for _, h := range hs {
		d.Historial = append(d.Historial, HistorialPWA{Tipo: h.Tipo, De: h.De, A: h.A, Detalle: h.Detalle, Origen: h.Origen, ActorID: h.ActorID,
			ActorNombre: h.ActorNombre, OcurridoEn: h.OcurridoEn})
	}
	// Solo las de la dueña de la cuenta (una plantilla archivada de una dueña anterior no se presta).
	ps, err := q.PrestatariasDeCuentaWA(ctx, mensajeriabd.PrestatariasDeCuentaWAParams{WabaID: f.WabaID, Empresa: derefEmpresa(f.EmpresaID)})
	for _, p := range ps {
		d.Prestatarias = append(d.Prestatarias, PrestatariaPWA{EmpresaID: p.ID, Nombre: p.Nombre, Usos: lista(p.Usos)})
	}
	return d, err
}

// Validar -----------------------------------------------------------------------------------------

// EntradaValidarPWA es una plantilla a revisar (sin guardarla).
type EntradaValidarPWA struct {
	ID         int64         `json:"id,omitempty" minimum:"0" doc:"La plantilla que se edita (para no chocar con su propio nombre)"`
	WabaID     string        `json:"wabaId" pattern:"^[0-9]{1,30}$"`
	Nombre     string        `json:"nombre" maxLength:"600"`
	Idioma     string        `json:"idioma" maxLength:"10"`
	Categoria  string        `json:"categoria" enum:"UTILITY,MARKETING,AUTHENTICATION"`
	Definicion DefinicionPWA `json:"definicion"`
}

// ResultadoReglasPWA es la lista «Antes de enviar a Meta».
type ResultadoReglasPWA struct {
	Ok      bool       `json:"ok" doc:"Todas las reglas se cumplen: se puede enviar"`
	Formato string     `json:"formato" enum:"NAMED,POSITIONAL"`
	Reglas  []ReglaPWA `json:"reglas"`
}

// reglasCompletas: las de Meta más las que miran la base (nombre libre y marketing).
func (m *Modulo) reglasCompletas(ctx context.Context, q *mensajeriabd.Queries, empresa int64, waba string, excepto int64, in EntradaReglasPW) (ResultadoReglasPWA, error) {
	in.Definicion = normalizarDefinicion(in.Definicion)
	r := revisarPlantilla(in)
	r.agregar("nombre_libre", "Nombre libre en esta cuenta y en este idioma")
	if r.reglas["nombre"].Ok {
		if err := revisarNombre(ctx, q, empresa, waba, in.Nombre, in.Idioma, excepto, m.ahora()); err != nil {
			var p *nucleo.Problema
			if !errors.As(err, &p) {
				return ResultadoReglasPWA{}, err
			}
			msg := p.Detail
			if p.Sugerencia != nil {
				msg += " Prueba con «" + *p.Sugerencia + "»."
			}
			r.falla("nombre_libre", "nombre", msg)
		}
	}
	if in.Categoria == CategoriaMarketing {
		r.agregar("marketing", "Marketing activado para tu empresa (Meta cobra más)")
		ok, err := marketingPermitido(ctx, q, empresa)
		if err != nil {
			return ResultadoReglasPWA{}, err
		}
		if !ok {
			r.falla("marketing", "categoria", marketingNoPermitido().Detail)
		}
	}
	out := ResultadoReglasPWA{Ok: true, Reglas: r.lista(), Formato: formatoDe(slices.Concat(variablesDeTexto(in.Definicion.Cuerpo),
		variablesDeTexto(derefTexto(encabezadoTexto(in.Definicion)))))}
	if out.Formato == "" {
		out.Formato = "NAMED"
	}
	for _, x := range out.Reglas {
		out.Ok = out.Ok && x.Ok
	}
	return out, nil
}

func encabezadoTexto(d DefinicionPWA) *string {
	if d.Encabezado == nil {
		return nil
	}
	return d.Encabezado.Texto
}

func nuevoValidarPWA(m *Modulo) *auditoria.Caso[EntradaValidarPWA, ResultadoReglasPWA] {
	return auditoria.Nuevo(auditoria.Caso[EntradaValidarPWA, ResultadoReglasPWA]{
		Accion: "plantillas_whatsapp.validar", Permiso: nucleo.PermPlantillasWhatsAppVer, AuditarExito: sinAuditar[ResultadoReglasPWA],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaValidarPWA) (ResultadoReglasPWA, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return ResultadoReglasPWA{}, err
			}
			q := mensajeriabd.New(t)
			if _, err := canalesDeCuentaEmpresa(ctx, q, contexto.EmpresaDe(ctx), in.WabaID); err != nil {
				return ResultadoReglasPWA{}, err
			}
			return m.reglasCompletas(ctx, q, contexto.EmpresaDe(ctx), in.WabaID, in.ID,
				EntradaReglasPW{Nombre: in.Nombre, Idioma: in.Idioma, Categoria: in.Categoria, Definicion: in.Definicion})
		},
	})
}

// Crear y editar borradores ---------------------------------------------------------------------

// EntradaCrearPWA crea un borrador (y, con enviar, lo manda a Meta: ver la ruta).
type EntradaCrearPWA struct {
	WabaID     string        `json:"wabaId" pattern:"^[0-9]{1,30}$"`
	Etiqueta   string        `json:"etiqueta" minLength:"1" maxLength:"80" doc:"Nombre para personas"`
	Nombre     string        `json:"nombre,omitempty" maxLength:"512" doc:"Nombre en Meta (vacío = se propone desde la etiqueta)"`
	Idioma     string        `json:"idioma" minLength:"2" maxLength:"10" doc:"es, es_MX…"`
	Categoria  string        `json:"categoria" enum:"UTILITY,MARKETING,AUTHENTICATION"`
	Definicion DefinicionPWA `json:"definicion"`
	Usos       []string      `json:"usos,omitempty" maxItems:"3" doc:"avisos, conversaciones, asistente"`
	Codigos    []string      `json:"codigos,omitempty" maxItems:"13" doc:"Códigos del catálogo de Iris que atenderá"`
	Enviar     bool          `json:"enviar,omitempty" doc:"Además, enviarla a Meta (si las reglas se cumplen)"`
	sugerida   *string
	origen     string
}

// EntradaEditarPWA edita un borrador.
type EntradaEditarPWA struct {
	ID         int64         `json:"-"`
	Etiqueta   string        `json:"etiqueta" minLength:"1" maxLength:"80"`
	Nombre     string        `json:"nombre" minLength:"1" maxLength:"512"`
	Idioma     string        `json:"idioma" minLength:"2" maxLength:"10"`
	Categoria  string        `json:"categoria" enum:"UTILITY,MARKETING,AUTHENTICATION"`
	Definicion DefinicionPWA `json:"definicion"`
	Version    int32         `json:"version" minimum:"1"`
}

func formatoDeDefinicion(d DefinicionPWA) string {
	vs := slices.Concat(variablesDeTexto(derefTexto(encabezadoTexto(d))), variablesDeTexto(d.Cuerpo))
	for _, b := range d.Botones {
		if b.Tipo == "url" {
			vs = append(vs, variablesDeTexto(derefTexto(b.URL))...)
		}
	}
	if f := formatoDe(vs); f != "" {
		return f
	}
	return "NAMED"
}

func nuevoCrearPWA(m *Modulo) *auditoria.Caso[EntradaCrearPWA, PlantillaWA] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearPWA, PlantillaWA]{
		Accion: "plantillas_whatsapp.crear", Permiso: nucleo.PermPlantillasWhatsAppCrear,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearPWA) (PlantillaWA, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return PlantillaWA{}, err
			}
			q := mensajeriabd.New(t)
			empresa := contexto.EmpresaDe(ctx)
			canales, err := canalesDeCuentaEmpresa(ctx, q, empresa, in.WabaID)
			if err != nil {
				return PlantillaWA{}, err
			}
			// El candado de la cuenta antes que sus filas (mismo orden que la relectura).
			if err := q.BloquearCuentaPlantillas(ctx, in.WabaID); err != nil {
				return PlantillaWA{}, err
			}
			ahora := m.ahora()
			in.Etiqueta = strings.TrimSpace(in.Etiqueta)
			if in.Nombre = strings.TrimSpace(in.Nombre); in.Nombre == "" {
				if in.Nombre, err = nombreLibre(ctx, q, empresa, in.WabaID, NombreMetaDe(in.Etiqueta), ahora); err != nil {
					return PlantillaWA{}, err
				}
			}
			if !reNombrePW.MatchString(in.Nombre) {
				return PlantillaWA{}, campoConv("nombre", "El nombre en Meta solo lleva minúsculas sin tildes, números y _", "pattern")
			}
			if !reIdiomaPW.MatchString(in.Idioma) {
				return PlantillaWA{}, campoConv("idioma", "Idioma de Meta (es, es_MX…)", "pattern")
			}
			if err := validarUsosPW(in.Usos); err != nil {
				return PlantillaWA{}, err
			}
			if err := revisarNombre(ctx, q, empresa, in.WabaID, in.Nombre, in.Idioma, 0, ahora); err != nil {
				return PlantillaWA{}, err
			}
			if in.Categoria == CategoriaMarketing {
				ok, err := marketingPermitido(ctx, q, empresa)
				if err != nil {
					return PlantillaWA{}, err
				}
				if !ok {
					return PlantillaWA{}, marketingNoPermitido()
				}
			}
			d := normalizarDefinicion(in.Definicion)
			formato := formatoDeDefinicion(d)
			params := parametrosDe(d)
			codigos := normalizarCodigos(in.Codigos)
			for _, codigo := range codigos {
				if err := revisarCodigo(codigo, in.Categoria, d, formato, params); err != nil {
					return PlantillaWA{}, err
				}
			}
			if err := tomarCodigos(ctx, t, q, empresa, in.WabaID, 0, codigos, ahora); err != nil {
				return PlantillaWA{}, err
			}
			comp, _ := json.Marshal(d)
			pj, _ := json.Marshal(params)
			origen := in.origen
			if origen == "" {
				origen = OrigenPWIris
			}
			usos := in.Usos
			if usos == nil {
				usos = []string{}
			}
			f, err := q.InsertarPlantillaWA(ctx, mensajeriabd.InsertarPlantillaWAParams{EmpresaID: canales[0].EmpresaID, WabaID: in.WabaID, Nombre: in.Nombre,
				Idioma: in.Idioma, Etiqueta: in.Etiqueta, Categoria: in.Categoria, Estado: EstadoPWBorrador, Calidad: "UNKNOWN", Formato: formato,
				Componentes: comp, Codigos: codigos, Parametros: pj, Usos: usos, Origen: origen, Sugerida: in.sugerida, CreadaPor: actorPW(ctx)})
			if err != nil {
				return PlantillaWA{}, conflictoNombrePW(err)
			}
			t.FijarEmpresa(empresa)
			t.RegistrarCambios("plantilla_whatsapp", f.ID, nil, auditablePW(f))
			if err := historialPW(ctx, q, f.ID, "creada", nil, ptr(EstadoPWBorrador), map[string]any{"origen": origen, "sugerida": in.sugerida},
				"usuario", actorPW(ctx), ahora); err != nil {
				return PlantillaWA{}, err
			}
			return aPlantillaWA(f, ahora), t.Emitir("plantilla_whatsapp.creada", conPW(basePW(f), map[string]any{"nombre": f.Nombre, "idioma": f.Idioma,
				"categoria": f.Categoria, "origen": origen}))
		},
	})
}

// conflictoNombrePW: la carrera entre dos altas con el mismo nombre (índice único).
func conflictoNombrePW(err error) error {
	var pe interface{ SQLState() string }
	if errors.As(err, &pe) && pe.SQLState() == "23505" {
		return nucleo.NuevoProblema(http.StatusConflict, "nombre_en_uso", "Nombre en uso", "Ya hay una plantilla con ese nombre e idioma, o ese aviso ya tiene plantilla")
	}
	return err
}

func nuevoEditarPWA(m *Modulo) *auditoria.Caso[EntradaEditarPWA, PlantillaWA] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEditarPWA, PlantillaWA]{
		Accion: "plantillas_whatsapp.editar", Permiso: nucleo.PermPlantillasWhatsAppEditar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEditarPWA) (PlantillaWA, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return PlantillaWA{}, err
			}
			q := mensajeriabd.New(t)
			antes, err := m.plantillaConCandado(ctx, q, in.ID)
			if err != nil {
				return PlantillaWA{}, err
			}
			if antes.Version != in.Version {
				return PlantillaWA{}, nucleo.ConflictoVersion("plantilla_whatsapp", in.ID)
			}
			if antes.Estado != EstadoPWBorrador {
				return PlantillaWA{}, nucleo.Conflicto("no_es_borrador", "La plantilla ya está en Meta: corrígela con «Corregir y volver a enviar»")
			}
			ahora := m.ahora()
			in.Nombre, in.Etiqueta = strings.TrimSpace(in.Nombre), strings.TrimSpace(in.Etiqueta)
			if !reNombrePW.MatchString(in.Nombre) {
				return PlantillaWA{}, campoConv("nombre", "El nombre en Meta solo lleva minúsculas sin tildes, números y _", "pattern")
			}
			if !reIdiomaPW.MatchString(in.Idioma) {
				return PlantillaWA{}, campoConv("idioma", "Idioma de Meta (es, es_MX…)", "pattern")
			}
			if err := revisarNombre(ctx, q, contexto.EmpresaDe(ctx), antes.WabaID, in.Nombre, in.Idioma, antes.ID, ahora); err != nil {
				return PlantillaWA{}, err
			}
			if in.Categoria == CategoriaMarketing && antes.Categoria != CategoriaMarketing {
				ok, err := marketingPermitido(ctx, q, contexto.EmpresaDe(ctx))
				if err != nil {
					return PlantillaWA{}, err
				}
				if !ok {
					return PlantillaWA{}, marketingNoPermitido()
				}
			}
			d := normalizarDefinicion(in.Definicion)
			f := antes
			f.Nombre, f.Idioma, f.Etiqueta, f.Categoria, f.Formato = in.Nombre, in.Idioma, in.Etiqueta, in.Categoria, formatoDeDefinicion(d)
			params := parametrosDe(d)
			for _, codigo := range f.Codigos {
				if err := revisarCodigo(codigo, f.Categoria, d, f.Formato, params); err != nil {
					return PlantillaWA{}, err
				}
			}
			f.Componentes, _ = json.Marshal(d)
			f.Parametros, _ = json.Marshal(params)
			if f, err = guardarPW(ctx, q, f); err != nil {
				return PlantillaWA{}, conflictoNombrePW(err)
			}
			t.FijarEmpresa(contexto.EmpresaDe(ctx))
			campos := t.RegistrarCambios("plantilla_whatsapp", f.ID, auditablePW(antes), auditablePW(f))
			if campos == nil {
				campos = []string{}
			}
			if err := historialPW(ctx, q, f.ID, "editada", nil, nil, map[string]any{"campos": campos}, "usuario", actorPW(ctx), ahora); err != nil {
				return PlantillaWA{}, err
			}
			return aPlantillaWA(f, ahora), t.Emitir("plantilla_whatsapp.actualizada", conPW(basePW(f), map[string]any{"campos": campos}))
		},
	})
}

// Usos y código -----------------------------------------------------------------------------------

// EntradaUsosPWA dice dónde la usa Iris.
type EntradaUsosPWA struct {
	ID      int64    `json:"-"`
	Usos    []string `json:"usos" maxItems:"3" doc:"avisos, conversaciones, asistente (vacío = no se usa)"`
	Codigos []string `json:"codigos" maxItems:"13" doc:"Códigos del catálogo que atiende ([] = ninguno). Si otra plantilla de la cuenta tenía alguno, lo suelta"`
	// Variables: qué dato de Iris va en cada variable (las de una plantilla creada fuera de Iris).
	Variables []VariableDatoPWA `json:"variables,omitempty" maxItems:"40"`
	Version   int32             `json:"version" minimum:"1"`
}

// VariableDatoPWA asigna un dato a una variable.
type VariableDatoPWA struct {
	Nombre string `json:"nombre" maxLength:"60"`
	Dato   string `json:"dato" maxLength:"60"`
}

func nuevoUsosPWA(m *Modulo) *auditoria.Caso[EntradaUsosPWA, PlantillaWA] {
	return auditoria.Nuevo(auditoria.Caso[EntradaUsosPWA, PlantillaWA]{
		Accion: "plantillas_whatsapp.usos", Permiso: nucleo.PermPlantillasWhatsAppEditar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaUsosPWA) (PlantillaWA, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return PlantillaWA{}, err
			}
			q := mensajeriabd.New(t)
			// Mismo orden de bloqueo que el resto (cuenta y después la fila): leer sin bloquear para
			// saber la cuenta, bloquear la cuenta y después la fila (si no, dos «Usos» cruzados se
			// interbloqueaban con tomarCodigos).
			antes, err := m.plantillaConCandado(ctx, q, in.ID)
			if err != nil {
				return PlantillaWA{}, err
			}
			if antes.Version != in.Version {
				return PlantillaWA{}, nucleo.ConflictoVersion("plantilla_whatsapp", in.ID)
			}
			if antes.Estado == EstadoPWBorrada {
				return PlantillaWA{}, reglaPW("plantilla_borrada", "La plantilla ya no está en Meta: elige otra")
			}
			if in.Usos == nil {
				in.Usos = []string{}
			}
			slices.Sort(in.Usos)
			in.Usos = slices.Compact(in.Usos)
			if err := validarUsosPW(in.Usos); err != nil {
				return PlantillaWA{}, err
			}
			if antes.Categoria == CategoriaMarketing && (len(in.Usos) > 0 || len(in.Codigos) > 0) {
				ok, err := marketingPermitido(ctx, q, contexto.EmpresaDe(ctx))
				if err != nil {
					return PlantillaWA{}, err
				}
				if !ok {
					return PlantillaWA{}, marketingNoPermitido()
				}
			}
			f := antes
			d := definicionDe(f.Componentes)
			if len(in.Variables) > 0 {
				for i, v := range in.Variables {
					if _, ok := DatoPorClave(v.Dato); !ok {
						return PlantillaWA{}, campoConv(fmt.Sprintf("variables[%d].dato", i), "Iris no conoce ese dato (GET /plantillas-whatsapp/datos)", "dato")
					}
					j := slices.IndexFunc(d.Variables, func(x VariablePWA) bool { return x.Nombre == v.Nombre })
					if j < 0 {
						return PlantillaWA{}, campoConv(fmt.Sprintf("variables[%d].nombre", i), "La plantilla no tiene esa variable", "variable")
					}
					d.Variables[j].Dato = ptr(v.Dato)
				}
				f.Componentes, _ = json.Marshal(d)
				f.Parametros, _ = json.Marshal(parametrosDe(d))
			}
			ahora := m.ahora()
			codigos := normalizarCodigos(in.Codigos)
			var params []ParametroPWA
			_ = json.Unmarshal(f.Parametros, &params)
			for _, codigo := range codigos {
				if err := revisarCodigo(codigo, f.Categoria, d, f.Formato, params); err != nil {
					return PlantillaWA{}, err
				}
			}
			if err := tomarCodigos(ctx, t, q, contexto.EmpresaDe(ctx), f.WabaID, f.ID, codigos, ahora); err != nil {
				return PlantillaWA{}, err
			}
			f.Codigos = codigos
			f.Usos = in.Usos
			if f, err = guardarPW(ctx, q, f); err != nil {
				return PlantillaWA{}, conflictoNombrePW(err)
			}
			t.FijarEmpresa(contexto.EmpresaDe(ctx))
			campos := t.RegistrarCambios("plantilla_whatsapp", f.ID, auditablePW(antes), auditablePW(f))
			if len(in.Variables) > 0 {
				campos = append(campos, "variables")
			}
			if campos == nil {
				campos = []string{}
			}
			if err := historialPW(ctx, q, f.ID, "usos", nil, nil, map[string]any{"usos": f.Usos, "codigos": f.Codigos}, "usuario", actorPW(ctx), ahora); err != nil {
				return PlantillaWA{}, err
			}
			return aPlantillaWA(f, ahora), t.Emitir("plantilla_whatsapp.actualizada", conPW(basePW(f), map[string]any{"campos": campos}))
		},
	})
}

// Ajustes -----------------------------------------------------------------------------------------

// AjustesPWA son los ajustes de plantillas de la empresa.
type AjustesPWA struct {
	MarketingPermitido bool  `json:"marketingPermitido" doc:"Se pueden crear y usar plantillas de marketing (Meta cobra más). Apagado de fábrica"`
	Version            int32 `json:"version" doc:"0 = sin guardar (los de fábrica)"`
}

// EntradaAjustesPWA cambia los ajustes.
type EntradaAjustesPWA struct {
	MarketingPermitido bool  `json:"marketingPermitido"`
	Version            int32 `json:"version" minimum:"0" doc:"La leída (0 si nunca se guardaron)"`
}

func nuevoVerAjustesPWA() *auditoria.Caso[struct{}, AjustesPWA] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, AjustesPWA]{
		Accion: "plantillas_whatsapp.ver", Permiso: nucleo.PermPlantillasWhatsAppVer, AuditarExito: sinAuditar[AjustesPWA],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (AjustesPWA, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return AjustesPWA{}, err
			}
			f, err := mensajeriabd.New(t).AjustesPlantillasWA(ctx, contexto.EmpresaDe(ctx))
			if errors.Is(err, pgx.ErrNoRows) {
				return AjustesPWA{}, nil
			}
			return AjustesPWA{MarketingPermitido: f.MarketingPermitido, Version: f.Version}, err
		},
	})
}

func nuevoGuardarAjustesPWA() *auditoria.Caso[EntradaAjustesPWA, AjustesPWA] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAjustesPWA, AjustesPWA]{
		Accion: "plantillas_whatsapp.ajustes", Permiso: nucleo.PermPlantillasWhatsAppEditar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAjustesPWA) (AjustesPWA, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return AjustesPWA{}, err
			}
			q := mensajeriabd.New(t)
			empresa := contexto.EmpresaDe(ctx)
			antes, err := q.AjustesPlantillasWA(ctx, empresa)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				antes = mensajeriabd.MensajeriaPlantillaWAAjuste{}
			case err != nil:
				return AjustesPWA{}, err
			}
			if antes.Version != in.Version {
				return AjustesPWA{}, nucleo.ConflictoVersion("ajustes_plantillas_whatsapp", empresa)
			}
			f, err := q.GuardarAjustesPlantillasWA(ctx, mensajeriabd.GuardarAjustesPlantillasWAParams{EmpresaID: empresa, MarketingPermitido: in.MarketingPermitido})
			if err != nil {
				return AjustesPWA{}, err
			}
			t.FijarEmpresa(empresa)
			campos := t.RegistrarCambios("ajustes_plantillas_whatsapp", empresa, map[string]any{"marketing_permitido": antes.MarketingPermitido},
				map[string]any{"marketing_permitido": f.MarketingPermitido})
			if campos == nil {
				campos = []string{}
			}
			return AjustesPWA{MarketingPermitido: f.MarketingPermitido, Version: f.Version},
				t.Emitir("plantillas_whatsapp.ajustes_cambiados", map[string]any{"empresa_id": empresa, "campos": campos, "marketing_permitido": f.MarketingPermitido})
		},
	})
}

// Sugeridas ---------------------------------------------------------------------------------------

// EntradaSugeridasPWA elige la cuenta.
type EntradaSugeridasPWA struct {
	WabaID string `query:"wabaId" pattern:"^[0-9]{1,30}$" required:"true"`
}

// SugeridaEnCuentaPWA es una sugerida con la plantilla que ya la cubre (si hay).
type SugeridaEnCuentaPWA struct {
	SugeridaPWA
	Plantilla *SugeridaExistentePWA `json:"plantilla" doc:"La que ya tienes (null = falta crearla)"`
}

// SugeridaExistentePWA resume la que ya existe.
type SugeridaExistentePWA struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
	Estado string `json:"estado"`
}

// ListaSugeridasPWA es { data }.
type ListaSugeridasPWA struct {
	Data    []SugeridaEnCuentaPWA `json:"data"`
	Faltan  int                   `json:"faltan"`
	Empresa string                `json:"empresa" doc:"Nombre de la empresa (pie de fábrica)"`
}

func nuevoSugeridasPWA(m *Modulo) *auditoria.Caso[EntradaSugeridasPWA, ListaSugeridasPWA] {
	return auditoria.Nuevo(auditoria.Caso[EntradaSugeridasPWA, ListaSugeridasPWA]{
		Accion: "plantillas_whatsapp.ver", Permiso: nucleo.PermPlantillasWhatsAppVer, AuditarExito: sinAuditar[ListaSugeridasPWA],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaSugeridasPWA) (ListaSugeridasPWA, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return ListaSugeridasPWA{}, err
			}
			q := mensajeriabd.New(t)
			empresa := contexto.EmpresaDe(ctx)
			if _, err := canalesDeCuentaEmpresa(ctx, q, empresa, in.WabaID); err != nil {
				return ListaSugeridasPWA{}, err
			}
			fs, err := q.ListarPlantillasWA(ctx, mensajeriabd.ListarPlantillasWAParams{Empresa: empresa, WabaID: in.WabaID, Limite: 10000})
			if err != nil {
				return ListaSugeridasPWA{}, err
			}
			nombre, err := q.NombreEmpresaParaPlantilla(ctx, empresa)
			if err != nil {
				return ListaSugeridasPWA{}, err
			}
			out := ListaSugeridasPWA{Data: []SugeridaEnCuentaPWA{}, Empresa: nombre}
			for _, s := range Sugeridas {
				x := SugeridaEnCuentaPWA{SugeridaPWA: s}
				for _, f := range fs {
					if f.Estado == EstadoPWBorrada {
						continue
					}
					if derefTexto(f.Sugerida) == s.Clave || f.Nombre == s.Clave || (s.Codigo != nil && slices.Contains(f.Codigos, *s.Codigo)) {
						x.Plantilla = &SugeridaExistentePWA{ID: f.ID, Nombre: f.Nombre, Estado: f.Estado}
						break
					}
				}
				if x.Plantilla == nil {
					out.Faltan++
				}
				out.Data = append(out.Data, x)
			}
			return out, nil
		},
	})
}

// EntradaCrearSugeridaPWA crea una sugerida en la cuenta.
type EntradaCrearSugeridaPWA struct {
	Clave  string  `json:"-"`
	WabaID string  `json:"wabaId" pattern:"^[0-9]{1,30}$"`
	Idioma string  `json:"idioma,omitempty" maxLength:"10" doc:"es por defecto"`
	Pie    *string `json:"pie,omitempty" maxLength:"60" doc:"Pie (por defecto, el nombre de la empresa); vacío = sin pie"`
	Enviar bool    `json:"enviar,omitempty" doc:"Además, enviarla a Meta"`
}

func nuevoCrearSugeridaPWA(m *Modulo) *auditoria.Caso[EntradaCrearSugeridaPWA, PlantillaWA] {
	crear := nuevoCrearPWA(m)
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearSugeridaPWA, PlantillaWA]{
		Accion: "plantillas_whatsapp.crear_sugerida", Permiso: nucleo.PermPlantillasWhatsAppCrear, AuditarExito: sinAuditar[PlantillaWA],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearSugeridaPWA) (PlantillaWA, error) {
			if err := soloPersonasPW(ctx); err != nil {
				return PlantillaWA{}, err
			}
			empresa := contexto.EmpresaDe(ctx)
			// Primero que la cuenta sea de la empresa (lecturas sin bloqueo), después el candado de la cuenta
			// antes que sus filas (mismo orden que la relectura).
			if _, err := canalesDeCuentaEmpresa(ctx, mensajeriabd.New(t), empresa, in.WabaID); err != nil {
				return PlantillaWA{}, err
			}
			if err := mensajeriabd.New(t).BloquearCuentaPlantillas(ctx, in.WabaID); err != nil {
				return PlantillaWA{}, err
			}
			s, ok := SugeridaPorClave(in.Clave)
			if !ok {
				return PlantillaWA{}, nucleo.NoEncontrado("No es una plantilla sugerida")
			}
			if in.Idioma == "" {
				in.Idioma = "es"
			}
			d := s.Definicion
			d.Botones = slices.Clone(d.Botones)
			d.Variables = slices.Clone(d.Variables)
			if s.Categoria != CategoriaAutenticacion {
				pie := in.Pie
				if pie == nil {
					n, err := mensajeriabd.New(t).NombreEmpresaParaPlantilla(ctx, contexto.EmpresaDe(ctx))
					if err != nil {
						return PlantillaWA{}, err
					}
					pie = &n
				}
				if p := strings.TrimSpace(*pie); p != "" {
					d.Pie = ptr(recortar(p, MaxPiePW))
				}
			}
			codigos := []string{}
			if s.Codigo != nil {
				// Si otra plantilla ya atiende ese aviso, la sugerida se crea sin el código (se elige después).
				fs, err := mensajeriabd.New(t).PlantillasWACodigoDe(ctx, mensajeriabd.PlantillasWACodigoDeParams{WabaID: in.WabaID, Empresa: empresa, Codigo: *s.Codigo, Excepto: 0})
				if err != nil {
					return PlantillaWA{}, err
				}
				if !slices.ContainsFunc(fs, func(f mensajeriabd.MensajeriaPlantillaWA) bool { return f.Estado != EstadoPWBorrada }) {
					codigos = []string{*s.Codigo}
				}
			}
			nombre, err := nombreLibre(ctx, mensajeriabd.New(t), empresa, in.WabaID, s.Clave, m.ahora())
			if err != nil {
				return PlantillaWA{}, err
			}
			return crear.Ejecutar(ctx, EntradaCrearPWA{WabaID: in.WabaID, Etiqueta: s.Etiqueta, Nombre: nombre, Idioma: in.Idioma, Categoria: s.Categoria,
				Definicion: d, Usos: slices.Clone(s.Usos), Codigos: codigos, sugerida: &s.Clave, origen: OrigenPWSugerida})
		},
	})
}
