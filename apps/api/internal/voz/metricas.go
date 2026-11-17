package voz

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Componente es el estado de una pieza del paquete; «omitido» = esta instalación no la trae.
type Componente struct {
	Estado string `json:"estado"`
}

// Estados de un componente y del servidor.
const (
	EstadoListo     = "listo"
	EstadoDegradado = "degradado"
	EstadoOmitido   = "omitido"
	EstadoError     = "error"
)

// SaludLLM es el estado del modelo de lenguaje.
type SaludLLM struct {
	Estado         string   `json:"estado"`
	Modelos        []string `json:"modelos"`
	MaxSimultaneas int      `json:"maxSimultaneas"`
	EnCurso        int      `json:"enCurso"`
}

// Capacidad de llamadas (en la primera entrega, 0).
type Capacidad struct {
	MaxSimultaneas int `json:"maxSimultaneas"`
	EnCurso        int `json:"enCurso"`
}

// Salud es la respuesta de GET /salud (voz.md §4.5).
type Salud struct {
	Estado          string     `json:"estado"`
	Version         string     `json:"version"`
	Protocolo       string     `json:"protocolo"`
	IrisMin         string     `json:"irisMin"`
	IrisMax         string     `json:"irisMax"`
	AceptaNuevas    bool       `json:"aceptaNuevas"`
	Capacidad       Capacidad  `json:"capacidad"`
	STT             Componente `json:"stt"`
	TTS             Componente `json:"tts"`
	UDP             Componente `json:"udp"`
	LLM             SaludLLM   `json:"llm"`
	HuellaSiguiente string     `json:"huellaSiguiente,omitempty"`
	IPObservada     string     `json:"ipObservada"`
	RestringirIP    bool       `json:"restringirIp"`
}

// GPU es la lectura de una tarjeta. Un dato que la tarjeta no da va null.
type GPU struct {
	Indice       int      `json:"indice"`
	Nombre       string   `json:"nombre"`
	UsoPct       *float64 `json:"usoPct"`
	VRAMUsadaMb  *float64 `json:"vramUsadaMb"`
	VRAMTotalMb  *float64 `json:"vramTotalMb"`
	TemperaturaC *float64 `json:"temperaturaC"`
	PotenciaW    *float64 `json:"potenciaW"`
}

// LecturaGPU es lo que deja el contenedor medidor en su archivo.
type LecturaGPU struct {
	TomadoEn time.Time `json:"tomadoEn"`
	GPUs     []GPU     `json:"gpus"`
}

// GPUs es la lista de tarjetas o «omitido».
type GPUs struct {
	Estado string `json:"estado,omitempty"`
	Lista  []GPU  `json:"lista"`
}

// ModeloCatalogo es un modelo del catálogo.
type ModeloCatalogo struct {
	ID string `json:"id"`
}

// Catalogo de lo que el servidor declara.
type Catalogo struct {
	STT []ModeloCatalogo `json:"stt"`
	TTS []ModeloCatalogo `json:"tts"`
	LLM []ModeloCatalogo `json:"llm"`
}

// GPUResumen: la primera tarjeta, para la tarjeta del panel.
type GPUResumen struct {
	Estado string `json:"estado,omitempty"`
	Modelo string `json:"modelo"`
	VRAMMb int64  `json:"vramMb"`
}

// Version es la respuesta de GET /version (voz.md §4.7).
type Version struct {
	Manifiesto
	GPU      GPUResumen `json:"gpu"`
	Catalogo Catalogo   `json:"catalogo"`
}

// ModeloMetricas es un modelo instalado y su estado de carga.
type ModeloMetricas struct {
	ID           string     `json:"id"`
	TamanoMb     int64      `json:"tamanoMb"`
	Permitido    bool       `json:"permitido"`
	Cargado      bool       `json:"cargado"`
	VRAMMb       int64      `json:"vramMb"`
	CargadoHasta *time.Time `json:"cargadoHasta"`
}

// Uso de un recurso en megas.
type Uso struct {
	UsadoMb int64 `json:"usadoMb"`
	TotalMb int64 `json:"totalMb"`
}

// CPU del servidor.
type CPU struct {
	Nucleos int      `json:"nucleos"`
	UsoPct  *float64 `json:"usoPct"`
}

// LLMMetricas: ocupación del modelo (no hay cola).
type LLMMetricas struct {
	EnCurso        int `json:"enCurso"`
	MaxSimultaneas int `json:"maxSimultaneas"`
}

// Metricas es la respuesta de GET /metricas (voz.md §4.12). Nunca lleva prompts, respuestas,
// nombres de clientes ni números: la puerta no los tiene.
type Metricas struct {
	Estado       string           `json:"estado"`
	EncendidoSeg int64            `json:"encendidoSeg"`
	Version      string           `json:"version"`
	TomadoEn     time.Time        `json:"tomadoEn"`
	GPUs         GPUs             `json:"gpus"`
	CPU          CPU              `json:"cpu"`
	Memoria      Uso              `json:"memoria"`
	Disco        Uso              `json:"disco"`
	Modelos      []ModeloMetricas `json:"modelos"`
	LLM          LLMMetricas      `json:"llm"`
}

// ReNombreGPU: forma del nombre de una tarjeta.
var ReNombreGPU = regexp.MustCompile(`^[a-zA-Z0-9 ._()/+-]{1,128}$`)

// vidaLecturaGPU: una lectura más vieja se da por perdida.
const vidaLecturaGPU = 15 * time.Second

// leerGPU lee el archivo del medidor. La puerta no ve la GPU ni ejecuta nada.
func (p *Puerta) leerGPU() ([]GPU, bool) {
	if p.cfg.MedidorRuta == "" {
		return nil, false
	}
	f, err := os.Open(p.cfg.MedidorRuta)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()
	var l LecturaGPU
	if json.NewDecoder(bufio.NewReader(f)).Decode(&l) != nil || p.cfg.Ahora().Sub(l.TomadoEn) > vidaLecturaGPU {
		return nil, false
	}
	var r []GPU
	for _, g := range l.GPUs {
		if len(r) >= 64 || !ReNombreGPU.MatchString(g.Nombre) {
			continue
		}
		r = append(r, g)
	}
	return r, true
}

func (p *Puerta) estadoLLM(ctx context.Context, e Estado) (SaludLLM, []modeloOllama, []modeloOllama) {
	todos, err := p.catalogo(ctx)
	permitidos := utilizables(todos, e)
	s := SaludLLM{Estado: EstadoListo, Modelos: []string{}, EnCurso: p.ocupadas(), MaxSimultaneas: p.maxSimultaneas(permitidos)}
	for _, m := range permitidos {
		s.Modelos = append(s.Modelos, m.Nombre)
	}
	if err != nil || len(permitidos) == 0 {
		// Sin Ollama o sin ningún modelo permitido instalado, el servidor no sirve para nada.
		s.Estado = EstadoError
	}
	return s, todos, permitidos
}

func (p *Puerta) salud(o respuesta, r *http.Request, e Estado) int {
	llm, _, _ := p.estadoLLM(r.Context(), e)
	om := Componente{Estado: EstadoOmitido}
	s := Salud{Estado: EstadoListo, Version: p.cfg.Manifiesto.Version, Protocolo: p.cfg.Manifiesto.Protocolo,
		IrisMin: p.cfg.Manifiesto.IrisMin, IrisMax: p.cfg.Manifiesto.IrisMax, AceptaNuevas: true,
		STT: om, TTS: om, UDP: om, LLM: llm, HuellaSiguiente: p.cfg.Almacen.HuellaSiguiente(),
		IPObservada: e.IPObservada, RestringirIP: e.RestringirIP}
	if llm.Estado != EstadoListo {
		s.Estado = EstadoDegradado
	}
	o.json(http.StatusOK, s)
	return http.StatusOK
}

func (p *Puerta) version(o respuesta, r *http.Request, e Estado) int {
	_, _, permitidos := p.estadoLLM(r.Context(), e)
	v := Version{Manifiesto: p.cfg.Manifiesto, GPU: GPUResumen{Estado: EstadoOmitido},
		Catalogo: Catalogo{STT: []ModeloCatalogo{}, TTS: []ModeloCatalogo{}, LLM: []ModeloCatalogo{}}}
	if gpus, ok := p.leerGPU(); ok && len(gpus) > 0 {
		v.GPU = GPUResumen{Modelo: gpus[0].Nombre}
		for _, g := range gpus {
			if g.VRAMTotalMb != nil {
				v.GPU.VRAMMb += int64(*g.VRAMTotalMb)
			}
		}
	}
	for _, m := range permitidos {
		v.Catalogo.LLM = append(v.Catalogo.LLM, ModeloCatalogo{ID: m.Nombre})
	}
	o.json(http.StatusOK, v)
	return http.StatusOK
}

func (p *Puerta) metricas(o respuesta, r *http.Request, e Estado) int {
	llm, todos, _ := p.estadoLLM(r.Context(), e)
	ahora := p.cfg.Ahora()
	m := Metricas{Estado: EstadoListo, EncendidoSeg: int64(ahora.Sub(p.inicio).Seconds()), Version: p.cfg.Manifiesto.Version,
		TomadoEn: ahora.UTC(), GPUs: GPUs{Estado: EstadoOmitido, Lista: []GPU{}}, Modelos: []ModeloMetricas{},
		LLM: LLMMetricas{EnCurso: llm.EnCurso, MaxSimultaneas: llm.MaxSimultaneas}}
	if llm.Estado != EstadoListo {
		m.Estado = EstadoDegradado
	}
	if gpus, ok := p.leerGPU(); ok {
		m.GPUs = GPUs{Lista: gpus}
		if gpus == nil {
			m.GPUs.Lista = []GPU{}
		}
	}
	for _, x := range todos {
		mm := ModeloMetricas{ID: x.Nombre, TamanoMb: x.Tamano >> 20, Cargado: x.Cargado, VRAMMb: x.VRAM >> 20}
		for _, n := range e.ModelosPermitidos {
			mm.Permitido = mm.Permitido || n == x.Nombre
		}
		if x.Cargado && !x.Hasta.IsZero() {
			h := x.Hasta.UTC()
			mm.CargadoHasta = &h
		}
		m.Modelos = append(m.Modelos, mm)
	}
	m.CPU = p.cpu.leer()
	m.Memoria = leerMemoria()
	m.Disco = leerDisco(p.cfg.DiscoRuta)
	o.json(http.StatusOK, m)
	return http.StatusOK
}

// --- CPU, memoria y disco (de /proc y statfs: sin ejecutar nada) ---------------------------------

type medidorCPU struct {
	mu            sync.Mutex
	total, ocioso uint64
	en            time.Time
	ultimo        *float64
}

func tiemposCPU() (total, ocioso uint64, nucleos int, ok bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, 0, false
	}
	for _, l := range strings.Split(string(b), "\n") {
		c := strings.Fields(l)
		if len(c) < 5 || !strings.HasPrefix(c[0], "cpu") {
			continue
		}
		if c[0] != "cpu" {
			nucleos++
			continue
		}
		for i, v := range c[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			total += n
			if i == 3 || i == 4 { // idle e iowait
				ocioso += n
			}
		}
		ok = true
	}
	return total, ocioso, nucleos, ok
}

// leer devuelve el uso de CPU entre esta lectura y la anterior (null en la primera).
func (m *medidorCPU) leer() CPU {
	total, ocioso, nucleos, ok := tiemposCPU()
	m.mu.Lock()
	defer m.mu.Unlock()
	c := CPU{Nucleos: nucleos, UsoPct: m.ultimo}
	if !ok {
		return CPU{Nucleos: nucleos}
	}
	if !m.en.IsZero() && time.Since(m.en) < time.Second {
		return c
	}
	if !m.en.IsZero() && total > m.total {
		uso := 100 * (1 - float64(ocioso-m.ocioso)/float64(total-m.total))
		uso = min(max(uso, 0), 100)
		m.ultimo, c.UsoPct = &uso, &uso
	}
	m.total, m.ocioso, m.en = total, ocioso, time.Now()
	return c
}

func leerMemoria() Uso {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Uso{}
	}
	var total, libre int64
	for _, l := range strings.Split(string(b), "\n") {
		c := strings.Fields(l)
		if len(c) < 2 {
			continue
		}
		n, _ := strconv.ParseInt(c[1], 10, 64)
		switch c[0] {
		case "MemTotal:":
			total = n
		case "MemAvailable:":
			libre = n
		}
	}
	return Uso{UsadoMb: max(total-libre, 0) >> 10, TotalMb: total >> 10}
}

func leerDisco(ruta string) Uso {
	if ruta == "" {
		return Uso{}
	}
	var s syscall.Statfs_t
	if syscall.Statfs(ruta, &s) != nil || s.Bsize <= 0 {
		return Uso{}
	}
	bloque := uint64(s.Bsize)
	total, libre := s.Blocks*bloque, s.Bavail*bloque
	return Uso{UsadoMb: int64((total - min(libre, total)) >> 20), TotalMb: int64(total >> 20)} //nolint:gosec // megas: cabe en int64
}

// --- Medidor: el único proceso que ve la GPU ------------------------------------------------------

var argumentosSMI = []string{
	"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw",
	"--format=csv,noheader,nounits",
}

func numero(s string) *float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 0 {
		return nil // «[N/A]» y similares
	}
	return &f
}

// InterpretarSMI convierte la salida CSV de nvidia-smi en tarjetas.
func InterpretarSMI(salida []byte) []GPU {
	var r []GPU
	for _, l := range bytes.Split(salida, []byte("\n")) {
		c := strings.Split(string(l), ",")
		if len(c) != 7 || len(r) >= 64 {
			continue
		}
		i, err := strconv.Atoi(strings.TrimSpace(c[0]))
		nombre := strings.TrimSpace(c[1])
		if err != nil || i < 0 || !ReNombreGPU.MatchString(nombre) {
			continue
		}
		r = append(r, GPU{Indice: i, Nombre: nombre, UsoPct: numero(c[2]), VRAMUsadaMb: numero(c[3]),
			VRAMTotalMb: numero(c[4]), TemperaturaC: numero(c[5]), PotenciaW: numero(c[6])})
	}
	return r
}

// Medir ejecuta nvidia-smi (argumentos fijos, sin shell, plazo de 2 s) y deja la lectura en ruta
// con un renombrado atómico. Lo corre el contenedor medidor, que no tiene red ni puertos.
func Medir(ctx context.Context, binario, ruta string, ahora time.Time) error {
	ctx, cancelar := context.WithTimeout(ctx, 2*time.Second)
	defer cancelar()
	salida, err := exec.CommandContext(ctx, binario, argumentosSMI...).Output()
	if err != nil {
		return err
	}
	b, err := json.Marshal(LecturaGPU{TomadoEn: ahora.UTC(), GPUs: InterpretarSMI(salida)})
	if err != nil {
		return err
	}
	tmp := ruta + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil { //nolint:gosec // lo lee la puerta, que corre con otro usuario
		return err
	}
	return os.Rename(tmp, ruta)
}
