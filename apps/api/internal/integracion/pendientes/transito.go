package pendientes

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Depósito de tránsito (contrato argosisp.md 0.10.0 §3.4.8, pedido B19). Lo sensible de una
// llamada del paso «Confirmar titular y plan» (número de documento, teléfono, correo, NIT/CUI y
// las fotos del documento) nunca va en el payload del pendiente: queda cifrado en
// integracion.datos_transito y se agrega al cuerpo solo al enviarlo. Se borra al 2xx de Argos, al
// resolverse a mano o a los 7 días de tomado. Nadie lo lee en Iris: no hay ruta que lo devuelva.

// Operaciones del paso «Confirmar titular y plan» (§3.4).
const (
	OpRegistrarIdentidad = "registrar_identidad" // POST /clientes/:id/identidad (A41, multipart)
	OpCambiarTitular     = "cambiar_titular"     // POST /servicios/:id/cambiar-titular (A39, multipart)
	OpCompletarDatos     = "completar_datos"     // POST /servicios/:id/completar-datos (A43)
	OpCambiarPlan        = "cambiar_plan"        // POST /servicios/:id/cambiar-plan (A40)
)

// VigenciaTransito: lo no entregado se borra a los 7 días de tomado (§3.4.8).
const VigenciaTransito = 7 * 24 * time.Hour

// CodigoFotoVencida: el pendiente ya no tiene su depósito (se borró a los 7 días): hay que tomar
// la foto otra vez.
const CodigoFotoVencida = "foto_vencida"

// FotoTransito es una foto del documento (frente o reverso).
type FotoTransito struct {
	Campo string `json:"campo"` // frente | reverso
	Mime  string `json:"mime"`
	Datos []byte `json:"datos"`
}

// Transito es lo sensible de una llamada.
//   - Campos: se mezclan con el cuerpo JSON (claves de primer nivel o, con «objeto.clave», dentro de
//     un objeto del cuerpo). En multipart, son partes de texto.
//   - Fotos: partes de archivo del multipart, en este orden.
type Transito struct {
	Campos map[string]any `json:"campos,omitempty"`
	Fotos  []FotoTransito `json:"fotos,omitempty"`
}

// DatosAsociadosTransito ata el depósito (y su llave) a su fila, su orden y su empresa:
// "transito:<uuid>:<orden_id>:<empresa_id>" (empresa "-" sin conector). Lo usan también la rotación
// de la clave (internal/nucleo/clave) y la lectura, que lo arma con la orden y la empresa del
// pendiente: un depósito copiado a otra orden o empresa no abre.
func DatosAsociadosTransito(id uuid.UUID, orden int64, empresa *int64) []byte {
	e := "-"
	if empresa != nil {
		e = strconv.FormatInt(*empresa, 10)
	}
	return []byte("transito:" + id.String() + ":" + strconv.FormatInt(orden, 10) + ":" + e)
}

// sellar cifra con AES-256-GCM (nonce aleatorio delante).
func sellar(llave, plano, ad []byte) ([]byte, error) {
	bloque, err := aes.NewCipher(llave)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(bloque)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, plano, ad), nil
}

func abrir(llave, cifrado, ad []byte) ([]byte, error) {
	bloque, err := aes.NewCipher(llave)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(bloque)
	if err != nil {
		return nil, err
	}
	if len(cifrado) < g.NonceSize() {
		return nil, errors.New("cifrado corto")
	}
	return g.Open(nil, cifrado[:g.NonceSize()], cifrado[g.NonceSize():], ad)
}

// GuardarTransito cifra y guarda el depósito en la transacción del caso de uso, con su propia
// llave de datos (aleatoria, cifrada con IRIS_CLAVE_CIFRADO y guardada en la fila). Sin cifrador no
// se guarda nada sensible: error.
func GuardarTransito(ctx context.Context, t *auditoria.Tx, cif *nucleo.Cifrador, empresa *int64, orden int64, d Transito, tomadoEn time.Time) (uuid.UUID, error) {
	if cif == nil {
		return uuid.Nil, nucleo.NoDisponible("Falta IRIS_CLAVE_CIFRADO: Iris no guarda la identidad del titular sin cifrar")
	}
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	plano, err := json.Marshal(d)
	if err != nil {
		return uuid.Nil, err
	}
	defer clear(plano)
	llave := make([]byte, 32)
	if _, err := rand.Read(llave); err != nil {
		return uuid.Nil, err
	}
	defer clear(llave)
	ad := DatosAsociadosTransito(id, orden, empresa)
	cifrado, err := sellar(llave, plano, ad)
	if err != nil {
		return uuid.Nil, err
	}
	llaveCifrada, err := cif.Cifrar(llave, ad)
	if err != nil {
		return uuid.Nil, err
	}
	return id, pendientesbd.New(t).GuardarTransito(ctx, pendientesbd.GuardarTransitoParams{
		ID: id, EmpresaID: empresa, OrdenID: orden, Cifrado: cifrado, LlaveCifrada: llaveCifrada, TomadoEn: tomadoEn.UTC()})
}

// errVencido: el depósito ya se borró.
var errVencido = errors.New("el depósito de tránsito ya se borró")

// leerTransito abre el depósito del pendiente. Los datos asociados se arman con la orden y la
// empresa del pendiente (no las de la fila): si no coinciden, no abre.
func (m *Modulo) leerTransito(ctx context.Context, q *pendientesbd.Queries, p pendientesbd.IntegracionPendiente) (Transito, error) {
	var d Transito
	f, err := q.LeerTransito(ctx, p.TransitoID.Bytes)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (f.Cifrado == nil || f.LlaveCifrada == nil)) {
		return d, errVencido
	}
	if err != nil {
		return d, err
	}
	if m.cfg.Cifrador == nil {
		return d, errors.New("sin IRIS_CLAVE_CIFRADO para abrir el depósito de tránsito")
	}
	orden, err := strconv.ParseInt(p.EntidadID, 10, 64)
	if err != nil || p.Entidad != "orden" {
		return d, errors.New("el depósito de tránsito no es de una orden")
	}
	ad := DatosAsociadosTransito(f.ID, orden, p.EmpresaID)
	llave, err := m.cfg.Cifrador.Descifrar(f.LlaveCifrada, ad)
	if err != nil {
		return d, errors.New("no se pudo abrir la llave del depósito de tránsito")
	}
	defer clear(llave)
	plano, err := abrir(llave, f.Cifrado, ad)
	if err != nil {
		return d, errors.New("no se pudo descifrar el depósito de tránsito")
	}
	defer clear(plano)
	return d, json.Unmarshal(plano, &d)
}

// borrarTransito borra el depósito del pendiente (entregado, resuelto a mano o cancelado): el
// contenido y su llave.
func borrarTransito(ctx context.Context, t *auditoria.Tx, f pendientesbd.IntegracionPendiente, motivo string, ahora time.Time) error {
	if !f.TransitoID.Valid {
		return nil
	}
	return pendientesbd.New(t).BorrarTransito(ctx, pendientesbd.BorrarTransitoParams{ID: f.TransitoID.Bytes, Ahora: &ahora, Motivo: &motivo})
}

// PurgarTransito borra los depósitos con más de 7 días (job diario ArgsPurgarTransito).
func PurgarTransito(ctx context.Context, q *pendientesbd.Queries, ahora time.Time) (int64, error) {
	return q.PurgarTransito(ctx, pendientesbd.PurgarTransitoParams{Ahora: &ahora, Antes: ahora.Add(-VigenciaTransito)})
}

// esMultipart: A41 y A39 van en multipart/form-data.
func esMultipart(operacion string) bool {
	return operacion == OpRegistrarIdentidad || operacion == OpCambiarTitular
}

// cuerpoConTransito mezcla los campos del depósito con el cuerpo JSON del pendiente. El resultado
// se serializa con las claves ordenadas: los mismos bytes en cada intento (huella de §3.1).
func cuerpoConTransito(payload []byte, d Transito) (map[string]any, error) {
	var cuerpo map[string]any
	if err := json.Unmarshal(payload, &cuerpo); err != nil {
		return nil, err
	}
	if cuerpo == nil {
		cuerpo = map[string]any{}
	}
	for _, k := range slices.Sorted(maps.Keys(d.Campos)) {
		objeto, clave, anidado := cortarClave(k)
		if !anidado {
			cuerpo[k] = d.Campos[k]
			continue
		}
		sub, _ := cuerpo[objeto].(map[string]any)
		if sub == nil {
			sub = map[string]any{}
		}
		sub[clave] = d.Campos[k]
		cuerpo[objeto] = sub
	}
	return cuerpo, nil
}

func cortarClave(k string) (objeto, clave string, ok bool) {
	for i := 0; i < len(k); i++ {
		if k[i] == '.' {
			return k[:i], k[i+1:], true
		}
	}
	return "", k, false
}

// partesMultipart arma las partes de A41 (campos de texto en orden alfabético y luego las fotos)
// o de A39 (la parte «datos» con el JSON y luego las fotos). Los mismos bytes en cada intento; la
// huella de Argos no cuenta el boundary (§3.4, «Idempotencia en multipart»).
func partesMultipart(operacion string, payload []byte, d Transito) ([]conector.Parte, error) {
	cuerpo, err := cuerpoConTransito(payload, d)
	if err != nil {
		return nil, err
	}
	var partes []conector.Parte
	if operacion == OpCambiarTitular {
		datos, err := json.Marshal(cuerpo)
		if err != nil {
			return nil, err
		}
		partes = append(partes, conector.Parte{Campo: "datos", Mime: "application/json", Datos: datos})
	} else {
		for _, k := range slices.Sorted(maps.Keys(cuerpo)) {
			var b bytes.Buffer
			switch v := cuerpo[k].(type) {
			case string:
				b.WriteString(v)
			default:
				enc, err := json.Marshal(v)
				if err != nil {
					return nil, err
				}
				b.Write(enc)
			}
			partes = append(partes, conector.Parte{Campo: k, Datos: b.Bytes()})
		}
	}
	for _, f := range d.Fotos {
		ext := map[string]string{"image/jpeg": "jpg", "image/png": "png"}[f.Mime]
		partes = append(partes, conector.Parte{Campo: f.Campo, Nombre: fmt.Sprintf("%s.%s", f.Campo, ext), Mime: f.Mime, Datos: f.Datos})
	}
	return partes, nil
}

func uuidPG(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}
