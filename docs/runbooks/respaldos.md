# Respaldo y restauración de la base

Scripts en `infra/respaldo/`. La base de Iris es la única fuente de verdad propia (tickets,
órdenes, auditoría, outbox, colas de River); los adjuntos viven en S3/MinIO y se respaldan aparte
(versionado o replicación del bucket). Lo de Argos se vuelve a leer de Argos.

## Cifrado: age con claves públicas

- `respaldar.sh` cifra el `pg_dump` con **age** hacia una o más **claves públicas**
  (`IRIS_RESPALDO_DESTINATARIOS`). El servidor que respalda no puede descifrar lo que escribe.
- La **clave privada** (`IRIS_RESPALDO_IDENTIDAD`, `AGE-SECRET-KEY-…`) se guarda fuera del servidor
  (gestor de secretos y una copia offline) y solo se usa al restaurar.
- Por qué age y no gpg simétrico: no hay una frase de paso que tenga que vivir en el servidor ni en
  el cron; age es un binario sin estado (sin llavero ni `gpg-agent`) y el formato es simple y
  auditado. Puedes poner varias claves públicas (p. ej. la del equipo y una de emergencia).

```sh
age-keygen -o iris-respaldos.key                   # privada: guárdala fuera del servidor
age-keygen -y iris-respaldos.key > destinatarios.txt   # pública: va al servidor
```

## Respaldar

```sh
# Con pg_dump local (cliente de PostgreSQL 16) contra IRIS_DB_URL:
IRIS_RESPALDO_DESTINATARIOS=/etc/iris/destinatarios.txt infra/respaldo/respaldar.sh
# Con el pg_dump del contenedor postgres del compose (lab):
infra/respaldo/respaldar.sh --compose
# O con make (ARGS=--compose para el compose):
make respaldo
```

Deja `IRIS_RESPALDO_DIR/<base>-<AAAAMMDDTHHMMSSZ>.dump.age` y su `.sha256`, y borra los de más de
`IRIS_RESPALDO_DIAS` días (14; 0 = no borra). Si `pg_dump` falla no queda archivo con nombre final.
El formato es `pg_dump --format=custom` de toda la base (todos los schemas, incluidos `auditoria` y
`river`).

Programar (cron del host, UTC):

```cron
17 3 * * * cd /opt/iris && infra/respaldo/respaldar.sh --compose >>/var/log/iris-respaldo.log 2>&1
```

Copia la carpeta de salida a otro lugar (otro servidor, bucket con bloqueo de objetos): un respaldo
en el mismo disco que la base no protege de perder la máquina.

## Restaurar

```sh
createdb -h <host> -U iris iris_restaurada
IRIS_RESPALDO_IDENTIDAD=~/iris-respaldos.key \
    infra/respaldo/restaurar.sh salida/iris-20261028T031700Z.dump.age \
    "postgres://iris:…@<host>:5432/iris_restaurada?sslmode=disable"
```

- Verifica el `.sha256` si está junto al archivo.
- El destino se escribe siempre a mano (nunca se toma `IRIS_DB_URL`) y debe estar **vacío**; con
  `--limpiar` borra antes lo que el respaldo trae (`pg_restore --clean --if-exists`).
- `--single-transaction --exit-on-error`: o se restaura todo o nada. `--no-owner --no-privileges`:
  el dueño pasa a ser el usuario de la URL.

Para volver a poner el lab en un respaldo:

```sh
make parar
docker compose -f infra/docker-compose.yml --env-file .env up -d --wait postgres
# borrar y crear la base (dentro del contenedor):
docker compose -f infra/docker-compose.yml --env-file .env exec -T postgres \
    sh -c 'dropdb -U "$POSTGRES_USER" "$POSTGRES_DB" && createdb -U "$POSTGRES_USER" "$POSTGRES_DB"'
IRIS_RESPALDO_IDENTIDAD=~/iris-respaldos.key infra/respaldo/restaurar.sh <archivo> \
    "postgres://iris:<contraseña>@127.0.0.1:5432/iris?sslmode=disable"
make dev-app        # migrar aplica lo que falte si el respaldo es de una versión anterior
```

La base restaurada necesita la **misma `IRIS_CLAVE_CIFRADO`** que tenía cuando se respaldó: sin
ella, MFA, conectores y buzones quedan ilegibles ([rotación](rotar-clave-cifrado.md)). Guarda la
clave junto con la privada de age.

## Ensayo (CI y a mano)

`infra/respaldo/ensayo.sh <url de origen> <base de destino>` respalda con una clave age efímera,
comprueba que el archivo no se pueda leer sin descifrar, restaura en `<base de destino>` (la borra
y la crea) y compara la cantidad exacta de filas de cada tabla. El job `respaldo` de CI lo corre en
cada PR sobre una base migrada y sembrada (`iris admin crear`, `iris campo semilla`).

```sh
IRIS_DB_URL=postgres://iris:iris@localhost:5432/iris_test_<agente>?sslmode=disable make ensayo-respaldo
```
