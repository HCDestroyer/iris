// voz-puerta es la puerta del Servidor de IA («Iris Voz», contrato voz.md §4): el único proceso que
// escucha en el puerto de control. Termina TLS, verifica mTLS, firma y nonce de toda ruta y reparte
// /llm/ al contenedor de Ollama. No tiene base de datos ni credenciales de Iris y nunca abre
// conexiones hacia Iris.
//
// Órdenes: servir (por defecto), enlazar, estado, huella, desenlazar, renovar-certificado,
// ip restringir|liberar, modelo permitir|retirar|lista|digest, medidor, proxy-descarga, salud, version.
// Las usa `iris-voz` (infra/voz/install-voz.sh) con `docker compose exec`.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/voz"
)

// version queda fijada al construir (-ldflags "-X main.version=…"); manda la del manifiesto.
var version = "0.0.0-dev"

func entorno(nombre, defecto string) string {
	if v := strings.TrimSpace(os.Getenv(nombre)); v != "" {
		return v
	}
	return defecto
}

func entero(nombre string, defecto int) (int, error) {
	v := strings.TrimSpace(os.Getenv(nombre))
	if v == "" {
		return defecto, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s inválido: %s", nombre, v)
	}
	return n, nil
}

func almacen() (*voz.Almacen, error) {
	return voz.AbrirAlmacen(entorno("IRIS_VOZ_ESTADO", "/estado"))
}

func nombreCert() string { return entorno("IRIS_VOZ_NOMBRE", "iris-voz") }

func manifiesto() (voz.Manifiesto, error) {
	var m voz.Manifiesto
	b, err := os.ReadFile(entorno("IRIS_VOZ_MANIFIESTO", "/etc/iris-voz/manifiesto.json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	return m, m.Validar()
}

func main() {
	orden := "servir"
	args := os.Args[1:]
	if len(args) > 0 {
		orden, args = args[0], args[1:]
	}
	if err := ejecutar(orden, args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func ejecutar(orden string, args []string) error {
	switch orden {
	case "servir":
		return servir()
	case "medidor":
		return medidor()
	case "proxy-descarga":
		return proxyDescarga()
	case "salud":
		return salud()
	case "version":
		fmt.Println(version)
		return nil
	}
	alm, err := almacen()
	if err != nil {
		return err
	}
	ahora := time.Now()
	switch orden {
	case "huella":
		h, err := alm.AsegurarCertificado(nombreCert())
		if err == nil {
			fmt.Println(h)
		}
		return err
	case "enlazar":
		// El código llega por la entrada estándar (iris-voz lo lee sin eco); nunca como argumento
		// ni por variable de entorno, que quedarían en el historial y en la lista de procesos.
		if len(args) != 0 {
			return errors.New("el código no se pasa como argumento: se escribe cuando se pide")
		}
		linea, err := bufio.NewReader(io.LimitReader(os.Stdin, 256)).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		codigo := strings.TrimSpace(linea)
		if !voz.CodigoValido(codigo) {
			return errors.New("eso no es un código de enlace (voz-… de 36 caracteres)")
		}
		h, err := alm.AsegurarCertificado(nombreCert())
		if err != nil {
			return err
		}
		if err := alm.AbrirVentana(codigo, ahora); err != nil {
			return err
		}
		fmt.Printf("Ventana de enlace abierta durante %d minutos.\nHuella del certificado de este servidor (compárala con la del panel antes de «Activar»):\n  %s\n",
			int(voz.VidaCodigo.Minutes()), h)
		return nil
	case "estado":
		e, err := alm.Leer()
		if err != nil {
			return err
		}
		h, err := alm.AsegurarCertificado(nombreCert())
		if err != nil {
			return err
		}
		fmt.Println("huella:", h)
		if s := alm.HuellaSiguiente(); s != "" {
			fmt.Println("huella siguiente (pendiente de que Iris la fije):", s)
		}
		fmt.Println("enlazado:", e.Enlazado())
		if e.Enlazado() {
			fmt.Println("enlazado en:", e.EnlazadoEn.Format(time.RFC3339))
			fmt.Println("IP de Iris observada:", e.IPObservada)
		}
		fmt.Println("restringir por IP:", e.RestringirIP)
		fmt.Println("modelos permitidos:", strings.Join(e.ModelosPermitidos, ", "))
		if vence, ok := alm.VentanaAbierta(ahora); ok {
			fmt.Println("ventana de enlace abierta hasta:", vence.Format(time.RFC3339))
		}
		return nil
	case "desenlazar":
		if err := alm.CerrarVentana(); err != nil {
			return err
		}
		if err := alm.Desenlazar(); err != nil {
			return err
		}
		fmt.Println("Desenlazado: este servidor no acepta nada hasta un enlace nuevo con código.")
		return nil
	case "renovar-certificado":
		e, err := alm.Leer()
		if err != nil {
			return err
		}
		if slices.Contains(args, "--sin-canal") || !e.Enlazado() {
			h, err := alm.RegenerarCertificado(nombreCert())
			if err != nil {
				return err
			}
			fmt.Printf("Certificado nuevo y servidor desenlazado. Hay que enlazar de nuevo con código.\nHuella: %s\n", h)
			return nil
		}
		h, err := alm.PrepararSiguiente(nombreCert())
		if err != nil {
			return err
		}
		fmt.Printf("Certificado siguiente preparado. Se anuncia a Iris por el canal vigente y se usa cuando Iris lo confirme.\nHuella siguiente: %s\n", h)
		return nil
	case "ip":
		if len(args) != 1 || (args[0] != "restringir" && args[0] != "liberar") {
			return errors.New("uso: ip restringir|liberar")
		}
		return alm.Cambiar(func(e *voz.Estado) error {
			if args[0] == "restringir" && e.IPObservada == "" {
				return errors.New("todavía no hay una IP observada: enlaza primero")
			}
			e.RestringirIP = args[0] == "restringir"
			fmt.Println("restringir por IP:", e.RestringirIP, e.IPObservada)
			return nil
		})
	case "modelo":
		return modelo(alm, args)
	}
	return fmt.Errorf("orden desconocida: %s", orden)
}

// digestModelo pregunta al contenedor llm el digest de un modelo instalado (para comprobarlo contra
// el manifiesto del paquete después de una descarga).
func digestModelo(nombre string) error {
	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	base := strings.TrimRight(entorno("IRIS_VOZ_OLLAMA_URL", "http://llm:11434"), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tags); err != nil {
		return err
	}
	for _, m := range tags.Models {
		if m.Name == nombre {
			fmt.Println("sha256:" + strings.TrimPrefix(m.Digest, "sha256:"))
			return nil
		}
	}
	return fmt.Errorf("el modelo %s no está instalado", nombre)
}

func modelo(alm *voz.Almacen, args []string) error {
	if len(args) == 2 && args[0] == "digest" && voz.ReNombreModelo.MatchString(args[1]) {
		nombre := args[1]
		if !strings.Contains(nombre, ":") {
			nombre += ":latest"
		}
		return digestModelo(nombre)
	}
	if len(args) == 1 && args[0] == "lista" {
		e, err := alm.Leer()
		if err != nil {
			return err
		}
		for _, m := range e.ModelosPermitidos {
			fmt.Println(m)
		}
		return nil
	}
	if len(args) != 2 || !voz.ReNombreModelo.MatchString(args[1]) {
		return errors.New("uso: modelo permitir|retirar <nombre> | modelo lista")
	}
	nombre := args[1]
	if !strings.Contains(nombre, ":") {
		nombre += ":latest" // Ollama nombra así el modelo sin etiqueta
	}
	return alm.Cambiar(func(e *voz.Estado) error {
		i := slices.Index(e.ModelosPermitidos, nombre)
		switch args[0] {
		case "permitir":
			if i < 0 {
				e.ModelosPermitidos = append(e.ModelosPermitidos, nombre)
			}
		case "retirar":
			if i >= 0 {
				e.ModelosPermitidos = slices.Delete(e.ModelosPermitidos, i, i+1)
			}
		default:
			return errors.New("uso: modelo permitir|retirar <nombre>")
		}
		slices.Sort(e.ModelosPermitidos)
		fmt.Println("modelos permitidos:", strings.Join(e.ModelosPermitidos, ", "))
		return nil
	})
}

func servir() error {
	registro := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	alm, err := almacen()
	if err != nil {
		return err
	}
	man, err := manifiesto()
	if err != nil {
		return fmt.Errorf("manifiesto: %w", err)
	}
	huella, err := alm.AsegurarCertificado(nombreCert())
	if err != nil {
		return err
	}
	simultaneas, err := entero("IRIS_VOZ_LLM_SIMULTANEAS", 0)
	if err != nil {
		return err
	}
	maxTokens, err := entero("IRIS_VOZ_LLM_MAX_TOKENS", 4096)
	if err != nil {
		return err
	}
	puerto, err := entero("IRIS_VOZ_PUERTO", 8443)
	if err != nil {
		return err
	}
	p, err := voz.Nueva(voz.Config{Almacen: alm, Manifiesto: man, OllamaURL: strings.TrimRight(entorno("IRIS_VOZ_OLLAMA_URL", "http://llm:11434"), "/"),
		MedidorRuta: os.Getenv("IRIS_VOZ_MEDIDOR"), DiscoRuta: os.Getenv("IRIS_VOZ_MODELOS_DIR"),
		MaxSimultaneas: simultaneas, MaxTokens: maxTokens, Log: registro})
	if err != nil {
		return err
	}
	l, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(puerto)))
	if err != nil {
		return err
	}
	srv := voz.Servidor(p)
	ctx, parar := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer parar()
	go func() {
		<-ctx.Done()
		cierre, cancelar := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancelar()
		_ = srv.Shutdown(cierre)
	}()
	registro.Info("puerta lista", "version", man.Version, "protocolo", man.Protocolo, "puerto", puerto, "huella", huella)
	if err := voz.Escuchar(srv, l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// medidor: el único proceso que ve la GPU. Sin red ni puertos; deja la lectura en un archivo.
func medidor() error {
	ruta := entorno("IRIS_VOZ_MEDIDOR", "/medidor/gpu.json")
	binario := entorno("IRIS_VOZ_SMI", "nvidia-smi")
	ctx, parar := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer parar()
	tic := time.NewTicker(5 * time.Second)
	defer tic.Stop()
	avisado := false
	for {
		if err := voz.Medir(ctx, binario, ruta, time.Now()); err != nil && !avisado {
			fmt.Fprintln(os.Stderr, "medidor: no se pudo leer la GPU:", err)
			avisado = true
		} else if err == nil {
			avisado = false
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tic.C:
		}
	}
}

// proxy-descarga: la única salida del paquete, solo hacia el registro de modelos.
func proxyDescarga() error {
	hosts := strings.Split(entorno("IRIS_VOZ_DESCARGA_HOSTS", "registry.ollama.ai,.r2.cloudflarestorage.com"), ",")
	srv := &http.Server{Addr: ":3128", Handler: voz.ProxyDescarga{Hosts: hosts}, ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

// salud: comprobación del contenedor (el puerto acepta conexiones; sin certificado no hay más que ver).
func salud() error {
	puerto, err := entero("IRIS_VOZ_PUERTO", 8443)
	if err != nil {
		return err
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(puerto)), 2*time.Second)
	if err != nil {
		return err
	}
	return c.Close()
}
