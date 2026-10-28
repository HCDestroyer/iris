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

openssl req -x509 -newkey rsa:2048 -nodes -days 825 -sha256 \
    -subj "/O=Iris dev/CN=Iris CA de desarrollo (Mailpit)" \
    -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
    -addext "keyUsage=critical,keyCertSign,cRLSign" \
    -keyout "$tmp/ca.key" -out "$tmp/ca.pem" 2>/dev/null

openssl req -newkey rsa:2048 -nodes -sha256 -subj "/O=Iris dev/CN=mailpit" \
    -keyout "$tmp/mailpit.key" -out "$tmp/mailpit.csr" 2>/dev/null

cat >"$tmp/ext.cnf" <<'CNF'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:mailpit,DNS:localhost,IP:127.0.0.1
CNF

openssl x509 -req -in "$tmp/mailpit.csr" -CA "$tmp/ca.pem" -CAkey "$tmp/ca.key" -CAcreateserial \
    -days 825 -sha256 -extfile "$tmp/ext.cnf" -out "$tmp/mailpit.pem" 2>/dev/null

install -m 0644 "$tmp/ca.pem" "$dir/ca.pem"
install -m 0644 "$tmp/mailpit.pem" "$dir/mailpit.pem"
install -m 0644 "$tmp/mailpit.key" "$dir/mailpit.key"
openssl verify -CAfile "$dir/ca.pem" "$dir/mailpit.pem" >/dev/null
echo "Certificados de Mailpit creados en $dir (CA: ca.pem → IRIS_CORREO_CA)"
