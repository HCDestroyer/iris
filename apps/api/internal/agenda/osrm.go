package agenda

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Punto geográfico (grados decimales).
type Punto struct {
	Lat float64
	Lon float64
}

// Matriz de tiempos (segundos) y distancias (metros) entre puntos: [desde][hasta].
type Matriz struct {
	Segundos [][]float64
	Metros   [][]float64
}

// Enrutador da la matriz de tiempos y el trazo de una ruta (OSRM). Se llama siempre fuera de
// una transacción. nil = sin OSRM: la ruta se calcula en línea recta (haversine).
type Enrutador interface {
	// Matriz de viaje entre todos los puntos (OSRM /table).
	Matriz(ctx context.Context, puntos []Punto) (Matriz, error)
	// Trazo de la ruta que pasa por los puntos en ese orden (OSRM /route), como coordenadas
	// GeoJSON [lon, lat].
	Trazo(ctx context.Context, puntos []Punto) ([][2]float64, error)
}

// ErrOSRM: OSRM no respondió o respondió algo que no se entiende (la ruta se degrada a línea
// recta).
var ErrOSRM = errors.New("agenda: osrm no disponible")

// OSRM es el cliente HTTP del servicio propio (osrm-routed, perfil driving).
type OSRM struct {
	base    *url.URL
	cliente *http.Client
}

// maxRespuestaOSRM: una matriz de 101 × 101 con distancias cabe de sobra.
const maxRespuestaOSRM = 8 << 20

// NuevoOSRM arma el cliente con la URL base (http://osrm:5000) y el tiempo máximo por llamada
// (3 s por defecto).
func NuevoOSRM(base string, espera time.Duration) (*OSRM, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("agenda: URL de OSRM inválida %q (http o https)", base)
	}
	if espera <= 0 {
		espera = 3 * time.Second
	}
	return &OSRM{base: u, cliente: &http.Client{Timeout: espera}}, nil
}

// OSRMDesdeEntorno lee IRIS_OSRM_URL (vacía = sin OSRM: línea recta) e IRIS_OSRM_ESPERA
// (duración, 3s por defecto).
func OSRMDesdeEntorno() (Enrutador, error) {
	base := strings.TrimSpace(os.Getenv("IRIS_OSRM_URL"))
	if base == "" {
		return nil, nil
	}
	var espera time.Duration
	if v := os.Getenv("IRIS_OSRM_ESPERA"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, errors.New("IRIS_OSRM_ESPERA: duración positiva (p. ej. 3s)")
		}
		espera = d
	}
	o, err := NuevoOSRM(base, espera)
	if err != nil {
		return nil, err
	}
	return o, nil
}

// coordenadas en el formato de OSRM: lon,lat;lon,lat (6 decimales ≈ 10 cm).
func coordenadas(puntos []Punto) string {
	partes := make([]string, len(puntos))
	for i, p := range puntos {
		partes[i] = strconv.FormatFloat(p.Lon, 'f', 6, 64) + "," + strconv.FormatFloat(p.Lat, 'f', 6, 64)
	}
	return strings.Join(partes, ";")
}

// pedir hace GET <base>/<servicio>/v1/driving/<coordenadas>?<consulta> (la consulta va tal cual:
// OSRM espera las comas sin escapar).
func (o *OSRM) pedir(ctx context.Context, servicio string, puntos []Punto, consulta string, destino any) error {
	u := *o.base
	u.Path = strings.TrimRight(u.Path, "/") + "/" + servicio + "/v1/driving/" + coordenadas(puntos)
	u.RawQuery = consulta
	pet, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	res, err := o.cliente.Do(pet)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrOSRM, err)
	}
	defer func() { _ = res.Body.Close() }()
	cuerpo, err := io.ReadAll(io.LimitReader(res.Body, maxRespuestaOSRM))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrOSRM, err)
	}
	var codigo struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(cuerpo, &codigo); err != nil {
		return fmt.Errorf("%w: respuesta %d ilegible", ErrOSRM, res.StatusCode)
	}
	if res.StatusCode != http.StatusOK || codigo.Code != "Ok" {
		return fmt.Errorf("%w: %d %s %s", ErrOSRM, res.StatusCode, codigo.Code, codigo.Message)
	}
	if err := json.Unmarshal(cuerpo, destino); err != nil {
		return fmt.Errorf("%w: %w", ErrOSRM, err)
	}
	return nil
}

// Matriz implementa Enrutador con /table (annotations=duration,distance). Un par sin camino
// (null) se completa en línea recta.
func (o *OSRM) Matriz(ctx context.Context, puntos []Punto) (Matriz, error) {
	var r struct {
		Durations [][]*float64 `json:"durations"`
		Distances [][]*float64 `json:"distances"`
	}
	if err := o.pedir(ctx, "table", puntos, "annotations=duration,distance", &r); err != nil {
		return Matriz{}, err
	}
	n := len(puntos)
	if len(r.Durations) != n || len(r.Distances) != n {
		return Matriz{}, fmt.Errorf("%w: matriz de %d filas para %d puntos", ErrOSRM, len(r.Durations), n)
	}
	recta := MatrizEnLineaRecta(puntos)
	m := Matriz{Segundos: make([][]float64, n), Metros: make([][]float64, n)}
	for i := range n {
		if len(r.Durations[i]) != n || len(r.Distances[i]) != n {
			return Matriz{}, fmt.Errorf("%w: fila %d incompleta", ErrOSRM, i)
		}
		m.Segundos[i], m.Metros[i] = make([]float64, n), make([]float64, n)
		for j := range n {
			s, d := r.Durations[i][j], r.Distances[i][j]
			if s == nil || d == nil || *s < 0 || *d < 0 || math.IsNaN(*s) || math.IsNaN(*d) {
				m.Segundos[i][j], m.Metros[i][j] = recta.Segundos[i][j], recta.Metros[i][j]
				continue
			}
			m.Segundos[i][j], m.Metros[i][j] = *s, *d
		}
	}
	return m, nil
}

// Trazo implementa Enrutador con /route (overview=simplified, geometries=geojson).
func (o *OSRM) Trazo(ctx context.Context, puntos []Punto) ([][2]float64, error) {
	if len(puntos) < 2 {
		return nil, nil
	}
	var r struct {
		Routes []struct {
			Geometry struct {
				Coordinates [][2]float64 `json:"coordinates"`
			} `json:"geometry"`
		} `json:"routes"`
	}
	if err := o.pedir(ctx, "route", puntos, "overview=simplified&geometries=geojson", &r); err != nil {
		return nil, err
	}
	if len(r.Routes) == 0 {
		return nil, fmt.Errorf("%w: sin rutas", ErrOSRM)
	}
	return r.Routes[0].Geometry.Coordinates, nil
}

// Línea recta (haversine): lo que se usa sin OSRM. La distancia por calle se estima como la
// recta × FactorRodeo y el tiempo a VelocidadUrbana (km/h).
const (
	radioTierraM    = 6371008.8
	FactorRodeo     = 1.3
	VelocidadUrbana = 30.0 // km/h
)

// Haversine: distancia en metros entre dos puntos sobre la esfera.
func Haversine(a, b Punto) float64 {
	rad := math.Pi / 180
	dLat, dLon := (b.Lat-a.Lat)*rad, (b.Lon-a.Lon)*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * radioTierraM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// MatrizEnLineaRecta: distancias haversine × FactorRodeo y tiempos a VelocidadUrbana.
func MatrizEnLineaRecta(puntos []Punto) Matriz {
	n := len(puntos)
	m := Matriz{Segundos: make([][]float64, n), Metros: make([][]float64, n)}
	for i := range n {
		m.Segundos[i], m.Metros[i] = make([]float64, n), make([]float64, n)
		for j := range n {
			if i == j {
				continue
			}
			d := Haversine(puntos[i], puntos[j]) * FactorRodeo
			m.Metros[i][j], m.Segundos[i][j] = d, d/(VelocidadUrbana*1000/3600)
		}
	}
	return m
}
