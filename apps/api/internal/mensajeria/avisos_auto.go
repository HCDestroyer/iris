package mensajeria

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Recordatorios que entrega Iris (contrato con Argos 0.15.0 §4.6 y §13.3, bloque C).
//
// Iris declara por empresa (capacidades.mensajeria[]) los canales por los que entrega y, en cada uno,
// los códigos de §6.3 que puede mandar. Hoy Iris solo entrega por WhatsApp: correo y Telegram los
// sigue mandando Argos (C4). Argos pide con canal_preferido "auto" y, opcional, canales_permitidos
// (la intersección del paso con lo que Iris declara). Iris usa un solo canal por aviso (C2), en el
// orden de la empresa: pasa al siguiente solo antes del primer envío y solo por causas del canal
// (sin permiso, baja de ese canal, sin contacto o canal caído); si no queda ninguno, el aviso queda
// bloqueado con canal_no_disponible y Argos lo manda por sus canales con otro envio_id (C12).

// CanalAuto es el canal_preferido con el que Iris elige el canal (0.15.0).
const CanalAuto = "auto"

// ContratoEntregaAuto es el primer contrato de Argos que pide avisos con canal_preferido "auto".
const ContratoEntregaAuto = argos.ContratoEntregaAuto

// canalesEntrega son los canales por los que Iris entrega avisos, en el orden de la empresa. Hoy solo
// WhatsApp (C1, C4): un canal entra aquí solo cuando Iris lo implementa.
var canalesEntrega = []string{CanalWhatsApp}

// canalDeclarado es un canal que Iris declara para una empresa con sus códigos usables.
type canalDeclarado struct {
	canal   string
	codigos []string
}

// declararEmpresa: los canales que Iris declara para la empresa (C1, C8, C11).
//
//   - WhatsApp, solo con un número de avisos propio o prestado (con el uso avisos y marcado como su
//     número de avisos, §15.3 de conversaciones.md) activo, conectado y sin el token rechazado por
//     Meta; o, sin número y en desarrollo, el simulado.
//   - códigos: los de Argos (§6.3) con una plantilla aprobada, sin pausa y con el uso avisos en la
//     cuenta de WhatsApp dueña del número (con préstamo, la de la empresa que presta), utility o, si
//     la empresa lo permite, marketing (una reclasificada a marketing sin permiso sale de la lista).
func declararEmpresa(ctx context.Context, q *mensajeriabd.Queries, empresa int64, ahora time.Time, simulado bool) ([]canalDeclarado, error) {
	out := []canalDeclarado{}
	for _, medio := range canalesEntrega {
		codigos, ok, err := codigosDeCanal(ctx, q, empresa, medio, ahora, simulado)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, canalDeclarado{canal: medio, codigos: codigos})
		}
	}
	return out, nil
}

func codigosDeCanal(ctx context.Context, q *mensajeriabd.Queries, empresa int64, medio string, ahora time.Time, simulado bool) ([]string, bool, error) {
	f, err := q.CanalAvisosDeEmpresa(ctx, mensajeriabd.CanalAvisosDeEmpresaParams{EmpresaID: empresa, Canal: medio})
	if errors.Is(err, pgx.ErrNoRows) {
		f, err = q.CanalAvisosPrestado(ctx, mensajeriabd.CanalAvisosPrestadoParams{EmpresaID: empresa, Canal: medio})
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if simulado && medio == CanalWhatsApp {
			return codigosArgos(func(string) bool { return true }), true, nil
		}
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	// El token o el permiso del número los rechazó Meta al enviar: no está operativo hasta que un
	// envío vuelva a salir o se pruebe la conexión.
	if f.Tipo != TipoWhatsAppCloud || f.RechazadoPorEnvio {
		return nil, false, nil
	}
	plantillas, err := plantillasDeCanalEn(ctx, q, f, "", ahora)
	if err != nil {
		return nil, false, err
	}
	return codigosArgos(func(codigo string) bool {
		pm, ok := plantillas[codigo]
		if !ok || (pm.Usos != nil && !slices.Contains(pm.Usos, UsoPWAvisos)) {
			return false
		}
		// Utility (o del mapa propio del número, sin categoría) y marketing solo si la empresa lo
		// permite (ya lo filtra PlantillasWAUsables). Nunca una de autenticación.
		return pm.Categoria == "" || pm.Categoria == CategoriaUtility || pm.Categoria == CategoriaMarketing
	}), true, nil
}

// codigosArgos: los códigos que pide Argos (§6.3) que cumplen usable, en el orden del catálogo.
func codigosArgos(usable func(string) bool) []string {
	out := []string{}
	for _, p := range Catalogo {
		if p.Pide == PideArgos && !p.Interna && usable(p.Codigo) {
			out = append(out, p.Codigo)
		}
	}
	return out
}

// DeclararMensajeria calcula capacidades.mensajeria[] (0.15.0 §4.6 y §13.3) para las empresas del
// alcance del conector. La conecta cmd/iris en el conector Argos (GET …/capacidades), que la llama
// solo con un Argos ≥ 0.15.0. simulado: IRIS_MENSAJERIA_SIMULADO (desarrollo y CI).
func DeclararMensajeria(simulado bool, ahora func() time.Time) argos.DeclararMensajeria {
	if ahora == nil {
		ahora = time.Now
	}
	return func(ctx context.Context, db argosbd.DBTX, empresas []int64) ([]argos.CapacidadMensajeria, error) {
		q := mensajeriabd.New(db)
		out := []argos.CapacidadMensajeria{}
		vistas := map[int64]bool{}
		for _, e := range empresas {
			if vistas[e] {
				continue
			}
			vistas[e] = true
			ds, err := declararEmpresa(ctx, q, e, ahora(), simulado)
			if err != nil {
				return nil, err
			}
			fila := argos.CapacidadMensajeria{EmpresaID: e, EntregaAuto: len(ds) > 0, Canales: []argos.CanalMensajeriaCap{}}
			for _, d := range ds {
				fila.Canales = append(fila.Canales, argos.CanalMensajeriaCap{Canal: d.canal, Codigos: d.codigos})
			}
			out = append(out, fila)
		}
		return out, nil
	}
}

// canalNoPermitido es el 422 de §6.2 y §13.3 (C10).
func canalNoPermitido(detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "canal_no_permitido", "Canal no permitido", detalle,
		nucleo.ErrorCampo{Field: "canales_permitidos", Message: detalle, Rule: "canal_no_permitido"})
}

// validarCanales aplica C10 a la forma de la entrada (sin la base): canales_permitidos solo con
// "auto", sin lista vacía ni valores repetidos. La ausencia de la lista es «todos los declarados».
func validarCanales(in EntradaAviso) error {
	if in.CanalesPermitidos == nil {
		return nil
	}
	switch {
	case in.CanalPreferido != CanalAuto:
		return canalNoPermitido("canales_permitidos solo va con canal_preferido auto")
	case len(in.CanalesPermitidos) == 0:
		return canalNoPermitido("canales_permitidos no puede ir vacía (sin la lista, Iris usa todos los canales que declara)")
	}
	vistos := map[string]bool{}
	for _, c := range in.CanalesPermitidos {
		if vistos[c] {
			return canalNoPermitido("canales_permitidos tiene valores repetidos")
		}
		vistos[c] = true
	}
	return nil
}

// candidatosAuto: los canales que Iris prueba para un aviso auto, en el orden de la empresa. Un
// integración necesita un Argos que informó 0.15.0 (§4.6); un canal de canales_permitidos que Iris no
// declara para la empresa responde 422 canal_no_permitido (§4.6, C10: Argos relee capacidades).
func (m *Modulo) candidatosAuto(ctx context.Context, q *mensajeriabd.Queries, in EntradaAviso, ahora time.Time) ([]canalDeclarado, error) {
	if origenDe(ctx) == "argos" {
		contrato, err := q.ContratoArgosActivo(ctx)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if !conector.VersionAlMenos(contrato, ContratoEntregaAuto) {
			return nil, canalNoPermitido("canal_preferido auto necesita un Argos que informe el contrato " + ContratoEntregaAuto + " o posterior")
		}
	}
	declarados, err := declararEmpresa(ctx, q, in.EmpresaID, ahora, m.cfg.Simulado)
	if err != nil {
		return nil, err
	}
	if in.CanalesPermitidos == nil {
		return declarados, nil
	}
	for _, c := range in.CanalesPermitidos {
		if !slices.ContainsFunc(declarados, func(d canalDeclarado) bool { return d.canal == c }) {
			return nil, canalNoPermitido("Iris no declara el canal " + recortar(c, 20) + " para la empresa (relee capacidades.mensajeria)")
		}
	}
	// En el orden de la empresa, no en el de la lista.
	out := []canalDeclarado{}
	for _, d := range declarados {
		if slices.Contains(in.CanalesPermitidos, d.canal) {
			out = append(out, d)
		}
	}
	return out, nil
}

// causaDeCanal: motivos por los que, con auto, se pasa al canal siguiente antes del primer envío.
// El límite diario no es del canal (aplica a todos, §6.5).
var causaDeCanal = map[string]bool{MotivoSinPermiso: true, MotivoBaja: true, MotivoSinContacto: true, MotivoCanalNoDisponible: true}

// decidirAuto prueba los canales candidatos en orden (C2): el primero que puede enviarlo queda como
// el canal del aviso. Sin ninguno, canal_no_disponible con el detalle de cada canal (nunca el número).
func (m *Modulo) decidirAuto(ctx context.Context, q *mensajeriabd.Queries, in entradaFinalAviso, p Plantilla, fila *mensajeriabd.InsertarAvisoParams) (string, error) {
	causas := []string{}
	for _, c := range in.candidatos {
		if !slices.Contains(c.codigos, p.Codigo) {
			causas = append(causas, c.canal+": "+MotivoCanalNoDisponible)
			continue
		}
		motivo, err := m.probarMedioAviso(ctx, q, in, p, fila, c.canal)
		if err != nil {
			return "", err
		}
		if motivo == "" {
			return "", nil
		}
		if !causaDeCanal[motivo] {
			return motivo, nil
		}
		causas = append(causas, c.canal+": "+motivo)
	}
	if len(causas) == 0 {
		causas = append(causas, "sin canales declarados")
	}
	d := strings.Join(causas, "; ")
	fila.Detalle = &d
	fila.Canal, fila.CanalID, fila.Remitente, fila.Destino = nil, nil, nil, nil
	return MotivoCanalNoDisponible, nil
}
