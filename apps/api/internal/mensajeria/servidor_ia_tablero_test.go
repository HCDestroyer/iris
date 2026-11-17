package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/voz"
	"github.com/HCDestroyer/iris/apps/api/internal/voz/vozprueba"
)

// El tablero del servidor de IA (contratos/voz.md §4.12): muestras de /metricas, series, episodios
// de aviso y retención, contra la puerta real con un medidor de GPU de archivo.

func pf(v float64) *float64 { return &v }

// medir deja la lectura que escribiría el contenedor medidor y corre el latido 10 s después.
func (x *bancoIA) medir(ruta string, gpu voz.GPU) {
	x.t.Helper()
	*x.reloj = x.reloj.Add(10 * time.Second)
	b, err := json.Marshal(voz.LecturaGPU{TomadoEn: *x.reloj, GPUs: []voz.GPU{gpu}})
	if err != nil {
		x.t.Fatal(err)
	}
	if err := os.WriteFile(ruta, b, 0o600); err != nil {
		x.t.Fatal(err)
	}
	x.latir()
}

func (x *bancoIA) tablero(rango string) map[string]any {
	x.t.Helper()
	return x.exigir(x.api(x.admin, http.MethodGet, "/servidor-ia/tablero?rango="+rango, nil), http.StatusOK).cuerpo
}

// avisoIA devuelve el episodio más reciente de ese tipo en el tablero (nil si no hay).
func avisoIA(tab map[string]any, tipo string) map[string]any {
	for _, a := range tab["avisos"].([]any) {
		if m := a.(map[string]any); m["tipo"] == tipo {
			return m
		}
	}
	return nil
}

func (x *bancoIA) umbralEventos(evento, tipo string) int {
	x.t.Helper()
	return x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = $1 AND (payload->>'motor_id')::bigint = $2 AND payload->>'tipo' = $3`, evento, x.motor, tipo)
}

func puntoEn(tab map[string]any, t time.Time) map[string]any {
	for _, p := range tab["series"].([]any) {
		m := p.(map[string]any)
		if pt, err := time.Parse(time.RFC3339, m["t"].(string)); err == nil && pt.Equal(t) {
			return m
		}
	}
	return nil
}

func TestServidorIATableroMuestrasYAvisos(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "gpu.json")
	x := nuevoBancoIA(t, vozprueba.Opciones{MedidorRuta: ruta})
	x.enlazar()
	x.activar()
	x.exigir(x.elegir(x.admin, "modelo-a:latest", nil), http.StatusOK)
	um := x.exigir(x.api(x.admin, http.MethodGet, "/servidor-ia/umbrales", nil), http.StatusOK)
	x.exigir(x.api(x.admin, http.MethodPost, "/servidor-ia/umbrales/actualizar", map[string]any{"version": um.cuerpo["version"], "vramPct": 90, "vramMinutos": 1,
		"discoPct": 99, "temperaturaC": 80, "temperaturaMinutos": 1, "saturacionRechazos": 1, "saturacionMinutos": 1}), http.StatusOK)
	caliente := voz.GPU{Indice: 0, Nombre: "NVIDIA GeForce RTX 4090", UsoPct: pf(70), VRAMUsadaMb: pf(23000), VRAMTotalMb: pf(24000), TemperaturaC: pf(85), PotenciaW: pf(310)}
	normal := voz.GPU{Indice: 0, Nombre: "NVIDIA GeForce RTX 4090", UsoPct: pf(20), VRAMUsadaMb: pf(9000), VRAMTotalMb: pf(24000), TemperaturaC: pf(55), PotenciaW: pf(120)}

	// Con medidor y sin lectura todavía (el archivo no existe): «omitido» = no se puede afirmar nada.
	// Con una lectura sin ninguna tarjeta: «sin_gpu».
	if g := x.tablero("ahora")["servidor"].(map[string]any)["gpus"].(map[string]any); g["estado"] != "omitido" {
		t.Fatalf("GPU sin lectura: %v", g)
	}
	*x.reloj = x.reloj.Add(10 * time.Second)
	vacia, _ := json.Marshal(voz.LecturaGPU{TomadoEn: *x.reloj, GPUs: []voz.GPU{}})
	if err := os.WriteFile(ruta, vacia, 0o600); err != nil {
		t.Fatal(err)
	}
	x.latir()
	if g := x.tablero("ahora")["servidor"].(map[string]any)["gpus"].(map[string]any); g["estado"] != "sin_gpu" {
		t.Fatalf("GPU con lectura y sin tarjetas: %v", g)
	}
	*x.reloj = x.reloj.Add(time.Minute) // fuera de la ventana de «durante 1 minuto»

	// «Durante 1 minuto»: dos muestras por encima no bastan; con la tercera se abre el episodio, una
	// sola vez, con su evento.
	x.medir(ruta, caliente)
	x.medir(ruta, caliente)
	if a := avisoIA(x.tablero("ahora"), "vram"); a != nil {
		t.Fatalf("dos muestras no bastan para «durante 1 minuto»: %v", a)
	}
	x.medir(ruta, caliente)
	x.medir(ruta, caliente)
	tab := x.tablero("ahora")
	for _, tipo := range []string{"vram", "temperatura"} {
		a := avisoIA(tab, tipo)
		if a == nil || a["activo"] != true || a["hasta"] != nil || a["valor"] == nil || a["umbral"] == nil {
			t.Fatalf("episodio de %s: %v", tipo, a)
		}
		if n := x.umbralEventos("servidor_ia.umbral_superado", tipo); n != 1 {
			t.Fatalf("servidor_ia.umbral_superado de %s: %d", tipo, n)
		}
	}
	if a := avisoIA(tab, "vram"); a["umbral"] != float64(90) || a["valor"].(float64) < 95 || a["valor"].(float64) > 96 {
		t.Fatalf("valor y umbral del episodio de VRAM: %v", a)
	}
	// La última lectura buena y su serie.
	est := tab["estado"].(map[string]any)
	if est["lecturaOk"] != true || est["tomadoEn"] != x.reloj.Format(time.RFC3339) || est["encendidoSeg"] == nil {
		t.Fatalf("estado del tablero: %v", est)
	}
	srv := tab["servidor"].(map[string]any)
	gpus := srv["gpus"].(map[string]any)
	g := gpus["lista"].([]any)[0].(map[string]any)
	if gpus["estado"] != "ok" || g["nombre"] != "NVIDIA GeForce RTX 4090" || g["usoPct"] != float64(70) || g["temperaturaC"] != float64(85) ||
		srv["memoria"].(map[string]any)["totalMb"].(float64) <= 0 || srv["cpu"].(map[string]any)["nucleos"].(float64) < 1 ||
		len(srv["modelos"].([]any)) != 2 || srv["llm"].(map[string]any)["maxSimultaneas"] != float64(4) {
		t.Fatalf("servidor del tablero: %v", srv)
	}
	if p := puntoEn(tab, *x.reloj); p == nil || p["usoPct"] != float64(70) || p["vramPct"].(float64) < 95 {
		t.Fatalf("punto de la serie en la última muestra: %v", p)
	}
	if p := puntoEn(tab, x.reloj.Add(-5*time.Minute)); p == nil || p["usoPct"] != nil || p["vramPct"] != nil {
		t.Fatalf("un instante sin muestra va en null: %v", p)
	}

	// Vuelve a la normalidad: el episodio se cierra solo, con su evento.
	x.medir(ruta, normal)
	tab = x.tablero("ahora")
	for _, tipo := range []string{"vram", "temperatura"} {
		if a := avisoIA(tab, tipo); a == nil || a["activo"] != false || a["hasta"] != x.reloj.Format(time.RFC3339) {
			t.Fatalf("episodio de %s cerrado: %v", tipo, a)
		}
		if n := x.umbralEventos("servidor_ia.umbral_normalizado", tipo); n != 1 {
			t.Fatalf("servidor_ia.umbral_normalizado de %s: %d", tipo, n)
		}
	}

	// Una lectura que no pasa la validación de Iris (un porcentaje imposible) se descarta entera:
	// cuenta como fallida, no se muestra como actual y no mueve los umbrales.
	buena := *x.reloj
	malo := caliente
	malo.UsoPct = pf(150)
	x.medir(ruta, malo)
	tab = x.tablero("ahora")
	est = tab["estado"].(map[string]any)
	if est["lecturaOk"] != false || est["tomadoEn"] != buena.Format(time.RFC3339) || est["estado"] != "listo" {
		t.Fatalf("tras una lectura inválida: %v", est)
	}
	if g := tab["servidor"].(map[string]any)["gpus"].(map[string]any)["lista"].([]any)[0].(map[string]any); g["usoPct"] != float64(20) {
		t.Fatalf("lo que queda es la última lectura buena: %v", g)
	}
	if p := puntoEn(tab, *x.reloj); p == nil || p["usoPct"] != nil {
		t.Fatalf("la lectura inválida no entra en la serie: %v", p)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.servidor_ia_muestras WHERE motor_id = $1 AND NOT ok AND datos IS NULL AND tomada_en = $2`, x.motor, *x.reloj); n != 1 {
		t.Fatalf("muestra fallida guardada: %d", n)
	}
	if avisoIA(tab, "vram")["activo"] != false {
		t.Fatal("una lectura inválida no abre episodios")
	}

	// Saturación: un rechazo por falta de plaza en el último minuto abre el episodio.
	x.banco.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Demora = 300 * time.Millisecond })
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = x.generar()
		}()
	}
	wg.Wait()
	x.banco.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Demora = 0 })
	x.medir(ruta, normal)
	tab = x.tablero("1h")
	if a := avisoIA(tab, "saturacion"); a == nil || a["activo"] != true || a["valor"] != float64(1) || x.umbralEventos("servidor_ia.umbral_superado", "saturacion") != 1 {
		t.Fatalf("episodio de saturación: %v", a)
	}
	if tab["servidor"].(map[string]any)["rechazadas"] != float64(1) {
		t.Fatalf("rechazadas del rango: %v", tab["servidor"])
	}
	// Las peticiones y los tiempos van en la serie por minuto.
	if p := puntoEn(tab, x.reloj.Truncate(time.Minute)); p == nil || p["peticionesMin"] != float64(2) || p["latenciaMediaMs"].(float64) < 250 || p["latenciaP95Ms"] == nil {
		t.Fatalf("punto de uso de la serie: %v", p)
	}
	// Disco: el umbral es instantáneo (depende del disco de la máquina de pruebas).
	disco := tab["servidor"].(map[string]any)["disco"].(map[string]any)
	lleno := 100*disco["usadoMb"].(float64)/disco["totalMb"].(float64) >= 50
	um = x.exigir(x.api(x.admin, http.MethodGet, "/servidor-ia/umbrales", nil), http.StatusOK)
	x.exigir(x.api(x.admin, http.MethodPost, "/servidor-ia/umbrales/actualizar", map[string]any{"version": um.cuerpo["version"], "vramPct": 90, "vramMinutos": 1,
		"discoPct": 50, "temperaturaC": 80, "temperaturaMinutos": 1, "saturacionRechazos": 1, "saturacionMinutos": 1}), http.StatusOK)
	x.medir(ruta, normal)
	if a := avisoIA(x.tablero("1h"), "disco"); (a != nil && a["activo"] == true) != lleno {
		t.Fatalf("episodio de disco (lleno=%v): %v", lleno, a)
	}

	// B6: quien solo ve el uso de su empresa no recibe el detalle del estado, ni el servidor, ni las
	// series de GPU, ni el uso de otras empresas.
	x.sql(`UPDATE mensajeria.voz_motores SET detalle = 'detalle interno del servidor' WHERE id = $1`, x.motor)
	if d := x.tablero("1h")["estado"].(map[string]any)["detalle"]; d != "detalle interno del servidor" {
		t.Fatalf("quien ve todo sí recibe el detalle: %v", d)
	}
	deEmpresa, err := x.m.ServidorIA.Tablero.Ejecutar(pruebas.ContextoUsuarioEnEmpresa(x.bd, x.agente.id, 1, nucleo.PermServidorIAVerUso), mensajeria.EntradaTableroIA{Rango: "ahora"})
	if err != nil {
		t.Fatal(err)
	}
	crudo, _ := json.Marshal(deEmpresa)
	if deEmpresa.Estado.Detalle != nil || deEmpresa.Servidor != nil || strings.Contains(string(crudo), "detalle interno") || strings.Contains(string(crudo), "RTX") ||
		deEmpresa.Estado.EmpresasActivas != nil || len(deEmpresa.Avisos) != 0 || deEmpresa.Uso == nil || len(deEmpresa.Uso.Empresas) != 1 || deEmpresa.Uso.Empresas[0].EmpresaID != 1 {
		t.Fatalf("tablero de quien solo ve su uso: %s", crudo)
	}
	for _, p := range deEmpresa.Series {
		if p.UsoPct != nil || p.VRAMPct != nil {
			t.Fatalf("las series de GPU no son para el admin de empresa: %+v", p)
		}
	}
	otra, err := x.m.ServidorIA.Tablero.Ejecutar(pruebas.ContextoUsuarioEnEmpresa(x.bd, x.agente.id, 2, nucleo.PermServidorIAVerUso), mensajeria.EntradaTableroIA{Rango: "1h"})
	if err != nil || (otra.Uso != nil && len(otra.Uso.Empresas) != 0 && otra.Uso.Empresas[0].Peticiones != 0) {
		t.Fatalf("otra empresa no ve el uso de la 1: %+v %v", otra.Uso, err)
	}

	// Caído: tres latidos sin respuesta. Las muestras quedan como fallidas y se abre el episodio.
	x.banco.Apagar()
	for range 3 {
		*x.reloj = x.reloj.Add(10 * time.Second)
		x.latir()
	}
	tab = x.tablero("1h")
	if a := avisoIA(tab, "caido"); x.estado() != "no_disponible" || a == nil || a["activo"] != true || a["valor"] != nil ||
		tab["estado"].(map[string]any)["lecturaOk"] != false || tab["servidor"] == nil {
		t.Fatalf("caído: %v %v", a, tab["estado"])
	}

	// Retención y agregado por hora (cada 5 minutos): lo viejo se borra y queda la hora agregada.
	viejo := x.reloj.Add(-8 * 24 * time.Hour)
	muyViejo := x.reloj.Add(-400 * 24 * time.Hour)
	x.sql(`INSERT INTO mensajeria.servidor_ia_muestras (motor_id, tomada_en, ok, uso_pct) VALUES ($1, $2, true, 50)`, x.motor, viejo)
	x.sql(`INSERT INTO mensajeria.servidor_ia_muestras_hora (motor_id, hora, muestras, fallidas) VALUES ($1, $2, 1, 0)`, x.motor, muyViejo.Truncate(time.Hour))
	x.sql(`INSERT INTO mensajeria.servidor_ia_avisos (motor_id, tipo, desde, hasta) VALUES ($1, 'disco', $2, $3)`, x.motor, muyViejo, muyViejo.Add(time.Hour))
	x.sql(`INSERT INTO mensajeria.servidor_ia_uso (motor_id, hora, empresa_id, modelo, peticiones) VALUES ($1, $2, 1, 'modelo-a:latest', 1)`, x.motor, muyViejo.Truncate(time.Minute))
	hora := x.reloj.Truncate(time.Hour)
	*x.reloj = x.reloj.Truncate(5 * time.Minute).Add(5 * time.Minute)
	if !x.reloj.Truncate(time.Hour).Equal(hora) {
		t.Fatalf("la prueba debía quedarse en la misma hora: %s", x.reloj)
	}
	x.latir()
	for tabla, donde := range map[string]string{"servidor_ia_muestras": "tomada_en", "servidor_ia_muestras_hora": "hora", "servidor_ia_avisos": "desde", "servidor_ia_uso": "hora"} {
		if n := x.contar(fmt.Sprintf(`SELECT count(*) FROM mensajeria.%s WHERE motor_id = $1 AND %s < $2`, tabla, donde), x.motor, x.reloj.Add(-7*24*time.Hour)); n != 0 {
			t.Fatalf("retención de %s: quedan %d filas viejas", tabla, n)
		}
	}
	var muestras, fallidas int
	var uso *float32
	if err := x.bd.QueryRow(x.ctx, `SELECT muestras, fallidas, uso_pct FROM mensajeria.servidor_ia_muestras_hora WHERE motor_id = $1 AND hora = $2`, x.motor, hora).Scan(&muestras, &fallidas, &uso); err != nil ||
		muestras < 10 || fallidas != 4 || uso == nil || *uso < 20 || *uso > 70 {
		t.Fatalf("agregado de la hora: %d muestras, %d fallidas, uso %v: %v", muestras, fallidas, uso, err)
	}
	// El rango de 7 días usa el agregado por hora.
	if p := puntoEn(x.tablero("7d"), hora); p == nil || p["usoPct"] == nil || p["peticionesMin"] == nil {
		t.Fatalf("punto de la hora en el rango de 7 días: %v", p)
	}
}

// Lo que devuelve /metricas se revalida entero antes de guardarlo (el servidor no es de confianza).
func TestServidorIAMetricasRevalidadas(t *testing.T) {
	buena := voz.Metricas{EncendidoSeg: 3600}
	buena.CPU.Nucleos, buena.Memoria.UsadoMb, buena.Memoria.TotalMb = 16, 1000, 64000
	buena.GPUs.Lista = []voz.GPU{{Indice: 0, Nombre: "NVIDIA A4000", UsoPct: pf(10), VRAMUsadaMb: pf(100), VRAMTotalMb: pf(16000)}}
	buena.Modelos = []voz.ModeloMetricas{{ID: "modelo-a:latest", TamanoMb: 4000}}
	if !mensajeria.MetricasValidas(buena) {
		t.Fatal("una lectura normal debía valer")
	}
	for nombre, romper := range map[string]func(*voz.Metricas){
		"nombre de GPU con marcado":     func(m *voz.Metricas) { m.GPUs.Lista[0].Nombre = `<img src=x onerror=alert(1)>` },
		"porcentaje imposible":          func(m *voz.Metricas) { m.GPUs.Lista[0].UsoPct = pf(101) },
		"temperatura imposible":         func(m *voz.Metricas) { m.GPUs.Lista[0].TemperaturaC = pf(-5) },
		"memoria usada mayor que total": func(m *voz.Metricas) { m.Memoria.UsadoMb = m.Memoria.TotalMb + 1 },
		"disco negativo":                func(m *voz.Metricas) { m.Disco.UsadoMb = -1 },
		"demasiadas GPU":                func(m *voz.Metricas) { m.GPUs.Lista = make([]voz.GPU, 65) },
		"modelo con nombre raro":        func(m *voz.Metricas) { m.Modelos[0].ID = "modelo a; rm -rf /" },
		"peticiones en curso absurdas":  func(m *voz.Metricas) { m.LLM.EnCurso = 100000 },
		"tiempo encendido negativo":     func(m *voz.Metricas) { m.EncendidoSeg = -1 },
		"estado de GPU fuera de lista":  func(m *voz.Metricas) { m.GPUs.Estado = "raro" },
	} {
		mala := buena
		mala.GPUs.Lista = append([]voz.GPU(nil), buena.GPUs.Lista...)
		mala.Modelos = append([]voz.ModeloMetricas(nil), buena.Modelos...)
		romper(&mala)
		if mensajeria.MetricasValidas(mala) {
			t.Fatalf("%s: debía descartarse", nombre)
		}
	}
}
