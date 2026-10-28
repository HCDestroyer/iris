# infra/imagenes

`sin-ca.pem` (vacío) es el valor por defecto del secreto de build `ca_extra` del compose: sin
proxy que intercepte TLS no hace falta nada.

Si construyes detrás de un proxy así, copia su CA aquí como `ca-extra.pem` (ignorado por git) y pon
`IRIS_BUILD_CA_EXTRA=./imagenes/ca-extra.pem` en `.env` (ruta relativa a `infra/`). Debe quedar dentro
del repo: el build de compose (bake) no lee archivos de fuera sin `--allow`. Con `make imagenes`
sirve cualquier ruta absoluta.
