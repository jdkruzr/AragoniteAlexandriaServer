"""Reproduce backup path disagreement without running backup or container commands.
Run from the audited repository root: python3 docs/surveys/20261018/backup-selection.py
"""
from pathlib import Path
import os
import subprocess
import tempfile

common = Path('deploy/compose/common.sh').read_text()
probe = '''set -eu
. ./common.sh
resolved=${ALEXANDRIA_BACKUP_DIR:-./backups}
dir=$(sed -n 's/^ALEXANDRIA_BACKUP_DIR=//p' .env 2>/dev/null)
dir=${dir:-./backups}
printf 'common=%s\nbackup-script=%s\n' "$resolved" "$dir"
'''
for name, config in [
    ('derived-from-data-dir', 'ALEXANDRIA_DATA_DIR=/tmp/survey-data\n'),
    ('quoted-backup-dir', "ALEXANDRIA_BACKUP_DIR='/tmp/survey backups'\n"),
    ('simple-explicit-control', 'ALEXANDRIA_BACKUP_DIR=/tmp/survey-backups\n'),
]:
    with tempfile.TemporaryDirectory(prefix='alexandria-path-survey-') as folder:
        root = Path(folder)
        (root / 'common.sh').write_text(common)
        (root / '.env').write_text(config)
        env = {k: v for k, v in os.environ.items() if not k.startswith('ALEXANDRIA_')}
        result = subprocess.check_output(['sh'], input=probe, text=True, cwd=root, env=env)
        lines = result.splitlines()
        matches = lines[0].partition('=')[2] == lines[1].partition('=')[2]
        assert matches == (name == 'simple-explicit-control'), (name, result)
        print(name + ': ' + result.strip().replace('\n', '; '))
