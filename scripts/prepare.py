"""Generate private installation secrets and replace only known example values.
Existing real credentials are never rotated by rerunning the installer.
"""
from pathlib import Path
import os,secrets,tempfile

root=Path(__file__).resolve().parents[1]
target=root/'.env'
if target.is_symlink():raise SystemExit('Refusing symlink .env')
text=target.read_text(encoding='utf-8') if target.exists() else ''
lines=text.splitlines();values={}
for line in lines:
 if '=' in line and not line.startswith('#'):
  key,value=line.split('=',1)
  if key in values:raise SystemExit('Duplicate configuration key; repair explicitly: '+key)
  values[key]=value
sizes={'POSTGRES_PASSWORD':24,'ADMIN_TOKEN':32,'ENGINE_SECRET':32,'MASTER_KEY':32}
placeholders={'generate-a-random-password','generate-at-least-32-random-bytes','generate-and-preserve-a-separate-random-engine-secret','generate-and-preserve-64-hex-characters','generate-64-hex-characters','change-me','changeme'}
changed={}
for key,size in sizes.items():
 if key not in values or values[key] in placeholders or values[key].startswith('generate-'):
  changed[key]=secrets.token_hex(size)
 elif not values[key]:raise SystemExit('Existing required setting is empty; repair explicitly: '+key)
if changed:
 output=[];done=set()
 for line in lines:
  key=line.partition('=')[0]
  if key in changed:output.append(key+'='+changed[key]);done.add(key)
  else:output.append(line)
 output.extend(key+'='+value for key,value in changed.items() if key not in done)
 fd,temporary=tempfile.mkstemp(prefix='.env-',dir=root)
 with os.fdopen(fd,'w',encoding='utf-8',newline='\n') as out:out.write('\n'.join(output)+'\n');out.flush();os.fsync(out.fileno())
 os.chmod(temporary,0o600);os.replace(temporary,target)
if os.name!='nt':target.chmod(0o600)
print('Configuration ready. Unique installation secrets generated where needed; existing real credentials preserved. Secrets are not printed.')
