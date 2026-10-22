"""Idempotently add missing generated settings; never rotate existing credentials."""
from pathlib import Path
import os
import secrets

root = Path(__file__).resolve().parents[1]
target = root / '.env'
if target.is_symlink():
    raise SystemExit('Refusing symlink .env')
values = {}
if target.exists():
    for line in target.read_text(encoding='utf-8').splitlines():
        if '=' in line and not line.startswith('#'):
            k, v = line.split('=', 1)
            values[k] = v
add = {key: secrets.token_hex(size) for key, size in {
    'POSTGRES_PASSWORD': 24, 'ADMIN_TOKEN': 32, 'ENGINE_SECRET': 32, 'MASTER_KEY': 32
}.items() if key not in values}
if any(not values.get(k, add.get(k, '')) for k in ('POSTGRES_PASSWORD','ADMIN_TOKEN','MASTER_KEY')):
    raise SystemExit('Existing required setting is empty; repair it explicitly.')
if add:
    # The append keeps previous comments and all existing values intact.
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    with os.fdopen(fd, 'a', encoding='utf-8', newline='\n') as out:
        out.write('\n' + ''.join(f'{k}={v}\n' for k,v in add.items()))
if os.name != 'nt':
    target.chmod(0o600)
print('Configuration ready. Existing credentials preserved; secrets are not printed.')
