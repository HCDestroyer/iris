# Iris production install

Files shipped in each GitHub Release (`iris-X.Y.Z.tar.gz`, built by `package.sh`):

| File | What it is |
| --- | --- |
| `install.sh` | Installer and manager (`irisctl` after the install): install, upgrade, status, logs, stop, start, backup, create-admin |
| `docker-compose.yml` | Production compose: GHCR images, no build, no Mailpit, ports on 127.0.0.1 only |
| `.env.example` | Template of `<install dir>/.env` (shipped as `env.example`) |
| `package.sh` | Builds the release package (used by `.github/workflows/release.yml`) |

The package also carries `infra/respaldo/respaldar.sh` and `restaurar.sh` (folder `backup/`).

Install folder (`/opt/iris` by default):

```
/opt/iris/.env                 settings and secrets (600)
/opt/iris/current -> releases/X.Y.Z
/opt/iris/releases/X.Y.Z/      unpacked package of each installed version (last 3 kept)
/opt/iris/backups/             encrypted database backups (*.dump.age)
/opt/iris/backup-keys/         age key pair (copy identity.txt off the server)
/opt/iris/certs/               optional CA for a mail server with a private certificate
```

The compose project is `iris` with the same service and volume names as `infra/docker-compose.yml`,
so a `make dev-app` lab becomes a production install in place (`install.sh --from-lab`).

Full guide: [docs/runbooks/install.md](../../docs/runbooks/install.md). The installer is in English
by PM decision (an exception to the repository's Spanish-messages rule).
