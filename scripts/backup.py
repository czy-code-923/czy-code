"""Compose database backup. Output is private and never overwritten."""
from pathlib import Path
import os
import subprocess
import sys
root=Path(__file__).resolve().parents[1]
path=Path(sys.argv[1] if len(sys.argv)>1 else 'harbor-backup.dump').resolve()
fd=os.open(path,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600)
try:
    with os.fdopen(fd,'wb') as out:
        subprocess.run(['docker','compose','exec','-T','db','pg_dump','-U','harbor','-Fc','harbor'],cwd=root,stdout=out,check=True)
except BaseException:
    print('Backup failed; partial output retained for inspection.',file=sys.stderr)
    raise
print('Database backup created. Also back up .env (MASTER_KEY) separately and securely.')
