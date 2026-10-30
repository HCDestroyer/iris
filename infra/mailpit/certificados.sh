#!/usr/bin/env bash
# Certificados de desarrollo para Mailpit con TLS (STARTTLS obligatorio en el SMTP de dev).
#
#   infra/mailpit/certificados.sh            # crea la CA y el certificado si faltan (idempotente)
#   infra/mailpit/certificados.sh --forzar   # los vuelve a crear
#
# Deja en infra/mailpit/certs/ (ignorado por git; nunca se commitea):
#   ca.pem       CA local de dev. La API y el worker confían en ella con IRIS_CORREO_CA.
#   mailpit.pem  certificado del servidor (SAN: mailpit, localhost, 127.0.0.1), firmado por la CA.
#   mailpit.key  su llave (lectura para todos: Mailpit corre con otro uid; solo dev).
# La llave de la CA se borra al terminar: no se pueden emitir más certificados con ella.
# Requiere openssl.
set -euo pipefail

dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/certs"
forzar=0
[ "${1:-}" = "--forzar" ] && forzar=1

if [ "$forzar" = 0 ] && [ -s "$dir/ca.pem" ] && [ -s "$dir/mailpit.pem" ] && [ -s "$dir/mailpit.key" ] \
    && openssl x509 -checkend 86400 -noout -in "$dir/mailpit.pem" >/dev/null 2>&1; then
    echo "Certificados de Mailpit al día en $dir"
    exit 0
fi

command -v openssl >/dev/null || { echo "Falta openssl para generar los certificados de Mailpit." >&2; exit 1; }

mkdir -p "$dir"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Configuración propia (sin -addext ni el openssl.cnf del sistema): con OpenSSL 1.1.1 (Ubuntu 20.04),
# -addext se suma a las extensiones v3_ca del openssl.cnf y la CA queda con extensiones repetidas,
# así que `openssl verify` no la reconoce («unable to get local issuer certificate»).
cat >"$tmp/ca.cnf" <<'CNF'
[req]
prompt = no
distinguished_name = dn
x509_extensions = v3_ca
[dn]
O = Iris dev
CN = Iris CA de desarrollo (Mailpit)
[v3_ca]
basicConstraints = critical,CA:TRUE,pathlen:0
keyUsage = critical,keyCertSign,cRLSign
subjectKeyIdentifier = hash
CNF

openssl req -x509 -config "$tmp/ca.cnf" -newkey rsa:2048 -nodes -days 825 -sha256 \
    -keyout "$tmp/ca.key" -out "$tmp/ca.pem"

cat >"$tmp/csr.cnf" <<'CNF'
[req]
prompt = no
distinguished_name = dn
[dn]
O = Iris dev
CN = mailpit
CNF

openssl req -config "$tmp/csr.cnf" -newkey rsa:2048 -nodes -sha256 \
    -keyout "$tmp/mailpit.key" -out "$tmp/mailpit.csr"

cat >"$tmp/ext.cnf" <<'CNF'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:mailpit,DNS:localhost,IP:127.0.0.1
subjectKeyIdentifier=hash
authorityKeyIdentifier=keyid
CNF

openssl x509 -req -in "$tmp/mailpit.csr" -CA "$tmp/ca.pem" -CAkey "$tmp/ca.key" -CAcreateserial \
    -days 825 -sha256 -extfile "$tmp/ext.cnf" -out "$tmp/mailpit.pem" 2>/dev/null

# Se verifica antes de instalar: si falla, no quedan certificados rotos que la próxima corrida
# daría por buenos.
openssl verify -CAfile "$tmp/ca.pem" "$tmp/mailpit.pem" >/dev/null
install -m 0644 "$tmp/ca.pem" "$dir/ca.pem"
install -m 0644 "$tmp/mailpit.pem" "$dir/mailpit.pem"
install -m 0644 "$tmp/mailpit.key" "$dir/mailpit.key"
echo "Certificados de Mailpit creados en $dir (CA: ca.pem → IRIS_CORREO_CA)"
