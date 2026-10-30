# Installing Iris in production

> This runbook and the installer are in English by PM decision (an exception to the repository's
> Spanish-messages rule). Variable names stay as the app reads them (`IRIS_*`).

The production install never builds anything on the server. Each GitHub Release carries:

| Asset | What it is |
| --- | --- |
| `install.sh` + `install.sh.sha256` | The installer, also the day-to-day manager (`irisctl`) |
| `iris-X.Y.Z.tar.gz` + `.sha256` | Install package: production `docker-compose.yml`, `env.example`, the installer and the backup scripts |

The images come from GHCR: `ghcr.io/hcdestroyer/iris-api`, `iris-worker` and `iris-web`, tag `X.Y.Z`.
Postgres, Redis and MinIO run next to them; there is no Mailpit. OSRM (street routing) is optional.

Sources: `infra/produccion/` (compose, env template, `install.sh`, `package.sh`) and
`.github/workflows/release.yml` (job `paquete`).

## 1. Requirements

- Ubuntu or Debian, 4 GB of RAM, 20 GB of free disk.
- Docker Engine and the Compose v2 plugin (2.24 or newer):
  <https://docs.docker.com/engine/install/ubuntu/>. The installer checks them; it does not install Docker.
- `curl`, `tar`, `sha256sum` (always there on Ubuntu). `age` for encrypted backups (the installer
  installs it with `apt-get` when it runs as root).
- A reverse proxy with HTTPS for the public name (Apache/Virtualmin, nginx, Nginx Proxy Manager…).
- **One Iris per Docker host**: the compose project and its volumes are always named `iris`.

## 2. Install

```sh
curl -fsSLO https://github.com/HCDestroyer/iris/releases/latest/download/install.sh
curl -fsSLO https://github.com/HCDestroyer/iris/releases/latest/download/install.sh.sha256
sha256sum -c install.sh.sha256          # must print "install.sh: OK"
sudo bash install.sh
```

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

What it does, step by step:

1. Checks Docker, Compose v2 and that the ports it will publish are free (ports used by Iris's own
   containers do not count).
2. Downloads `iris-X.Y.Z.tar.gz` and its `.sha256` from the release, verifies the sum and unpacks it
   into `/opt/iris/releases/X.Y.Z` (`/opt/iris/current` points to it).
3. Writes `/opt/iris/.env` (mode 600) from the template with your answers. It generates
   `IRIS_CLAVE_CIFRADO`, the Postgres password and the MinIO secret **only if they are missing**,
   and an age key pair for backups in `/opt/iris/backup-keys/`.
4. Pulls the images, runs the migrations (`migrar`, before api and worker), starts everything and
   waits for `/api/v1/salud` on the API and through the panel.
5. Offers to create the first administrator and prints its password **once**.
6. Prints the reverse-proxy settings and how to check.

Running it again is safe: it keeps every existing value and secret, fills in only what is missing,
applies the flags you pass and makes sure everything is up. Containers that did not change are not
restarted. After the first run the same script is `irisctl` (`/usr/local/bin/irisctl`).

**Back up `/opt/iris/.env` outside the server right away.** `IRIS_CLAVE_CIFRADO` encrypts MFA
secrets, connector tokens and mailbox passwords; without it they cannot be recovered
([key rotation](rotar-clave-cifrado.md)). Copy `/opt/iris/backup-keys/identity.txt` (the private
backup key) outside the server too, and then you may delete it from the server: backups only need
the public key (`recipients.txt`); the private one is only needed to restore.

## 3. Reverse proxy

Every port is bound to `127.0.0.1`. Only the panel port (`IRIS_WEB_PUERTO`) goes behind the proxy:
the panel serves `/api/*` itself.

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
irisctl backup                 # encrypted database backup now (/opt/iris/backups)
irisctl upgrade                # backup → new package and images → migrations → health check
irisctl upgrade --version X.Y.Z
irisctl stop                   # stop everything (data is kept)
irisctl start                  # start again and wait until healthy
irisctl create-admin --email a@b.c --name "Name"
irisctl compose ps             # any docker compose command on the install
```

`irisctl upgrade` refuses to go to an older version (migrations only move forward) and does nothing
if the backup fails (unless `--skip-backup`). If the new version does not become healthy, it prints
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
   - moves the checkout `/opt/iris` to `/opt/iris-lab` (after asking) and installs in `/opt/iris`;
   - builds the new `/opt/iris/.env` from the lab `.env`: encryption key, database and MinIO
     credentials, public address, Argos/Horus origins and networks, ports (`53000`, `58080`, `55432`,
     `59000/59001`, `55000`), Docker subnet and every other `IRIS_*` value the lab set. It drops what
     only made sense in the lab: Mailpit (`IRIS_SMTP_*`, `IRIS_CORREO_CA`, its ports), `localhost`
     URLs, build settings;
   - takes an encrypted backup of the lab database into `/opt/iris/backups`;
   - starts the production containers on the same volumes and removes Mailpit;
   - once healthy, replaces the dev database password (`iris`) with a generated one
     (`--keep-db-password` skips this).
3. Apache keeps pointing to `127.0.0.1:53000`: nothing to change in Virtualmin or NPM.
4. Check: `irisctl status`, sign in with the existing users, open an order with photos.
5. Mailboxes created against Mailpit in the lab now fail: edit them in the panel to use the real
   SMTP/IMAP server.
6. Do not run `make dev-app` in `/opt/iris-lab` again (it would take the containers back). When
   everything works for a few days, delete `/opt/iris-lab` and the old images
   (`docker image rm iris/api:dev iris/worker:dev iris/web:dev`).

Non-interactive: `sudo bash install.sh --yes`. If the lab `.env` is somewhere else:
`--from-lab /path/to/.env`. The MinIO secret of the lab stays (it is only reachable from 127.0.0.1);
changing it means changing `IRIS_S3_SECRET_KEY` and restarting MinIO with care, so it is left as a
manual step.

### Back to the lab

If the migration fails before Iris is healthy, the lab `.env` is still valid as is:

```sh
cd /opt/iris && docker compose --env-file .env -f current/docker-compose.yml down   # keeps volumes
sudo mv /opt/iris /opt/iris-failed && sudo mv /opt/iris-lab /opt/iris
cd /opt/iris && make dev-app
```

If the installer already finished (the database password was changed), first copy
`IRIS_PG_CONTRASENA` from `/opt/iris-failed/.env` into `/opt/iris/.env`.
