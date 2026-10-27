"""Generate local development credentials without printing them or overwriting existing values."""
from pathlib import Path
import secrets
import os

root = Path(__file__).resolve().parents[1]
target = root / '.env'
with target.open('x', encoding='utf-8') as f:
    f.write(f'MASTER_KEY={secrets.token_hex(32)}\nPOSTGRES_PASSWORD={secrets.token_hex(24)}\nADMIN_TOKEN={secrets.token_hex(32)}\nENGINE_SECRET={secrets.token_hex(32)}\n')
if os.name != 'nt':
    target.chmod(0o600)
print('Created .env. Keep it private. Existing files are never overwritten.')
