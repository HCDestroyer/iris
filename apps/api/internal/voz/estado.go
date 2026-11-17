package voz

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Estado es lo único que la puerta guarda en disco (directorio de estado, modo 0700, archivos
// 0600): credenciales del canal y ajustes del servidor. Nunca datos de clientes.
type Estado struct {
	// Token y TokenAnterior: secreto compartido de la firma (el anterior vale hasta TokenAnteriorVence).
	Token              []byte    `json:"token,omitempty"`
	TokenAnterior      []byte    `json:"tokenAnterior,omitempty"`
	TokenAnteriorVence time.Time `json:"tokenAnteriorVence,omitempty"`
	// HuellaCliente: certificado de cliente de Iris fijado (y el anterior, hasta HuellaAnteriorVence).
	HuellaCliente         string    `json:"huellaCliente,omitempty"`
	HuellaClienteAnterior string    `json:"huellaClienteAnterior,omitempty"`
	HuellaAnteriorVence   time.Time `json:"huellaAnteriorVence,omitempty"`
	// IPObservada: la IP de Iris vista al enlazar. RestringirIP la impone (apagado de fábrica).
	IPObservada  string    `json:"ipObservada,omitempty"`
	RestringirIP bool      `json:"restringirIp,omitempty"`
	EnlazadoEn   time.Time `json:"enlazadoEn,omitempty"`
	// ModelosPermitidos: los modelos que el admin de la instalación deja usar (vacío = ninguno).
	ModelosPermitidos []string `json:"modelosPermitidos,omitempty"`
}

// Enlazado dice si hay un Iris fijado.
func (e Estado) Enlazado() bool { return len(e.Token) == LargoToken && e.HuellaCliente != "" }

func vigente(vence, ahora time.Time) bool { return !vence.IsZero() && ahora.Before(vence) }

// ventana es la ventana de enlace abierta desde el servidor (voz.md §4.8).
type ventana struct {
	Codigo string    `json:"codigo"`
	Vence  time.Time `json:"vence"`
}

// Almacen lee y escribe el directorio de estado. Lo comparten el servidor y las órdenes de consola
// (procesos distintos): cada cambio toma un candado de archivo y escribe con renombrado atómico.
type Almacen struct{ dir string }

// AbrirAlmacen prepara el directorio de estado.
func AbrirAlmacen(dir string) (*Almacen, error) {
	if dir == "" {
		return nil, errors.New("falta el directorio de estado")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Almacen{dir: dir}, nil
}

func (a *Almacen) ruta(n string) string { return filepath.Join(a.dir, n) }

func (a *Almacen) escribir(nombre string, datos []byte) error {
	tmp, err := os.CreateTemp(a.dir, nombre+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(datos); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), a.ruta(nombre))
}

func (a *Almacen) conCandado(f func() error) error {
	c, err := os.OpenFile(a.ruta("candado"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if err := syscall.Flock(int(c.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(c.Fd()), syscall.LOCK_UN) //nolint:errcheck // se suelta al cerrar
	return f()
}

// Leer devuelve el estado (vacío si todavía no existe).
func (a *Almacen) Leer() (Estado, error) {
	var e Estado
	b, err := os.ReadFile(a.ruta("estado.json"))
	if errors.Is(err, os.ErrNotExist) {
		return e, nil
	}
	if err != nil {
		return e, err
	}
	return e, json.Unmarshal(b, &e)
}

// Cambiar aplica f al estado y lo guarda.
func (a *Almacen) Cambiar(f func(*Estado) error) error {
	return a.conCandado(func() error {
		e, err := a.Leer()
		if err != nil {
			return err
		}
		if err := f(&e); err != nil {
			return err
		}
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		return a.escribir("estado.json", b)
	})
}

// AbrirVentana abre la ventana de enlace con ese código durante VidaCodigo.
func (a *Almacen) AbrirVentana(codigo string, ahora time.Time) error {
	if !CodigoValido(codigo) {
		return errors.New("código de enlace inválido")
	}
	b, err := json.Marshal(ventana{Codigo: codigo, Vence: ahora.Add(VidaCodigo)})
	if err != nil {
		return err
	}
	return a.conCandado(func() error { return a.escribir("enlace.json", b) })
}

// Ventana devuelve el código de la ventana abierta; ok es false si no hay o ya venció (y la borra).
func (a *Almacen) Ventana(ahora time.Time) (string, bool) {
	b, err := os.ReadFile(a.ruta("enlace.json"))
	if err != nil {
		return "", false
	}
	var v ventana
	if json.Unmarshal(b, &v) != nil || !CodigoValido(v.Codigo) || !ahora.Before(v.Vence) {
		_ = os.Remove(a.ruta("enlace.json"))
		return "", false
	}
	return v.Codigo, true
}

// CerrarVentana cierra la ventana de enlace.
func (a *Almacen) CerrarVentana() error {
	err := os.Remove(a.ruta("enlace.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Certificado y clave viven en UN archivo PEM (los dos bloques): así cambiar de certificado es un
// solo renombrado atómico y nunca se ve un certificado con la clave de otro.
const (
	archivoPar          = "servidor.pem"
	archivoParSiguiente = "servidor-siguiente.pem"
)

func (a *Almacen) cargar(nombre string) (*tls.Certificate, error) {
	b, err := os.ReadFile(a.ruta(nombre))
	if err != nil {
		return nil, err
	}
	par, err := tls.X509KeyPair(b, b)
	if err != nil {
		return nil, err
	}
	return &par, nil
}

// VidaCertificado: vida del certificado del servidor.
const VidaCertificado = 2 * 365 * 24 * time.Hour

// AsegurarCertificado genera el certificado del servidor si no existe y devuelve su huella.
func (a *Almacen) AsegurarCertificado(nombre string) (string, error) {
	var huella string
	err := a.conCandado(func() error {
		if c, err := a.cargar(archivoPar); err == nil {
			huella = huellaDeTLS(c)
			return nil
		}
		cert, clave, err := GenerarCertificado(nombre, VidaCertificado)
		if err != nil {
			return err
		}
		if err := a.escribir(archivoPar, append(cert, clave...)); err != nil {
			return err
		}
		huella, _, err = HuellaDePEM(cert)
		return err
	})
	return huella, err
}

// Certificado devuelve el certificado vigente del servidor.
func (a *Almacen) Certificado() (*tls.Certificate, error) { return a.cargar(archivoPar) }

// HuellaSiguiente devuelve la huella del certificado siguiente, si hay una renovación pendiente.
func (a *Almacen) HuellaSiguiente() string {
	c, err := a.cargar(archivoParSiguiente)
	if err != nil {
		return ""
	}
	return huellaDeTLS(c)
}

// PrepararSiguiente genera el certificado siguiente (voz.md §4.10): la puerta lo anuncia por el
// canal vigente y solo lo usa cuando Iris confirma que lo fijó.
func (a *Almacen) PrepararSiguiente(nombre string) (string, error) {
	var huella string
	err := a.conCandado(func() error {
		cert, clave, err := GenerarCertificado(nombre, VidaCertificado)
		if err != nil {
			return err
		}
		if err := a.escribir(archivoParSiguiente, append(cert, clave...)); err != nil {
			return err
		}
		huella, _, err = HuellaDePEM(cert)
		return err
	})
	return huella, err
}

// PromoverSiguiente pasa el certificado siguiente a vigente si su huella es la confirmada.
func (a *Almacen) PromoverSiguiente(huella string) error {
	return a.conCandado(func() error {
		c, err := a.cargar(archivoParSiguiente)
		if err != nil || huellaDeTLS(c) != huella {
			return errors.New("no hay un certificado siguiente con esa huella")
		}
		return os.Rename(a.ruta(archivoParSiguiente), a.ruta(archivoPar))
	})
}

// Desenlazar borra token y huella fijada: la puerta no acepta nada hasta un enlace nuevo con código.
func (a *Almacen) Desenlazar() error {
	return a.Cambiar(func(e *Estado) error {
		e.Token, e.TokenAnterior, e.TokenAnteriorVence = nil, nil, time.Time{}
		e.HuellaCliente, e.HuellaClienteAnterior, e.HuellaAnteriorVence = "", "", time.Time{}
		e.EnlazadoEn = time.Time{}
		return nil
	})
}

// RegenerarCertificado tira el certificado del servidor y crea otro. Sin canal vigente no hay cómo
// anunciar la huella nueva, así que también desenlaza: hay que enlazar de nuevo con código.
func (a *Almacen) RegenerarCertificado(nombre string) (string, error) {
	if err := a.Desenlazar(); err != nil {
		return "", err
	}
	for _, n := range []string{archivoPar, archivoParSiguiente} {
		if err := os.Remove(a.ruta(n)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return a.AsegurarCertificado(nombre)
}

// VentanaAbierta dice si hay una ventana de enlace abierta y hasta cuándo.
func (a *Almacen) VentanaAbierta(ahora time.Time) (time.Time, bool) {
	if _, ok := a.Ventana(ahora); !ok {
		return time.Time{}, false
	}
	b, err := os.ReadFile(a.ruta("enlace.json"))
	var v ventana
	if err != nil || json.Unmarshal(b, &v) != nil {
		return time.Time{}, false
	}
	return v.Vence, true
}
