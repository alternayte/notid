# Back up and restore an installation

This page is for the operator of an installation. It covers the database, the
data volume and the configuration. It does not cover the backups that the owners
of a community keep. Those are on the page about moving a community.

## What a backup holds

An installation keeps its state in three places. Back up all three.

| Part | Where | What it holds |
| --- | --- | --- |
| Database | the `pgdata` volume | people, communities, posts, comments, votes, reports, sessions, federation state |
| Data volume | the `data` volume, mounted at `/data` | uploaded images in `/data/media`, TLS certificates in `/data/certs` |
| Configuration | `.env` next to `compose.yaml` | `SECRET_KEY`, `APP_DOMAIN`, mail settings, `POSTGRES_PASSWORD` |

`SECRET_KEY` encrypts the private keys of every person and community. A restore
without the same `SECRET_KEY` loses those keys. Peers then refuse every activity
that the installation signs. Keep a copy of `.env` with each backup.

The database holds email addresses, reports and the names of reporters. Store
backups encrypted, and on a different machine from the installation.

## Make a backup

Run these commands in the directory that holds `compose.yaml`. The stack stays
up while they run.

1. Dump the database. `pg_dump` reads one consistent snapshot.

   ```sh
   docker compose exec -T postgres pg_dump -U notid -Fc notid > notid-$(date +%F).dump
   ```

2. Copy the data volume. The notid image has no shell, so a small Alpine
   container reads the volume.

   ```sh
   docker run --rm --volumes-from "$(docker compose ps -q notid)" -v "$PWD:/backup" \
     alpine tar czf "/backup/notid-data-$(date +%F).tar.gz" -C /data .
   ```

3. Copy `.env`.

   ```sh
   cp .env notid-env-$(date +%F)
   ```

4. Move the three files to the backup storage.

Do step 1 before step 2. An image uploaded between the two steps is then in the
data copy but not in the database, and notid deletes it after a day. The
opposite order would give database rows for images that the copy does not have.

To back up every night, put the three commands in a script and run the script
from cron.

## Restore

Use these steps to move the installation to a new server, or to recover after a
failure.

1. On the new server, install Docker. Copy `compose.yaml` and the saved `.env`
   into an empty directory. Keep `APP_DOMAIN` and `SECRET_KEY` as they were.

2. Point the DNS records of the app domain and of every community domain at the
   new server.

3. Start the database only.

   ```sh
   docker compose up -d postgres
   ```

4. Load the dump.

   ```sh
   docker compose exec -T postgres pg_restore -U notid -d notid --clean --if-exists --no-owner < notid-YYYY-MM-DD.dump
   ```

5. Create the notid container without starting it, then load the data copy into
   its volume.

   ```sh
   docker compose create notid
   docker run --rm --volumes-from "$(docker compose ps -aq notid)" -v "$PWD:/backup" \
     alpine tar xzpf /backup/notid-data-YYYY-MM-DD.tar.gz --numeric-owner -C /data
   ```

6. Start the stack. The `migrate` service runs first and brings the schema up to
   the version of the image.

   ```sh
   docker compose up -d
   ```

7. Check the installation.
   - `curl https://APP_DOMAIN/healthz` answers `ok`.
   - You can sign in, and an existing image post shows its image.
   - The admin page lists the peers. Their delivery health recovers within a few
     minutes.

## Notes

- Use an image of the same or a newer notid version for the restore. The
  migrations never go back to an older schema.
- Peers retry deliveries that fail. Activities they sent while the installation
  was down arrive when it is back, if the peer still retries then.
- Activities that this installation queued after the last backup are lost. The
  people who wrote them can post again.
