# Installing Iris in production

> This runbook and the installer are in English by PM decision (an exception to the repository's
> Spanish-messages rule). Variable names stay as the app reads them (`IRIS_*`).

The production install never builds anything on the server. Each GitHub Release carries:

| Asset | What it is |
| --- | --- |
| `install.sh` + `install.sh.sha256` | The installer, also the day-to-day manager (`irisctl`) |
| `iris-X.Y.Z.tar.gz` + `.sha256` | Install package: production `docker-compose.yml`, `env.example`, the installer and the backup scripts |

The images come from GHCR: `ghcr.io/hcdestroyer/iris-api`, `iris-worker` and `iris-web`, tag `X.Y.Z`,
pinned by digest in the packaged compose (so are Postgres, Redis, MinIO and OSRM). Postgres, Redis
and MinIO run next to them; there is no Mailpit. OSRM (street routing) is optional.

Sources: `infra/produccion/` (compose, env template, `install.sh`, `package.sh`) and
`.github/workflows/release.yml` (job `paquete`).

## 1. Requirements

- Ubuntu or Debian, 4 GB of RAM, 20 GB of free disk.
- Docker Engine and the Compose v2 plugin (2.24 or newer):
  <https://docs.docker.com/engine/install/ubuntu/>. The installer checks them; it does not install Docker.
  **Docker Engine 28 or newer is recommended:** before 28.0, a port published on `127.0.0.1` can be
  reached by other machines on the same network segment. The installer warns about older engines.
- `curl`, `tar`, `sha256sum` (always there on Ubuntu).
- `age` for encrypted backups. The installer installs it with `apt-get`; where the distribution has
  no package (Ubuntu 20.04), it downloads the official `age` v1.2.1 binary from its GitHub release,
  checks its pinned sha256 and keeps it in `/opt/iris/bin`. If that also fails, backups are plain
  `pg_dump` files readable only by root, with a loud warning: a lab migration or an upgrade never
  runs without a backup unless you pass `--skip-backup`.
- A reverse proxy with HTTPS for the public name (Apache/Virtualmin, nginx, Nginx Proxy Manager…).
- **One Iris per Docker host**: the compose project and its volumes are always named `iris`.

## 2. Install

```sh
curl -fsSLO https://github.com/HCDestroyer/iris/releases/latest/download/install.sh
curl -fsSLO https://github.com/HCDestroyer/iris/releases/latest/download/install.sh.sha256
sha256sum -c install.sh.sha256          # must print "install.sh: OK"
sudo bash install.sh
```

Optional, stronger check (needs the GitHub CLI): every release asset carries a build provenance
attestation, which proves it was built by this repository's release workflow:

```sh
gh attestation verify install.sh --repo HCDestroyer/iris \
    --signer-workflow HCDestroyer/iris/.github/workflows/release.yml --source-ref refs/heads/main
```

The installer does the same for the package it downloads (`iris-X.Y.Z.tar.gz`, also on
`irisctl upgrade`), with the same signer workflow and ref (packages are only built by `release.yml`
running from `main`): when `gh` is installed and logged in as root (`sudo gh auth login`), a package
whose attestation does not verify is refused, and `gh`'s own error is shown (a network, rate-limit
or login problem also stops it). Without `gh` it says so, checks the sha256 only and prints the
command to verify the package by hand from any machine that has `gh`.

`install`, `upgrade`, `start` and `backup` change the install, so they run as root and share one
lock per server (`/run/iris/install.lock`); `status`, `logs`, `stop` and `compose` only need Docker.

It asks for what it needs. The same answers can be passed as flags (`--yes` never asks):

```sh
sudo bash install.sh --yes \
    --url https://iris.example.com \
    --argos-origin https://argos.example.com \
    --web-port 3000 \
    --admin-email admin@example.com --admin-name "Admin"
```

| Flag | Variable | Notes |
| --- | --- | --- |
| `--url` | `IRIS_BASE_URL` | Public HTTPS address of the panel (the reverse proxy's). Required |
| `--argos-origin` | `IRIS_ARGOS_ORIGENES` | Origin(s) of ArgosISP allowed to connect, comma separated. Empty = connect later |
| `--horus-origin` | `IRIS_HORUS_ORIGENES` | Same for Horus |
| `--web-port` | `IRIS_WEB_PUERTO` | Panel port on 127.0.0.1: the reverse proxy target (3000) |
| `--api-port` | `IRIS_API_PUERTO` | API port on 127.0.0.1, for local checks (8080) |
| `--version` | `IRIS_VERSION` | Default: the latest release |
| `--dir` | `IRIS_DIR` | Default `/opt/iris` |
| `--with-osrm` | `IRIS_OSRM` | Also run OSRM (needs `infra/osrm/preparar.sh` data in the `iris_osrm` volume) |
| `--from-lab` | | Migrate a `make dev-app` install (section 6) |
| `--skip-backup` | | Lab migration without a backup first (not recommended) |

What it does, step by step:

1. Checks Docker, Compose v2 and that the ports it will publish are free (ports used by Iris's own
   containers do not count). It refuses an install folder owned by another user, and only one Iris
   runs per Docker host.
2. Downloads `iris-X.Y.Z.tar.gz` and its `.sha256` from the release, verifies the sum and unpacks it
   into `/opt/iris/releases/X.Y.Z` (`/opt/iris/current` points to it).
3. Writes `/opt/iris/.env` (mode 600) from the template with your answers. It generates
   `IRIS_CLAVE_CIFRADO`, the Postgres password and the MinIO secret **only if they are missing**,
   and an age key pair for backups in `/opt/iris/backup-keys/`. Until the install finishes, a marker
   (`/opt/iris/.install-pending`) makes a re-run resume it with the same steps.
4. Pulls the images, runs the migrations (`migrar`, before api and worker), starts everything and
   waits for `/api/v1/salud` on the API and through the panel.
5. Offers to create the first administrator and prints its password **once** (on the terminal).
6. Prints the reverse-proxy settings and how to check, then shows the private backup key once and
   offers to delete it from the server after you copy it.

Running it again is safe: it keeps every existing value and secret, fills in only what is missing,
applies the flags you pass and makes sure everything is up. Containers that did not change are not
restarted. After the first run the same script is `irisctl` (`/usr/local/bin/irisctl`).

**Back up `/opt/iris/.env` outside the server right away.** `IRIS_CLAVE_CIFRADO` encrypts MFA
secrets, connector tokens and mailbox passwords; without it they cannot be recovered
([key rotation](rotar-clave-cifrado.md)). Copy `/opt/iris/backup-keys/identity.txt` (the private
backup key) outside the server too, and then you may delete it from the server: backups only need
the public key (`recipients.txt`); the private one is only needed to restore.

## 3. Reverse proxy

Only the panel (`IRIS_WEB_PUERTO`) and the API (`IRIS_API_PUERTO`, for health checks) are published,
both on `127.0.0.1`. Only the panel port goes behind the proxy: the panel serves `/api/*` itself.
Postgres, MinIO and OSRM are reachable only inside the compose network. To reach them from the host
(psql, the MinIO console through an SSH tunnel), set `IRIS_PUERTOS_ADMIN=true` in the `.env` and
`irisctl start`: they are then published on `127.0.0.1` at `IRIS_PG_PUERTO`, `IRIS_MINIO_PUERTO`,
`IRIS_MINIO_CONSOLA_PUERTO` and `IRIS_OSRM_PUERTO` (`docker-compose.admin-ports.yml`).

Apache / Virtualmin (Server Configuration → Edit Directives, in the `:443` VirtualHost; modules
`proxy`, `proxy_http` and `headers`):

```apache
ProxyPreserveHost On
ProxyPass        / http://127.0.0.1:3000/
ProxyPassReverse / http://127.0.0.1:3000/
RequestHeader set X-Forwarded-Proto "https"
```

nginx:

```nginx
server {
    listen 443 ssl;
    server_name iris.example.com;
    client_max_body_size 30m;          # photo uploads (25 MiB per attachment)
    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

Nginx Proxy Manager: NPM usually runs in Docker or on another machine, and cannot reach a port bound
to the server's `127.0.0.1`. Point the NPM Proxy Host (SSL certificate, "Force SSL") to the
Apache/nginx of the Iris server, and let that one proxy to `127.0.0.1:<IRIS_WEB_PUERTO>` as above.
That is the chain on the PM's server: NPM → Apache (Virtualmin) → `127.0.0.1:53000`.

Client IP: the API trusts `X-Forwarded-For` only from the panel's fixed IP (`IRIS_WEB_IP`). Never put
the Docker network or its gateway in `IRIS_PROXIES_CONFIABLES` ([lab](lab.md), "IP del cliente").

Check:

```sh
curl -fsS http://127.0.0.1:8080/api/v1/salud        # {"estado":"ok","version":"X.Y.Z"}
curl -fsS http://127.0.0.1:3000/api/v1/salud        # the same, through the panel
curl -fsSI https://iris.example.com/entrar          # 200 through the reverse proxy
irisctl status
```

Then sign in, turn on MFA, and connect Argos from its panel (the origin must be in
`IRIS_ARGOS_ORIGENES`; see [lab](lab.md), "Conectar con Argos o Horus").

If Argos runs on the same server and Iris reaches it through a private address, add that network to
`IRIS_ARGOS_REDES_PERMITIDAS` (otherwise the SSRF protection blocks it). If Iris reaches Argos
through its public HTTPS name, leave it empty.

## 4. Mail (real SMTP)

Production has no Mailpit. Support mailboxes are created in the panel (Support → Mailboxes): each one
stores its own IMAP and SMTP host, port, user and password (encrypted with `IRIS_CLAVE_CIFRADO`), and
TLS is always verified ([buzones](buzones.md)). Relevant variables in `/opt/iris/.env`:

| Variable | Use |
| --- | --- |
| `IRIS_CORREO_REDES_PERMITIDAS` | Empty when the mail server has a public IP. If it is on an internal network, its CIDR |
| `IRIS_CORREO_CA` | **Optional.** Only for a mail server with a private certificate: copy its CA (PEM) to `/opt/iris/certs/` and set `IRIS_CORREO_CA=/run/iris/certs/<file>.pem`. Empty = public CAs |
| `IRIS_CORREO_TIMEOUT` | Time limit per mail connection (30s) |
| `IRIS_SMTP_HOST`, `IRIS_SMTP_PORT`, `IRIS_SMTP_USUARIO`, `IRIS_SMTP_CONTRASENA`, `IRIS_SMTP_TLS`, `IRIS_SMTP_REMITENTE` | System mail. Reserved: today Iris only sends mail through the mailboxes |

After editing the `.env`: `irisctl start`.

## 5. Day to day

```sh
irisctl status                 # installed and latest version, containers, health, last backup
irisctl logs [api|worker|web]  # follow the logs
irisctl backup                 # database backup now (/opt/iris/backups, encrypted with age)
irisctl upgrade                # backup → new package and images → migrations → health check
irisctl upgrade --version X.Y.Z
irisctl stop                   # stop everything (data is kept)
irisctl start                  # start again and wait until healthy
irisctl create-admin --email a@b.c --name "Name"
irisctl compose ps             # any docker compose command on the install
```

`irisctl upgrade` checks the new compose file, pulls the new images and takes a backup before
changing anything. It refuses to go to an older version (migrations only move forward) and does
nothing if the backup fails (unless `--skip-backup`). If the new version does not become healthy, it prints
how to go back: restore the backup it just took ([respaldos](respaldos.md)), point `IRIS_VERSION`
and `/opt/iris/current` to the previous version and `irisctl start`.

Daily backup (host cron, UTC):

```cron
17 3 * * * /usr/local/bin/irisctl backup >>/var/log/iris-backup.log 2>&1
```

Backups cover the database. Attachments live in the `iris_minio` volume: copy it (or replicate the
bucket) separately. Copy `/opt/iris/backups` to another machine.

## 6. Migrating a `make dev-app` lab install

A lab install keeps its data: the production compose uses the same project (`iris`), service and
volume names (`iris_pgdata`, `iris_minio`), so the containers are recreated with the GHCR images on
top of the same volumes.

On the PM's server the lab is a git checkout in `/opt/iris` (tag v0.2.0) with the containers
`iris-api-1`, `iris-web-1`, `iris-worker-1`, `iris-postgres-1`, `iris-redis-1`, `iris-minio-1` and
`iris-mailpit-1`. Steps:

1. Keep a copy of the lab `.env` outside the server (it has `IRIS_CLAVE_CIFRADO`).
2. Run the installer exactly as in section 2 (`sudo bash install.sh`, from any folder other than
   `/opt/iris`, e.g. your home). It detects the lab and:
   - moves the checkout `/opt/iris` to `/opt/iris-lab` (after asking), before creating anything, and
     installs in `/opt/iris`;
   - checks, before writing anything, that the release is not older than the lab database schema
     (with the lab's own postgres container). If it is, it stops and tells you to pass a newer
     `--version`;
   - builds the new `/opt/iris/.env` from the lab `.env`: encryption key, database and MinIO
     credentials, public address, Argos/Horus origins and networks, ports, Docker subnet and every
     other `IRIS_*` value the lab set. It drops what only made sense in the lab: Mailpit
     (`IRIS_SMTP_*`, `IRIS_CORREO_CA`, its ports), `localhost` URLs, build settings. Lab backup keys
     are copied into `/opt/iris/backup-keys`;
   - replaces the lab MinIO secret when it is the public default or has characters other than
     letters and digits (e.g. one made with `openssl rand -base64`): the production compose passes it
     in a URL. Attachments stay readable: MinIO takes its root credentials from the environment;
   - starts the lab database under the production compose and checks the schema again;
   - takes a backup of the lab database into `/opt/iris/backups` (encrypted with age, or the plain
     fallback above). If the backup fails, it stops before any migration;
   - starts the production containers on the same volumes (migrations run first) and removes Mailpit;
   - once healthy, replaces the public lab database password `iris` with a generated one (stored as a
     SCRAM verifier, never sent in clear; `--keep-db-password` skips it). The new password is saved
     in the `.env` first, so an interruption at any point is finished by the next run.
3. Apache keeps pointing to `127.0.0.1:53000` (and the API stays on `58080`): nothing to change in
   Virtualmin or NPM. Postgres (`55432`) and MinIO (`59000/59001`) are no longer published on the
   host; see "Reverse proxy" for `IRIS_PUERTOS_ADMIN` if you used them.
4. Check: `irisctl status`, sign in with the existing users, open an order with photos.
5. Mailboxes created against Mailpit in the lab now fail: edit them in the panel to use the real
   SMTP/IMAP server.
6. Do not run `make dev-app` in `/opt/iris-lab` again (it would take the containers back with the old
   passwords). When everything works for a few days, delete `/opt/iris-lab` and the old images
   (`docker image rm iris/api:dev iris/worker:dev iris/web:dev`).

If anything stops half way (a busy port, no network, a failed backup), fix it and run
`sudo bash install.sh` again: it resumes the migration, including the backup and the password
changes. While nothing has been migrated yet, the resumed run may use another `--version`. Until the
install finishes, `irisctl status`, `logs`, `stop`, `compose` and `backup` work (with a warning);
`start`, `upgrade` and `create-admin` wait for it. Non-interactive: `sudo bash install.sh --yes`. If the lab `.env` is somewhere else:
`--from-lab /path/to/.env`.

### Discard an unfinished install

`/opt/iris/.install-pending` says how far it got: `lab prepared …` means no migration has run;
`lab migrating …` means the migrations may have run (then use "Back to the lab" below).

With `prepared`, the lab database is untouched and the lab `.env` (in `/opt/iris-lab`) is still
valid as is:

```sh
docker compose --env-file /opt/iris/.env -f /opt/iris/current/docker-compose.yml down   # keeps volumes
sudo mv /opt/iris /opt/iris-discarded && sudo mv /opt/iris-lab /opt/iris
cd /opt/iris && make dev-app
```

MinIO takes its credentials from the `.env` of whoever starts it, so the lab's own secret works
again. Delete `/opt/iris-discarded` once the lab is back (it holds a backup and secrets).

### Back to the lab

Once `migrar` has run, the database schema belongs to the installed release. Going back means
restoring the backup taken in step 2, not just starting the lab again:

```sh
docker compose --env-file /opt/iris/.env -f /opt/iris/current/docker-compose.yml down   # keeps volumes
sudo mv /opt/iris /opt/iris-failed && sudo mv /opt/iris-lab /opt/iris
cd /opt/iris
docker compose -f infra/docker-compose.yml --env-file .env up -d --wait postgres
docker compose -f infra/docker-compose.yml --env-file .env exec -T postgres \
    sh -c 'dropdb -U "$POSTGRES_USER" "$POSTGRES_DB" && createdb -U "$POSTGRES_USER" "$POSTGRES_DB"'
# encrypted backup (.dump.age):
age -d -i /opt/iris-failed/backup-keys/identity.txt /opt/iris-failed/backups/<file>.dump.age |
    docker compose -f infra/docker-compose.yml --env-file .env exec -T postgres \
    sh -c 'pg_restore --no-owner --exit-on-error --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
# plain fallback backup (.dump): the same pg_restore, reading the file instead of age's output
```

If the installer had already replaced the lab defaults, put the values from
`/opt/iris-failed/.env` into `/opt/iris/.env` before starting the lab: `IRIS_PG_CONTRASENA` (the
database role keeps the new password) and `IRIS_S3_SECRET_KEY`. Then `make dev-app`.
