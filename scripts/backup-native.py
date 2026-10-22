"""Private, streaming backup for the documented native control + Docker PostgreSQL setup."""
import datetime
import gzip
import os
from pathlib import Path
import shutil
import subprocess
import tarfile


def main():
    if os.geteuid() != 0:
        raise SystemExit('Run as root to include private environment files.')
    os.umask(0o077)
    base = Path('/var/backups/harbor')
    base.mkdir(mode=0o700, parents=True, exist_ok=True)
    dest = base / datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    dest.mkdir(mode=0o700)
    partial = dest / 'database.sql.gz.partial'
    with gzip.open(partial, 'wb') as stream:
        process = subprocess.Popen(['docker', 'exec', 'harbor-postgres', 'pg_dump', '-U', 'harbor', '-d', 'harbor', '--no-owner'], stdout=subprocess.PIPE)
        try:
            shutil.copyfileobj(process.stdout, stream)
        except BaseException:
            process.kill()
            process.wait()
            raise
        finally:
            process.stdout.close()
        if process.wait() != 0:
            raise RuntimeError('pg_dump failed; partial backup retained for inspection')
    partial.rename(dest / 'database.sql.gz')
    with tarfile.open(dest / 'private-settings.tar.gz', 'w:gz') as archive:
        archive.add('/etc/harbor', arcname='etc/harbor')
    print('Harbor backup completed:', dest.name)


if __name__ == '__main__':
    main()
