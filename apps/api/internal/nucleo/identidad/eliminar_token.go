package identidad

import (
	"context"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Eliminar tokens de API (tokens.eliminar): borrado lógico de un token revocado o vencido. La
// fila queda (auditoría, accesos, conectores y portal la siguen referenciando), su hash se borra
// (no queda material de credencial) y deja de aparecer en GET /tokens. Emite token.eliminado.

// Motivos de token.eliminado.
const (
	motivoEliminarManual   = "manual"
	motivoEliminarLote     = "lote"
	motivoEliminarConector = "conector"
)

// EntradaEliminarToken es la entrada de tokens.eliminar.
type EntradaEliminarToken struct {
	ID     int64  `json:"-"`
	Motivo string `json:"motivo,omitempty" maxLength:"500" doc:"Nota libre (opcional); va en la auditoría"`
	// origen es el motivo del evento (manual, lote, conector); no sale de la entrada HTTP.
	origen string
}

// TokensEliminables cuenta lo que eliminaría POST /tokens/eliminar-inactivos.
type TokensEliminables struct {
	Cantidad int `json:"cantidad" doc:"Tokens revocados o vencidos que se pueden eliminar"`
	EnUso    int `json:"enUso" doc:"Revocados o vencidos que no se eliminan porque un conector activo depende de ellos"`
}

// TokensEliminados es la respuesta del lote.
type TokensEliminados struct {
	Eliminados int     `json:"eliminados"`
	Omitidos   int     `json:"omitidos" doc:"Revocados o vencidos que quedaron (un conector activo depende de ellos)"`
	IDs        []int64 `json:"ids" doc:"Ids de los tokens eliminados"`
}

// EntradaTokensInactivos filtra el lote (y su conteo) por dueño.
type EntradaTokensInactivos struct {
	UsuarioID int64 `json:"usuarioId,omitempty" minimum:"0" doc:"Solo los tokens de este usuario"`
}

// ConsultaTokensInactivos es el filtro de GET /tokens/eliminables.
type ConsultaTokensInactivos struct {
	UsuarioID int64 `query:"usuario_id" minimum:"0" doc:"Solo los tokens de este usuario"`
}

func tokenVigente(t identidadbd.NucleoTokensApi, ahora time.Time) bool {
	return t.RevocadoEn == nil && (t.ExpiraEn == nil || t.ExpiraEn.After(ahora))
}

// nuevoEliminarToken arma tokens.eliminar. interno: lo llama el conector al eliminar un conector
// apagado (ya autorizado con integraciones.eliminar): no exige tokens.eliminar ni el alcance de
// empresas, y si el token aún vale lo revoca primero (token.revocado) en la misma transacción.
func nuevoEliminarToken(interno bool) *auditoria.Caso[EntradaEliminarToken, TokenAPI] {
	c := auditoria.Caso[EntradaEliminarToken, TokenAPI]{
		Accion:     "tokens.eliminar",
		Permiso:    nucleo.PermTokensEliminar,
		SinEmpresa: true, // administración: no es operativa
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEliminarToken) (TokenAPI, error) {
			q := identidadbd.New(t)
			t.FijarEntidad("token", in.ID)
			antes, err := q.TokenPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return TokenAPI{}, err
			}
			// Fuera del alcance de empresas: 404, como uno inexistente (igual que revocar).
			if !interno {
				if err := exigirVisible(ctx, q, antes.UsuarioID, "eliminar tokens de este usuario"); err != nil {
					return TokenAPI{}, err
				}
			}
			// Idempotente: eliminarlo otra vez responde lo mismo, sin cambios ni evento.
			if antes.EliminadoEn != nil {
				t.Anotar(map[string]any{"ya_eliminado": true})
				return aTokenAPI(antes), nil
			}
			usado, err := q.TokenUsadoPorConectorActivo(ctx, in.ID)
			if err != nil {
				return TokenAPI{}, err
			}
			if usado {
				return TokenAPI{}, nucleo.Conflicto("token_en_uso_por_conector",
					"Un conector activo de Argos u Horus usa este token. Desactiva o reemplaza el conector en Integraciones antes de eliminarlo")
			}
			actual := antes
			if tokenVigente(antes, time.Now()) {
				if !interno {
					return TokenAPI{}, nucleo.Conflicto("token_vigente", "El token aún vale: revócalo primero y luego elimínalo")
				}
				if actual, err = q.RevocarToken(ctx, in.ID); err != nil {
					return TokenAPI{}, err
				}
				t.RegistrarCambios("token", in.ID, aTokenAPI(antes), aTokenAPI(actual))
				if err := t.Emitir("token.revocado", map[string]any{"token_id": in.ID, "usuario_id": antes.UsuarioID, "motivo": "conector_eliminado"}); err != nil {
					return TokenAPI{}, err
				}
			}
			var por *int64
			if a := contexto.ActorDe(ctx); a.EsUsuario() {
				por = &a.ID
			}
			despues, err := q.EliminarToken(ctx, identidadbd.EliminarTokenParams{ID: in.ID, EliminadoPor: por})
			if err != nil {
				return TokenAPI{}, err
			}
			t.RegistrarCambios("token", in.ID, aTokenAPI(actual), aTokenAPI(despues))
			motivo := in.origen
			if motivo == "" {
				motivo = motivoEliminarManual
			}
			if err := t.Emitir("token.eliminado", map[string]any{"token_id": in.ID, "usuario_id": antes.UsuarioID, "motivo": motivo}); err != nil {
				return TokenAPI{}, err
			}
			return aTokenAPI(despues), nil
		},
	}
	if interno {
		c.Acceso, c.Permiso = auditoria.Autenticado, ""
	}
	return auditoria.Nuevo(c)
}

// eliminables devuelve los revocados o vencidos que el actor puede eliminar (los que ve en la
// lista y administra, como revocar) y cuántos quedan porque un conector activo depende de ellos.
func eliminables(ctx context.Context, q *identidadbd.Queries, usuarioID int64) (ids []int64, enUso int, err error) {
	a, err := alcanceDelActor(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	p := identidadbd.TokensEliminablesParams{Todas: a.todas, Empresas: lista(a.propias), Yo: contexto.ActorDe(ctx).ID}
	if usuarioID != 0 {
		p.UsuarioID = &usuarioID
	}
	filas, err := q.TokensEliminables(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	visibles := map[int64]bool{} // por dueño: exigirVisible una vez por usuario
	ids = []int64{}
	for _, f := range filas {
		ve, ok := visibles[f.UsuarioID]
		if !ok {
			e := exigirVisible(ctx, q, f.UsuarioID, "eliminar tokens de este usuario")
			if e != nil && !esCodigo(e, "no_encontrado") {
				return nil, 0, e
			}
			ve = e == nil
			visibles[f.UsuarioID] = ve
		}
		if !ve {
			continue
		}
		usado, err := q.TokenUsadoPorConectorActivo(ctx, f.ID)
		if err != nil {
			return nil, 0, err
		}
		if usado {
			enUso++
			continue
		}
		ids = append(ids, f.ID)
	}
	return ids, enUso, nil
}

func nuevoContarTokensEliminables() *auditoria.Caso[EntradaTokensInactivos, TokensEliminables] {
	return auditoria.Nuevo(auditoria.Caso[EntradaTokensInactivos, TokensEliminables]{
		Accion:       "tokens.contar_eliminables",
		Permiso:      nucleo.PermTokensEliminar,
		SinEmpresa:   true,
		AuditarExito: soloLecturas[TokensEliminables],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTokensInactivos) (TokensEliminables, error) {
			ids, enUso, err := eliminables(ctx, identidadbd.New(t), in.UsuarioID)
			return TokensEliminables{Cantidad: len(ids), EnUso: enUso}, err
		},
	})
}

// nuevoEliminarTokensInactivos elimina en lote todos los revocados o vencidos que el actor
// administra. Cada token pasa por tokens.eliminar (anidado): su fila de auditoría y su
// token.eliminado (motivo lote), en la misma transacción.
func nuevoEliminarTokensInactivos(uno *auditoria.Caso[EntradaEliminarToken, TokenAPI]) *auditoria.Caso[EntradaTokensInactivos, TokensEliminados] {
	return auditoria.Nuevo(auditoria.Caso[EntradaTokensInactivos, TokensEliminados]{
		Accion:     "tokens.eliminar_inactivos",
		Permiso:    nucleo.PermTokensEliminar,
		SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTokensInactivos) (TokensEliminados, error) {
			ids, enUso, err := eliminables(ctx, identidadbd.New(t), in.UsuarioID)
			if err != nil {
				return TokensEliminados{}, err
			}
			for _, id := range ids {
				if _, err := uno.Ejecutar(ctx, EntradaEliminarToken{ID: id, origen: motivoEliminarLote}); err != nil {
					return TokensEliminados{}, err
				}
			}
			t.Anotar(map[string]any{"eliminados": ids, "omitidos": enUso})
			return TokensEliminados{Eliminados: len(ids), Omitidos: enUso, IDs: ids}, nil
		},
	})
}

// EliminarTokenIntegracion revoca (si aún vale) y elimina el iris_pat_ que Iris entregó a un
// conector que se elimina. Con el actor de ctx; eliminar uno ya eliminado no es un error.
func (m *Modulo) EliminarTokenIntegracion(ctx context.Context, id int64) error {
	_, err := m.eliminarTokenInterno.Ejecutar(ctx, EntradaEliminarToken{ID: id, origen: motivoEliminarConector})
	return err
}
