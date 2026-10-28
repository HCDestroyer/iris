package identidad

import (
	"crypto/subtle"
	"strconv"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

const periodoTOTP = 30

var opcionesTOTP = totp.ValidateOpts{Period: periodoTOTP, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}

// validarTOTP acepta el código del paso actual o de uno vecino (desfase de reloj) y devuelve el
// paso que coincidió, para rechazar que se reuse.
func validarTOTP(secreto, codigo string, ahora time.Time) (int64, bool) {
	if len(codigo) != 6 {
		return 0, false
	}
	for _, d := range []int{0, -1, 1} {
		t := ahora.Add(time.Duration(d*periodoTOTP) * time.Second)
		esperado, err := totp.GenerateCodeCustom(secreto, t, opcionesTOTP)
		if err == nil && subtle.ConstantTimeCompare([]byte(esperado), []byte(codigo)) == 1 {
			return t.Unix() / periodoTOTP, true
		}
	}
	return 0, false
}

func datosAsociadosTOTP(usuarioID int64) []byte {
	return []byte("usuario:" + strconv.FormatInt(usuarioID, 10) + ":totp")
}

// secretoTOTP descifra el secreto del usuario.
func secretoTOTP(cfg Config, u identidadbd.NucleoUsuario) (string, error) {
	if cfg.Cifrador == nil {
		return "", nucleo.ReglaNegocio("mfa_no_disponible", "MFA no está disponible: falta IRIS_CLAVE_CIFRADO")
	}
	if u.TotpSecreto == nil {
		return "", nucleo.ReglaNegocio("mfa_sin_configurar", "El usuario no tiene MFA configurado")
	}
	plano, err := cfg.Cifrador.Descifrar(u.TotpSecreto, datosAsociadosTOTP(u.ID))
	if err != nil {
		return "", err
	}
	return string(plano), nil
}

func codigoInvalido() *nucleo.Problema {
	return nucleo.NuevoProblema(401, "codigo_invalido", "Código inválido", "El código de verificación no es válido o ya se usó")
}
