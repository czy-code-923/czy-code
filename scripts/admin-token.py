"""Display only the existing administrator token in your local terminal.
Never uploads, rotates or stores the token. Avoid shared terminals and recording.
"""
import argparse,os,sys
from pathlib import Path

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--env',help='Explicit private environment file for a manual installation');args=parser.parse_args()
    candidates=[Path(args.env)] if args.env else ([Path(os.environ.get('ProgramData','C:/ProgramData'))/'StarhubControl/control.env'] if os.name=='nt' else [Path('/etc/harbor/control.env'),Path('/opt/starhub-control/control.env')])
    files=[path for path in candidates if path.is_file() and not path.is_symlink()]
    if len(files)!=1:raise ValueError('Choose exactly one existing private file with --env; do not create a new token')
    values=[line.partition('=')[2] for line in files[0].read_text(encoding='utf-8-sig').splitlines() if line.startswith('ADMIN_TOKEN=')]
    if len(values)!=1 or len(values[0])<32:raise ValueError('No valid ADMIN_TOKEN found')
    print(values[0])

if __name__=='__main__':
    try:main()
    except (OSError,ValueError) as error:print(str(error),file=sys.stderr);sys.exit(1)
