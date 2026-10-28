package nucleo

import (
	"encoding/base64"
	"encoding/json"
)

// Paginación por cursor (keyset): `?cursor=&limit=` → `{ data, nextCursor }`.
//
// El cursor es opaco para el cliente: base64url de un arreglo JSON con los valores de la última
// fila según el orden de la consulta (normalmente su id).

const (
	LimitePorDefecto = 50
	LimiteMaximo     = 200
)

// ParamsPagina se embebe en la entrada de los endpoints de lista (huma lo lee del query).
type ParamsPagina struct {
	Cursor string `query:"cursor" maxLength:"200" doc:"Cursor opaco que devolvió la página anterior (nextCursor)"`
	Limit  int    `query:"limit" minimum:"1" maximum:"200" default:"50" doc:"Tamaño de página"`
}

// Limite devuelve el límite acotado a [1, LimiteMaximo].
func (p ParamsPagina) Limite() int {
	switch {
	case p.Limit <= 0:
		return LimitePorDefecto
	case p.Limit > LimiteMaximo:
		return LimiteMaximo
	}
	return p.Limit
}

// Pagina es la respuesta de toda lista.
type Pagina[T any] struct {
	Data       []T     `json:"data"`
	NextCursor *string `json:"nextCursor" nullable:"true" doc:"Cursor de la página siguiente; null si no hay más"`
}

// CodificarCursor arma el cursor con los valores de la última fila.
func CodificarCursor(valores ...any) string {
	b, _ := json.Marshal(valores)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodificarCursor lee el cursor en los destinos dados (punteros). Un cursor vacío deja los
// destinos sin tocar y devuelve false. Un cursor mal formado es un 422 en el campo cursor.
func DecodificarCursor(cursor string, destinos ...any) (bool, error) {
	if cursor == "" {
		return false, nil
	}
	invalido := Validacion(ErrorCampo{Field: "cursor", Message: "Cursor inválido", Rule: "cursor"})
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return false, invalido
	}
	var crudos []json.RawMessage
	if err := json.Unmarshal(b, &crudos); err != nil || len(crudos) != len(destinos) {
		return false, invalido
	}
	for i, d := range destinos {
		if err := json.Unmarshal(crudos[i], d); err != nil {
			return false, invalido
		}
	}
	return true, nil
}

// Paginar recorta filas (pedidas con límite + 1) y arma la página. clave da los valores del
// cursor de una fila.
func Paginar[F, T any](filas []F, limite int, convertir func(F) T, clave func(F) []any) Pagina[T] {
	hayMas := len(filas) > limite
	if hayMas {
		filas = filas[:limite]
	}
	data := make([]T, 0, len(filas))
	for _, f := range filas {
		data = append(data, convertir(f))
	}
	pagina := Pagina[T]{Data: data}
	if hayMas && len(filas) > 0 {
		c := CodificarCursor(clave(filas[len(filas)-1])...)
		pagina.NextCursor = &c
	}
	return pagina
}

// PaginaDe arma una página sin más resultados (listas cortas como el catálogo de permisos).
func PaginaDe[T any](data []T) Pagina[T] {
	if data == nil {
		data = []T{}
	}
	return Pagina[T]{Data: data}
}
