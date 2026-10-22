# Editing and publishing documentation

## Local workflow

Use Node 24.13.0 and Task, matching CI. From the repository root:

```sh
task docs:install
task docs:dev
```

Open `http://localhost:5173/docs/`. Edit Markdown under `docs/`; changes reload in the
browser. Navigation lives in `docs/.vitepress/config.mts`. Use ordinary relative
Markdown links. Keep hardware claims tied to evidence and update documentation in
the same change as software behavior.

```sh
task docs:build
task docs:preview
```

Vite is pinned to a patched compatible release through a dependency override.
The preview uses Vite directly because VitePress 1.6 preview does not honor a
loopback host option.

The build checks internal page links and writes `docs/.vitepress/dist`. Preview
listens on `http://localhost:4173/docs/`. Development and preview bind to loopback and
have no authentication. The production manual is currently public as described below.

## Authentication regression check

With Docker and curl available, run `task docs:test:auth`. It builds the site,
starts a temporary Caddy container on a random loopback port, and checks missing,
incorrect, and correct credentials against pages and the actual search index.
It also checks private cache headers, then removes the container and credentials.
The documentation CI job runs this check.

## Production: IFIC

The production URL is **https://pet.next-experiment.org/docs/**.
Cloudflare DNS has a DNS-only A record to the IFIC server `147.156.49.194`.
The existing Caddy service on `next.ific.uv.es` owns HTTPS and imports
`/etc/caddy/pet.caddy` (the template is `deploy/docs/pet.caddy`).

**Current access policy:** authentication was temporarily disabled at the operator's
request on 2026-10-22. The manual, pages, and search assets are publicly readable.
NEXT100 docs retain their separate authentication. The indexing and cache headers
remain configured; they do not restrict access.

The dedicated `/etc/caddy/pet-auth.caddy` snippet is retained but is not imported.
Its web login remains stored in Infisical at `prod:/services/pet-caen/docs`
(`USERNAME`, `PASSWORD`, `URL`), separate from the SSH deployment key. The username
is `petdocs`. To restore the password requirement, add
`import /etc/caddy/pet-auth.caddy` inside the PET site block, validate the shared
configuration, reload Caddy, and verify unauthenticated pages and assets return 401.

The dedicated `petdocs` Linux account deploys over SSH port `2222`, has no sudo
or Docker access, and owns `/home/petdocs/content`. Caddy mounts this directory
read-only at `/srv/pet-docs`. Releases are uploaded into `content/releases/`, then
`content/site` switches atomically to the uploaded release. Do not run the standalone
Compose example on the production host: its public ports belong to shared Caddy.

To change the production route, back up `/home/next/next-server/caddy/Caddyfile`
and `pet.caddy`, validate, then reload only Caddy. The initial volume addition
requires recreating only the Caddy service; later content deployments need no reload.
Keep the parent content mount intact so symlink switches remain visible.

## Standalone protected server example

The hosting example uses a dedicated Linux server with Docker Compose, a DNS
hostname pointing to it, and ports 80/443 available for Caddy HTTPS. It serves
static documentation independently of the DAQ and provides no acquisition API.

Copy `deploy/docs/Caddyfile`, `compose.yml`, and `.env.example` to
`/srv/pet-caen-docs/` on that server. Copy `.env.example` to `.env` and set
`DOCS_DOMAIN` to the real hostname.

Create one password hash per collaborator interactively:

```sh
docker run --rm -it caddy:2.11.2-alpine caddy hash-password
```

Create `/srv/pet-caen-docs/users.caddy` with one username and hash per line:

```text
alice <paste-alice-password-hash-here>
bob <paste-bob-password-hash-here>
```

Replace the placeholders before starting. Keep this file on the server, outside
Git. Never commit passwords. An empty file is not a configured collaborator list.
Caddy authenticates every request, including search assets and downloads.

For the first publication, copy the contents of `docs/.vitepress/dist/` into
`/srv/pet-caen-docs/content/releases/initial/`, then on the server:

```sh
cd /srv/pet-caen-docs
ln -s releases/initial content/site
docker compose config --quiet
docker compose run --rm docs caddy validate --config /etc/caddy/Caddyfile
docker compose up -d
```

Only expose Caddy's ports. Keep `content` readable by Caddy. The configured headers
ask browsers and proxies not to cache private documentation. `noindex` is an
indexing preference; server authentication enforces access.

Before sharing the hostname, verify that unauthenticated requests to `/docs/`,
`/docs/guide/telemetry.html`, and a generated `/docs/assets/` URL all return **401**. Verify
that a collaborator can log in, navigate, and search. To revoke access, remove
the user's line and recreate the service with `docker compose up -d --force-recreate`.
Use the same restart after changing credentials.

## Automatic publication

The Documentation GitHub Actions workflow builds pull requests and main updates.
It uploads a build artifact for seven days. Repository readers can download that
artifact; use a private repository if the source and artifacts must be private.
It does not publish to a public Pages origin.

To enable deployment after setting up the protected server:

1. Create a dedicated SSH deployment user able to write only the documentation
   content directory. Production uses `petdocs` and `/home/petdocs/content`.
   For another server, update the workflow paths.
2. Configure the GitHub `documentation` environment with secrets `DOCS_SSH_HOST`,
   `DOCS_SSH_USER`, `DOCS_SSH_PORT`, `DOCS_SSH_KEY`, and `DOCS_SSH_KNOWN_HOSTS`. The last value must
   contain the server's SSH host key verified through a trusted channel.
3. Set repository variable `DOCS_DEPLOY_ENABLED` to `true`.

Main updates then build and publish automatically; the workflow can also be
started manually on main. Each upload gets a new release directory and switches
`content/site` only after transfer succeeds. Caddy mounts the parent directory,
so it sees the new symlink without restarting. Domain and password files stay on
the server. The deployment account needs neither Docker access nor DAQ access.

To roll back, point `content/site` at a previous release using the same temporary
symlink and rename pattern. Old release directories are retained; periodically
remove unused releases while retaining the current target and a rollback copy.

## Software versions

This site initially publishes one current documentation build. Match it to the
installed software using the source revision shown in the build workflow. Git
retains documentation history. A public version selector and concurrent release
manuals are not implemented yet.
