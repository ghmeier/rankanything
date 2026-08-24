# Deploying Rankanything

The app ships as one container: a static Go binary with the markup, the
stylesheet, the JavaScript, and the migrations all compiled in. There is no
separate asset step at runtime and no SQL on disk in the image.

Host is Render, configured by `render.yaml` rather than through the
dashboard, so the deploy is reviewable in git.

## First deploy

1. **Create the Blueprint.** In Render, New → Blueprint, point it at this
   repository. It reads `render.yaml` and proposes one web service
   (`rankanything`) and one Postgres instance (`rankanything-db`), both in
   `oregon`. The database URL is wired to the service automatically.

2. **Fill in the three variables Render prompts for.** They are marked
   `sync: false` in the blueprint precisely so they are never committed:

   | Variable | First deploy value | Notes |
   | -------- | ------------------ | ----- |
   | `RESEND_API_KEY` | from the Resend dashboard | Leave unset to run without email; see below for what that costs. |
   | `BASE_URL` | `https://rankanything.onrender.com` | No trailing slash. |
   | `EMAIL_FROM` | `Rank Anything <onboarding@resend.dev>` | Until a domain is verified. |

3. **Deploy.** The container migrates the database on boot and then serves.
   Render waits for `/healthz` to pass before routing traffic to the new
   container, so a failed migration leaves the previous version serving.

4. **Smoke test it.**

   ```bash
   scripts/smoke-test.sh https://rankanything.onrender.com
   ```

   Ten unauthenticated checks. It creates no account and sends no email, so
   it is safe to re-run against production whenever you want.

## `BASE_URL` is not cosmetic

Verification and password-reset emails build their links from `BASE_URL`. If
it is wrong, the app looks completely healthy — pages render, sign-up
succeeds — and every emailed link goes somewhere that does not exist. Update
it in the same deploy that moves the app to a real domain, not after.

## Email and the domain

No domain is registered yet, so the MVP ships sending from Resend's shared
`onboarding@resend.dev` sender. That works, but unverified shared senders
land in spam noticeably more often, so expect some users not to receive the
verification mail.

With no `RESEND_API_KEY` set at all, `internal/email` falls back to its dev
sink: it logs the message instead of sending it. Registration still succeeds
and the verification link appears in the Render logs. That is fine for a
soft launch with people you can reach directly, and wrong for anything
public — email verification gates the share control, so users who never
receive the mail cannot share.

### When the domain is registered

1. Add it in Resend under Domains. Resend issues three records to create at
   the registrar:
   - a **DKIM** `TXT` record (`resend._domainkey`), which signs outgoing mail;
   - an **SPF** `TXT` record on the sending subdomain, listing Resend as an
     authorized sender;
   - an **MX** record on the sending subdomain, for bounce handling.
2. Add a **DMARC** `TXT` record at `_dmarc` yourself — Resend does not issue
   one and inbox providers increasingly expect it. Start at
   `v=DMARC1; p=none; rua=mailto:you@yourdomain`, which only asks for
   reports, and tighten to `p=quarantine` once those reports come back clean.
3. Wait for Resend to verify, then update `EMAIL_FROM` to the new domain and
   `BASE_URL` to the new host, and point the domain at the Render service
   under Settings → Custom Domains. Render provisions the TLS certificate.
4. Remove the stale default in `internal/config/config.go`, which still
   carries a TODO about exactly this.

## Backups and restore

Paid Render Postgres instances get point-in-time recovery. The window
follows the **workspace** plan rather than the database instance type: 3
days on Hobby, 7 days on Pro and above. Logical backups you export are kept
for seven days regardless of plan.

Free instances get neither — no point-in-time recovery and no backups of any
kind — and expire 30 days after creation, with a 14-day grace period before
deletion. That is why the blueprint specifies `basic-256mb` and not `free`.

Restoring is something you initiate from the dashboard when you need it, so
the procedure below is worth walking through once before you need it, not
during an incident.

A backup nobody has restored is a guess, so verify it rather than trusting
it. Restore into a *new* database — never over the live one:

```bash
# 1. Take a logical dump of production. Read-only; safe to run any time.
pg_dump "$RENDER_EXTERNAL_DATABASE_URL" --no-owner --no-acl -Fc -f /tmp/rankanything.dump

# 2. Restore it into a scratch local database.
createdb rankanything_restore_check
pg_restore --no-owner --no-acl -d rankanything_restore_check /tmp/rankanything.dump

# 3. Confirm the data actually arrived, not just that the schema did.
psql -d rankanything_restore_check -c "
  SELECT (SELECT count(*) FROM users)            AS users,
         (SELECT count(*) FROM rankings)         AS rankings,
         (SELECT count(*) FROM ranking_versions) AS versions,
         (SELECT count(*) FROM ranking_items)    AS items;"

# 4. Point a local binary at the restored copy and open it.
DATABASE_URL='postgres://postgres:postgres@localhost:5432/rankanything_restore_check?sslmode=disable' \
  PORT=8002 BASE_URL='http://localhost:8002' go run ./cmd/rankanything

# 5. Clean up.
dropdb rankanything_restore_check
```

Step 4 is the part that makes this a real test: it proves the dump restores
into a schema the current binary can actually serve, which a row count alone
does not. Do it after any migration that rewrites existing data.

## Rolling back

Render's dashboard rolls back to a previous deploy. That reverts the
*binary*, not the database — goose does not run down-migrations on rollback,
and the older binary will happily run against the newer schema as long as
the migration was additive.

A migration that drops or renames something is therefore not safely
rollback-able on its own. Split those: deploy the additive half, let it
settle, and remove the old column in a later deploy.

## Configuration reference

| Variable | Required | Default | Purpose |
| -------- | -------- | ------- | ------- |
| `DATABASE_URL` | yes | — | Postgres connection string. Boot fails without it. |
| `PORT` | no | `8001` | Render injects this; do not set it by hand. |
| `APP_ENV` | no | `development` | `production` turns on secure cookies and hides `/components`. |
| `BASE_URL` | no | `http://localhost:8001` | Origin used to build emailed links. |
| `RESEND_API_KEY` | no | — | Unset means email is logged, not sent. |
| `EMAIL_FROM` | no | `Rank Anything <onboarding@resend.dev>` | Must be a Resend-verified sender. |

There is no `.env` file in production. `config.Load` reads one when it is
present for local development and ignores its absence, so the container runs
on injected environment variables alone.
