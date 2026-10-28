#!/bin/sh
# Crea el bucket de adjuntos de Iris en el MinIO del compose (servicio minio-init). Idempotente.
# Espera a que MinIO acepte credenciales (hasta ~60 s) en lugar de depender de un healthcheck.
set -eu
intentos=0
until mc alias set local http://minio:9000 "$IRIS_S3_ACCESS_KEY" "$IRIS_S3_SECRET_KEY" >/dev/null 2>&1; do
    intentos=$((intentos + 1))
    if [ "$intentos" -ge 30 ]; then
        echo "MinIO no respondió en http://minio:9000 tras $intentos intentos." >&2
        exit 1
    fi
    sleep 2
done
mc mb --ignore-existing "local/$IRIS_S3_BUCKET"
# Los adjuntos son privados: se sirven con URLs firmadas desde la API.
mc anonymous set none "local/$IRIS_S3_BUCKET" >/dev/null
echo "Bucket $IRIS_S3_BUCKET listo."
