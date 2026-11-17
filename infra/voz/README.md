# Iris Voz — the AI server of Iris

A separate package for a dedicated server with an NVIDIA GPU. It runs Ollama for the Iris chat
assistant behind an authenticated gate (`puerta`). It is **not** part of the Iris installation: no
Postgres, no Redis, no panel, and it never opens a connection to Iris. Full guide (Spanish):
`docs/runbooks/servidor-ia.md`. Contract: `.claude/integration/contratos/voz.md` §4.

## What runs

| Service | What it is | Network |
| --- | --- | --- |
| `puerta` | The only listener (control port, 8443). TLS 1.3, mutual TLS with pinned fingerprints, HMAC signature and nonce on every route. Proxies only chat completions and the model list to Ollama, re-serialising every request with a closed list of fields | published port + internal |
| `llm` | Ollama. Read-only model volume, no way out | internal only |
| `medidor` | Reads the GPU with `nvidia-smi` for the Iris dashboard | none |
| `descarga` + `proxy-descarga` | One-shot containers of `iris-voz model pull`: the only outbound traffic, to the model registry only | internal + proxy |

Every container is read-only, unprivileged (uid 65532), with all capabilities dropped.

## Install

Requirements: Ubuntu/Debian, Docker with the compose plugin (2.24+), the NVIDIA driver and
`nvidia-container-toolkit`, NTP on, swap off (or encrypted).

```sh
V=0.1.0
base=https://github.com/HCDestroyer/iris/releases/download/voz-v$V
curl -fsSLO $base/install-voz.sh
curl -fsSLO $base/install-voz.sh.sigstore.json
# Verify the installer BEFORE running it (cosign 2.4+ from your own trusted source):
cosign verify-blob --new-bundle-format --bundle install-voz.sh.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity https://github.com/HCDestroyer/iris/.github/workflows/voz-release.yml@refs/tags/voz-v$V \
  install-voz.sh
sudo bash install-voz.sh --version $V
```

The installer verifies the package signature **offline** (Sigstore bundle, trusted root embedded in
the script, transparency-log proof mandatory) and that the signer is the `voz-release` workflow of
this repository on the `voz-v<version>` tag. It fails closed. Images are pinned by digest and the
default models by the digest in the signed manifest.

Then pair it from the Iris panel (Administration → Integrations → AI server): the panel shows a
one-time code, `sudo iris-voz pair` asks for it (hidden input), and the panel shows the certificate
fingerprint Iris saw: **compare it with the one this server printed before pressing "Activate"**.

## Day to day

```sh
sudo iris-voz status                 # version, containers, fingerprint, Iris IP, models
sudo iris-voz model pull qwen2.5:7b  # download (only reaches the model registry)
sudo iris-voz model allow qwen2.5:7b # let Iris use it (Iris can never pull, delete or create models)
sudo iris-voz upgrade                # verified like the install; never downgrades
sudo iris-voz renew-cert             # new certificate, announced to Iris over the current channel
sudo iris-voz unpair                 # forget Iris (stolen Iris credentials: unpair, then pair again)
sudo iris-voz ip restrict            # optional: only accept the Iris IP seen at pairing
```

Settings live in `/opt/iris-voz/.env`: `IRIS_VOZ_PUERTO`, `IRIS_VOZ_ESCUCHA`,
`IRIS_VOZ_LLM_SIMULTANEAS` (0 = computed from the VRAM and the allowed models),
`IRIS_VOZ_LLM_MAX_TOKENS`, `IRIS_VOZ_LLM_KEEP_ALIVE`, `IRIS_VOZ_LLM_CONTEXTO`,
`IRIS_VOZ_LLM_PARALELO`. Run `sudo iris-voz start` after changing them.

## What is stored and logged

Only the channel credentials (token, pinned fingerprint, the server certificate) in the `estado`
volume, and the models. **Never prompts or answers**: the gate logs route, size, duration and status
code; Ollama runs without history or debug logs.
