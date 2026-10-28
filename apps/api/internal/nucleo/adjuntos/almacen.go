package adjuntos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Almacen guarda el contenido de los adjuntos completos. Las claves las arma el módulo
// (e<empresa>/<entidad>/<uuid>), nunca el cliente.
type Almacen interface {
	// Nombre es el valor de nucleo.adjuntos.almacenamiento: "local" o "s3".
	Nombre() string
	// Guardar escribe el objeto completo (tamano bytes de r). sha256Hex ya está verificado.
	// Reescribir la misma clave con el mismo contenido es válido (reintento tras una caída).
	Guardar(ctx context.Context, clave string, r io.Reader, tamano int64, mime, sha256Hex string) error
	// Abrir devuelve el contenido. ErrNoExiste si no está.
	Abrir(ctx context.Context, clave string) (io.ReadCloser, error)
	// Borrar quita el objeto (no falla si no existe).
	Borrar(ctx context.Context, clave string) error
}

// ErrNoExiste: el objeto no está en el almacenamiento.
var ErrNoExiste = errors.New("adjuntos: el objeto no existe en el almacenamiento")

// claveValida: segmentos [a-z0-9_-] separados por «/», sin «..» ni rutas absolutas.
var claveValida = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(/[a-z0-9][a-z0-9_-]*)*$`)

func validarClave(clave string) error {
	if len(clave) > 200 || !claveValida.MatchString(clave) {
		return fmt.Errorf("adjuntos: clave de almacenamiento inválida %q", clave)
	}
	return nil
}

// Disco guarda los adjuntos en un directorio local (desarrollo y pruebas, o un volumen).
type Disco struct {
	dir string
}

// NuevoDisco usa (y crea) el directorio dir.
func NuevoDisco(dir string) (*Disco, error) {
	if dir == "" {
		return nil, errors.New("adjuntos: falta el directorio del almacenamiento local (IRIS_ADJUNTOS_DIR)")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("adjuntos: crear %s: %w", abs, err)
	}
	return &Disco{dir: abs}, nil
}

// Nombre implementa Almacen.
func (d *Disco) Nombre() string { return "local" }

// ruta resuelve la clave dentro del directorio, sin salirse de él (path traversal).
func (d *Disco) ruta(clave string) (string, error) {
	if err := validarClave(clave); err != nil {
		return "", err
	}
	r := filepath.Join(d.dir, filepath.FromSlash(clave))
	if !strings.HasPrefix(r, d.dir+string(filepath.Separator)) {
		return "", fmt.Errorf("adjuntos: la clave %q sale del directorio", clave)
	}
	return r, nil
}

// Guardar implementa Almacen: escribe en un temporal y lo renombra (atómico).
func (d *Disco) Guardar(_ context.Context, clave string, r io.Reader, tamano int64, _, _ string) error {
	destino, err := d.ruta(clave)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destino), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destino), ".subida-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	n, err := io.Copy(tmp, io.LimitReader(r, tamano+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != tamano {
		return fmt.Errorf("adjuntos: se escribieron %d bytes de %d", n, tamano)
	}
	return os.Rename(tmp.Name(), destino)
}

// Abrir implementa Almacen.
func (d *Disco) Abrir(_ context.Context, clave string) (io.ReadCloser, error) {
	r, err := d.ruta(clave)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(r) //nolint:gosec // ruta validada dentro del directorio
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoExiste
	}
	return f, err
}

// Borrar implementa Almacen.
func (d *Disco) Borrar(_ context.Context, clave string) error {
	r, err := d.ruta(clave)
	if err != nil {
		return err
	}
	if err := os.Remove(r); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
