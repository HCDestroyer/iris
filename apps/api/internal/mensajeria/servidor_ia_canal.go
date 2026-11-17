package mensajeria

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/voz"
)

// Lo que habla con el servidor de IA: enlazar, activar, probar, rotar y el latido. La red va
// siempre fuera de la transacción: un caso lee, se llama al servidor y otro caso guarda.

func ptrSi(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// --- Conectar (el intercambio de enlace, voz.md §4.8 paso 4) --------------------------------------

type enlaceLeido struct {
	conexionMotor
	codigo string
}

type enlaceHecho struct {
	ID        int64               `json:"id"`
	Version   int32               `json:"-"`
	Resultado voz.ResultadoEnlace `json:"-"`
}

// ConectarMotor hace el intercambio de enlace con el código guardado.
type ConectarMotor struct {
	m       *Modulo
	k       *CasosServidorIA
	leer    *auditoria.Caso[int64, enlaceLeido]
	guardar *auditoria.Caso[enlaceHecho, MotorIA]
}

func nuevoConectarMotor(m *Modulo, k *CasosServidorIA) *ConectarMotor {
	return &ConectarMotor{m: m, k: k,
		leer: auditoria.Nuevo(auditoria.Caso[int64, enlaceLeido]{
			Accion: "servidor_ia.conectar", Permiso: nucleo.PermServidorIAAdministrar, SinEmpresa: true, AuditarExito: sinAuditar[enlaceLeido],
			Logica: func(ctx context.Context, t *auditoria.Tx, id int64) (enlaceLeido, error) {
				if err := soloInstalacion(ctx); err != nil {
					return enlaceLeido{}, err
				}
				f, err := mensajeriabd.New(t).MotorIAPorID(ctx, id)
				if errors.Is(err, pgx.ErrNoRows) {
					return enlaceLeido{}, nucleo.NoEncontrado("El servidor de IA no existe")
				}
				if err != nil {
					return enlaceLeido{}, err
				}
				if f.EnlaceCodigo == nil || f.EnlaceVenceEn == nil || !m.ahora().Before(*f.EnlaceVenceEn) {
					return enlaceLeido{}, reglaMotor(http.StatusConflict, "enlace_vencido", "No hay un código de enlace vigente: pide otro con «Enlazar servidor de IA»")
				}
				c, err := m.conexionDeMotor(f)
				if err != nil {
					return enlaceLeido{}, err
				}
				codigo, err := m.descifrarMotor(f.ID, "enlace_codigo", f.EnlaceCodigo)
				if err != nil {
					return enlaceLeido{}, err
				}
				return enlaceLeido{conexionMotor: c, codigo: string(codigo)}, nil
			},
		}),
		guardar: auditoria.Nuevo(auditoria.Caso[enlaceHecho, MotorIA]{
			Accion: "servidor_ia.conectar", Permiso: nucleo.PermServidorIAAdministrar, SinEmpresa: true,
			Logica: func(ctx context.Context, t *auditoria.Tx, in enlaceHecho) (MotorIA, error) {
				t.FijarEmpresa(0)
				t.FijarEntidad("voz_motor", in.ID)
				token, err := m.cifrarMotor(in.ID, "token", in.Resultado.Token)
				if err != nil {
					return MotorIA{}, err
				}
				man := in.Resultado.Manifiesto
				ok, motivo := voz.Compatible(man, m.versionIris(), VozMin)
				if man.Validar() != nil {
					ok, motivo = false, "protocolo"
					man = voz.Manifiesto{}
				}
				f, err := mensajeriabd.New(t).GuardarEnlaceMotorIA(ctx, mensajeriabd.GuardarEnlaceMotorIAParams{ID: in.ID, Version: in.Version, Token: token,
					HuellaTls: &in.Resultado.HuellaServidor, VersionMotor: ptrSi(man.Version), VersionProtocolo: ptrSi(man.Protocolo),
					IrisMin: ptrSi(man.IrisMin), IrisMax: ptrSi(man.IrisMax), Compatible: ok, MotivoIncompatible: ptrSi(motivo)})
				if errors.Is(err, pgx.ErrNoRows) {
					// Reenlazaron mientras se conectaba: este token ya no corresponde al certificado guardado.
					return MotorIA{}, reglaMotor(http.StatusConflict, "enlace_vencido", "El enlace cambió mientras se conectaba: pide otro código")
				}
				if err != nil {
					return MotorIA{}, err
				}
				// Nunca el código ni el token.
				t.Anotar(map[string]any{"huella_tls": in.Resultado.HuellaServidor, "version": man.Version, "compatible": ok})
				return aMotorIA(f, 0), t.EmitirEnEmpresa("voz.motor_enlazado", nil, map[string]any{"motor_id": f.ID, "version": man.Version,
					"protocolo": man.Protocolo, "usuario_id": contexto.ActorDe(ctx).ID})
			},
		})}
}

// Ejecutar hace el intercambio. Iris presenta su certificado de cliente y una prueba del código
// atada a los dos certificados; el token inicial se deriva en los dos lados y no viaja.
func (k *ConectarMotor) Ejecutar(ctx context.Context, id int64) (MotorIA, error) {
	e, err := k.leer.Ejecutar(ctx, id)
	if err != nil {
		return MotorIA{}, err
	}
	cctx, cancelar := context.WithTimeout(ctx, 10*time.Second)
	defer cancelar()
	r, err := voz.Enlazar(cctx, voz.OpcionesEnlace{URL: e.fila.Url, Codigo: e.codigo, Cert: e.cert, Control: controlIA(k.m.cfg.PermitirLoopbackIA)})
	if err != nil {
		// Queda en la auditoría (sin el código): un intento fallido también es un hecho.
		_, _ = k.k.conectarFallido.Ejecutar(ctx, id)
		return MotorIA{}, motorNoResponde()
	}
	return k.guardar.Ejecutar(ctx, enlaceHecho{ID: id, Version: e.fila.Version, Resultado: r})
}

// --- Canal vivo -----------------------------------------------------------------------------------

// canalVivo: las credenciales con las que el servidor contesta ahora.
type canalVivo struct {
	token []byte
	cert  tls.Certificate
	// siguiente: contesta con lo «siguiente»: una rotación quedó a medias (el servidor la aplicó y la
	// respuesta se perdió) y solo falta promoverla en Iris.
	siguiente bool
}

// rechazosParaDescartar: latidos seguidos en los que el servidor debe rechazar lo «siguiente» de
// forma explícita antes de darlo por no entregado.
const rechazosParaDescartar = 3

// esperaRotacionFallida: tras fallar la rotación automática, no se intenta otra hasta pasado esto.
const esperaRotacionFallida = time.Hour

// vidaSiguienteSinAplicar: pasado esto desde que se apartó la rotación (la convivencia más un
// margen), si lo vigente sigue contestando y lo «siguiente» no, la entrega no se aplicó nunca: de
// haberse aplicado, lo vigente ya no valdría. Se descarta aunque no haya habido un rechazo explícito.
const vidaSiguienteSinAplicar = convivenciaRotacion + time.Hour

func (m *Modulo) probarCanal(ctx context.Context, c conexionMotor, cert tls.Certificate, token []byte) error {
	cli, err := m.clienteCon(c, cert, token)
	if err != nil {
		return err
	}
	defer cli.Cerrar()
	cctx, cancelar := context.WithTimeout(ctx, plazoLatidoIA)
	defer cancelar()
	_, err = cli.Salud(cctx)
	return err
}

// canalSiguiente: las credenciales que se estaban entregando (el certificado nuevo, si lo había).
func (c conexionMotor) canalSiguiente() (canalVivo, bool) {
	if len(c.tokenSiguiente) != voz.LargoToken {
		return canalVivo{}, false
	}
	v := canalVivo{token: c.tokenSiguiente, cert: c.cert, siguiente: true}
	if c.certSiguiente != nil {
		v.cert = *c.certSiguiente
	}
	return v, true
}

// canalVivo prueba el canal con lo vigente y, si no contesta, con lo «siguiente».
func (m *Modulo) canalVivo(ctx context.Context, c conexionMotor) (canalVivo, error) {
	err := errors.New("servidor de IA sin credenciales")
	if len(c.token) == voz.LargoToken {
		if err = m.probarCanal(ctx, c, c.cert, c.token); err == nil {
			return canalVivo{token: c.token, cert: c.cert}, nil
		}
	}
	if sig, hay := c.canalSiguiente(); hay {
		if errSig := m.probarCanal(ctx, c, sig.cert, sig.token); errSig == nil {
			return sig, nil
		}
	}
	return canalVivo{}, err
}

// plazoRotacionIA: lo que dura apartada una rotación (más que entregarla y comprobarla). Mientras
// tanto nadie más rota ni termina esa rotación: ni otra persona, ni otra réplica, ni el latido.
const plazoRotacionIA = 30 * time.Second

func motorDesactivado() *nucleo.Problema {
	return reglaMotor(http.StatusConflict, ErrIAServidorDesactivado, "El servidor de IA está desactivado: actívalo de nuevo o enlázalo otra vez")
}

func motorIncompatible(motivo *string) *nucleo.Problema {
	detalle := "La versión del servidor de IA y la de Iris no son compatibles"
	switch derefTexto(motivo) {
	case "iris_antiguo":
		detalle += ": hay que actualizar Iris"
	case "iris_nuevo", "paquete_antiguo":
		detalle += ": hay que actualizar el servidor de IA (iris-voz upgrade)"
	case "protocolo":
		detalle += ": hablan protocolos distintos; actualiza el más antiguo de los dos"
	}
	p := reglaMotor(http.StatusConflict, "incompatible", detalle)
	if m := derefTexto(motivo); m != "" {
		p.Errors = []nucleo.ErrorCampo{{Field: "paquete.motivoIncompatible", Message: detalle, Rule: m}}
	}
	return p
}

func rotacionEnCurso() *nucleo.Problema {
	return nucleo.Conflicto("rotacion_en_curso", "Hay otra rotación de credenciales en curso: vuelve a intentarlo en unos segundos")
}

// rotacionEnVuelo: otra rotación está apartada y todavía dentro de su plazo.
func (m *Modulo) rotacionEnVuelo(f mensajeriabd.MensajeriaVozMotor) bool {
	return f.RotacionDesde != nil && m.ahora().Sub(*f.RotacionDesde) < plazoRotacionIA
}

// credencialEntregada: el servidor ya tiene lo «siguiente»; se promueve en Iris. No lleva ningún
// secreto: el token y la clave del certificado nuevo ya están guardados (cifrados) en la fila.
type credencialEntregada struct {
	ID      int64  `json:"id"`
	Origen  string `json:"origen"`
	Activar bool   `json:"activar"`
}

// entregarCredencial genera un token aleatorio (y, si se pide, un certificado de cliente nuevo), lo
// guarda como «siguiente» ANTES de entregarlo (si la respuesta se pierde, Iris reintenta con lo
// guardado en vez de quedarse sin canal) y lo entrega firmando con el canal vigente.
func (m *Modulo) entregarCredencial(ctx context.Context, k *CasosServidorIA, c conexionMotor, vigente canalVivo, convivencia time.Duration, renovarCert bool) error {
	nuevo, err := voz.NuevoToken()
	if err != nil {
		return err
	}
	fijar := entradaFijarSiguiente{ID: c.fila.ID, Version: c.fila.Version, Token: nuevo}
	huella, sig := "", conexionMotor{fila: c.fila, cert: c.cert, tokenSiguiente: nuevo}
	if renovarCert {
		cert, clave, err := voz.GenerarCertificado("iris", vidaCertCliente)
		if err != nil {
			return err
		}
		if huella, _, err = voz.HuellaDePEM(cert); err != nil {
			return err
		}
		cifrada, err := m.cifrarMotor(c.fila.ID, "cert_cliente_clave", clave)
		if err != nil {
			return err
		}
		par, err := tls.X509KeyPair(cert, clave)
		if err != nil {
			return err
		}
		pem, vence := string(cert), m.ahora().Add(vidaCertCliente)
		fijar.Cert, fijar.CertClave, fijar.CertVence, sig.certSiguiente = &pem, cifrada, &vence, &par
	}
	if _, err := k.fijarSiguiente.Ejecutar(ctx, fijar); err != nil {
		return err
	}
	cli, err := m.clienteCon(c, vigente.cert, vigente.token)
	if err != nil {
		return err
	}
	defer cli.Cerrar()
	cctx, cancelar := context.WithTimeout(ctx, 5*time.Second)
	defer cancelar()
	err = cli.Credencial(cctx, voz.PeticionCredencial{Token: nuevo, HuellaCliente: huella, ConvivenciaSeg: int(convivencia.Seconds())})
	if err == nil {
		return nil
	}
	// ¿La aplicó y se perdió la respuesta? Si el servidor ya contesta con lo nuevo, está entregada.
	if v, hay := sig.canalSiguiente(); hay && m.probarCanal(ctx, c, v.cert, v.token) == nil {
		return nil
	}
	return motorNoResponde()
}

// --- Activar --------------------------------------------------------------------------------------

// ActivarMotor: el administrador ya comparó la huella. Lo primero que hace Iris es rotar a un token
// aleatorio, sin convivencia: el derivado del código deja de valer en ese momento.
type ActivarMotor struct {
	m       *Modulo
	k       *CasosServidorIA
	guardar *auditoria.Caso[credencialEntregada, MotorIA]
}

func nuevoActivarMotor(m *Modulo, k *CasosServidorIA) *ActivarMotor {
	return &ActivarMotor{m: m, k: k, guardar: nuevoGuardarCredencial(m, "servidor_ia.activar")}
}

// nuevoGuardarCredencial: promueve lo «siguiente». Siempre va detrás de k.admin (personas) o del
// latido (sistema), que son los que autorizan.
func nuevoGuardarCredencial(m *Modulo, accion string) *auditoria.Caso[credencialEntregada, MotorIA] {
	return auditoria.Nuevo(auditoria.Caso[credencialEntregada, MotorIA]{
		Accion: accion, Acceso: auditoria.Autenticado, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in credencialEntregada) (MotorIA, error) {
			t.FijarEmpresa(0)
			t.FijarEntidad("voz_motor", in.ID)
			q := mensajeriabd.New(t)
			antes, err := q.MotorIAParaActualizar(ctx, in.ID)
			if err != nil {
				return MotorIA{}, err
			}
			f, err := q.PromoverTokenMotorIA(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return MotorIA{}, rotacionEnCurso()
			}
			if err != nil {
				return MotorIA{}, err
			}
			if in.Activar {
				// Queda no_disponible hasta el primer latido, que llega enseguida y emite el cambio de
				// estado real (aquí no: todavía no se sabe nada del servidor).
				if f, err = q.ActivarMotorIA(ctx, mensajeriabd.ActivarMotorIAParams{ID: in.ID, Estado: MotorNoDisponible}); err != nil {
					return MotorIA{}, err
				}
				if err := t.EmitirEnEmpresa("voz.motor_activado", nil, map[string]any{"motor_id": f.ID, "usuario_id": contexto.ActorDe(ctx).ID, "version": f.VersionMotor}); err != nil {
					return MotorIA{}, err
				}
			}
			t.Anotar(map[string]any{"origen": in.Origen, "cert_cliente_renovado": antes.CertClienteSiguiente != nil})
			// Nunca el token.
			return aMotorIA(f, 0), t.EmitirEnEmpresa("voz.motor_token_rotado", nil, map[string]any{"motor_id": f.ID, "origen": in.Origen})
		},
	})
}

// EntradaActivarMotor es POST /voz/motores/{id}/activar.
type EntradaActivarMotor struct {
	ID        int64  `json:"-"`
	HuellaTLS string `json:"huellaTls" minLength:"16" maxLength:"128" doc:"La huella que el administrador comparó con la del instalador (la que devolvió «Conectar»). Si el servidor enlazado ya no es ese, 409 huella_cambiada"`
}

// Ejecutar activa el servidor. La huella de la entrada liga la activación a lo que el administrador
// comparó: si entre «Conectar» y «Activar» alguien enlazó otro servidor, no se activa a ciegas.
func (a *ActivarMotor) Ejecutar(ctx context.Context, in EntradaActivarMotor) (MotorIA, error) {
	id := in.ID
	c, err := a.k.admin.Ejecutar(ctx, id)
	if err != nil {
		return MotorIA{}, err
	}
	f := c.fila
	switch {
	case f.EnlazadoEn == nil || len(c.token) != voz.LargoToken:
		return MotorIA{}, reglaMotor(http.StatusConflict, "motor_sin_enlazar", "Primero hay que enlazar el servidor con el código y «Conectar»")
	case f.HuellaTls == nil || subtle.ConstantTimeCompare([]byte(*f.HuellaTls), []byte(in.HuellaTLS)) != 1:
		return MotorIA{}, reglaMotor(http.StatusConflict, "huella_cambiada",
			"El servidor enlazado ya no es el que comparaste: revisa la huella que muestra ahora la tarjeta y vuelve a compararla con la del instalador")
	case f.ActivadoEn != nil && f.Estado != MotorDesactivado:
		return aMotorIA(f, 0), nil
	case f.ActivadoEn == nil && a.m.ahora().Sub(*f.EnlazadoEn) > vidaEnlaceSinActivar:
		return MotorIA{}, reglaMotor(http.StatusConflict, "enlace_vencido", "El enlace caducó sin activarse: enlaza de nuevo con otro código")
	}
	if !f.Compatible {
		// No se activa algo que Iris no va a usar: el motivo dice cuál de los dos hay que actualizar.
		return MotorIA{}, motorIncompatible(f.MotivoIncompatible)
	}
	if voz.VersionDesarrollo(a.m.versionIris()) {
		slog.WarnContext(ctx, "servidor de IA: este Iris es un binario de desarrollo; no se compara su versión con el rango del paquete", "version", a.m.versionIris())
	}
	if a.m.rotacionEnVuelo(f) {
		return MotorIA{}, rotacionEnCurso()
	}
	// Lo primero es rotar (antes de cualquier otra petición al servidor). Solo si un intento anterior
	// dejó algo «siguiente» guardado hay que mirar antes con qué contesta.
	entregado := false
	if _, hay := c.canalSiguiente(); hay {
		v, err := a.m.canalVivo(ctx, c)
		if err != nil {
			return MotorIA{}, motorNoResponde()
		}
		entregado = v.siguiente
	}
	if !entregado {
		if err := a.m.entregarCredencial(ctx, a.k, c, canalVivo{token: c.token, cert: c.cert}, 0, false); err != nil {
			return MotorIA{}, err
		}
	}
	m, err := a.guardar.Ejecutar(ctx, credencialEntregada{ID: id, Origen: "enlace", Activar: true})
	if err != nil {
		return MotorIA{}, err
	}
	// El primer latido, ya con el token aleatorio: deja el estado real en la respuesta.
	if f2, err := a.m.latirMotor(ctx, id); err == nil {
		m = aMotorIA(f2, 0)
	}
	return m, nil
}

// --- Rotar ----------------------------------------------------------------------------------------

// EntradaRotarMotor es POST /voz/motores/{id}/rotar-token.
type EntradaRotarMotor struct {
	ID      int64 `json:"-"`
	Revocar bool  `json:"revocar,omitempty" doc:"«Revocar ahora»: las credenciales anteriores dejan de valer en el acto (sin convivencia)"`
}

// RotarMotor cambia el token (y el certificado de cliente si vence pronto o se revoca).
type RotarMotor struct {
	m       *Modulo
	k       *CasosServidorIA
	guardar *auditoria.Caso[credencialEntregada, MotorIA]
}

func nuevoRotarMotor(m *Modulo, k *CasosServidorIA) *RotarMotor {
	return &RotarMotor{m: m, k: k, guardar: nuevoGuardarCredencial(m, "servidor_ia.rotar")}
}

// Ejecutar rota desde el panel.
func (r *RotarMotor) Ejecutar(ctx context.Context, in EntradaRotarMotor) (MotorIA, error) {
	c, err := r.k.admin.Ejecutar(ctx, in.ID)
	if err != nil {
		return MotorIA{}, err
	}
	return r.rotar(ctx, c, "manual", in.Revocar)
}

func (r *RotarMotor) rotar(ctx context.Context, c conexionMotor, origen string, revocar bool) (MotorIA, error) {
	if c.fila.ActivadoEn == nil || len(c.token) != voz.LargoToken {
		return MotorIA{}, reglaMotor(http.StatusConflict, "motor_sin_enlazar", "El servidor de IA no está activado")
	}
	if c.fila.Estado == MotorDesactivado {
		return MotorIA{}, motorDesactivado()
	}
	if r.m.rotacionEnVuelo(c.fila) {
		return MotorIA{}, rotacionEnCurso()
	}
	vivo, err := r.m.canalVivo(ctx, c)
	if err != nil {
		// ¿No contesta, o es que otra rotación cambió las credenciales mientras se probaba con las
		// que se leyeron? Lo segundo no es una falla del servidor.
		if c2, errL := r.k.leer.Ejecutar(ctx, c.fila.ID); errL == nil && (c2.fila.Version != c.fila.Version || r.m.rotacionEnVuelo(c2.fila)) {
			return MotorIA{}, rotacionEnCurso()
		}
		return MotorIA{}, motorNoResponde()
	}
	if vivo.siguiente {
		// Una rotación anterior llegó al servidor y no se terminó en Iris: primero se termina esa.
		m, err := r.guardar.Ejecutar(ctx, credencialEntregada{ID: c.fila.ID, Origen: origen})
		if err != nil || !revocar {
			return m, err
		}
		if c, err = r.k.leer.Ejecutar(ctx, c.fila.ID); err != nil {
			return MotorIA{}, err
		}
		vivo = canalVivo{token: c.token, cert: c.cert}
	}
	convivencia := convivenciaRotacion
	if revocar {
		convivencia = 0
	}
	// El certificado de cliente se renueva en la misma operación si vence pronto o si se revoca
	// (token y clave privada caen juntos).
	renovar := revocar || c.fila.CertClienteVenceEn == nil || c.fila.CertClienteVenceEn.Sub(r.m.ahora()) < margenCertCliente
	if err := r.m.entregarCredencial(ctx, r.k, c, vivo, convivencia, renovar); err != nil {
		return MotorIA{}, err
	}
	return r.guardar.Ejecutar(ctx, credencialEntregada{ID: c.fila.ID, Origen: origen})
}

// --- Probar ---------------------------------------------------------------------------------------

// ProbarMotor: «Probar» de la tarjeta. No cuenta para los topes.
type ProbarMotor struct {
	m       *Modulo
	k       *CasosServidorIA
	guardar *auditoria.Caso[pruebaHecha, MotorIA]
}

type pruebaHecha struct {
	ID     int64
	Prueba PruebaMotor
}

func nuevoProbarMotor(m *Modulo, k *CasosServidorIA) *ProbarMotor {
	return &ProbarMotor{m: m, k: k, guardar: auditoria.Nuevo(auditoria.Caso[pruebaHecha, MotorIA]{
		Accion: "servidor_ia.probar", Permiso: nucleo.PermServidorIAAdministrar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in pruebaHecha) (MotorIA, error) {
			t.FijarEmpresa(0)
			t.FijarEntidad("voz_motor", in.ID)
			b, err := json.Marshal(in.Prueba)
			if err != nil {
				return MotorIA{}, err
			}
			f, err := mensajeriabd.New(t).GuardarPruebaMotorIA(ctx, mensajeriabd.GuardarPruebaMotorIAParams{ID: in.ID, UltimaPrueba: b})
			if err != nil {
				return MotorIA{}, err
			}
			t.Anotar(map[string]any{"estado": in.Prueba.Estado})
			return aMotorIA(f, 0), nil
		},
	})}
}

// Ejecutar prueba el canal, la compatibilidad y el modelo con una frase fija de ida y vuelta.
func (p *ProbarMotor) Ejecutar(ctx context.Context, id int64) (MotorIA, error) {
	c, err := p.k.admin.Ejecutar(ctx, id)
	if err != nil {
		return MotorIA{}, err
	}
	if c.fila.EnlazadoEn == nil || c.fila.ActivadoEn == nil {
		return MotorIA{}, reglaMotor(http.StatusConflict, "motor_sin_enlazar", "El servidor de IA no está activado")
	}
	if c.fila.Estado == MotorDesactivado {
		return MotorIA{}, motorDesactivado()
	}
	pr := PruebaMotor{Estado: "ok", ProbadoEn: p.m.ahora(), Pasos: []PasoPrueba{}}
	paso := func(codigo string, inicio time.Time, err error, detalle string) bool {
		s := PasoPrueba{Codigo: codigo, Estado: "ok", Ms: time.Since(inicio).Milliseconds(), Detalle: detalle}
		if err != nil {
			// Un solo texto por paso: sin direcciones, sin el error crudo de la red.
			s.Estado, s.Detalle, pr.Estado = "error", detalle, "error"
		}
		pr.Pasos = append(pr.Pasos, s)
		return err == nil
	}
	omitido := func(codigo string) {
		pr.Pasos = append(pr.Pasos, PasoPrueba{Codigo: codigo, Estado: "omitido", Detalle: "Esta instalación no lo trae"})
	}
	inicio := time.Now()
	vivo, err := p.m.canalVivo(ctx, c)
	if !paso("conexion", inicio, err, map[bool]string{true: "TLS, huella y firma correctas", false: "No se pudo hablar con el servidor de IA"}[err == nil]) {
		return p.guardar.Ejecutar(ctx, pruebaHecha{ID: id, Prueba: pr})
	}
	cli, err := p.m.clienteCon(c, vivo.cert, vivo.token)
	if err != nil {
		return MotorIA{}, err
	}
	defer cli.Cerrar()
	cctx, cancelar := context.WithTimeout(ctx, p.m.cfg.PlazoPruebaIA)
	defer cancelar()

	inicio = time.Now()
	v, err := cli.Version(cctx)
	compatible, motivo := false, "sin_respuesta"
	if err == nil {
		compatible, motivo = voz.Compatible(v.Manifiesto, p.m.versionIris(), VozMin)
	}
	if !compatible && err == nil {
		err = errors.New(motivo)
	}
	paso("compatibilidad", inicio, err, map[bool]string{true: "Versiones compatibles", false: "Las versiones no son compatibles: " + motivo}[err == nil])

	if err == nil && v.GPU.Estado != voz.EstadoOmitido && voz.ReNombreGPU.MatchString(v.GPU.Modelo) {
		pr.Pasos = append(pr.Pasos, PasoPrueba{Codigo: "gpu", Estado: "ok", Detalle: v.GPU.Modelo})
	} else {
		omitido("gpu")
	}
	omitido("stt")
	omitido("tts")

	inicio = time.Now()
	modelos := catalogoValido(v.Catalogo.LLM)
	err = errors.New("sin modelos")
	if len(modelos) > 0 && compatible {
		cuerpo, _ := json.Marshal(map[string]any{"model": modelos[0], "max_tokens": 8, "stream": false,
			"messages": []map[string]string{{"role": "user", "content": "Responde solo: ok"}}})
		_, err = cli.Hacer(cctx, http.MethodPost, voz.RutaChat, cuerpo, voz.TopeRespuestaLLM)
	}
	paso("llm", inicio, err, map[bool]string{true: "El modelo respondió", false: "El modelo no respondió (o no hay ningún modelo permitido)"}[err == nil])
	omitido("medios")

	inicio = time.Now()
	s, err := cli.Salud(cctx)
	if err == nil && s.LLM.MaxSimultaneas < 1 {
		err = errors.New("sin capacidad")
	}
	paso("capacidad", inicio, err, map[bool]string{true: "Capacidad declarada", false: "El servidor no declara capacidad"}[err == nil])
	return p.guardar.Ejecutar(ctx, pruebaHecha{ID: id, Prueba: pr})
}

// catalogoValido: los ids de modelo que declara el servidor, revalidados por Iris (el servidor no
// es de confianza): forma, longitud y cantidad.
func catalogoValido(ms []voz.ModeloCatalogo) []string {
	out := []string{}
	for _, x := range ms {
		if len(out) >= 256 {
			break
		}
		if voz.ReNombreModelo.MatchString(x.ID) && len(x.ID) <= 128 {
			out = append(out, x.ID)
		}
	}
	return out
}

// --- Latido ---------------------------------------------------------------------------------------

type resultadoLatido struct {
	ID      int64
	Salud   *voz.Salud
	Version *voz.Version
	Vista   string // huella que presentó el servidor en este latido
	// Metricas: la lectura de /metricas de este latido (nil si no se pudo leer).
	Metricas *voz.Metricas
	Confirmo string // huella siguiente que Iris acaba de confirmar
}

func nuevosCasosLatido(m *Modulo, k *CasosServidorIA) {
	k.motorActual = auditoria.Nuevo(auditoria.Caso[struct{}, *mensajeriabd.MensajeriaVozMotor]{
		Accion: "servidor_ia.leer", Acceso: auditoria.Autenticado, SinEmpresa: true, AuditarExito: sinAuditar[*mensajeriabd.MensajeriaVozMotor],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (*mensajeriabd.MensajeriaVozMotor, error) {
			f, err := mensajeriabd.New(t).MotorIA(ctx)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			return &f, err
		},
	})
	k.mantener = auditoria.Nuevo(auditoria.Caso[struct{}, struct{}]{
		Accion: "servidor_ia.mantener", Acceso: auditoria.Autenticado, SinEmpresa: true, AuditarExito: sinAuditar[struct{}],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (struct{}, error) {
			q := mensajeriabd.New(t)
			// El código de enlace no se queda guardado después de vencer.
			if _, err := q.BorrarCodigosEnlaceVencidos(ctx, ptrT(m.ahora())); err != nil {
				return struct{}{}, err
			}
			if _, err := q.BorrarPlazasVencidasIA(ctx, m.ahora()); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, m.mantenerMuestras(ctx, q)
		},
	})
	k.latido = auditoria.Nuevo(auditoria.Caso[resultadoLatido, mensajeriabd.MensajeriaVozMotor]{
		Accion: "servidor_ia.latido", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: sinAuditar[mensajeriabd.MensajeriaVozMotor],
		Logica: func(ctx context.Context, t *auditoria.Tx, in resultadoLatido) (mensajeriabd.MensajeriaVozMotor, error) {
			t.FijarEmpresa(0)
			q := mensajeriabd.New(t)
			f, err := q.MotorIAParaActualizar(ctx, in.ID)
			if err != nil {
				return f, err
			}
			if f.ActivadoEn == nil || f.Estado == MotorDesactivado {
				return f, nil
			}
			de := f.Estado
			p := mensajeriabd.GuardarLatidoMotorIAParams{ID: f.ID, Estado: f.Estado, Detalle: f.Detalle, FallosSeguidos: f.FallosSeguidos,
				Compatible: f.Compatible, MotivoIncompatible: f.MotivoIncompatible, MaxSimultaneas: f.MaxSimultaneas, RestringeIp: f.RestringeIp,
				HuellaTlsSiguiente: f.HuellaTlsSiguiente}
			if in.Salud == nil {
				p.FallosSeguidos = f.FallosSeguidos + 1
				if p.FallosSeguidos >= fallosParaCaido {
					p.Estado, p.Detalle = MotorNoDisponible, ptrSi("El servidor de IA no responde")
				}
			} else {
				s := in.Salud
				ahora := m.ahora()
				man := voz.Manifiesto{Version: s.Version, Protocolo: s.Protocolo, IrisMin: s.IrisMin, IrisMax: s.IrisMax}
				ok, motivo := man.Validar() == nil, "protocolo"
				if ok {
					ok, motivo = voz.Compatible(man, m.versionIris(), VozMin)
				}
				p.FallosSeguidos, p.UltimoLatidoEn, p.Compatible, p.MotivoIncompatible = 0, &ahora, ok, ptrSi(motivo)
				if man.Validar() == nil {
					p.VersionMotor, p.VersionProtocolo, p.IrisMin, p.IrisMax = &man.Version, &man.Protocolo, &man.IrisMin, &man.IrisMax
				}
				// Lo que declara el servidor se revalida antes de guardarlo: no es de confianza.
				cat := catalogoGuardado{LLM: []string{}}
				for _, id := range s.LLM.Modelos {
					if voz.ReNombreModelo.MatchString(id) && len(cat.LLM) < 256 {
						cat.LLM = append(cat.LLM, id)
					}
				}
				if in.Version != nil && in.Version.GPU.Estado != voz.EstadoOmitido && voz.ReNombreGPU.MatchString(in.Version.GPU.Modelo) {
					cat.GPU = &GPUMotor{Modelo: in.Version.GPU.Modelo, VramMb: min(max(in.Version.GPU.VRAMMb, 0), 1<<24)}
				} else if viejo := catalogoDe(f); in.Version == nil {
					cat.GPU = viejo.GPU
				}
				if p.Catalogo, err = json.Marshal(cat); err != nil {
					return f, err
				}
				p.MaxSimultaneas = int32(min(max(s.LLM.MaxSimultaneas, 0), 64)) //nolint:gosec // acotado
				p.RestringeIp = s.RestringirIP
				if ip := s.IPObservada; len(ip) <= 64 && esIP(ip) {
					p.IpObservada = &ip
				}
				switch {
				case !ok:
					p.Estado, p.Detalle = MotorIncompatible, ptrSi("La versión del servidor de IA y la de Iris no son compatibles")
				case s.Estado != voz.EstadoListo || s.LLM.Estado != voz.EstadoListo || len(cat.LLM) == 0 || p.MaxSimultaneas < 1:
					p.Estado, p.Detalle = MotorDegradado, ptrSi("El servidor contesta, pero su modelo de lenguaje no está listo")
				default:
					p.Estado, p.Detalle = MotorListo, nil
				}
				// Renovación del certificado del servidor: la huella nueva solo se acepta anunciada por
				// el canal vigente (respuesta firmada sobre el mTLS con el certificado actual).
				switch {
				case in.Confirmo != "":
					p.HuellaTlsSiguiente = &in.Confirmo
				case s.HuellaSiguiente == "":
					if f.HuellaTlsSiguiente != nil && in.Vista != *f.HuellaTlsSiguiente {
						p.HuellaTlsSiguiente = nil // el servidor canceló la renovación
					}
				}
			}
			if f, err = q.GuardarLatidoMotorIA(ctx, p); err != nil {
				return f, err
			}
			if f.HuellaTlsSiguiente != nil && in.Vista == *f.HuellaTlsSiguiente {
				// El servidor ya usa el certificado nuevo: se suelta la huella anterior.
				if err := q.PromoverHuellaMotorIA(ctx, mensajeriabd.PromoverHuellaMotorIAParams{ID: f.ID, Huella: f.HuellaTlsSiguiente}); err != nil {
					return f, err
				}
				if f, err = q.MotorIAPorID(ctx, f.ID); err != nil {
					return f, err
				}
				slog.InfoContext(ctx, "servidor de IA: huella del certificado renovada por el canal vigente", "motor_id", f.ID)
			}
			if err := emitirEstadoMotor(t, f, de); err != nil {
				return f, err
			}
			// La muestra del tablero (buena o fallida) y los episodios de aviso.
			return f, m.guardarMuestra(ctx, t, q, f, in.Metricas)
		},
	})
}

// latirMotor consulta /salud (y /version si cambió) y guarda el resultado.
func (m *Modulo) latirMotor(ctx context.Context, id int64) (mensajeriabd.MensajeriaVozMotor, error) {
	k := m.ServidorIA
	c, err := k.leer.Ejecutar(ctx, id)
	if err != nil {
		return c.fila, err
	}
	f := c.fila
	if f.ActivadoEn == nil || f.Estado == MotorDesactivado {
		return f, nil
	}
	r := resultadoLatido{ID: id}
	vivo, err := m.canalVivo(ctx, c)
	if sig, hay := c.canalSiguiente(); hay && !m.rotacionEnVuelo(f) {
		// Quedó algo «siguiente» de una rotación que ya no está en vuelo (si lo está, la termina
		// quien la empezó). Se mira qué tiene el servidor:
		var errSig error
		if err != nil || !vivo.siguiente {
			errSig = m.probarCanal(ctx, c, sig.cert, sig.token)
			if m.sondeoSiguienteIA != nil {
				if errPrueba := m.sondeoSiguienteIA(); errPrueba != nil {
					errSig = errPrueba
				}
			}
		}
		if errSig == nil {
			// La aplicó (la respuesta se perdió): se termina sola, sin perder el canal. Con convivencia
			// lo vigente también contesta todavía; se promueve igual, antes de que deje de valer.
			if _, errP := k.Rotar.guardar.Ejecutar(ctx, credencialEntregada{ID: id, Origen: "automatica"}); errP != nil {
				slog.WarnContext(ctx, "servidor de IA: terminar una rotación a medias", "err", errP)
			}
		} else {
			// Lo siguiente no contesta. Solo se descarta si lo vigente sí contesta y el servidor rechaza
			// lo siguiente de forma explícita varios latidos seguidos: entonces la entrega nunca llegó.
			// Un tiempo agotado o un corte de red no dicen nada: se conserva y se vuelve a probar (si la
			// rotación sí se aplicó, descartarla dejaría a Iris con una credencial que caduca).
			n, errN := k.rechazoSiguiente.Ejecutar(ctx, entradaRechazoSiguiente{ID: id, Explicito: err == nil && voz.RechazoExplicito(errSig)})
			// Sin rechazo explícito también hay un final: pasada la convivencia más un margen, con lo
			// vigente contestando, lo siguiente no pudo aplicarse (si no, lo vigente ya habría caducado).
			vencida := err == nil && f.RotacionDesde != nil && m.ahora().Sub(*f.RotacionDesde) > vidaSiguienteSinAplicar
			if errN == nil && (n >= rechazosParaDescartar || vencida) {
				if _, errD := k.descartarSiguiente.Ejecutar(ctx, id); errD != nil {
					slog.WarnContext(ctx, "servidor de IA: descartar una entrega que no llegó", "err", errD)
				}
			}
		}
	}
	token := vivo.token
	if err == nil {
		cli, errCli := m.clienteCon(c, vivo.cert, token)
		if errCli != nil {
			return f, errCli
		}
		defer cli.Cerrar()
		cctx, cancelar := context.WithTimeout(ctx, plazoLatidoIA)
		s, errS := cli.Salud(cctx)
		cancelar()
		if errS == nil {
			r.Salud, r.Vista = &s, cli.HuellaVista()
			// La muestra del tablero, en la misma pasada. Si falla o no valida, cuenta como lectura fallida.
			mctx, cancelarM := context.WithTimeout(ctx, plazoLatidoIA)
			if mt, errM := cli.Metricas(mctx); errM == nil {
				r.Metricas = &mt
			}
			cancelarM()
			if f.VersionMotor == nil || *f.VersionMotor != s.Version || catalogoDe(f).GPU == nil {
				cctx, cancelar := context.WithTimeout(ctx, plazoLatidoIA)
				if v, errV := cli.Version(cctx); errV == nil {
					r.Version = &v
				}
				cancelar()
			}
			// Huella siguiente anunciada por el canal vigente: Iris la fija sola y lo confirma.
			if h := s.HuellaSiguiente; voz.HuellaValida(h) && (f.HuellaTlsSiguiente == nil || *f.HuellaTlsSiguiente != h) && r.Vista != h {
				cctx, cancelar := context.WithTimeout(ctx, plazoLatidoIA)
				con := c
				con.fila.HuellaTlsSiguiente = &h
				if cli2, errC := m.clienteCon(con, vivo.cert, token); errC == nil {
					if cli2.Credencial(cctx, voz.PeticionCredencial{ConfirmaHuellaServidor: h}) == nil {
						r.Confirmo = h
					}
					cli2.Cerrar()
				}
				cancelar()
			}
		}
	}
	return k.latido.Ejecutar(ctx, r)
}

// MantenerServidorIA es el latido (cada 10 s): salud, compatibilidad, huella siguiente, rotación
// automática del token cada 30 días y limpieza de códigos de enlace y plazas vencidas.
func (m *Modulo) MantenerServidorIA(ctx context.Context, bd nucleo.BD) error {
	ctx = ctxSistema(ctx, bd)
	k := m.ServidorIA
	if _, err := k.mantener.Ejecutar(ctx, struct{}{}); err != nil {
		return err
	}
	f, err := k.motorActual.Ejecutar(ctx, struct{}{})
	if err != nil || f == nil {
		return err
	}
	f2, err := m.latirMotor(ctx, f.ID)
	if err != nil {
		return err
	}
	// La rotación automática que falla no se repite en cada latido (un servidor que rechaza
	// /credencial no provoca un bucle de claves nuevas y auditoría): un intento por hora como mucho.
	enEspera := f2.RotacionFallidaEn != nil && m.ahora().Sub(*f2.RotacionFallidaEn) < esperaRotacionFallida
	if motorUtilizable(f2) && f2.TokenRotadoEn != nil && m.ahora().Sub(*f2.TokenRotadoEn) > vidaToken && f2.TokenSiguiente == nil && !enEspera {
		c, err := k.leer.Ejecutar(ctx, f2.ID)
		if err == nil {
			if _, err := k.Rotar.rotar(ctx, c, "automatica", false); err != nil {
				slog.WarnContext(ctx, "servidor de IA: rotación automática; se reintenta en una hora", "err", err)
				_, _ = k.rotacionFallida.Ejecutar(ctx, f2.ID)
			}
		}
	}
	return nil
}
