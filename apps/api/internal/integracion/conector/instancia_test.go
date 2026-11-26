package conector

import (
	"context"
	"testing"

	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// 0.14.0 §6.6: integracion.url_instancia normaliza como el canje (esquema y host en minúsculas, sin
// punto final ni puerto por defecto, sin barra final; la ruta tal cual), y ExisteConectorConBase
// reconoce la misma instancia escrita distinto.
func TestInstanciaDeArgosEnSQL(t *testing.T) {
	ctx := context.Background()
	pool := pruebas.Pool(t)
	for crudo, quiere := range map[string]string{
		"HTTPS://Argos.Example.COM.:443/Api/": "https://argos.example.com/Api",
		"http://argos.local:80":               "http://argos.local",
		"https://argos.example.com:8443/x":    "https://argos.example.com:8443/x",
		"https://argos.example.com":           "https://argos.example.com",
	} {
		var got string
		if err := pool.QueryRow(ctx, `SELECT integracion.url_instancia($1)`, crudo).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != quiere {
			t.Errorf("%s → %s; quería %s", crudo, got, quiere)
		}
	}
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO integracion.conectores (sistema, base_url, origen, token_cifrado, secreto_webhook_cifrado, estado)
		VALUES ('argosisp', 'https://instancia.example.com/api', 'enlace_remoto', '\\x00', '\\x00', 'activo') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM integracion.conectores WHERE id = $1`, id) })
	q := conectorbd.New(pool)
	for base, quiere := range map[string]bool{
		"https://INSTANCIA.example.com.:443/api/": true,
		"https://instancia.example.com/api":       true,
		"https://otra.example.com/api":            false,
	} {
		existe, err := q.ExisteConectorConBase(ctx, conectorbd.ExisteConectorConBaseParams{Sistema: "argosisp", BaseUrl: base})
		if err != nil {
			t.Fatal(err)
		}
		if existe != quiere {
			t.Errorf("%s: existe=%v", base, existe)
		}
	}
}
